// sandbox-mcp is a lightweight MCP server (stdio) for AI Sandbox scripts and tools.
// AI Sandbox のスクリプトやツールを検出・実行する軽量 MCP サーバー（stdio）。
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/YujiSuzuki/sandbox-mcp/internal/config"
	"github.com/YujiSuzuki/sandbox-mcp/internal/jsonrpc"
	"github.com/YujiSuzuki/sandbox-mcp/internal/server"
)

// version is set at build time via ldflags.
// ビルド時に ldflags で設定されます。
//
// go build -ldflags "-X main.version=1.0.0"
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("sandbox-mcp " + version)
		return
	}

	// Parse CLI flags
	// CLI フラグの解析
	flagScriptsDir := flag.String("scripts-dir", "", "Path to scripts directory (default: .sandbox/scripts)")
	flagToolsDir := flag.String("tools-dir", "", "Path to tools directory (default: .sandbox/tools)")
	flagConfig := flag.String("config", "", "Path to config file (default: auto-detect)")
	flagWorkspace := flag.String("workspace", "", "Workspace root directory (resolves scripts/tools dirs relative to it)")
	flag.Parse()

	// Log to stderr only (stdout is reserved for JSON-RPC)
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	})))

	// Resolve workspace: use --workspace if given, otherwise fall back to CWD
	// ワークスペースの解決: --workspace が指定されていればそれを使用、なければ CWD にフォールバック
	var workspaceDir string
	if *flagWorkspace != "" {
		absWS, err := filepath.Abs(*flagWorkspace)
		if err != nil {
			slog.Error("invalid workspace path", "error", err)
			os.Exit(1)
		}
		workspaceDir = absWS
	} else {
		var err error
		workspaceDir, err = os.Getwd()
		if err != nil {
			slog.Error("failed to get working directory", "error", err)
			os.Exit(1)
		}
	}

	// Resolve configuration: CLI flags > config file > env vars > defaults
	// 設定の解決: CLI フラグ > 設定ファイル > 環境変数 > デフォルト
	configFile := *flagConfig
	if configFile == "" {
		configFile = config.FindConfigFile(workspaceDir)
	}
	cfg, cfgErr := config.Resolve(configFile, *flagScriptsDir, *flagToolsDir, workspaceDir)
	if cfgErr != nil {
		slog.Warn("config file error, using defaults/env", "file", configFile, "error", cfgErr)
	}

	srv := server.New(
		cfg.ScriptsDir,
		cfg.ToolsDir,
		version,
	)

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req jsonrpc.Request
		if err := json.Unmarshal(line, &req); err != nil {
			resp := jsonrpc.NewErrorResponse(nil, jsonrpc.CodeParseError, "Parse error")
			writeResponse(resp)
			continue
		}

		resp := srv.HandleRequest(&req)
		if resp != nil {
			writeResponse(resp)
		}
	}

	if err := scanner.Err(); err != nil {
		slog.Error("stdin scanner error", "error", err)
		os.Exit(1)
	}
}

func writeResponse(resp *jsonrpc.Response) {
	data, err := json.Marshal(resp)
	if err != nil {
		slog.Error("failed to marshal response", "error", err)
		return
	}
	os.Stdout.Write(data)
	os.Stdout.Write([]byte("\n"))
}
