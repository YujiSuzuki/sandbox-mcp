# SandboxMCP

[English README is here](README.md)

`.sandbox/scripts/` にスクリプトを置くだけで、AI が自動的に発見・実行できる軽量 MCP（Model Context Protocol）サーバーです。各ツールを MCP に個別登録する手間がかかりません。

## 概要

SandboxMCP は **AI Sandbox** エコシステムの一部です:

| | SandboxMCP | [HostMCP](https://github.com/YujiSuzuki/hostmcp) | [AI Sandbox](https://github.com/YujiSuzuki/ai-sandbox) |
|---|---|---|---|
| 動作場所 | コンテナ内 | ホスト OS | テンプレート／環境 |
| トランスポート | stdio | SSE (HTTP) | — |
| 用途 | スクリプト/ツール検出 | コンテナ間アクセス | 全体をまとめる |
| 起動 | 自動（Claude Code） | 手動（`hostmcp serve`） | — |

**典型的な構成:**

```
AI Sandbox（コンテナ）
  └─ SandboxMCP（stdio）  ← .sandbox/scripts/ と .sandbox/tools/ を検出
  └─ HostMCP（HTTP 経由）  ← ホスト OS 上の他コンテナへのアクセスを中継
        ↓
  ホスト OS: HostMCP サーバー → API コンテナ、DB コンテナ など
```

> **AI Sandbox を使っている場合:** コンテナ起動のたびに自動でインストール・登録されます。手動での作業は不要です。
>
> **既存の開発コンテナに追加したい場合:** 以下のインストール手順に従ってください。

## インストール

```bash
go install github.com/YujiSuzuki/sandbox-mcp@latest
```

ソースからビルドする場合:

```bash
git clone https://github.com/YujiSuzuki/sandbox-mcp.git
cd sandbox-mcp
make install
```

## 使い方

### Claude Code または Gemini CLI に登録

インストールと登録を一度に行う方法:

```bash
make install register
```

手動で行う場合:

```bash
claude mcp add sandbox-mcp sandbox-mcp
# または Gemini CLI の場合:
gemini mcp add sandbox-mcp sandbox-mcp
```

カスタムパスを指定する場合:

```bash
claude mcp add sandbox-mcp sandbox-mcp -- --scripts-dir /path/to/scripts --tools-dir /path/to/tools
```

### CLI フラグ

| フラグ | デフォルト | 説明 |
|--------|-----------|------|
| `--scripts-dir` | `.sandbox/scripts` | スクリプトディレクトリのパス |
| `--tools-dir` | `.sandbox/tools` | ツールディレクトリのパス |
| `--config` | （自動検出） | 設定ファイルのパス |
| `--workspace` | （CWD） | 相対パスを解決する起点となるワークスペースルート |

### バージョン確認

```bash
sandbox-mcp version
```

### 設定

設定は以下の優先順位で解決されます（上が最優先）:

1. CLI フラグ（`--scripts-dir`, `--tools-dir`）
2. 設定ファイル
3. 環境変数（`SANDBOX_SCRIPTS_DIR`, `SANDBOX_TOOLS_DIR`）
4. デフォルト値（`.sandbox/scripts`, `.sandbox/tools`）

#### 設定ファイル

以下の順序で設定ファイルを探索します:

1. `.sandbox/config/sandbox-mcp.yaml`（プロジェクトレベル）
2. `~/.config/sandbox-mcp/config.yaml`（ユーザーレベル）

```yaml
scripts_dir: ".sandbox/scripts"
tools_dir: ".sandbox/tools"
```

## MCP ツール

スクリプトやツールの実行を依頼すると、AI は裏側でこの順に動きます: `list_*` → `get_*_info` → `run_*`。

| ツール | 説明 |
|--------|------|
| `list_scripts` | スクリプト一覧を表示。オプション: `category` フィルタ（`"utility"` / `"test"` / `"all"`） |
| `get_script_info` | スクリプトの詳細情報を取得 |
| `run_script` | スクリプトを実行。ホスト専用スクリプトは手動実行のヒント付きで拒否される |
| `list_tools` | 利用可能な Go ツールを一覧表示 |
| `get_tool_info` | ツールの詳細情報を取得 |
| `run_tool` | `go run` で Go ツールを実行。タイムアウト: 30 秒 |

> **注:** `run_script` も 30 秒でタイムアウトします。長時間かかるスクリプトはバックグラウンド処理などで対応してください。

## スクリプトとツールの追加

### スクリプト（`.sandbox/scripts/`）

ヘッダーコメントで説明を記述したシェルスクリプト:

```bash
#!/bin/bash
# my-script.sh
# list_scripts に表示される簡単な説明
#
# 詳細な使い方。
#
# Usage:
#   my-script.sh [オプション] <引数>
#
# ---
# # --- 以降はパーサーに読み込まれません（日本語説明など人間向けの内容を書く場所）
```

- **1行目**: Shebang
- **2行目**: ファイル名（パーサーはスキップ）
- **3行目以降**: 説明文（`list_scripts` に表示）
- **`# ---`**: ここで解析停止。以降は人間向けの内容
- **`Usage:` または `使用法:`**: `# ---` より前にあれば `get_script_info` で表示。英語だけでも日本語だけでも構いません

**カテゴリ**はファイル名から自動判定されます:
- `test-` で始まる → `test`
- それ以外 → `utility`

**実行環境（Env）**は既知のシステムスクリプトに対して内部で決定されます:

| Env | スクリプト |
|-----|-----------|
| `host` — `run_script` で拒否され、ホスト上での実行方法が案内される | `init-host-env.sh` |
| `container` — コンテナ内専用 | `sync-secrets.sh`, `validate-secrets.sh`, `sync-compose-secrets.sh` |
| `any` — どこでも実行可能 | それ以外すべて |

> **ヒント:** `_` プレフィックスのスクリプト（例: `_lib.sh`）はライブラリとして扱われ、`list_scripts` の一覧から除外されます。`help.sh` も同様に除外されます。

### ツール（`.sandbox/tools/`）

`go run` で実行される Go ソースファイルです:

```go
// my-tool.go - 簡単な説明
//
// Usage:
//   go run .sandbox/tools/my-tool.go [オプション] <引数>
//
// Examples:
//   go run .sandbox/tools/my-tool.go --flag value
//
// ---
// （// --- 以降はパーサーに読み込まれません）
package main
```

- **最初の空でないコメント行**: 説明文（`list_tools` に表示）
- **`Usage:`**: `// ---` より前にあれば `get_tool_info` で表示
- **`Examples:`**: `// ---` より前にあれば `get_tool_info` で表示
- **`// ---`**: ここで解析停止。以降は人間向けの内容
- `package` 宣言に達した時点でも解析を停止します

## 開発

```bash
make build         # バイナリビルド
make install       # GOPATH/bin へインストール
make register      # 利用可能な CLI（Claude、Gemini）に登録
make unregister    # 利用可能な CLI から登録を解除
make test          # ユニットテスト実行
make test-version  # ldflags バージョン注入の検証
make clean         # ビルド済みバイナリを削除
```

## ライセンス

MIT
