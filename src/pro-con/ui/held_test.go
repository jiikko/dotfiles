package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"pro-con/backend"
)

// 人が dispatcher を止めた印があれば、ゲージに「止めてある」と出し、c → y/N で起こす (issue 459)。N なら何も送らない。
// 印が無ければ c は確認を出さない (動いている dispatcher を起こす確認に y を押させない)。
func TestResumeHeldDispatcher(t *testing.T) {
	be := newSpy()
	m := New(be, nil)
	if g := ansi.Strip(m.gauge()); strings.Contains(g, "止めてある") {
		t.Fatalf("印が無いのに止めてあると出す: %q", g)
	}
	press(m, "c")
	if m.mode != modeBoard || len(be.applied) != 0 {
		t.Fatalf("印が無いのに c で確認へ進んだ / 送った: mode=%v applied=%v", m.mode, be.applied)
	}

	be.snap.DispatcherHeld = true
	m.snap = be.snap
	if g := ansi.Strip(m.gauge()); !strings.Contains(g, "dispatcher 止めてある (c で起こす)") {
		t.Fatalf("止めてあると出さない: %q", g)
	}
	if h := ansi.Strip(strings.Join(m.hints(), " ")); !strings.Contains(h, "c dispatcher を起こす") {
		t.Fatalf("案内に c を出さない: %q", h)
	}
	press(m, "c")
	m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if len(be.applied) != 0 {
		t.Fatalf("n で取り消したのに送った: %v", be.applied)
	}
	press(m, "c")
	if m.mode != modeConfirm || !strings.Contains(m.confirmText, "起こします") {
		t.Fatalf("c で確認が出ない: mode=%v %q", m.mode, m.confirmText)
	}
	m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if len(be.applied) != 1 || be.applied[0] != (backend.ResumeDispatcher{}) {
		t.Fatalf("y で起こす操作を送らない: %v", be.applied)
	}
}
