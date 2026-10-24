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
- [ ] cgo で巨大な `parser.c` をコンパイルするので、CI の lint (`_go-project.yml`。timeout 25 分) と test のコールドの所要を実測して書く

## 進捗

- 2026-10-24 起票 (epic 890 の分解)
- 2026-10-24 901 の結論を待つことにした
- 2026-10-24 他の repo の CI での入れ方 (版の固定) を受け入れ条件に足した (反証レビュー: 汎用化には入手の手段が要る)
- 2026-10-24 骨組みを作った (commit「feat(dupscan): Swift の重複したメソッドを構文木で組にする CLI の第 1 段」): Makefile・go.mod / go.sum・README・`src/README.md` の一覧・
  `.github/workflows/src_dupscan.yml`・`PACKAGES.md`・`bin/dupscan` (zsh。`ZSH_SYNTAX_FILES` に登録)・`.gitignore`。`make -C src/dupscan lint` 0 issues・`make test-changed`・`make test-lint` rc=0
  - 文法の module のテストが誤った import path を持ち `go mod tidy` が止まるので、`src/dupscan/third_party/tree-sitter-swift-teststub` に空の代わりを置いて replace した。
    `scripts/go_packages_index.sh` は入れ子の module を落とすので、module の直下の `third_party/` だけを外す例外を足した (テスト `tests/scripts/test_go_packages_index.sh` に、直下は通し深い場所は落とす 2 件を足し、変更前の版と最初の誤った版でそれぞれ赤を確認)
  - 手元の `make lint` (温まった状態) は 10 秒。**残り**: CI のコールドの所要 (cgo で parser.c 23 MB をコンパイル) の実測・他の repo の CI での入れ方
