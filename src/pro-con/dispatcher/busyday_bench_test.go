package dispatcher

import (
	"context"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/store/storetest"
)

// BenchmarkTickBusyDay は完了して 24 時間以内のカードが溜まった日の記録での Tick 1 回の所要 (issue 528)。
// 組み立ては BenchmarkTickWithFinishedCards と同じ (偽の launcher・session 一覧は空)。最初の Tick (起動・状態の変化) は測らない。
// 本物の写しで測るときは PRO_CON_BENCH_CARDS に cards.json の写しのパスを渡す。
func BenchmarkTickBusyDay(b *testing.B) {
	dir := b.TempDir()
	now := storetest.BusyDay(b, dir)
	d := &Dispatcher{Dir: dir, Limit: 2, Launch: &fakeLauncher{}, PMOff: true,
		List: func(context.Context) ([]agents.Session, error) { return nil, nil }, Now: func() time.Time { return now }, Sleep: func(time.Duration) {}}
	if _, err := d.Tick(context.Background()); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := d.Tick(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}
