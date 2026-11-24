# 1107 (perf): CI の Tests の rest の job が上限 20 分に近い

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-11-24〜）

起票日: 2026-11-24

## 概要

`.github/workflows/tests.yml` の `dotfiles-tests (rest)` が 11〜17 分かかり、`timeout-minutes: 20` に対して余裕が 3 分ほどしかない。遅くなっただけで落ちる。

## 実測 (2026-11-24)

- rest の job の所要 (成功した直近 13 run、`gh run view <id> --json jobs` の startedAt〜completedAt): 11.2〜16.7 分
- run 45177850917 (87527f420) の rest の内訳: suite の step が 887 秒、そのほかの step (checkout・toolchain・nvim・Go) が計 66 秒。
  suite の中は並列腕 (143 件) が約 10.5 分 (22:57:58〜23:08:28)、直列腕 (tests/tmux など) が約 3.5 分
- 手元 (14 コア) で `make test-discovered-rest NPROC=3` を回したときの並列腕のテストごとの所要: 143 件の合計 790 秒。
  突出したものは無く、上位 10 件 (`tests/devtool/test_mutate_verify.sh` 59 秒、`tests/githooks/test_pre_push.sh` 41 秒、…) を足しても 4 割
- 合計 790 秒を並列度 3 で割ると 4.4 分になるが、CI の並列腕は 10.5 分かかっている。
  tests/CLAUDE.md の実測では、rest のテストは大半が待ち (real に対する user+sys が 9〜43%) なので、コア数と同じ並列度では CPU が遊んでいると見ている (CI の runner のコア数は未確認。次の run のログの `並列度` で読む)

## 対応方針

1. runner (Makefile の `run_tests` / `run_tests_parallel`) がテストごとの所要を `[ok] <名前> (Ns)` で出し、腕ごとに長い順の上位 10 件を `[時間]` で出す。CI のログだけでどこが伸びたかを読めるようにする
2. CI の rest だけ並列度をコア数の 2 倍にする (`tests.yml` の matrix の `jobs-per-cpu`)。heavy (av1ify / concat) は CPU を使い切るので 1 のまま
3. 2 の後の rest の所要を前と比べる。縮まなければ 2 を戻し、直列腕を並列腕と同時に走らせる案 (`scripts/run_make_targets_parallel.sh` の冒頭が「安全かは未検証」としている) か job の分割を検討する
4. 負荷を上げたことで、待ちの上限を持つテストが不安定にならないかを数 run 見る

`timeout-minutes` を上げる案は採らない: 上限は hang を止めるためのもので、上げると伸びたことに気づくのが遅れる。

## 進捗

- [x] 1. runner の所要の出力 (commit「perf(ci): rest の並列度をコア数の 2 倍にし、テストごとの所要を出す」)
- [x] 2. rest の並列度を 2 倍 (同じ commit) → **効かなかったので戻した** (commit「perf(ci): rest の並列度を戻す」)
- [x] 3. CI の rest の所要を比べた (run 45291097303、54599e579)
  - runner のコア数は 3 (ログの `並列度 6` = 3 × 2)
  - 並列腕は 23:44 台〜23:55:17 の約 11 分で、並列度 3 のとき (約 10.5 分) から縮まなかった。job 全体は 15.9 分
  - 時間の上限を持つテスト 3 本が負荷で落ちた: `tests/claude/test_deny_bare_tmux_kill.sh` (hook の 10 秒の上限)、
    `tests/claude/test_deny_piped_push_then_destroy.sh` (`runtimeout` の rc=124)、`tests/claude/test_ja_textlint_on_edit.sh` (知らせが出る前に打ち切り)
  - 結論: CI の rest は待ちではなく CPU で縛られている。tests/CLAUDE.md の「大半が待ち」は 14 コア機の実測で、CI には当てはまらない (tests/CLAUDE.md に追記した)
- [x] 4. 不安定になった (上の 3 本) ので、並列度を上げる方向は採らない
- [ ] 5. 並列度 3 に戻した run で、CI のテストごとの所要 (`[時間]`) を読む
- [ ] 6. 5 を見て、CPU を食う上位のテストを軽くするか、rest を 2 つの job に分けるか (runner を 1 台増やす。repo は public なので分単位の課金は無い) を決める
