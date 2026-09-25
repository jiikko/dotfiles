// pro-con — PM (producer) と PG (consumer) を分けて Claude Code を並列に回すための TUI。
// 設計は issues/ の 415 (epic)。本物の backend は今は読み取り専用 (live。issue 424)、模擬 (fake) は動作確認用に残す。
//
//	pro-con              TUI を起動する (本物: 今の Claude Code の session を読み取り専用で出す)
//	pro-con --mock       模擬データで起動する (claude は起動しない。動作確認用)
//	pro-con card …       PM / PG が使うカードの操作 (受付の箱に置く。pro-con card で使い方)
//	pro-con log          dispatcher の出来事の記録を読む (読むだけ。pro-con log --help)
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

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	view := len(args) > 0 && args[0] == "--view" // 見ているだけの画面 (dispatcher を起こさない・止めない・受付の箱にも記録にも書かない。画面の中継 relay/ だけは書く)。--e2e と重ねてよい
	if view {
		args = args[1:]
	}
	mock, e2e, modeArgs, args, err := parseMode(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con:", err)
		return 2
	}
	if view {
		if mock {
			_, _ = fmt.Fprintln(stderr, "pro-con: --view は本物のモード (と --e2e) で使う (模擬は見るだけにする必要が無い)")
			return 2
		}
		modeArgs = append([]string{"--view"}, modeArgs...) // ライブアップグレードの後も --view のまま
	}
	if len(args) > 0 && len(modeArgs) > 0 {
		// 🚨 --e2e / --mock は画面の起動にだけ付ける。サブコマンドの前に付くと、サブコマンドは本物の置き場で動いてしまう
		// (`pro-con --e2e X dispatcher --stop` が本物の dispatcher と PG を止める)。dispatcher は `pro-con dispatcher --e2e <dir>`
		_, _ = fmt.Fprintf(stderr, "pro-con: %s はサブコマンド (%s) の前には付けない (dispatcher なら pro-con dispatcher --e2e <dir>、画面なら pro-con %s)\n",
			modeArgs[0], args[0], strings.Join(modeArgs, " "))
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
		case "e2e": // Claude が e2e モードの画面を操作する口 (e2ecmd.go)
			return runE2E(args[1:], stdout, stderr)
		case "card": // PM / PG が使うカードの操作 (受付の箱に置くだけ。cardcmd.go)
			home, err := os.UserHomeDir()
			if err != nil {
				_, _ = fmt.Fprintln(stderr, "pro-con:", err)
				return 1
			}
			return runCard(args[1:], viewEnv{dir: liveDir(home), projects: filepath.Join(home, ".claude", "projects"), now: time.Now}, stdout, stderr)
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
			repos, _ := config.Discover(cfg, home)
			paths := map[string]string{}
			for _, r := range repos {
				paths[r.Name] = r.Path
			}
			pmRepo, warn := cfg.PMRepoPath(home)
			if warn != "" {
				_, _ = fmt.Fprintln(stderr, "pro-con dispatcher:", warn)
			}
			return runDispatcher(args[1:], liveDir(home), filepath.Join(home, ".claude", "projects"), paths, pmConfig{Repo: pmRepo, Mode: cfg.PM}, stdout, stderr)
		case "-h", "--help":
			_, _ = fmt.Fprintln(stdout, "usage: pro-con [--mock]   (既定は今の Claude Code の session を読み取り専用で出す。--mock は模擬データ)")
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
		be, more = wireLive(lb, view, dir, func(dir string) error { return spawnDispatcher(dir, dispatcherArgs) },
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
			w.Put(relay.Frame{View: view, At: time.Now(), Width: width, Height: height, ANSI: a, State: st})
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
		closeRelay() // 新しい版へ exec すると defer が走らず、中継のファイルが落ちた画面の残りになる。戻ってきたら (失敗) 開き直す
		p, err := switchToNew(m, be, append(append([]string(nil), modeArgs...), args...), dir, resumePath)
		resumePath = p
		m.UpgradeFailed(err)
		openRelay()
	}
}

// startDispatcherIfIdle は、dispatcher が動いていなければ spawn で起動する (動いていれば何もしない)。
// 確かめてから起動するまでの間に別の画面が起動しても、2 つ目の dispatcher はロックを取れずに抜けるだけ。
func startDispatcherIfIdle(dir string, spawn func(dir string) error) (bool, error) {
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
// そうでなければ、画面を閉じるときに止める口 (stop) と、画面が開いている間 dispatcher を動かし続ける口 (spawn) をつなぎ、
// 居なければ今 dispatcher を起こす。起動のときに画面へ出す知らせを返す。
func wireLive(lb *live.Backend, view bool, dir string, spawn func(dir string) error, stop func(context.Context) error) (backend.Backend, []string) {
	if view {
		return lb.View(), []string{"見ているだけの画面 (--view): 依頼・回答・attach は受けない。quit で閉じても dispatcher と PG は止めない"}
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
	} else if started {
		notes = append(notes, "dispatcher を起動した (ログ: "+filepath.Join(dir, "dispatcher.log")+")")
	}
	return lb, notes
}

// dispatcherCmd は画面が起こす dispatcher のコマンド。画面が 1 つも無い状態が 1 分続いたら、PG を止めて抜ける
// (最後の画面が quit を通らずに消えても PG を残さない)。
func dispatcherCmd(exe string, extra []string) *exec.Cmd {
	return exec.Command(exe, append([]string{"dispatcher", "--exit-without-screens", "1m"}, extra...)...)
}

// spawnDispatcher は `pro-con dispatcher` を画面とは別のプロセスグループで起動する (画面を閉じても、ctrl+c が届いても道連れにしない。
// 止めるのは終了のときの `dispatcher --stop`)。出力は状態の置き場の dispatcher.log へ足す (画面より長く生きるのでパイプにしない)。
func spawnDispatcher(dir string, extra []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "dispatcher.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	cmd := dispatcherCmd(exe, extra)
	cmd.Stdout, cmd.Stderr = f, f
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
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
	cmd := exec.Command(exe, append([]string{"dispatcher", "--stop"}, extra...)...)
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
