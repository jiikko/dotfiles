package foreground

import (
	"io"
	"os"
	"os/exec"

	"golang.org/x/sys/unix"
)

// Exec は端末を外のコマンド (エディタ・シェル) へ渡す bubbletea の ExecCommand (Run と SetStdin / SetStdout / SetStderr を持つ。
// bubbletea は import しない)。結果 (Was / Err) は Run の後にこの値へ入るので、値を手元に置いてから渡す:
//
//	x := foreground.NewExec(cmd)
//	return tea.Exec(x, func(err error) tea.Msg { return doneMsg{err: err, reclaimErr: x.Err} })
//
// `tea.Exec(foreground.NewExec(cmd), fn)` と直に書くと、取り戻せなかった理由 (Err) を読めずに黙って捨てる。
//
// 渡す前に前面を持っていたなら、子が終わった直後 (bubbletea が端末を入れ直す前) に前面を取り戻す。子 (の孫) の対話のシェルは
// job control のために前面を自分のグループへ移し、戻さずに終わることがある (`$SHELL -ic` が最後のコマンドを exec した等)。
// 前面でないまま入れ直すと、端末の設定を書くところで SIGTTOU で止まるか (前景ジョブの形)、EIO で何も読めない (session の先頭の形)。
// 持っていなかった前面は奪わない (裏に回された画面が勝手に前へ出ない)。戻ったときに前面が session の先頭 (= 画面を起こした外の
// シェル) にあるなら、それも奪わない: 子の実行中に Ctrl-Z → bg され、シェルが正当に取り戻した前面だから。奪うと、孤児になった
// シェルのグループの読みが EIO になる。画面自身が session の先頭 (tmux のペインで直に動く形) なら当たらない。
type Exec struct {
	Cmd *exec.Cmd // nil = 何も走らせない (bubbletea の手放し → 入れ直しだけを通す)
	// Owned・Reclaim は前面の確かめ方と取り戻し方 (テストが差し替える)。nil なら stdin の端末で確かめる (ゼロ値の Exec{Cmd: c} でも
	// 取り戻しが黙って消えない)
	Owned   func() bool
	Reclaim func() (int, error)
	Was     int   // 戻ったときに前面を持っていたグループ (0 = 外れていなかった)
	Err     error // 取り戻せなかった理由
}

// NewExec は stdin の端末で前面を確かめる Exec を作る。stdin が端末でなければ取り戻さない (bubbletea は stdin が端末でないとき
// /dev/tty を開いて読むが、その形の前面はここでは見ない。従来の tea.ExecProcess と同じく固まりうる)。
func NewExec(c *exec.Cmd) *Exec { return &Exec{Cmd: c} }

func stdinOwned() bool {
	fg, mine, err := Owner(int(os.Stdin.Fd()))
	return err == nil && fg == mine
}

func stdinReclaim() (int, error) {
	fd := int(os.Stdin.Fd())
	if shellTookBack(fd) {
		return 0, nil
	}
	return Reclaim(fd)
}

// shellTookBack は、前面が session の先頭のグループにあり、自分はその先頭でないか (= 外のシェルが前面を持っている)。
func shellTookBack(fd int) bool {
	fg, mine, err := Owner(fd)
	if err != nil || fg == mine {
		return false
	}
	sid, err := unix.Getsid(0)
	return err == nil && mine != sid && fg == sid
}

// Run は子を走らせ、渡す前に前面を持っていたなら戻った後に取り戻す。子の終了の err をそのまま返す (取り戻しの結果は Was・Err)。
func (x *Exec) Run() error {
	if x.Cmd == nil {
		return nil
	}
	owned, reclaim := x.Owned, x.Reclaim
	if owned == nil {
		owned = stdinOwned
	}
	if reclaim == nil {
		reclaim = stdinReclaim
	}
	held := owned()
	err := x.Cmd.Run()
	if held {
		x.Was, x.Err = reclaim()
	}
	return err
}

// dupscan:allow-group 3 intentional bubbletea の ExecCommand (SetStdin(io.Reader) / SetStdout(io.Writer) / SetStderr(io.Writer)) が 3 つを別の名前・別の引数の型で要求する。寄せる先の helper を足しても 3 つの口は残る
func (x *Exec) SetStdin(r io.Reader) {
	if x.Cmd != nil && x.Cmd.Stdin == nil {
		x.Cmd.Stdin = r
	}
}

func (x *Exec) SetStdout(w io.Writer) {
	if x.Cmd != nil && x.Cmd.Stdout == nil {
		x.Cmd.Stdout = w
	}
}

func (x *Exec) SetStderr(w io.Writer) {
	if x.Cmd != nil && x.Cmd.Stderr == nil {
		x.Cmd.Stderr = w
	}
}
