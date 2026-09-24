// pro-con — PM (producer) と PG (consumer) を分けて Claude Code を並列に回すための TUI。
// 設計は issues/ の 415。今は模擬 backend (fake) だけで動く「ハリボテ」で、claude は起動しない。
//
//	pro-con              TUI を起動する (模擬データ)
//	pro-con fake-attach  attach の代わりに TUI から起動される内部用のコマンド
//
// 設定は ~/.config/pro-con/config.toml (無ければ既定値。書式は config package の doc)。
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"pro-con/backend"
	"pro-con/config"
	"pro-con/fake"
	"pro-con/ui"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "fake-attach":
			if len(args) != 2 {
				_, _ = fmt.Fprintln(stderr, "usage: pro-con fake-attach <session-id>")
				return 2
			}
			return fakeAttach(args[1], stdin, stdout)
		case "-h", "--help":
			_, _ = fmt.Fprintln(stdout, "usage: pro-con   (模擬データで TUI を起動する。claude は起動しない)")
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
	// 模擬時間の起点は固定しない (時刻の表示が今に近い方が見本として読みやすい)。刻みは fake が決める
	m := ui.New(fake.New(time.Now().Truncate(time.Minute)), scopes)
	if len(warnings) > 0 {
		m.Notify(fmt.Sprintf("設定の警告 %d 件 (%s): %s", len(warnings), cfgPath, warnings[0]))
	}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con:", err)
		return 1
	}
	return 0
}

// fakeAttach は claude attach <id> の代わり。TUI から tea.ExecProcess で起動され、抜けると TUI へ戻る。
func fakeAttach(session string, stdin io.Reader, stdout io.Writer) int {
	_, _ = fmt.Fprintf(stdout, "\n  [模擬] session %s に attach したつもりの画面です。\n", session)
	_, _ = fmt.Fprintln(stdout, "  本番ではここが claude attach の画面 (本物の Claude Code) になります。")
	_, _ = fmt.Fprint(stdout, "\n  Enter で pro-con に戻る > ")
	_, _ = bufio.NewReader(stdin).ReadString('\n')
	return 0
}
