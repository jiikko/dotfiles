package foreground_test

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"

	"pro-con/foreground"
	"pro-con/foreground/ptytest"
)

func TestMain(m *testing.M) {
	ptytest.Helper(steal)
	os.Exit(m.Run())
}

// steal は mode の起こし方 (pgid = 別のプロセスグループ / sid = Detached) で、孫に `zsh -i -c` を走らせる
// (2026-09-26 に画面を止めた、テストの係の make test の中の形。zsh がグループの先頭でないとき、job control を入れるために
// 前面を自分のグループへ移したまま終わる)。その後の前面と、Reclaim で取り戻せるかを 1 行で返す。
func steal(mode string) string {
	fd := int(os.Stdin.Fd())
	_, mine, _ := foreground.Owner(fd)
	c := exec.Command("sh", "-c", "zsh -f -i -c true </dev/null; exec 3</dev/tty && echo tty-open || echo tty-closed")
	var out strings.Builder
	c.Stdout = &out
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if mode == "sid" {
		c.SysProcAttr = foreground.Detached()
	}
	if err := c.Run(); err != nil {
		return "run-failed " + err.Error()
	}
	after, _, _ := foreground.Owner(fd)
	was, err := foreground.Reclaim(fd)
	back, _, _ := foreground.Owner(fd)
	want := 0 // Reclaim が返すのは、移す前の前面のグループ (外れていなければ 0)
	if after != mine {
		want = after
	}
	// 取り戻した後も SIGTTOU を無視しない (無視が残ると、前面でないまま端末の設定を書いても止まらず、子へも引き継がれる)
	return fmt.Sprintf("child=%s stolen=%v was-ok=%v back=%v err=%v ttou-ignored=%v",
		strings.TrimSpace(out.String()), after != mine, was == want, back == mine, err, signal.Ignored(syscall.SIGTTOU))
}

// 別のプロセスグループで起こした子の孫 (zsh -i -c) は前面を奪う。Reclaim はそれを自分へ取り戻す。
func TestReclaimTakesBackForegroundStolenByInteractiveZsh(t *testing.T) {
	if got, want := ptytest.Run(t, "pgid"), "child=tty-open stolen=true was-ok=true back=true err=<nil> ttou-ignored=false"; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// Detached で起こした子は制御端末を持たない: 孫の zsh -i -c は前面を奪えず、/dev/tty も開けない。
func TestDetachedChildCannotTakeTerminal(t *testing.T) {
	if got, want := ptytest.Run(t, "sid"), "child=tty-closed stolen=false was-ok=true back=true err=<nil> ttou-ignored=false"; got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
