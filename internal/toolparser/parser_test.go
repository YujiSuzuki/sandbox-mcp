package toolparser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testToolsDir returns the tools directory for integration tests.
// Uses SANDBOX_TOOLS_DIR env var if set, otherwise falls back to the
// default ai-sandbox path. Skips the test if the directory is not available.
func testToolsDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("SANDBOX_TOOLS_DIR")
	if dir == "" {
		dir = "/workspace/.sandbox/tools"
	}
	if _, err := os.Stat(dir); err != nil {
		t.Skip("Tools directory not found (set SANDBOX_TOOLS_DIR to run integration tests)")
	}
	return dir
}

func TestListTools(t *testing.T) {
	toolsDir := testToolsDir(t)

	tools, err := ListTools(toolsDir)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	if len(tools) == 0 {
		t.Fatal("Expected at least one tool")
	}

	// search-history.go should be found
	found := false
	for _, tool := range tools {
		if tool.Name == "search-history.go" {
			found = true
			if tool.Description == "" {
				t.Error("Expected non-empty description for search-history.go")
			}
			if tool.Usage == "" {
				t.Error("Expected non-empty usage for search-history.go")
			}
			if len(tool.Examples) == 0 {
				t.Error("Expected at least one example for search-history.go")
			}
		}
	}
	if !found {
		t.Error("search-history.go not found in tools list")
	}
}

func TestGetDetailedInfoPathTraversal(t *testing.T) {
	dir := t.TempDir()

	_, err := GetDetailedInfo(dir, "../etc/passwd")
	if err == nil {
		t.Error("Expected error for path traversal")
	}
}

func TestListToolsNonexistentDir(t *testing.T) {
	_, err := ListTools("/nonexistent/directory/xyz")
	if err == nil {
		t.Error("Expected error for non-existent directory")
	}
}

func TestGetDetailedInfoNonexistentTool(t *testing.T) {
	toolsDir := testToolsDir(t)

	_, err := GetDetailedInfo(toolsDir, "does-not-exist.go")
	if err == nil {
		t.Error("Expected error for non-existent tool")
	}
}

func TestParseGoHeaderMinimalFile(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "minimal.go")
	os.WriteFile(tool, []byte("package main\n"), 0644)

	info, err := parseGoHeader(tool)
	if err != nil {
		t.Fatalf("parseGoHeader: %v", err)
	}
	if info.Name != "minimal.go" {
		t.Errorf("Name = %q, want %q", info.Name, "minimal.go")
	}
	// No comment header → empty description
	if info.Description != "" {
		t.Errorf("Description = %q, want empty", info.Description)
	}
}

func TestParseGoHeaderFullFormat(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "full.go")
	os.WriteFile(tool, []byte(`// A tool that does something
//
// Usage:
//   go run full.go [options]
//
// Examples:
//   go run full.go --verbose
//   go run full.go "search term"
package main
`), 0644)

	info, err := parseGoHeader(tool)
	if err != nil {
		t.Fatalf("parseGoHeader: %v", err)
	}
	if info.Description != "A tool that does something" {
		t.Errorf("Description = %q, want %q", info.Description, "A tool that does something")
	}
	if !strings.Contains(info.Usage, "go run full.go") {
		t.Errorf("Usage = %q, want to contain %q", info.Usage, "go run full.go")
	}
	if len(info.Examples) != 2 {
		t.Errorf("Examples count = %d, want 2", len(info.Examples))
	}
	if info.Examples[0] != `go run full.go --verbose` {
		t.Errorf("Examples[0] = %q, want %q", info.Examples[0], "go run full.go --verbose")
	}
}

func TestListToolsSkipsNonGo(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "tool.go"), []byte("// a tool\npackage main\n"), 0644)
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# readme"), 0644)

	tools, err := ListTools(dir)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 {
		t.Errorf("Expected 1 tool, got %d", len(tools))
	}
	for _, tool := range tools {
		if tool.Name == "readme.md" {
			t.Error("Non-.go file should be excluded")
		}
	}
}

// TestListToolsSkipsTestFiles verifies that _test.go files are excluded from tool listing.
// TestListToolsSkipsTestFiles は _test.go ファイルがツール一覧から除外されることを検証する。
func TestListToolsSkipsTestFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "tool.go"), []byte("// a tool\npackage main\n"), 0644)
	os.WriteFile(filepath.Join(dir, "tool_test.go"), []byte("// test file\npackage main\n"), 0644)

	tools, err := ListTools(dir)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range tools {
		if tool.Name == "tool_test.go" {
			t.Error("_test.go file should be excluded from tools")
		}
	}
	if len(tools) != 1 {
		t.Errorf("Expected 1 tool, got %d", len(tools))
	}
}

func TestParseGoHeaderSeparatorStopsParser(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "bilingual.go")
	os.WriteFile(tool, []byte(`// A bilingual tool
//
// Usage:
//   go run bilingual.go [options] <pattern>
//
// Examples:
//   go run bilingual.go "hello"
//   go run bilingual.go -i "world"
//
// --- localized description (not parsed) ---
//
// このツールは二言語対応です。
//
// 使い方:
//   go run bilingual.go "こんにちは"
//
// 例:
//   go run bilingual.go -i "世界"
package main
`), 0644)

	info, err := parseGoHeader(tool)
	if err != nil {
		t.Fatalf("parseGoHeader: %v", err)
	}
	if info.Description != "A bilingual tool" {
		t.Errorf("Description = %q, want %q", info.Description, "A bilingual tool")
	}
	if info.Usage == "" {
		t.Error("Expected non-empty usage")
	}
	if len(info.Examples) != 2 {
		t.Errorf("Examples count = %d, want 2", len(info.Examples))
	}
	// Japanese examples should NOT be included
	for _, ex := range info.Examples {
		if strings.Contains(ex, "こんにちは") || strings.Contains(ex, "世界") {
			t.Errorf("Japanese example should not be parsed: %q", ex)
		}
	}
}

// TestParseGoHeaderContentBetweenSections verifies that text between sections
// (after a blank line but before the next section header) is NOT captured.
// This was broken by a dead-code bug: the section reset on blank lines never fired.
func TestParseGoHeaderContentBetweenSections(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "noted.go")
	os.WriteFile(tool, []byte(`// A tool with a note between sections
//
// Usage:
//   go run noted.go [options]
//
// Note: requires Go 1.21+
//
// Examples:
//   go run noted.go "hello"
package main
`), 0644)

	info, err := parseGoHeader(tool)
	if err != nil {
		t.Fatalf("parseGoHeader: %v", err)
	}

	// Usage should only contain the command line, NOT the "Note:" line
	if strings.Contains(info.Usage, "Note:") {
		t.Errorf("Usage should not contain inter-section text, got: %q", info.Usage)
	}
	if !strings.Contains(info.Usage, "go run noted.go") {
		t.Errorf("Usage should contain the command, got: %q", info.Usage)
	}

	// Examples should be parsed correctly
	if len(info.Examples) != 1 {
		t.Errorf("Examples count = %d, want 1", len(info.Examples))
	}
}

func TestGetDetailedInfoSlash(t *testing.T) {
	_, err := GetDetailedInfo("/workspace/.sandbox/tools", "subdir/tool.go")
	if err == nil {
		t.Error("Expected error for path with slash")
	}
}
