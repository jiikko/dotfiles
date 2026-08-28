package main

import "testing"

func TestEditorMultibyteEditing(t *testing.T) {
	var e editor
	e.handle("", "あい")
	if e.value() != "あい" || e.pos != 2 {
		t.Fatalf("挿入後 = %q pos=%d", e.value(), e.pos)
	}
	// 全角の backspace は 1 文字単位 (バイト単位で消すと文字化けする)
	e.handle("backspace", "")
	if e.value() != "あ" {
		t.Errorf("backspace 後 = %q; want あ", e.value())
	}
	e.handle("", "う")
	if e.value() != "あう" {
		t.Errorf("= %q; want あう", e.value())
	}
	// カーソル列は表示幅 (全角 2 セル)
	if got := e.cursorCol(); got != 4 {
		t.Errorf("cursorCol = %d; want 4", got)
	}
	e.handle("left", "")
	if got := e.cursorCol(); got != 2 {
		t.Errorf("left 後の cursorCol = %d; want 2", got)
	}
	// カーソル位置への挿入
	e.handle("", "X")
	if e.value() != "あXう" {
		t.Errorf("中間挿入 = %q; want あXう", e.value())
	}
}

func TestEditorLineOps(t *testing.T) {
	var e editor
	e.setValue("make test now")
	e.handle("ctrl+w", "")
	if e.value() != "make test " {
		t.Errorf("ctrl+w = %q", e.value())
	}
	e.handle("ctrl+u", "")
	if e.value() != "" {
		t.Errorf("ctrl+u = %q", e.value())
	}
	e.setValue("あいう")
	e.handle("home", "")
	e.handle("delete", "")
	if e.value() != "いう" {
		t.Errorf("delete = %q; want いう", e.value())
	}
	e.handle("end", "")
	e.handle("ctrl+k", "")
	if e.value() != "いう" {
		t.Errorf("末尾での ctrl+k で値が変わった: %q", e.value())
	}
}
