package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"pro-con/card"
)

// 作業中・質問待ちのカードがあるとき、q / ctrl+c はすぐ終了せず、中央に確認のダイアログを出す。
// 入力欄・y/N 確認・issue の板の ctrl+c も同じ確認を通る。
func TestQuitAsksWhenCardsAreBusy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*Model)
		key   string
	}{
		{"ボードの q", func(*Model) {}, "q"},
		{"ボードの ctrl+c", func(*Model) {}, "ctrl+c"},
		{"入力欄の ctrl+c", func(m *Model) { press(m, "n") }, "ctrl+c"},
		{"issue の板の ctrl+c", func(m *Model) { m.picker.open = true }, "ctrl+c"},
	} {
		m := New(newSpy(), nil)
		tc.setup(m)
		if isQuit(press(m, tc.key)) {
			t.Fatalf("%s: 作業中のカードがあるのに確認なしで終了した", tc.name)
		}
		if !m.quitAsk || !strings.Contains(ansi.Strip(m.render()), "本当に終了しますか") {
			t.Fatalf("%s: 確認のダイアログが出ない", tc.name)
		}
	}
}

// 確認では y / Enter / もう一度の ctrl+c で終了し、それ以外は取り消し (知らないキーで終了しない)。
func TestQuitDialogKeys(t *testing.T) {
	for _, tc := range []struct {
		key  string
		quit bool
	}{{"y", true}, {"enter", true}, {"ctrl+c", true}, {"n", false}, {"esc", false}, {"j", false}, {"Y", false}} {
		m := New(newSpy(), nil)
		press(m, "q")
		got := isQuit(press(m, tc.key))
		if got != tc.quit || m.quitAsk {
			t.Fatalf("確認中の %q: 終了=%v (期待 %v) / まだ確認中=%v", tc.key, got, tc.quit, m.quitAsk)
		}
	}
}

// 動いているカードが無ければ、確認を出さずにすぐ終了する。
func TestQuitImmediatelyWhenIdle(t *testing.T) {
	be := newSpy()
	be.snap.Cards = []card.Card{{ID: "D1", State: card.Done, Ending: card.EndAnswered}, {ID: "P1", State: card.Planned}}
	m := New(be, nil)
	if !isQuit(press(m, "q")) || m.quitAsk {
		t.Fatal("動いているカードが無いのに確認を出した")
	}
}

// ダイアログの文は板の幅で切れない (切れると、何件動いているか・何が起きるかが読めない)。
func TestQuitDialogFitsWithoutTruncation(t *testing.T) {
	m := New(newSpy(), nil)
	press(m, "q")
	out := ansi.Strip(m.render())
	for _, want := range []string{"作業中 1 枚・質問待ち 2 枚", "本当に終了しますか?", "終了すると進み具合は消えます"} {
		if !strings.Contains(out, want) {
			t.Fatalf("ダイアログに %q が切れずに出ていない:\n%s", want, out)
		}
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

// 本物のモードでは、終了の前に daemon と PG を止める。止めている間は「止めています」を出し、止め終えてから閉じる。
func TestQuitStopsBackendBeforeClosing(t *testing.T) {
	be := &stopSpy{spy: newSpy()}
	be.snap.Cards = []card.Card{{ID: "D1", State: card.Done, Ending: card.EndAnswered}}
	m := New(be, nil)
	cmd := press(m, "q")
	if !m.stopping || !strings.Contains(ansi.Strip(m.render()), "止めています") || be.calls != 0 {
		t.Fatalf("止めている最中を出さない / 閉じる前に止め終えた扱い: stopping=%v calls=%d", m.stopping, be.calls)
	}
	if !isQuit(runStop(t, m, cmd)) || be.calls != 1 {
		t.Fatalf("止め終えても閉じない / 止めていない: calls=%d", be.calls)
	}
}

// 動いている PG があれば、止める前に確認する。y で止めて閉じ、それ以外は止めない。
func TestQuitConfirmsBeforeStoppingBusyPGs(t *testing.T) {
	for _, tc := range []struct {
		key  string
		stop bool
	}{{"y", true}, {"n", false}} {
		be := &stopSpy{spy: newSpy()} // 作業中 1・質問待ち 2
		m := New(be, nil)
		press(m, "q")
		if !m.quitAsk || !strings.Contains(ansi.Strip(m.render()), "PG と daemon を止めて終了しますか") {
			t.Fatalf("%s: 動いている PG があるのに確認を出さない", tc.key)
		}
		cmd := press(m, tc.key)
		if tc.stop {
			if !isQuit(runStop(t, m, cmd)) || be.calls != 1 {
				t.Fatalf("y で止めて閉じない: calls=%d", be.calls)
			}
		} else if cmd != nil && isQuit(cmd) || be.calls != 0 || m.stopping {
			t.Fatalf("取り消したのに止めた / 閉じた: calls=%d stopping=%v", be.calls, m.stopping)
		}
	}
}

// 止めている間は他のキーを受けない。ctrl+c だけは待たずに閉じる。
func TestQuitWhileStoppingIgnoresKeys(t *testing.T) {
	be := &stopSpy{spy: newSpy()}
	be.snap.Cards = nil
	m := New(be, nil)
	press(m, "q")
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
	be.snap.Cards = nil
	m := New(be, nil)
	if !isQuit(runStop(t, m, press(m, "q"))) || m.StopErr() == nil {
		t.Fatalf("止めきれなかった理由を残さない: %v", m.StopErr())
	}
}
