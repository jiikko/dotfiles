package entry

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
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
	dir            string
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	stdout, stderr *bytes.Buffer
	done           chan error
}

// startReading は args で子を起こし、子が標準入力を読み始めるまで待つ。標準入力は閉じない (子は読み終わらずに待ち続ける)。
func startReading(t *testing.T, args ...string) *signalChild {
	t.Helper()
	c := newSignalChild(t, args...)
	c.start(t)
	c.waitReading(t)
	return c
}

// newSignalChild は args の子を、まだ起こさずに用意する (作業フォルダは c.dir)。
func newSignalChild(t *testing.T, args ...string) *signalChild {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	c := &signalChild{dir: dir, cmd: exec.Command(exe, args...), stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, done: make(chan error, 1)}
	c.cmd.Dir = dir
	c.cmd.Env = append(os.Environ(), entryChildEnv+"=1")
	c.cmd.Stdout, c.cmd.Stderr = c.stdout, c.stderr
	if c.stdin, err = c.cmd.StdinPipe(); err != nil {
		t.Fatal(err)
	}
	return c
}

// start は子を起こす。止まらなかった子は後片付けで kill する。
func (c *signalChild) start(t *testing.T) {
	t.Helper()
	if err := c.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.cmd.Process.Kill() // 止まらなかった子を残さない (終わっていれば何もしない)
		_ = c.stdin.Close()
	})
	go func() { c.done <- c.cmd.Wait() }()
}

// waitReading は子が標準入力を読み始めるまで待つ。読み始めたことは、パイプの容量より大きい書き込みが戻ったことで知る
// (戻るのは子が読んで空きを作ったとき。シグナルの受け方は読む前に整っている)。
func (c *signalChild) waitReading(t *testing.T) {
	t.Helper()
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
// 標準入力を読んでいる間に届いても、読み終わり (EOF) を待たずに止まり、状態フォルダを作らない (本家は入力を読み終えるまで何も書かない)。
func TestCheckStopsWithMessageOnSignal(t *testing.T) {
	for _, args := range [][]string{{"check", "verify", "--final", "-"}, {"check", "run", "--stdin"}, {"check", "run", "--source", "-", "--final", "f.md"}} {
		for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
			t.Run(strings.Join(args[1:], " ")+"/"+sig.String(), func(t *testing.T) {
				c := startReading(t, args...)
				if err := c.cmd.Process.Signal(sig); err != nil {
					t.Fatal(err)
				}
				c.wantStopped(t, sig)
			})
		}
	}
}

// wantStopped は check が sig で止められ、何も出さず (止めた知らせだけ)・状態フォルダを作らずに 128+番号で終わったことを見る。
func (c *signalChild) wantStopped(t *testing.T, sig syscall.Signal) {
	t.Helper()
	state := c.wait(t)
	name := map[syscall.Signal]string{syscall.SIGINT: "SIGINT", syscall.SIGTERM: "SIGTERM", syscall.SIGHUP: "SIGHUP"}[sig]
	want := "yomiyasu_check.py: 止められました（" + name + "）。動いていた検査を止めて終わります。\n"
	if state.ExitCode() != 128+int(sig) || c.stderr.String() != want || c.stdout.String() != "" {
		t.Fatalf("rc=%d stdout=%q stderr=%q", state.ExitCode(), c.stdout.String(), c.stderr.String())
	}
	if _, err := os.Lstat(c.dir + "/.yomiyasu"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("止められた check が状態フォルダを作った: %v", err)
	}
}

// mkfifo は子の作業フォルダに FIFO を作る。
func (c *signalChild) mkfifo(t *testing.T, name string) string {
	t.Helper()
	path := c.dir + "/" + name
	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// パスで渡した入力 (FIFO) を開いている間に届いたシグナルでも、本家と同じく開き終わりを待たずに止まる (書き手の居ない FIFO の open は戻らない)。
// 子が FIFO を開く所まで来たことは見られないので、標準入力 (FIFO より先に読む) を読み始めたのを見てから閉じ、シグナルを送る。
// 届くのが標準入力を読み終わる前でも FIFO を開いている間でも、check は止まって同じ答えになる (どちらも入力を読む間)。
// 守りが無いと、子は FIFO の open で止まったまま終わらない。
func TestCheckStopsWhileOpeningFifoWithoutWriter(t *testing.T) {
	for _, args := range [][]string{{"check", "run", "--source", "-", "--material", "m.fifo", "--final", "f.md"}, {"check", "verify", "--source", "-", "--final", "m.fifo"}} {
		t.Run(strings.Join(args[1:], " "), func(t *testing.T) {
			c := newSignalChild(t, args...)
			c.mkfifo(t, "m.fifo")
			c.start(t)
			c.waitReading(t)
			if err := c.stdin.Close(); err != nil {
				t.Fatal(err)
			}
			if err := c.cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			c.wantStopped(t, syscall.SIGTERM)
		})
	}
}

// 書き手が居て読みが続いている FIFO でも、読み終わりを待たずに止まり、結果を出さない。
// 子が FIFO を開いたことは、書き手を O_NONBLOCK で開けたことで知る (読み手が居ないと ENXIO。読み手の open もこれで戻り、子は read で待つ)。
func TestCheckStopsWhileReadingFifo(t *testing.T) {
	for _, args := range [][]string{{"check", "run", "--material", "r.fifo", "--final", "f.md"}, {"check", "verify", "--final", "r.fifo"}} {
		t.Run(strings.Join(args[1:], " "), func(t *testing.T) {
			c := newSignalChild(t, args...)
			fifo := c.mkfifo(t, "r.fifo")
			c.start(t)
			writer := -1
			testwait.Until(t, "子が FIFO を開く", func() bool {
				fd, err := syscall.Open(fifo, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
				if err != nil {
					return false
				}
				writer = fd
				return true
			})
			t.Cleanup(func() { _ = syscall.Close(writer) })
			if _, err := syscall.Write(writer, []byte("資料の途中です。")); err != nil {
				t.Fatal(err)
			}
			if err := c.cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			c.wantStopped(t, syscall.SIGTERM)
		})
	}
}

// 無視して起動された SIGHUP・SIGINT は、本家の _install_stop_handlers と同じく受けない (nohup で起こした check は SIGHUP で止まらない)。
// 生き残ったことは、続けて送った SIGTERM で止まり、知らせと終了コードが SIGTERM のものであることで見る
// (無視していなければ、先に送ったシグナルで止まる。Go の runtime は届いたシグナルを番号の小さい順に渡し、HUP と INT は TERM より小さい)。
func TestCheckKeepsSignalsIgnoredAtStart(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGHUP, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			c := newSignalChild(t, "check", "verify", "--final", "-")
			// 子は exec のときに無視の設定を引き継ぐ。テストのプロセスが無視するのは子を起こす間だけ
			signal.Ignore(sig)
			c.start(t)
			signal.Reset(sig)
			c.waitReading(t)
			for _, s := range []syscall.Signal{sig, syscall.SIGTERM} {
				if err := c.cmd.Process.Signal(s); err != nil {
					t.Fatal(err)
				}
			}
			c.wantStopped(t, syscall.SIGTERM)
		})
	}
}
