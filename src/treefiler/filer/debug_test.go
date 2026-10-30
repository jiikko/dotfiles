package filer

import (
	"testing"
	"time"
)

// DebugState はカーソル (root からの相対パス)・押しっぱなし・欄の待ち・開いている層を写す (tools/tfperf の -snap が読む)。
func TestDebugStateReflectsModel(t *testing.T) {
	m := newTest(t)
	release := blockLoader(t, m)
	cdTo(t, m, "a/one.txt")
	m.Advance(m.lastAdv.Add(200 * time.Millisecond))
	s := m.DebugState()
	if s.Cursor != "a/one.txt" || s.CursorDir || s.Depth != 2 {
		t.Fatalf("カーソル: %q dir=%v depth=%d", s.Cursor, s.CursorDir, s.Depth)
	}
	if !s.PaneWaiting || s.PaneWidth != m.paneWidth() || s.PaneGen != m.pane.gen {
		t.Fatalf("欄: waiting=%v width=%d gen=%d", s.PaneWaiting, s.PaneWidth, s.PaneGen)
	}
	release()
	settlePane(t, m)
	if s := m.DebugState(); s.PaneWaiting || s.PaneShown != "a/one.txt" || !s.PaneFresh || s.PaneLines == 0 {
		t.Fatalf("届いた後の欄: %+v", s)
	}
	at := m.lastAdv.Add(time.Second)
	m.Advance(at)
	for range 3 {
		m.HandleKey("j")
		at = at.Add(30 * time.Millisecond)
		m.Advance(at)
	}
	if s := m.DebugState(); !s.Holding || s.Pulses != 0 {
		t.Fatalf("押しっぱなし: holding=%v pulses=%d", s.Holding, s.Pulses)
	}
	m.HandleKey("?")
	if s := m.DebugState(); s.Layer != "help" {
		t.Fatalf("キー一覧を開いても layer=%q", s.Layer)
	}
}
