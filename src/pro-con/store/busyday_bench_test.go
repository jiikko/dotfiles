package store_test

import (
	"testing"

	"pro-con/store"
	"pro-con/store/storetest"
)

// BenchmarkLoadBusyDay は完了して 24 時間以内のカードが溜まった日の記録を 1 回読む所要 (issue 528)。
// 本物の写しで測るときは PRO_CON_BENCH_CARDS に cards.json の写しのパスを渡す。
func BenchmarkLoadBusyDay(b *testing.B) {
	dir := b.TempDir()
	storetest.BusyDay(b, dir)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := store.Load(dir); err != nil {
			b.Fatal(err)
		}
	}
}
