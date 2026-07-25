# SandboxMCP

[日本語版はこちら](README.ja.md)

A **lightweight MCP (Model Context Protocol) server that runs inside your container**. It communicates with AI agents like Claude Code via stdio and auto-discovers scripts and tools placed in `.sandbox/scripts/` or `.sandbox/tools/`, making them available for AI to run autonomously.

- Drop scripts you want AI to run — AI discovers them automatically
- No need to tell AI the script path or usage every time
- AI reads the purpose and usage directly from header comments

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
claude mcp add sandbox-mcp sandbox-mcp -- --scripts-dir /path/to/scripts --tools-dir /path/to/tools
```

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--scripts-dir` | `.sandbox/scripts` | Path to scripts directory |
| `--tools-dir` | `.sandbox/tools` | Path to tools directory |
| `--config` | (auto-detect) | Path to config file |
| `--workspace` | (CWD) | Workspace root for resolving relative paths |

### Version

```bash
sandbox-mcp version
```

### Configuration

Configuration is resolved with the following priority (highest first):

1. CLI flags (`--scripts-dir`, `--tools-dir`)
2. Config file
3. Environment variables (`SANDBOX_SCRIPTS_DIR`, `SANDBOX_TOOLS_DIR`)
4. Defaults (`.sandbox/scripts`, `.sandbox/tools`)

#### Config File

SandboxMCP looks for a config file in these locations:

1. `.sandbox/config/sandbox-mcp.yaml` (project-level)
2. `~/.config/sandbox-mcp/config.yaml` (user-level)

```yaml
scripts_dir: ".sandbox/scripts"
tools_dir: ".sandbox/tools"
```

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

### Nested Git Repository Detection

SandboxMCP scans the workspace for independent git repositories (up to 3 levels deep) and lists them in the instructions. This prevents the AI from accidentally running git commands in the wrong directory.

Example output in `<system-reminder>`:
```
Nested git repositories (independent repos — run git commands from within each directory, not the workspace root):
- sandbox-mcp
```

### Setup Scripts (`.sandbox/sandbox-mcp-setup/`)

Place shell scripts in `.sandbox/sandbox-mcp-setup/` to inject custom context at startup. Scripts run in alphabetical order; their stdout is appended to the instructions.

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

- Confirm it's a `.go` file directly under `--tools-dir` (default `.sandbox/tools/`) with a `package main` declaration.

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
