package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// 入力欄では端末のカーソルをキャレットに置く (IME は変換中の文字をカーソルの位置に出す)。行は入力欄の行、桁は全角を 2 と数えた
// キャレットの前までの幅。キャレットを戻すとカーソルも戻る。入力欄を閉じるとカーソルを隠す。
func TestCaretFollowsInputForIME(t *testing.T) {
	m := New(newSpy(), nil)
	if m.View().Cursor != nil {
		t.Fatal("ボードでカーソルが出ている")
	}
	press(m, "n")
	typeText(m, "日本語ab")
	lines := strings.Split(m.View().Content, "\n")
	c := m.View().Cursor
	if c == nil {
		t.Fatal("入力欄を開いてもカーソルを置かない")
	}
	row := ansi.Strip(lines[c.Y])
	head, _, _ := m.inputField()
	if !strings.Contains(row, "日本語ab") || !strings.HasPrefix(row, ansi.Strip(head)) {
		t.Fatalf("カーソルの行 %d が入力欄ではない: %q", c.Y, row)
	}
	want := ansi.StringWidth(ansi.Strip(head)) + 3*2 + 2
	if c.X != want {
		t.Fatalf("カーソルの桁 %d (キャレットの直後は %d。全角は 2 セル)", c.X, want)
	}
	press(m, "left", "left", "left")
	if c := m.View().Cursor; c.X != want-1-1-2 {
		t.Fatalf("キャレットを 3 つ戻したのにカーソルの桁が %d", c.X)
	}
	press(m, "esc")
	if m.View().Cursor != nil {
		t.Fatal("入力欄を閉じてもカーソルが残る")
	}
}

// どの送る欄でも、全角の文字を欄の幅より長く書いても、端末のカーソルは画面の中で、書いた最後の文字の直後に居る
// (前を切って欄に収める。カーソルが欄の外や文字とずれた所に居ると、IME の変換中の文字もそこに出る。issue 517)。
func TestCaretStaysAfterLastCharForLongText(t *testing.T) {
	for _, f := range sendFields() {
		t.Run(f.name, func(t *testing.T) {
			m, _ := f.open(t)
			m.Update(tea.PasteMsg{Content: strings.Repeat("日本語の長い文", 30)})
			typeText(m, "おわり")
			v := m.View()
			c := v.Cursor
			if c == nil {
				t.Fatal("書く欄に居るのに端末のカーソルが無い")
			}
			if c.X >= m.width {
				t.Fatalf("カーソルが画面の外 (%d / 幅 %d)", c.X, m.width)
			}
			row := ansi.Strip(strings.Split(v.Content, "\n")[c.Y])
			if before := ansi.Truncate(row, c.X, ""); !strings.HasSuffix(before, "おわり") {
				t.Fatalf("カーソル (%d) の直前が書いた最後の文字でない: %q", c.X, before)
			}
		})
	}
}
