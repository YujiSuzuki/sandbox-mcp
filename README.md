# SandboxMCP

[日本語版はこちら](README.ja.md)

A **lightweight MCP (Model Context Protocol) server that runs inside your container**. It communicates with AI agents like Claude Code via stdio, auto-discovers scripts and tools placed in `.sandbox/scripts/` or `.sandbox/tools/` for AI to run autonomously, and automatically pushes workspace context (git status, environment info, etc.) to the AI at startup.

- Drop scripts you want AI to run — AI discovers them automatically
- No need to tell AI the script path or usage every time
- AI reads the purpose and usage directly from header comments
- Also pushes custom context (git status, env info, etc.) to the AI automatically at startup — see [Startup Context](#startup-context)

## Overview

Relationship with **AI Sandbox** and **HostMCP**:

| | SandboxMCP | [HostMCP](https://github.com/YujiSuzuki/hostmcp) | [AI Sandbox](https://github.com/YujiSuzuki/ai-sandbox) |
|---|---|---|---|
| Location | Inside container | Host OS | Template / environment |
| Transport | stdio | SSE (HTTP) | — |
| Purpose | Script/tool discovery | Docker containers, host tools & host OS commands | Container setup & secret management template |
| Startup | Auto (Claude Code) | Manual (`hostmcp serve`) | — |

**Typical setup:**

```
AI Sandbox (container)
  └─ SandboxMCP (stdio)   ← discovers .sandbox/scripts/ and .sandbox/tools/
  └─ hostmcp client (via HTTP)   ← communicates with HostMCP server on the host OS
        ↓
Host OS: HostMCP server → API container, DB container, host tools, …
```

> **Using AI Sandbox?** SandboxMCP is automatically installed and registered when the container starts — no manual steps needed.
> 
> **Using your own existing container?** Follow the installation steps below to add SandboxMCP to it.

## Installation

```bash
go install github.com/YujiSuzuki/sandbox-mcp@latest
```

Or download a prebuilt binary (no Go required — SandboxMCP is designed to run inside a Linux container, so only Linux binaries are published):

```bash
curl -L https://github.com/YujiSuzuki/sandbox-mcp/releases/latest/download/sandbox-mcp_linux_amd64 -o sandbox-mcp
chmod +x sandbox-mcp
sudo mv sandbox-mcp /usr/local/bin/
```

> `sandbox-mcp_linux_arm64` is also available for ARM hosts. This is the same binary that AI Sandbox's `startup.sh` downloads automatically when Go isn't present in the container.

Or build from source:

```bash
git clone https://github.com/YujiSuzuki/sandbox-mcp.git
cd sandbox-mcp
make install
```

## Usage

### Register with Claude Code or Gemini CLI

The easiest way is to install and register in one step:

```bash
make install register
```

Or manually:

```bash
claude mcp add sandbox-mcp sandbox-mcp
# or for Gemini CLI:
gemini mcp add sandbox-mcp sandbox-mcp
```

With custom paths:

```bash
claude mcp add sandbox-mcp sandbox-mcp -- --scripts-dir /path/to/scripts --tools-dir /path/to/tools --setup-dir /path/to/setup
```

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--scripts-dir` | `.sandbox/scripts` | Path to scripts directory |
| `--tools-dir` | `.sandbox/tools` | Path to tools directory |
| `--setup-dir` | `.sandbox/sandbox-mcp-setup` | Path to setup-scripts directory |
| `--setup-output-dir` | `.sandbox/.state/setup-output` | Path to setup-script output spill directory (see [`@output: file`](#setup-scripts-sandboxsandbox-mcp-setup)) |
| `--config` | (auto-detect) | Path to config file |
| `--workspace` | (CWD) | Workspace root for resolving relative paths |

### Version

```bash
sandbox-mcp version
```

### Configuration

Configuration is resolved with the following priority (highest first):

1. CLI flags (`--scripts-dir`, `--tools-dir`, `--setup-dir`, `--setup-output-dir`)
2. Config file
3. Environment variables (`SANDBOX_SCRIPTS_DIR`, `SANDBOX_TOOLS_DIR`, `SANDBOX_SETUP_DIR`, `SANDBOX_SETUP_OUTPUT_DIR`)
4. Defaults (`.sandbox/scripts`, `.sandbox/tools`, `.sandbox/sandbox-mcp-setup`, `.sandbox/.state/setup-output`)

#### Config File

SandboxMCP looks for a config file in these locations:

1. `.sandbox/config/sandbox-mcp.yaml` (project-level)
2. `~/.config/sandbox-mcp/config.yaml` (user-level)

```yaml
scripts_dir: ".sandbox/scripts"
tools_dir: ".sandbox/tools"
setup_dir: ".sandbox/sandbox-mcp-setup"
setup_output_dir: ".sandbox/.state/setup-output"
```

> **Tip:** To disable the [setup-scripts feature](#setup-scripts-sandboxsandbox-mcp-setup) entirely, set `setup_dir: ""` (it does not fall back to the workspace root).

## MCP Tools

When you ask the AI to run a script or tool, it works behind the scenes in this order: `list_*` → `get_*_info` → `run_*`.

| Tool | Description |
|------|-------------|
| `list_scripts` | List available scripts. Optional: `category` filter (`"utility"` / `"test"` / `"all"`) |
| `get_script_info` | Get detailed info about a specific script |
| `run_script` | Execute a script |
| `list_tools` | List available Go tools |
| `get_tool_info` | Get detailed info about a specific tool |
| `run_tool` | Execute a Go tool via `go run`. Timeout: 30 seconds |

> **Note:** `run_script` also has a 30-second timeout. Scripts that need more time should handle this themselves (e.g. run a background process).

## Startup Context

At startup, SandboxMCP automatically builds context for the AI and includes it in the MCP `instructions` (shown as `<system-reminder>` in Claude Code). This helps the AI understand your workspace without manual explanation.

This matters even for AI agents that already have full shell access. Because `instructions` is delivered automatically at MCP connection time, the AI receives this context without having to decide to go look for it — no separate discovery step, no risk of it being skipped. And because it's a standard MCP protocol field rather than an editor-specific hook, the same setup scripts work unmodified for any MCP client that connects to SandboxMCP (e.g. Gemini CLI), instead of being locked to one editor's hook configuration.

### Nested Git Repository Detection

SandboxMCP scans the workspace for independent git repositories (up to 3 levels deep) and lists them in the instructions. This prevents the AI from accidentally running git commands in the wrong directory.

Example output in `<system-reminder>`:
```
Nested git repositories (independent repos — run git commands from within each directory, not the workspace root):
- sandbox-mcp
```

### Setup Scripts (`.sandbox/sandbox-mcp-setup/`)

Place shell scripts in `.sandbox/sandbox-mcp-setup/` (or wherever `--setup-dir` / `SANDBOX_SETUP_DIR` / `setup_dir` points — see [Configuration](#configuration)) to inject custom context at startup. Scripts run in alphabetical order; their stdout is appended to the instructions.

```
.sandbox/sandbox-mcp-setup/
├── 10-find-git-repos.sh   # e.g. show repos with current branch
└── 20-check-env.sh        # e.g. verify required environment variables
```

Scripts are executed with `bash` and have a 5-second timeout. Failed or timed-out scripts are silently skipped. A numeric prefix (`10-`, `20-`, ...) is a common convention for controlling execution order, since scripts run in alphabetical order.

Example (`.sandbox/sandbox-mcp-setup/10-find-git-repos.sh`):
```bash
#!/bin/bash
WORKSPACE="${WORKSPACE_DIR:-/workspace}"
find "$WORKSPACE" -maxdepth 3 -name ".git" -type d 2>/dev/null \
  | grep -v "^$WORKSPACE/.git$" | sed 's|/.git$||' | sort \
  | while IFS= read -r repo_path; do
      rel="${repo_path#"$WORKSPACE"/}"
      branch=$(git -C "$repo_path" branch --show-current 2>/dev/null || echo "detached")
      echo "- $rel (branch: $branch)"
    done
```

A script's header comment can also declare `# @output: file` to write its stdout to a file instead of inlining it into `instructions`:

```bash
#!/bin/bash
# @output: file
echo "This goes to a file, not directly into the instructions field."
```

`instructions` has a byte budget, and MCP clients silently truncate it once exceeded — output beyond the limit is dropped with no indication anything was cut. Tagging a script `@output: file` avoids that risk: the full stdout is written to `<setup-output-dir>/sandbox-mcp-pids/<pid>/<script-name>.txt` (default `.sandbox/.state/setup-output`, configurable via `--setup-output-dir` / `setup_output_dir` in the config file / `SANDBOX_SETUP_OUTPUT_DIR`) instead of counting against the `instructions` budget, and `instructions` gets only a short pointer line. Stale directories left behind by past instances that are no longer running are pruned automatically. If no output directory is configured, the tag has no effect and output is inlined as usual.

SandboxMCP's own capabilities section — the "Available tools" / "Available scripts" / nested git repos listing at the top of `instructions` — grows as you add more scripts and tools, so it's spilled the same way whenever `setup-output-dir` is configured: it's written to `<setup-output-dir>/sandbox-mcp-pids/<pid>/00-capabilities.txt`, merged into the same single pointer line as any spilled setup scripts. This happens automatically, with no tag or extra configuration needed; if no output directory is configured, or the spill attempt fails, it stays inlined as before.

> **Real-world example:** see [AI Sandbox's `.sandbox/sandbox-mcp-setup/`](https://github.com/YujiSuzuki/ai-sandbox/tree/main/.sandbox/sandbox-mcp-setup) and [its architecture docs](https://github.com/YujiSuzuki/ai-sandbox/blob/main/docs/architecture.md#startup-context-injection).

## Adding Scripts and Tools

Sample scripts and tools can be found in [AI Sandbox](https://github.com/YujiSuzuki/ai-sandbox) under `.sandbox/scripts/` and `.sandbox/tools/`.

### Scripts (`.sandbox/scripts/`)

Executable scripts with a header comment for description. There is no language/extension requirement — `run_script` invokes the file directly (not via `bash`), so any language works as long as the file is executable (`chmod +x`) and has a shebang line (e.g. `#!/usr/bin/env python3`). The header parser looks for `#`-style comments, so this works out of the box for Python, Ruby, Perl, and shell; other comment syntaxes (e.g. `//`) will run fine but won't get a parsed description.

```bash
#!/bin/bash
# my-script.sh
# Short description shown in list_scripts
# @advertise: true
#
# Detailed usage information.
#
# Usage:
#   my-script.sh [options] <args>
#
# ---
# Anything after # --- is ignored by the parser (e.g. localized descriptions)
```

- **Line 1**: Shebang
- **Line 2**: Filename (skipped by the parser)
- **Line 3**: One-line description (shown in `list_scripts` and `<system-reminder>`)
- **Line 4**: `@advertise: true`. The description shown in `<system-reminder>` is limited to line 3 (lines below `@advertise` are not included, even if present).
- **`@advertise: true`**: The script is listed in `<system-reminder>` at the start of every conversation — the AI knows about it without needing to call `list_scripts` first
- **`@env: container`**: Marks the script as container-only (see **Env** below). Default is `any`; `@env: any` is also accepted (a no-op, but useful to state explicitly)
- **`@category: test` / `@category: utility`**: Overrides the filename-based `Category` classification below — useful when a script's purpose doesn't match its name
- **`@hidden: true`**: Excludes the script from `list_scripts` (e.g. a human-facing CLI entry point that isn't meant to be run by the AI). Default is `false`
- **`# ---`**: Parsing stops here; content below is for human readers only
- **`Usage:` (or `使用法:` in Japanese)**: If present before `# ---`, shown by `get_script_info`

> **Note:** `@advertise`, `@env`, `@category`, and `@hidden` are currently the only supported `@key:` metadata tags — any other `@key:` line just stops description collection without being parsed. Candidate additions, not yet implemented:
> - **`@timeout: 60`** — allow individual scripts to opt into a longer timeout than the fixed 30s default (see [MCP Tools](#mcp-tools))
> - **`@requires: gh, jq`** — declare external command dependencies, so the AI can check they're available (or explain why a script can't run) before executing it, instead of failing partway through

**Category** is auto-detected from the filename, and can be overridden per-script with `@category:`:
- Starts with `test-` → `test`
- All others → `utility`

**Env** (execution environment) defaults to `any` for every script. There is no built-in filename list — mark a script container-only by adding `@env: container` to its own header:

| Env | Meaning |
|-----|---------|
| `container` — container-only | Set via `@env: container` in the script's own header |
| `any` — runs anywhere | Default; no tag needed |

> **Note:** `Environment` is informational only — it's surfaced to the AI via `list_scripts`/`get_script_info` as a hint, but `run_script` does not check it or block execution based on it.

> **Tip:** Scripts with a `_` prefix (e.g. `_lib.sh`) are treated as libraries and are excluded from `list_scripts`. There is no hardcoded filename exclusion beyond this — a script you want left out of `list_scripts` excludes itself via `@hidden: true` in its own header.

### Tools (`.sandbox/tools/`)

Go source files with header comments. Tools are executed via `go run`:

```go
// my-tool.go - Short description
//
// Usage:
//   go run .sandbox/tools/my-tool.go [options] <args>
//
// Examples:
//   go run .sandbox/tools/my-tool.go --flag value
//
// ---
// (anything after // --- is ignored by the parser)
package main
```

- **First non-empty comment line**: Description (shown in `list_tools`)
- **`Usage:`**: If present before `// ---`, shown by `get_tool_info`
- **`Examples:`**: If present before `// ---`, shown by `get_tool_info`
- **`// ---`**: Parsing stops here; content below is for human readers only
- Parsing also stops at the `package` declaration

## Troubleshooting

### "sandbox-mcp: command not found"

- Installed via `go install`? Ensure `$(go env GOPATH)/bin` (usually `~/go/bin`) is on your `PATH`.
- Installed via the prebuilt binary? Ensure the directory you moved it to (e.g. `/usr/local/bin`) is on your `PATH`.

### MCP tools not showing up in Claude Code / Gemini CLI

1. Verify registration: `claude mcp list` (or `gemini mcp list`) should include `sandbox-mcp`.
2. Reconnect: in Claude Code, run `/mcp` → "Reconnect".
3. Re-register if missing: `claude mcp add sandbox-mcp sandbox-mcp` (see [Usage](#usage)).

### A script doesn't appear in `list_scripts`

- Files starting with `_` (e.g. `_lib.sh`) are treated as libraries and excluded by design.
- Check the header doesn't have `@hidden: true`.
- Confirm the file is executable (`chmod +x`) — non-executable files are skipped.
- If filtering with the `category` argument, confirm the filename (or `@category:` override) matches: `test-` prefix → `test`, everything else → `utility`.

### A tool doesn't appear in `list_tools`

- Confirm it's a `.go` file directly under `--tools-dir` (default `.sandbox/tools/`) and that its name doesn't end in `_test.go`. Unlike scripts, listing doesn't check the `package` declaration.

### What happens to `@output: file` if `setup_output_dir` isn't configured?

The tag has no effect, and the stdout is inlined into `instructions` as usual (no error).

### Do old output files written by `@output: file` clean themselves up?

Yes. Directories left behind by past processes that are no longer running (`<setup-output-dir>/sandbox-mcp-pids/<pid>/`) are pruned automatically.

## Development

```bash
make build         # Build binary
make install       # Install to GOPATH/bin
make register      # Register with available CLIs (Claude, Gemini)
make unregister    # Remove from available CLIs
make test          # Run unit tests
make test-version  # Verify ldflags version injection
make clean         # Remove built binary
```

## License

MIT




## Consulting / Hire Me

Open to consulting on MCP server security and AI agent sandbox design.

- GitHub: https://github.com/YujiSuzuki
- LinkedIn: https://www.linkedin.com/in/yuji-suzuki-dev
