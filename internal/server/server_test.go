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
	return New(t.TempDir(), t.TempDir(), "test")
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

	return New(scriptsDir, toolsDir, "test")
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

func TestHostOnlyScriptRejection(t *testing.T) {
	srv := newTestServer(t)

	srv.HandleRequest(&jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      float64(1),
		Method:  "initialize",
		Params:  json.RawMessage(`{"clientInfo":{"name":"test"}}`),
	})

	callReq := &jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      float64(2),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"run_script","arguments":{"name":"init-host-env.sh"}}`),
	}
	resp := srv.HandleRequest(callReq)

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("Expected map result")
	}
	isError, _ := result["isError"].(bool)
	if !isError {
		t.Error("Expected isError=true for host-only script")
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
	srv := New(scriptsDir, toolsDir, "test")
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
