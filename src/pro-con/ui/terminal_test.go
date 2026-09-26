package ui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// 外のコマンドへ渡す前に前面を持っていたときだけ、戻った後に取り戻す (持っていなかった前面を奪わない)。
func TestTermExecReclaimsOnlyWhenItHeldForeground(t *testing.T) {
	for _, held := range []bool{true, false} {
		called := false
		x := &termExec{
			cmd:     exec.Command("true"),
			owned:   func() bool { return held },
			reclaim: func() (int, error) { called = true; return 4242, nil },
		}
		if err := x.Run(); err != nil {
			t.Fatal(err)
		}
		if called != held {
			t.Errorf("held=%v: 取り戻しを呼んだか = %v", held, called)
		}
		if held && x.was != 4242 {
			t.Errorf("取り戻す前の前面を覚えていない: %d", x.was)
		}
	}
}

// 止められた後の入れ直しは何も走らせない (bubbletea の手放し → 入れ直しだけを通す)。
func TestTermExecWithoutCommandRunsNothing(t *testing.T) {
	x := &termExec{}
	if err := x.Run(); err != nil {
		t.Fatal(err)
	}
}

// 戻ったときに前面が外れていたら出来事に残し、元の知らせ (エディタ・attach の戻り) はそのまま届ける。
func TestTermReturnRecordsLostForegroundAndPassesInner(t *testing.T) {
	m := New(newSpy(), nil)
	var events []string
	m.OnTerminalEvent(func(s string) { events = append(events, s) })

	inner := editorDoneMsg{path: "x.md"}
	cmd := m.onTermReturn(termReturnMsg{inner: inner, name: "nvim", was: 4242})
	if cmd == nil || cmd() != inner {
		t.Fatal("元の知らせを届けない")
	}
	if len(events) != 1 || !strings.Contains(events[0], "nvim") || !strings.Contains(events[0], "4242") {
		t.Fatalf("取り戻したことを残していない: %q", events)
	}

	events = nil
	m.onTermReturn(termReturnMsg{inner: inner, name: "nvim"})
	if len(events) != 0 {
		t.Fatalf("前面が外れていないのに残した: %q", events)
	}

	m.onTermReturn(termReturnMsg{name: "claude", was: 4242, err: errors.New("EPERM")})
	if len(events) != 1 || !strings.Contains(events[0], "取り戻せなかった") || !strings.Contains(m.toasts.Text(), "fg") {
		t.Fatalf("取り戻せなかったことを残して出していない: events=%q toast=%q", events, m.toasts.Text())
	}
}

// 戻りの知らせは、外のコマンドの名前と、戻ったときに取り戻した前面を載せ、元の知らせを包む。
func TestTermReturnCarriesReclaimResult(t *testing.T) {
	x := &termExec{cmd: exec.Command("/usr/bin/true"), was: 4242, err: errors.New("x")}
	msg, ok := termReturn(x, func(error) tea.Msg { return editorDoneMsg{path: "p"} })(nil).(termReturnMsg)
	if !ok || msg.name != "true" || msg.was != 4242 || msg.err == nil || msg.inner != (editorDoneMsg{path: "p"}) {
		t.Fatalf("戻りの知らせが足りない: %+v", msg)
	}
	if msg := termReturn(x, nil)(nil).(termReturnMsg); msg.inner != nil {
		t.Fatalf("知らせの関数が無いのに中身がある: %+v", msg)
	}
}

// SIGCONT (main の Continued) を受けたら端末を入れ直し、済んだら画面と出来事に出す。
func TestContinuedRestoresTerminalAndRecords(t *testing.T) {
	m := New(newSpy(), nil)
	var events []string
	m.OnTerminalEvent(func(s string) { events = append(events, s) })
	// 入れ直しは bubbletea の Exec (手放し → 入れ直し) を通す。Exec の知らせの型は非公開なので名前で見る
	if cmd := m.onContinued(); cmd == nil || fmt.Sprintf("%T", cmd()) != "tea.execMsg" {
		t.Fatal("端末を入れ直す Exec を返さない")
	}
	m.Update(termResumedMsg{})
	if len(events) != 1 || !strings.Contains(events[0], "SIGCONT") || !strings.Contains(m.toasts.Text(), "描き直した") {
		t.Fatalf("入れ直したことを出していない: events=%q toast=%q", events, m.toasts.Text())
	}
}

// 見張りの振り分け: 端末を持っているときの SIGTSTP は画面の手順 (Exec で端末を戻して止まる) へ回し、外のコマンドに渡している間は
// その場で止まる。自分で止まった後の SIGCONT は読み飛ばし、そうでない SIGCONT で入れ直しを頼む。
func TestOnSignalRoutesStops(t *testing.T) {
	m := New(newSpy(), nil)
	stopped := 0
	m.stops.kill = func() { stopped++ }
	var sent []tea.Msg
	send := func(msg tea.Msg) { sent = append(sent, msg) }

	m.onSignal(syscall.SIGTSTP, send)
	if len(sent) != 1 || sent[0] != (suspendMsg{}) || stopped != 0 {
		t.Fatalf("端末を持っているときの SIGTSTP: sent=%v stopped=%d", sent, stopped)
	}

	sent = nil
	m.stops.handedOver.Store(true)
	m.onSignal(syscall.SIGTSTP, send)
	m.stops.handedOver.Store(false)
	if len(sent) != 0 || stopped != 1 {
		t.Fatalf("外のコマンドに渡している間の SIGTSTP はその場で止まる: sent=%v stopped=%d", sent, stopped)
	}

	m.onSignal(syscall.SIGCONT, send) // 自分で止まった後
	if len(sent) != 0 {
		t.Fatalf("自分で止まった後の SIGCONT で入れ直しを頼んだ: %v", sent)
	}
	m.onSignal(syscall.SIGCONT, send) // 外から止められていた
	if len(sent) != 1 || sent[0] != (Continued{}) {
		t.Fatalf("外から止められていた後の SIGCONT で入れ直しを頼まない: %v", sent)
	}
}

// 画面の中の ctrl+z は、どの画面からでも端末を戻して止まる Exec を返す。
func TestCtrlZSuspends(t *testing.T) {
	m := New(newSpy(), nil)
	if cmd := m.suspend(); cmd == nil || fmt.Sprintf("%T", cmd()) != "tea.execMsg" {
		t.Fatal("止める Exec を返さない")
	}
	m.mode = modeInput
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if cmd == nil || !strings.Contains(fmt.Sprintf("%v", collectMsgTypes(cmd)), "tea.execMsg") {
		t.Fatalf("入力欄から ctrl+z で止まらない: %v", collectMsgTypes(cmd))
	}
}

// collectMsgTypes は cmd (Batch を含む) が出す知らせの型の名前を集める。
func collectMsgTypes(cmd tea.Cmd) []string {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []string
		for _, c := range b {
			out = append(out, collectMsgTypes(c)...)
		}
		return out
	}
	return []string{fmt.Sprintf("%T", msg)}
}

// 止まるときは 1 行出してから止まり、止まっている間は SIGTSTP をその場で止まる側へ回す。戻った後は元に戻す。人が止めて戻したことは出来事に残さない。
func TestSuspendExecNotesAndStops(t *testing.T) {
	m := New(newSpy(), nil)
	var events []string
	m.OnTerminalEvent(func(s string) { events = append(events, s) })
	var during bool
	m.stops.kill = func() { during = m.stops.handedOver.Load() }
	var out strings.Builder
	if err := (&suspendExec{stops: m.stops, out: &out}).Run(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "dispatcher と PG は別のプロセスで動き続ける") || !during || m.stops.handedOver.Load() {
		t.Fatalf("out=%q during=%v after=%v", out.String(), during, m.stops.handedOver.Load())
	}
	m.Update(termResumedMsg{suspended: true})
	if len(events) != 0 {
		t.Fatalf("人が止めて戻したことを出来事に残した: %q", events)
	}
}

// SIGTTIN・SIGTTOU: 前面を外れていれば、端末の設定を戻す列と 1 行を書いてから止まり、続けられたら入れ直しを頼む。
// 前面にいるときに届いたもの (溜まった古い SIGTTIN) では止まらない。
func TestOnSignalTTINLeavesTerminalOnlyInBackground(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTTIN, syscall.SIGTTOU} {
		m := New(newSpy(), nil)
		stopped := 0
		bg := false
		var tty strings.Builder
		m.stops.kill = func() { stopped++ }
		m.stops.background = func() bool { return bg }
		m.stops.tty = &tty
		var sent []tea.Msg
		send := func(msg tea.Msg) { sent = append(sent, msg) }

		m.onSignal(sig, send)
		if stopped != 0 || tty.Len() != 0 {
			t.Fatalf("%v: 前面にいるのに止まった / 書いた: stopped=%d tty=%q", sig, stopped, tty.String())
		}

		bg = true
		m.onSignal(sig, send)
		if stopped != 1 || !strings.Contains(tty.String(), "\x1b[?1049l") || !strings.Contains(tty.String(), "前面を外された") {
			t.Fatalf("%v: 前面を外れているのに設定を戻して止まらない: stopped=%d tty=%q", sig, stopped, tty.String())
		}
		m.onSignal(syscall.SIGCONT, send)
		if len(sent) != 1 || sent[0] != (Continued{}) {
			t.Fatalf("%v: 続けられたときに入れ直しを頼まない: %v", sig, sent)
		}
	}
}

// SIGTSTP が続けて届いても、止まる手順は 1 度だけ頼む (fg の後にまた止まらない)。手順が走り終えたら次の SIGTSTP を受ける。
func TestOnSignalSuspendsOnceForRepeatedTSTP(t *testing.T) {
	m := New(newSpy(), nil)
	m.stops.kill = func() {}
	var sent []tea.Msg
	send := func(msg tea.Msg) { sent = append(sent, msg) }
	m.onSignal(syscall.SIGTSTP, send)
	m.onSignal(syscall.SIGTSTP, send)
	if len(sent) != 1 {
		t.Fatalf("止まる手順を %d 度頼んだ", len(sent))
	}
	_ = (&suspendExec{stops: m.stops, out: io.Discard}).Run()
	m.onSignal(syscall.SIGTSTP, send)
	if len(sent) != 2 {
		t.Fatalf("手順が走り終えた後の SIGTSTP を受けない: %d", len(sent))
	}
}

// Program の外 (509 の切り替え中など) の SIGTSTP・SIGTTIN では、その場で止まる (既定の動作の代わり)。端末へは書かない。
func TestOnSignalOutsideProgramStopsInPlace(t *testing.T) {
	m := New(newSpy(), nil)
	stopped := 0
	var tty strings.Builder
	m.stops.kill = func() { stopped++ }
	m.stops.background = func() bool { return true }
	m.stops.tty = &tty
	m.onSignal(syscall.SIGTSTP, nil)
	m.onSignal(syscall.SIGTTIN, nil)
	m.onSignal(syscall.SIGCONT, nil)
	if stopped != 2 || tty.Len() != 0 {
		t.Fatalf("stopped=%d tty=%q", stopped, tty.String())
	}
}

// 本物の信号で: 前面を外れて止まっている間 (見張りは止まる処理の中) に SIGTTIN が溜まっても、fg の SIGCONT は捨てられずに入れ直しを頼む。
func TestStopWatcherKeepsContinueAfterTTINFlood(t *testing.T) {
	m := New(newSpy(), nil)
	var bg atomic.Bool
	bg.Store(true)
	m.stops.background = bg.Load
	m.stops.tty = io.Discard
	stopped, release := make(chan struct{}), make(chan struct{})
	m.stops.kill = func() { close(stopped); <-release; bg.Store(false) } // 止まっている間 = 見張りはここで待つ。fg で前面に戻る
	got := make(chan tea.Msg, 16)
	w := m.WatchStops()
	f := func(msg tea.Msg) { got <- msg }
	w.send.Store(&f)
	defer w.Detach()
	_ = syscall.Kill(os.Getpid(), syscall.SIGTTIN)
	<-stopped
	for range 10 { // 前面でない read の繰り返しで SIGTTIN が溜まる
		_ = syscall.Kill(os.Getpid(), syscall.SIGTTIN)
		time.Sleep(5 * time.Millisecond)
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGCONT)
	time.Sleep(50 * time.Millisecond) // SIGCONT がチャンネルへ届いてから、見張りを止まる処理から戻す
	close(release)
	select {
	case msg := <-got:
		if msg != (Continued{}) {
			t.Fatalf("知らせが違う: %#v", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SIGCONT が捨てられ、入れ直しを頼まない")
	}
}

// 入れ直しの途中で前面を外れた (bg で続けた) ときの EINTR は失敗として出さない (見張りが止まり、次の fg で入れ直す)。
func TestTermResumedIgnoresEINTR(t *testing.T) {
	m := New(newSpy(), nil)
	var events []string
	m.OnTerminalEvent(func(s string) { events = append(events, s) })
	m.Update(termResumedMsg{err: fmt.Errorf("bubbletea: error restoring console: %w", syscall.EINTR)})
	m.Update(termResumedMsg{suspended: true, err: syscall.EINTR})
	if len(events) != 0 || strings.Contains(m.toasts.Text(), "できなかった") || strings.Contains(m.toasts.Text(), "せなかった") {
		t.Fatalf("EINTR を失敗として出した: events=%q toast=%q", events, m.toasts.Text())
	}
}
