// Package server implements the MCP server for sandbox tools.
package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
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
		instructions := s.buildInstructions()
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
		if out := runSetupScripts(s.setupDir, s.setupOutputDir); out != "" {
			sb.WriteString("\n")
			sb.WriteString(out)
		}
	}

	return sb.String()
}

// runSetupScripts runs all .sh scripts in setupDir alphabetically and returns
// their combined stdout, to be inlined into the MCP instructions field.
// Failed or timed-out scripts are silently skipped.
//
// A script whose header declares "@output: file" (see
// setupScriptWantsFileOutput) has its stdout written under outputDir instead
// of inlined directly, with only a short pointer line surfacing here. This is
// not a position-based mitigation -- setup script output is already the last
// section of buildInstructions, so a script running early within this
// section (e.g. alphabetically first) is no safer from truncation than one
// running last. The tag instead gives output that must reliably reach the
// AI an unconditional, position-independent guarantee: because the client
// silently truncates the instructions field once its byte budget is
// exceeded (see buildInstructions), relying on ordering to let truncation
// quietly drop that content would lose it with no trace that anything was
// cut. Spilling to a file avoids that outcome entirely, and
// also shrinks the field's overall footprint, lowering truncation risk for
// everything else. If outputDir is empty, "@output: file" has no effect and
// the script's output is inlined as usual, so the tag never silently drops
// output.
//
// Spilled files go under outputDir/<pidsSubdir>/<pid>/ (this process's own
// PID), not directly under outputDir, for two reasons. First, sandbox-mcp is
// commonly run as several concurrent instances against the same workspace
// (e.g. multiple Claude Code windows on the same repo) -- a shared, unscoped
// file would let one instance overwrite another's output mid-read. Second,
// outputDir itself is arbitrary operator config (setup_output_dir /
// SANDBOX_SETUP_OUTPUT_DIR, see internal/config) that may not be exclusively
// sandbox-mcp's own scratch space -- pruneStaleOutputDirs recursively deletes
// any integer-named subdirectory it finds whose PID isn't alive, so scanning
// outputDir directly could destroy unrelated directories an operator happens
// to have there (e.g. version folders "1", "2", "3"). Confining that scan to
// the fixed pidsSubdir component means pruning only ever touches directories
// sandbox-mcp itself created. Before writing, stale directories left behind
// by past instances that are no longer running are pruned (see
// pruneStaleOutputDirs), so disk usage doesn't grow unbounded.
//
// If any files are spilled, exactly one pointer line lists all of them,
// rather than one line per file -- otherwise tagging more scripts over time
// would recreate the same linear growth in the instructions field that
// "@output: file" exists to avoid.
//
// ヘッダーに "@output: file" を宣言したスクリプト(setupScriptWantsFileOutput
// 参照)は、標準出力を直接埋め込む代わりに outputDir 配下へ書き出し、ここには
// 短いポインタ行だけを残す。これは実行順に頼った対策ではない -- セットアップ
// スクリプトの出力はもともと buildInstructions の最後のセクションなので、
// このセクション内で早く実行されるスクリプト(アルファベット順で先頭など)も、
// 最後に実行されるものと切り詰めリスクは変わらない。このタグは、AI に確実に
// 届ける必要のある出力に対して、実行順に依存しない無条件の保証を与えるための
// ものである。クライアントは instructions のバイト予算を超えると無音のまま
// 切り詰めるため(buildInstructions 参照)、実行順に任せてその内容が黙って
// 削られるに任せると、何が削られたのか痕跡すら残らない。ファイルへの書き出しは
// この事態を完全に回避し、フィールド全体のサイズも小さくなるため、他の内容の切り詰め
// リスクも下がる。outputDir が空文字列の場合は "@output: file" は無効となり、
// スクリプトの出力は従来通りそのまま埋め込まれる -- つまりこのタグが出力を
// 黙って失わせることはない。
//
// 書き出したファイルは outputDir 直下ではなく outputDir/<pidsSubdir>/<pid>/
// (このプロセス自身の PID) の下に置く。理由は2つある。第一に、sandbox-mcp は
// 同じワークスペースに対して複数インスタンスが同時に動くことが多く(同じ
// リポジトリを開いた複数の Claude Code ウィンドウなど)、共有された無区別の
// ファイルだと、あるインスタンスが読み取り中の別インスタンスの出力を上書き
// してしまう。第二に、outputDir 自体は運用者側の任意設定(setup_output_dir /
// SANDBOX_SETUP_OUTPUT_DIR、internal/config 参照)であり、sandbox-mcp 専用の
// 作業領域とは限らない -- pruneStaleOutputDirs は、生存していない PID の
// 整数名サブディレクトリを見つけ次第再帰的に削除するため、outputDir を直接
// スキャンすると、運用者がそこに置いている無関係なディレクトリ(バージョン
// フォルダ "1", "2", "3" など)まで壊しかねない。固定の pidsSubdir 配下だけを
// スキャン対象にすることで、削除対象を sandbox-mcp 自身が作成したディレクトリ
// だけに限定できる。書き込みの前には、動いていない過去のインスタンスが残した
// 古いディレクトリを削除する(pruneStaleOutputDirs 参照)ため、ディスク使用量が
// 際限なく増え続けることもない。
//
// ファイルを書き出した場合、ファイルごとに1行ではなく、それらをまとめた
// ポインタ行を1行だけ出力する -- そうしないと、タグを付けるスクリプトが
// 増えるたびに、"@output: file" がそもそも防ごうとしていた instructions
// フィールドの線形増加が再び起きてしまう。
func runSetupScripts(setupDir, outputDir string) string {
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

	var myOutputDir string
	if outputDir != "" {
		pid := os.Getpid()
		pidsDir := filepath.Join(outputDir, pidsSubdir)
		pruneStaleOutputDirs(pidsDir, pid)
		myOutputDir = filepath.Join(pidsDir, strconv.Itoa(pid))
		// Start this run's directory clean; a leftover here could only be
		// stale garbage, since a live process can't share our own PID.
		// 今回の実行用ディレクトリは空の状態から始める -- 生存中の他プロセスが
		// 自分と同じ PID を持つことはないため、ここに何か残っていたとしても
		// それは古いゴミでしかない。
		os.RemoveAll(myOutputDir)
	}

	var sb strings.Builder
	var spilled []string
	for _, name := range names {
		path := filepath.Join(setupDir, name)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, err := exec.CommandContext(ctx, "bash", path).Output()
		cancel()
		if err != nil || len(out) == 0 {
			continue
		}

		if myOutputDir != "" && setupScriptWantsFileOutput(path) {
			fileName := strings.TrimSuffix(name, ".sh") + ".txt"
			outPath := filepath.Join(myOutputDir, fileName)
			if err := os.MkdirAll(myOutputDir, 0755); err == nil {
				if err := os.WriteFile(outPath, out, 0644); err == nil {
					spilled = append(spilled, fileName)
					continue
				}
			}
			// Fall through to inline on any write failure, so the tag never
			// silently drops the script's output.
			// 書き込みに失敗した場合は埋め込み方式にフォールバックする。これに
			// より、このタグがスクリプトの出力を黙って失わせることはない。
		}

		sb.Write(out)
		if out[len(out)-1] != '\n' {
			sb.WriteByte('\n')
		}
	}

	if len(spilled) > 0 {
		sb.WriteString(fmt.Sprintf("Setup produced file(s) under %s -- these were moved out of your instructions only due to space, not because they're optional: read them now: %s\n", myOutputDir, strings.Join(spilled, ", ")))
	}

	return sb.String()
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
