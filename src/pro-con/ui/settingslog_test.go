package ui

import (
	"strings"
	"testing"
	"time"

	"pro-con/eventlog"
	"termsafe"
)

// logSpy はログのタブが読む backend (backend.EventLog)。pending を次の Events で渡し、呼ばれた回数を数える。
// changed があれば変化を知らせる backend (backend.Notifier) になる (notifyLogSpy)。
type logSpy struct {
	*inspSpy
	pending []eventlog.Event
	calls   int
}

func (s *logSpy) Events() ([]eventlog.Event, error) {
	s.calls++
	evs := s.pending
	s.pending = nil
	return evs, nil
}

type notifyLogSpy struct {
	*logSpy
	changed chan struct{}
}

func (s notifyLogSpy) Changed() <-chan struct{} { return s.changed }

func newLogSpy() *logSpy {
	s := &logSpy{inspSpy: newInspSpy()}
	t0 := s.snap.Now.Add(-time.Hour)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	s.pending = []eventlog.Event{
		{At: at(0), Kind: eventlog.KindDispatcher, Reason: "dispatcher が起きた (pid 1・手で起動した)"},
		{At: at(1), Kind: eventlog.KindApply, Card: "C-002", Reason: "C-002: 着手待ちへ"},
		{At: at(10), Kind: eventlog.KindSupervisor, Reason: "dispatcher が落ちた (signal: killed。10m0s の間に 1 回目)。10s 後に起こし直す"},
	}
	for i := range 4 { // 3 秒ごとの同じ失敗 (1 行に畳まれる)
		s.pending = append(s.pending, eventlog.Event{At: at(20 + 3*i), Kind: eventlog.KindLaunch, Card: "C-003", Reason: "C-003 の PG を起動できない: repo の場所が設定に無い"})
	}
	return s
}

// openLogTab は設定画面を開いてログのタブへ移る。
func openLogTab(t *testing.T, s *logSpy) *Model {
	t.Helper()
	m := openSettingsFor(t, s)
	for range len(m.settingsTabs()) {
		if m.set.tab == tabLog {
			break
		}
		run(m, press(m, "tab"))
	}
	if m.set.tab != tabLog {
		t.Fatalf("ログのタブが無い: %v", m.settingsTabs())
	}
	return m
}

// ログのタブはプロセスの出来事を時刻順 (新しいほど下) に 1 行ずつ出し、繰り返しを畳む。カードの出来事は c で混ぜる。
func TestSettingsLogTab(t *testing.T) {
	s := newLogSpy()
	m := openLogTab(t, s)
	scr := setScreen(m)
	iUp, iDown, iFail := strings.Index(scr, "dispatcher が起きた"), strings.Index(scr, "dispatcher が落ちた"), strings.Index(scr, "起動できない")
	if iUp < 0 || iDown < iUp || iFail < iDown {
		t.Fatalf("プロセスの出来事が時刻順に出ない:\n%s", scr)
	}
	if strings.Count(scr, "起動できない") != 1 || !strings.Contains(scr, "… ×4 回、最後") {
		t.Fatalf("繰り返しを畳まない:\n%s", scr)
	}
	if strings.Contains(scr, "着手待ちへ") {
		t.Fatalf("カードの出来事を既定で出した:\n%s", scr)
	}
	for _, w := range []string{"supervisor", "C-003", "3 行 (畳む前 6 件)"} {
		if !strings.Contains(scr, w) {
			t.Fatalf("%q が無い:\n%s", w, scr)
		}
	}
	if m.set.cursor != 2 {
		t.Fatalf("開いたときに最新 (一番下) を選ばない: cursor=%d", m.set.cursor)
	}
	press(m, "c")
	if scr := setScreen(m); !strings.Contains(scr, "着手待ちへ") || !strings.Contains(scr, "c でプロセスの出来事だけにする") {
		t.Fatalf("c でカードの出来事を混ぜない:\n%s", scr)
	}
	press(m, "c")
	if strings.Contains(setScreen(m), "着手待ちへ") {
		t.Fatal("もう一度 c でプロセスの出来事だけに戻らない")
	}
}

// 開いている間に足された出来事は、変化の知らせで読み進めて出る (最新を選んでいれば付いていく)。ログのタブが見えていなければ読まない。
func TestSettingsLogFollowsChanges(t *testing.T) {
	s := newLogSpy()
	n := notifyLogSpy{logSpy: s, changed: make(chan struct{}, 1)}
	m := openSettingsFor(t, n)
	calls := s.calls
	m.Update(changedMsg{})
	if s.calls != calls {
		t.Fatalf("ログのタブが見えていないのに読んだ (%d → %d)", calls, s.calls)
	}
	for m.set.tab != tabLog {
		run(m, press(m, "tab"))
	}
	s.pending = []eventlog.Event{{At: s.snap.Now, Kind: eventlog.KindDispatcher, Reason: "dispatcher が起きた (pid 2・画面か supervisor が起こした)"}}
	_, cmd := m.Update(changedMsg{})
	n.changed <- struct{}{} // 次の知らせを待つ cmd (waitChanged) を run の中で返させる
	run(m, cmd)
	if scr := setScreen(m); !strings.Contains(scr, "pid 2") {
		t.Fatalf("開いている間に足された出来事が出ない:\n%s", scr)
	}
	if last := len(m.set.log.rows) - 1; m.set.cursor != last {
		t.Fatalf("最新に付いていかない: cursor=%d last=%d", m.set.cursor, last)
	}
	press(m, "k") // 上へ送ったら、足されても動かない
	cur := m.set.cursor
	s.pending = []eventlog.Event{{At: s.snap.Now.Add(time.Second), Kind: eventlog.KindDispatcher, Reason: "dispatcher が抜ける (pid 2・rc=0)"}}
	_, cmd = m.Update(changedMsg{})
	n.changed <- struct{}{}
	run(m, cmd)
	if m.set.cursor != cur {
		t.Fatalf("上へ送った後に足されて選んでいる行が動いた: %d → %d", cur, m.set.cursor)
	}
	// 描くだけでは読まない
	calls = s.calls
	for range 3 {
		_ = m.render()
	}
	if s.calls != calls {
		t.Fatalf("描くたびに読んだ (%d → %d)", calls, s.calls)
	}
}

// 行が窓より多くても、最新を選んだまま枠の見出し (題・列の名前) を残し、行だけを送る。
func TestSettingsLogKeepsHeaderWhenScrolled(t *testing.T) {
	s := newLogSpy()
	for i := range 80 {
		s.pending = append(s.pending, eventlog.Event{At: s.snap.Now.Add(time.Duration(i) * 5 * time.Minute), Kind: eventlog.KindDispatcher, Reason: "dispatcher が起きた (pid " + strings.Repeat("9", i%3+1) + ")"})
	}
	m := openLogTab(t, s)
	scr := setScreen(m)
	if !strings.Contains(scr, "プロセスの出来事") || !strings.Contains(scr, "出来事") || !strings.Contains(scr, " 時刻") {
		t.Fatalf("送ったときに枠の見出しが消えた:\n%s", scr)
	}
	if !strings.Contains(scr, "▌"+s.snap.Now.Add(79*5*time.Minute).Local().Format("01-02 15:04:05")) {
		t.Fatalf("最新の行を選んで出していない:\n%s", scr)
	}
}

// 上へ送って見ている間に c で切り替えても、見ていた時刻の辺りから動かない (最新へ飛ばない)。
func TestSettingsLogToggleKeepsPosition(t *testing.T) {
	s := newLogSpy()
	m := openLogTab(t, s)
	press(m, "k") // 「dispatcher が落ちた」の行
	if got := m.set.log.rows[m.set.cursor].Last.Reason; !strings.HasPrefix(got, "dispatcher が落ちた") {
		t.Fatalf("前提: 選んでいる行 = %q", got)
	}
	for range 2 {
		press(m, "c")
		if got := m.set.log.rows[m.set.cursor].Last.Reason; !strings.HasPrefix(got, "dispatcher が落ちた") {
			t.Fatalf("c で見ていた位置が動いた: 選んでいる行 = %q (showCards=%v)", got, m.set.log.showCards)
		}
	}
}

// ログのタブ: 選んでいる行だけに下線を引き (途中の色の戻しの後も引き直す)、y は行を、Y は出来事の文だけを写す。
func TestSettingsLogSelectedUnderlineAndYank(t *testing.T) {
	s := newLogSpy()
	m := openLogTab(t, s)
	var copied []string
	m.copy = func(v string) error { copied = append(copied, v); return nil }
	f := m.set.log.rows[m.set.cursor] // 開いたときは最新 (畳んだ「起動できない」×4)
	sel, other := m.logRow(f, true, 160), m.logRow(f, false, 160)
	if !strings.Contains(sel, sgrUnderline) || strings.Contains(other, sgrUnderline) {
		t.Fatalf("選んだ行だけに下線を引かない:\nsel=%q\nother=%q", sel, other)
	}
	if strings.Count(sel, sgrReset) != strings.Count(sel, sgrReset+sgrUnderline) {
		t.Fatalf("色の戻しの後に下線を引き直さない: %q", sel)
	}
	press(m, "y", "Y")
	if len(copied) != 2 {
		t.Fatalf("y / Y で写さない: %q", copied)
	}
	reason := termsafe.PlainLine(f.Last.Reason)
	if copied[1] != reason {
		t.Fatalf("Y が出来事の文だけでない: %q (want %q)", copied[1], reason)
	}
	for _, w := range []string{f.Last.At.Local().Format("01-02 15:04:05"), f.Role, reason, "×4 回"} {
		if !strings.Contains(copied[0], w) {
			t.Fatalf("y の行に %q が無い: %q", w, copied[0])
		}
	}
	if strings.Contains(copied[0], "\x1b") {
		t.Fatalf("y の行に色の列が混ざる: %q", copied[0])
	}
}
