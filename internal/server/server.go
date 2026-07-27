// Package server implements the MCP server for sandbox tools.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/YujiSuzuki/sandbox-mcp/internal/jsonrpc"
	"github.com/YujiSuzuki/sandbox-mcp/internal/scriptparser"
	"github.com/YujiSuzuki/sandbox-mcp/internal/toolparser"
)

// Server handles MCP requests for sandbox scripts and tools.
type Server struct {
	scriptsDir   string
	toolsDir     string
	setupDir     string
	workspaceDir string
	version      string
	initialized  bool
}

// New creates a new MCP server.
func New(scriptsDir, toolsDir, setupDir, version, workspaceDir string) *Server {
	return &Server{
		scriptsDir:   scriptsDir,
		toolsDir:     toolsDir,
		setupDir:     setupDir,
		workspaceDir: workspaceDir,
		version:      version,
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

	scripts, err := scriptparser.ListScripts(s.scriptsDir)
	if err == nil {
		var advertised []scriptparser.ScriptInfo
		for _, sc := range scripts {
			if sc.Advertise {
				advertised = append(advertised, sc)
			}
		}
		if len(advertised) > 0 {
			sb.WriteString("\nAvailable scripts (use run_script to execute):\n")
			for _, sc := range advertised {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", sc.Name, sc.Description))
			}
			sb.WriteString("\nUse list_scripts for full details.\n")
		}
	}

	repos := scanGitRepos(s.workspaceDir, 3)
	if len(repos) > 0 {
		sb.WriteString("\nNested git repositories (independent repos — run git commands from within each directory, not the workspace root):\n")
		for _, r := range repos {
			sb.WriteString(fmt.Sprintf("- %s\n", r))
		}
	}

	if s.setupDir != "" {
		if out := runSetupScripts(s.setupDir); out != "" {
			sb.WriteString("\n")
			sb.WriteString(out)
		}
	}

	return sb.String()
}

// runSetupScripts runs all .sh scripts in setupDir alphabetically and returns their combined stdout.
// Failed or timed-out scripts are silently skipped.
func runSetupScripts(setupDir string) string {
	if setupDir == "" {
		return ""
	}
	entries, err := os.ReadDir(setupDir)
	if err != nil {
		return ""
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sh") {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)

	var sb strings.Builder
	for _, name := range names {
		path := filepath.Join(setupDir, name)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, err := exec.CommandContext(ctx, "bash", path).Output()
		cancel()
		if err != nil || len(out) == 0 {
			continue
		}
		sb.Write(out)
		if out[len(out)-1] != '\n' {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

var skipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
}

// scanGitRepos finds independent git repositories nested inside workspaceDir,
// up to maxDepth levels deep. The workspace root itself is excluded.
func scanGitRepos(workspaceDir string, maxDepth int) []string {
	if workspaceDir == "" {
		return nil
	}
	var repos []string
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > maxDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		hasGit := false
		for _, entry := range entries {
			if entry.IsDir() && entry.Name() == ".git" {
				hasGit = true
				break
			}
		}
		if hasGit && dir != workspaceDir {
			rel, err := filepath.Rel(workspaceDir, dir)
			if err == nil {
				repos = append(repos, rel)
			}
			return
		}
		for _, entry := range entries {
			name := entry.Name()
			if !entry.IsDir() || strings.HasPrefix(name, ".") || skipDirs[name] {
				continue
			}
			walk(filepath.Join(dir, entry.Name()), depth+1)
		}
	}
	walk(workspaceDir, 0)
	sort.Strings(repos)
	return repos
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
