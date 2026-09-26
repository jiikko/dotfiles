package ui

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

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
