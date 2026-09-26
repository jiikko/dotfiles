package upgrade

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func readBack(t *testing.T, f *os.File) string {
	t.Helper()
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Keep のあいだだけ「alt screen を抜ける」「カーソルを出す」を落とす (bubbletea の終了の出力を模す)。ほかの列はそのまま。
func TestScreenKeepDropsLeavingAltScreen(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewScreen(f)
	closing := "\x1b[>4;0m\x1b[?1049l\x1b[?25h\x1b[?2004l"
	if n, err := s.Write([]byte(closing)); err != nil || n != len(closing) {
		t.Fatalf("Write: n=%d err=%v", n, err)
	}
	s.Keep()
	if n, err := s.Write([]byte(closing)); err != nil || n != len(closing) {
		t.Fatalf("Keep の Write は渡された長さを返すはず: n=%d err=%v", n, err)
	}
	s.Release()
	_, _ = s.Write([]byte("\x1b[?1049l"))
	want := closing + "\x1b[>4;0m\x1b[?2004l" + "\x1b[?1049l"
	if got := readBack(t, f); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// alt screen を bubbletea の外で持っている間に割り込み (ctrl+c = SIGINT) を受けたら、alt screen を抜けてから終わる。
// 見張りを外した後は何も書かない。子のプロセス (このテストのバイナリ) で確かめる。
func TestGuardAltScreenLeavesOnSignal(t *testing.T) {
	if os.Getenv("GUARD_ALT_CHILD") == "1" {
		stop := GuardAltScreen(os.Stdout)
		if os.Getenv("GUARD_ALT_STOP") == "1" {
			stop()
			signal.Ignore(syscall.SIGINT) // 外した後に既定の動作で死なないよう (外した見張りが書かないことだけを見る)
		}
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
		if os.Getenv("GUARD_ALT_STOP") != "1" {
			select {} // 見張りが os.Exit する (しなければ親の上限で殺される)
		}
		// 外した見張りが書かないことの確認は、書くならこの間に書く猶予として短く待つ (否定の確認なので時間に頼る)
		time.Sleep(200 * time.Millisecond)
		os.Exit(0)
	}
	for _, stopped := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGuardAltScreenLeavesOnSignal$")
		cmd.Env = append(os.Environ(), "GUARD_ALT_CHILD=1")
		if stopped {
			cmd.Env = append(cmd.Env, "GUARD_ALT_STOP=1")
		}
		out, err := cmd.Output()
		code := 0
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		}
		left := strings.Contains(string(out), "\x1b[?1049l")
		if !stopped && (!left || code != 130) {
			t.Fatalf("SIGINT で alt screen を抜けて 130 で終わるはず: left=%v code=%d out=%q", left, code, out)
		}
		if stopped && left {
			t.Fatalf("外した見張りが書いた: %q", out)
		}
	}
}
