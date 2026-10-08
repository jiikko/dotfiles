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
- `GOWORK` を設定している箇所は今 0 件 (go_autobuild・Makefile・workflow を grep。2026-10-08)

## 対応方針 (案。実装の前に決める)

| 案 | 中身 | 欠点 |
|---|---|---|
| A. `src/go.work` を commit し、ビルド・テストの全経路で `GOWORK=off` | エディタは自動で拾う | 経路を 1 つでも漏らすと workspace mode で黙ってビルドされる。漏れを止める検査が要る |
| B. go.work を `src/` の外 (例: `nvim/` 側や `~/.cache`) に置き、gopls にだけ `GOWORK` を渡す (gopls の `build.env`) | ビルドの経路は何も変わらない | エディタ以外 (LLM の LSP ツール・手で打つ `go`) には効かない。gopls の起動経路ごとに設定が要る |
| C. go.work を生成物にして gitignore し、使うときに作る | commit されたファイルがビルドを変えない | 手元に作った時点で A と同じリスクが出る |

どれを選ぶにしても:

- 採用前に、全 module で `go list -deps -f '{{.ImportPath}} {{.Module}}' ./...` (と `go build ./...`) の結果を go.work の有無で比べ、**差が 0** であることを確かめる
- 実際に module をまたいで引けることを確かめる: gopls の workspace シンボル検索で、`src/glogx` を開いた状態から `src/tuikit/termwidth` のシンボルが出る
- A を選ぶなら、`GOWORK=off` が抜けた経路を機械で検出する (CI で go.work がある状態のビルドと差が出ないことを見る、など)。
  手で書いた一覧で経路を数えない

## 受け入れ条件

- [ ] 案を決め、理由を本 issue に書く
- [ ] go.work の有無で、全 module の依存の解決結果 (`go list -deps`) に差が無いことを確認した (コマンドと出力の要約を本 issue に書く)
- [ ] `make test-go` と CI の `src_*.yml` が今までどおり通る
- [ ] gopls で module をまたいだシンボル検索が効くことを確認した
- [ ] 使い方 (go.work がどこにあり、何に効き、ビルドには効かないこと) を `src/README.md` に書く

## 関連ファイル

- `src/*/go.mod` / `bin/lib/go_autobuild.zsh` / `Makefile` (`test-go`) / `.github/workflows/src_*.yml`
- `nvim/lua/dotfiles/lsp.lua` (gopls の設定)
- `src/README.md`

## 進捗

- 2026-10-08: 起票
