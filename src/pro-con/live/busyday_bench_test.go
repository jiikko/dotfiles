package live

import (
	"context"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/backend"
	"pro-con/store/storetest"
)

// BenchmarkRefreshBusyDay は完了して 24 時間以内のカードが溜まった日の記録での、画面の読み直し (refresh) 1 回の所要 (issue 528)。
// session 一覧は空 (claude agents は呼ばない)。本物の写しで測るときは PRO_CON_BENCH_CARDS に cards.json の写しのパスを渡す。
func BenchmarkRefreshBusyDay(b *testing.B) {
	dir := b.TempDir()
	now := storetest.BusyDay(b, dir)
	bk := New([]backend.Repo{{Name: "dotfiles", Path: "/w/dotfiles"}}, b.TempDir(), dir)
	bk.list = func(context.Context) ([]agents.Session, error) { return nil, nil }
	bk.now = func() time.Time { return now }
	ctx := context.Background()
	bk.refresh(ctx, true)
	b.ReportAllocs()
	for b.Loop() {
		bk.refresh(ctx, false)
	}
}
