#!/usr/bin/env bash
# glogx の描画ホットパス回帰ベンチ。go test -bench の ns/op を "metric=<name> ms=<value>"
# 行へ変換して出力する (CI では tests/run_bench.sh が 3 回実行 → min 集約 →
# tests/glogx/bench_budgets.ci でゲート)。
#
# 測るもの (回帰しがちなホットパス。いずれも chroma を含まない glogx 純粋コード):
#   - view_steady        : 一覧ビュー 1 フレームの View() (fetch/アニメ中は 80ms ごとに走る恒常コスト)
#   - view_panel         : job パネル + 詳細ポップアップを開いた状態の View() 1 フレーム
#   - render_large_patch : -p の大きな patch を含む RenderLines (起動/リロード時の一括整形)
#   - cursor_move_view   : j/k 1 打あたりの Update + View (最頻操作の入力→描画。2026-07-29 追加)
#   - view_diff          : diff オーバーレイ (スクロールバー込み) 表示中の 1 フレーム (同上)
#   - model_init_200     : 起動時の Go 側コスト (モデル構築 + 200 コミットの行構築。同上)
#   - glogx_calib        : runner 速度の較正器 (repo コード非依存の固定ワークロード。
#                          比較テーブルの rel 正規化と budgets の rel スケールに使う。2026-07-30)
#
# バイナリ起動の壁時計 / プロセス RSS は意図的に対象外: 実起動は git log / gh の fork と
# ネットワークに依存し CI では flake 枠 (chroma と同じ判断)。Go 側は model_init_200 が代理し、
# ヒープ傾向は各 benchmark の -benchmem (ローカル実行時) で見る。
#
# ⚠️ BenchmarkHighlightDiff は意図的に対象外: chroma のハイライトは共有 runner の速度ムラで
# 桁で膨れ (2026-07-21 実測: budget 5s に対し 10.36s で flake)、「CI ではゲートせずローカルで
# 測る」と判断済み (highlight_test.go の同 benchmark 直上コメントが一次情報)。ここに足す前に
# その判断を再評価すること。
#
# -benchtime は短め (200ms/本): run_bench.sh が 3 回走らせ、その min を採るため 1 回は軽くて
# よい (ノイズ耐性は run 階層の min 集約が担う)。-run '^$' でユニットテストは走らせない。
set -euo pipefail

# checker の数値検証は dot 小数前提。カンマ小数ロケールで awk の printf %.3f が "1,234" を
# 出さないよう数値カテゴリを C に固定する (bench_tmux.sh と同じ理由)
unset LC_ALL
export LC_NUMERIC=C

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
GLOGX_DIR=$(cd "$SCRIPT_DIR/../../src/glogx" && pwd)
cd "$GLOGX_DIR"

# 対象 benchmark を改名/削除したら、この一覧と下の awk・bench_budgets.ci を同時に更新する
# こと (漏れは checker の「予算にある metric が出力に無い」検出で CI が fail する)
go test -run '^$' \
  -bench '^(BenchmarkViewSteady|BenchmarkViewWithPanel|BenchmarkRenderLinesLargePatch|BenchmarkCursorMoveView|BenchmarkViewWithDiff|BenchmarkModelInit200|BenchmarkCalibrate)$' \
  -benchtime=200ms . |
  awk '
    /^BenchmarkViewSteady/            { printf "metric=view_steady ms=%.3f\n", $3 / 1000000 }
    /^BenchmarkViewWithPanel/         { printf "metric=view_panel ms=%.3f\n", $3 / 1000000 }
    /^BenchmarkRenderLinesLargePatch/ { printf "metric=render_large_patch ms=%.3f\n", $3 / 1000000 }
    /^BenchmarkCursorMoveView/        { printf "metric=cursor_move_view ms=%.3f\n", $3 / 1000000 }
    /^BenchmarkViewWithDiff/          { printf "metric=view_diff ms=%.3f\n", $3 / 1000000 }
    /^BenchmarkModelInit200/          { printf "metric=model_init_200 ms=%.3f\n", $3 / 1000000 }
    /^BenchmarkCalibrate/             { printf "metric=glogx_calib ms=%.3f\n", $3 / 1000000 }
  '
