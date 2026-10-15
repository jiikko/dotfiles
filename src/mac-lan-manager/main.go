// mac-lan-manager は手元の Mac 同士を内部の網で繋いで管理する道具 (bin/mac-lan-manager)。glogx の L と同じ画面を単体で開く。
//
//	mac-lan-manager   一覧を開く (Bonjour で見えている Mac と、台帳に載っている Mac)
//
// 画面の部品は lanmgr パッケージ (glogx からも使う)。ここは端末と toast の面倒だけを見る。要件と決定は issue 747。
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/jiikko/dotfiles/src/tuikit/caret"
	"github.com/jiikko/dotfiles/src/tuikit/foreground"
	"github.com/jiikko/dotfiles/src/tuikit/layout"
	"github.com/jiikko/dotfiles/src/tuikit/toast"
	"github.com/jiikko/dotfiles/src/tuikit/widthenv"

	"mac-lan-manager/discover"
	"mac-lan-manager/lanmgr"
	"mac-lan-manager/ledger"
	"mac-lan-manager/netinfo"
	"mac-lan-manager/prefs"
	"subproc"
)

// busyInterval は走査の結果を取り込む周期 (spinner もこの周期で回る。glogx の spinnerInterval と同じ)。
const busyInterval = 80 * time.Millisecond

// frameInterval は toast が動いている間の描き直しの周期。
const frameInterval = 16 * time.Millisecond

type tickMsg struct{}

// changedMsg は走査の結果が変わった合図 (ok=false はチャネルが閉じた = もう待たない)。
type changedMsg struct{ ok bool }

// waitChange は走査の合図を待つ (Busy の間しか tick を張らないので、後から見つかった Mac を出すにはこれを待つ)。
func waitChange(ch <-chan struct{}) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		_, ok := <-ch
		return changedMsg{ok}
	}
}

type execDoneMsg struct{ err, reclaimErr error }

type app struct {
	m       *lanmgr.Model
	toast   toast.Stack
	w, h    int
	ticking bool
}

func (a *app) Init() tea.Cmd { return tea.Batch(a.tick(), waitChange(a.m.Changed())) }

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
		if msg.ok {
			cmds = append(cmds, waitChange(a.m.Changed())) // 取り込みは下の Advance。また次の合図を待つ
		}
	}
	a.m.Advance(time.Now())
	for _, n := range a.m.TakeNotices() {
		a.toast.Show(n.Text, n.OK)
	}
	for _, t := range a.toast.Advance() {
		cmds = append(cmds, tea.Tick(t.After, func(time.Time) tea.Msg { return t.Msg }))
	}
	cmds = append(cmds, a.tick())
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

func runOut(ctx context.Context, name string, args ...string) (string, error) {
	out, err := subproc.CommandContext(ctx, name, args...).Output()
	return string(out), err
}

func localHost() string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := runOut(ctx, "scutil", "--get", "LocalHostName")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out) + ".local"
}

func ifName(index int) string {
	if i, err := net.InterfaceByIndex(index); err == nil {
		return i.Name
	}
	return ""
}

// trueColorThroughTmux は tmux の中で、外側の端末が 24bit の色に対応しているか (tmux のクライアントの端末の機能に RGB がある)。
// 🚨 bubbletea の色の判定 (colorprofile) は tmux の中では COLORTERM を見ず、`tmux info` (tmux の端末の定義) の Tc / RGB だけを見る。
// この repo の tmux は定義に RGB を持たない (missing) ので 256 色と判定され、面の色 (#2A2430 など) が紺 (48;5;17) に落ちた
// (2026-10-15 に隔離した tmux で撮って確かめた)。一方、_tmux.conf の terminal-features で外側の端末に RGB を付けていて、
// アプリが 24bit の色を書けば tmux はそのまま外側へ渡す。そこでクライアントの機能を直接聞いて、対応していれば 24bit で書き出す。
func trueColorThroughTmux() bool {
	if os.Getenv("TMUX") == "" {
		return false // tmux の外は colorprofile の判定 (COLORTERM) に任せる
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := runOut(ctx, "tmux", "display", "-p", "#{client_termfeatures}")
	if err != nil {
		return false
	}
	return slices.Contains(strings.Split(strings.TrimSpace(out), ","), "RGB")
}

func pbcopy(s string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(s)
	return cmd.Run()
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
	ledgerPath, err := ledger.DefaultPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "mac-lan-manager:", err)
		os.Exit(1)
	}
	prefsPath, err := prefs.DefaultPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "mac-lan-manager:", err)
		os.Exit(1)
	}
	m := lanmgr.New(lanmgr.Options{
		ReadNet:   func(ctx context.Context) (netinfo.Snapshot, error) { return netinfo.Read(ctx, runOut) },
		LocalHost: localHost,
		StartScan: func(now time.Time) lanmgr.Scanner { return discover.Start(discover.ExecStarter, now) },
		IfName:    ifName,
		Ledger:    ledger.FileStore{Path: ledgerPath},
		PrefsPath: prefsPath,
		SavePrefs: prefs.Save,
		Copy:      pbcopy,
	})
	if widthenv.EastAsianAmbiguous() {
		fmt.Fprintln(os.Stderr, "🚨 "+widthenv.Message)
	}
	var opts []tea.ProgramOption
	if trueColorThroughTmux() {
		opts = append(opts, tea.WithColorProfile(colorprofile.TrueColor))
	}
	if _, err := tea.NewProgram(&app{m: m}, opts...).Run(); err != nil {
		m.Close()
		fmt.Fprintln(os.Stderr, "mac-lan-manager:", err)
		os.Exit(1)
	}
}
