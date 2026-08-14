# bench 予算チェッカのエラー文言が単位を常に ms と書く (KB/MB の metric が時間の回帰に見える)

起票日: 2026-08-14
種別: chore
優先度: **P3** (ゲートの判定は正しい。読み手を誤らせるだけ)

## 何が起きるか

`check_bench_budgets.sh` は単位を固定文字列で書いている。**同じ形が 2 箇所ある**
(121 行の警告経路と 123 行のエラー経路):

```sh
121:  printf '::warning::bench over budget (極端な混雑 run のため警告のみ): %s %sms > %sms (%s)\n' ...
123:  printf '::error::bench regression: %s %sms > budget %sms (%s)\n' ...
```

⚠️ 121 行 (`rel` metric が極端な混雑 run で警告のみに落ちる経路) を忘れないこと。
エラー側だけ直すと、そちらに KB/MB の誤表示が残る。

この repo の metric は**単位を名前が持つ**流儀 (`server_rss_mb` = MB / `startup_cpu_ms` =
CPU ms / 051 で追加した `*_alloc_kb` = KB)。そのため CI ログにこう出る:

```
::error::bench regression: status_view_frame_alloc_kb 50.363ms > budget 42.9ms
::error::bench regression: server_rss_mb 42.0ms > budget 30ms          (tmux の既存 metric)
```

**50.363 は KB、42.0 は MB** で、どちらも時間ではない。実測は 051 の変異実験で確認した
(`b.Grow(size)` → `b.Grow(size * 2)` を当てたときの出力)。

## なぜ (放置したときの害)

赤くなった人が最初に見るのがこの 1 行で、**時間の回帰を探しに行く**。確保の予算は
「+3% の絶対値」で時間の予算 (較正つき rel、20 倍の粗さ) とは対処が全く違うので、
入口で取り違えると調査が空振りする。

051 では Step Summary の表ヘッダだけ直した (`budget (ms)` → `budget`。
`tests/bench_stats.sh`)。**エラー文言側は未対応**で残っている。

## 対応方針 (案)

metric 名の接尾辞から表示単位を引く 3 行程度:

```sh
unit_of() { case "$1" in *_kb) printf 'KB';; *_mb) printf 'MB';; *_cpu_ms|*) printf 'ms';; esac; }
```

共有 checker なので nvim / tmux / zsh / glogx の全 metric に同時に効く。
**呼び出しは 121 / 123 の両方**に入れること (片方だけ直すのが典型的な取りこぼし)。

⚠️ **予算ファイルに単位トークンを持たせる案 (`view_steady_alloc_kb 31.0 kb`) は採らない**。
051 で「行の書式は増やさず、単位は metric 名が持つ」と決めた判断と衝突する
(書式を増やすと checker / bench_stats / 縮退経路 / 既存 4 予算ファイルに波及する)。
表示だけの問題なので、表示側で解く。

## 未確認

- `_ms` で終わる metric が将来 `_cpu_ms` 以外に出てきたときの分岐 (現状は既定が ms なので問題ない)
- CI ログを機械的に grep している経路が `ms` の文字列に依存していないか
  (`rules/bench-watch-after-push.md` が使うのは `metric=` 行と `bench_env_shift=` 行で、
  `::error::` 行ではない — 起票時点の読みでは影響なしだが、未実測)

## 関連

- `issues/done/051-perf-glogx-bench-gates-time-only.md` (`*_alloc_kb` の追加と、書式を
  増やさない判断の一次情報)
- `tests/check_bench_budgets.sh` / `tests/bench_stats.sh`
