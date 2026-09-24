package ui

import (
	"strings"
	"testing"
)

// 入力欄を開いている間は、カンバンの領域を 1 色 (暗い灰) で描く。カードの地の色 (bg) は消え、入力欄の行だけに地の色が敷かれる。
func TestBoardDimsWhileTyping(t *testing.T) {
	m := New(newSpy(), nil)
	m.width, m.height = 120, 30
	bgOf := func() (board, input int) {
		lines := strings.Split(m.render(), "\n")
		for i, l := range lines {
			if !strings.Contains(l, "\x1b[48;5;") {
				continue
			}
			if i >= headerRows && i < len(lines)-len(m.footGroup()) {
				board++
			} else if strings.Contains(l, bg(236)) {
				input++
			}
		}
		return board, input
	}
	if b, _ := bgOf(); b == 0 {
		t.Fatal("前提: ボードのときはカードに地の色がある")
	}
	press(m, "n")
	if m.mode != modeInput {
		t.Fatal("前提: n で入力欄が開くはず")
	}
	if b, in := bgOf(); b != 0 || in != 1 {
		t.Fatalf("入力中なのにカンバンに地の色が %d 行残っている / 入力欄の行の地の色 %d 行", b, in)
	}
	press(m, "esc")
	if b, _ := bgOf(); b == 0 {
		t.Fatal("入力欄を閉じてもカンバンが暗いまま")
	}
}
