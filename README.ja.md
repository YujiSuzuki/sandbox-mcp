# SandboxMCP

[English README is here](README.md)

**コンテナ内で動作する軽量 MCP（Model Context Protocol）サーバー**です。Claude Code などの AI エージェントと stdio で通信し、`.sandbox/scripts/` や `.sandbox/tools/` に置いたスクリプト・ツールを自動検出して AI が自律的に実行できるようにするほか、起動時にはワークスペースの文脈（git の状態や環境情報など）も AI へ自動的に届けます。

- AI に実行してほしいスクリプトを置くだけで AI が自動的に発見できる
- スクリプトのパスや使い方を AI に毎回伝える必要がない
- ヘッダーコメントから目的・使い方を AI が自分で読める
- 起動時に git の状態や環境情報などのカスタムコンテキストを AI へ自動的に届けることもできる — [起動時コンテキスト](#起動時コンテキスト) 参照

## 概要

**AI Sandbox**・**HostMCP** との関係:

| | SandboxMCP | [HostMCP](https://github.com/YujiSuzuki/hostmcp) | [AI Sandbox](https://github.com/YujiSuzuki/ai-sandbox) |
|---|---|---|---|
| 動作場所 | コンテナ内 | ホスト OS | テンプレート／環境 |
| トランスポート | stdio | SSE (HTTP) | — |
| 用途 | スクリプト/ツール検出 | Docker コンテナ・ホストツール・ホスト OS コマンド | 環境構築・秘匿管理テンプレート |
| 起動 | 自動（Claude Code） | 手動（`hostmcp serve`） | — |

**典型的な構成:**

```
AI Sandbox（コンテナ）
  └─ SandboxMCP（stdio）  ← .sandbox/scripts/ と .sandbox/tools/ を検出
  └─ hostmcp client（HTTP 経由）  ← ホスト OS の HostMCP サーバーと通信
        ↓
ホスト OS: HostMCP サーバー → API コンテナ、DB コンテナ、ホストツール など
```

> **AI Sandbox を使っている場合:** コンテナ起動時に自動でインストール・登録されます。手動での作業は不要です。
>
> **既存の開発コンテナに追加したい場合:** 以下のインストール手順に従ってください。

## インストール

```bash
go install github.com/YujiSuzuki/sandbox-mcp@latest
```

または、ビルド済みバイナリをダウンロード（Go不要。SandboxMCPはLinuxコンテナ内で動作する前提のため、Linux向けバイナリのみ提供しています）:

```bash
curl -L https://github.com/YujiSuzuki/sandbox-mcp/releases/latest/download/sandbox-mcp_linux_amd64 -o sandbox-mcp
chmod +x sandbox-mcp
sudo mv sandbox-mcp /usr/local/bin/
```

> ARMホスト向けに `sandbox-mcp_linux_arm64` も提供しています。これは AI Sandbox の `startup.sh` がコンテナ内にGoがない場合に自動でダウンロードするバイナリと同じものです。

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
claude mcp add sandbox-mcp sandbox-mcp -- --scripts-dir /path/to/scripts --tools-dir /path/to/tools --setup-dir /path/to/setup
```

### CLI フラグ

| フラグ | デフォルト | 説明 |
|--------|-----------|------|
| `--scripts-dir` | `.sandbox/scripts` | スクリプトディレクトリのパス |
| `--tools-dir` | `.sandbox/tools` | ツールディレクトリのパス |
| `--setup-dir` | `.sandbox/sandbox-mcp-setup` | セットアップスクリプトディレクトリのパス |
| `--setup-output-dir` | `.sandbox/.state/setup-output` | セットアップスクリプトの出力書き出し先ディレクトリのパス（[`@output: file`](#セットアップスクリプトsandboxsandbox-mcp-setup)参照） |
| `--config` | （自動検出） | 設定ファイルのパス |
| `--workspace` | （CWD） | 相対パスを解決する起点となるワークスペースルート |

### バージョン確認

```bash
sandbox-mcp version
```

### 設定

設定は以下の優先順位で解決されます（上が最優先）:

1. CLI フラグ（`--scripts-dir`, `--tools-dir`, `--setup-dir`, `--setup-output-dir`）
2. 設定ファイル
3. 環境変数（`SANDBOX_SCRIPTS_DIR`, `SANDBOX_TOOLS_DIR`, `SANDBOX_SETUP_DIR`, `SANDBOX_SETUP_OUTPUT_DIR`）
4. デフォルト値（`.sandbox/scripts`, `.sandbox/tools`, `.sandbox/sandbox-mcp-setup`, `.sandbox/.state/setup-output`）

#### 設定ファイル

以下の順序で設定ファイルを探索します:

1. `.sandbox/config/sandbox-mcp.yaml`（プロジェクトレベル）
2. `~/.config/sandbox-mcp/config.yaml`（ユーザーレベル）

```yaml
scripts_dir: ".sandbox/scripts"
tools_dir: ".sandbox/tools"
setup_dir: ".sandbox/sandbox-mcp-setup"
setup_output_dir: ".sandbox/.state/setup-output"
```

> **ヒント:** [セットアップスクリプト機能](#セットアップスクリプトsandboxsandbox-mcp-setup)を無効化するには `setup_dir: ""` を指定してください（ワークスペースルートへのフォールバックにはなりません）。

## MCP ツール

スクリプトやツールの実行を依頼すると、AI は裏側でこの順に動きます: `list_*` → `get_*_info` → `run_*`。

| ツール | 説明 |
|--------|------|
| `list_scripts` | スクリプト一覧を表示。オプション: `category` フィルタ（`"utility"` / `"test"` / `"all"`） |
| `get_script_info` | スクリプトの詳細情報を取得 |
| `run_script` | スクリプトを実行 |
| `list_tools` | 利用可能な Go ツールを一覧表示 |
| `get_tool_info` | ツールの詳細情報を取得 |
| `run_tool` | `go run` で Go ツールを実行。タイムアウト: 30 秒 |

> **注:** `run_script` も 30 秒でタイムアウトします。長時間かかるスクリプトはバックグラウンド処理などで対応してください。

## 起動時コンテキスト

起動時に SandboxMCP は AI 向けのコンテキストを自動的に構築し、MCP の `instructions`（Claude Code では `<system-reminder>` として表示）に含めます。AI がワークスペースの構成を把握するために毎回説明する手間がなくなります。

これは、すでにシェルへ直接アクセスできるAIエージェントにとっても意味があります。`instructions` はMCP接続時に自動的に届くため、AIが自ら探しに行く必要がなく、見落とされるリスクもありません。また、エディタ固有のフックではなくMCPプロトコル標準のフィールドであるため、同じセットアップスクリプトが SandboxMCP に接続する任意のMCPクライアント（例: Gemini CLI）でもそのまま動作し、特定エディタのフック設定に縛られません。

### ネストされた git リポジトリの自動検出

SandboxMCP は起動時にワークスペース内の独立した git リポジトリをスキャン（最大 3 階層）し、instructions に一覧を追加します。AI が誤ったディレクトリで git コマンドを実行するミスを防ぎます。

`<system-reminder>` への出力例:
```
Nested git repositories (independent repos — run git commands from within each directory, not the workspace root):
- sandbox-mcp
```

### セットアップスクリプト（`.sandbox/sandbox-mcp-setup/`）

`.sandbox/sandbox-mcp-setup/`（`--setup-dir` / `SANDBOX_SETUP_DIR` / `setup_dir` で変更可能 — [設定](#設定)参照）にシェルスクリプトを置くと、起動時にカスタムコンテキストを注入できます。スクリプトはアルファベット順に実行され、stdout(スクリプトの出力内容) が instructions に追記されます。

```
.sandbox/sandbox-mcp-setup/
├── 10-find-git-repos.sh   # 例: 現在のブランチ付きでリポジトリ一覧を表示
└── 20-check-env.sh        # 例: 必要な環境変数の確認
```

スクリプトは `bash` で実行され、タイムアウトは 5 秒です。失敗・タイムアウトしたスクリプトはサイレントスキップされます。スクリプトはアルファベット順に実行されるため、数字プレフィックス（`10-`, `20-`, ...）で実行順を制御するのが一般的な慣習です。

スクリプト例（`.sandbox/sandbox-mcp-setup/10-find-git-repos.sh`）:
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

`instructions` にはバイト数の上限があり、超過するとMCPクライアント側で無音のまま切り詰められます（何が削られたのか手がかりは残りません）。この切り詰めリスクを避けるため、スクリプト冒頭のコメントに `# @output: file` と書くと、標準出力を `instructions` に直接埋め込む代わりに、ファイルへ書き出すことができます。

```bash
#!/bin/bash
# @output: file
echo "この内容は instructions ではなくファイルに書き出されます。"
```

標準出力は `instructions` のバイト予算を消費する代わりに `<setup-output-dir>/sandbox-mcp-pids/<pid>/<スクリプト名>.txt`（デフォルトは `.sandbox/.state/setup-output`。`--setup-output-dir` / 設定ファイルの `setup_output_dir` / 環境変数 `SANDBOX_SETUP_OUTPUT_DIR` で変更可能）へ書き出され、`instructions` には短いポインタ行だけが残ります。

SandboxMCP自身が持つ「Available tools」「Available scripts」「ネストgitリポジトリ一覧」という`instructions`冒頭のセクションも、スクリプトやツールを追加していくほど肥大化していく性質を持つため、`setup-output-dir`が設定されていれば同じ仕組みで`<setup-output-dir>/sandbox-mcp-pids/<pid>/00-capabilities.txt`へ書き出され、退避済みのセットアップスクリプトと同じ1本のポインタ行に統合されます。タグや追加設定なしに常にこの動作になり、出力先ディレクトリが未設定の場合や書き出しに失敗した場合は、従来通りインラインのままです。

> **実際の運用例:** [AI Sandbox の `.sandbox/sandbox-mcp-setup/`](https://github.com/YujiSuzuki/ai-sandbox/tree/main/.sandbox/sandbox-mcp-setup) と [そのアーキテクチャドキュメント](https://github.com/YujiSuzuki/ai-sandbox/blob/main/docs/architecture.ja.md#起動時コンテキスト注入) を参照してください。

## スクリプトとツールの追加

[AI Sandbox](https://github.com/YujiSuzuki/ai-sandbox) の `.sandbox/scripts/` と `.sandbox/tools/` にサンプルがあります。参考にしてください。

### スクリプト（`.sandbox/scripts/`）

ヘッダーコメントで説明を記述した、実行可能なスクリプト。言語や拡張子の制約はありません — `run_script` はファイルを(`bash`経由ではなく)直接実行するため、実行権限(`chmod +x`)とシェバン行(例: `#!/usr/bin/env python3`)さえあれば、どの言語でも動きます。ヘッダー解析は `#` 形式のコメントを前提にしているため、Python・Ruby・Perl・シェルはそのまま説明文が読み取れます。それ以外のコメント記法(例: `//`)を使う言語も実行自体は問題なく動きますが、説明文は解析されません。

```bash
#!/bin/bash
# my-script.sh
# list_scripts に表示される簡単な説明
# @advertise: true
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
- **3行目**: 1行の説明文（`list_scripts` や `<system-reminder>` に表示）
- **4行目**: `@advertise: true`。`<system-reminder>` に表示される説明は3行目までで、`@advertise` より下の行があっても含まれません。
- **`@advertise: true`**: AI との会話開始時に自動的に `<system-reminder>` へ掲載される。AI がこのスクリプトの存在を最初から把握できるようになる
- **`@env: container`**: スクリプトをコンテナ専用としてマークする（下記の**Env**を参照）。デフォルトは `any`。`@env: any` も明示的に書ける（値としては何も変わらないが、意図を明記したい場合に）
- **`@category: test` / `@category: utility`**: 下記のファイル名ベースの `Category` 判定を上書きする。スクリプトの用途がファイル名と一致しない場合に便利
- **`@hidden: true`**: `list_scripts` の一覧から除外する（例: AIに実行させる想定のない、人間向けCLIの入口スクリプトなど）。デフォルトは `false`
- **`# ---`**: ここで解析停止。以降は人間向けの内容
- **`Usage:` または `使用法:`**: `# ---` より前にあれば `get_script_info` で表示。英語だけでも日本語だけでも構いません

> **注:** 現時点で実際にサポートされている `@key:` メタデータは `@advertise`、`@env`、`@category`、`@hidden` です。それ以外の `@key:` 行は説明文の収集を止める効果はありますが、値自体はパースされません。
>
> 今後の追加候補（まだ未実装）:
> - **`@timeout: 60`** — 固定30秒（[MCP ツール](#mcp-ツール)参照）より長いタイムアウトが必要なスクリプトだけ、個別に延長できるようにする
> - **`@requires: gh, jq`** — 依存する外部コマンドを宣言し、実行途中で失敗する前に、AIが事前に有無を確認したり「このツールがないので実行できない」と説明できるようにする

**カテゴリ**はファイル名から自動判定され、`@category:` でスクリプトごとに上書きできます:
- `test-` で始まる → `test`
- それ以外 → `utility`

**実行環境（Env）**は全スクリプトでデフォルト `any` です。組み込みのファイル名リストは存在しません — スクリプト自身のヘッダーに `@env: container` を書くことで、コンテナ専用と宣言できます:

| Env | 意味 |
|-----|-----|
| `container` — コンテナ内専用 | スクリプト自身のヘッダーで `@env: container` を指定 |
| `any` — どこでも実行可能 | デフォルト。タグ不要 |

> **注:** `Environment` はあくまで参考情報です。`list_scripts`/`get_script_info` を通じてAIへのヒントとして表示されるだけで、`run_script` はこの値をチェックせず、実行をブロックすることもありません。

> **ヒント:** `_` プレフィックスのスクリプト（例: `_lib.sh`）はライブラリとして扱われ、`list_scripts` の一覧から除外されます。それ以外にハードコードされたファイル名の除外はありません — `list_scripts` の一覧から外したいスクリプトは、自身のヘッダーで `@hidden: true` を指定することで一覧から除外されます。

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

## トラブルシューティング

### 「sandbox-mcp: command not found」と表示される

- `go install` でインストールした場合: `$(go env GOPATH)/bin`（通常は `~/go/bin`）が `PATH` に含まれているか確認してください。
- ビルド済みバイナリでインストールした場合: 配置先ディレクトリ（例: `/usr/local/bin`）が `PATH` に含まれているか確認してください。

### Claude Code / Gemini CLI に MCP ツールが表示されない

1. 登録状況を確認: `claude mcp list`（または `gemini mcp list`）に `sandbox-mcp` が含まれているか確認
2. 再接続: Claude Code で `/mcp` → 「Reconnect」を実行
3. 登録されていなければ再登録: `claude mcp add sandbox-mcp sandbox-mcp`（[使い方](#使い方)を参照）

### スクリプトが `list_scripts` に表示されない

- `_` で始まるファイル（例: `_lib.sh`）はライブラリとして扱われ、仕様上除外されます
- ヘッダーに `@hidden: true` が指定されていないか確認してください
- ファイルに実行権限があるか確認してください（`chmod +x`）。実行権限のないファイルはスキップされます
- `category` 引数でフィルタしている場合、ファイル名（または `@category:` による上書き）が条件と一致しているか確認してください（`test-` プレフィックス → `test`、それ以外 → `utility`）

### ツールが `list_tools` に表示されない

- `--tools-dir`(デフォルト `.sandbox/tools/`)直下の `.go` ファイルで、ファイル名が `_test.go` で終わっていないか確認してください。スクリプトと異なり、一覧表示時に `package` 宣言の中身はチェックされません。

### `setup_output_dir` を設定していない場合、`@output: file` はどうなる？

タグは何もせず、標準出力は通常通り `instructions` にそのまま埋め込まれます(エラーにはなりません)。

### `@output: file` で書き出した過去の出力ファイルは自動で消える？

はい。もう動いていない過去のプロセスのディレクトリ(`<setup-output-dir>/sandbox-mcp-pids/<pid>/`)は自動的に削除されます。

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



## コンサルティング / お仕事のご依頼

MCPサーバーのセキュリティおよびAIエージェントのサンドボックス設計に関するコンサルティングを承っています。

- GitHub: https://github.com/YujiSuzuki
- LinkedIn: https://www.linkedin.com/in/yuji-suzuki-dev
