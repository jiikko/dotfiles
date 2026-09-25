package card

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// 順番 (After) の相手が記録に無い・順番が循環している、は違反 (dispatcher が永久に起動しない)。
func TestCheckAfter(t *testing.T) {
	ok := []Card{{ID: "A", State: Planned}, {ID: "B", State: Planned, After: []string{"A"}}}
	if vs := Check(ok); len(vs) != 0 {
		t.Fatalf("順番の付いた正しい集合で違反が出た: %v", reasons(vs))
	}
	missing := []Card{{ID: "B", State: Planned, After: []string{"nope"}}}
	if vs := Check(missing); len(vs) != 1 || vs[0].CardID != "B" || !strings.Contains(vs[0].Reason, "nope") {
		t.Fatalf("存在しない相手を違反として出すはず: %v", reasons(vs))
	}
	self := []Card{{ID: "A", State: Planned, After: []string{"A"}}}
	if vs := Check(self); len(vs) != 1 || !strings.Contains(vs[0].Reason, "循環") {
		t.Fatalf("自分の後は循環として出すはず: %v", reasons(vs))
	}
	cycle := []Card{{ID: "A", State: Planned, After: []string{"C"}}, {ID: "B", State: Planned, After: []string{"A"}}, {ID: "C", State: Requested, After: []string{"B"}}}
	if vs := Check(cycle); len(vs) != 3 {
		t.Fatalf("循環に載った 3 枚を違反として出すはず: %v", reasons(vs))
	}
	tail := []Card{{ID: "A", State: Planned, After: []string{"B"}}, {ID: "B", State: Planned, After: []string{"A"}}, {ID: "D", State: Planned, After: []string{"A"}}}
	if vs := Check(tail); len(vs) != 2 {
		t.Fatalf("循環の外から循環を指すだけのカード (D) は循環に数えない: %v", reasons(vs))
	}
}

// Blockers は前のカードのうち完了していないもの。完了 (却下も含む) は待たない。
func TestBlockers(t *testing.T) {
	cards := []Card{
		{ID: "A", State: Review},
		{ID: "B", State: Done, Ending: EndRejected},
		{ID: "C", State: Planned, After: []string{"A", "B"}},
	}
	if got := Blockers(cards, cards[2]); !slices.Equal(got, []string{"A"}) {
		t.Fatalf("完了していない A だけを待つはず: %v", got)
	}
	cards[0].State, cards[0].Ending = Done, EndAnswered
	if got := Blockers(cards, cards[2]); len(got) != 0 {
		t.Fatalf("前が全部完了したら待たない: %v", got)
	}
}

// Drop はカードを外し、そのカードを前に持つカードの順番からも外して履歴に残す (外さないと相手の無い順番が残る)。
func TestDropStripsAfter(t *testing.T) {
	now := time.Date(2026, 9, 25, 1, 0, 0, 0, time.UTC)
	cards := []Card{{ID: "A", State: Planned}, {ID: "B", State: Planned, After: []string{"A", "X"}}, {ID: "X", State: Running}}
	got := Drop(cards, "A", now)
	if len(got) != 2 || got[0].ID != "B" {
		t.Fatalf("A だけが外れるはず: %v", got)
	}
	if !slices.Equal(got[0].After, []string{"X"}) {
		t.Fatalf("B の順番から A だけが外れるはず: %v", got[0].After)
	}
	if n := len(got[0].History); n != 1 || !strings.Contains(got[0].History[0].Text, "A") {
		t.Fatalf("B の履歴に待ちを外した理由を残すはず: %v", got[0].History)
	}
	if vs := Check(got); len(vs) != 0 {
		t.Fatalf("外した後に違反が残る: %v", reasons(vs))
	}
	if cards[1].After[0] != "A" {
		t.Fatal("渡した集合 (呼び出し側の記録) を書き換えない")
	}
}
