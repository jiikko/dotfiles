# src/ — 自作ツールのソースコード

各サブディレクトリが 1 つの独立したプロジェクト（現在はすべて Go）。使い方・設計は各プロジェクトの README を参照。シェルからの入口は `bin/` のラッパが担う。

## プロジェクトの一覧

各プロジェクトの使い方はそれぞれの README。`src/*/go.mod` を持つディレクトリが 1 プロジェクトで、足したらこの表にも 1 行足す。

| プロジェクト | 何か |
|---|---|
| [`glogx`](glogx/README.md) | git log の TUI (tmux の `C-g` の popup)。stage / push・CI・issues viewer・ratelimit・doctor の画面 |
| [`pro-con`](pro-con/README.md) | PM と PG を分けて Claude Code を並列に回すカンバンの TUI |
| [`schedkeys`](schedkeys/README.md) | 「いつ・何を pane へ打つか」を決める予約入力の popup の UI |
| [`ratelimit`](ratelimit/README.md) | Claude / codex の利用枠 (5h / weekly) を取って整形する module と `bin/ratelimit` |
| [`doctor`](doctor/README.md) | 消してよさそうなもの・壊れて残っている常駐を見つけるライブラリと `diskdoctor` / `svcdoctor` |
| [`lockman`](lockman/README.md) | ディレクトリ単位の排他を取る CLI (SMB 越しも) |
| [`runtimeout`](runtimeout/README.md) | 時間の上限付きで実行し、子孫ごと止める CLI (`timeout` の代わり) |
| [`restartable`](restartable/README.md) | コマンドを前面で起動し、キーか socket から再起動できる runner |
| [`process_supervisor`](process_supervisor/README.md) | 呼んだプロセスが生きている間だけ子を見張って起こし直す module |
| [`zundamon-kaisetsu`](zundamon-kaisetsu/README.md) | VOICEVOX の掛け合い解説動画 (HTML / mp4) を作る CLI (skill `zundamon-kaisetsu` の本体) |
| [`disassemble_excel`](disassemble_excel/README.md) | Excel (`.xlsx` / `.xlsm` / `.xlsb`) を diff できるテキスト群に分解する CLI |
| [`chromecookie`](chromecookie/README.md) | Chrome のプロファイルから Cookie を復号して読むライブラリ |
| [`tuikit`](tuikit/README.md) | TUI の部品 (画面遷移・演出・幅計算・確認・入力欄)。glogx / pro-con / schedkeys / ratelimit / restartable が使う |
| [`termsafe`](termsafe/README.md) | 外から来た文字列を端末へ安全に出す無害化 |
| [`subproc`](subproc/README.md) | 外部プロセス実行の安全弁 (WaitDelay と git の timeout) |
| [`atomicfile`](atomicfile/README.md) | 途中の状態を残さないファイル書き込み (temp + rename) |
| [`proctree`](proctree/README.md) | プロセスの子孫を集めて止める (runtimeout と zundamon-kaisetsu が使う) |

## 新規プロジェクトのガイドライン

プロジェクトを追加するときは、以下の **3 点セット**を必ず揃える。どれか欠けると lint / test がローカルまたは CI から漏れる（disassemble_excel はこれが無かったためテスト 6 ファイルが死蔵していた実例あり）。

1. **プロジェクト直下に Makefile（`lint` / `test` ターゲット必須）**
   root の `make test-go-lint` / `make test-go` と CI が `make -C src/<name> lint|test` として呼ぶ契約。実装言語が Go 以外になっても、この 2 ターゲットの契約だけは維持する
2. **`go.mod` と `go.sum` をプロジェクト直下に置く** (登録作業は無い。依存が無くても `go.sum` は空のファイルを置く)
   root [Makefile](../Makefile) の `GO_PROJECT_DIRS` は `src/*/go.mod` の存在で自動発見する
   (issue 080)。ローカルの `make test`（コミット前検証）に自動で組み込まれる。`go.sum` は CI のキャッシュの復元に使うので、
   無いと CI の Lint (`test-go-project-lanes`) が落ちる (issue 641 で漏らした)
3. **`.github/workflows/src_<name>.yml` を作成**
   paths filter 付きの専用 workflow で lint / test を回す（プロジェクトに触れた push だけで起動）。
   🚨 paths filter 付き check を branch protection の **required check に登録しないこと**（非接触 PR では run が生成されず、check が永遠に pending になる）

揃えたら、プロジェクトの `make lint` / `make test` だけでなく **root の `make test-lint`** も回す (CI の Lint と同じ集約 target。
3 点セットと go.sum・CI レーンの有無は `test-go-project-lanes` が検査する)。

補足:

- **1 つの module に複数の CLI (main) を置くとき** は `src/<mod>/cmd/<name>/main.go` に置き、bin ラッパーは
  `go_autobuild_exec --pkg cmd/<name> "$src/<mod>" <name> -- "$@"` と書く (実例: `src/doctor` の
  `bin/svcdoctor` / `bin/diskdoctor`)。指紋は module root 全体、成果物と `.autobuild.*` は `cmd/<name>/` に置かれる。
  `.gitignore` は `cmd/<name>/<name>` と `cmd/*/.autobuild.*` を除外する。別 module から取り込むときは
  go.mod の `replace <mod> => ../<mod>` (実例: glogx → doctor)

- **`bin/` のラッパーを zsh で書いたら root [Makefile](../Makefile) の `ZSH_SYNTAX_FILES` に登録する**。
  `scripts/discover_shell_scripts.sh` が shebang で拾い、登録漏れは **shellcheck の SC1071 で
  `make test` 全体が落ちる** (bin/schedkeys 追加時に実際に落とした 2026-08-27)
- `.golangci.yml` は必須。`run:` 節に `allow-parallel-runners: true` を入れる（root の `make test` が全プロジェクトの lint を並列に回すため。`tests/scripts/test_golangci_parallel_runners.sh` が無い・入っていないを落とす。issue 258）。カスタム lint は任意で、実例は glogx
- **tuikit を使う module (go.mod に `src/tuikit` を持つもの) は、x/ansi と bubbletea を tuikit と同じ版にし、`.golangci.yml` の forbidigo に折り返しの禁止 `^ansi\.(Hardwrap|Wrap|Wordwrap)$` を入れる**（`tests/scripts/test_tuikit_consumers_aligned.sh` がずれを落とす。issue 603）。新しく tuikit を使う module を切るときも同じ
- golangci-lint はインストール不要（Makefile が `scripts/golangci_lint.sh` 経由で版を固定して実行。初回に版ごとの決まった場所へビルドし、以後はそれを起動する）
- テストが「重い / 環境依存」に思えても、CI から除外する前に**実測**すること（parallel-each は「TUI 依存で重い」とされていたが実測 8.7s で CI 投入できた。
  この repo からは 2026-09-08 に出たが、判断の作法として残す）

## Template

下のテンプレは実物（[glogx/Makefile](glogx/Makefile)・[src_glogx.yml](../.github/workflows/src_glogx.yml) 等）の写し。乖離していたら実物を正としてこちらを直す。

### src/&lt;name&gt;/Makefile

```make
# <name> (Go) の静的解析とテスト。root Makefile の test-go-lint / test-go と CI
# (.github/workflows/src_<name>.yml) から `make -C src/<name> lint|test` として呼ばれる
# 自己完結ターゲット。golangci-lint はインストール不要で、scripts/golangci_lint.sh が版を固定して一度だけビルドし起動する。
GOLANGCI_LINT_VERSION := v2.5.0

.PHONY: lint test

lint:
	../../scripts/golangci_lint.sh $(GOLANGCI_LINT_VERSION) run ./...

test:
	go test -race ./...
```

### .github/workflows/src_&lt;name&gt;.yml

```yaml
---
# src/<name> の CI。lint + test の本体は再利用 workflow _go-project.yml にあり、ここは
# paths filter (触れた変更のときだけ起動) と対象 dir の指定だけを持つ薄い caller。
name: src/<name>

on:
  push:
    branches: [master]
    paths:
      - 'src/<name>/**'
      - .github/workflows/src_<name>.yml
      - .github/workflows/_go-project.yml
      - scripts/golangci_lint.sh   # 全プロジェクトの make lint が通る共有の入口
  pull_request:
    paths:
      - 'src/<name>/**'
      - .github/workflows/src_<name>.yml
      - .github/workflows/_go-project.yml
      - scripts/golangci_lint.sh
  workflow_dispatch:  # GitHub UI からの手動再実行用

permissions:
  contents: read

# 同一 ref の連続 push では古い run を打ち切る (旧コミットの結果に価値が無いため)
concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true

jobs:
  # job id はチェック名の識別子 (check 名が "<id> / lint" になる)。全 caller で同じだと区別できないのでプロジェクト名にする
  <name>:
    uses: ./.github/workflows/_go-project.yml
    with:
      dir: src/<name>
```
