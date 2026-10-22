// mac-lan-manager は手元の Mac 同士を内部の網で繋いで管理する道具 (bin/mac-lan-manager)。glogx の L と同じ画面を単体で開く。
//
//	mac-lan-manager   一覧を開く (Bonjour で見えている Mac と、台帳に載っている Mac)
//
// 画面の部品は lanmgr パッケージ (glogx からも使う)。ここは端末と toast の面倒だけを見る。要件と決定は issue 747。
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
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
	"mac-lan-manager/setup"
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
// 🚨 この待ち直しは mac-lan-manager の app.watch・glogx の lanView / filerView.rearm・treefiler の waitChange にもあるが、共通の部品にしない:
// 比べるのは「今のチャネルか」の 1 行で、状態の持ち主 (自分で持つ / model が持つ / 比べない)・ok=false のときに取り込むか・tick との関係が
// 4 か所で違い、部品にすると各画面の前提を引数で表すだけになる (tuikit は bubbletea を import できないので tea.Cmd も返せない)。
// 4 か所のどれかで穴を直したら、残りの 3 か所も同じ穴を grep で探す。
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
	sig      *sigWatch
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
				argv := r.RunArgv() // 初期設定の手順は、端末を渡す前に板を出す
				cmd := subproc.CommandContext(context.Background(), argv[0], argv[1:]...)
				x := foreground.NewExec(cmd)
				cmds = append(cmds, tea.Exec(a.sig.Guard(x), func(err error) tea.Msg { return execDoneMsg{err: err, reclaimErr: x.Err} }))
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

// quietModel は画面を出さないサブコマンド (dump / speedtest / ping) の Model。操作のログ (~/Library/Logs) に reload や知らせを書かない
// (dump は読むだけ、speedtest / ping は結果を stdout に出す)。
func quietModel() (*lanmgr.Model, error) {
	o, err := live.Options()
	if err != nil {
		return nil, err
	}
	o.Passive = true // 一覧を開いたときの疎通の確認をしない (dump は読むだけ・speedtest の帯域を食わない。ping は runPing が自分で確かめる)
	return lanmgr.New(o), nil
}

const usage = `usage: mac-lan-manager
       mac-lan-manager dump [--json] [--wait 8s]
       mac-lan-manager dump --peer <IP> [--user <name>] [--host <Bonjour のホスト名>] [--json]
       mac-lan-manager speedtest [--json] [--wait 8s] [<ホスト名か別名>...]
       mac-lan-manager ping [--json] [--wait 8s] [<ホスト名か別名>...]

手元の Mac 同士を、ネットに繋がっていない有線の LAN (内部の網) で繋いで管理する。
Bonjour で見えている Mac と台帳 (~/.config/mac-lan-manager/hosts.toml) の Mac を一覧にする。
キーの一覧は中で ? を押す。設定は , (~/.config/mac-lan-manager/settings.toml)。

dump は画面を出さずに、この Mac の網・一覧 (画面と同じ判定)・Bonjour の生の結果を文字 (--json で JSON) で出す。
読むだけ (台帳・設定・操作のログに書かない。初回だけ旧い置き場から移す。デバッグ用)。Bonjour の走査が終わるか --wait の上限まで待つ。
dump --peer は相手を鍵だけの ssh で読み、鍵で入れるかと相手のルーターの無い有線のサービスを出す (読むだけ。上限 15 秒)。
--user の既定は $USER、--host (known_hosts の鍵の名前) の既定は IP。

speedtest は管理対象の相手 (引数を渡せばその相手だけ) との通信速度を ssh 越しに 1 台ずつ測る (画面の t と同じ宛先・同じ測り方。
上り・下り各 5 秒、その間は回線をほぼ使い切る。確認は出さない)。有線で 100 Mbps 付近なら頭打ちの疑いを出す。
終了コード: 0 = 全部測れた / 1 = 測れない相手がいた / 2 = 引数の誤り・管理対象に無い相手 / 130 = 止められた。

ping は管理対象の相手 (引数を渡せばその相手だけ) の名前 (<host>.local と、内部 IP が固定なら <host>.wired.local) ごとに、
名前を引いて、引けた IP へ ping (3 回) と ssh の口 (22 番に TCP で繋ぐ) を確かめる (画面の p と同じ)。届く = ping が返るか 22 番が開く。
届いた相手が本人かは見ない (同じ IP の別の機器でも届くと出る)。有線の名前が内部 IP と違う IP に引けたら知らせる。
終了コード: 0 = 確かめた名前が全部届く / 1 = 届かない・引けない名前があった / 2 = 引数の誤り・管理対象に無い相手 / 130 = 止められた。
`

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help":
			fmt.Print(usage)
			return
		case "dump":
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
			rc := runDump(os.Args[2:], os.Stdout, os.Stderr, time.Now, ctx.Done(), quietModel, live.Observe)
			cancel()
			os.Exit(rc)
		case "speedtest":
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
			rc := runSpeed(os.Args[2:], os.Stdout, os.Stderr, time.Now, ctx.Done(), quietModel, live.SpeedTest)
			cancel()
			os.Exit(rc)
		case "ping":
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
			rc := runPing(os.Args[2:], os.Stdout, os.Stderr, time.Now, ctx.Done(), quietModel, live.Reach)
			cancel()
			os.Exit(rc)
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
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM) // 期待の表は sigWatch の注記
	sig := newSigWatch(sigs)
	_, err = tea.NewProgram(&app{m: m, sig: sig}, opts...).Run()
	signal.Notify(sigs, syscall.SIGHUP) // m.Close の前に聞く (Close の最中に外から HUP が来ると、dns-sd を止める前に死ぬ)
	// 🚨 err が無くても止める: bubbletea は SIGTERM を「終了」に変えて err=nil で戻すので、err のときだけ止めると
	// dns-sd が 9 本残った (-B は終わらない)
	m.Close()
	finishBG(os.Stderr, m, sig.Done())
	signal.Stop(sigs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mac-lan-manager:", err)
		os.Exit(1)
	}
}

// bgWaiter は終わる前に待つ裏の後始末 (lanmgr.Model)。
type bgWaiter interface {
	PendingBG() int
	WaitBG(stop <-chan struct{}) ([]lanmgr.Notice, int)
}

// finishBG は画面を閉じた後に、台帳から外した相手の広告を止める ssh の結果を待って出す (画面の知らせは閉じた後には出ない)。
func finishBG(w io.Writer, m bgWaiter, stop <-chan struct{}) {
	if m.PendingBG() == 0 {
		return
	}
	// 裏の ssh は同じプロセスグループに居るので、端末の Ctrl-C は ssh も止める (待つのをやめるだけではない)
	fmt.Fprintln(w, "台帳から外した相手の広告を止めている (Ctrl-C で取り消して終える。止める ssh も止まる)")
	got, left := m.WaitBG(stop)
	for _, n := range got {
		mark := "✓"
		if !n.OK {
			mark = "✗"
		}
		fmt.Fprintln(w, mark, n.Text)
	}
	if left > 0 {
		fmt.Fprintf(w, "✗ %d 台の結果を待たずに終えた (止まったかは分からない)。相手で: %s\n", left, setup.RemoveHint())
	}
}
