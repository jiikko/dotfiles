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
- [x] 2. rest の並列度を 2 倍 (同じ commit)
- [ ] 3. CI の rest の所要を比べる
- [ ] 4. 数 run で不安定にならないかを見る
