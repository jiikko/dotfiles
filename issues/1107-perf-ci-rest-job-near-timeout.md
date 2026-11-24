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
- [x] 2. rest の並列度を 2 倍 (同じ commit) → 並列腕が縮まなかったので戻した (commit「perf(ci): rest の並列度を戻す」)
- [x] 3. CI の rest の所要を比べた (run 45291097303、54599e579)
  - runner のコア数は 3 (ログの `並列度 6` = 3 × 2)
  - 並列腕は 23:44 台〜23:55:17 の約 11 分で、並列度 3 のとき (約 10.5 分) から縮まなかった。job 全体は 15.9 分
  - 時間の上限を持つテスト 3 本が負荷で落ちた: `tests/claude/test_deny_bare_tmux_kill.sh` (hook の 10 秒の上限)、
    `tests/claude/test_deny_piped_push_then_destroy.sh` (`runtimeout` の rc=124)、`tests/claude/test_ja_textlint_on_edit.sh` (知らせが出る前に打ち切り)
  - 結論: CI の rest は待ちではなく CPU で縛られている。tests/CLAUDE.md の「大半が待ち」は 14 コア機の実測で、CI には当てはまらない (tests/CLAUDE.md に追記した)
- [x] 4. 不安定になった (上の 3 本) ので、並列度を上げる方向は採らない
- [x] 5. 並列度 3 に戻した run (45293693619、273179deb) の CI のテストごとの所要を読んだ
  - job 全体 13.1 分。suite の中は並列腕 8.5 分 (00:02:55〜00:11:27)、直列腕 3.2 分
  - 並列腕の 143 件の所要の合計は 1459 秒。3 で割ると 8.1 分で wall と合う (3 本の枠が埋まり続けている)。手元の同じ並列度の合計 790 秒の 1.85 倍
  - 上位 10 件で 696 秒 (合計の 48%): `tests/devtool/test_mutate_verify.sh` 106、`tests/githooks/test_pre_push.sh` 98、
    `tests/scripts/test_no_failfast_entrypoints.sh` 95、`tests/scripts/test_test_fail_ledger.sh` 72、`tests/bin/test_ja_textlint.sh` 66、
    `tests/bin/test_go_autobuild_warmup.sh` 66、`tests/claude/test_suggest_dupscan_for_symbol_grep.sh` 57、`tests/bin/test_dupscan_no_build.sh` 48、
    `tests/issues/test_issue_done.sh` 44、`tests/claude/test_ja_textlint_on_edit.sh` 44
  - 直列腕の上位は `tests/tmux/test_server_watchdog.sh` 41、`tests/tmux/test_schedule_keys.sh` 31、`tests/tmux/test_socket_cleanup.sh` 25
  - job の所要はテストの中身がほぼ同じ commit の間でも 11〜17 分を揺れる。1 本の run の値で効果を言わない
- [x] 6. 5 を見て決めた: ユーザーの判断 (2026-11-24) で rest を 2 つの job に分ける (commit「perf(ci): rest の並列腕を TEST_SHARD で 2 job に分ける」)
  - Makefile の `TEST_SHARD=k/n`: 並列の runner が、発見したテストを `LC_ALL=C` の名前の順で交互に n 組へ振り、k 組目だけを走らせる。何も渡さなければ全部 (手元の `make test` は変わらない)
  - tests.yml: rest = `test-runtime-rest TEST_SHARD=1/2` (構文・並列腕の半分・直列腕・bats)、rest2 = `test-discovered-rest-parallel TEST_SHARD=2/2`。依存は新しい `deps-group` で選ぶ (rest2 も rest と同じ)
  - `scripts/check_ci_group_deps.sh` の 4: 組が rest の並列腕の make-target に字で書かれ、1/n〜n/n を 1 回ずつ持ち、test-runtime-rest がちょうど 1 つであること
  - runner がテストへ `MAKEFLAGS`・`MFLAGS`・`MAKELEVEL`・`TEST_SHARD` を空で渡す (コマンド行の値が `MAKEFLAGS` に乗ってテストの中の make に届き、`tests/scripts/test_no_failfast_entrypoints.sh` の偽の suite まで組で割っていた。`MAKEFLAGS=' -- TEST_SHARD=1/2'` で再現し、直した後は緑)
  - 実装をわざと壊してテストが落ちることを 12 か所で確かめた (組の振り分けのずれ・振り分けを呼ばない・組が空の断り・コメント行の除外・番号の照合・掛けた target・字以外の書き方・重複を消す・行末のコメント・子への MAKEFLAGS の漏れ・test-runtime-rest の数・行頭の固定)
  - `make test`: 赤は `tests/claude/test_dangling_symlinks.sh` だけ (`~/.claude/skills` に残った古い link。この変更と無関係)
- [ ] 7. CI で rest と rest2 の所要を確かめる (目安: どちらも 10 分を切る)

## 反証レビュー (opus 3 周) の採否

- 採った: 組を数えるだけで掛かる target を見ない / matrix の変数・env: の書き方を読まない / 重なりと理由の文言をテストが見ない / 組の中身を照合しない / `MAKEFLAGS` 経由でテストへ漏れる (P1。実行で再現) / 両方を並列腕だけにすると直列腕が消える / `make-target:` の行頭が固定されていない / workflow のパスにディレクトリ / `MFLAGS`・環境変数の `TEST_SHARD` の漏れ
- 記録のみ:
  - `TEST_SHARD` の値をシェルへ素の字で埋め込む (渡すのは開発者と workflow だけ)
  - `TEST_SHARD` が下位の make へ届くことを本番の入口 (`test-runtime-rest`) で通すテストが無い (CI のログの `[並列]` の行の「組 1/2」で毎回読める)
  - 組の無い rest の entry を組と並べても通す (2 回走るだけで、走らないテストは出ない)
  - 検査 4 が見ない形: matrix の `exclude:`・job の `if:`・組の entry の依存の宣言 (`deps-group` など) の食い違い。引用符や flow 形式の make-target は落ちる側 (うるさい赤)
- 確かめて影響なし: job 名の変更 (`dotfiles-tests (rest2)` が増える) は、master に branch protection も ruleset も無いので必須チェックに響かない (`gh api .../branches/master/protection` が 404、rulesets が空)
