#!/usr/bin/env bash
# glogx の描画ホットパス回帰ベンチ。go test -bench の ns/op と B/op を
# "metric=<name> ms=<value>" 行へ変換して出力する (CI では tests/run_bench.sh が複数回実行 →
# min 集約 → tests/glogx/bench_budgets.ci でゲート。実行回数は run_bench.sh の BENCH_RUNS が出典)。
#
# 時間 (<name>) と確保バイト (<name>_alloc_kb) の 2 本立て。🚨 単位は metric 名が持ち、
# 行の項目名は常に ms= (tmux の server_rss_mb / nvim の startup_cpu_ms と同じ流儀。
# checker と bench_stats を 1 書式のまま保つため)。_alloc_kb は B/op ÷ 1024。
#
# 確保も測る理由 (issue 051): フレームのコストの本体は確保で、時間だけ見ていると
# 「確保が 2 倍でも時間が予算内なら通る」。047/048 の実測では時間 −10〜34% に対し
# 確保はバイトで −23〜53% 動いた (GC の回転数に直結する。047 で runtime.kevent の 42% が
# gcStart 由来だった)。回数の退行は src/glogx/frame_alloc_test.go 側が -race 付きで見る。
#
# 測るもの (回帰しがちなホットパス。いずれも chroma を含まない glogx 純粋コード):
#   - view_steady        : 一覧ビュー 1 フレームの View() (fetch/アニメ中は 80ms ごとに走る恒常コスト)
#   - view_panel         : job パネル + 詳細ポップアップを開いた状態の View() 1 フレーム
#   - render_large_patch : -p の大きな patch を含む RenderLines (起動/リロード時の一括整形)
#   - cursor_move_view   : j/k 1 打あたりの Update + View (最頻操作の入力→描画。2026-07-29 追加)
#   - view_diff          : diff オーバーレイ (スクロールバー込み) 表示中の 1 フレーム (同上)
#   - model_init_200     : 起動時の Go 側コスト (モデル構築 + 200 コミットの行構築。同上)
#   - issue_scan         : issue 一覧生成 (Scan + 全件 LoadMeta、合成 50 件。git fork は
#                          含まない。ファイル I/O を含むため時間予算は他より粗め。2026-08-15)
#   - issues_view_frame  : issues viewer の 1 フレーム (40 件)。status viewer と対になる
#                          全画面ビューで、これまで唯一ゲート外だった (issue 062。2026-08-15)
#   - issues_view_2000   : 同 2000 件 (「件数に比例して働いていないか」の status_view_2000 と
#                          同型のゲート。導入時実測は 40 件比 +5% で比例していない)
#   - glogx_calib        : runner 速度の較正器 (repo コード非依存の固定ワークロード。
#                          比較テーブルの rel 正規化と budgets の rel スケールに使う。2026-07-30)
#
# バイナリ起動の壁時計 / プロセス RSS は意図的に対象外: 実起動は git log / gh の fork と
# ネットワークに依存し CI では flake 枠 (chroma と同じ判断)。Go 側は model_init_200 が代理し、
# ヒープ傾向は各 benchmark の -benchmem (ローカル実行時) で見る。
#
# 🚨 BenchmarkHighlightDiff は意図的に対象外: chroma のハイライトは共有 runner の速度ムラで
# 桁で膨れ (2026-07-21 実測: budget 5s に対し 10.36s で flake)、「CI ではゲートせずローカルで
# 測る」と判断済み (highlight_test.go の同 benchmark 直上コメントが一次情報)。ここに足す前に
# その判断を再評価すること。
#
# -benchtime は短め (200ms/本): run_bench.sh が同じスクリプトを何度も走らせ、その min を採る
# ため 1 回は軽くてよい (ノイズ耐性は run 階層の min 集約が担う)。-run '^$' でユニットテストは
# 走らせない。回数を上げ下げしたいときは run_bench.sh の BENCH_RUNS を触る (ここは追従不要)。
set -euo pipefail
unset CDPATH  # CDPATH が export されていると `cd foo` が解決先を stdout に出し、
              # SCRIPT_DIR=$(cd ... && pwd) が 2 行に化ける (rules/shell-script-cdpath.md)

# checker の数値検証は dot 小数前提。カンマ小数ロケールで awk の printf %.3f が "1,234" を
# 出さないよう数値カテゴリを C に固定する (bench_tmux.sh と同じ理由)
unset LC_ALL
export LC_NUMERIC=C

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
GLOGX_DIR=$(cd "$SCRIPT_DIR/../../src/glogx" && pwd)
cd "$GLOGX_DIR" || exit 1

# 対象 benchmark を改名/削除したら、この一覧と下の awk・bench_budgets.ci を同時に更新する
# こと (漏れは checker の「予算にある metric が出力に無い」検出で CI が fail する)
#
# テスト用 seam (tests/glogx/test_bench_glogx_metrics.sh が使う。CI/運用では未設定のまま):
#   GLOGX_BENCH_INPUT : go test を走らせず、このファイルの内容を awk に流す (列ずれガードの検証用)
#   GLOGX_BENCHTIME   : -benchtime の上書き (テストは 1x で配管だけ速く回す)。
#                       🚨 -run '^$' を外して短い benchtime と併用すると、testing.Benchmark を
#                       使う TestFrameAllocBudget が反復不足で Fatal する (issue 063 / 051 実測)
run_bench() {
  if [ -n "${GLOGX_BENCH_INPUT:-}" ]; then
    cat "$GLOGX_BENCH_INPUT"
    return
  fi
  go test -run '^$' \
    -bench '^(BenchmarkViewSteady|BenchmarkViewWithPanel|BenchmarkRenderLinesLargePatch|BenchmarkCursorMoveView|BenchmarkViewWithDiff|BenchmarkModelInit200|BenchmarkStatusViewFrame|BenchmarkStatusViewFrame2000|BenchmarkIssuesViewFrame|BenchmarkIssuesViewFrame2000|BenchmarkIssueScan|BenchmarkCalibrate)$' \
    -benchtime="${GLOGX_BENCHTIME:-200ms}" -benchmem .
}
run_bench |
  awk '
    # 🚨 列位置 ($3=ns/op の値・$5=B/op の値) を項目名で検証してから読む。b.ReportMetric を
    # 足した benchmark が混ざると列がずれ、検証が無いと**別の数値を予算照合して黙って
    # pass する**。ずれたら emit しない = checker の「予算にある metric が出力に無い」で
    # loud に落ちる (-benchmem を付けているので、ReportAllocs の有無には依存しない)。
    function emit(name) {
      if ($4 != "ns/op" || $6 != "B/op") {
        printf "bench: %s の列が想定と違うため metric を出さない: %s\n", name, $0 > "/dev/stderr"
        return
      }
      printf "metric=%s ms=%.3f\n", name, $3 / 1000000
      printf "metric=%s_alloc_kb ms=%.3f\n", name, $5 / 1024
      ns[name] = $3   # 比 metric (END で出す) 用に ns/op を覚える
    }
    # 較正器は「runner がどれだけ遅いか」を測る道具で、確保 (8 B/op) はゲートする意味が
    # 無いため時間だけ出す
    function emit_time_only(name) {
      if ($4 != "ns/op") {
        printf "bench: %s の列が想定と違うため metric を出さない: %s\n", name, $0 > "/dev/stderr"
        return
      }
      printf "metric=%s ms=%.3f\n", name, $3 / 1000000
    }
    # 🚨 前方一致で振り分けない。benchmark 名は他の benchmark の接頭辞になりうるので
    # (BenchmarkViewSteady ⊂ BenchmarkViewSteadyJA)、前方一致だと **兄弟 benchmark の
    # 数値が既存 metric を黙って上書きする** (実測: JA の行が view_steady として出た)。
    # 末尾の -<GOMAXPROCS> を落として完全一致で振り分ける
    { bench = $1; sub(/-[0-9]+$/, "", bench) }
    bench == "BenchmarkViewSteady"            { emit("view_steady") }
    bench == "BenchmarkViewWithPanel"         { emit("view_panel") }
    bench == "BenchmarkRenderLinesLargePatch" { emit("render_large_patch") }
    bench == "BenchmarkCursorMoveView"        { emit("cursor_move_view") }
    bench == "BenchmarkViewWithDiff"          { emit("view_diff") }
    bench == "BenchmarkModelInit200"          { emit("model_init_200") }
    bench == "BenchmarkStatusViewFrame2000"   { emit("status_view_2000") }
    bench == "BenchmarkStatusViewFrame"       { emit("status_view_frame") }
    bench == "BenchmarkIssuesViewFrame2000"   { emit("issues_view_2000") }
    bench == "BenchmarkIssuesViewFrame"       { emit("issues_view_frame") }
    bench == "BenchmarkIssueScan"             { emit("issue_scan") }
    bench == "BenchmarkCalibrate"             { emit_time_only("glogx_calib") }

    # 🚨 「件数を増やしたときの伸び率」を**同一 run 内の比**で出す (issue 269)。
    # 絶対時間の予算は runner の速度差・混雑に晒されるので粗くせざるを得ず、実測の
    # 16〜125 倍まで緩んでいて 2.7 倍の退行 (issue 268) を素通しした。比なら**同じ run の
    # 2 測定に同じ倍率が乗って打ち消える**ので、rel も較正も要らずに締められる。
    # tests/tmux/bench_tmux.sh の agent_panel_tick_scale_x100 が同じ形の前例。
    # 計測失敗 (どちらか欠測 / 0) は 99999 の番兵で loud に落とす (0 にすると予算内に見える)。
    function emit_scale(name, small, big,   s, b) {
      s = ns[small]; b = ns[big]
      # 🚨 「この run に含まれていない」と「測ったが壊れた」を分ける。
      # 両方欠けている = そもそも走らせていない (部分実行・合成入力) ので黙る。
      # checker 側の「予算にある metric が出力に無い」が loud に落とすので、本番の
      # 取りこぼしはそちらで捕まる。片方だけ欠ける / 0 は計測失敗なので番兵で落とす。
      if (s == "" && b == "") return
      if (s == "" || b == "" || s + 0 <= 0) {
        printf "metric=%s ms=99999\n", name
        printf "bench: %s の比を出せない (small=%s big=%s)\n", name, s, b > "/dev/stderr"
        return
      }
      printf "metric=%s ms=%d\n", name, (b * 100) / s
    }
    END {
      emit_scale("issues_view_scale_x100", "issues_view_frame", "issues_view_2000")
      emit_scale("status_view_scale_x100", "status_view_frame", "status_view_2000")
    }
  '
