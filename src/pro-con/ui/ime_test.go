package ui

import (
	"strings"
	"testing"

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
	head, _, _ := m.inputParts()
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
