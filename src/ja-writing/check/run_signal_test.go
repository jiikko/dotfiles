package check

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"ja-writing/cmdenv"

	"github.com/jiikko/dotfiles/src/testwait"
)

// 止まらない子 (テストのバイナリを本番と同じ入口で起こし、pid をファイルに書いてから眠る。child_test.go の childHang) を差し込んで、
// 時間切れ・シグナルで run が子を kill して終わることを見る。
// 時間は assert しない: 待つのは「pid のファイルが書かれた」と「run が戻った」という条件で、上限は止まったときの安全網。

type hangFixture struct {
	dir, pidFile string
	rt           Runtime
}

func newHangFixture(t *testing.T) *hangFixture {
	t.Helper()
	dir := fixtureRoot(t)
	if err := os.Mkdir(dir+"/.git", 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"src.md": vSourceText, "final.md": vFinalText} {
		if err := os.WriteFile(dir+"/"+name, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h := &hangFixture{dir: dir, pidFile: dir + "/child.pid"}
	h.rt = childRuntime(t)
	h.rt.ChildEnv = func() []string { return append(os.Environ(), childEnvName+"="+childHang, "JA_PIDFILE="+h.pidFile) }
	temporary := t.TempDir()
	h.rt.TempDir = func() string { return temporary }
	return h
}

const (
	vSourceText = "今日は良い天気です。散歩に行きました。\n"
	vFinalText  = "今日は良い天気でした。散歩に行った。\n"
)

// childPID は子が書いた pid (書かれるまで待つ)。
func (h *hangFixture) childPID(t *testing.T) int {
	t.Helper()
	var pid int
	testwait.Until(t, "子が pid を書く", func() bool {
		data, err := os.ReadFile(h.pidFile)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
		return err == nil
	})
	return pid
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

type runOutcome struct {
	rc             int
	stdout, stderr string
}

func (h *hangFixture) start(ctx context.Context, args ...string) chan runOutcome {
	done := make(chan runOutcome, 1)
	go func() {
		var stdout, stderr bytes.Buffer
		rc := MainWith(ctx, cmdenv.Env{Args: append([]string{"run"}, args...), Stdout: &stdout, Stderr: &stderr, Cwd: h.dir, Getenv: os.Getenv}, h.rt)
		done <- runOutcome{rc, stdout.String(), stderr.String()}
	}()
	return done
}

func waitOutcome(t *testing.T, done chan runOutcome) runOutcome {
	t.Helper()
	select {
	case out := <-done:
		return out
	case <-time.After(30 * time.Second):
		t.Fatal("run が戻らない (子を kill していない)")
	}
	return runOutcome{}
}

// leftovers は状態フォルダの runs/ の下に残った回のファイル。
func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	found, err := filepath.Glob(dir + "/.yomiyasu/runs/*/attempt-*")
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// --timeout を超えた子は kill され、run は partial (時間切れ) の 3 で終わる。
func TestRunKillsChildAfterTimeout(t *testing.T) {
	h := newHangFixture(t)
	done := h.start(context.Background(), "--source", "src.md", "--final", "final.md", "--run-id", "t1", "--timeout", "0.5")
	pid := h.childPID(t)
	out := waitOutcome(t, done)
	if out.rc != 3 || !strings.Contains(out.stdout, "lint、diffが時間切れ") || !strings.Contains(out.stdout, "failed=lint(timeout),diff(timeout)") {
		t.Fatalf("rc=%d stdout=%q stderr=%q", out.rc, out.stdout, out.stderr)
	}
	testwait.Until(t, "時間切れの子が止まる", func() bool { return !alive(pid) })
}

// 外から止められた (ctx の取り消しの cause がシグナル) run は、動いている子を kill し、128+番号で終わり、記録の回を残さない。
// --no-record の run は一時フォルダの写しを消す。シグナルを ctx の取り消しにするのは entry.Run (entry のテストが本物のシグナルで見る)。
func TestRunStopsChildrenOnSignal(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		for _, noRecord := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/no-record=%v", sig, noRecord), func(t *testing.T) {
				h := newHangFixture(t)
				ctx, cancel := context.WithCancelCause(context.Background())
				defer cancel(nil)
				args := []string{"--source", "src.md", "--final", "final.md", "--run-id", "s1", "--timeout", "60"}
				if noRecord {
					args = append(args, "--no-record")
				}
				done := h.start(ctx, args...)
				pid := h.childPID(t)
				cancel(cmdenv.Signaled{Signal: sig})
				out := waitOutcome(t, done)
				want := "yomiyasu_check.py: 止められました（" + signalName(sig) + "）。動いていた検査を止めて終わります。\n"
				if out.rc != 128+int(sig) || out.stderr != want || out.stdout != "" {
					t.Fatalf("rc=%d stdout=%q stderr=%q", out.rc, out.stdout, out.stderr)
				}
				if alive(pid) {
					t.Fatalf("子 %d が止まっていない", pid)
				}
				if left := leftovers(t, h.dir); len(left) != 0 {
					t.Fatalf("止められた run の回が残った: %v", left)
				}
				temporary, err := filepath.Glob(filepath.Join(h.rt.TempDir(), "yomiyasu-check-*"))
				if err != nil || len(temporary) != 0 {
					t.Fatalf("一時フォルダが残った: %v %v", temporary, err)
				}
			})
		}
	}
}

// 取り消しの cause がシグナルでない ctx は SIGINT とみなす (本家は Ctrl+C の KeyboardInterrupt)。
func TestStoppedTreatsPlainCancelAsInterrupt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stop errStopped
	if err := stopped(ctx); !errors.As(err, &stop) || stop.signal != syscall.SIGINT {
		t.Fatalf("stopped = %v", err)
	}
	if err := stopped(context.Background()); err != nil {
		t.Fatalf("取り消していない ctx で %v", err)
	}
}
