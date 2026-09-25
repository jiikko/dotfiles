package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"pro-con/backend"
	"pro-con/card"
	"pro-con/fake"
)

// laneIDs は選んでいるレーンのカードを上から並べる。
func laneIDs(m *Model) []string {
	var out []string
	for _, c := range m.columns()[m.col] {
		out = append(out, c.ID)
	}
	return out
}

// K / J は選んでいるカードを上 / 下の隣と入れ替える (模擬のモード)。選択はカードについていく。端では頼まずに止めて知らせる。
func TestMoveCardWithKJ(t *testing.T) {
	m := New(fake.New(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)), []backend.Repo{{Name: "dotfiles"}})
	m.jumpCol(slices.Index(card.Columns, card.Planned))
	before := laneIDs(m)
	if len(before) < 2 {
		t.Fatalf("見本の分解済みのレーンに 2 枚以上要る: %v", before)
	}
	m.selected = before[1]
	press(m, "K")
	if got := laneIDs(m); got[0] != before[1] || got[1] != before[0] || m.selected != before[1] {
		t.Fatalf("K で 1 つ上がり、選択はついていくはず: %v → %v / 選択 %s", before, got, m.selected)
	}
	press(m, "K")
	if got := laneIDs(m); got[0] != before[1] || !strings.Contains(m.toasts.Text(), "先頭") {
		t.Fatalf("先頭で止めて知らせるはず: %v / %q", got, m.toasts.Text())
	}
	press(m, "J")
	if got := laneIDs(m); !slices.Equal(got, before) || m.selected != before[1] {
		t.Fatalf("J で 1 つ下がるはず: %v → %v", before, got)
	}
}

// 端では backend に頼まない (巻かない)。頼むときは画面のタブを添える (repo のタブではその repo の中の隣)。
func TestMoveCardSendsTabAndStopsAtEdge(t *testing.T) {
	be := newSpy()
	m := New(be, nil)
	m.jumpCol(slices.Index(card.Columns, card.Waiting))
	m.selected = "W1"
	press(m, "K")
	if len(be.applied) != 0 {
		t.Fatalf("先頭で上へを backend に頼んだ: %+v", be.applied)
	}
	press(m, "J")
	if len(be.applied) != 1 || be.applied[0] != (backend.MoveCard{CardID: "W1", Repo: m.tab, Delta: 1, Seen: be.snap.Cards[1].Since}) {
		t.Fatalf("J で下へを頼むはず: %+v", be.applied)
	}
}
