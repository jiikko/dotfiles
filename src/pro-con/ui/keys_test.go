package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"pro-con/backend"
	"pro-con/card"
)

// 1 列 (分解済み) に P0..P4 の 5 枚、右の列 (作業中) に R0。高さは 1 列 4 枚が見える大きさ (半ページ = 2 枚)。
func keysModel(t *testing.T) *Model {
	t.Helper()
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	var cs []card.Card
	for i, id := range []string{"P0", "P1", "P2", "P3", "P4"} {
		cs = append(cs, card.Card{ID: id, State: card.Planned, Since: now.Add(time.Duration(i) * time.Minute)})
	}
	cs = append(cs, card.Card{ID: "R0", State: card.Running, Since: now})
	m := New(&spy{snap: backend.Snapshot{Now: now, Cards: cs}}, nil)
	m.height = 9 + 2 + 4*perCardLines
	m.selected = "P0"
	if m.shownCards() != 4 {
		t.Fatalf("前提: 1 列 4 枚のはず: %d", m.shownCards())
	}
	return m
}

func ctrl(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }

// 移動は tuikit の語彙 (glogx と同じ): ctrl+n / ctrl+p は ↓ / ↑、g / G は先頭 / 末尾、ctrl+d / ctrl+u は半ページ、ctrl+f は →。
func TestMotionVocabulary(t *testing.T) {
	steps := []struct {
		name string
		key  tea.KeyPressMsg
		want string
	}{
		{"ctrl+n", ctrl('n'), "P1"},
		{"ctrl+n", ctrl('n'), "P2"},
		{"ctrl+p", ctrl('p'), "P1"},
		{"G", tea.KeyPressMsg{Code: 'G', Text: "G"}, "P4"},
		{"g", tea.KeyPressMsg{Code: 'g', Text: "g"}, "P0"},
		{"ctrl+d", ctrl('d'), "P2"},
		{"ctrl+d (端で止まる)", ctrl('d'), "P4"},
		{"ctrl+u", ctrl('u'), "P2"},
		{"ctrl+f", ctrl('f'), "R0"},
	}
	m := keysModel(t)
	for _, st := range steps {
		m.Update(st.key)
		if m.selected != st.want {
			t.Fatalf("%s の後の選択が違う: got %s want %s", st.name, m.selected, st.want)
		}
	}
}
