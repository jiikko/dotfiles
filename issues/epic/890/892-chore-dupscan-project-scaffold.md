# 892 (chore): dupscan のプロジェクトの骨組み (src/dupscan・bin・CI・README・PACKAGES.md)

起票日: 2026-10-24

## 概要

epic 890 の土台。`src/` の新しいプロジェクトの手順に沿って、中身が空でも lint とテストが CI で回る状態を作る。要件は [891 (要件の正本)](891-design-dupscan-requirements.md)。

着手は [901 (既存の道具の調査)](901-research-dupscan-existing-tools.md) の結論を待つ (既存の道具で足りる部分が大きければ、骨組みの形が変わる)。

## 受け入れ条件

- [ ] `src/` の新しいプロジェクトの手順 (`src/README.md`「新規プロジェクトのガイドライン」) を満たす:
  - `src/dupscan/Makefile` の `lint:` (`../../scripts/go_module_graph.sh` を呼ぶ) / `test:`、`go.sum`、`src/dupscan/README.md`、`src/README.md` の一覧に 1 行
  - `.github/workflows/src_dupscan.yml` (`_go-project.yml` を呼ぶ。paths に `src/dupscan/**` と共有の script / 設定、replace で取り込む共有 module の dir)
  - `PACKAGES.md` の再生成と全 package の doc comment (`scripts/go_packages_index.sh --check`)
  - `bin/dupscan` を zsh で書くなら root の `Makefile` の `ZSH_SYNTAX_FILES` に登録。go_autobuild の成果物を `src/dupscan/.gitignore` に
  - `make test-lint` (`test-go-project-lanes` ほか) が通る
- [ ] cgo で巨大な `parser.c` をコンパイルするので、CI の lint (`_go-project.yml`。timeout 25 分) と test のコールドの所要を実測して書く

## 進捗

- 2026-10-24 起票 (epic 890 の分解)
- 2026-10-24 901 の結論を待つことにした
