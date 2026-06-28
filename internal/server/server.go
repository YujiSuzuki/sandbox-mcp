// Package server implements the MCP server for sandbox tools.
package server

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/YujiSuzuki/sandbox-mcp/internal/jsonrpc"
	"github.com/YujiSuzuki/sandbox-mcp/internal/toolparser"
)

// Server handles MCP requests for sandbox scripts and tools.
type Server struct {
	scriptsDir  string
	toolsDir    string
	version     string
	initialized bool
}

// New creates a new MCP server.
func New(scriptsDir, toolsDir, version string) *Server {
	return &Server{
		scriptsDir: scriptsDir,
		toolsDir:   toolsDir,
		version:    version,
	}
}

// HandleRequest dispatches a JSON-RPC request to the appropriate handler.
func (s *Server) HandleRequest(req *jsonrpc.Request) *jsonrpc.Response {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "notifications/initialized":
		return nil // Notification, no response
	case "tools/list":
		if !s.initialized {
			return jsonrpc.NewErrorResponse(req.ID, jsonrpc.CodeInternalError, "Server not initialized")
		}
		return s.handleToolsList(req)
	case "tools/call":
		if !s.initialized {
			return jsonrpc.NewErrorResponse(req.ID, jsonrpc.CodeInternalError, "Server not initialized")
		}
		return s.handleToolsCall(req)
	default:
		return jsonrpc.NewErrorResponse(req.ID, jsonrpc.CodeMethodNotFound, fmt.Sprintf("Unknown method: %s", req.Method))
	}
}

func (s *Server) handleInitialize(req *jsonrpc.Request) *jsonrpc.Response {
	s.initialized = true
	result := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "sandbox-mcp",
			"version": s.version,
		},
		"instructions": s.buildInstructions(),
	}
	return jsonrpc.NewResponse(req.ID, result)
}

func (s *Server) buildInstructions() string {
	var sb strings.Builder
	sb.WriteString("sandbox-mcp provides scripts and tools for AI-assisted development.\n\n")

	tools, err := toolparser.ListTools(s.toolsDir)
	if err == nil && len(tools) > 0 {
		sb.WriteString("Available tools (use run_tool to execute):\n")
		for _, t := range tools {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", t.Name, t.Description))
		}
		sb.WriteString("\nUse list_tools for full details.\n")
	}

	return sb.String()
}

// toolsCallParams represents the params for tools/call.
type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// textContent creates MCP text content response.
func textContent(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": text,
			},
		},
	}
}

// errorContent creates MCP error content response.
func errorContent(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": text,
			},
		},
		"isError": true,
	}
}
