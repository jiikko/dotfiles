// pro-con — PM (producer) と PG (consumer) を分けて Claude Code を並列に回すための TUI。
// 設計は issues/ の 415 (epic)。本物の backend は今は読み取り専用 (live。issue 424)、模擬 (fake) は動作確認用に残す。
//
//	pro-con              TUI を起動する (本物: 今の Claude Code の session を読み取り専用で出す)
//	pro-con --mock       模擬データで起動する (claude は起動しない。動作確認用)
//	pro-con card …       PM / PG が使うカードの操作 (受付の箱に置く。pro-con card で使い方)
//	pro-con log          dispatcher の出来事の記録を読む (読むだけ。pro-con log --help)
//	pro-con config …     止めずに PG の枠と PM の数を変える (受付の箱に置く。pro-con config で使い方)
//	pro-con ps           pro-con が起動したプロセスを役ごとに出す (読むだけ)
//	pro-con dispatcher       本物のモードの dispatcher を常駐させる (PG を起動する。週の利用枠を使う)
//	pro-con fake-attach  attach の代わりに TUI から起動される内部用のコマンド
//
// 設定は ~/.config/pro-con/config.toml (無ければ既定値。書式は config package の doc)。
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"pro-con/backend"
	"pro-con/config"
	"pro-con/dispatcher"
	"pro-con/fake"
	"pro-con/live"
	"pro-con/relay"
	"pro-con/store"
	"pro-con/ui"
	"pro-con/upgrade"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// parseMode は先頭の起動モードの引数 (--mock / --e2e <置き場>) を読む。modeArgs はライブアップグレードで新版に付け直す引数
// (付け忘れると、模擬や e2e で使っていたのに本物で起動し直す)。
func parseMode(args []string) (mock bool, e2e *dispatcher.E2E, modeArgs, rest []string, err error) {
	switch {
	case len(args) > 0 && args[0] == "--mock":
		return true, nil, []string{"--mock"}, args[1:], nil
	case len(args) > 0 && args[0] == "--e2e":
		if len(args) < 2 {
			return false, nil, nil, nil, errors.New("--e2e には置き場のディレクトリが要る (pro-con --e2e <dir>)")
		}
		root, err := filepath.Abs(args[1])
		if err != nil {
			return false, nil, nil, nil, err
		}
		return false, &dispatcher.E2E{Root: root}, []string{"--e2e", root}, args[2:], nil
	}
	return false, nil, nil, args, nil
}

// stateful は、ライブアップグレードで状態を引き継ぐ backend (模擬だけ。本物の読み取り専用は持たない)。
type stateful interface {
	Save() ([]byte, error)
	Restore([]byte) error
}

// screenFlags は画面の起動に付ける、画面の種類のフラグ (--view / --join / --as <ラベル>。サブコマンドの前には付けない)。
type screenFlags struct {
	// view は見ているだけの画面 (dispatcher を起こさない・止めない・受付の箱にも記録にも書かない。書くのは画面の中継 relay/ と、
	// ctrl+r の引き継ぎ・落ちた画面の印の後始末だけ = issue 445)。--e2e と重ねてよい
	view bool
	// join は加わった画面 (読み書きするが、dispatcher を起こさず、quit で閉じても止めない。issue 481)。--e2e と重ねてよい
	join bool
	// label は画面の一覧に出す名前 (--as。任意。持ち主と join の画面だけ)
	label string
	args  []string // ライブアップグレードで新版に付け直す引数
}

// parseScreen は先頭の画面の種類のフラグを読む (順は問わない)。併用できない組は誤り。
func parseScreen(args []string) (screenFlags, []string, error) {
	var f screenFlags
	for len(args) > 0 {
		switch args[0] {
		case "--view":
			f.view = true
		case "--join":
			f.join = true
		case "--as":
			if len(args) < 2 || strings.TrimSpace(args[1]) == "" || strings.HasPrefix(args[1], "--") {
				return f, nil, errors.New("--as には画面の名前を付ける (例 pro-con --join --as review)")
			}
			f.label = strings.TrimSpace(args[1])
			f.args = append(f.args, args[0])
			args = args[1:]
		default:
			return f, args, f.check()
		}
		f.args = append(f.args, args[0])
		args = args[1:]
	}
	return f, args, f.check()
}

func (f screenFlags) check() error {
	switch {
	case f.view && f.join:
		// 読むだけのつもりが書ける形を作らない (issue 481)
		return errors.New("--join と --view は併用しない (--view は見るだけ、--join は読み書きする画面)")
	case f.view && f.label != "":
		return errors.New("--as は --view と組まない (見ているだけの画面は画面の一覧に入らない)")
	}
	return nil
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	screen, args, err := parseScreen(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con:", err)
		return 2
	}
	view := screen.view
	mock, e2e, modeArgs, args, err := parseMode(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con:", err)
		return 2
	}
	if mock && view {
		_, _ = fmt.Fprintln(stderr, "pro-con: --view は本物のモード (と --e2e) で使う (模擬は見るだけにする必要が無い)")
		return 2
	}
	if mock && screen.join {
		_, _ = fmt.Fprintln(stderr, "pro-con: --join は本物のモード (と --e2e) で使う (模擬には加わる dispatcher が無い)")
		return 2
	}
	if first := append(append([]string(nil), screen.args...), modeArgs...); len(args) > 0 && len(first) > 0 {
		// 🚨 --e2e / --mock は画面の起動にだけ付ける。サブコマンドの前に付くと、サブコマンドは本物の置き場で動いてしまう
		// (`pro-con --e2e X dispatcher --stop` が本物の dispatcher と PG を止める)。dispatcher は `pro-con dispatcher --e2e <dir>`
		_, _ = fmt.Fprintf(stderr, "pro-con: %s はサブコマンド (%s) の前には付けない (dispatcher なら pro-con dispatcher --e2e <dir>、画面なら pro-con %s)\n",
			first[0], args[0], strings.Join(first, " "))
		return 2
	}
	if len(args) > 0 {
		switch args[0] {
		case "fake-attach":
			if len(args) != 2 {
				_, _ = fmt.Fprintln(stderr, "usage: pro-con fake-attach <session-id>")
				return 2
			}
			return fakeAttach(args[1], stdin, stdout)
		case spawnDetachedCmd: // spawnDispatcher が挟む中継 (内部用)
			return spawnDetached(args[1:], stderr)
		case "e2e": // Claude が e2e モードの画面を操作する口 (e2ecmd.go)
			return runE2E(args[1:], stdout, stderr)
		case "card": // PM / PG が使うカードの操作 (受付の箱に置くだけ。cardcmd.go)
			home, err := os.UserHomeDir()
			if err != nil {
				_, _ = fmt.Fprintln(stderr, "pro-con:", err)
				return 1
			}
			return runCard(args[1:], viewEnv{dir: liveDir(home), projects: filepath.Join(home, ".claude", "projects"), now: time.Now}, stdout, stderr)
		case "config", "ps": // 止めずに PG の枠・PM の数を変える口 (configcmd.go) / 役ごとのプロセスの一覧 (読むだけ。pscmd.go)
			home, err := os.UserHomeDir()
			if err != nil {
				_, _ = fmt.Fprintln(stderr, "pro-con:", err)
				return 1
			}
			if args[0] == "ps" {
				return runPS(args[1:], liveDir(home), time.Now, execProcs, stdout, stderr)
			}
			return runConfig(args[1:], liveDir(home), stdout, stderr)
		case "screen": // 人間の画面に今出ているものを外から読む (読むだけ。screencmd.go)
			home, err := os.UserHomeDir()
			if err != nil {
				_, _ = fmt.Fprintln(stderr, "pro-con:", err)
				return 1
			}
			return runScreen(args[1:], home, time.Now, stdout, stderr)
		case "log": // dispatcher の出来事の記録を読む (読むだけ。logcmd.go)
			home, err := os.UserHomeDir()
			if err != nil {
				_, _ = fmt.Fprintln(stderr, "pro-con:", err)
				return 1
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM) // --follow は ctrl+c で終わる (rc=0)
			defer stop()
			return runLog(ctx, args[1:], liveDir(home), time.Now, stdout, stderr)
		case "dispatcher", "daemon": // 本物のモードの dispatcher を常駐させる (dispatchercmd.go)。daemon は 2026-09-25 に dispatcher へ改名する前の名前 (別名として残す)
			if args[0] == "daemon" {
				_, _ = fmt.Fprintln(stderr, "pro-con: daemon は dispatcher に改名した (pro-con dispatcher)")
			}
			home, err := os.UserHomeDir()
			if err != nil {
				_, _ = fmt.Fprintln(stderr, "pro-con:", err)
				return 1
			}
			cfg, err := config.Load(config.DefaultPath(home))
			if err != nil {
				_, _ = fmt.Fprintln(stderr, "pro-con: 設定を読めない:", err)
				return 1
			}
			paths := discoverPaths(cfg, home)
			pmRepo, warn := cfg.PMRepoPath(home)
			if warn != "" {
				_, _ = fmt.Fprintln(stderr, "pro-con dispatcher:", warn)
			}
			return runDispatcher(args[1:], liveDir(home), filepath.Join(home, ".claude", "projects"), paths, pmConfig{Repo: pmRepo, Mode: cfg.PM, IntegratorMode: cfg.Integrator}, stdout, stderr)
		case "monitor": // 見張り (dispatcher が子として起こす。読むだけで、見つけたことは受付の箱に置く。monitorcmd.go)
			home, err := os.UserHomeDir()
			if err != nil {
				_, _ = fmt.Fprintln(stderr, "pro-con:", err)
				return 1
			}
			paths, err := repoPaths(home)
			if err != nil {
				_, _ = fmt.Fprintln(stderr, "pro-con monitor:", err)
				return 1
			}
			return runMonitor(args[1:], liveDir(home), paths, stdout, stderr)
		case "-h", "--help":
			_, _ = fmt.Fprintln(stdout, "usage: pro-con [--view | --join] [--as <名前>] [--mock | --e2e <置き場>]   (--view は見るだけ、--join は加わる (閉じても止めない)。詳しくは README)")
			return 0
		default:
			_, _ = fmt.Fprintf(stderr, "pro-con: 未知の引数 %q (pro-con --help)\n", args[0])
			return 2
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con:", err)
		return 1
	}
	cfgPath := config.DefaultPath(home)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		// 壊れた設定で黙って既定値へ落とさない (直すべき場所を出して止まる)
		_, _ = fmt.Fprintln(stderr, "pro-con: 設定を読めない:", err)
		return 1
	}
	repos, warnings := config.Discover(cfg, home)
	scopes := make([]backend.Repo, len(repos))
	for i, r := range repos {
		scopes[i] = backend.Repo{Name: r.Name, Path: r.Path}
	}
	if e2e != nil { // e2e モードの repo は置き場の下の偽の repo だけ (本物の repo に触らない)
		scopes, warnings = []backend.Repo{{Name: dispatcher.E2ERepo, Path: e2e.RepoDir()}}, nil
	}
	var notes []string // 起動時に画面へ出す知らせ
	// 模擬と本物で状態ファイルの置き場所を分ける (模擬のカードが本物の記録に混ざらないように。issue 424)
	var be backend.Backend
	dir := liveDir(home)
	if mock {
		be = fake.New(time.Now().Truncate(time.Minute)) // 模擬時間の起点は今 (時刻の表示が今に近い方が見本として読みやすい)
		dir = filepath.Join(stateDir(home), "mock")
	} else {
		var dispatcherArgs []string // dispatcher の子プロセスに渡す引数 (e2e モードなら --e2e <置き場>)
		if e2e != nil {
			dir, dispatcherArgs = e2e.StateDir(), modeArgs
		}
		lb := live.New(scopes, home, dir)
		if e2e != nil {
			lb.SetList(e2e.List) // 偽の session の一覧 (本物の claude agents を読まない)
			if exe, err := os.Executable(); err == nil {
				lb.SetAttach(func(id string) *exec.Cmd { return exec.Command(exe, "fake-attach", id) }) // 本物の claude attach を起動しない
			}
		}
		var more []string
		lb.SetScreenInfo(screen.label, ttyName(stdin))
		be, more = wireLive(lb, screen, dir, func(dir string) error { return spawnDispatcher(dir, dispatcherArgs) },
			func(ctx context.Context) error { return stopInChild(ctx, dir, dispatcherArgs) })
		notes = append(notes, more...)
		ctx, cancel := context.WithCancel(context.Background())
		lb.Start(ctx)
		defer func() { cancel(); lb.Wait() }() // 読み直しが止まるのを待ってから抜ける (claude の子プロセスを残さない)
	}

	// ライブアップグレードで引き継いだ状態。読んだら環境変数は消す (エディタ・claude などの子プロセスへ漏らさない)
	resumePath := takeResumeEnv()
	var uiData []byte
	if resumePath != "" {
		if st, err := upgrade.Load(resumePath); err != nil {
			// 読めなかったファイルは消さない (版が違うなら古い版で読める)。パスを出して、要らなければ人が消せるようにする
			notes = append(notes, "引き継ぎに失敗したので最初から ("+resumePath+" は残した): "+err.Error())
			resumePath = ""
		} else {
			if s, ok := be.(stateful); ok && len(st.Backend) > 0 {
				if err := s.Restore(st.Backend); err != nil {
					notes = append(notes, "模擬の状態を引き継げなかった: "+err.Error())
				}
			}
			uiData = st.UI
		}
	}
	m := ui.New(be, scopes)
	if uiData != nil {
		if err := m.ImportState(uiData); err != nil {
			notes = append(notes, "UI の状態を引き継げなかった: "+err.Error())
		}
	}
	if len(warnings) > 0 {
		notes = append(notes, fmt.Sprintf("設定の警告 %d 件 (%s): %s", len(warnings), cfgPath, warnings[0]))
	}
	if len(notes) > 0 {
		m.Notify(strings.Join(notes, " / "))
	}
	if exe, err := os.Executable(); err == nil {
		_ = m.EnableUpgrade(exe, upgrade.ExecRunner) // bin/pro-con 以外から起動していれば無効 (ctrl+r で理由を出す)
	}
	// 画面の中継 (issue 443。外の Claude が pro-con screen で読む)。本物のモードと e2e モードだけ (--view の画面も中継する)
	var rw *relay.Writer
	openRelay := func() {
		if mock {
			return
		}
		w, err := relay.Open(dir)
		if err != nil {
			m.Notify("画面の中継を置けない (pro-con screen から読めない): " + err.Error())
			return
		}
		rw = w
		m.SetFrameSink(func(a string, width, height int, st map[string]string) {
			w.Put(relay.Frame{View: view, Join: screen.join, Label: screen.label, At: time.Now(), Width: width, Height: height, ANSI: a, State: st})
		})
	}
	closeRelay := func() {
		if rw != nil {
			m.SetFrameSink(nil)
			rw.Close()
			rw = nil
		}
	}
	openRelay()
	defer closeRelay()
	for {
		if _, err := tea.NewProgram(m).Run(); err != nil {
			_, _ = fmt.Fprintln(stderr, "pro-con:", err)
			if resumePath != "" {
				// 引き継いだ状態は残す (新版が起動直後に落ちた等。直したら同じ状態で起動し直せる)
				_, _ = fmt.Fprintf(stderr, "pro-con: 引き継いだ状態は %s に残した。直したら次で引き継げる:\n  %s=%s %s\n",
					resumePath, upgrade.ResumeEnv, resumePath, wrapperPath())
			}
			return 1
		}
		if !m.UpgradeRequested() {
			// 裏の処理 (attach の間の指示を受付の箱へ置く等) を終えてから抜ける。bubbletea は走っている Cmd を待たない
			_ = m.WaitChildren(ui.SwitchWait)
			removeResume(resumePath)
			var kept backend.KeptRunning
			if err := m.StopErr(); errors.As(err, &kept) { // ほかの画面が開いているので止めなかった (失敗ではない)
				_, _ = fmt.Fprintln(stderr, "pro-con:", err)
				return 0
			}
			if err := m.StopErr(); err != nil { // 終了のときに dispatcher と PG を止めきれなかった (画面を閉じた後に出す)
				_, _ = fmt.Fprintln(stderr, "pro-con: 終了のときに止めきれなかった:", err)
				_, _ = fmt.Fprintf(stderr, "  様子: %s / %s。もう一度止める: pro-con dispatcher --stop\n",
					filepath.Join(dir, "dispatcher.log"), filepath.Join(dir, "stop.log"))
				return 1
			}
			return 0
		}
		closeRelay()                                                                                                               // 新しい版へ exec すると defer が走らず、中継のファイルが落ちた画面の残りになる。戻ってきたら (失敗) 開き直す
		p, err := switchToNew(m, be, append(append(append([]string(nil), screen.args...), modeArgs...), args...), dir, resumePath) // 新版も同じ種類の画面で開く
		resumePath = p
		m.UpgradeFailed(err)
		openRelay()
	}
}

// startDispatcherIfIdle は、dispatcher が動いていなければ spawn で起動する (動いていれば何もしない)。
// 確かめてから起動するまでの間に別の画面が起動しても、2 つ目の dispatcher はロックを取れずに抜けるだけ。
// 人が止めた印 (store.Held。issue 459) があれば起こさない。確かめてから起こすまでに印が置かれても、起こされた側 (--from-screen) が抜ける
func startDispatcherIfIdle(dir string, spawn func(dir string) error) (bool, error) {
	if store.Held(dir) {
		return false, nil
	}
	unlock, err := dispatcher.Lock(dir)
	if errors.Is(err, dispatcher.ErrRunning) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	unlock()
	return true, spawn(dir)
}

// wireLive は本物の画面の backend をつなぐ (Start の前)。view なら見ているだけ: 止める口・起こす口をつながず、dispatcher も起こさない。
// join なら加わるだけ: 書く口は持つが、止める口・起こす口をつながず、dispatcher も起こさない (居なければ・止めてあればそう出す。issue 481)。
// そうでなければ、画面を閉じるときに止める口 (stop) と、画面が開いている間 dispatcher を動かし続ける口 (spawn) をつなぎ、
// 居なければ今 dispatcher を起こす。起動のときに画面へ出す知らせを返す。
func wireLive(lb *live.Backend, screen screenFlags, dir string, spawn func(dir string) error, stop func(context.Context) error) (backend.Backend, []string) {
	if screen.view {
		return lb.View(), []string{"見ているだけの画面 (--view): 依頼・回答・attach は受けない。quit で閉じても dispatcher と PG は止めない"}
	}
	if screen.join {
		notes := []string{"加わった画面 (--join): 依頼・回答・attach は受ける。dispatcher は起こさず、quit で閉じても dispatcher と PG は止めない"}
		if store.Held(dir) {
			notes = append(notes, "dispatcher は人が止めてある (pro-con dispatcher --stop)。join の画面からは外せない (持ち主の画面の c か、手で pro-con dispatcher を起動する)")
		} else if ds, _, err := store.LoadDispatcherState(dir); err == nil && (ds.Tick.IsZero() || time.Since(ds.Tick) > joinIdleAfter) {
			// 🚨 dispatcher の lock で確かめない: 一瞬でも取ると、ちょうど起動した dispatcher が「動いている」と見て抜ける
			notes = append(notes, "dispatcher が動いていない。起こすのは持ち主の画面か pro-con dispatcher (打った依頼は受付の箱で待つ)")
		}
		return lb.Join(), notes
	}
	var notes []string
	lb.SetStopper(stop)
	// 画面が開いている間は dispatcher を動かし続ける (落ちた / 前の画面が止めている間に開いた、を起こし直す)
	lb.SetKeeper(func() error {
		_, err := startDispatcherIfIdle(dir, spawn)
		return err
	})
	// 画面を開いたら dispatcher も立てる (閉じると止める。2026-09-25 にユーザーが決めた形。415 の「TUI と常駐プロセス」)
	if started, err := startDispatcherIfIdle(dir, spawn); err != nil {
		notes = append(notes, "dispatcher を起動できない: "+err.Error()+" (手で起動する: pro-con dispatcher)")
	} else if store.Held(dir) {
		notes = append(notes, "dispatcher は人が止めてある (pro-con dispatcher --stop) ので起こさない。c で起こす")
	} else if started {
		notes = append(notes, "dispatcher を起動した (ログ: "+filepath.Join(dir, "dispatcher.log")+")")
	}
	return lb, notes
}

// joinIdleAfter は、dispatcher がこれより長く回っていなければ join の画面が「動いていない」と出す (Tick は 3 秒ごと)。
const joinIdleAfter = 10 * time.Second

// ttyName は画面の端末 (画面の一覧に出す見分け。取れなければ空)。stdin が端末のときだけ tty(1) に聞く。
func ttyName(stdin io.Reader) string {
	f, ok := stdin.(*os.File)
	if !ok {
		return ""
	}
	if st, err := f.Stat(); err != nil || st.Mode()&os.ModeCharDevice == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tty")
	cmd.Stdin = f
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// dispatcherCmd は画面が起こす dispatcher のコマンド。画面が 1 つも無い状態が 1 分続いたら、PG を止めて抜ける
// (最後の画面が quit を通らずに消えても PG を残さない)。
func dispatcherCmd(exe string, extra []string) *exec.Cmd {
	return exec.Command(exe, append([]string{"dispatcher", "--" + fromScreenFlag, "--exit-without-screens", "1m"}, extra...)...)
}

// spawnDispatcher は `pro-con dispatcher` を画面とは別のプロセスグループで起動する (画面を閉じても、ctrl+c が届いても道連れにしない。
// 止めるのは終了のときの `dispatcher --stop`)。出力は状態の置き場の dispatcher.log へ足す (画面より長く生きるのでパイプにしない)。
// 🚨 画面の子にしない: 中継 (spawnDetached) を挟み、中継だけを待つ。dispatcher は launchd の子になり、抜けたら launchd が刈り取る
// (画面の子のままだと、抜けた dispatcher が画面を閉じるまでゾンビで残り、keeper が起こし直すたびに溜まる。Wait の goroutine は
// ctrl+r の exec で消えるので足りない。issue 477)。
func spawnDispatcher(dir string, extra []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logPath := filepath.Join(dir, "dispatcher.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	cmd := dispatcherCmd(exe, extra)
	cmd.Args = append([]string{exe, spawnDetachedCmd}, cmd.Args[1:]...)
	cmd.Stdout, cmd.Stderr = f, f
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("dispatcher を起動する中継が失敗した (%w。様子は %s)", err, logPath)
	}
	return nil
}

// spawnDetachedCmd は spawnDispatcher が挟む中継の内部用のサブコマンド。
const spawnDetachedCmd = "spawn-detached"

// spawnDetached は中継: 自分のバイナリを args で別のプロセスグループに起動し、待たずに抜ける (起こした子は launchd の子になる)。
// 子の出力は中継の stdout / stderr (spawnDispatcher が渡した dispatcher.log) をそのまま引き継ぐ。
func spawnDetached(args []string, stderr io.Writer) int {
	exe, err := os.Executable()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con spawn-detached:", err)
		return 1
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con spawn-detached:", err)
		return 1
	}
	return 0
}

// stopCmd は画面の quit (と e2e の後始末) が dispatcher と PG を止めるコマンド。人が止めた印を置かない (--from-screen。issue 459):
// 最後の画面の quit で止めても、次に開いた画面は今までどおり dispatcher を起こす。
func stopCmd(exe string, extra []string) *exec.Cmd {
	return exec.Command(exe, append([]string{"dispatcher", "--stop", "--" + fromScreenFlag}, extra...)...)
}

// stopInChild は `pro-con dispatcher --stop` を別のプロセスで走らせて待つ。画面を ctrl+c で閉じても (待たずに閉じても)、
// 止める処理は子が最後まで続ける (画面のプロセスの中で止めると、閉じた瞬間に途中で切れる)。子の出力は状態の置き場の stop.log へ
// (画面が先に閉じるとパイプが切れて子が書けなくなるので、パイプにしない)。
func stopInChild(ctx context.Context, dir string, extra []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logPath := filepath.Join(dir, "stop.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) // 足していく (前の --stop が止め直しを続けていれば、そのログを消さない)
	if err != nil {
		return err
	}
	var from int64 // 失敗したときに出すのは、この子が書いた分だけ
	if st, err := f.Stat(); err == nil {
		from = st.Size()
	}
	cmd := stopCmd(exe, extra)
	cmd.Stdout, cmd.Stderr = f, f
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // 端末の割り込みを子へ届けない
	if err := cmd.Start(); err != nil {
		_ = f.Close()
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait(); _ = f.Close() }()
	select {
	case err := <-done:
		if err != nil {
			out, _ := os.ReadFile(logPath)
			if int64(len(out)) >= from {
				out = out[from:]
			}
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("止める処理が時間内に終わらない (子はまだ続けている。結果は %s)", logPath)
	}
}

// execFn は syscall.Exec (テストで差し替える)。
var execFn upgrade.ExecFunc = syscall.Exec

// switchToNew は状態を書き出して新版を exec する。成功すると戻らない。戻ってきたら (状態のファイルのパス, エラー)
// (旧版のまま続ける)。引き継いだ状態のファイル (resume) があればそこへ上書きする (溜めない。消したパスを案内しない)。
// exec に失敗したとき、新しく作ったファイルは消す (旧版のまま続けるので要らない)。
func switchToNew(m *ui.Model, be backend.Backend, args []string, dir, resume string) (string, error) {
	uiData, err := m.PrepareSwitch(ui.SwitchWait)
	if err != nil {
		return resume, err
	}
	var beData []byte
	if s, ok := be.(stateful); ok {
		if beData, err = s.Save(); err != nil {
			return resume, err
		}
	}
	// 🚨 --view の画面もここで状態の置き場に resume-*.json を書く (読むだけの例外 (a)。issue 445): 中身はこの画面自身の表示の状態
	// (選んでいるカード・開いている板) だけで、カード・受付の箱・dispatcher・PG の状態は変えず、新版が読んだら消す
	path, err := upgrade.Save(dir, resume, upgrade.State{UI: uiData, Backend: beData})
	if err != nil {
		return resume, err
	}
	if err := upgrade.Exec(m.UpgradeExe(), args, os.Environ(), path, execFn); err != nil {
		if resume == "" {
			removeResume(path)
		}
		return resume, err
	}
	return path, nil
}

// wrapperPath は案内に出す起動のコマンド (bin/pro-con の絶対パス。見つからなければ "bin/pro-con")。
func wrapperPath() string {
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "..", "..", "bin", "pro-con")
		if _, err := os.Stat(p); err == nil {
			return filepath.Clean(p)
		}
	}
	return "bin/pro-con"
}

// takeResumeEnv は引き継いだ状態のファイルのパスを取り、環境変数からは消す (エディタ・claude などの子プロセスへ漏らさない。
// 漏れると、その子から起動した pro-con が他人の状態を読む)。
func takeResumeEnv() string {
	p := os.Getenv(upgrade.ResumeEnv)
	_ = os.Unsetenv(upgrade.ResumeEnv)
	return p
}

func removeResume(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

// stateDir は引き継ぐ状態の置き場 ($XDG_STATE_HOME/pro-con、未設定なら ~/.local/state/pro-con)。
func stateDir(home string) string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "pro-con")
}

// fakeAttach は claude attach <id> の代わり。TUI から tea.ExecProcess で起動され、抜けると TUI へ戻る。
func fakeAttach(session string, stdin io.Reader, stdout io.Writer) int {
	_, _ = fmt.Fprintf(stdout, "\n  [模擬] session %s に attach したつもりの画面です。\n", session)
	_, _ = fmt.Fprintln(stdout, "  本番ではここが claude attach の画面 (本物の Claude Code) になります。")
	_, _ = fmt.Fprint(stdout, "\n  Enter で pro-con に戻る > ")
	_, _ = bufio.NewReader(stdin).ReadString('\n')
	return 0
}

// repoPaths は設定の repo の名前 → パス。
func repoPaths(home string) (map[string]string, error) {
	cfg, err := config.Load(config.DefaultPath(home))
	if err != nil {
		return nil, fmt.Errorf("設定を読めない: %w", err)
	}
	return discoverPaths(cfg, home), nil
}

// discoverPaths は設定の repo を見つけて、名前 → パスにする (見つからない repo は入れない)。
func discoverPaths(cfg config.Config, home string) map[string]string {
	repos, _ := config.Discover(cfg, home)
	paths := map[string]string{}
	for _, r := range repos {
		paths[r.Name] = r.Path
	}
	return paths
}
