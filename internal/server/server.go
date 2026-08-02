// Package server implements the MCP server for sandbox tools.
package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/YujiSuzuki/sandbox-mcp/internal/jsonrpc"
	"github.com/YujiSuzuki/sandbox-mcp/internal/scriptparser"
	"github.com/YujiSuzuki/sandbox-mcp/internal/toolparser"
)

// Server handles MCP requests for sandbox scripts and tools.
type Server struct {
	scriptsDir     string
	toolsDir       string
	setupDir       string
	setupOutputDir string
	workspaceDir   string
	version        string
	initialized    bool

	// cachedInstructions holds the result of the first buildInstructions()
	// call. A client that sends "initialize" more than once on the same
	// process (e.g. a reconnect/retry) must keep getting the same
	// instructions text rather than triggering setup scripts to re-run: with
	// setupOutputDir configured, re-running deletes and repopulates spilled
	// files (see runSetupScripts), which would make a file path already
	// handed to the client momentarily disappear.
	// cachedInstructions は最初の buildInstructions() 呼び出しの結果を保持する。
	// 同一プロセスに対してクライアントが "initialize" を複数回送ってきた場合
	// (再接続・リトライなど)、セットアップスクリプトを再実行させず、常に同じ
	// instructions を返す必要がある。setupOutputDir が設定されていると、再実行は
	// 書き出し済みのファイル(runSetupScripts 参照)を削除・再生成してしまい、
	// すでにクライアントへ渡したファイルパスが一時的に消えてしまうため。
	cachedInstructions *string
}

// New creates a new MCP server.
func New(scriptsDir, toolsDir, setupDir, setupOutputDir, version, workspaceDir string) *Server {
	return &Server{
		scriptsDir:     scriptsDir,
		toolsDir:       toolsDir,
		setupDir:       setupDir,
		setupOutputDir: setupOutputDir,
		workspaceDir:   workspaceDir,
		version:        version,
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
	if s.cachedInstructions == nil {
		clientName := parseClientName(req.Params)
		instructions := s.buildInstructions(clientName)
		s.cachedInstructions = &instructions
	}
	result := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "sandbox-mcp",
			"version": s.version,
		},
		"instructions": *s.cachedInstructions,
	}
	return jsonrpc.NewResponse(req.ID, result)
}

// parseClientName best-effort extracts clientInfo.name from an "initialize"
// request's params. Any failure (missing clientInfo, malformed JSON, empty
// params) returns "" rather than an error -- clientInfo is optional and
// varies by MCP client, so a client that omits or malforms it must still be
// able to initialize successfully, just without qualifying as spill-safe
// below.
func parseClientName(params json.RawMessage) string {
	var p struct {
		ClientInfo struct {
			Name string `json:"name"`
		} `json:"clientInfo"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return ""
	}
	return p.ClientInfo.Name
}

// spillSafeClients holds MCP clientInfo.name values known to (a) enforce a
// byte budget on "instructions" that silently truncates when exceeded, and
// (b) have a matching mechanism that reliably resurfaces spilled file
// contents every turn (currently: ai-sandbox's setup-output-reminder.sh, a
// Claude Code-only .claude/settings.json hook). Spilling for any other or
// unknown client would leave content behind a one-line pointer with nothing
// to resurface it -- worse than the truncation this mechanism exists to
// avoid, so the default for anything not listed here is to inline instead.
// The exact string "claude-code" is confirmed by a sibling project
// (HostMCP) logging client_name=claude-code for real Claude Code
// connections; kept as an exact-match allowlist (not a prefix match) so
// adding a new client is a deliberate one-line change once it has its own
// resurfacing mechanism, not an accidental match.
var spillSafeClients = map[string]struct{}{
	"claude-code": {},
}

func isSpillSafeClient(name string) bool {
	_, ok := spillSafeClients[name]
	return ok
}

func (s *Server) buildInstructions(clientName string) string {
	var capabilities strings.Builder
	capabilities.WriteString("sandbox-mcp provides scripts and tools for AI-assisted development.\n\n")

	tools, err := toolparser.ListTools(s.toolsDir)
	if err == nil && len(tools) > 0 {
		capabilities.WriteString("Available tools (use run_tool to execute):\n")
		for _, t := range tools {
			capabilities.WriteString(fmt.Sprintf("- %s: %s\n", t.Name, t.Description))
		}
		capabilities.WriteString("\nUse list_tools for full details.\n")
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
			capabilities.WriteString("\nAvailable scripts (use run_script to execute):\n")
			for _, sc := range advertised {
				capabilities.WriteString(fmt.Sprintf("- %s: %s\n", sc.Name, sc.Description))
			}
			capabilities.WriteString("\nUse list_scripts for full details.\n")
		}
	}

	repos := scanGitRepos(s.workspaceDir, 3)
	if len(repos) > 0 {
		capabilities.WriteString("\nNested git repositories (independent repos — run git commands from within each directory, not the workspace root):\n")
		for _, r := range repos {
			capabilities.WriteString(fmt.Sprintf("- %s\n", r))
		}
	}

	// Resolved at most once per buildInstructions() call (itself invoked at
	// most once per process, see cachedInstructions) and shared by every
	// content source spilled below -- resolving it more than once would wipe
	// out whichever source already wrote into the directory. Stays "" (i.e.
	// spilling disabled, everything below falls back to inlining) unless
	// clientName is recognized as spill-safe -- see spillSafeClients.
	var myOutputDir string
	if isSpillSafeClient(clientName) {
		myOutputDir = resolveMyOutputDir(s.setupOutputDir)
	} else if s.setupOutputDir != "" {
		slog.Warn("setup-output-dir is configured but client is not recognized as spill-safe; falling back to inline instructions", "client", clientName)
	}

	var sb strings.Builder
	var spilled []string

	if fileName, ok := spillFile(myOutputDir, "sandbox-mcp-capabilities.txt", []byte(capabilities.String())); ok {
		spilled = append(spilled, fileName)
	} else {
		sb.WriteString(capabilities.String())
	}

	if s.setupDir != "" {
		inline, setupSpilled := runSetupScripts(s.setupDir, myOutputDir)
		spilled = append(spilled, setupSpilled...)
		if inline != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(inline)
		}
	}

	if len(spilled) > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("sandbox-mcp produced file(s) under %s -- these were moved out of your instructions only due to space, not because they're optional: read them now: %s\n", myOutputDir, strings.Join(spilled, ", ")))
	}

	return sb.String()
}

// resolveMyOutputDir resolves and prepares this process's own spill
// directory under outputDir/pidsSubdir: pruning stale dead-PID directories,
// then wiping this process's own subdirectory clean (a leftover here can
// only be stale garbage, since a live process can't share our own PID).
// Returns "" if outputDir is empty, signaling to callers that spilling is
// disabled and they should fall back to inlining.
//
// Must be called exactly ONCE per buildInstructions() invocation, and the
// result shared by every content source that spills into it (currently: the
// capabilities dump and runSetupScripts' per-script "@output: file" spill).
// Calling it more than once per invocation would wipe out whichever source
// already wrote into the directory.
func resolveMyOutputDir(outputDir string) string {
	if outputDir == "" {
		return ""
	}
	pid := os.Getpid()
	pidsDir := filepath.Join(outputDir, pidsSubdir)
	pruneStaleOutputDirs(pidsDir, pid)
	myOutputDir := filepath.Join(pidsDir, strconv.Itoa(pid))
	os.RemoveAll(myOutputDir)
	return myOutputDir
}

// spillFile writes content to a file named name under myOutputDir, creating
// myOutputDir if needed. Returns (name, true) on success, or ("", false) on
// any failure (including myOutputDir being "", i.e. spilling disabled) --
// callers must fall back to inlining content directly on failure, so a
// spill attempt never silently drops output.
func spillFile(myOutputDir, name string, content []byte) (string, bool) {
	if myOutputDir == "" {
		return "", false
	}
	if err := os.MkdirAll(myOutputDir, 0755); err != nil {
		return "", false
	}
	if err := os.WriteFile(filepath.Join(myOutputDir, name), content, 0644); err != nil {
		return "", false
	}
	return name, true
}

// runSetupScripts runs all .sh scripts in setupDir alphabetically. It
// returns the combined stdout of scripts that stay inline, and separately
// the list of filenames it spilled under myOutputDir for scripts tagged
// "@output: file". It builds no pointer line itself -- the caller
// (buildInstructions) merges this function's spilled names together with
// any other content source's spilled names into exactly one combined
// pointer line. Failed or timed-out scripts are silently skipped.
//
// A script whose header declares "@output: file" (see
// setupScriptWantsFileOutput) has its stdout spilled via spillFile into
// myOutputDir instead of inlined directly; its filename is reported back to
// the caller via the spilled return value rather than a pointer line built
// here. This is not a position-based mitigation -- setup script output is
// already the last section of buildInstructions, so a script running early
// within this section (e.g. alphabetically first) is no safer from
// truncation than one running last. The tag instead gives output that must
// reliably reach the AI an unconditional, position-independent guarantee:
// because the client silently truncates the instructions field once its
// byte budget is exceeded (see buildInstructions), relying on ordering to
// let truncation quietly drop that content would lose it with no trace that
// anything was cut. Spilling to a file avoids that outcome entirely, and
// also shrinks the field's overall footprint, lowering truncation risk for
// everything else. If myOutputDir is empty, "@output: file" has no effect
// and the script's output is inlined as usual, so the tag never silently
// drops output.
//
// myOutputDir must already be resolved via resolveMyOutputDir (or be "" to
// disable spilling) -- this function does no pruning or wiping of its own,
// so it is safe to call after another content source has already written
// into myOutputDir in the same buildInstructions() invocation.
//
// ヘッダーに "@output: file" を宣言したスクリプト(setupScriptWantsFileOutput
// 参照)は、標準出力を直接埋め込む代わりに spillFile 経由で myOutputDir へ
// 書き出し、そのファイル名は(ここでポインタ行を組み立てるのではなく)戻り値
// spilled 経由で呼び出し元へ伝える。これは実行順に頼った対策ではない --
// セットアップスクリプトの出力はもともと buildInstructions の最後のセクション
// なので、このセクション内で早く実行されるスクリプト(アルファベット順で先頭
// など)も、最後に実行されるものと切り詰めリスクは変わらない。このタグは、AI に
// 確実に届ける必要のある出力に対して、実行順に依存しない無条件の保証を与える
// ためのものである。クライアントは instructions のバイト予算を超えると無音の
// まま切り詰めるため(buildInstructions 参照)、実行順に任せてその内容が黙って
// 削られるに任せると、何が削られたのか痕跡すら残らない。ファイルへの書き出しは
// この事態を完全に回避し、フィールド全体のサイズも小さくなるため、他の内容の切り詰め
// リスクも下がる。myOutputDir が空文字列の場合は "@output: file" は無効となり、
// スクリプトの出力は従来通りそのまま埋め込まれる -- つまりこのタグが出力を
// 黙って失わせることはない。
//
// myOutputDir は resolveMyOutputDir で解決済みのものを渡すこと("" を渡せば
// 退避を無効化できる) -- この関数自身はprune/wipeを一切行わないため、同じ
// buildInstructions() 呼び出しの中で別の退避元が既に書き込んだ後でも
// 安全に呼び出せる。
func runSetupScripts(setupDir, myOutputDir string) (inline string, spilled []string) {
	if setupDir == "" {
		return "", nil
	}
	entries, err := os.ReadDir(setupDir)
	if err != nil {
		return "", nil
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sh") {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return "", nil
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

		if setupScriptWantsFileOutput(path) {
			fileName := strings.TrimSuffix(name, ".sh") + ".txt"
			if spilledName, ok := spillFile(myOutputDir, fileName, out); ok {
				spilled = append(spilled, spilledName)
				continue
			}
			// Fall through to inline on any write failure (or myOutputDir
			// == ""), so the tag never silently drops the script's output.
			// 書き込みに失敗した場合(または myOutputDir == "" の場合)は
			// 埋め込み方式にフォールバックする。これにより、このタグが
			// スクリプトの出力を黙って失わせることはない。
		}

		sb.Write(out)
		if out[len(out)-1] != '\n' {
			sb.WriteByte('\n')
		}
	}

	return sb.String(), spilled
}

// pidsSubdir is the fixed path component sandbox-mcp nests its own PID
// directories under, one level below the operator-configured outputDir (see
// runSetupScripts). outputDir itself may not be exclusive to sandbox-mcp, so
// pruneStaleOutputDirs must never scan it directly -- it only ever scans
// outputDir/pidsSubdir, a path sandbox-mcp fully owns.
// pidsSubdir は、運用者が設定した outputDir(runSetupScripts 参照)の1階層下に
// sandbox-mcp が自身の PID ディレクトリをまとめる固定パス要素。outputDir 自体は
// sandbox-mcp 専用とは限らないため、pruneStaleOutputDirs は outputDir を直接
// スキャンしてはならず、常に sandbox-mcp が完全に所有する outputDir/pidsSubdir
// だけをスキャン対象とする。
const pidsSubdir = "sandbox-mcp-pids"

// pruneStaleOutputDirs removes subdirectories of pidsDir named after a PID
// whose process is no longer running, reclaiming setup-output directories
// orphaned by past sandbox-mcp instances (e.g. a session that ended without
// a clean shutdown) so disk usage doesn't grow unbounded across sessions.
// pidsDir must be outputDir/pidsSubdir (see runSetupScripts), never the raw
// operator-configured outputDir -- this function deletes any entry whose
// name parses as an integer and whose PID isn't alive, with no ownership
// check beyond that, so scanning an arbitrary directory could destroy
// unrelated content that happens to use integer names.
// ownPID's own directory is skipped here -- the caller manages it directly.
// Entries whose name isn't a plain PID number are left untouched, since
// they aren't ours to manage.
// pruneStaleOutputDirs は、プロセスがすでに終了している PID の名前を持つ
// pidsDir 直下のサブディレクトリを削除し、過去の sandbox-mcp インスタンスが
// (正常終了しなかったセッションなどで)残した setup-output ディレクトリを
// 回収する。これにより、セッションをまたいでディスク使用量が際限なく増え
// 続けることを防ぐ。pidsDir には必ず outputDir/pidsSubdir(runSetupScripts
// 参照)を渡すこと -- 運用者が設定した生の outputDir を渡してはならない。
// この関数は、名前が整数としてパースでき、かつ該当 PID が生存していない
// エントリを、それ以上の所有権チェックなしに削除するため、任意のディレクトリ
// をスキャンすると、たまたま整数名を使っている無関係なコンテンツまで
// 壊しかねない。ownPID 自身のディレクトリはここではスキップされ、呼び出し元が
// 直接管理する。単純な PID 番号でない名前のエントリは、sandbox-mcp が管理
// すべきものではないため、そのまま残される。
func pruneStaleOutputDirs(pidsDir string, ownPID int) {
	entries, err := os.ReadDir(pidsDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		if pid == ownPID || isProcessAlive(pid) {
			continue
		}
		os.RemoveAll(filepath.Join(pidsDir, entry.Name()))
	}
}

// isProcessAlive reports whether a process with the given PID currently
// exists, using the standard Unix "kill -0" idiom: sending signal 0
// performs error checking (permission, existence) without actually
// delivering a signal.
// isProcessAlive は、指定した PID のプロセスが現在存在するかどうかを、
// Unix の "kill -0" の慣習的な手法で調べる: シグナル 0 の送信は、実際には
// 何もシグナルを配送せず、権限・存在確認のエラーチェックだけを行う。
func isProcessAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

// setupScriptWantsFileOutput scans a setup script's leading "#" comment
// block for an "@output: file" header line (default: inline, matching the
// pre-existing behavior for scripts without the tag). Mirrors the "@key:
// value" convention used by scriptparser for .sandbox/scripts/.
// setupScriptWantsFileOutput は、セットアップスクリプト先頭の "#" コメント
// ブロックから "@output: file" ヘッダー行を探す(タグがない場合はデフォルトで
// 埋め込みとなり、これは既存スクリプトの挙動と同じ)。.sandbox/scripts/ 向けに
// scriptparser が使う "@key: value" の慣習にならったもの。
func setupScriptWantsFileOutput(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if lineNum == 1 && strings.HasPrefix(line, "#!") {
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, "#") {
			break
		}

		content := strings.TrimSpace(strings.TrimPrefix(line, "#"))
		if strings.HasPrefix(content, "@output:") {
			fields := strings.Fields(strings.TrimPrefix(content, "@output:"))
			return len(fields) > 0 && fields[0] == "file"
		}
	}
	return false
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
