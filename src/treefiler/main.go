// treefiler は横に育つ木のファイラー (bin/treefiler)。glogx の F と同じ画面を単体で開く。
//
//	treefiler [DIR]   DIR (既定はカレントディレクトリ) を root にして開く
//
// 画面の部品は filer パッケージ (glogx からも使う)。ここは端末と toast の面倒だけを見る。
// 仕様は docs/treefiler-spec.md。
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/jiikko/dotfiles/src/tuikit/caret"
	"github.com/jiikko/dotfiles/src/tuikit/layout"
	"github.com/jiikko/dotfiles/src/tuikit/toast"

	"subproc"
	"treefiler/filer"
)

// frameInterval は動いている間の描き直しの周期 (~60fps)。止まっている間は描かない。
const frameInterval = 16 * time.Millisecond

// busyInterval は裏の処理を待つ間の周期 (glogx の spinnerInterval と同じ)。
const busyInterval = 80 * time.Millisecond

type tickMsg struct{}

// execDoneMsg は ! / s で起こしたプロセスから戻った合図。err は起動できなかったとき (終了コードは含めない)。
type execDoneMsg struct{ err error }

// changedMsg はライブ更新の合図 (ok=false はチャネルが閉じた = もう待たない)。
type changedMsg struct{ ok bool }

func waitChange(ch <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		_, ok := <-ch
		return changedMsg{ok}
	}
}

type app struct {
	f       *filer.Model
	toast   toast.Stack
	w, h    int
	ticking bool
}

func (a *app) Init() tea.Cmd { return waitChange(a.f.Changed()) }

func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmds := make([]tea.Cmd, 0, 4)
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
		a.f.Resize(a.w, a.h)
	case tea.PasteMsg:
		a.f.Paste(msg.Content) // 入力中だけ入る (filer が捨てる)
	case tea.KeyPressMsg:
		switch a.f.HandleInput(msg.String(), msg.Text) {
		case filer.Quit:
			a.f.Close() // 前回の場所を覚える (Remember place。savePlace は Close の中) と、ライブ更新の停止
			return a, tea.Quit
		case filer.Exec:
			if r, ok := a.f.TakeExec(); ok && len(r.Argv) > 0 {
				// 前景で端末を明け渡す。ctx は持たない (ユーザーのシェルが終わるまで待つのが仕様)。パイプは張らない
				cmd := subproc.CommandContext(context.Background(), r.Argv[0], r.Argv[1:]...)
				cmd.Dir = r.Dir
				cmd.Env = append(os.Environ(), r.Env...)
				cmds = append(cmds, tea.ExecProcess(cmd, func(err error) tea.Msg { return execDoneMsg{err} }))
			}
		case filer.None:
		}
		for _, n := range a.f.TakeNotices() {
			a.toast.Show(n.Text, n.OK)
		}
	case toast.Msg:
		a.toast.StartLeaving(msg)
	case execDoneMsg:
		a.f.Refresh() // シェルで作った・消したファイルを出す
		// 起動できなかったときだけ知らせる (作業フォルダが消えた・$SHELL が実行できない)。シェルは最後のコマンドの
		// 終了コードで抜けるのが普通なので、終了コードは失敗と言わない (glogx の editorClosedMsg と同じ判断)
		var exitErr interface{ ExitCode() int } // *exec.ExitError (os/exec は import しない。exec_boundary_test)
		if msg.err != nil && !errors.As(msg.err, &exitErr) {
			a.toast.Show("シェルを起動できませんでした: "+msg.err.Error(), false)
		}
	case changedMsg:
		if msg.ok {
			cmds = append(cmds, waitChange(a.f.Changed())) // 取り込みは下の Advance。また次の合図を待つ
		}
	case tickMsg:
		a.ticking = false
	}
	a.f.Advance(time.Now())
	for _, t := range a.toast.Advance() {
		cmds = append(cmds, tea.Tick(t.After, func(time.Time) tea.Msg { return t.Msg }))
	}
	if !a.ticking {
		switch {
		case a.f.Animating() || a.toast.Animating():
			a.ticking = true
			cmds = append(cmds, tea.Tick(frameInterval, func(time.Time) tea.Msg { return tickMsg{} }))
		case a.f.Busy():
			// 裏の走査・git の取得の結果を取り込むための遅い周期 (動いていなければ描き直しも少ない)
			a.ticking = true
			cmds = append(cmds, tea.Tick(busyInterval, func(time.Time) tea.Msg { return tickMsg{} }))
		}
	}
	return a, tea.Batch(cmds...)
}

func (a *app) View() tea.View {
	lines := a.f.View()
	if box := a.toast.BoxLines(true, 6, a.w); len(box) > 0 && len(lines) > len(box) {
		// 右下 (ステータスバーの上) に重ねる
		lines = layout.OverlayRight(lines, box, a.w, true, len(lines)-1-len(box))
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	if x, y, ok := a.f.CaretPos(); ok {
		v.Cursor = caret.At(x, y, a.w, a.h) // 入力欄に端末のカーソルを置く (IME の変換中の文字が欄に出るように)
	}
	return v
}

const usage = `usage: treefiler [DIR]

DIR (既定はカレントディレクトリ) を root にして、横に育つ木のファイラーを開く。
キーの一覧は中で ? を押す。設定は , (~/.config/glogx/treefiler.toml)。
`

func main() {
	dir := "."
	switch len(os.Args) {
	case 1:
	case 2:
		if os.Args[1] == "-h" || os.Args[1] == "--help" {
			fmt.Print(usage)
			return
		}
		dir = os.Args[1]
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	f, err := filer.New(dir, filer.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "treefiler:", err)
		os.Exit(1)
	}
	if _, err := tea.NewProgram(&app{f: f}).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "treefiler:", err)
		os.Exit(1)
	}
}
