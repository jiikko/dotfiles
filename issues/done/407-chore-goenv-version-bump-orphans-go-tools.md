# goenv の版を上げると go 製ツールが使えなくなるが、**実際に困るのは 1〜2 本だけ**だった

起票日: 2026-09-20
カテゴリ: chore / priority: low
対象: `~/go/<version>/bin` の go 製ツール群 / `scripts/check_unused_excluding_tests.sh` の案内
出典: [405](405-bug-staticcheck-guard-passes-through-goenv-shim.md) の残る問題

## 事実

goenv は **GOPATH を版ごとに分ける** (`~/go/<version>`)。`go install` したバイナリはその版の
`bin` に入るので、global を 1.25.4 → 1.26.0 へ上げると**旧版の bin にある 17 本が一斉に
PATH から外れる** (ファイルは消えていない。`~/go/1.25.4/bin` に残っている)。
shim は残るので `command -v` は成功し続ける (405 のバグの原因)。

## 🚨 起票時の前提が 2 つ誤っていた (2026-09-20 に訂正)

1. **「`gopls` / `dlv` / `goimports` が消えてエディタが困る」は誤り**。
   `gopls` と `goimports` は **mason が入れる** (`nvim/lua/dotfiles/lsp.lua` の `server_packages` と
   `_nviminit.lua` の `mason-tool-installer` の `ensure_installed`)。mason bin は
   `nvim-lspconfig` の `BufReadPre` で PATH 先頭へ prepend されるので、**goenv 側が空でも
   nvim は自分が入れた方を使う**
2. **「`global_go_version` の導線」は存在しない**。ファイル (`./global_go_version` = `1.26.0`) は
   実在するが、**参照しているのは issue の本文だけ** (`.sh` / `.zsh` / `Makefile` / `.lua` / `.yml`
   からの参照は 0 件)。「版を上げる導線に入れ直す処理を足す」は**存在しない場所を指していた**

## 使用実績の全数勘定 (2026-09-20)

17 本それぞれを repo 全体で参照検索し、誤検出 (散文の `impl` / `motion` 等) を目視で除いた結果:

| ツール | 実際の参照 | 持ち主 | 判定 |
|---|---|---|---|
| `staticcheck` | `scripts/check_unused_excluding_tests.sh` / CI | **持ち主なし** (手動 `go install`。CI だけ `v0.7.0` を pin) | **入れ直した** (405 で現行版へ) |
| `goimports` | `_nviminit.lua` の conform (Go の保存時整形) | **mason** (`ensure_installed`) | **入れ直した** (mason 経由。下記) |
| `gopls` | `nvim/lua/dotfiles/lsp.lua` | **mason** | 対応不要 (mason にあった) |
| `golangci-lint` | 各 module の Makefile | **`go run …@v2.5.0`** (都度取る) | 対応不要 (版上げの影響を受けない) |
| `errcheck` | `.golangci.yml` に**名前**が出るだけ | golangci-lint が内蔵 | 対応不要 (単体のバイナリは不要) |
| 残り 12 本 (`asmfmt` / `benchstat` / `deadcode` / `dlv` / `fillstruct` / `godef` / `gomodifytags` / `gotags` / `iferr` / `impl` / `motion` / `revive`) | **0 件** | — | **入れ直さない** |

## やったこと

- `goimports` を **mason で** 入れ直した (`nvim --headless "+Lazy! load mason.nvim mason-tool-installer.nvim" "+MasonToolsInstallSync"`)。
  🚨 `go install` では入れない — 宣言された持ち主は mason なので、素の `go install` で入れると
  **同じツールに持ち主が 2 つ**できる (mason bin が PATH 先頭なので、goenv 側は使われないまま残る)
  - ついでに同じ `ensure_installed` にあった `prettierd` / `shellcheck` / `shfmt` も入った
    (どれも mason bin には無かった = **mason-tool-installer が一度も走っていなかった**)
- 残り 12 本は**入れ直さない**。参照 0 件で、必要になった人がその場で `go install` すれば足りる

## 残る本当の問題 (優先度 low)

**`staticcheck` だけが持ち主のいない `go install` に依存している**。版を上げると消えるが、
405 のガードが「実行できない」を検出して**入れるコマンドを版つきで出す**ようになったので、
黙って壊れることはなくなった。これ以上の自動化 (setup.sh へ一覧を置く / `go run` の pin へ寄せる) は、
**対象が 1 本では割に合わない**と判断して見送る。

再開の trigger: 持ち主のいない go 製ツールが 2 本目を超えたとき。

## 残タスク

- [x] 使用実績の全数勘定 (上表)
- [x] 困るものだけ入れ直した (`goimports` を mason 経由で。`staticcheck` は 405 で対応済み)
- [x] 起票時の前提の誤り 2 件を訂正した
- [x] **受容 (ユーザー判断 2026-09-20)**: mason-tool-installer が普段の nvim 起動で走るかは
      **確認しない**。理由は「**足りなければ実行時にエラーで分かる**」から (整形が効かない /
      LSP が起動しない、はすぐ目に見える)。
      🚨 観測としての限界を記録しておく: 今回 4 本を入れた**後**では「走ったが入れるものが
      無かった」と「走っていない」を**区別できない** (有無で結果が変わらない観測)。
      判別するには pty を与えて `VeryLazy` を発火させる必要があるが、**そこまでのコストは
      掛けないと決めた**。再開の trigger: `ensure_installed` に足したツールが入らない、と
      気づいたとき (そのときは「一度も走っていない」が確定する)
