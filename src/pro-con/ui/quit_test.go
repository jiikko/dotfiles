package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"
)

// quitBy は Q で終了の入力欄を開き、text を打って enter する。最後のコマンドを返す。
func quitBy(m *Model, text string) tea.Cmd {
	press(m, "Q")
	typeText(m, text)
	return press(m, "enter")
}

// 終了は Q で開く入力欄に quit と打って enter したときだけ。それ以外の文字・空・esc では閉じない (打ち間違いで dispatcher と PG を止めない)。
func TestQuitOnlyByTypingQuit(t *testing.T) {
	for _, text := range []string{"", "q", "quit!", "Quit", "yes"} {
		m := New(newSpy(), nil)
		if isQuit(quitBy(m, text)) || m.mode != modeBoard {
			t.Fatalf("%q で閉じた / 入力欄が残った: mode=%v", text, m.mode)
		}
	}
	m := New(newSpy(), nil)
	press(m, "Q")
	if m.mode != modeInput || !strings.Contains(ansi.Strip(m.render()), "quit と打って") {
		t.Fatal("Q で終了の入力欄が開かない")
	}
	if isQuit(press(m, "esc")) || m.mode != modeBoard {
		t.Fatal("esc で閉じた / 取り消せない")
	}
	if !isQuit(quitBy(New(newSpy(), nil), "quit")) {
		t.Fatal("quit と打って enter しても閉じない")
	}
}

// q と ctrl+c の 1 打では閉じない。ctrl+c は終了の入力欄を開くだけ。入力の途中の ctrl+c は書きかけの文を消さない。
func TestSingleKeysDoNotQuit(t *testing.T) {
	m := New(newSpy(), nil)
	if isQuit(press(m, "q")) || isQuit(press(m, "ctrl+c")) || m.mode != modeInput || m.inputKind != inputQuit {
		t.Fatalf("q / ctrl+c の 1 打で閉じた / ctrl+c で終了の入力欄が開かない: mode=%v", m.mode)
	}
	m = New(newSpy(), nil)
	press(m, "n")
	typeText(m, "書きかけ")
	if isQuit(press(m, "ctrl+c")) || m.inputKind != inputNew || m.line.String() != "書きかけ" {
		t.Fatalf("入力の途中の ctrl+c で書きかけを消した: kind=%v line=%q", m.inputKind, m.line.String())
	}
}

// 終了の入力欄には、止める PG の本数を出す (本物のモードでは dispatcher と PG を止めて閉じる)。
func TestQuitLabelShowsBusyPGs(t *testing.T) {
	m := New(&stopSpy{spy: newSpy()}, nil) // 作業中 1・質問待ち 2
	press(m, "Q")
	if got := ansi.Strip(m.render()); !strings.Contains(got, "作業中 1 本・質問待ち 2 本") {
		t.Fatal("終了の入力欄に止める PG の本数が出ない")
	}
}

// stopSpy は止める口を持つ backend (本物のモードの形)。
type stopSpy struct {
	*spy
	calls int
	err   error
}

func (s *stopSpy) StopAll(context.Context) error { s.calls++; return s.err }

// runStop は quitNow が返したコマンドを走らせ、その結果を画面に渡す (止め終えた知らせ → 終了)。
func runStop(t *testing.T, m *Model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		t.Fatal("止めるコマンドを返さない")
	}
	msg := cmd()
	if _, ok := msg.(stopDoneMsg); !ok {
		t.Fatalf("止め終えた知らせではない: %T", msg)
	}
	_, next := m.Update(msg)
	return next
}

// 本物のモードでは、終了の前に dispatcher と PG を止める。止めている間は「止めています」を出し、止め終えてから閉じる。
func TestQuitStopsBackendBeforeClosing(t *testing.T) {
	be := &stopSpy{spy: newSpy()}
	m := New(be, nil)
	cmd := quitBy(m, "quit")
	if !m.stopping || !strings.Contains(ansi.Strip(m.render()), "止めています") || be.calls != 0 {
		t.Fatalf("止めている最中を出さない / 閉じる前に止め終えた扱い: stopping=%v calls=%d", m.stopping, be.calls)
	}
	if !isQuit(runStop(t, m, cmd)) || be.calls != 1 {
		t.Fatalf("止め終えても閉じない / 止めていない: calls=%d", be.calls)
	}
}

// 止めている間は他のキーを受けない。ctrl+c だけは待たずに閉じる。
func TestQuitWhileStoppingIgnoresKeys(t *testing.T) {
	be := &stopSpy{spy: newSpy()}
	m := New(be, nil)
	quitBy(m, "quit")
	if cmd := press(m, "n"); cmd != nil || m.mode != modeBoard || !m.stopping {
		t.Fatal("止めている間にキーを受けた")
	}
	if !isQuit(press(m, "ctrl+c")) {
		t.Fatal("止めている間の ctrl+c で閉じない")
	}
}

// 止めきれなかった理由は、閉じた後に main が出せるように残す。
func TestQuitKeepsStopError(t *testing.T) {
	be := &stopSpy{spy: newSpy(), err: errors.New("2 本の PG を止められなかった")}
	m := New(be, nil)
	if !isQuit(runStop(t, m, quitBy(m, "quit"))) || m.StopErr() == nil {
		t.Fatalf("止めきれなかった理由を残さない: %v", m.StopErr())
	}
}

// viewSpy は読み取りだけの backend (backend.ReadOnly。止める口を持たない)。
type viewSpy struct{ *spy }

func (viewSpy) ReadOnly() {}

// 見ているだけの画面の終了の見出しは、この画面を閉じるだけで何も止めないと案内する
// (止める口が無いこと自体は live の TestViewOnlyBackend と main の TestWireLiveViewStopsAndStartsNothing が型で見る)。
func TestViewOnlyQuitStopsNothing(t *testing.T) {
	m := New(viewSpy{spy: newSpy()}, nil)
	if l := m.quitLabel(); !strings.Contains(l, "見ているだけ") || strings.Contains(l, "止めて閉じる") {
		t.Fatalf("見ているだけの画面で止めると案内した: %q", l)
	}
}
