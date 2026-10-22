# 873 (test): 並列負荷でだけ落ちるテストを、どのテストが何回落ちたかで記録する入口

起票日: 2026-10-22

## 概要

`make test` が、並列のセッションの負荷 (load average 20〜40) で時間の上限に当たったテストで赤くなる。単独では通るので、そのたびに
「自分の変更と経路がつながらないか」を 1 本ずつ確かめ直している。どのテストが・いつ・どの負荷で落ちたかを残す入口が無く、
同じテストの 2 回目・3 回目でも帰属の確認を一からやり直す。872 の踏んだところ 4 から切り出した。

## 詳細 (2026-10-22 の 1 日に見たもの)

- 872 のセッション: `tests/claude/test_deny_bare_tmux_kill.sh` (90KB の入力で hook が 10 秒の timeout)・`test_deny_piped_push_then_destroy.sh`・
  `tests/claude/test_claude_mods.sh` の tool-elapsed (5 分ごとの toast のテストが 10 秒を超えた)・CI の `src/restartable` の
  `TestPTYRunnerDrainsLateOutputDuringTerminationWithInputBurst` (再実行で success)
- 861 / 864 のセッション (dotfiles-b9): 上の hook の 3 本が同じ形で落ちた (load average 37〜40。単独で rc=0)。`discover` の
  `TestScannerFindsPeersAndClosesAll` (864 の A9)。tuikit/markdown の `TestRenderPathologicalParagraphsAreLinear` (10 秒の上限。単独で 0.8 秒)
- `test_deny_bare_tmux_kill.sh` の大きな入力は、2026-09-24 から 422 (waiting) で再発を待っている。今日の 2 回 (872 と dotfiles-b9) は
  422 に記録されていない (dotfiles-b9 は 422 を引かずに単独の再実行で帰属を決めた。872 の側は未確認)

## 対応方針 (案。決めるのは着手の時)

- `make test` の失敗の要約を出すところで (root の `Makefile` に 3 か所ある: 並列の腕ごとの `✗ 失敗したテスト:` 2 か所と `✗ 全腕合計`。
  全腕合計だけで記録すると単独の target の失敗が漏れ、全部で記録すると重なる。どこで書くかを着手の時に決める)、落ちたテスト名・日時・commit・load average を
  1 行ずつ追記するログを 1 つ持つ (置き場は本体 checkout の外の `~/.cache/` の下。worktree を消しても残す)
- そのログをテスト名ごとに数えて出す 1 コマンド (`… --top` の形) と、既知の waiting issue (422 など) へのリンクを並べる
- 落ちたテストを単独で回し直す段を手順に足すかは別に決める (869 の serial-run は「重い検査を同時に走らせない」側で、こちらは「落ちた記録」側)

## 受け入れ条件

- [ ] `make test` の赤で、落ちたテストが記録に 1 行ずつ残る (緑の run では何も書かない)
- [ ] 記録をテスト名ごとに数えて出せる
- [ ] 入口のドキュメント (docs / 該当 skill) にその見方を書く

## 関連

- 872 (切り出し元)・869 (重い検査の直列化)・422 (hook の大きな入力の flaky)・864 の A9・871 (treefiler の負荷でだけ落ちるテスト)。
  負荷でだけ落ちるテストごとの issue が 3 本 (422・864 の A9・871) あり、記録が issue ごとに散っている
