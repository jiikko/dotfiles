package ui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"pro-con/foreground"
)

// 端末を外のコマンドへ渡して戻る処理と、裏に回されても (止められても) 壊れないための処理 (issue 518)。
//
// 画面は前面で端末を読み続け、端末を raw・alt screen にしている。何もしないと、止まり方ごとに次のように壊れる:
//   - 前面が外れて端末を読む → SIGTTIN で止まる。止まっている間にシェルが端末を cooked に戻すので、fg で続けても
//     bubbletea は raw・alt screen・キーの設定を入れ直さず、画面は描き直されず、キーも効かない
//   - 外からの SIGTSTP (端末が cooked の間の ctrl+z も) → 端末を raw・alt screen のまま止まり、シェルが使えない
//   - 画面の中の ctrl+z は raw なので信号でなくキーで届く → 何も起きない
// 次のように扱う (StopWatcher が SIGTSTP・SIGTTIN・SIGTTOU・SIGCONT を見張る):
//   - ctrl+z と SIGTSTP: bubbletea の Exec で端末を戻し (手放し)、1 行出してから自分を止める。fg で Exec が戻り、bubbletea が
//     端末を入れ直して描き直す。外のコマンドに端末を渡している間の SIGTSTP は、その場で止まる (子と一緒に裏へ回る)
//   - SIGTTIN・SIGTTOU (前面でないのに端末を読んだ・設定を書いた): 前面でないので端末の設定 (raw) は戻せないが、書くことはできる。
//     端末に入れた設定 (alt screen・キーの拡張ほか) を戻す列と 1 行を書いてから止まる。fg の SIGCONT で入れ直す (次の項)
//   - 自分で止まった後でない SIGCONT (SIGTTIN・SIGTTOU・外からの SIGSTOP で止まっていた): 何も走らせない Exec で
//     手放し → 入れ直しを通して描き直す (外のコマンドから戻るときと同じ経路なので、端末の設定と描き直しが揃う)
//   - 外のコマンドから戻ったとき: 渡す前に前面を持っていて、戻ったら外れていれば取り戻す (子が前面を移したまま終わった)
// 止められていたこと・前面を取り戻したことは出来事に残す (onTermEvent)。起きたら経路を調べる手がかりにする。

// Continued は、自分で止めていないのに続けられた (SIGCONT) ことを知らせる (WatchStops が送る)。
type Continued struct{}

// suspendMsg は、画面を止める (裏に回す) ことを知らせる (ctrl+z のキー・WatchStops が受けた SIGTSTP)。
type suspendMsg struct{}

// suspendNote は止まるときに端末へ出す 1 行 (画面は止まっても、作業は別のプロセスで続く)。
const suspendNote = "pro-con: 画面を止めた (fg で戻る)。dispatcher と PG は別のプロセスで動き続ける"

// ttyStopNote は端末の前面を外されて止まるときに出す 1 行。
const ttyStopNote = "pro-con: 端末の前面を外されたので画面を止めた (fg で戻る)。dispatcher と PG は別のプロセスで動き続ける"

// leaveTerminalModes は画面が端末に入れた設定を戻す列 (bubbletea の renderer が終わるときに書くものと同じ並び。
// キーの拡張 → alt screen を抜ける → カーソルを出す → 貼り付け・フォーカス・マウスの報告を切る)。前面でなくても書ける。
var leaveTerminalModes = ansi.ResetModifyOtherKeys + ansi.KittyKeyboard(0, 1) + "\x1b[?1049l" + "\x1b[?25h" +
	ansi.ResetModeBracketedPaste + ansi.ResetModeFocusEvent + ansi.ResetModeMouseButtonEvent + ansi.ResetModeMouseAnyEvent + ansi.ResetModeMouseExtSgr

// stopState は止まり方の見張り (WatchStops の goroutine) と画面の間で分け合う状態。
type stopState struct {
	handedOver atomic.Bool  // 端末を外のコマンドに渡している・自分で止まろうとしている (SIGTSTP を画面の手順へ回さず、その場で止まる)
	suspending atomic.Bool  // 止まる手順 (suspendExec) を頼んだ・走らせている (続けて届いた SIGTSTP を読み飛ばす)
	expectCont atomic.Int32 // 自分で止まった回数。その後の SIGCONT は入れ直しを頼まない (止まった Exec が戻るとき bubbletea が入れ直す)
	kill       func()       // 自分を止める (nil なら SIGSTOP。テストは差し替える)
	tty        io.Writer    // 端末 (SIGTTIN・SIGTTOU で止まる前に設定を戻す列を書く先。nil なら os.Stdout)
	background func() bool  // 端末の前面を外れているか (nil なら stdin の前面を見る。テストは差し替える)
}

func (s *stopState) inBackground() bool {
	if s.background != nil {
		return s.background()
	}
	fg, mine, err := foreground.Owner(int(os.Stdin.Fd()))
	return err == nil && fg != mine
}

// stopSelf は自分を止める。SIGTSTP は見張りが受けてしまうので、受けられない SIGSTOP で止まる (シェルには「suspended (signal)」と出る)。
func (s *stopState) stopSelf() {
	s.expectCont.Add(1)
	s.stop()
}

// stop は、続けられたときに入れ直しを頼むつもりで止まる (SIGCONT を読み飛ばさない)。
func (s *stopState) stop() {
	if s.kill != nil {
		s.kill()
		return
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGSTOP)
}

// takeExpectedCont は、自分で止まった後の SIGCONT なら数を減らして true を返す。
func (s *stopState) takeExpectedCont() bool {
	for {
		n := s.expectCont.Load()
		if n <= 0 {
			return false
		}
		if s.expectCont.CompareAndSwap(n, n-1) {
			return true
		}
	}
}

// StopWatcher は SIGTSTP・SIGTTIN・SIGTTOU・SIGCONT の見張り。main がプロセスに 1 つ置き、Program を走らせる間だけ Attach する。
// 🚨 外さない (signal.Stop しない): Go の runtime は Stop の後もこれらの信号の handler を残し、誰も受けていない信号を捨てる
// (既定の動作 = 止まる、に戻らない。敵対的レビューで実測)。Program の外 (509 の切り替え中・終わる間際) では見張りが自分で止まる。
type StopWatcher struct {
	m    *Model
	send atomic.Pointer[func(tea.Msg)] // 走っている Program へ知らせる (nil = Program の外)
}

// WatchStops は見張りを置く (プロセスに 1 回)。
// 🚨 SIGCONT は別のチャンネルで受ける: 前面でないまま端末を読むと SIGTTIN が連発し、同じチャンネルだと溢れて fg の SIGCONT が捨てられる。
func (m *Model) WatchStops() *StopWatcher {
	w := &StopWatcher{m: m}
	stops, cont := make(chan os.Signal, 4), make(chan os.Signal, 1)
	signal.Notify(stops, syscall.SIGTSTP, syscall.SIGTTIN, syscall.SIGTTOU)
	signal.Notify(cont, syscall.SIGCONT)
	go func() {
		for {
			var sig os.Signal
			select {
			case sig = <-stops:
			case sig = <-cont:
			}
			m.onSignal(sig, w.sender())
		}
	}()
	return w
}

// Attach は p を走らせる間、見張りから p へ知らせる。Detach で外す (p.Run が戻ったら呼ぶ)。
func (w *StopWatcher) Attach(p *tea.Program) {
	f := p.Send
	w.send.Store(&f)
}

func (w *StopWatcher) Detach() {
	w.send.Store(nil)
	w.m.stops.suspending.Store(false)
}

// sender は見張りから Program へ知らせる関数 (Program の外なら nil)。
// 🚨 待たずに送る: 外のコマンドを走らせている間は Program の event loop が止まっていて Send が戻らない。見張りが待つと、その間の SIGTSTP を扱えない
func (w *StopWatcher) sender() func(tea.Msg) {
	f := w.send.Load()
	if f == nil {
		return nil
	}
	return func(msg tea.Msg) { go (*f)(msg) }
}

// onSignal は見張りが受けた信号を振り分ける (send は Program へ知らせる。nil = Program の外)。
func (m *Model) onSignal(sig os.Signal, send func(tea.Msg)) {
	switch sig {
	case syscall.SIGTSTP:
		switch {
		case send == nil: // Program の外: 戻す端末の設定は bubbletea が持っていない。既定の動作の代わりにその場で止まる
			m.stops.stop()
		case m.stops.handedOver.Load(): // 端末は子が持っている (か止まる途中): 画面の手順 (Exec) は回らないので、その場で止まる
			m.stops.stopSelf()
		case m.stops.suspending.CompareAndSwap(false, true): // 続けて届いた分で 2 度止まらない (止まる手順が走り終えるまで)
			send(suspendMsg{})
		}
	case syscall.SIGTTIN, syscall.SIGTTOU:
		// 🚨 前面にいるなら何もしない。前面でないまま端末を読むと、read が繰り返されて SIGTTIN が何度も届く。溜まった分を
		// fg で続けられた後に読んで止まり直さない (前面にいれば端末を触っても止まらないので、守るものも無い)
		if !m.stops.inBackground() {
			return
		}
		if send != nil && !m.stops.handedOver.Load() { // 子が端末を持っている間・Program の外では、端末へ書かない
			tty := m.stops.tty
			if tty == nil {
				tty = os.Stdout
			}
			_, _ = io.WriteString(tty, leaveTerminalModes+"\r\n"+ttyStopNote+"\r\n")
		}
		m.stops.stop()
	case syscall.SIGCONT:
		if !m.stops.takeExpectedCont() && send != nil {
			send(Continued{})
		}
	}
}

// suspendExec は画面を止める tea.ExecCommand。bubbletea が端末を戻した後に 1 行出して止まり、fg で戻る。
type suspendExec struct {
	stops *stopState
	out   io.Writer
}

func (x *suspendExec) Run() error {
	x.stops.handedOver.Store(true)
	defer func() { x.stops.handedOver.Store(false); x.stops.suspending.Store(false) }()
	_, _ = fmt.Fprintln(x.out, suspendNote)
	x.stops.stopSelf()
	return nil
}

func (x *suspendExec) SetStdin(io.Reader)  {}
func (x *suspendExec) SetStdout(io.Writer) {}
func (x *suspendExec) SetStderr(io.Writer) {}

// suspend は画面を止める (裏に回す)。
func (m *Model) suspend() tea.Cmd {
	m.stops.suspending.Store(true)
	return tea.Exec(&suspendExec{stops: m.stops, out: os.Stderr}, func(err error) tea.Msg { return termResumedMsg{suspended: true, err: err} })
}

// termExec は端末を外のコマンドへ渡す tea.ExecCommand。
type termExec struct {
	cmd     *exec.Cmd           // nil = 何も走らせない (止められた後の入れ直し)
	stops   *stopState          // 子を走らせている間 handedOver を立てる (nil なら立てない)
	owned   func() bool         // 今、端末の前面を持っているか
	reclaim func() (int, error) // 前面が外れていたら取り戻す。取り戻す前の前面のグループを返す (0 = 外れていなかった)
	was     int                 // 戻ったときに前面を持っていたグループ (0 = 外れていなかった)
	err     error               // 取り戻せなかった理由
}

// newTermExec は画面の端末 (stdin) で前面を確かめる termExec を作る。stdin が端末でなければ確かめない。
func newTermExec(c *exec.Cmd) *termExec {
	fd := int(os.Stdin.Fd())
	return &termExec{
		cmd: c,
		owned: func() bool {
			fg, mine, err := foreground.Owner(fd)
			return err == nil && fg == mine
		},
		reclaim: func() (int, error) { return foreground.Reclaim(fd) },
	}
}

// Run は bubbletea が端末を手放した後に呼ぶ。戻った後、bubbletea が端末を入れ直す (RestoreTerminal) より前に前面を取り戻す
// (前面でないまま入れ直すと、端末の設定を書くところで SIGTTOU で止まる)。
func (x *termExec) Run() error {
	if x.cmd == nil {
		return nil
	}
	held := x.owned()
	if x.stops != nil {
		x.stops.handedOver.Store(true)
		defer x.stops.handedOver.Store(false)
	}
	err := x.cmd.Run()
	if held {
		x.was, x.err = x.reclaim()
	}
	return err
}

func (x *termExec) SetStdin(r io.Reader) {
	if x.cmd != nil && x.cmd.Stdin == nil {
		x.cmd.Stdin = r
	}
}

func (x *termExec) SetStdout(w io.Writer) {
	if x.cmd != nil && x.cmd.Stdout == nil {
		x.cmd.Stdout = w
	}
}

func (x *termExec) SetStderr(w io.Writer) {
	if x.cmd != nil && x.cmd.Stderr == nil {
		x.cmd.Stderr = w
	}
}

// termReturnMsg は外のコマンドから戻った知らせ。前面が外れていたら、その様子を出来事に残してから inner を届ける。
type termReturnMsg struct {
	inner tea.Msg
	name  string // 外のコマンドの名前 (出来事に書く)
	was   int
	err   error
}

// termResumedMsg は止まった後の入れ直しが済んだ知らせ。
type termResumedMsg struct {
	err       error
	suspended bool // 自分で止まった (ctrl+z・SIGTSTP)。false = 止められていた (SIGCONT を受けた)
}

// execOnTerminal は外のコマンドに端末を渡す (m.execProcess の既定)。
// 🚨 子の Stdout を端末 (os.Stdout) に固定する。bubbletea は子の Stdout が空なら画面の出力を渡す。main は画面の出力を
// upgrade.Screen で包むので (*os.File でない)、そのまま渡ると os/exec がパイプを挟み、attach の claude やエディタの stdout が端末でなくなる。
func (m *Model) execOnTerminal(c *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
	if c.Stdout == nil {
		c.Stdout = os.Stdout
	}
	x := newTermExec(c)
	x.stops = m.stops
	return execTerm(x, fn)
}

func execTerm(x *termExec, fn tea.ExecCallback) tea.Cmd { return tea.Exec(x, termReturn(x, fn)) }

// termReturn は外のコマンドから戻ったときの知らせを作る (x が戻ったときに取り戻したかを載せる)。
func termReturn(x *termExec, fn tea.ExecCallback) tea.ExecCallback {
	return func(err error) tea.Msg {
		var inner tea.Msg
		if fn != nil {
			inner = fn(err)
		}
		return termReturnMsg{inner: inner, name: filepath.Base(x.cmd.Path), was: x.was, err: x.err}
	}
}

// OnTerminalEvent は端末の前面が外れた・止められた後に入れ直した、を出来事に残す関数を置く (main が live の画面の出来事へ繋ぐ)。
func (m *Model) OnTerminalEvent(f func(string)) { m.onTermEvent = f }

func (m *Model) termEvent(s string) {
	if m.onTermEvent != nil {
		m.onTermEvent(s)
	}
}

// onTermReturn は外のコマンドから戻ったときに前面が外れていたら知らせて残し、inner を次に届ける。
func (m *Model) onTermReturn(msg termReturnMsg) tea.Cmd {
	switch {
	case msg.err != nil:
		s := fmt.Sprintf("%s から戻ったら端末の前面が外れていて、取り戻せなかった (%v)。止まったら fg で戻す", msg.name, msg.err)
		m.fail(s)
		m.termEvent(s)
	case msg.was != 0:
		m.termEvent(fmt.Sprintf("%s から戻ったら端末の前面がグループ %d に移っていたので取り戻した", msg.name, msg.was))
	}
	if msg.inner == nil {
		return nil
	}
	return func() tea.Msg { return msg.inner }
}

// onContinued は止められた後に続けられたとき、端末を入れ直して描き直す。
func (m *Model) onContinued() tea.Cmd {
	return tea.Exec(&termExec{}, func(err error) tea.Msg { return termResumedMsg{err: err} })
}

func (m *Model) onTermResumed(msg termResumedMsg) {
	// 入れ直しの途中で前面を外れていた (fg でなく bg で続けた等): 端末の設定を書くところで SIGTTOU を受けて EINTR で返る。
	// 見張りがそのまま止まり、次の fg の SIGCONT で入れ直すので、失敗として出さない (隔離 tmux・試作で EINTR を確かめた)
	if errors.Is(msg.err, syscall.EINTR) {
		return
	}
	if msg.suspended { // 人が止めて戻した: 出来事には残さない (経路を調べる手がかりにならない)
		if msg.err != nil {
			m.fail("止めた画面を戻せなかった: " + msg.err.Error())
		}
		return
	}
	if msg.err != nil {
		s := "止められた後 (SIGCONT) に端末を入れ直せなかった: " + msg.err.Error()
		m.fail(s)
		m.termEvent(s)
		return
	}
	// 自分で止まった後の SIGCONT は数えて読み飛ばすので、ここへ来るのは外から止められていたとき (SIGTTIN・SIGTTOU・SIGSTOP)
	m.info("止められていた画面を描き直した")
	m.termEvent("止められた後 (SIGCONT) に端末を入れ直して描き直した (SIGTTIN・SIGTTOU・外からの SIGSTOP で止められていた)")
}
