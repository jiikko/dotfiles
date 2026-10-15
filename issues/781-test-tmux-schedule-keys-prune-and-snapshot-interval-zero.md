# 781 (test): tmux の schedule_keys のログの刈り込みと、snapshot_health の間隔 0 の扱いをテストで固定する

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
