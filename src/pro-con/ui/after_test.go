package ui

import (
	"strings"
	"testing"

	"pro-con/card"
)

// 順番で起動を待っているカードは、バッジに「前のカードの後」が出る (issue 468)。前が完了したら出さない。
func TestBadgeShowsAfter(t *testing.T) {
	be := newSpy()
	now := be.snap.Now
	be.snap.Cards = []card.Card{
		{ID: "C-001", State: card.Running, Since: now},
		{ID: "C-002", State: card.Planned, Since: now, After: []string{"C-001"}},
	}
	m := New(be, nil)
	if b := m.badge(be.snap.Cards[1]); !strings.Contains(b, "…C-001 の後") {
		t.Fatalf("バッジに順番の待ちが無い: %q", b)
	}
	m.snap.Cards[0].State, m.snap.Cards[0].Ending = card.Done, card.EndAnswered // 画面は New で Snapshot を取り込んでいる
	if b := m.badge(m.snap.Cards[1]); strings.Contains(b, "の後") {
		t.Fatalf("前が完了しても順番の待ちを出す: %q", b)
	}
}

// 引き出し (カードの詳細) に順番が出る (pro-con card show と同じ中身)。
func TestDrawerShowsAfter(t *testing.T) {
	be := newSpy()
	be.snap.Cards = []card.Card{
		{ID: "C-001", State: card.Running, Since: be.snap.Now},
		{ID: "C-002", State: card.Planned, Since: be.snap.Now, After: []string{"C-001"}},
	}
	m := New(be, nil)
	m.width, m.drawerCard = 200, "C-002"
	if body := strings.Join(m.drawerBody(), "\n"); !strings.Contains(body, "順番: C-001 の後") || !strings.Contains(body, "待ち: C-001 の後") {
		t.Fatalf("引き出しに順番が無い: %q", body)
	}
}
