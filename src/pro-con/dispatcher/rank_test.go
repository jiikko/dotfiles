package dispatcher

import (
	"context"
	"slices"
	"testing"

	"pro-con/card"
	"pro-con/store"
)

// 分解済みの列は上から起動する (人が並べ替えた優先度。issue 470)。並べ替えの依頼は同じ Tick の適用で当たる。
func TestDispatchFollowsLaneOrder(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 3)
	for range 2 { // C-003 を先頭へ
		if _, err := store.Submit(dir, store.Request{Kind: "move", CardID: "C-003", Delta: -1}); err != nil {
			t.Fatal(err)
		}
	}
	l := &fakeLauncher{}
	d := newDispatcher(t, dir, l, nil)
	d.Limit = 1
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(l.starts, []string{"pc-c-003"}) {
		t.Fatalf("上限 1 で先頭へ上げた C-003 から起動するはず: %v", l.starts)
	}
	if cs := states(t, dir); cs["C-003"].State != card.Running || cs["C-001"].State != card.Planned {
		t.Fatalf("C-003 だけ作業中のはず: %v %v", cs["C-003"].State, cs["C-001"].State)
	}
}
