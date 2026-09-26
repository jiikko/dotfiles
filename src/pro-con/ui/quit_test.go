package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"pro-con/backend"
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

// 見ているだけの画面の終了のダイアログは、この画面を閉じるだけで何も止めないと案内する
// (止める口が無いこと自体は live の TestViewOnlyBackend と main の TestWireLiveViewStopsAndStartsNothing が型で見る)。
func TestViewOnlyQuitStopsNothing(t *testing.T) {
	m := New(viewSpy{spy: newSpy()}, nil)
	if l := quitText(m); !strings.Contains(l, "見ているだけ") || strings.Contains(l, "止めて閉じる") {
		t.Fatalf("見ているだけの画面で止めると案内した: %q", l)
	}
}

// quitText は終了のダイアログの中身 (色を落として 1 つの文字列に)。
func quitText(m *Model) string { return ansi.Strip(strings.Join(m.quitBody(), "\n")) }

// paneSpy は端末を tmux の pane の名前に直す口を持つ backend (本物のモードの形)。
type paneSpy struct {
	*stopSpy
	panes  map[string]string
	during func() // 問い合わせの最中に呼ぶ (任意)
}

func (p *paneSpy) PaneNames(context.Context) map[string]string {
	if p.during != nil {
		p.during()
	}
	return p.panes
}

// openQuit は Q で終了の入力欄を開き、pane の名前を引くコマンドを走らせて画面に渡す。
func openQuit(t *testing.T, m *Model) {
	t.Helper()
	if cmd := press(m, "Q"); cmd != nil {
		m.Update(cmd())
	}
	if m.mode != modeInput || m.inputKind != inputQuit {
		t.Fatalf("Q で終了の入力欄が開かない: mode=%v", m.mode)
	}
}

// 持ち主が 2 つ: Q でダイアログが出て、「動いたまま」と、ほかの画面の居場所 (tmux の中なら pane の名前、外なら tty) が読める。
// 閉じると何が起きるかは入力欄の見出しに書かない (ダイアログと 2 か所に持たない = issue 519)。
func TestQuitDialogShowsOtherScreens(t *testing.T) {
	be := &paneSpy{stopSpy: &stopSpy{spy: newSpy()}, panes: map[string]string{"/dev/ttys003": "main:2.1", "/dev/ttys007": "main:0.0"}}
	now := be.snap.Now
	be.snap.Screens = []backend.Screen{
		{ID: "aaaaaa", TTY: "/dev/ttys003", Opened: now.Add(-time.Hour)},
		{ID: "bbbbbb", TTY: "/dev/ttys007", Opened: now, Self: true},
		{ID: "cccccc", Join: true, Label: "review", TTY: "/dev/ttys012", Opened: now},
	}
	m := New(be, nil)
	openQuit(t, m)
	lines := strings.Split(ansi.Strip(m.render()), "\n")
	screen := strings.Join(lines, "\n")
	for _, want := range []string{"ほかに持ち主の画面が 1 開いている", "この画面だけ閉じる。動いたまま", "dispatcher · supervisor · PM · 取り込みの係", "作業中 1 本・質問待ち 2 本",
		"ほかに開いている画面", "main:2.1 (tmux)", "join review", "/dev/ttys012"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("ダイアログに %q が無い:\n%s", want, screen)
		}
	}
	if strings.Contains(screen, "main:0.0") {
		t.Fatalf("この画面をほかの画面として出した:\n%s", screen)
	}
	head := lines[m.inputRow]
	if !strings.Contains(head, "quit と打って enter") || strings.Contains(head, "動いたまま") || strings.Contains(head, "止め") {
		t.Fatalf("入力欄の見出しが残っていない / ダイアログの中身を二重に書いた: %q", head)
	}
	typeText(m, "quit")
	if !isQuit(runStop(t, m, press(m, "enter"))) {
		t.Fatal("閉じ方 (quit と打って enter) が変わった")
	}
}

// 最後の持ち主: 止めるもの (作業中・質問待ちの PG の本数と dispatcher) が並ぶ。join の画面だけが残るなら、止めた後に表示が止まると添える。
// pane を引けない (tmux の外・引く口が無い) 端末は tty のまま出す。
func TestQuitDialogLastOwnerListsWhatStops(t *testing.T) {
	be := &stopSpy{spy: newSpy()}
	be.snap.Screens = []backend.Screen{{ID: "aaaaaa", Self: true}, {ID: "cccccc", Join: true, TTY: "/dev/ttys012"}}
	m := New(be, nil)
	openQuit(t, m)
	screen := ansi.Strip(m.render())
	for _, want := range []string{"最後の持ち主の画面なので、止めて閉じる", "PG (作業中 1 本・質問待ち 2 本)", "dispatcher", "次に開くと続きから",
		"/dev/ttys012", "join の画面は、止めた後は表示が止まる"} {
		if !strings.Contains(screen, want) {
			t.Fatalf("ダイアログに %q が無い:\n%s", want, screen)
		}
	}
	be.snap.Screens = be.snap.Screens[:1]
	if l := quitText(New(be, nil)); strings.Contains(l, "ほかに開いている画面") || strings.Contains(l, "表示が止まる") {
		t.Fatalf("ほかに画面が無いのに一覧・join の注記を出した: %q", l)
	}
}

// pane の名前は開くたびに引き直す (前に開いたときの表を使わない。pane は動く)。tmux が答えるまでは tty のまま出し、
// 前の回の問い合わせが後から返っても新しい回の表を上書きしない。問い合わせは裏の処理として数える (入れ替え (ctrl+r) が待つ)。
func TestQuitDialogRefreshesPanes(t *testing.T) {
	be := &paneSpy{stopSpy: &stopSpy{spy: newSpy()}, panes: map[string]string{"/dev/ttys003": "main:2.1"}}
	be.snap.Screens = []backend.Screen{{ID: "aaaaaa", TTY: "/dev/ttys003"}, {ID: "bbbbbb", Self: true}}
	m := New(be, nil)
	be.during = func() {
		if n := m.children.Load(); n != 1 {
			t.Errorf("pane の問い合わせを裏の処理として数えない: %d", n)
		}
	}
	openQuit(t, m)
	press(m, "esc")
	be.panes = map[string]string{"/dev/ttys003": "work:1.0"}
	stale := press(m, "Q")
	if l := quitText(m); !strings.Contains(l, "/dev/ttys003") || strings.Contains(l, "main:2.1") {
		t.Fatalf("前に開いたときの pane の名前を出した: %q", l)
	}
	staleMsg := stale()
	press(m, "esc")
	be.panes = map[string]string{"/dev/ttys003": "work:3.2"}
	m.Update(press(m, "Q")())
	m.Update(staleMsg) // 前の回が後から返る
	if l := quitText(m); !strings.Contains(l, "work:3.2 (tmux)") {
		t.Fatalf("pane の名前を引き直さない / 前の回の答えで上書きした: %q", l)
	}
}
