package entry

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jiikko/dotfiles/src/testwait"
)

// 本物のシグナルをプロセスに送って、Run が subcommand ごとに本家と同じ止まり方をすることを見る。テストのバイナリを、印
// (entryChildEnv) を付けて起こし直し、本番の main と同じ Run(args, Main) を通す。
// 時間は assert しない: 待つのは「子が標準入力を読み始めた」と「子が終わった」という条件で、上限は止まったときの安全網。

const entryChildEnv = "JA_WRITING_ENTRY_CHILD"

func TestMain(m *testing.M) {
	if os.Getenv(entryChildEnv) == "1" {
		os.Exit(Run(os.Args[1:], Main))
	}
	os.Exit(m.Run())
}

type signalChild struct {
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	stdout, stderr *bytes.Buffer
	done           chan error
}

// startReading は args で子を起こし、子が標準入力を読み始めるまで待つ。標準入力は閉じない (子は読み終わらずに待ち続ける)。
// 読み始めたことは、パイプの容量より大きい書き込みが戻ったことで知る (戻るのは子が読んで空きを作ったとき。シグナルの受け方は読む前に整っている)。
func startReading(t *testing.T, args ...string) *signalChild {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	c := &signalChild{cmd: exec.Command(exe, args...), stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, done: make(chan error, 1)}
	c.cmd.Dir = dir
	c.cmd.Env = append(os.Environ(), entryChildEnv+"=1")
	c.cmd.Stdout, c.cmd.Stderr = c.stdout, c.stderr
	if c.stdin, err = c.cmd.StdinPipe(); err != nil {
		t.Fatal(err)
	}
	if err := c.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.cmd.Process.Kill() // 止まらなかった子を残さない (終わっていれば何もしない)
		_ = c.stdin.Close()
	})
	go func() { c.done <- c.cmd.Wait() }()
	written := make(chan struct{})
	go func() {
		_, _ = c.stdin.Write(bytes.Repeat([]byte("あ"), 100000))
		close(written)
	}()
	testwait.Until(t, "子が標準入力を読み始める", func() bool {
		select {
		case <-written:
			return true
		default:
			return false
		}
	})
	return c
}

// wait は子が終わるのを待ち、終わった状態を返す。
func (c *signalChild) wait(t *testing.T) *os.ProcessState {
	t.Helper()
	select {
	case err := <-c.done:
		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			t.Fatal(err)
		}
		return c.cmd.ProcessState
	case <-time.After(30 * time.Second):
		t.Fatalf("シグナルの後に子が終わらない stderr=%q", c.stderr.String())
	}
	return nil
}

// lint と diff は本家と同じくシグナルを受けず、そのシグナルで終わる (シェルから見て 128+番号)。
func TestLintAndDiffEndBySignal(t *testing.T) {
	for _, args := range [][]string{{"lint"}, {"diff", "-", "b.md"}} {
		for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
			t.Run(args[0]+"/"+sig.String(), func(t *testing.T) {
				c := startReading(t, args...)
				if err := c.cmd.Process.Signal(sig); err != nil {
					t.Fatal(err)
				}
				state := c.wait(t)
				ws, ok := state.Sys().(syscall.WaitStatus)
				if !ok || !ws.Signaled() || ws.Signal() != sig {
					t.Fatalf("シグナルで終わっていない: %v stdout=%q stderr=%q", state, c.stdout.String(), c.stderr.String())
				}
			})
		}
	}
}

// check は本家の main と同じく SIGINT・SIGTERM・SIGHUP を受け、「止められました」の文を出して 128+番号で終わる (verify も)。
func TestCheckStopsWithMessageOnSignal(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			c := startReading(t, "check", "verify", "--final", "-")
			if err := c.cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			if err := c.stdin.Close(); err != nil {
				t.Fatal(err)
			}
			state := c.wait(t)
			name := map[syscall.Signal]string{syscall.SIGINT: "SIGINT", syscall.SIGTERM: "SIGTERM", syscall.SIGHUP: "SIGHUP"}[sig]
			want := "yomiyasu_check.py: 止められました（" + name + "）。動いていた検査を止めて終わります。\n"
			if state.ExitCode() != 128+int(sig) || !strings.HasSuffix(c.stderr.String(), want) {
				t.Fatalf("rc=%d stdout=%q stderr=%q", state.ExitCode(), c.stdout.String(), c.stderr.String())
			}
		})
	}
}
