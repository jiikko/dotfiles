# 1103 (bug): tests/githooks/test_pre_push.sh が赤い (test_issue_links_awk が pre-push の一覧に無い)

起票日: 2026-11-21

## 概要

root の `make test` と CI の Tests (rest) で、`tests/githooks/test_pre_push.sh` だけが落ち続けている。

```
✗ hook の一覧と tests/issues/ が食い違う (新しい検査を CHECKS か EXCLUDED に足す):
3a4
> test_issue_links_awk
```

- このテストは、`githooks/pre-push` の `CHECKS` と `EXCLUDED` の和が `tests/issues/test_*.sh` の実在と一致することを見る (テストの「0.」の段)
- commit「refactor(issues): issue_done.sh のリンクの awk を scripts/lib に分け、obaket と共有する」(2026-11-20) が `tests/issues/test_issue_links_awk.sh` を足したが、pre-push のどちらの一覧にも載せなかった
- 実測: 2026-11-20〜21 の root の `make test` (worktree の 4 回) と CI の run 44755382986 (e6292ba33) で、落ちたテストはこの 1 件だけだった

## なぜ困るか

root の `make test` が赤いままだと、新しい赤が出ても見分けにくい。issue 1077 の作業でも、毎回「既知の 1 件だけ」と数え直していた。

## 対応方針

`test_issue_links_awk` は道具自身の self-test (awk の 2 本を固定の入力に通す。repo の issue の状態を見ない) なので、`githooks/pre-push` の `EXCLUDED` に足す。`test_issue_done` と同じ扱いになる。
足した後に、`tests/githooks/test_pre_push.sh` と `make test` が緑になることを確かめる。

## 関連ファイル

- `githooks/pre-push` (CHECKS・EXCLUDED)
- `tests/githooks/test_pre_push.sh`
- `tests/issues/test_issue_links_awk.sh`
