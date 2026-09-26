package ui

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"pro-con/foreground"
)

// 端末を外のコマンドへ渡して戻る処理と、止められた後に端末を入れ直す処理 (issue 518)。
//
// 画面は前面で端末を読み続けるので、端末の前面 (前面のプロセスグループ) が外れると SIGTTIN で止まる (シェルに「suspended (tty input)」)。
// 止められている間にシェルが端末を cooked に戻すので、fg で続けても bubbletea は raw・alt screen・キーの設定を入れ直さず、
// 画面は描き直されず、キーも効かない。
//   - 外のコマンドから戻ったとき: 渡す前に前面を持っていて、戻ったら外れていれば取り戻す (子が前面を移したまま終わった)
//   - SIGCONT を受けたとき (main が Continued を送る): bubbletea の手放し → 入れ直しを、何も走らせないコマンドで通す
//     (外のコマンドから戻るときと同じ経路なので、端末の設定と描き直しが揃う)
// どちらも出来事に残す (onTermEvent)。起きたら経路を調べる手がかりにする。

// Continued は、画面が止められた後に続けられた (SIGCONT) ことを main から知らせる。
type Continued struct{}

// termExec は端末を外のコマンドへ渡す tea.ExecCommand。
type termExec struct {
	cmd     *exec.Cmd           // nil = 何も走らせない (止められた後の入れ直し)
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

// termResumedMsg は止められた後の入れ直しが済んだ知らせ。
type termResumedMsg struct{ err error }

// execOnTerminal は外のコマンドに端末を渡す (m.execProcess の既定)。
// 🚨 子の Stdout を端末 (os.Stdout) に固定する。bubbletea は子の Stdout が空なら画面の出力を渡す。main は画面の出力を
// upgrade.Screen で包むので (*os.File でない)、そのまま渡ると os/exec がパイプを挟み、attach の claude やエディタの stdout が端末でなくなる。
func execOnTerminal(c *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
	if c.Stdout == nil {
		c.Stdout = os.Stdout
	}
	return execTerm(newTermExec(c), fn)
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
	if msg.err != nil {
		s := "止められた後 (SIGCONT) に端末を入れ直せなかった: " + msg.err.Error()
		m.fail(s)
		m.termEvent(s)
		return
	}
	// 止められた理由はここでは分からない (前面を外されて SIGTTIN / 外のコマンドの中で ctrl+z → fg でも、画面ごと止まって SIGCONT が来る)
	m.info("止められていた画面を描き直した")
	m.termEvent("止められた後 (SIGCONT) に端末を入れ直して描き直した (外のコマンドの中の ctrl+z → fg でも出る)")
}
