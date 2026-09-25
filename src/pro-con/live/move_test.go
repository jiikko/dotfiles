package live

import (
	"context"
	"slices"
	"testing"
	"time"

	"pro-con/backend"
	"pro-con/card"
	"pro-con/store"
)

func laneOf(cards []card.Card) []string {
	cs := slices.Clone(cards)
	slices.SortStableFunc(cs, card.LaneCompare)
	var out []string
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

// 並べ替えは dispatcher の適用を待たずに画面の並びへ先に当てる (続けて押したキーが古い並びで端と断られない)。
// dispatcher が適用して読み直した後も同じ並び (正本は記録)。
func TestMoveAheadBeforeDispatcherApplies(t *testing.T) {
	b, _ := testBackend(t, nil, nil)
	for range 3 {
		if _, err := store.Submit(b.dir, store.Request{Kind: "add", Title: "t", Repo: "dotfiles"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Apply(b.dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	b.refresh(context.Background(), false)
	for range 2 {
		cs := b.Snapshot().Cards
		c := cs[slices.IndexFunc(cs, func(c card.Card) bool { return c.ID == "C-003" })]
		if _, err := b.Apply(backend.MoveCard{CardID: "C-003", Delta: -1, Seen: c.Since}); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"C-003", "C-001", "C-002"}
	if got := laneOf(b.Snapshot().Cards); !slices.Equal(got, want) {
		t.Fatalf("適用の前に画面の並びへ当たっていない: %v", got)
	}
	res, err := store.Apply(b.dir, time.Now())
	if err != nil || len(res) != 2 || res[0].Err != "" || res[1].Err != "" {
		t.Fatalf("dispatcher の適用: %+v %v", res, err)
	}
	b.refresh(context.Background(), false)
	if got := laneOf(b.Snapshot().Cards); !slices.Equal(got, want) {
		t.Fatalf("読み直した記録の並びが画面と違う: %v", got)
	}
}
