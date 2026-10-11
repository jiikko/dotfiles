package filer

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/jiikko/dotfiles/src/tuikit/foreground"
	"golang.org/x/sys/unix"
)

// `!` で起こしたプロセスが終わった後、端末の前面が起こした側 (treefiler / glogx) へ戻っていること。
//
// 擬似端末の上で、テストのバイナリを helper (TestBangHelperOnPTY) として起こす。helper は `!` が作る本物の Argv を、
// treefiler・glogx と同じ包み (foreground.NewExec。戻った後に前面を取り戻す) で自分のグループのまま起こし、戻ったら前面が自分かを
// READY の行で返す。アプリがこの包みを通ることは gorules の execViaForeground が止める (tea.ExecProcess を書けない)。起こし方 (shape):
//   - leader: helper が擬似端末の session の先頭 (glogx が tmux のペインで直に動く形)。孤児のグループなので、前面を失うと
//     stty・dd は止まらず EIO で即座に抜ける
//   - job: session の先頭の下の前景ジョブ (外のシェルから起こした treefiler の形)。前面を失うと stty・dd が SIGTTOU で止まる
//     (= hang guard で落ちる)
//
// キーは dd が読んだことまで確かめる: helper は戻った後に 1 行読み、テストはキー待ちの案内にキーを、READY に "y\n" を送る。
// dd がキーを読まずに抜けたら、helper の行は "xy" になる。
func TestBangGivesTerminalBackOnPTY(t *testing.T) {
	shells := []string{"/bin/sh"} // macOS では bash 3.2
	if zsh, err := exec.LookPath("zsh"); err == nil {
		shells = append(shells, zsh)
	} else {
		t.Logf("zsh が無いので /bin/sh だけで確かめる: %v", err)
	}
	// コマンドは外部コマンドにする: 前面を持ち去るのは、シェルが自分のグループで前面を取ったまま最後の外部コマンドを exec したとき。
	// ビルトイン (echo・true) は exec されないので、trap を外しても前面は動かず緑のまま通る (変異で実測)
	cases := make([]bangCase, 0, 36)
	for _, shape := range []string{"leader", "job"} {
		for _, sh := range shells {
			cases = append(cases,
				bangCase{name: "external", shape: shape, shell: sh, cmd: "/bin/echo ran-bang", wantKeyWait: true, wantBack: true},
				// コマンドの中の対話のシェル (中のシェルも最後のコマンドを exec する)
				bangCase{name: "nested", shape: shape, shell: sh, cmd: "/bin/sh -ic '/bin/echo ran-bang'", wantKeyWait: true, wantBack: true},
				// 失敗の rc を [exit N] に出す (trap が rc を潰すと [done] に化ける)
				bangCase{name: "rc", shape: shape, shell: sh, cmd: "/bin/echo ran-bang; /usr/bin/false", want: "[exit 1]", wantKeyWait: true, wantBack: true},
				// errexit で抜ける経路 (zsh は trap の中で exit しないと前面を戻さない)
				bangCase{name: "errexit", shape: shape, shell: sh, cmd: "set -e; /bin/echo ran-bang; /usr/bin/false; /bin/echo unreachable", want: "[exit 1]", wantKeyWait: true, wantBack: true},
			)
		}
	}
	for _, sh := range shells {
		for _, shape := range []string{"leader", "job"} {
			cases = append(cases,
				// trap で戻らない形 (シェルが自分の終了処理を通らずに死ぬ)。包みの sh は前面でないのでキーを待たずに戻り、
				// 前面は Go の取り戻し (foreground.Exec) で戻る
				bangCase{name: "kill-self", shape: shape, shell: sh, cmd: "/bin/echo ran-bang; kill -KILL $$", wantBack: true},
				// 包まない `-ic` (前面を持ち去る) も、Go の取り戻しで戻る (trap に頼らない層の証拠)
				bangCase{name: "unwrapped-reclaim", shape: shape, shell: sh, argv: []string{sh, "-ic", "/bin/echo ran-bang"}, wantBack: true},
			)
			if filepath.Base(sh) == "zsh" { // bash 3.2 は trap を外しても戻る (実測)
				// コマンドの中で包みの trap を外す: zsh は最後のコマンドを exec して前面を持ち去る
				cases = append(cases, bangCase{name: "trap-removed", shape: shape, shell: sh, cmd: "trap - EXIT; /bin/echo ran-bang", wantBack: true})
			}
		}
		cases = append(cases,
			// 負の対照: 取り戻さずに (素の Run で) 包まない `-ic` を起こすと前面が戻らないこと (このテストが見ている現象が今の環境で
			// 起きている証拠。bash 5 は自分で戻すので、macOS の /bin/sh = bash 3.2 と zsh で見る)
			bangCase{name: "unwrapped", shape: "leader", shell: sh, argv: []string{sh, "-ic", "/bin/echo ran-bang"}, raw: true},
			// 実行中の Ctrl-Z。シェルが最後のコマンドを exec すると、止まったジョブを待つ者が居なくなって永久に固まる
			bangCase{name: "ctrl-z", shape: "job", shell: sh, cmd: "/bin/sh -c 'echo ran-bang; exec /bin/sleep 30'",
				trigger: "ran-bang", send: "\x1a", wantKeyWait: true, wantBack: true},
		)
	}
	cases = append(cases,
		// 3 秒以上かかる経路 (キーを待たない) でも前面が戻ること (戻った bubbletea が前面で端末を戻すため)。
		// コマンドは 1 つの外部コマンドにする (`a; b` だと bash は途中のジョブの後で前面を取り戻し、trap を外しても緑のまま通る。変異で実測)
		bangCase{name: "slow", shape: "job", shell: "/bin/sh", cmd: "/bin/sh -c '/bin/sleep 3; /bin/echo ran-bang'", wantBack: true},
		// 子の実行中に外のシェルが前面を取り戻した (Ctrl-Z → bg の後の形): 戻った treefiler はシェルから前面を奪わない
		// (奪うと孤児になったシェルのグループの読みが EIO になる)。session の先頭の helper がシェル役で、子が印のファイルを
		// 作った後に前面を自分へ戻し、子が終わった後も前面が自分にあることを LEADER の行で返す
		// 子は `e` の GUI エディタと同じく、シェルを挟まず端末に触らないもの (`!` の包みの対話のシェルは、終わるときに自分で
		// 前面を treefiler のグループへ戻すので、この形にならない。実測)
		bangCase{name: "shell-took-back", shape: "job", shell: "/bin/sh",
			argv: []string{"/bin/sh", "-c", `: > "$TREEFILER_BANG_PTY_TAKEBACK"; exec /bin/sleep 1`}, takeBack: true, want: "LEADER kept=true"},
		// キー待ちの Ctrl-\ はただのキー (前面が戻った後の SIGQUIT は treefiler 自身を落とす)
		bangCase{name: "ctrl-backslash", shape: "job", shell: shells[len(shells)-1], cmd: "/bin/echo ran-bang", key: "\x1c", wantKeyWait: true, wantBack: true},
	)
	for _, c := range cases {
		t.Run(c.shape+"/"+filepath.Base(c.shell)+"/"+c.name, func(t *testing.T) { runBangOnPTY(t, c) })
	}
}

type bangCase struct {
	name, shape, shell string
	cmd                string   // `!` に打つコマンド行
	argv               []string // 包まずに起こす
	raw                bool     // 前面を取り戻さずに起こす (負の対照)
	takeBack           bool     // job の形で、子の実行中に session の先頭 (シェル役) が前面を取り戻す
	trigger, send      string   // trigger が出力に出たら send を送る (実行中のキー)
	key                string   // キー待ちに送るキー (既定 "x")
	want               string   // 出力に出るはずの文字列 (既定 "ran-bang")
	wantKeyWait        bool
	wantBack           bool
}

const keyWaitText = "何かキーを押すと"

func runBangOnPTY(t *testing.T, c bangCase) {
	home := t.TempDir() // 対話のシェルにユーザーの rc を読ませない (空の .zshrc は zsh-newuser-install の対話を止める)
	mustWrite(t, home+"/.zshrc", "")
	t.Setenv("SHELL", c.shell)
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", home)
	t.Setenv("ENV", "")
	t.Setenv("BASH_ENV", "")
	if c.takeBack {
		t.Setenv(bangTakeBackEnv, home+"/took-back") // ExecRequest の Env (newTest の後に作る) と helper の両方へ届く
	}

	m := newTest(t)
	var r ExecRequest
	if c.argv != nil {
		r = ExecRequest{Dir: m.workDir(), Argv: c.argv, Env: m.execEnv()}
	} else {
		m.HandleKey("!")
		typeText(m, c.cmd)
		m.HandleKey("enter")
		var ok bool
		if r, ok = m.TakeExec(); !ok {
			t.Fatal("! で起こすプロセスが無い")
		}
	}
	req, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestBangHelperOnPTY$")
	// GORACE: -race のバイナリは終わる前に既定で 1 秒待ち、helper は 1 ケースで 1〜2 回終わる (42 秒 → 7 秒。issue 708)。
	// helper の終了コードは見ていないので、helper の中の race は待っても待たなくても出力にしか出ない
	cmd.Env = append(os.Environ(), bangHelperEnv+"="+string(req), bangShapeEnv+"="+c.shape, bangRawEnv+"="+strconv.FormatBool(c.raw),
		"GORACE=atexit_sleep_ms=0")
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 100})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var out bytes.Buffer
	read := make(chan struct{})
	go func() {
		defer close(read)
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			mu.Lock()
			out.Write(buf[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	text := func() string { mu.Lock(); defer mu.Unlock(); return out.String() }
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	isDone := func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}
	t.Cleanup(func() {
		if !isDone() {
			// hang guard で落ちたときだけ。孫 (止まった sh・stty) は、session の先頭が死んで孤児になったグループへの
			// SIGHUP・SIGCONT と、擬似端末を閉じた SIGHUP で終わる
			_ = cmd.Process.Kill()
			<-done
		}
		_ = master.Close()
	})
	send := func(s string) { _, _ = master.WriteString(s) } // helper が先に抜けていたら EIO になるだけ (判定は READY で行う)
	seen := func(s string) func() bool {
		return func() bool { o := text(); return strings.Contains(o, s) || strings.Contains(o, "READY ") || isDone() }
	}

	if c.trigger != "" {
		waitFor(t, c.trigger, seen(c.trigger))
		send(c.send)
	}
	waitFor(t, "キー待ちの案内か helper の結果", seen(keyWaitText))
	if o := text(); strings.Contains(o, keyWaitText) && !strings.Contains(o, "READY ") {
		key := c.key
		if key == "" {
			key = "x"
		}
		send(key)
	}
	waitFor(t, "helper の結果", seen("READY "))
	got := text()
	i := strings.Index(got, "READY ")
	if i < 0 {
		t.Fatalf("helper の結果が無い (helper が落ちた):\n%s", got)
	}
	readsLine := c.wantBack
	if want := fmt.Sprintf("start-fg=true err=<nil> back=%v", c.wantBack); readyLine(got[i:]) != want {
		t.Fatalf("got  %s\nwant %s\n(back=false = 前面が戻っていない)\n%s", readyLine(got[i:]), want, got)
	}
	if readsLine {
		send("y\n")
	}
	waitFor(t, "helper の終了", isDone)
	_ = master.Close()
	<-read

	got = text()
	if readsLine && !strings.Contains(got, `LINE "y"`) {
		t.Fatalf("キー待ちに送ったキーを dd が読んでいない (helper が読んだ行が y でない):\n%s", got)
	}
	want := c.want
	if want == "" {
		want = "ran-bang"
	}
	if !strings.Contains(got, want) {
		t.Fatalf("出力に %q が無い:\n%s", want, got)
	}
	if strings.Contains(got, keyWaitText) != c.wantKeyWait {
		t.Fatalf("キー待ちの案内の有無 = %v、want %v:\n%s", !c.wantKeyWait, c.wantKeyWait, got)
	}
}

func readyLine(s string) string {
	s = strings.TrimPrefix(s, "READY ")
	return strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
}

const (
	bangHelperEnv = "TREEFILER_BANG_PTY_HELPER"
	bangShapeEnv  = "TREEFILER_BANG_PTY_SHAPE"
	bangRawEnv    = "TREEFILER_BANG_PTY_RAW"
	// bangTakeBackEnv は子が作る印のファイル。session の先頭の helper はそれを見てから前面を自分へ戻す
	bangTakeBackEnv = "TREEFILER_BANG_PTY_TAKEBACK"
)

// TestBangHelperOnPTY は TestBangGivesTerminalBackOnPTY が擬似端末の上で起こす helper (直接走らせたときは何もしない)。
func TestBangHelperOnPTY(t *testing.T) {
	raw := os.Getenv(bangHelperEnv)
	if raw == "" {
		t.Skip("TestBangGivesTerminalBackOnPTY が起こす helper")
	}
	if os.Getenv(bangShapeEnv) == "job" { // 自分 (session の先頭) の下に、前景ジョブとして helper をもう 1 つ起こす
		c := exec.Command(os.Args[0], os.Args[1:]...)
		c.Env = append(os.Environ(), bangShapeEnv+"=leaf")
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
		c.SysProcAttr = &syscall.SysProcAttr{Foreground: true, Ctty: 0}
		mark := os.Getenv(bangTakeBackEnv)
		if err := c.Start(); err != nil {
			fmt.Printf("\nHELPER %v\n", err)
			os.Exit(0)
		}
		if mark != "" { // シェルが bg の後に前面を取り戻すのと同じ: 前面でないので SIGTTOU を無視して移す
			for deadline := time.Now().Add(10 * time.Second); ; {
				if _, err := os.Stat(mark); err == nil || time.Now().After(deadline) {
					break
				}
				time.Sleep(5 * time.Millisecond) // sleep-ok: tick: 子が印のファイルを作るのを条件で待つループの刻み
			}
			signal.Ignore(syscall.SIGTTOU)
			if err := unix.IoctlSetPointerInt(0, unix.TIOCSPGRP, syscall.Getpgrp()); err != nil {
				fmt.Printf("\nHELPER take-back %v\n", err)
			}
		}
		if err := c.Wait(); err != nil {
			fmt.Printf("\nHELPER %v\n", err)
		}
		if mark != "" {
			fg, err := unix.IoctlGetInt(0, unix.TIOCGPGRP)
			fmt.Printf("\nLEADER kept=%v\n", err == nil && fg == syscall.Getpgrp())
		}
		os.Exit(0)
	}
	var r ExecRequest
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		fmt.Printf("\nHELPER bad-request %v\n", err)
		os.Exit(0)
	}
	mine := syscall.Getpgrp()
	fg, fgErr := unix.IoctlGetInt(0, unix.TIOCGPGRP)
	c := exec.Command(r.Argv[0], r.Argv[1:]...)
	c.Dir, c.Env = r.Dir, r.Env
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr // 端末をそのまま渡す (tea.Exec と同じ。グループは分けない)
	var err error
	if os.Getenv(bangRawEnv) == "true" {
		err = c.Run()
	} else {
		x := foreground.NewExec(c)
		if err = x.Run(); x.Err != nil {
			fmt.Printf("\nHELPER reclaim %v\n", x.Err)
		}
	}
	after, afterErr := unix.IoctlGetInt(0, unix.TIOCGPGRP)
	back := afterErr == nil && after == mine
	fmt.Printf("\nREADY start-fg=%v err=%v back=%v\n", fgErr == nil && fg == mine, err, back)
	if back { // 前面に居ないなら読まない (読むと job の形では SIGTTIN で止まる)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		fmt.Printf("LINE %q\n", strings.TrimSpace(line))
	}
	os.Exit(0)
}
