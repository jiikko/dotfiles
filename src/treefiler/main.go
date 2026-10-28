// treefiler は横に育つ木のファイラー (bin/treefiler)。glogx の F と同じ画面を単体で開く。
//
//	treefiler [DIR]   DIR (既定はカレントディレクトリ) を root にして開く
//
// 画面の部品は filer パッケージ (glogx からも使う)。ここは端末と toast の面倒だけを見る。
// 仕様は docs/treefiler-spec.md。
package main

import (
	"context"
	"fmt"
	"github.com/jiikko/dotfiles/src/tuikit/widthenv"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/jiikko/dotfiles/src/tuikit/caret"
	"github.com/jiikko/dotfiles/src/tuikit/foreground"
	"github.com/jiikko/dotfiles/src/tuikit/layout"
	"github.com/jiikko/dotfiles/src/tuikit/termcaps"
	"github.com/jiikko/dotfiles/src/tuikit/toast"

	"subproc"
	"treefiler/filer"
	"treefiler/termsync"
)

// frameInterval は動いている間の描き直しの周期 (~60fps)。止まっている間は描かない。
const frameInterval = 16 * time.Millisecond

// busyInterval は裏の処理を待つ間の周期 (glogx の spinnerInterval と同じ)。
const busyInterval = 80 * time.Millisecond

type tickMsg struct{}

// execDoneMsg は ! / s で起こしたプロセスから戻った合図。err は起動できなかったとき (終了コードは含めない)。
// reclaimErr は戻ったときに外れていた端末の前面を取り戻せなかった理由。
type execDoneMsg struct{ err, reclaimErr error }

// changedMsg はライブ更新の合図 (ok=false はチャネルが閉じた = もう待たない)。
type changedMsg struct{ ok bool }

func waitChange(ch <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		_, ok := <-ch
		return changedMsg{ok}
	}
}

// prefixMsg は tmux の prefix の取得結果 (起動時に 1 回、裏で)。
type prefixMsg struct{ key string }

// toggleKeyEnv は tmux の bind が popup に渡す、prefix の後に押すと閉じるキー (_tmux.conf の bind f)。
// popup の中のキーは tmux の bind を通らず treefiler に届くので、開いた prefix+f で閉じるにはここで受ける。
// 環境変数で受けるのは、ペインで直接起動したとき (prefix は tmux に取られて届かない) と glogx の F に効かせないため。
const toggleKeyEnv = "TREEFILER_TOGGLE_KEY"

type app struct {
	f       *filer.Model
	toast   toast.Stack
	w, h    int
	ticking bool
	// toggleKey は prefix の後に押すと閉じるキー ("" = 機能なし)。prefix は tmux の今の値 ("" = 未取得・tmux の外)。
	// armed は直前のキーが prefix だった。
	toggleKey, prefix string
	armed             bool
}

func (a *app) Init() tea.Cmd {
	if a.toggleKey == "" {
		return waitChange(a.f.Changed())
	}
	// 起動時の描画を tmux への問い合わせで待たせない (glogx の prefix の取得と同じ)
	return tea.Batch(waitChange(a.f.Changed()), func() tea.Msg {
		return prefixMsg{termcaps.TmuxPrefix(os.Getenv("TMUX"), runCmd)}
	})
}

func runCmd(ctx context.Context, name string, args ...string) (string, error) {
	out, err := subproc.CommandContext(ctx, name, args...).Output()
	return string(out), err
}

// quit は前回の場所を覚え (Remember place。savePlace は Close の中)、ライブ更新を止めて終える。
func (a *app) quit() (tea.Model, tea.Cmd) {
	a.f.Close()
	return a, tea.Quit
}

// takeToggle は prefix → toggleKey の並びを捌く。prefix は飲む (true)。prefix の後の toggleKey 以外のキーは
// 普通の操作として通す: prefix は押し間違いで押されるので、打ち直した入力まで捨てない (glogx の prefix の扱いと同じ)。
func (a *app) takeToggle(key string) (consumed, quit bool) {
	armed := a.armed // prefix が "" (未取得・tmux の外) なら、どのキーも prefix に一致しないので待ちに入らない
	a.armed = key == a.prefix
	switch {
	case a.armed:
		return true, false
	case armed && key == a.toggleKey:
		return true, true
	}
	return false, false
}

func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmds := make([]tea.Cmd, 0, 4)
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
		a.f.Resize(a.w, a.h)
	case tea.PasteMsg:
		a.f.Paste(msg.Content) // 入力中だけ入る (filer が捨てる)
	case prefixMsg:
		a.prefix = msg.key
	case tea.KeyPressMsg:
		consumed, quit := a.takeToggle(msg.String())
		if quit {
			return a.quit()
		}
		if consumed {
			break
		}
		switch a.f.HandleInput(msg.String(), msg.Text) {
		case filer.Quit:
			return a.quit()
		case filer.Exec:
			if r, ok := a.f.TakeExec(); ok && len(r.Argv) > 0 {
				// 前景で端末を明け渡す。ctx は持たない (ユーザーのシェルが終わるまで待つのが仕様)。パイプは張らない。
				// 戻った後に前面が外れていたら取り戻す (foreground.Exec。シェルによらず treefiler が端末を読めるように)
				cmd := subproc.CommandContext(context.Background(), r.Argv[0], r.Argv[1:]...)
				cmd.Dir = r.Dir
				cmd.Env = r.Env
				x := foreground.NewExec(cmd)
				cmds = append(cmds, tea.Exec(x, func(err error) tea.Msg { return execDoneMsg{err: err, reclaimErr: x.Err} }))
			}
		case filer.None:
		}
	case toast.Msg:
		a.toast.StartLeaving(msg)
	case execDoneMsg:
		if w := a.f.ExecDone(msg.err); w != "" {
			a.toast.Show(w, false)
		}
		if msg.reclaimErr != nil {
			a.toast.Show("端末の前面を取り戻せませんでした。止まったら fg で戻してください", false)
		}
	case changedMsg:
		if msg.ok {
			cmds = append(cmds, waitChange(a.f.Changed())) // 取り込みは下の Advance。また次の合図を待つ
		}
	case tickMsg:
		a.ticking = false
	}
	a.f.Advance(time.Now())
	for _, n := range a.f.TakeNotices() { // キーの後とも Advance の中で積まれた知らせとも、ここで 1 回だけ取り出す (issue 731)
		a.toast.Show(n.Text, n.OK)
	}
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
キーの一覧は中で ? を押す。設定は , (~/.config/treefiler/treefiler.toml)。
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
	unsynced := !termcaps.SyncedOutput(os.Getenv, runCmd)
	f, err := filer.New(dir, filer.Options{Unsynced: unsynced})
	if err != nil {
		fmt.Fprintln(os.Stderr, "treefiler:", err)
		os.Exit(1)
	}
	if widthenv.EastAsianAmbiguous() { // 支持しない幅の env は黙って枠を崩さず一度知らせる (glogx の main と同じ。alt screen の後に見える)
		fmt.Fprintln(os.Stderr, "🚨 "+widthenv.Message)
	}
	if _, err := tea.NewProgram(&app{f: f, toggleKey: os.Getenv(toggleKeyEnv)}, termsync.ProgramOptions(unsynced)...).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "treefiler:", err)
		os.Exit(1)
	}
}
