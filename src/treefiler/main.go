// treefiler は横に育つ木のファイラー (bin/treefiler)。glogx の F と同じ画面を単体で開く。
//
//	treefiler [DIR]   DIR (既定はカレントディレクトリ) を root にして開く
//
// 画面の部品は filer パッケージ (glogx からも使う)。ここは端末と toast の面倒だけを見る。
// 仕様は docs/treefiler-spec.md。
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/jiikko/dotfiles/src/tuikit/layout"
	"github.com/jiikko/dotfiles/src/tuikit/toast"

	"treefiler/filer"
)

// frameInterval は動いている間の描き直しの周期 (~60fps)。止まっている間は描かない。
const frameInterval = 16 * time.Millisecond

// busyInterval は裏の処理を待つ間の周期 (glogx の spinnerInterval と同じ)。
const busyInterval = 80 * time.Millisecond

type tickMsg struct{}

type app struct {
	f       *filer.Model
	toast   toast.Stack
	w, h    int
	ticking bool
}

func (a *app) Init() tea.Cmd { return nil }

func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmds := make([]tea.Cmd, 0, 4)
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
		a.f.Resize(a.w, a.h)
	case tea.KeyPressMsg:
		if a.f.HandleKey(msg.String()) == filer.Quit {
			return a, tea.Quit
		}
		for _, n := range a.f.TakeNotices() {
			a.toast.Show(n.Text, n.OK)
		}
	case toast.Msg:
		a.toast.StartLeaving(msg)
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
	return v
}

func main() {
	dir := "."
	switch len(os.Args) {
	case 1:
	case 2:
		dir = os.Args[1]
	default:
		fmt.Fprintln(os.Stderr, "usage: treefiler [DIR]")
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
