# 787 (chore): go.mod の replace の抜けを手元でも止める (`go mod tidy -diff` / `go list -m all` を手元の lint に入れる)

起票日: 2026-10-16

## 概要

依存先の module が新しく別の module を require したとき、依存元の go.mod にその replace が無いと `go list -m all` が
`malformed module path "<名前>": missing dot in first path element` で落ちる (replace は依存元へ伝わらない)。
この検査は CI の `_go-project.yml` の lint の job の step「Module graph (go mod tidy -diff / go list -m all)」にしか無く、
手元の `make -C src/<proj> lint`・root の `make test` / `make test-src` は通る (`go build` / `go test` / golangci-lint はこの抜けで落ちない)。

## 詳細 (同じ形が 2 回)

- issue 736 (done): pro-con に `replace treefiler => ../treefiler` が無かった (glogx が treefiler を require)。このとき CI に step を足したが、
  「手元の make lint には入っていない (CI だけ)」と書いたまま、入れなかった理由は書かれていない
- 2026-10-16: 22d95786 (issue 747) で glogx が mac-lan-manager を require し、pro-con の `src/pro-con` の CI が 5 回連続で赤
  (22d95786 / f06d6a90 / 05e4c06f / 6c41bbdd / c138da32)。1d8eb4be で replace を足して直した。手元の `make test` は rc=0 のまま push されていた
- 加えて、replace を足すと `scripts/check_go_project_lanes.sh` (`make test-lint` の `test-go-project-lanes`) が「workflow の paths に無い」で落ちる。
  1d8eb4be はこれも手元で回さずに push し、Lint を赤くした (f1b98036 で paths を足した)。replace を足す手順と paths を足す手順が別々の場所にある

## 実測 (2026-10-16、開発機・go1.26.0)

- 23 module すべてで `go mod tidy -diff && go list -m all` を順に回して **10 秒** (全部 rc=0。module のキャッシュが温まった状態。1 サンプル)

## 対応方針 (案)

- 置き場所の候補:
  - A. 各 `src/*/Makefile` の `lint` に足す: 23 ファイルに同じ 2 行を写すことになる。写すなら共通のスクリプト (`scripts/go_module_graph.sh` 等) を呼ぶ 1 行にする
  - B. root の `test-go-lint` (`run_go_projects`) に足す: `make test-src` からは入るが、`make test` は `test-go` (test のみ) なので入らない
  - C. `make test-lint` に `scripts/check_*.sh` として足す: `make test` から必ず入る。ただし test-lint は Go を持たない CI の job (lint.yml) でも走るので、Go が無いときの扱いを決める
- CI の step と同じ入口を使う (CI は `run:` に 2 行を直書きしている。手元と CI で 2 通りの書き方にしない)
- 落ちたときの文言に「依存元の go.mod に `replace <名前> => ../<名前>` を足し、`.github/workflows/src_<proj>.yml` の paths にも足す」を出す
  (`check_go_project_lanes.sh` の文言と揃える)

## 関連ファイル

- `.github/workflows/_go-project.yml` (step「Module graph」) / `src/*/Makefile` (`lint`) / `Makefile` (`test-go-lint` / `run_go_projects` / `test-lint`)
- `scripts/check_go_project_lanes.sh` / `scripts/golangci_lint.sh`
- issue 736 (done)

## 進捗

- [ ] 反証レビュー
- [ ] 置き場所を決める
- [ ] 実装・変異 (replace を 1 行消して手元の入口が落ちることを見る)
