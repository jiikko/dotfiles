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

- [x] `make test` の赤で、落ちたテストが記録に 1 行ずつ残る (緑の run では何も書かない)
- [x] 記録をテスト名ごとに数えて出せる
- [x] 入口のドキュメント (docs / 該当 skill) にその見方を書く (tests/CLAUDE.md の「負荷でだけ落ちるテストは、帰属を確かめる前に記録を数える」)

## 結果 (2026-10-29)

commit「test: make test の赤で落ちたテストを記録し、テスト名ごとに数える」。

- 記録先: `${XDG_CACHE_HOME:-~/.cache}/dotfiles-test-fails/log.tsv` (TSV: 日時・commit・load average・種類・名前)。書き手は
  `scripts/test_fail_ledger.sh record`、数えるのは `scripts/test_fail_ledger.sh top [N]` (回数順。名前を単語として書いた issue の番号を添える)
- どこで書くか (対応方針で保留した点): runner ごとに 1 回 (Makefile の `record_failed_tests`)。直列 (`run_tests`)・並列
  (`run_tests_parallel`)・`test-bats`・go (`run_go_projects` の test のときだけ。名前は `go-names` が `go test` の出力から
  `src/<module> <package> Test名` で取る)。全腕合計では書かない (単独の target の失敗が漏れ、2 重にもなる)
- 書かないもの: 緑の run・CI・中断 (TERM / HUP / INT) した run・並列 runner が結果を報告しなかったテスト・lint の赤
- 偽の失敗を本物の記録へ入れない: runner は各テストへ `TEST_CHILD_ENV` (記録先・失敗と skip の集計先を空) を渡す。
  runner の外からテストを起こす `bin/mutate-verify` の検証コマンド・`make test-assert-reaches-exit` の probe・
  `tests/scripts/test_no_failfast_entrypoints.sh` も記録先を空にする。runner の外の集計先の漏れ (テストの中の make が外側の
  全腕合計に偽の失敗を足していた) も同じ変数で塞いだ
- 実測: 2026-10-29 の 2 回の `make test` で、本物の赤 `tests/claude/test_dangling_symlinks.sh` (手元の `~/.claude/agents/` の
  dangling link) が load 40.00 / 101.59 で 1 行ずつ残り、わざと落とすテストの偽の失敗は 0 行
- テスト: `tests/scripts/test_test_fail_ledger.sh` (28 項目)・`tests/bin/test_mutate_verify.sh` のケース 52。
  変異 19 本で red を確認 (runner の子へ空で渡す 4 か所・sh / go / bats の記録の呼び出し・go の subtest と package の扱い・
  CI と緑と空の記録先で書かない・正規化・`grep -w`・テスト名の無い行で issue を引かない・報告しなかったテスト・中断の印)
- 敵対的レビュー 3 周 (opus)。採った指摘は上の形で直した。4 周目 (中断の印) は回していない。直した中断のテストは、
  外側の runner の中に入れ子で走らせて、外側へ TERM を送らないことを確かめた (1 周目の版は `make test` の中で外側の runner を止めていた)

### 直さなかった指摘 (理由)

- `make test-changed` の Go の経路は記録しない: `scripts/test_changed.sh` が module の make を直接叩き runner を通らない。必要になったら
  そこから `go-names` と `record` を呼ぶ
- lint・runner 自体の故障は記録しない: 負荷で落ちるテストを数える目的から外れる
- issue 番号は名前の最後の部分 (ファイル名・Test 名) で引くので、同じ名前の別のテストを書いた issue にも付く (手がかりとして出す)
- go の出力が `FAIL\t<pkg>` 行の前で切れた (go test が外から殺された) ときは、名前が package 無しで残り、普通の行と別に数えられる
- runner を通さずテストを直接走らせると記録先は空にならない (tests/CLAUDE.md が直接実行を禁じている)
- 未確認: build の失敗が import する package の数だけ「(テスト名なし)」を作るか / 大量の行の追記が 1 回の write に収まるか

## 関連

- 872 (切り出し元)・869 (重い検査の直列化)・422 (hook の大きな入力の flaky)・864 の A9・871 (treefiler の負荷でだけ落ちるテスト)。
  負荷でだけ落ちるテストごとの issue が 3 本 (422・864 の A9・871) あり、記録が issue ごとに散っている
- 2026-10-23: 871 で解消 (原因は 2 つ: 光の粒の速さが前の走りから残る製品側の不具合と、2 つの Model で裏の走査の結果が届くフレームが
  ずれるテストの組み立て)。負荷の下の揺れは `GOMAXPROCS=1` で再現できた (直す前 960 回中 10 回赤 → 直した後 0 回)。他の揺れにも使える再現の手
