# 781 (test): tmux の schedule_keys のログの刈り込みと、snapshot_health の間隔 0 の扱いをテストで固定する

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-16〜）

起票日: 2026-10-16

## 概要

issue 776 で `tt_prune_log_lines` と `tt_save_interval_minutes` を guards.sh に寄せたとき、敵対的レビューが変異で確かめたところ、次の 2 つはどのテストも見ていなかった (前からの穴)。

- `tmux_schedule_keys.sh` の `log` が書き込みのたびにログを刈ること (`tt_prune_log_lines "$LOG" "$LOG_MAX_LINES"`)。刈り込みを no-op にしても `tests/tmux/test_schedule_keys.sh` は緑
- `tmux_snapshot_health.sh` が間隔 0 (continuum では「無効」) を既定の 15 分に寄せること。`tests/tmux/test_snapshot_health.sh` は緑のまま (周期保存側は 776 で `tests/tmux/test_periodic_save.sh` に足した)

## 対応方針

- test_schedule_keys.sh に「LOG_MAX_LINES を小さくして何度か書くと、新しい側の行だけが残る」を足す
- test_snapshot_health.sh に「@continuum-save-interval=0 / 08 / abc のとき、鮮度の閾値が既定の間隔から計算される」を足す
- 足したテストに、刈り込みを no-op にする・`|0)` の寄せを外す変異を当てて red を確かめる

## 関連ファイル

- `scripts/tmux_schedule_keys.sh` / `scripts/tmux_snapshot_health.sh` / `scripts/lib/tmux_resurrect_guards.sh` / issue 776 (done)

## 進捗

- [x] test_schedule_keys.sh に「log: 書き込みのたびに上限の行数まで刈る」: 古い 5 行のログに `TMUX_SCHEDULE_KEYS_LOG_MAX_LINES=3` で 1 回書かせ、3 行・新しい行が残る・古い側が落ちる
- [x] test_snapshot_health.sh に (8): 下限 (`TT_HEALTH_MIN_STALE_SECONDS`) を 60 秒に下げ、`@continuum-save-interval` が 0 / abc / 08 / 20 で閾値が 30 / 30 / 16 / 40 分。
  health() は `TT_HEALTH_MIN_STALE_SECONDS` を明示で渡すようにした (既定 1800。親の環境から漏れない)
- 変異で red を確認 (`mutate-verify`): `log` の刈り込みを `:` に → 「上限 3 行に刈られない (7 行)」/ `[ "$v" -gt 0 ] || v=…` を外す → 「=0 で閾値が 30 分にならない」
  (test_periodic_save.sh も同時に red) / `v=$((10#$v))` を外す → 「=08 で閾値が 16 分にならない」
