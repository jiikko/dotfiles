package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"pro-con/backend"
	"pro-con/card"
	"pro-con/fake"
)

func doneCount(m *Model) int {
	n := 0
	for _, c := range m.visible() {
		if c.State == card.Done {
			n++
		}
	}
	return n
}

// x は完了のレーンを片付ける。y/N 確認を挟み、N なら何もしない。片付けたカードはタブの枚数・カンバンのどちらにも出ない。
func TestClearDoneLane(t *testing.T) {
	be := fake.New(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC))
	m := New(be, []backend.Repo{{Name: "dotfiles"}})
	total, done := len(m.snap.Cards), doneCount(m)
	if done == 0 {
		t.Fatal("見本に完了のカードが要る")
	}

	press(m, "x")
	if m.mode != modeConfirm || !strings.Contains(ansi.Strip(m.render()), "完了のカード") {
		t.Fatalf("x で確認が出ない: mode=%v", m.mode)
	}
	m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if m.mode != modeBoard || doneCount(m) != done {
		t.Fatalf("n で取り消したのに片付いた: 完了 %d → %d", done, doneCount(m))
	}

	press(m, "x")
	m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if doneCount(m) != 0 {
		t.Fatalf("y の後も完了のレーンに %d 枚残っている", doneCount(m))
	}
	if !strings.Contains(m.toasts.Text(), "片付けた") {
		t.Fatalf("片付けた旨が出ない: %q", m.toasts.Text())
	}
	if want := total - done; len(m.snap.Cards) != want || !strings.Contains(ansi.Strip(m.tabBar()), "global "+itoa(want)) {
		t.Fatalf("片付けたカードがまだ数えられている: %d 枚 / %q", len(m.snap.Cards), ansi.Strip(m.tabBar()))
	}

	// 片付けるものが無ければ確認を出さない (空の確認に y を押させない)
	press(m, "x")
	if m.mode != modeBoard || !strings.Contains(m.toasts.Text(), "無い") {
		t.Fatalf("完了が 0 枚なのに確認へ進んだ: mode=%v flash=%q", m.mode, m.toasts.Text())
	}
}

// repo のタブでは、その repo の完了だけを片付ける (確認の文にも repo 名を出す)。
func TestClearDoneScopedToTab(t *testing.T) {
	be := newSpy()
	be.snap.Cards = append(be.snap.Cards, card.Card{ID: "D1", Repo: "dotfiles", State: card.Done, Ending: card.EndAnswered})
	m := New(be, []backend.Repo{{Name: "dotfiles"}})
	m.tab = "dotfiles"
	press(m, "x")
	if !strings.Contains(m.confirmText, "dotfiles") {
		t.Fatalf("確認の文に repo 名が無い: %q", m.confirmText)
	}
	m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if len(be.applied) != 1 || be.applied[0] != (backend.ClearDone{Repo: "dotfiles"}) {
		t.Fatalf("タブの repo で片付けを送るはず: %#v", be.applied)
	}
}
