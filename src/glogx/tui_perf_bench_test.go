package main

import (
	"fmt"
	"hash/fnv"
	"testing"
)

// CI の Bench workflow (tests/glogx/bench_glogx.sh) が回す体感系ベンチ (2026-07-29 追加)。
// 既存の view_steady / view_panel (tui_bench_test.go) が「フレームの恒常コスト」を測るのに
// 対し、こちらはユーザー操作に直結する 3 点を測る:
//   - cursor_move_view : j/k 1 打あたりの Update + View (最頻操作の入力→描画レイテンシ)
//   - view_diff        : diff オーバーレイ (スクロールバー込み) を開いた状態の 1 フレーム
//   - model_init_200   : 起動時の Go 側コスト (モデル構築 + verbatim 200 コミットの lines 構築)
//
// バイナリ起動の壁時計 / プロセス RSS は意図的に対象外: 実起動は git log / gh の fork と
// ネットワークに依存し、CI ではハイライト (chroma) と同じ flake 枠になる
// (bench_glogx.sh ヘッダの判断と同型)。起動の repo 側コストはここの model_init_200 が代理する。

// カーソル移動 1 打の入力→描画。j/k を交互に打って範囲内に留まる
// (1 iteration = 2 打 + 2 フレーム。metric は 1 iteration あたりの値)。
func BenchmarkCursorMoveView(b *testing.B) {
	m := benchBrowse(b, 20, 120, 40)
	b.ReportAllocs()
	for b.Loop() {
		m.handleKey("j")
		_ = m.View().Content
		m.handleKey("k")
		_ = m.View().Content
	}
}

// diff オーバーレイ表示中のフレーム (withShadowScrollbar + buildShadowPanelBox が毎フレーム走る)。
func BenchmarkViewWithDiff(b *testing.B) {
	m := benchBrowse(b, 20, 120, 40)
	sha := m.commits[0].SHA
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = fmt.Sprintf("+ added line %d with some diff content", i)
	}
	m.diffOv.sha = sha
	m.diffOv.cache[sha] = lines
	m.diffOv.offset = 50
	b.ReportAllocs()
	for b.Loop() {
		_ = m.View().Content
	}
}

// 起動時の Go 側コスト: モデル構築 + 200 コミットの表示行構築 (verbatim 経路)。
// git log の fork は含まない (上記ヘッダ参照)。
func BenchmarkModelInit200(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		// benchBrowse は Cleanup 登録・cache 温めまで行うので、構築コスト自体を測るため
		// ここでは温め (lines) を計測区間に含める
		m := benchBrowse(b, 200, 120, 40)
		b.StartTimer()
		m.invalidateLines()
		_ = m.lines()
		_ = m.View().Content
	}
}

// BenchmarkCalibrate は runner 速度の較正器。glogx のコードに一切依存しない固定ワークロード
// (64KB の FNV-1a ハッシュ) で、この値の変動 = runner の CPU 世代/混雑の変動とみなせる。
// tests/glogx/bench_budgets.ci の calibrate 宣言と対で、Bench の比較テーブル (bench_stats.sh)
// が rel metric を run 間で正規化するのに使う (較正器なしだと無変更 push でも全 metric が
// 一様に ×2 級で振れ、偽の悪化表示になる。実測 2026-07-30: run 30454886162 → 30475764682)。
// ⚠️ このワークロードを変えたら budgets の calibrate 基準値も取り直すこと。
func BenchmarkCalibrate(b *testing.B) {
	buf := make([]byte, 64<<10)
	for i := range buf {
		buf[i] = byte(i)
	}
	b.ResetTimer()
	for b.Loop() {
		h := fnv.New64a()
		_, _ = h.Write(buf)
		calibSink = h.Sum64()
	}
}

// calibSink は較正器ワークロードの dead-code 除去防止。
var calibSink uint64
