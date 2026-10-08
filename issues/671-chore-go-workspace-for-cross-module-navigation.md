# 671 (chore): `src/` の Go module を go.work で 1 つの workspace にし、module をまたいで探せるようにする

起票日: 2026-10-08

## 概要

`src/` には Go の module が 17 個あり (`src/*/go.mod`)、go.work は無い (2026-10-08)。gopls は module ごとに閉じているので、
定義へのジャンプ・参照・workspace のシンボル検索が module をまたがない。
**既にある再利用できる package に気づけない**。実例: `bin/tmux-toast` は表示セル幅を python3 で自前に数えているが、
`src/tuikit/termwidth` が既にある (issue 670)。

go.work を置き、エディタ (nvim の gopls) と LLM の LSP ツールから全 module を 1 つの workspace として引けるようにする。

## 🚨 ビルドの解決を変えないこと (この issue の主なリスク)

- `go` コマンドは cwd から親へ go.work を探す。`src/go.work` を置くと、`src/<module>` で走る `go build` / `go test` / `go list` が
  **黙って workspace mode に入る**
- 今のビルドは module 単位で、依存先は各 `go.mod` の `replace` で解決している (`replace` を持つ module: glogx / ratelimit / doctor /
  pro-con / schedkeys / runtimeout / tuikit / zundamon-kaisetsu の 8 個)。workspace mode では go.work の `use` が優先され、解決結果が変わりうる
- 影響を受ける経路: `bin/lib/go_autobuild.zsh` (bin のラッパーの再ビルド)、各 `src/<module>/Makefile` の lint / test、
  root の `make test-go` / `make test-go-lint`、`.github/workflows/src_*.yml`、`go mod tidy`
- `GOWORK` を既に使っている箇所は `tests/scripts/test_tuikit_consumers_aligned.sh` だけ (`GOWORK=off go list -m`。「go.work があると全 module が
  workspace の版で解決され、ずれが見えなくなる」ため)。go_autobuild・Makefile・workflow には 0 件 (2026-10-08、`git grep GOWORK`)。
  **go.work があると、go.mod 間の版のずれの検出が構造的に弱まる**。この検査は今の形のまま GOWORK=off を保つ
- `bin/lib/go_autobuild.zsh` の指紋 (`_go_autobuild_inputs`) は `*.go` と各 root の `go.mod` / `go.sum` だけで、go.work を数えない。
  go.work が解決を変えると、**指紋の外で**ビルド結果が変わる (再ビルドも起きない)
- go.work を置くと `go.work.sum` もできる (sum の置き場が増える)。`go mod tidy` は workspace を無視して module 単位で動く
- CI の `cache-dependency-path` / `go-version-file` は go.work を見ないので、キャッシュの鍵は変わらない

## 対応方針 (案。実装の前に決める)

| 案 | 中身 | 欠点 |
|---|---|---|
| A. `src/go.work` を commit し、ビルド・テストの全経路で `GOWORK=off` | エディタは自動で拾う | 経路を 1 つでも漏らすと workspace mode で黙ってビルドされる。漏れを止める検査が要る |
| B. go.work を `src/` の外 (例: `nvim/` 側や `~/.cache`) に置き、gopls にだけ `GOWORK` を渡す (gopls の `build.env`。効くかは未確認) | ビルドの経路は何も変わらない | 手で打つ `go` には効かない。gopls を起動する経路ごとに設定が要る: `nvim/lua/dotfiles/lsp.lua` / `nvim/ftplugin/go.lua` / `_claude/settings.json` の 3 か所 |
| C. go.work を生成物にして gitignore し、使うときに作る | commit されたファイルがビルドを変えない | 手元に作った時点で A と同じリスクが出る |

どれを選ぶにしても:

- 採用前に、go.work の有無で解決が変わらないことを確かめる。🚨 **`go list -deps -f '{{.ImportPath}} {{.Module}}'` をそのまま比べない**:
  ローカル module の表示が `atomicfile v0.0.0 => ../atomicfile` から `atomicfile` に変わるので、解決が同じでも必ず差が出る
  (2026-10-08 の実測で glogx 58 行・pro-con 50・ratelimit 20・tuikit 10・doctor 4)。比べるのは
  **第三者の依存の版** (`.Module.Path` と `.Module.Version`、ローカル module を除く) と、**ローカル module の `.Module.Dir`** (同じ実体を指すか)。
  式は、上の既知の差が消えることで先に較正してから使う
- 実際に module をまたいで引けることを確かめる: gopls の workspace シンボル検索で、`src/glogx` を開いた状態から `src/tuikit/termwidth` のシンボルが出る
- A を選ぶなら、`go` を呼ぶ全 script / Makefile / workflow / `go_autobuild.zsh` の `go build` を機械で列挙して `GOWORK=off` を付け、
  抜けた経路を検出する。手で書いた一覧で経路を数えない

## 受け入れ条件

- [ ] 案を決め、理由を本 issue に書く
- [ ] go.work の有無で、全 module の第三者の依存の版とローカル module の実体 (`.Module.Dir`) が一致することを、較正した式で確認した (コマンドと出力の要約を本 issue に書く)
- [ ] `make test-go` と CI の `src_*.yml` が今までどおり通る
- [ ] gopls で module をまたいだシンボル検索が効くことを確認した
- [ ] 使い方 (go.work がどこにあり、何に効き、ビルドには効かないこと) を `src/README.md` に書く

## 関連ファイル

- `src/*/go.mod` / `bin/lib/go_autobuild.zsh` / `Makefile` (`test-go`) / `.github/workflows/src_*.yml`
- gopls の設定: `nvim/lua/dotfiles/lsp.lua` / `nvim/ftplugin/go.lua` / `_claude/settings.json`
- `tests/scripts/test_tuikit_consumers_aligned.sh` (GOWORK=off の既存の使用)
- `src/README.md`

## 進捗

- 2026-10-08: 起票
- 2026-10-08: 反証レビュー (Claude のサブエージェント 1 本。codex は 5h 枠切れ) を反映。比較の式がそのままでは必ず差を出す (P1)、GOWORK の既存使用と版ずれの検出が弱まる点・go.work.sum・go_autobuild の指紋の外・gopls の設定 3 か所 (P2)。反証できなかった主張: module 17 / replace 8 / go が親へ go.work を探すこと。gopls の `build.env` で GOWORK が効くかは未確認
