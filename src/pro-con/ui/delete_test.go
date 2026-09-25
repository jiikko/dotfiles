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

func shown(m *Model, id string) bool {
	for _, c := range m.snap.Cards {
		if c.ID == id {
			return true
		}
	}
	return false
}

// d は選んでいるカードを削除する。依頼の列でも y/N 確認を挟み (知らないキーは取り消し)、y ならすぐ消える。
func TestDeleteRequestedCardAsksFirst(t *testing.T) {
	m := New(fake.New(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)), []backend.Repo{{Name: "dotfiles"}})
	id := ""
	for _, c := range m.snap.Cards {
		if c.State == card.Requested {
			id = c.ID
		}
	}
	if id == "" {
		t.Fatal("見本に依頼の列のカードが要る")
	}
	m.selected = id
	press(m, "d")
	if m.mode != modeConfirm || !strings.Contains(m.confirmText, id) || !strings.Contains(m.confirmText, "すぐ消えます") {
		t.Fatalf("d で確認が出ない: mode=%v %q", m.mode, m.confirmText)
	}
	m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if !shown(m, id) {
		t.Fatal("n で取り消したのに消えた")
	}
	m.selected = id
	press(m, "d")
	m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if shown(m, id) || !strings.Contains(m.toasts.Text(), "削除した") {
		t.Fatalf("y の後も残っている: flash=%q", m.toasts.Text())
	}
}

// 処理中のカードは、確認に「PG の session を止めてから消える」と出し、y の後は「削除中」を出して、止めてから消える。
// 詳細を開いたままでも d が効く。
func TestDeleteRunningCardStopsPGFirst(t *testing.T) {
	be := fake.New(time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC))
	m := New(be, []backend.Repo{{Name: "dotfiles"}})
	m.selected = "C-005"
	press(m, "enter", "d")
	if m.mode != modeConfirm || !strings.Contains(m.confirmText, "PG の session を止めてから消えます") {
		t.Fatalf("処理中のカードの確認の文が違う: mode=%v %q", m.mode, m.confirmText)
	}
	m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if !shown(m, "C-005") || !strings.Contains(ansi.Strip(m.render()), "削除中") {
		t.Fatal("止める前に消えた / 削除中と出ない")
	}
	press(m, "d")
	if m.mode != modeBoard || !strings.Contains(m.toasts.Text(), "削除の依頼を受けている") {
		t.Fatalf("削除中のカードでまた確認を出した: mode=%v %q", m.mode, m.toasts.Text())
	}
	be.Step()
	m.setSnap(be.Snapshot())
	if shown(m, "C-005") {
		t.Fatal("PG を止めた後に消えない")
	}
}
