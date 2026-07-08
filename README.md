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
| `run_script` | Execute a script. Host-only scripts are rejected with guidance on how to run them manually |
| `list_tools` | List available Go tools |
| `get_tool_info` | Get detailed info about a specific tool |
| `run_tool` | Execute a Go tool via `go run`. Timeout: 30 seconds |

> **Note:** `run_script` also has a 30-second timeout. Scripts that need more time should handle this themselves (e.g. run a background process).

## Adding Scripts and Tools

Sample scripts and tools can be found in [AI Sandbox](https://github.com/YujiSuzuki/ai-sandbox) under `.sandbox/scripts/` and `.sandbox/tools/`.

### Scripts (`.sandbox/scripts/`)

Shell scripts with a header comment for description:

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
- **Line 4**: `@advertise: true` and other metadata. The description shown in `<system-reminder>` is limited to the line(s) above.
- **`@advertise: true`**: The script is listed in `<system-reminder>` at the start of every conversation — the AI knows about it without needing to call `list_scripts` first
- **`# ---`**: Parsing stops here; content below is for human readers only
- **`Usage:` (or `使用法:` in Japanese)**: If present before `# ---`, shown by `get_script_info`

**Category** is auto-detected from the filename:
- Starts with `test-` → `test`
- All others → `utility`

**Env** (execution environment) is determined internally for known system scripts:

| Env | Scripts |
|-----|---------|
| `container` — container-only | `sync-secrets.sh`, `validate-secrets.sh`, `sync-compose-secrets.sh` |
| `any` — runs anywhere | All others |

> **Tip:** Scripts with a `_` prefix (e.g. `_lib.sh`) are treated as libraries and are excluded from `list_scripts`. `help.sh` is also excluded.

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
