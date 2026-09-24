// pro-con — PM (producer) と PG (consumer) を分けて Claude Code を並列に回すための TUI。
// 設計は issues/ の 415 (epic)。本物の backend は今は読み取り専用 (live。issue 424)、模擬 (fake) は動作確認用に残す。
//
//	pro-con              TUI を起動する (本物: 今の Claude Code の session を読み取り専用で出す)
//	pro-con --mock       模擬データで起動する (claude は起動しない。動作確認用)
//	pro-con card …       PM / PG が使うカードの操作 (受付の箱に置く。pro-con card で使い方)
//	pro-con daemon       本物のモードの dispatcher を常駐させる (PG を起動する。週の利用枠を使う)
//	pro-con fake-attach  attach の代わりに TUI から起動される内部用のコマンド
//
// 設定は ~/.config/pro-con/config.toml (無ければ既定値。書式は config package の doc)。
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"pro-con/backend"
	"pro-con/config"
	"pro-con/fake"
	"pro-con/live"
	"pro-con/ui"
	"pro-con/upgrade"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// stateful は、ライブアップグレードで状態を引き継ぐ backend (模擬だけ。本物の読み取り専用は持たない)。
type stateful interface {
	Save() ([]byte, error)
	Restore([]byte) error
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	mock := false
	if len(args) > 0 && args[0] == "--mock" {
		mock = true
		args = args[1:]
	}
	if len(args) > 0 {
		switch args[0] {
		case "fake-attach":
			if len(args) != 2 {
				_, _ = fmt.Fprintln(stderr, "usage: pro-con fake-attach <session-id>")
				return 2
			}
			return fakeAttach(args[1], stdin, stdout)
		case "card": // PM / PG が使うカードの操作 (受付の箱に置くだけ。cardcmd.go)
			home, err := os.UserHomeDir()
			if err != nil {
				_, _ = fmt.Fprintln(stderr, "pro-con:", err)
				return 1
			}
			return runCard(args[1:], liveDir(home), stdout, stderr)
		case "daemon": // 本物のモードの dispatcher を常駐させる (daemoncmd.go)
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
			return runDaemon(args[1:], liveDir(home), filepath.Join(home, ".claude", "projects"), paths, stdout, stderr)
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
	// 模擬と本物で状態ファイルの置き場所を分ける (模擬のカードが本物の記録に混ざらないように。issue 424)
	var be backend.Backend
	dir := liveDir(home)
	if mock {
		be = fake.New(time.Now().Truncate(time.Minute)) // 模擬時間の起点は今 (時刻の表示が今に近い方が見本として読みやすい)
		dir = filepath.Join(stateDir(home), "mock")
	} else {
		lb := live.New(scopes, home, dir)
		ctx, cancel := context.WithCancel(context.Background())
		lb.Start(ctx)
		defer func() { cancel(); lb.Wait() }() // 読み直しが止まるのを待ってから抜ける (claude の子プロセスを残さない)
		be = lb
	}
	// ライブアップグレードで引き継いだ状態。読んだら環境変数は消す (エディタ・claude などの子プロセスへ漏らさない)
	resumePath := takeResumeEnv()
	var uiData []byte
	notes := []string{}
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
			removeResume(resumePath)
			return 0
		}
		p, err := switchToNew(m, be, execArgs(mock, args), dir, resumePath)
		resumePath = p
		m.UpgradeFailed(err)
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

// execArgs は新版に渡す引数 (--mock を付け直す。付け忘れると、模擬で使っていたのに本物で起動し直す)。
func execArgs(mock bool, args []string) []string {
	if mock {
		return append([]string{"--mock"}, args...)
	}
	return args
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
