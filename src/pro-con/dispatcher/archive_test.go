package dispatcher

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/store"
)

// finishedRecord は終えたカードと動いているカードを混ぜた記録を作る (issue 478 の発火条件: 終えたカードが記録に溜まる)。
func finishedRecord(t *testing.T, dir string) {
	t.Helper()
	done := func(id string, since time.Time) card.Card {
		return card.Card{ID: id, Title: id, Repo: "dotfiles", State: card.Done, Ending: card.EndAnswered, Since: since}
	}
	cleared := done("C-001", t0.Add(-time.Hour))
	cleared.Archived = true
	old := done("C-002", t0.Add(-store.AutoClearAfter-time.Minute))
	recent := done("C-003", t0.Add(-time.Hour))
	stopping := done("C-004", t0.Add(-store.AutoClearAfter-time.Minute))
	stopping.Archived, stopping.StopAfterClose = true, true
	parent := done("C-005", t0.Add(-store.AutoClearAfter-time.Minute))
	parent.Archived = true
	child := card.Card{ID: "C-006", ParentID: "C-005", Title: "子", Repo: "dotfiles", State: card.Requested, Since: t0}
	if err := store.Update(dir, time.Now(), func(s *store.State) error {
		s.NextID = 7
		s.Cards = []card.Card{cleared, old, recent, stopping, parent, child}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// 片付けたカードと、完了から AutoClearAfter たったカードは、Tick が記録から書庫へ移す (記録の大きさを作ったカードの総数に比例させない)。
// PG を止め終えていない・記録に残る子カードの親は残す。
func TestTickMovesFinishedCardsToArchive(t *testing.T) {
	dir := t.TempDir()
	finishedRecord(t, dir)
	d := newDispatcher(t, dir, &fakeLauncher{}, nil)
	d.PMOff = true
	notes, err := d.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := states(t, dir)
	for _, id := range []string{"C-001", "C-002"} {
		if _, ok := got[id]; ok {
			t.Errorf("%s が記録に残った (書庫へ移していない)", id)
		}
	}
	for _, id := range []string{"C-003", "C-004", "C-005", "C-006"} {
		if _, ok := got[id]; !ok {
			t.Errorf("%s を記録から外した (まだ移してはいけない)", id)
		}
	}
	arch, err := store.LoadArchive(dir)
	if err != nil {
		t.Fatal(err)
	}
	var archIDs []string
	for _, c := range arch {
		archIDs = append(archIDs, c.ID)
	}
	if !slices.Equal(archIDs, []string{"C-001", "C-002"}) {
		t.Fatalf("書庫の中身が違う: %v", archIDs)
	}
	if !arch[1].Archived || lastHistory(arch[1]) != store.AutoClearText {
		t.Fatalf("自動で片付けたカードに片付けた印・履歴が無い: Archived=%v 履歴=%q", arch[1].Archived, lastHistory(arch[1]))
	}
	if !hasNote(notes, eventlog.KindArchive, "C-002: "+store.AutoClearText) || hasNote(notes, eventlog.KindArchive, "C-001") {
		t.Fatalf("自動で片付けたカードだけを出来事に出していない: %v", notes)
	}
}

// BenchmarkTickWithFinishedCards は、終えたカードが 750 枚 (dogfooding の速さで約 50 日) 溜まった記録での空の Tick の所要 (issue 478)。
// 書庫へ移す前は Tick が毎回 750 枚を読み直す。移した後は動いているカードの数だけで決まる。
func BenchmarkTickWithFinishedCards(b *testing.B) {
	dir := b.TempDir()
	hist := make([]card.Event, 40) // 本物の 1 枚 (約 4KB) に近づける
	for i := range hist {
		hist[i] = card.Event{At: t0, Text: "PG を起動した (session xxxxxxxx)。テストの係に頼んだ: make test"}
	}
	var cs []card.Card
	for i := range 750 {
		cs = append(cs, card.Card{ID: fmt.Sprintf("C-%03d", i+1), Title: "終えた", Repo: "dotfiles", State: card.Done, Ending: card.EndAnswered,
			Since: t0, Archived: true, History: hist})
	}
	if err := store.Update(dir, time.Now(), func(s *store.State) error { s.Cards, s.NextID = cs, len(cs)+1; return nil }); err != nil {
		b.Fatal(err)
	}
	d := &Dispatcher{Dir: dir, Limit: 2, Launch: &fakeLauncher{}, PMOff: true,
		List: func(context.Context) ([]agents.Session, error) { return nil, nil }, Now: func() time.Time { return t0 }, Sleep: func(time.Duration) {}}
	b.ResetTimer()
	for range b.N {
		if _, err := d.Tick(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}
