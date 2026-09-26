package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pro-con/backend"
	"pro-con/card"
)

func spinModel(t *testing.T) (*Model, *clock) {
	t.Helper()
	be := newSpy()
	be.snap.Cards = append(be.snap.Cards, card.Card{ID: "T1", State: card.Running, Session: "s-t1", Since: be.snap.Now,
		Exec: card.Exec{Command: "make test", Since: be.snap.Now}})
	be.snap.Consumers = []backend.Consumer{{Session: "s-r1", CardID: "R1", Status: "busy"}, {Session: "s-w1", CardID: "W1", Status: "waiting"}}
	clk := &clock{t: be.snap.Now}
	m := New(be, nil)
	m.now = clk.now
	m.width, m.height = 120, 30
	m.Update(frameMsg{})
	return m, clk
}

func hasSpin(s string) bool {
	for _, f := range spinFrames {
		if strings.Contains(s, f) {
			return true
		}
	}
	return false
}

// PG が turn の途中 (status が busy) のカードだけに回る印が出る。PG が待っている (waiting) カードと、テストの係の結果を待つカード
// (実行中の T1 / 頼んだ直後でまだ busy の B1) には出さない (issue 455)。
func TestSpinnerOnlyOnProcessingCards(t *testing.T) {
	m, _ := spinModel(t)
	m.snap.Cards = append(m.snap.Cards, card.Card{ID: "B1", State: card.Running, Session: "s-b1", Run: "make test"})
	m.snap.Consumers = append(m.snap.Consumers, backend.Consumer{Session: "s-b1", CardID: "B1", Status: "busy"})
	for _, tc := range []struct {
		id   string
		want bool
	}{{"R1", true}, {"T1", false}, {"B1", false}, {"W1", false}, {"W2", false}} {
		var c card.Card
		for _, cc := range m.snap.Cards {
			if cc.ID == tc.id {
				c = cc
			}
		}
		if got := hasSpin(ansi.Strip(m.badge(c))); got != tc.want {
			t.Fatalf("%s: 回る印が出る = %v (期待 %v): %q", tc.id, got, tc.want, m.badge(c))
		}
	}
}

// 印は時刻で回り (spinInterval ごとに次のコマ)、回すカードが無くなると tick を止める (アイドルで描き直さない)。
func TestSpinnerTurnsAndStopsWhenIdle(t *testing.T) {
	m, clk := spinModel(t)
	a := m.spinFrame()
	clk.t = clk.t.Add(spinInterval)
	if b := m.spinFrame(); a == b {
		t.Fatalf("spinInterval が過ぎても同じコマ %q", a)
	}
	if !m.spinning {
		t.Fatal("処理中のカードがあるのに回る印の tick が回っていない")
	}
	if cmd := m.onSpin(); cmd == nil {
		t.Fatal("処理中のカードがあるのに次のコマを頼まない")
	}
	m.snap.Consumers, m.snap.Cards = nil, m.snap.Cards[:3] // PG が待ちに入り、テストの係も終わった
	if cmd := m.onSpin(); cmd != nil || m.spinning {
		t.Fatalf("回すカードが無いのに tick を回し続ける: cmd=%v spinning=%v", cmd != nil, m.spinning)
	}
}
