# 969 (ux): test_fail_ledger.sh top の issue 番号が多すぎて手がかりにならない

起票日: 2026-10-29

## 概要

`scripts/test_fail_ledger.sh top` は、落ちたテストの名前 (ファイル名・Test 名) を単語として書いた issue の番号を全部添える。
done の issue も同じ扱いで並ぶので、よく話題になるテストほど番号が増え、今も効く issue (waiting で再発を待つものなど) が埋もれる。

## 詳細 (2026-10-29 の実測)

`grep -rlFw --include='*.md' <名前> issues` の本数:

| 名前 | 全体 | done 以外 | done 以外の issue |
|---|---|---|---|
| `test_dangling_symlinks.sh` | 20 | 1 | 941 (epic 932) |
| `test_deny_bare_tmux_kill.sh` | 13 | 2 | 422 (waiting)・261 (pending) |
| `TestRenderPathologicalParagraphsAreLinear` | 2 | 0 | — |
| `TestScannerFindsPeersAndClosesAll` | 2 | 1 | 864 (waiting) |

`top` の 1 行に 20 本並ぶと、873 の目的 (既知の waiting issue へのリンクを並べる) の 422 のような番号が読めない。

ほかの絞り方も数えた。同じ行に「落ち・flaky・赤・FAIL・timeout・揺れ」がある issue に絞ると `test_dangling_symlinks.sh` で 7 本、
パス全体 (`tests/claude/test_dangling_symlinks.sh`) で引くと 11 本。どちらも done が大半で、多すぎる問題は残る。

## 対応方針

- 番号を並べるのは done 以外 (直下・`next/`・`pending/`・`waiting/`・`epic/<name>/` とその状態ディレクトリ) の issue だけにする
- done の issue は本数だけ添える (`(issue 422,261 / done 11 本)`)。直った記録があること (871 のような) は分かるまま、行を短くする

## 受け入れ条件

- [x] `top` の行に並ぶ番号が done 以外の issue だけになり、done は本数で出る
- [x] テストで固定する (done 以外だけ・done だけ・両方)

## 結果 (2026-10-29)

commit「ux: test_fail_ledger.sh top の issue 番号を done 以外だけにし、done は本数で出す (issue 969)」。

- 実測: `top` の `tests/claude/test_dangling_symlinks.sh` の行は「issue 001,060,…,941 (20 本)」から「(issue 941,969 / done 19 本)」になった。
  969 はこの issue 自身 (本文に名前を書いたため)。done へ移ると done の本数に入る
- 状態は issues ディレクトリからの相対パスで決める (反証レビューの P2: checkout の置き場所のパスに `/done/` があると、全部を done と読んでいた)
- テスト: tests/scripts/test_test_fail_ledger.sh に「done 以外と done の両方」「done だけ」「issues を done/ の下へ置いても同じ行」を足した (29 項目)。
  変異 3 本で red を確認 (done を除かない / done の本数を出さない / 相対パスにしない)
- 反証レビュー 1 本 (sonnet)。表の本数と状態は反証されなかった。P3 (next/ の symlink は数えが 2 重にならない・番号の無い md は数えない) は
  確認だけで、テストには固定していない
- `make test`: shellcheck (直した) と、手元の `~/.claude/agents/` の dangling link で落ちる `tests/claude/test_dangling_symlinks.sh` 以外は緑
