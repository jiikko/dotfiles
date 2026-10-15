// mac-lan-manager は手元の Mac 同士を内部の網で繋いで管理する道具 (bin/mac-lan-manager)。glogx の L と同じ画面を単体で開く。
//
//	mac-lan-manager   一覧を開く (Bonjour で見えている Mac と、台帳に載っている Mac)
//
// 画面の部品は lanmgr パッケージ (glogx からも使う)。ここは端末と toast の面倒だけを見る。要件と決定は issue 747。
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/jiikko/dotfiles/src/tuikit/caret"
	"github.com/jiikko/dotfiles/src/tuikit/foreground"
	"github.com/jiikko/dotfiles/src/tuikit/layout"
	"github.com/jiikko/dotfiles/src/tuikit/termcolor"
	"github.com/jiikko/dotfiles/src/tuikit/toast"
	"github.com/jiikko/dotfiles/src/tuikit/widthenv"

	"mac-lan-manager/lanmgr"
	"mac-lan-manager/live"
	"subproc"
)

// busyInterval は走査の結果を取り込む周期 (spinner もこの周期で回る。glogx の spinnerInterval と同じ)。
const busyInterval = 80 * time.Millisecond

// frameInterval は toast が動いている間の描き直しの周期。
const frameInterval = 16 * time.Millisecond

type tickMsg struct{}

// changedMsg は走査の結果が変わった合図。ch はどのチャネルからか (ok=false はチャネルが閉じた = もう待たない)。
type changedMsg struct {
	ch <-chan struct{}
	ok bool
}

// waitChange は走査の合図を待つ (Busy の間しか tick を張らないので、後から見つかった Mac を出すにはこれを待つ)。
func waitChange(ch <-chan struct{}) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		_, ok := <-ch
		return changedMsg{ch, ok}
	}
}

type execDoneMsg struct{ err, reclaimErr error }

type app struct {
	watching <-chan struct{} // 今待っている走査の合図のチャネル (r で走査が替わったら待ち直す)
	m        *lanmgr.Model
	toast    toast.Stack
	w, h     int
	ticking  bool
}

func (a *app) Init() tea.Cmd { return tea.Batch(a.tick(), a.watch()) }

// watch は今の走査のチャネルにまだ待ちを張っていなければ張る (r が走査を起こし直すと、前に張った待ちは古いチャネルを見ている)。
func (a *app) watch() tea.Cmd {
	ch := a.m.Changed()
	if ch == nil || ch == a.watching {
		return nil
	}
	a.watching = ch
	return waitChange(ch)
}

func (a *app) tick() tea.Cmd {
	if a.ticking {
		return nil
	}
	switch {
	case a.toast.Animating():
		a.ticking = true
		return tea.Tick(frameInterval, func(time.Time) tea.Msg { return tickMsg{} })
	case a.m.Busy():
		a.ticking = true
		return tea.Tick(busyInterval, func(time.Time) tea.Msg { return tickMsg{} })
	}
	return nil
}

func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmds := make([]tea.Cmd, 0, 4)
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
		a.m.Resize(a.w, a.h)
	case tea.PasteMsg:
		a.m.Paste(msg.Content)
	case tea.KeyPressMsg:
		switch a.m.HandleInput(msg.String(), msg.Text) {
		case lanmgr.Quit:
			a.m.Close() // 自分が起こした dns-sd を止める
			return a, tea.Quit
		case lanmgr.Exec:
			if r, ok := a.m.TakeExec(); ok && len(r.Argv) > 0 {
				// 前景で端末を明け渡す (ssh はパスワードとホスト鍵の確認に端末が要る)。戻った後に前面が外れていたら取り戻す
				cmd := subproc.CommandContext(context.Background(), r.Argv[0], r.Argv[1:]...)
				x := foreground.NewExec(cmd)
				cmds = append(cmds, tea.Exec(x, func(err error) tea.Msg { return execDoneMsg{err: err, reclaimErr: x.Err} }))
			}
		case lanmgr.None:
		}
	case toast.Msg:
		a.toast.StartLeaving(msg)
	case execDoneMsg:
		if w := a.m.ExecDone(msg.err); w != "" {
			a.toast.Show(w, false)
		}
		if msg.reclaimErr != nil {
			a.toast.Show("端末の前面を取り戻せませんでした。止まったら fg で戻してください", false)
		}
	case tickMsg:
		a.ticking = false
	case changedMsg:
		if msg.ok && msg.ch == a.watching {
			cmds = append(cmds, waitChange(msg.ch)) // 取り込みは下の Advance。また次の合図を待つ
		}
	}
	a.m.Advance(time.Now())
	for _, n := range a.m.TakeNotices() {
		a.toast.Show(n.Text, n.OK)
	}
	for _, t := range a.toast.Advance() {
		cmds = append(cmds, tea.Tick(t.After, func(time.Time) tea.Msg { return t.Msg }))
	}
	cmds = append(cmds, a.tick(), a.watch())
	return a, tea.Batch(cmds...)
}

func (a *app) View() tea.View {
	lines := a.m.View()
	if box := a.toast.BoxLines(true, 6, a.w); len(box) > 0 && len(lines) > len(box)+2 {
		lines = layout.OverlayRight(lines, box, a.w, true, len(lines)-3-len(box)) // 下のキーの案内を隠さない
	}
	v := tea.NewView(strings.Join(lines, "\n"))
	v.AltScreen = true
	if x, y, ok := a.m.CaretPos(); ok {
		v.Cursor = caret.At(x, y, a.w, a.h)
	}
	return v
}

const usage = `usage: mac-lan-manager

手元の Mac 同士を、ネットに繋がっていない有線の LAN (内部の網) で繋いで管理する。
Bonjour で見えている Mac と台帳 (~/.config/glogx/mac-lan-manager/hosts.toml) の Mac を一覧にする。
キーの一覧は中で ? を押す。設定は , (~/.config/glogx/mac-lan-manager/settings.toml)。
`

func main() {
	if len(os.Args) > 1 {
		if os.Args[1] == "-h" || os.Args[1] == "--help" {
			fmt.Print(usage)
			return
		}
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	o, err := live.Options()
	if err != nil {
		fmt.Fprintln(os.Stderr, "mac-lan-manager:", err)
		os.Exit(1)
	}
	m := lanmgr.New(o)
	if widthenv.EastAsianAmbiguous() {
		fmt.Fprintln(os.Stderr, "🚨 "+widthenv.Message)
	}
	var opts []tea.ProgramOption
	if termcolor.TrueColorThroughTmux(os.Getenv("TMUX"), live.RunOut) {
		opts = append(opts, tea.WithColorProfile(colorprofile.TrueColor))
	}
	if _, err := tea.NewProgram(&app{m: m}, opts...).Run(); err != nil {
		m.Close()
		fmt.Fprintln(os.Stderr, "mac-lan-manager:", err)
		os.Exit(1)
	}
}
