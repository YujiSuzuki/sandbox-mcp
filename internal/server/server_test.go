package server

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/YujiSuzuki/sandbox-mcp/internal/jsonrpc"
)

// newTestServer creates a server backed by empty temporary directories.
// Use for protocol-level tests that do not require real script or tool files.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	return New(t.TempDir(), t.TempDir(), "", "", "test", "")
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

	return New(scriptsDir, toolsDir, "", "", "test", "")
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
	srv := New(scriptsDir, toolsDir, "", "", "test", "")
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

	srv := New(t.TempDir(), t.TempDir(), "", "", "test", workspaceDir)
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
	srv := New(t.TempDir(), t.TempDir(), "", "", "test", t.TempDir())
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
	if out := runSetupScripts("", ""); out != "" {
		t.Errorf("Expected empty output for empty path, got %q", out)
	}
}

func TestRunSetupScripts_NonexistentDir(t *testing.T) {
	if out := runSetupScripts("/nonexistent/path", ""); out != "" {
		t.Errorf("Expected empty output for nonexistent dir, got %q", out)
	}
}

func TestRunSetupScripts_EmptyDir(t *testing.T) {
	if out := runSetupScripts(t.TempDir(), ""); out != "" {
		t.Errorf("Expected empty output for empty dir, got %q", out)
	}
}

func TestRunSetupScripts_RunsShScript(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "info.sh"), []byte("#!/bin/bash\necho 'hello from setup'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	out := runSetupScripts(dir, "")
	if !strings.Contains(out, "hello from setup") {
		t.Errorf("Expected script output, got %q", out)
	}
}

func TestRunSetupScripts_SkipsNonShFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "info.py"), []byte("print('should not run')\n"), 0755); err != nil {
		t.Fatal(err)
	}
	out := runSetupScripts(dir, "")
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
	_ = runSetupScripts(dir, "")
}

func TestRunSetupScripts_RunsInAlphabeticalOrder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "b.sh"), []byte("#!/bin/bash\necho 'B'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.sh"), []byte("#!/bin/bash\necho 'A'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	out := runSetupScripts(dir, "")
	aIdx := strings.Index(out, "A")
	bIdx := strings.Index(out, "B")
	if aIdx == -1 || bIdx == -1 || aIdx > bIdx {
		t.Errorf("Expected A before B in output, got %q", out)
	}
}

// TestRunSetupScripts_OutputFileTagSpillsToDisk verifies that a script whose
// header declares "@output: file" has its stdout written under outputDir
// instead of being inlined into the returned instructions text, with only a
// short pointer line surfacing inline. This keeps content that must
// reliably reach the AI out of the MCP instructions byte budget (see
// buildInstructions), which the client silently truncates once exceeded
// with no indication anything was cut.
//
// The file is written under a subdirectory named after this process's own
// PID (see pruneStaleOutputDirs) rather than directly under outputDir, so
// that concurrent sandbox-mcp instances sharing the same workspace (e.g.
// multiple Claude Code windows open on the same repo) never clobber each
// other's spilled output.
//
// ヘッダーで "@output: file" を宣言したスクリプトの標準出力が、返される
// instructions テキストに埋め込まれる代わりに outputDir 配下に書き出され、
// 埋め込み側には短いポインタ行だけが残ることを検証する。これにより、AI に
// 確実に届ける必要のある出力を MCP の instructions バイト予算(buildInstructions
// 参照。クライアントは超過分を痕跡なく無音のまま切り詰める)から切り離せる。
//
// ファイルは outputDir 直下ではなく、このプロセス自身の PID を名前とする
// サブディレクトリ(pruneStaleOutputDirs 参照)の下に書き出される。これに
// より、同じワークスペースを共有する複数の sandbox-mcp インスタンス(同じ
// リポジトリを開いた複数の Claude Code ウィンドウなど)が互いの書き出し
// 結果を上書きすることがない。
func TestRunSetupScripts_OutputFileTagSpillsToDisk(t *testing.T) {
	dir := t.TempDir()
	outputDir := t.TempDir()
	script := "# @output: file\necho 'verbose detail that should not be inlined'\n"
	if err := os.WriteFile(filepath.Join(dir, "big.sh"), []byte("#!/bin/bash\n"+script), 0755); err != nil {
		t.Fatal(err)
	}

	out := runSetupScripts(dir, outputDir)

	if strings.Contains(out, "verbose detail that should not be inlined") {
		t.Errorf("Expected @output: file script's stdout NOT to be inlined, got %q", out)
	}

	myDir := filepath.Join(outputDir, pidsSubdir, strconv.Itoa(os.Getpid()))
	if !strings.Contains(out, myDir) {
		t.Errorf("Expected inline output to mention directory %q, got %q", myDir, out)
	}
	if !strings.Contains(out, "big.txt") {
		t.Errorf("Expected inline output to mention filename %q, got %q", "big.txt", out)
	}

	wantFile := filepath.Join(myDir, "big.txt")
	written, err := os.ReadFile(wantFile)
	if err != nil {
		t.Fatalf("Expected output file %q to exist: %v", wantFile, err)
	}
	if !strings.Contains(string(written), "verbose detail that should not be inlined") {
		t.Errorf("Expected output file to contain script's stdout, got %q", string(written))
	}
}

// TestRunSetupScripts_DoesNotPruneUnrelatedDirsAtOutputRoot verifies that
// setup_output_dir can safely point at a directory that isn't exclusively
// sandbox-mcp's own scratch space: pruneStaleOutputDirs only ever scans
// outputDir/pidsSubdir, never outputDir itself, so a pre-existing, unrelated
// directory with an integer name (e.g. a version folder "12345") sitting
// directly at outputDir's root is left untouched.
func TestRunSetupScripts_DoesNotPruneUnrelatedDirsAtOutputRoot(t *testing.T) {
	dir := t.TempDir()
	outputDir := t.TempDir()
	unrelated := filepath.Join(outputDir, "12345")
	if err := os.MkdirAll(filepath.Join(unrelated, "important-data"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plain.sh"), []byte("#!/bin/bash\necho hi\n"), 0755); err != nil {
		t.Fatal(err)
	}

	runSetupScripts(dir, outputDir)

	if _, err := os.Stat(unrelated); err != nil {
		t.Errorf("Expected unrelated directory %q at outputDir root to survive, got: %v", unrelated, err)
	}
}

// TestRunSetupScripts_ConsolidatesMultipleFileTagsIntoOneLine verifies that
// when several scripts are tagged "@output: file", the instructions text
// gets exactly one pointer line naming all of them, rather than one full
// sentence per script. Without this, tagging more scripts over time would
// re-create the same linear growth in the instructions field that "@output:
// file" was introduced to avoid in the first place.
//
// 複数のスクリプトに "@output: file" が付いている場合、instructions テキストに
// スクリプトごとの1文ではなく、それらをまとめたポインタ行がちょうど1行だけ
// 現れることを検証する。これがないと、タグを付けるスクリプトが増えるたびに、
// "@output: file" が本来防ごうとしていた instructions フィールドの線形増加が
// 再び起きてしまう。
func TestRunSetupScripts_ConsolidatesMultipleFileTagsIntoOneLine(t *testing.T) {
	dir := t.TempDir()
	outputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.sh"), []byte("#!/bin/bash\n# @output: file\necho 'content A'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.sh"), []byte("#!/bin/bash\n# @output: file\necho 'content B'\n"), 0755); err != nil {
		t.Fatal(err)
	}

	out := runSetupScripts(dir, outputDir)

	lineCount := strings.Count(out, "\n")
	if lineCount != 1 {
		t.Errorf("Expected exactly one line in output, got %d lines: %q", lineCount, out)
	}
	if !strings.Contains(out, "a.txt") || !strings.Contains(out, "b.txt") {
		t.Errorf("Expected consolidated line to mention both a.txt and b.txt, got %q", out)
	}
}

// TestRunSetupScripts_OutputFileTagWithoutOutputDirFallsBackInline verifies
// that "@output: file" degrades to normal inline behavior when no
// outputDir is configured, rather than silently dropping the output.
//
// outputDir が設定されていない場合、"@output: file" は出力を黙って失わせる
// のではなく、通常の埋め込み挙動にフォールバックすることを検証する。
func TestRunSetupScripts_OutputFileTagWithoutOutputDirFallsBackInline(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/bash\n# @output: file\necho 'fallback content'\n"
	if err := os.WriteFile(filepath.Join(dir, "big.sh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	out := runSetupScripts(dir, "")

	if !strings.Contains(out, "fallback content") {
		t.Errorf("Expected inline fallback when outputDir is empty, got %q", out)
	}
}

// TestRunSetupScripts_DefaultOutputModeIsInline verifies that scripts without
// an "@output:" header keep the pre-existing inline behavior even when
// outputDir is configured, preserving backward compatibility.
//
// "@output:" ヘッダーを持たないスクリプトは、outputDir が設定されている場合
// でも従来通りの埋め込み挙動のままであり、後方互換性が保たれることを検証する。
func TestRunSetupScripts_DefaultOutputModeIsInline(t *testing.T) {
	dir := t.TempDir()
	outputDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plain.sh"), []byte("#!/bin/bash\necho 'plain inline output'\n"), 0755); err != nil {
		t.Fatal(err)
	}

	out := runSetupScripts(dir, outputDir)

	if !strings.Contains(out, "plain inline output") {
		t.Errorf("Expected untagged script to remain inline, got %q", out)
	}
}

// deadPID spawns and waits for a trivial subprocess, returning its PID after
// it has exited. Immediately after Wait() reaps a child, the OS will not
// reuse that PID for a new process within the lifetime of this test, so it's
// a reliable stand-in for "a PID that is definitely not running".
// deadPID は簡単なサブプロセスを起動して終了を待ち、終了後の PID を返す。
// Wait() が子プロセスを回収した直後は、このテストの実行中に OS がその PID を
// 別プロセスに再利用することはないため、「確実に動いていない PID」の代わりと
// して信頼できる。
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to run throwaway process: %v", err)
	}
	return cmd.Process.Pid
}

// TestSetupScriptWantsFileOutput_TrailingAnnotationOnSameLine verifies that a
// header like "# @output: file  (see runSetupScripts in ...)" is still
// detected: the matcher looks only at the first field after "@output:", not
// the entire rest of the line, so a trailing inline note doesn't defeat it.
func TestSetupScriptWantsFileOutput_TrailingAnnotationOnSameLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "annotated.sh")
	script := "#!/bin/bash\n# @output: file  (see runSetupScripts in internal/server/server.go)\necho hi\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	if !setupScriptWantsFileOutput(path) {
		t.Error("Expected @output: file to be detected even with a trailing annotation on the same line")
	}
}

func TestSetupScriptWantsFileOutput_PlainTag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.sh")
	if err := os.WriteFile(path, []byte("#!/bin/bash\n# @output: file\necho hi\n"), 0755); err != nil {
		t.Fatal(err)
	}

	if !setupScriptWantsFileOutput(path) {
		t.Error("Expected plain @output: file tag to be detected")
	}
}

func TestSetupScriptWantsFileOutput_NoTag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "untagged.sh")
	if err := os.WriteFile(path, []byte("#!/bin/bash\n# just a normal comment\necho hi\n"), 0755); err != nil {
		t.Fatal(err)
	}

	if setupScriptWantsFileOutput(path) {
		t.Error("Expected no @output: file tag to be detected for an untagged script")
	}
}

func TestSetupScriptWantsFileOutput_OtherValueIsNotFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inline.sh")
	if err := os.WriteFile(path, []byte("#!/bin/bash\n# @output: inline\necho hi\n"), 0755); err != nil {
		t.Fatal(err)
	}

	if setupScriptWantsFileOutput(path) {
		t.Error("Expected @output: inline (or any non-file value) to not be treated as file output")
	}
}

// TestSetupScriptWantsFileOutput_BlankLineWithinCommentBlock verifies that a
// blank line between the shebang and the "@output: file" tag (or between
// comment paragraphs before it) doesn't stop the scan early -- blank lines
// within the leading comment block are skipped, not treated as its end.
func TestSetupScriptWantsFileOutput_BlankLineWithinCommentBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spaced.sh")
	script := "#!/bin/bash\n\n# some description\n\n# @output: file\necho hi\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	if !setupScriptWantsFileOutput(path) {
		t.Error("Expected @output: file to be detected even with blank lines within the comment block")
	}
}

func TestIsProcessAlive_CurrentProcessIsAlive(t *testing.T) {
	if !isProcessAlive(os.Getpid()) {
		t.Error("Expected current process to be reported alive")
	}
}

func TestIsProcessAlive_ExitedProcessIsNotAlive(t *testing.T) {
	pid := deadPID(t)
	if isProcessAlive(pid) {
		t.Errorf("Expected exited process %d to be reported not alive", pid)
	}
}

// TestPruneStaleOutputDirs_RemovesOnlyDeadPIDDirs verifies that
// pruneStaleOutputDirs removes subdirectories named after a PID whose
// process has exited, while leaving alone: the caller's own PID directory,
// directories belonging to still-running processes (simulating other
// concurrent sandbox-mcp instances sharing the same workspace), and
// non-numeric entries that aren't PID directories at all.
//
// pruneStaleOutputDirs が、プロセスがすでに終了した PID を名前とする
// サブディレクトリだけを削除し、呼び出し元自身の PID ディレクトリ、
// (同じワークスペースを共有する他の sandbox-mcp インスタンスを模した)
// まだ動いているプロセスのディレクトリ、PID ディレクトリではない数値以外の
// 名前のエントリはそのまま残すことを検証する。
func TestPruneStaleOutputDirs_RemovesOnlyDeadPIDDirs(t *testing.T) {
	outputDir := t.TempDir()

	dead := deadPID(t)
	deadDir := filepath.Join(outputDir, strconv.Itoa(dead))
	if err := os.Mkdir(deadDir, 0755); err != nil {
		t.Fatal(err)
	}

	ownPID := os.Getpid()
	ownDir := filepath.Join(outputDir, strconv.Itoa(ownPID))
	if err := os.Mkdir(ownDir, 0755); err != nil {
		t.Fatal(err)
	}

	// A long-running sibling process standing in for another concurrently
	// running sandbox-mcp instance's own PID directory.
	sibling := exec.Command("sleep", "5")
	if err := sibling.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = sibling.Process.Kill()
		_ = sibling.Wait()
	}()
	aliveDir := filepath.Join(outputDir, strconv.Itoa(sibling.Process.Pid))
	if err := os.Mkdir(aliveDir, 0755); err != nil {
		t.Fatal(err)
	}

	nonNumericDir := filepath.Join(outputDir, "not-a-pid")
	if err := os.Mkdir(nonNumericDir, 0755); err != nil {
		t.Fatal(err)
	}

	pruneStaleOutputDirs(outputDir, ownPID)

	if _, err := os.Stat(deadDir); !os.IsNotExist(err) {
		t.Errorf("Expected dead PID dir %q to be removed", deadDir)
	}
	if _, err := os.Stat(ownDir); err != nil {
		t.Errorf("Expected own PID dir %q to survive, got: %v", ownDir, err)
	}
	if _, err := os.Stat(aliveDir); err != nil {
		t.Errorf("Expected alive sibling PID dir %q to survive, got: %v", aliveDir, err)
	}
	if _, err := os.Stat(nonNumericDir); err != nil {
		t.Errorf("Expected non-PID dir %q to be left untouched, got: %v", nonNumericDir, err)
	}
}

// TestInitializeInstructionsIncludesSetupOutput uses a setup dir at a
// non-default location (not workspaceDir/.sandbox/sandbox-mcp-setup) to
// verify buildInstructions() uses the explicitly configured s.setupDir
// rather than deriving it from workspaceDir.
func TestInitializeInstructionsIncludesSetupOutput(t *testing.T) {
	workspaceDir := t.TempDir()
	setupDir := filepath.Join(workspaceDir, "custom-setup-scripts")
	if err := os.MkdirAll(setupDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(setupDir, "info.sh"), []byte("#!/bin/bash\necho 'custom project info'\n"), 0755); err != nil {
		t.Fatal(err)
	}

	srv := New(t.TempDir(), t.TempDir(), setupDir, "", "test", workspaceDir)
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

// TestHandleInitialize_SecondCallReusesCachedInstructions verifies that a
// client sending a second "initialize" on the same process gets back the
// same cached instructions without re-running setup scripts: re-running
// would delete and repopulate setupOutputDir/<pid> (see runSetupScripts),
// making a spilled file path already handed to the client momentarily
// disappear.
func TestHandleInitialize_SecondCallReusesCachedInstructions(t *testing.T) {
	workspaceDir := t.TempDir()
	setupDir := filepath.Join(workspaceDir, "setup")
	if err := os.MkdirAll(setupDir, 0755); err != nil {
		t.Fatal(err)
	}
	outputDir := t.TempDir()
	counterFile := filepath.Join(workspaceDir, "run-count")
	script := "#!/bin/bash\n# @output: file\necho -n x >> " + counterFile + "\necho 'spilled content'\n"
	if err := os.WriteFile(filepath.Join(setupDir, "big.sh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	srv := New(t.TempDir(), t.TempDir(), setupDir, outputDir, "test", workspaceDir)

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "initialize",
		Params:  json.RawMessage(`{"clientInfo":{"name":"test"}}`),
	}
	resp1 := srv.HandleRequest(req)
	resp2 := srv.HandleRequest(req)
	if resp1 == nil || resp1.Error != nil || resp2 == nil || resp2.Error != nil {
		t.Fatalf("Unexpected error: resp1=%v resp2=%v", resp1.Error, resp2.Error)
	}

	instructions1 := resp1.Result.(map[string]any)["instructions"].(string)
	instructions2 := resp2.Result.(map[string]any)["instructions"].(string)
	if instructions1 != instructions2 {
		t.Errorf("Expected second initialize to reuse cached instructions, got different text:\n%q\nvs\n%q", instructions1, instructions2)
	}

	written, err := os.ReadFile(counterFile)
	if err != nil {
		t.Fatalf("Expected counter file to exist: %v", err)
	}
	if string(written) != "x" {
		t.Errorf("Expected setup script to run exactly once across two initialize calls, got %d run(s)", len(written))
	}

	spilledFile := filepath.Join(outputDir, pidsSubdir, strconv.Itoa(os.Getpid()), "big.txt")
	if _, err := os.Stat(spilledFile); err != nil {
		t.Errorf("Expected spilled file %q to still exist after second initialize: %v", spilledFile, err)
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

	srv := New(t.TempDir(), t.TempDir(), "", "", "test", workspaceDir)
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
