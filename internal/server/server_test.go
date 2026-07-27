package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YujiSuzuki/sandbox-mcp/internal/jsonrpc"
)

// newTestServer creates a server backed by empty temporary directories.
// Use for protocol-level tests that do not require real script or tool files.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	return New(t.TempDir(), t.TempDir(), "", "test", "")
}

// newServerWithFixtures creates a server with minimal fixture files.
// Use for tests that exercise real script and tool content.
func newServerWithFixtures(t *testing.T) *Server {
	t.Helper()
	scriptsDir := t.TempDir()
	toolsDir := t.TempDir()

	// validate-secrets.sh: utility category fixture
	if err := os.WriteFile(filepath.Join(scriptsDir, "validate-secrets.sh"), []byte(
		"#!/bin/bash\n"+
			"# validate-secrets.sh\n"+
			"# Validates secret file synchronization\n"), 0755); err != nil {
		t.Fatalf("failed to create script fixture: %v", err)
	}
	// test-validate-secrets.sh: test category fixture (name prefix "test-")
	if err := os.WriteFile(filepath.Join(scriptsDir, "test-validate-secrets.sh"), []byte(
		"#!/bin/bash\n"+
			"# test-validate-secrets.sh\n"+
			"# Tests validate-secrets\n"), 0755); err != nil {
		t.Fatalf("failed to create test script fixture: %v", err)
	}
	// install-slash-command.sh: advertised script fixture
	if err := os.WriteFile(filepath.Join(scriptsDir, "install-slash-command.sh"), []byte(
		"#!/bin/bash\n"+
			"# install-slash-command.sh\n"+
			"# Install a custom slash command\n"+
			"# @advertise: true\n"), 0755); err != nil {
		t.Fatalf("failed to create advertised script fixture: %v", err)
	}
	// search-history.go: tool fixture with required header format
	if err := os.WriteFile(filepath.Join(toolsDir, "search-history.go"), []byte(
		"// search-history - searches AI conversation history\n"+
			"//\n"+
			"// Usage:\n"+
			"//   go run search-history.go [options] <pattern>\n"+
			"//\n"+
			"// Examples:\n"+
			"//   go run search-history.go \"error\"\n"+
			"package main\n\n"+
			"import \"fmt\"\n\n"+
			"func main() { fmt.Println(\"search\") }\n"), 0644); err != nil {
		t.Fatalf("failed to create tool fixture: %v", err)
	}

	return New(scriptsDir, toolsDir, "", "test", "")
}

// initServer initializes a test server and returns it ready for tools/call.
func initServer(t *testing.T) *Server {
	t.Helper()
	srv := newTestServer(t)
	srv.HandleRequest(&jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "initialize",
		Params:  json.RawMessage(`{"clientInfo":{"name":"test"}}`),
	})
	return srv
}

// initServerWithFixtures initializes a fixture-backed server ready for tools/call.
func initServerWithFixtures(t *testing.T) *Server {
	t.Helper()
	srv := newServerWithFixtures(t)
	srv.HandleRequest(&jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "initialize",
		Params:  json.RawMessage(`{"clientInfo":{"name":"test"}}`),
	})
	return srv
}

// callTool sends a tools/call request and returns the response.
func callTool(srv *Server, toolName string, argsJSON string) *jsonrpc.Response {
	params := `{"name":"` + toolName + `"`
	if argsJSON != "" {
		params += `,"arguments":` + argsJSON
	}
	params += "}"
	return srv.HandleRequest(&jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      float64(2),
		Method:  "tools/call",
		Params:  json.RawMessage(params),
	})
}

func TestInitialize(t *testing.T) {
	srv := newTestServer(t)
	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "initialize",
		Params:  json.RawMessage(`{"clientInfo":{"name":"test"}}`),
	}

	resp := srv.HandleRequest(req)
	if resp == nil {
		t.Fatal("Expected response")
	}
	if resp.Error != nil {
		t.Fatalf("Unexpected error: %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("Expected map result")
	}
	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatal("Expected serverInfo")
	}
	if serverInfo["name"] != "sandbox-mcp" {
		t.Errorf("serverInfo.name = %v, want %q", serverInfo["name"], "sandbox-mcp")
	}
}

func TestInitializeInstructionsIncludesAdvertisedScripts(t *testing.T) {
	srv := newServerWithFixtures(t)
	resp := srv.HandleRequest(&jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "initialize",
		Params:  json.RawMessage(`{"clientInfo":{"name":"test"}}`),
	})
	if resp == nil || resp.Error != nil {
		t.Fatalf("Unexpected error: %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("Expected map result")
	}
	instructions, ok := result["instructions"].(string)
	if !ok {
		t.Fatal("Expected instructions string")
	}
	if !strings.Contains(instructions, "install-slash-command.sh") {
		t.Errorf("Expected advertised script in instructions, got:\n%s", instructions)
	}
	if strings.Contains(instructions, "validate-secrets.sh") {
		t.Error("Non-advertised script should not appear in instructions")
	}
}

func TestToolsListRequiresInit(t *testing.T) {
	srv := newTestServer(t)
	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "tools/list",
	}

	resp := srv.HandleRequest(req)
	if resp.Error == nil {
		t.Error("Expected error when not initialized")
	}
}

func TestToolsListAfterInit(t *testing.T) {
	srv := newTestServer(t)

	// Initialize first
	initReq := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "initialize",
		Params:  json.RawMessage(`{"clientInfo":{"name":"test"}}`),
	}
	srv.HandleRequest(initReq)

	// Now list tools
	listReq := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      float64(2),
		Method:  "tools/list",
	}
	resp := srv.HandleRequest(listReq)
	if resp.Error != nil {
		t.Fatalf("Unexpected error: %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("Expected map result")
	}
	tools, ok := result["tools"].([]toolDef)
	if !ok {
		t.Fatal("Expected tools array")
	}
	if len(tools) != 6 {
		t.Errorf("Expected 6 tools, got %d", len(tools))
	}
}

func TestToolsCallListScripts(t *testing.T) {
	srv := initServerWithFixtures(t)

	resp := callTool(srv, "list_scripts", `{"category":"utility"}`)
	if resp.Error != nil {
		t.Fatalf("Unexpected error: %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("Expected map result")
	}
	content, ok := result["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		t.Fatal("Expected at least one content block")
	}
	text, ok := content[0]["text"].(string)
	if !ok || text == "" {
		t.Error("Expected non-empty text content")
	}
	if !strings.Contains(text, "validate-secrets.sh") {
		t.Error("Expected validate-secrets.sh in utility scripts list")
	}
}

func TestUnknownMethod(t *testing.T) {
	srv := newTestServer(t)
	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "unknown/method",
	}
	resp := srv.HandleRequest(req)
	if resp.Error == nil {
		t.Error("Expected error for unknown method")
	}
	if resp.Error.Code != jsonrpc.CodeMethodNotFound {
		t.Errorf("Error code = %d, want %d", resp.Error.Code, jsonrpc.CodeMethodNotFound)
	}
}

func TestNotificationNoResponse(t *testing.T) {
	srv := newTestServer(t)

	// Initialize first
	srv.HandleRequest(&jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "initialize",
		Params:  json.RawMessage(`{"clientInfo":{"name":"test"}}`),
	})

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	resp := srv.HandleRequest(req)
	if resp != nil {
		t.Error("Expected nil response for notification")
	}
}

func TestToolsCallRequiresInit(t *testing.T) {
	srv := newTestServer(t) // NOT initialized
	resp := srv.HandleRequest(&jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"list_scripts"}`),
	})
	if resp.Error == nil {
		t.Error("Expected error when tools/call before initialize")
	}
	if resp.Error.Code != jsonrpc.CodeInternalError {
		t.Errorf("Error code = %d, want %d", resp.Error.Code, jsonrpc.CodeInternalError)
	}
}

func TestToolsCallGetScriptInfo(t *testing.T) {
	srv := initServerWithFixtures(t)
	resp := callTool(srv, "get_script_info", `{"name":"validate-secrets.sh"}`)
	if resp.Error != nil {
		t.Fatalf("Unexpected error: %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("Expected map result")
	}
	content, ok := result["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		t.Fatal("Expected content array with at least one entry")
	}
	text, _ := content[0]["text"].(string)
	if text == "" {
		t.Error("Expected non-empty text content")
	}
	if _, hasErr := result["isError"]; hasErr {
		t.Error("Unexpected isError in response")
	}
}

func TestToolsCallGetScriptInfoMissingName(t *testing.T) {
	srv := initServer(t)
	resp := callTool(srv, "get_script_info", `{}`)
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("Expected map result")
	}
	isError, _ := result["isError"].(bool)
	if !isError {
		t.Error("Expected isError=true for missing name param")
	}
}

func TestToolsCallListTools(t *testing.T) {
	srv := initServerWithFixtures(t)
	resp := callTool(srv, "list_tools", "")
	if resp.Error != nil {
		t.Fatalf("Unexpected error: %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("Expected map result")
	}
	content, ok := result["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		t.Fatal("Expected content array with at least one entry")
	}
	text, _ := content[0]["text"].(string)
	if text == "" {
		t.Error("Expected non-empty text content")
	}
	if !strings.Contains(text, "search-history.go") {
		t.Error("Expected list_tools to include search-history.go fixture")
	}
}

func TestToolsCallGetToolInfo(t *testing.T) {
	srv := initServerWithFixtures(t)
	resp := callTool(srv, "get_tool_info", `{"name":"search-history.go"}`)
	if resp.Error != nil {
		t.Fatalf("Unexpected error: %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("Expected map result")
	}
	content, ok := result["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		t.Fatal("Expected content array with at least one entry")
	}
	text, _ := content[0]["text"].(string)
	if text == "" {
		t.Error("Expected non-empty text content")
	}
	if _, hasErr := result["isError"]; hasErr {
		t.Error("Unexpected isError in response")
	}
}

func TestToolsCallGetToolInfoMissingName(t *testing.T) {
	srv := initServer(t)
	resp := callTool(srv, "get_tool_info", `{}`)
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("Expected map result")
	}
	isError, _ := result["isError"].(bool)
	if !isError {
		t.Error("Expected isError=true for missing name param")
	}
}

func TestToolsCallRunScriptMissingName(t *testing.T) {
	srv := initServer(t)
	resp := callTool(srv, "run_script", `{}`)
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("Expected map result")
	}
	isError, _ := result["isError"].(bool)
	if !isError {
		t.Error("Expected isError=true for missing name param")
	}
}

func TestToolsCallRunToolMissingName(t *testing.T) {
	srv := initServer(t)
	resp := callTool(srv, "run_tool", `{}`)
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("Expected map result")
	}
	isError, _ := result["isError"].(bool)
	if !isError {
		t.Error("Expected isError=true for missing name param")
	}
}

func TestToolsCallUnknownTool(t *testing.T) {
	srv := initServer(t)
	resp := callTool(srv, "nonexistent_tool", "")
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("Expected map result")
	}
	isError, _ := result["isError"].(bool)
	if !isError {
		t.Error("Expected isError=true for unknown tool")
	}
}

func TestToolsCallInvalidParams(t *testing.T) {
	srv := initServer(t)
	resp := srv.HandleRequest(&jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      float64(2),
		Method:  "tools/call",
		Params:  json.RawMessage(`not valid json`),
	})
	if resp.Error == nil {
		t.Error("Expected JSON-RPC error for invalid params")
	}
	if resp.Error.Code != jsonrpc.CodeInvalidParams {
		t.Errorf("Error code = %d, want %d", resp.Error.Code, jsonrpc.CodeInvalidParams)
	}
}

// TestToolsCallRunScriptSuccess verifies that a successful script execution
// is returned as textContent (no isError) — the critical wiring between
// executor output and the MCP response.
func TestToolsCallRunScriptSuccess(t *testing.T) {
	srv := initServerWithFixtures(t)
	resp := callTool(srv, "run_script", `{"name":"validate-secrets.sh"}`)
	if resp.Error != nil {
		t.Fatalf("Unexpected JSON-RPC error: %v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("Expected map result")
	}
	if isError, _ := result["isError"].(bool); isError {
		content, _ := result["content"].([]map[string]any)
		if len(content) > 0 {
			t.Fatalf("Expected no isError, got error content: %v", content[0]["text"])
		}
		t.Fatal("Expected isError=false for successful script execution")
	}
	content, ok := result["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		t.Fatal("Expected content array")
	}
	if content[0]["type"] != "text" {
		t.Errorf("content[0].type = %v, want \"text\"", content[0]["type"])
	}
}

// TestToolsCallRunScriptNotFound verifies that an executor error (e.g. script
// file not found on disk) is surfaced as isError=true, not a silent success.
func TestToolsCallRunScriptNotFound(t *testing.T) {
	srv := initServer(t) // empty dir — no scripts
	resp := callTool(srv, "run_script", `{"name":"ghost.sh"}`)
	if resp.Error != nil {
		t.Fatalf("Unexpected JSON-RPC error: %v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("Expected map result")
	}
	isError, _ := result["isError"].(bool)
	if !isError {
		t.Error("Expected isError=true for nonexistent script")
	}
	content, ok := result["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		t.Fatal("Expected content array")
	}
	text, _ := content[0]["text"].(string)
	if !strings.Contains(text, "Execution failed") {
		t.Errorf("Expected 'Execution failed' in error text, got: %q", text)
	}
}

// TestToolsCallRunToolSuccess verifies the happy-path wiring: executor output
// is returned as textContent with the tool's actual stdout.
func TestToolsCallRunToolSuccess(t *testing.T) {
	srv := initServerWithFixtures(t)
	resp := callTool(srv, "run_tool", `{"name":"search-history.go"}`)
	if resp.Error != nil {
		t.Fatalf("Unexpected JSON-RPC error: %v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("Expected map result")
	}
	if isError, _ := result["isError"].(bool); isError {
		content, _ := result["content"].([]map[string]any)
		if len(content) > 0 {
			t.Fatalf("Expected no isError, got error content: %v", content[0]["text"])
		}
		t.Fatal("Expected isError=false for successful tool execution")
	}
	content, ok := result["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		t.Fatal("Expected content array")
	}
	text, _ := content[0]["text"].(string)
	if !strings.Contains(text, "search") {
		t.Errorf("Expected tool output to contain 'search', got: %q", text)
	}
}

// TestToolsCallRunToolNonZeroExit verifies that a tool that exits with a
// non-zero code returns textContent (stderr + exit code), NOT isError=true.
// The executor treats non-zero exit as a result (not an error), so the server
// must pass it through as text so the caller can read the diagnostics.
func TestToolsCallRunToolNonZeroExit(t *testing.T) {
	// Write a tool that explicitly exits non-zero
	scriptsDir := t.TempDir()
	toolsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(toolsDir, "fail.go"), []byte(
		"package main\nimport \"os\"\nfunc main() { os.Exit(1) }\n"), 0644); err != nil {
		t.Fatalf("failed to create failing tool: %v", err)
	}
	srv := New(scriptsDir, toolsDir, "", "test", "")
	srv.HandleRequest(&jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "initialize",
		Params:  json.RawMessage(`{"clientInfo":{"name":"test"}}`),
	})

	resp := callTool(srv, "run_tool", `{"name":"fail.go"}`)
	if resp.Error != nil {
		t.Fatalf("Unexpected JSON-RPC error: %v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("Expected map result")
	}
	// Non-zero exit is textContent, not isError
	if isError, _ := result["isError"].(bool); isError {
		t.Error("Expected isError=false: non-zero exit should be textContent so caller sees diagnostics")
	}
	content, ok := result["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		t.Fatal("Expected content array")
	}
	text, _ := content[0]["text"].(string)
	if !strings.Contains(text, "exit code") {
		t.Errorf("Expected exit code info in tool output, got: %q", text)
	}
}

func TestScanGitRepos_EmptyWorkspaceDir(t *testing.T) {
	repos := scanGitRepos("", 3)
	if repos != nil {
		t.Errorf("Expected nil for empty workspaceDir, got %v", repos)
	}
}

func TestScanGitRepos_EmptyWorkspace(t *testing.T) {
	repos := scanGitRepos(t.TempDir(), 3)
	if len(repos) != 0 {
		t.Errorf("Expected no repos in empty workspace, got %v", repos)
	}
}

func TestScanGitRepos_FindsNestedRepo(t *testing.T) {
	workspaceDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspaceDir, "my-app", ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	repos := scanGitRepos(workspaceDir, 3)
	if len(repos) != 1 || repos[0] != "my-app" {
		t.Errorf("Expected [my-app], got %v", repos)
	}
}

func TestScanGitRepos_ExcludesWorkspaceRoot(t *testing.T) {
	workspaceDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspaceDir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	repos := scanGitRepos(workspaceDir, 3)
	if len(repos) != 0 {
		t.Errorf("Expected workspace root excluded, got %v", repos)
	}
}

func TestScanGitRepos_RespectsMaxDepth(t *testing.T) {
	workspaceDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspaceDir, "a", "b", ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	if repos := scanGitRepos(workspaceDir, 1); len(repos) != 0 {
		t.Errorf("Expected no repos at maxDepth=1, got %v", repos)
	}
	if repos := scanGitRepos(workspaceDir, 2); len(repos) != 1 {
		t.Errorf("Expected 1 repo at maxDepth=2, got %v", repos)
	}
}

func TestScanGitRepos_SkipsHiddenDirs(t *testing.T) {
	workspaceDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspaceDir, ".hidden", ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	repos := scanGitRepos(workspaceDir, 3)
	if len(repos) != 0 {
		t.Errorf("Expected hidden dir to be skipped, got %v", repos)
	}
}

func TestScanGitRepos_WorkspaceRootHasGitButStillScansSubdirs(t *testing.T) {
	workspaceDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspaceDir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspaceDir, "nested-app", ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	repos := scanGitRepos(workspaceDir, 3)
	if len(repos) != 1 || repos[0] != "nested-app" {
		t.Errorf("Expected [nested-app], got %v", repos)
	}
}

func TestInitializeInstructionsIncludesGitRepos(t *testing.T) {
	workspaceDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspaceDir, "my-app", ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	srv := New(t.TempDir(), t.TempDir(), "", "test", workspaceDir)
	resp := srv.HandleRequest(&jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "initialize",
		Params:  json.RawMessage(`{"clientInfo":{"name":"test"}}`),
	})
	if resp == nil || resp.Error != nil {
		t.Fatalf("Unexpected error: %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("Expected map result")
	}
	instructions, ok := result["instructions"].(string)
	if !ok {
		t.Fatal("Expected instructions string")
	}
	if !strings.Contains(instructions, "my-app") {
		t.Errorf("Expected nested git repo in instructions, got:\n%s", instructions)
	}
}

func TestInitializeInstructionsNoGitReposSection(t *testing.T) {
	srv := New(t.TempDir(), t.TempDir(), "", "test", t.TempDir())
	resp := srv.HandleRequest(&jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "initialize",
		Params:  json.RawMessage(`{"clientInfo":{"name":"test"}}`),
	})
	if resp == nil || resp.Error != nil {
		t.Fatalf("Unexpected error: %v", resp.Error)
	}

	result, _ := resp.Result.(map[string]any)
	instructions, _ := result["instructions"].(string)
	if strings.Contains(instructions, "Nested git") {
		t.Errorf("Expected no git repos section when workspace has no nested repos, got:\n%s", instructions)
	}
}

func TestRunSetupScripts_EmptyPath(t *testing.T) {
	if out := runSetupScripts(""); out != "" {
		t.Errorf("Expected empty output for empty path, got %q", out)
	}
}

func TestRunSetupScripts_NonexistentDir(t *testing.T) {
	if out := runSetupScripts("/nonexistent/path"); out != "" {
		t.Errorf("Expected empty output for nonexistent dir, got %q", out)
	}
}

func TestRunSetupScripts_EmptyDir(t *testing.T) {
	if out := runSetupScripts(t.TempDir()); out != "" {
		t.Errorf("Expected empty output for empty dir, got %q", out)
	}
}

func TestRunSetupScripts_RunsShScript(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "info.sh"), []byte("#!/bin/bash\necho 'hello from setup'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	out := runSetupScripts(dir)
	if !strings.Contains(out, "hello from setup") {
		t.Errorf("Expected script output, got %q", out)
	}
}

func TestRunSetupScripts_SkipsNonShFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "info.py"), []byte("print('should not run')\n"), 0755); err != nil {
		t.Fatal(err)
	}
	out := runSetupScripts(dir)
	if strings.Contains(out, "should not run") {
		t.Errorf("Expected non-.sh files to be skipped, got %q", out)
	}
}

func TestRunSetupScripts_HandlesFailedScript(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fail.sh"), []byte("#!/bin/bash\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	// Should not panic; failed scripts are silently skipped
	_ = runSetupScripts(dir)
}

func TestRunSetupScripts_RunsInAlphabeticalOrder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "b.sh"), []byte("#!/bin/bash\necho 'B'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.sh"), []byte("#!/bin/bash\necho 'A'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	out := runSetupScripts(dir)
	aIdx := strings.Index(out, "A")
	bIdx := strings.Index(out, "B")
	if aIdx == -1 || bIdx == -1 || aIdx > bIdx {
		t.Errorf("Expected A before B in output, got %q", out)
	}
}

// TestInitializeInstructionsIncludesSetupOutput uses a setup dir at a
// non-default location (not workspaceDir/.sandbox/sandbox-mcp-setup) to prove
// buildInstructions() uses the explicitly configured s.setupDir rather than
// deriving it from workspaceDir: if the old hardcoded-join behavior were
// still in place, this fixture would never be found.
func TestInitializeInstructionsIncludesSetupOutput(t *testing.T) {
	workspaceDir := t.TempDir()
	setupDir := filepath.Join(workspaceDir, "custom-setup-scripts")
	if err := os.MkdirAll(setupDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(setupDir, "info.sh"), []byte("#!/bin/bash\necho 'custom project info'\n"), 0755); err != nil {
		t.Fatal(err)
	}

	srv := New(t.TempDir(), t.TempDir(), setupDir, "test", workspaceDir)
	resp := srv.HandleRequest(&jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "initialize",
		Params:  json.RawMessage(`{"clientInfo":{"name":"test"}}`),
	})
	if resp == nil || resp.Error != nil {
		t.Fatalf("Unexpected error: %v", resp.Error)
	}

	result, _ := resp.Result.(map[string]any)
	instructions, _ := result["instructions"].(string)
	if !strings.Contains(instructions, "custom project info") {
		t.Errorf("Expected setup script output in instructions, got:\n%s", instructions)
	}
}

// TestInitializeInstructionsSetupDirEmpty verifies that an empty setupDir
// (e.g. resulting from an explicit `setup_dir: ""` override) disables the
// setup-scripts feature without crashing, and does not fall back to scanning
// workspaceDir.
func TestInitializeInstructionsSetupDirEmpty(t *testing.T) {
	workspaceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspaceDir, "should-not-run.sh"), []byte("#!/bin/bash\necho 'should not run'\n"), 0755); err != nil {
		t.Fatal(err)
	}

	srv := New(t.TempDir(), t.TempDir(), "", "test", workspaceDir)
	resp := srv.HandleRequest(&jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "initialize",
		Params:  json.RawMessage(`{"clientInfo":{"name":"test"}}`),
	})
	if resp == nil || resp.Error != nil {
		t.Fatalf("Unexpected error: %v", resp.Error)
	}

	result, _ := resp.Result.(map[string]any)
	instructions, _ := result["instructions"].(string)
	if strings.Contains(instructions, "should not run") {
		t.Errorf("Expected no setup output when setupDir is empty, got:\n%s", instructions)
	}
}

func TestToolsCallListScriptsFilterCategory(t *testing.T) {
	srv := initServerWithFixtures(t)

	// "test" category should return test-validate-secrets.sh, not validate-secrets.sh
	resp := callTool(srv, "list_scripts", `{"category":"test"}`)
	if resp.Error != nil {
		t.Fatalf("Unexpected error: %v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("Expected map result")
	}
	content, ok := result["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		t.Fatal("Expected content array")
	}
	text, _ := content[0]["text"].(string)
	if strings.Contains(text, `"category": "utility"`) {
		t.Error("Expected only test category scripts when filtering by 'test'")
	}
	if !strings.Contains(text, "test-validate-secrets.sh") {
		t.Error("Expected test-validate-secrets.sh in test category results")
	}
}
