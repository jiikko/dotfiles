package main

import (
	"runtime"
	"testing"
)

// フレームの確保バイトが、一覧の件数に比例しないこと (issue 607)。見えるのは 40 行前後なのに件数に比例して確保するなら、
// 見えない行のために描くたびに働いている (268 / 270 / 274 / 275 と同じ形の退行)。status の一覧は
// TestStatusFrameAllocBytesDoNotScaleWithFileCount が同じことを見る。
// 見ていないもの: 確保せずに件数に比例して走査する形 (時間は合否にしない)。キー操作の経路。
func TestListFramesAllocBytesDoNotScaleWithItems(t *testing.T) {
	bytesPerFrame := func(build func(tb testing.TB) *browseModel) uint64 {
		m := build(t)
		_ = m.View().Content // 遅延初期化を計測から外す
		var a, b runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&a)
		const n = 20
		for range n {
			_ = m.View().Content
		}
		runtime.ReadMemStats(&b)
		return (b.TotalAlloc - a.TotalAlloc) / n
	}
	cases := []struct {
		name       string
		small, big func(tb testing.TB) *browseModel
	}{
		{"git log", func(tb testing.TB) *browseModel { return benchBrowseSubjects(tb, 40, 120, 40, true) },
			func(tb testing.TB) *browseModel { return benchBrowseSubjects(tb, 2000, 120, 40, true) }},
		{"issues", func(tb testing.TB) *browseModel { return benchIssuesBrowse(tb, 40, 120, 40) },
			func(tb testing.TB) *browseModel { return benchIssuesBrowse(tb, 2000, 120, 40) }},
	}
	for _, c := range cases {
		small, big := bytesPerFrame(c.small), bytesPerFrame(c.big)
		t.Logf("%s: 40 件 %d B/frame / 2000 件 %d B/frame", c.name, small, big)
		if r := float64(big) / float64(small); r > 1.3 {
			t.Errorf("%s: 2000 件のフレームが 40 件の %.2f 倍を確保している (%d → %d B)。件数に比例する組み立てが描く経路に入った", c.name, r, small, big)
		}
	}
}
