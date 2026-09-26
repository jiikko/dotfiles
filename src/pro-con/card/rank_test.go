package card

import (
	"errors"
	"slices"
	"testing"
	"time"
)

// lane は State のレーンの ID を上から並べる (repo が空でなければその repo だけ)。
func lane(cards []Card, st State, repo string) []string {
	cs := slices.DeleteFunc(slices.Clone(cards), func(c Card) bool { return c.State != st || c.Archived || (repo != "" && c.Repo != repo) })
	slices.SortStableFunc(cs, LaneCompare)
	var out []string
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

// 同じ時刻に列へ入ったカード (同じ Tick で積んだ) どうしでも入れ替わる。押した回数だけ動き、端では止まる。
func TestMoveSwapsWithNeighbor(t *testing.T) {
	t0 := time.Date(2026, 9, 26, 1, 0, 0, 0, time.UTC)
	cs := []Card{{ID: "A", State: Planned, Since: t0}, {ID: "B", State: Planned, Since: t0}, {ID: "C", State: Planned, Since: t0},
		{ID: "X", State: Running, Since: t0}}
	if other, err := Move(cs, "C", "", -1); err != nil || other != "B" {
		t.Fatalf("C を上へ: 相手 %q / %v", other, err)
	}
	if got := lane(cs, Planned, ""); !slices.Equal(got, []string{"A", "C", "B"}) {
		t.Fatalf("1 回で 1 つ上がるはず: %v", got)
	}
	if _, err := Move(cs, "C", "", -1); err != nil {
		t.Fatal(err)
	}
	if _, err := Move(cs, "C", "", -1); !errors.Is(err, ErrLaneEdge) {
		t.Fatalf("先頭で上へは端のはず (巻かない): %v", err)
	}
	if got := lane(cs, Planned, ""); !slices.Equal(got, []string{"C", "A", "B"}) {
		t.Fatalf("2 回で先頭へ: %v", got)
	}
	if _, err := Move(cs, "C", "", 1); err != nil {
		t.Fatal(err)
	}
	if got := lane(cs, Planned, ""); !slices.Equal(got, []string{"A", "C", "B"}) {
		t.Fatalf("下へで 1 つ戻る: %v", got)
	}
	if _, err := Move(cs, "B", "", 1); !errors.Is(err, ErrLaneEdge) {
		t.Fatalf("末尾で下へは端のはず: %v", err)
	}
	if _, err := Move(cs, "X", "", 1); !errors.Is(err, ErrLaneEdge) {
		t.Fatalf("別のレーンのカードを隣にしない (作業中は X だけ): %v", err)
	}
}

// repo のタブから動かすと、その repo のカードの中の隣と入れ替わる (間に挟まった別の repo のカードは動かない)。
func TestMoveWithinRepo(t *testing.T) {
	t0 := time.Date(2026, 9, 26, 1, 0, 0, 0, time.UTC)
	cs := []Card{{ID: "A", Repo: "r", State: Planned, Since: t0}, {ID: "O", Repo: "o", State: Planned, Since: t0.Add(time.Second)},
		{ID: "B", Repo: "r", State: Planned, Since: t0.Add(2 * time.Second)}}
	if other, err := Move(cs, "B", "r", -1); err != nil || other != "A" {
		t.Fatalf("repo の中の隣は A: %q / %v", other, err)
	}
	if got := lane(cs, Planned, ""); !slices.Equal(got, []string{"B", "O", "A"}) {
		t.Fatalf("A と B だけが入れ替わるはず: %v", got)
	}
	if _, err := Move(cs, "O", "r", -1); err == nil {
		t.Fatal("repo の外のカードをその repo のタブから動かせた")
	}
}

// 列を移ったら並べ替えは効かなくなり、移った先では入った順 (末尾) に着く。
func TestRankDropsOnLaneChange(t *testing.T) {
	t0 := time.Date(2026, 9, 26, 1, 0, 0, 0, time.UTC)
	cs := []Card{{ID: "A", State: Planned, Since: t0}, {ID: "B", State: Planned, Since: t0.Add(time.Second)},
		{ID: "R", State: Running, Since: t0.Add(time.Minute)}}
	if _, err := Move(cs, "B", "", -1); err != nil {
		t.Fatal(err)
	}
	cs[1].State, cs[1].Since = Running, t0.Add(2*time.Minute) // B が起動した
	if got := lane(cs, Running, ""); !slices.Equal(got, []string{"R", "B"}) {
		t.Fatalf("移った先では末尾に着くはず: %v", got)
	}
}

// 同じ鍵をばらしても、同じ時刻に後から列へ入ったカードが入れ替えたカードの間に割り込まない (ばらすのは前へ)。
func TestMoveKeepsLaterArrivalsBelow(t *testing.T) {
	t0 := time.Date(2026, 9, 26, 1, 0, 0, 0, time.UTC)
	cs := []Card{{ID: "C-005", State: Planned, Since: t0}, {ID: "C-006", State: Planned, Since: t0}}
	if _, err := Move(cs, "C-006", "", -1); err != nil {
		t.Fatal(err)
	}
	cs = append(cs, Card{ID: "C-007", State: Planned, Since: t0}) // 同じ Apply (同じ now) で後から積まれた
	if got := lane(cs, Planned, ""); !slices.Equal(got, []string{"C-006", "C-005", "C-007"}) {
		t.Fatalf("後から来たカードは末尾のはず: %v", got)
	}
}
