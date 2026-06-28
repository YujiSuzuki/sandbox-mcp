package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.ScriptsDir != ".sandbox/scripts" {
		t.Errorf("ScriptsDir = %q, want %q", cfg.ScriptsDir, ".sandbox/scripts")
	}
	if cfg.ToolsDir != ".sandbox/tools" {
		t.Errorf("ToolsDir = %q, want %q", cfg.ToolsDir, ".sandbox/tools")
	}
	if cfg.UpdateCheck != true {
		t.Errorf("UpdateCheck = %v, want true", cfg.UpdateCheck)
	}
}

func TestLoadConfigFromFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "sandbox-mcp.yaml")

	content := `scripts_dir: "/custom/scripts"
tools_dir: "/custom/tools"
update_check: false
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(cfgFile)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}

	if cfg.ScriptsDir != "/custom/scripts" {
		t.Errorf("ScriptsDir = %q, want %q", cfg.ScriptsDir, "/custom/scripts")
	}
	if cfg.ToolsDir != "/custom/tools" {
		t.Errorf("ToolsDir = %q, want %q", cfg.ToolsDir, "/custom/tools")
	}
	if cfg.UpdateCheck != false {
		t.Errorf("UpdateCheck = %v, want false", cfg.UpdateCheck)
	}
}

func TestLoadConfigPartialOverride(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "sandbox-mcp.yaml")

	// Only override scripts_dir; tools_dir and update_check should use defaults
	content := `scripts_dir: "/only/scripts"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(cfgFile)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}

	if cfg.ScriptsDir != "/only/scripts" {
		t.Errorf("ScriptsDir = %q, want %q", cfg.ScriptsDir, "/only/scripts")
	}
	if cfg.ToolsDir != ".sandbox/tools" {
		t.Errorf("ToolsDir = %q, want default %q", cfg.ToolsDir, ".sandbox/tools")
	}
	if cfg.UpdateCheck != true {
		t.Errorf("UpdateCheck = %v, want default true", cfg.UpdateCheck)
	}
}

func TestLoadConfigFileNotFound(t *testing.T) {
	cfg, err := LoadFile("/nonexistent/path/sandbox-mcp.yaml")
	if err != nil {
		t.Fatalf("LoadFile should not error on missing file, got: %v", err)
	}

	// Should return all defaults
	defaults := DefaultConfig()
	if cfg.ScriptsDir != defaults.ScriptsDir {
		t.Errorf("ScriptsDir = %q, want default %q", cfg.ScriptsDir, defaults.ScriptsDir)
	}
	if cfg.ToolsDir != defaults.ToolsDir {
		t.Errorf("ToolsDir = %q, want default %q", cfg.ToolsDir, defaults.ToolsDir)
	}
	if cfg.UpdateCheck != defaults.UpdateCheck {
		t.Errorf("UpdateCheck = %v, want default %v", cfg.UpdateCheck, defaults.UpdateCheck)
	}
}

func TestLoadConfigInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "sandbox-mcp.yaml")

	if err := os.WriteFile(cfgFile, []byte("{{invalid yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFile(cfgFile)
	if err == nil {
		t.Error("Expected error for invalid YAML")
	}
}

func TestLoadConfigEmptyFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "sandbox-mcp.yaml")

	if err := os.WriteFile(cfgFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(cfgFile)
	if err != nil {
		t.Fatalf("LoadFile failed on empty file: %v", err)
	}

	// Should return all defaults
	defaults := DefaultConfig()
	if cfg.ScriptsDir != defaults.ScriptsDir {
		t.Errorf("ScriptsDir = %q, want default %q", cfg.ScriptsDir, defaults.ScriptsDir)
	}
	if cfg.ToolsDir != defaults.ToolsDir {
		t.Errorf("ToolsDir = %q, want default %q", cfg.ToolsDir, defaults.ToolsDir)
	}
	if cfg.UpdateCheck != defaults.UpdateCheck {
		t.Errorf("UpdateCheck = %v, want default %v", cfg.UpdateCheck, defaults.UpdateCheck)
	}
}

// TestLoadConfigWithComments verifies that # comment lines and single-quoted values
// are parsed correctly by parseSimpleYAML.
func TestLoadConfigWithComments(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "sandbox-mcp.yaml")
	content := `# this is a comment
scripts_dir: '/single/quoted'
# another comment
tools_dir: "/double/quoted"
update_check: true
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(cfgFile)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	if cfg.ScriptsDir != "/single/quoted" {
		t.Errorf("ScriptsDir = %q, want %q (single-quoted value)", cfg.ScriptsDir, "/single/quoted")
	}
	if cfg.ToolsDir != "/double/quoted" {
		t.Errorf("ToolsDir = %q, want %q", cfg.ToolsDir, "/double/quoted")
	}
}

// TestLoadConfigWithInlineComment verifies that inline comments are stripped.
// e.g. `scripts_dir: /my/dir  # optional comment` → ScriptsDir = "/my/dir"
func TestLoadConfigWithInlineComment(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "sandbox-mcp.yaml")
	content := `scripts_dir: /my/scripts  # custom scripts path
tools_dir: /my/tools
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(cfgFile)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	if cfg.ScriptsDir != "/my/scripts" {
		t.Errorf("ScriptsDir = %q, want %q (inline comment should be stripped)", cfg.ScriptsDir, "/my/scripts")
	}
}

// TestLoadConfigWhitespaceOnlyFile verifies that a file containing only whitespace
// returns defaults (same as empty file).
func TestLoadConfigWhitespaceOnlyFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "sandbox-mcp.yaml")

	if err := os.WriteFile(cfgFile, []byte("   \n  \n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFile(cfgFile)
	if err != nil {
		t.Fatalf("LoadFile failed on whitespace-only file: %v", err)
	}

	defaults := DefaultConfig()
	if cfg.ScriptsDir != defaults.ScriptsDir {
		t.Errorf("ScriptsDir = %q, want default %q", cfg.ScriptsDir, defaults.ScriptsDir)
	}
}

func TestLoadWithEnvVars(t *testing.T) {
	t.Setenv("SANDBOX_SCRIPTS_DIR", "/env/scripts")
	t.Setenv("SANDBOX_TOOLS_DIR", "/env/tools")

	cfg := LoadWithEnv(DefaultConfig())

	if cfg.ScriptsDir != "/env/scripts" {
		t.Errorf("ScriptsDir = %q, want %q", cfg.ScriptsDir, "/env/scripts")
	}
	if cfg.ToolsDir != "/env/tools" {
		t.Errorf("ToolsDir = %q, want %q", cfg.ToolsDir, "/env/tools")
	}
}

func TestLoadWithEnvVarsPartial(t *testing.T) {
	// Only set one env var
	t.Setenv("SANDBOX_SCRIPTS_DIR", "/env/scripts")
	t.Setenv("SANDBOX_TOOLS_DIR", "")

	cfg := LoadWithEnv(DefaultConfig())

	if cfg.ScriptsDir != "/env/scripts" {
		t.Errorf("ScriptsDir = %q, want %q", cfg.ScriptsDir, "/env/scripts")
	}
	if cfg.ToolsDir != ".sandbox/tools" {
		t.Errorf("ToolsDir = %q, want default %q", cfg.ToolsDir, ".sandbox/tools")
	}
}

func TestFindConfigFile(t *testing.T) {
	// Create a temp workspace with .sandbox/config/sandbox-mcp.yaml
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".sandbox", "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(configDir, "sandbox-mcp.yaml")
	if err := os.WriteFile(cfgFile, []byte("scripts_dir: found"), 0644); err != nil {
		t.Fatal(err)
	}

	found := FindConfigFile(dir)
	if found != cfgFile {
		t.Errorf("FindConfigFile = %q, want %q", found, cfgFile)
	}
}

func TestFindConfigFileNotFound(t *testing.T) {
	dir := t.TempDir()
	found := FindConfigFile(dir)
	if found != "" {
		t.Errorf("FindConfigFile = %q, want empty string", found)
	}
}

// TestFindConfigFileHomeDir verifies the ~/.config/sandbox-mcp/config.yaml fallback.
// Overrides HOME so the test is self-contained and doesn't touch the real home directory.
func TestFindConfigFileHomeDir(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	configDir := filepath.Join(fakeHome, ".config", "sandbox-mcp")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("scripts_dir: from-home"), 0644); err != nil {
		t.Fatal(err)
	}

	// Use a workspace that has no .sandbox/config/ → falls through to home dir
	workspace := t.TempDir()
	found := FindConfigFile(workspace)
	if found != cfgFile {
		t.Errorf("FindConfigFile = %q, want home-dir fallback %q", found, cfgFile)
	}
}

func TestResolveFullPriority(t *testing.T) {
	// Setup: config file with one value, env var with another
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".sandbox", "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(configDir, "sandbox-mcp.yaml")
	if err := os.WriteFile(cfgFile, []byte("scripts_dir: from-file\ntools_dir: from-file"), 0644); err != nil {
		t.Fatal(err)
	}

	// Env var set for scripts_dir, but config file should win
	t.Setenv("SANDBOX_SCRIPTS_DIR", "from-env")
	t.Setenv("SANDBOX_TOOLS_DIR", "")

	cfg, err := Resolve(cfgFile, "", "", "")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Priority: CLI flag > config file > env var > default
	// scripts_dir: config file wins over env var
	if cfg.ScriptsDir != "from-file" {
		t.Errorf("ScriptsDir = %q, want %q (config file should override env)", cfg.ScriptsDir, "from-file")
	}
	// tools_dir: file wins over default
	if cfg.ToolsDir != "from-file" {
		t.Errorf("ToolsDir = %q, want %q (file should override default)", cfg.ToolsDir, "from-file")
	}
}

func TestResolveEnvVarSurvivesPartialConfigFile(t *testing.T) {
	// Config file sets only scripts_dir; env var sets tools_dir.
	// tools_dir should come from env var, NOT revert to default.
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "sandbox-mcp.yaml")
	if err := os.WriteFile(cfgFile, []byte("scripts_dir: from-file"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SANDBOX_SCRIPTS_DIR", "from-env")
	t.Setenv("SANDBOX_TOOLS_DIR", "from-env-tools")

	cfg, err := Resolve(cfgFile, "", "", "")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// scripts_dir: config file wins over env var
	if cfg.ScriptsDir != "from-file" {
		t.Errorf("ScriptsDir = %q, want %q (config file should override env)", cfg.ScriptsDir, "from-file")
	}
	// tools_dir: env var survives because config file didn't set it
	if cfg.ToolsDir != "from-env-tools" {
		t.Errorf("ToolsDir = %q, want %q (env var should survive when file doesn't set it)", cfg.ToolsDir, "from-env-tools")
	}
}

func TestResolveCLIFlagWins(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, ".sandbox", "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(configDir, "sandbox-mcp.yaml")
	if err := os.WriteFile(cfgFile, []byte("scripts_dir: from-file"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SANDBOX_SCRIPTS_DIR", "from-env")

	// CLI flag should win over everything
	cfg, err := Resolve(cfgFile, "from-flag", "", "")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if cfg.ScriptsDir != "from-flag" {
		t.Errorf("ScriptsDir = %q, want %q (CLI flag should win)", cfg.ScriptsDir, "from-flag")
	}
}

func TestResolveWithWorkspace(t *testing.T) {
	workspace := t.TempDir()

	// No config file, no flags → relative defaults resolved against workspace
	cfg, err := Resolve("", "", "", workspace)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	wantScripts := filepath.Join(workspace, ".sandbox/scripts")
	wantTools := filepath.Join(workspace, ".sandbox/tools")

	if cfg.ScriptsDir != wantScripts {
		t.Errorf("ScriptsDir = %q, want %q", cfg.ScriptsDir, wantScripts)
	}
	if cfg.ToolsDir != wantTools {
		t.Errorf("ToolsDir = %q, want %q", cfg.ToolsDir, wantTools)
	}
}

func TestResolveInvalidConfigFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(cfgFile, []byte("{{invalid yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Resolve(cfgFile, "", "", "")
	if err == nil {
		t.Error("Expected error for invalid config file")
	}
	// Config should still return all defaults (not zero value)
	defaults := DefaultConfig()
	if cfg.ScriptsDir != defaults.ScriptsDir {
		t.Errorf("ScriptsDir = %q, want default %q on error", cfg.ScriptsDir, defaults.ScriptsDir)
	}
	if cfg.ToolsDir != defaults.ToolsDir {
		t.Errorf("ToolsDir = %q, want default %q on error", cfg.ToolsDir, defaults.ToolsDir)
	}
	if cfg.UpdateCheck != defaults.UpdateCheck {
		t.Errorf("UpdateCheck = %v, want default %v on error", cfg.UpdateCheck, defaults.UpdateCheck)
	}
}

func TestResolveWorkspaceDoesNotOverrideAbsPath(t *testing.T) {
	workspace := t.TempDir()

	// CLI flag provides an absolute path → workspace should not change it
	cfg, err := Resolve("", "/abs/scripts", "/abs/tools", workspace)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if cfg.ScriptsDir != "/abs/scripts" {
		t.Errorf("ScriptsDir = %q, want %q (absolute path should not be changed)", cfg.ScriptsDir, "/abs/scripts")
	}
	if cfg.ToolsDir != "/abs/tools" {
		t.Errorf("ToolsDir = %q, want %q (absolute path should not be changed)", cfg.ToolsDir, "/abs/tools")
	}
}
