package main

import (
	"glogx/internal/statusview"
	"strconv"
	"strings"
	"testing"
)

// statusRowsFixture は st を組んで receive まで通した statusView を返す。
func statusRowsFixture(t *testing.T, files int) *statusview.StatusView {
	t.Helper()
	recs := []string{"## master...origin/master"}
	for i := range files {
		p := "src/pkg" + strconv.Itoa(i) + "/f.go"
		if i%2 == 0 {
			recs = append(recs, "M  "+p) // staged
		} else {
			recs = append(recs, " M "+p) // unstaged
		}
	}
	v := &statusview.StatusView{}
	v.Fixture().SetShown(true)
	v.Fixture().ReceivePorcelain(strings.Join(recs, "\x00") + "\x00")
	if n := v.Fixture().RowCount(); n != files {
		t.Fatalf("前提が崩れた: rows=%d want %d", n, files)
	}
	return v
}

// フレームの確保**バイト数**がファイル数に比例しないこと (issue 048 の主張)。
//
// 🚨 `AllocsPerRun` (確保の**回数**) では測れない。この最適化が削るのは「1 本の大きな
// スライス」なので、回数の差は最適化の前後どちらも +5 で動かない — 実際 R1 レビューが
// 当初の回数版テストを「メモ化を丸ごと revert しても PASS」と実証した (false green)。
// バイト数なら 2000 件と 40 件の差が 48,745 B -> 682 B と 71 倍分離する。
func TestStatusFrameAllocBytesDoNotScaleWithFileCount(t *testing.T) {
	bytesPerFrame := func(files int) int64 {
		r := testing.Benchmark(func(b *testing.B) {
			m := benchStatusBrowse(b, files, 120, 40)
			_ = m.View().Content // 遅延初期化を計測から外す
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_ = m.View().Content
			}
		})
		return r.AllocedBytesPerOp()
	}
	small, big := bytesPerFrame(40), bytesPerFrame(2000)
	// メモ化前は +48,745 B、メモ化後は +682 B。上限 4000 は「桁で戻ったら気づく」ための粗いゲート
	// (実測の 6 倍弱の余裕。B/op はマシン非依存なので時間の予算より締められる)。
	const maxGrowth = 4000
	if big-small > maxGrowth {
		t.Errorf("2000 件のフレームが 40 件より %d B 多く確保している (40 件 %d B / 2000 件 %d B)。"+
			"行構成が毎フレーム作り直されていないか", big-small, small, big)
	}
	t.Logf("40 件 %d B/frame / 2000 件 %d B/frame (差 %d B)", small, big, big-small)
}
