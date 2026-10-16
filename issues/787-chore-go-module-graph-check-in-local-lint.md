# 787 (chore): go.mod の replace の抜けを手元でも止める (`go mod tidy -diff` / `go list -m all` を手元の lint に入れる)

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-16〜）

起票日: 2026-10-16

## 概要

依存先の module が新しく別の module を require したとき、依存元の go.mod にその replace が無いと `go list -m all` が
`malformed module path "<名前>": missing dot in first path element` で落ちる (replace は依存元へ伝わらない)。
この検査は CI の `_go-project.yml` の lint の job の step「Module graph (go mod tidy -diff / go list -m all)」にしか無く、
手元の `make test` は通る (`make test` は `test-src` 経由で各 src の `lint` (golangci-lint) と `test` まで回すが、どちらもこの抜けで落ちない。
実測: 2026-10-16 に replace の無い木 (c138da32 を rebase した worktree) で回した `make test` で `[go-lint] src/pro-con (rc=0)` / `[go-test] src/pro-con (rc=0)`)。

## 詳細 (同じ形が 2 回)

- issue 736 (done): pro-con に `replace treefiler => ../treefiler` が無かった (glogx が treefiler を require)。このとき CI に step を足したが、
  「手元の make lint には入っていない (CI だけ)」と書いたまま、入れなかった理由は書かれていない
- 2026-10-16: 22d95786 (issue 747) で glogx が mac-lan-manager を require し、pro-con の `src/pro-con` の CI が 5 回連続で赤
  (22d95786 / f06d6a90 / 05e4c06f / 6c41bbdd / c138da32)。1d8eb4be で replace を足して直した。手元の `make test` は rc=0 のまま push されていた
- 加えて、replace を足すと `scripts/check_go_project_lanes.sh` (`make test-lint` の `test-go-project-lanes`) が「workflow の paths に無い」で落ちる。
  1d8eb4be はこれも手元で回さずに push し、Lint を赤くした (run 38842511833。f1b98036 で paths を足した。f1b98036 の Lint は後続の push で取り消され、緑を見たのは 90e596af)。replace を足す手順と paths を足す手順が別々の場所にある

## 実測 (2026-10-16、開発機・go1.26.0)

- 23 module すべてで `go mod tidy -diff && go list -m all` を順に回して **10 秒** (全部 rc=0。module のキャッシュが温まった状態。1 サンプル)

## 対応方針 (案)

- 置き場所の候補 (`make test` = `test-lint` + `test-runtime` + `test-src`、`test-src` = `test-go-lint` (各 src の `lint`) + `test-unused-excluding-tests` + `test-go`):
  - A. 各 `src/*/Makefile` の `lint` から共通のスクリプト (`scripts/go_module_graph.sh` 等) を呼ぶ: `make test`・`make -C src/<proj> lint`・CI の Lint step の
    どれからも入る。CI の `_go-project.yml` の step「Module graph」を消して `make lint` 側を正本にできる (手元と CI が 1 つの入口になる。CI では失敗が Lint step の中で出る)
  - B. root の `test-go-lint` (`run_go_projects`) に足す: `make test` / `make test-src` からは入るが、`make -C src/<proj> lint` 単体と CI の Lint step からは入らない (CI の step は残る = 2 通り)
  - C. `make test-lint` に `scripts/check_*.sh` として足す: lint.yml (dotfiles-lint) は setup-go で Go を入れてから `make test-lint` を回す (Go を使う `test-go-packages-index` が既にある)。
    ただし lint.yml は `cache: false` なので、23 module の依存を冷えたキャッシュから解決する時間を測ってから決める (上の 10 秒は温まったキャッシュの値)
  - 却下: `githooks/pre-push` (issue の整合検査専用で、issues/ を触らない push では回さない。責務が違う)
  - 既存の仕組みは無い: `go list -m` を使うのは `tests/scripts/test_tuikit_consumers_aligned.sh` (版の照合) だけで、replace の抜けは見ない (2026-10-16 に scripts / bin / githooks / Makefile / src/*/Makefile / .github / tests を grep)
- 推奨は A (入口が 1 つになり、CI の step と手元が食い違わない)
- CI の step と同じ入口を使う (CI は `run:` に 2 行を直書きしている。手元と CI で 2 通りの書き方にしない)
- 落ちたときの文言に「依存元の go.mod に `replace <名前> => ../<名前>` を足し、`.github/workflows/src_<proj>.yml` の paths にも足す」を出す
  (`check_go_project_lanes.sh` の文言と揃える)

## 関連ファイル

- `.github/workflows/_go-project.yml` (step「Module graph」) / `src/*/Makefile` (`lint`) / `Makefile` (`test-go-lint` / `run_go_projects` / `test-lint`)
- `scripts/check_go_project_lanes.sh` / `scripts/golangci_lint.sh`
- issue 736 (done)

## 進捗

- [x] 反証レビュー (opus 1 本、2026-10-16): P1 2 件 (`make test` は `test-go-lint` まで回す / lint.yml は Go を持つ) を訂正、P2 (pre-push・既存の仕組み・CI の step を lint へ寄せる案) を案に足した、P3 (run ID) を訂正。反証できなかった: 5 回連続の赤とその step・module 数・issue 736 の引用
- [ ] 置き場所を決める
- [ ] 実装・変異 (replace を 1 行消して手元の入口が落ちることを見る)
