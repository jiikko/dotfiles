# 892 (chore): dupscan のプロジェクトの骨組み (src/dupscan・bin・CI・README・PACKAGES.md)

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-24〜）

起票日: 2026-10-24

## 概要

epic 890 の土台。`src/` の新しいプロジェクトの手順に沿って、中身が空でも lint とテストが CI で回る状態を作る。要件は [891 (要件の正本)](891-design-dupscan-requirements.md)。

着手は [901 (既存の道具の調査)](901-research-dupscan-existing-tools.md) の結論を待つ (既存の道具で足りる部分が大きければ、骨組みの形が変わる)。

## 受け入れ条件

- [ ] dotfiles を持たない repo の CI でも、版を固定して入れる手順がある (`go install <module>@<版>` か、ビルド済みの実行ファイル)。cgo なので C のコンパイラの要否も書く。DualNoteApp の CI (GitHub Actions) を例に手順を確かめる

- [x] `src/` の新しいプロジェクトの手順 (`src/README.md`「新規プロジェクトのガイドライン」) を満たす:
  - `src/dupscan/Makefile` の `lint:` (`../../scripts/go_module_graph.sh` を呼ぶ) / `test:`、`go.sum`、`src/dupscan/README.md`、`src/README.md` の一覧に 1 行
  - `.github/workflows/src_dupscan.yml` (`_go-project.yml` を呼ぶ。paths に `src/dupscan/**` と共有の script / 設定、replace で取り込む共有 module の dir)
  - `PACKAGES.md` の再生成と全 package の doc comment (`scripts/go_packages_index.sh --check`)
  - `bin/dupscan` を zsh で書くなら root の `Makefile` の `ZSH_SYNTAX_FILES` に登録。go_autobuild の成果物を `src/dupscan/.gitignore` に
  - `make test-lint` (`test-go-project-lanes` ほか) が通る
- [x] cgo で巨大な `parser.c` をコンパイルするので、CI の lint (`_go-project.yml`。timeout 25 分) と test のコールドの所要を実測して書く

## 進捗

- 2026-10-24 起票 (epic 890 の分解)
- 2026-10-24 901 の結論を待つことにした
- 2026-10-24 他の repo の CI での入れ方 (版の固定) を受け入れ条件に足した (反証レビュー: 汎用化には入手の手段が要る)
- 2026-10-24 骨組みを作った (commit「feat(dupscan): Swift の重複したメソッドを構文木で組にする CLI の第 1 段」): Makefile・go.mod / go.sum・README・`src/README.md` の一覧・
  `.github/workflows/src_dupscan.yml`・`PACKAGES.md`・`bin/dupscan` (zsh。`ZSH_SYNTAX_FILES` に登録)・`.gitignore`。`make -C src/dupscan lint` 0 issues・`make test-changed`・`make test-lint` rc=0
  - 文法の module のテストが誤った import path を持ち `go mod tidy` が止まるので、`src/dupscan/third_party/tree-sitter-swift-teststub` に空の代わりを置いて replace した。
    `scripts/go_packages_index.sh` は入れ子の module を落とすので、module の直下の `third_party/` だけを外す例外を足した (テスト `tests/scripts/test_go_packages_index.sh` に、直下は通し深い場所は落とす 2 件を足し、変更前の版と最初の誤った版でそれぞれ赤を確認)
  - 手元の `make lint` (温まった状態) は 10 秒。**残り**: CI のコールドの所要 (cgo で parser.c 23 MB をコンパイル) の実測・他の repo の CI での入れ方
- 2026-10-24 3 周目の敵対的レビュー: `scripts/go_packages_index.sh` の replace の先の判定で、CRLF の go.mod を読み、比べる値を環境変数で渡す (`-v` は `\` を解釈した)。テスト 2 件は直す前で赤
- 2026-10-24 CI: src/dupscan の lint 52 秒・test 35 秒 (cgo のコールドビルドを含む。run 40102531110)。別の workflow の unused (production から届かない symbol の検査) が、網羅テストだけが使う structKinds を赤にした。種類の分類を classify に寄せて変換も通すように直した。手元の make test-changed / test-lint はこの検査を含まないので、新しい Go の module では `make test-unused-excluding-tests` も回す / go_packages_index.sh は引用符付きの replace も読む
- 2026-10-25 CI の所要を実測した (`gh run list --workflow src_dupscan.yml` の 27 回): 最初の run (dupscan の go.sum に対する build cache が無いコールド) は lint の step 30 秒・test の step 22 秒。
  それ以降は lint 3〜6 秒・test 20〜51 秒 (macOS の runner)。`_go-project.yml` の timeout 25 分に対して十分短い。手元で空の GOCACHE から `go build` するのは 8 秒 (go1.26.0・darwin/arm64)。
  dotfiles を持たない repo の CI で入れる手順は `src/dupscan/README.md` の組み込みの例 (固定した commit の dotfiles から `src/dupscan`・`src/subproc` を取り出して build。
  module の path が `dupscan` なので `go install <module>@<版>` の形は取れない)。C のコンパイラは要る (GitHub の macOS / Ubuntu の runner には入っている)。
  **残り**: その手順を実際の repo の CI で 1 回走らせて確かめる (DualNoteApp は読み取りのみなので使えない。obaket で行う = 909)
