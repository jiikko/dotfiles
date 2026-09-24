package ui

import (
	"errors"
	"strings"
	"testing"

	"pro-con/card"
)

func yankSpy(t *testing.T, c card.Card) (*Model, *[]string) {
	t.Helper()
	be := newSpy()
	be.snap.Cards = []card.Card{c}
	m := New(be, nil)
	var copied []string
	m.copy = func(s string) error { copied = append(copied, s); return nil }
	return m, &copied
}

// y は選択中のカードの ID・タイトル・状態・issue・依頼の原文をちょうど 1 回コピーする。質問待ちなら質問も入れる。
func TestYankCopiesSelectedCard(t *testing.T) {
	c := card.Card{ID: "C-003", Title: "通知が重なる", Request: "toast が 2 つ同時に出ると\n読めない", Repo: "dotfiles",
		State: card.Waiting, Wait: card.Wait{Kind: card.WaitQuestion, Question: "縦に積みますか？"},
		Issues: []card.IssueRef{{Repo: "dotfiles", Number: 921, Status: "open"}}}
	m, copied := yankSpy(t, c)
	press(m, "y")
	if len(*copied) != 1 {
		t.Fatalf("コピーはちょうど 1 回のはず: %d", len(*copied))
	}
	got := (*copied)[0]
	if !strings.HasPrefix(got, "C-003 通知が重なる\n") {
		t.Fatalf("1 行目が ID とタイトルでない:\n%s", got)
	}
	for _, want := range []string{"dotfiles#921 (open)", "質問待ち", "toast が 2 つ同時に出ると\n読めない", "縦に積みますか？"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q が入っていない:\n%s", want, got)
		}
	}
}

// 本文のエスケープシーケンス・制御文字は落とす (貼った先の端末で発火させない)。改行は残す。
func TestYankStripsControlSequences(t *testing.T) {
	evil := "前\x1b]52;c;ZXZpbA==\x07中\x1b[31m赤\x1b[0m\r後\n次の行"
	m, copied := yankSpy(t, card.Card{ID: "C-001", Title: "t", Request: evil, State: card.Planned})
	press(m, "y")
	got := (*copied)[0]
	if strings.ContainsAny(got, "\x1b\x07\r") {
		t.Fatalf("制御文字が残っている: %q", got)
	}
	if !strings.Contains(got, "前") || !strings.Contains(got, "\n次の行") {
		t.Fatalf("本文か改行が消えた: %q", got)
	}
}

func TestYankReportsFailure(t *testing.T) {
	m, _ := yankSpy(t, card.Card{ID: "C-001", Title: "t", State: card.Planned})
	m.copy = func(string) error { return errors.New("pbcopy が無い") }
	press(m, "y")
	if !strings.Contains(m.flash, "失敗") {
		t.Fatalf("失敗を通知していない: %q", m.flash)
	}
}
