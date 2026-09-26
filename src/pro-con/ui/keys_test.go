package ui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"pro-con/backend"
	"pro-con/card"
)

// 1 列 (着手待ち) に P0..P4 の 5 枚、右の列 (作業中) に R0。高さは 1 列 4 枚が見える大きさ (半ページ = 2 枚)。
func keysModel(t *testing.T) *Model {
	t.Helper()
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	var cs []card.Card
	for i, id := range []string{"P0", "P1", "P2", "P3", "P4"} {
		cs = append(cs, card.Card{ID: id, State: card.Planned, Since: now.Add(time.Duration(i) * time.Minute)})
	}
	cs = append(cs, card.Card{ID: "R0", State: card.Running, Since: now})
	m := New(&spy{snap: backend.Snapshot{Now: now, Cards: cs}}, nil)
	m.height = 9 + 2 + 4*perCardLines + cardGap
	m.selected = "P0"
	if m.shownCards() != 4 {
		t.Fatalf("前提: 1 列 4 枚のはず: %d", m.shownCards())
	}
	return m
}

func ctrl(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }

// 移動は tuikit の語彙 (glogx と同じ): ctrl+n / ctrl+p は ↓ / ↑、g / G は先頭 / 末尾、ctrl+d / ctrl+u は半ページ、ctrl+f / ctrl+b は → / ←。
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
		{"ctrl+b", ctrl('b'), "P0"}, // ← の別名 (ユーザー要望 2026-09-25)。h / ← と同じく同じ行へ
	}
	m := keysModel(t)
	for _, st := range steps {
		m.Update(st.key)
		if m.selected != st.want {
			t.Fatalf("%s の後の選択が違う: got %s want %s", st.name, m.selected, st.want)
		}
	}
}

// 数字キーでレーンへ直接移る。空のレーンにもフォーカスが当たり (カードは選ばない)、h / l も空のレーンを飛ばさない。
func TestLaneFocus(t *testing.T) {
	m := keysModel(t) // 着手待ち (2) に P0..P4、作業中 (3) に R0。他は空
	m.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	if m.col != 2 || m.selected != "R0" {
		t.Fatalf("3 で作業中の R0 に移るはず: col=%d sel=%q", m.col, m.selected)
	}
	m.Update(tea.KeyPressMsg{Code: '5', Text: "5"})
	if m.col != 4 || m.selected != "" {
		t.Fatalf("5 で空のレビューのレーンにフォーカスが当たるはず: col=%d sel=%q", m.col, m.selected)
	}
	m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if m.col != 3 || m.selected != "" {
		t.Fatalf("h で空の質問待ちのレーンに止まるはず (飛ばさない): col=%d sel=%q", m.col, m.selected)
	}
	m.Update(tea.KeyPressMsg{Code: '9', Text: "9"}) // レーンの数を越える数字は何もしない
	if m.col != 3 {
		t.Fatalf("範囲外の数字でレーンが動いた: col=%d", m.col)
	}
	m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	if m.col != 1 || m.selected != "P0" {
		t.Fatalf("2 で着手待ちの先頭に移るはず: col=%d sel=%q", m.col, m.selected)
	}
}

// 空のレーンでカードの要る操作をしても何も送らず、落ちない。
func TestActionsOnEmptyLane(t *testing.T) {
	m := keysModel(t)
	be := m.be.(*spy)
	m.Update(tea.KeyPressMsg{Code: '5', Text: "5"})
	for _, k := range []string{"r", "a", "+", "w", "y", "Y", "e"} {
		if _, cmd := m.Update(tea.KeyPressMsg{Code: []rune(k)[0], Text: k}); cmd != nil {
			cmd() // attach は裏で頼むので、返ったコマンドまで走らせてから「送られていない」を確かめる
		}
		if m.mode != modeBoard {
			t.Fatalf("空のレーンで %s が入力欄を開いた", k)
		}
	}
	if len(be.applied) != 0 || len(be.attached) != 0 {
		t.Fatalf("空のレーンで操作が送られた: %v %v", be.applied, be.attached)
	}
}
