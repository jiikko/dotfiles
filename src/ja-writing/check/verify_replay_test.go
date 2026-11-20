package check

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"

	"ja-writing/cmdenv"

	"github.com/jiikko/dotfiles/src/testwait"
)

// recordedHangFixture は、本物の子で run を 1 回動かして記録を作った hangFixture (子の代わりは後で差し替える)。
func recordedHangFixture(t *testing.T) *hangFixture {
	t.Helper()
	h := newHangFixture(t)
	var stdout, stderr bytes.Buffer
	env := cmdenv.Env{Args: []string{"run", "--source", "src.md", "--final", "final.md", "--run-id", "r1"}, Stdout: &stdout, Stderr: &stderr, Cwd: h.dir, Environ: h.environ}
	if rc := MainWith(context.Background(), env, childRuntime(t)); rc != 0 {
		t.Fatalf("run: rc=%d stdout=%q stderr=%q", rc, stdout.String(), stderr.String())
	}
	return h
}

// 作り直しの子を起動できない・シグナルで止まったときは、状態を決めずに本家と同じ内部エラーの文で 3 にする
// (記録の突き合わせは内部エラーの行の後ろを伏せるので、文はここで本家 (Python 3.15.0 で実測) と比べる)。
func TestReplayCannotRunChildren(t *testing.T) {
	h := recordedHangFixture(t)
	killer := h.dir + "/killer.sh"
	if err := os.WriteFile(killer, []byte("#!/bin/sh\nkill -9 $$\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	missing := h.dir + "/nope.sh"
	const tail = "）。記録の食い違いではありません。子プロセスを動かせる環境で verify --replay をもう一度実行してください。\n"
	for _, c := range []struct{ exe, want string }{
		{killer, "yomiyasu_check.py: 内部エラー: --replay で検査を動かせません（lint: シグナル9で止まりました" + tail},
		{missing, "yomiyasu_check.py: 内部エラー: --replay で検査を動かせません（lint: 子プロセスを動かせません（FileNotFoundError: [Errno 2] No such file or directory: '" + missing + "'）" + tail},
	} {
		exe := c.exe
		h.rt.Executable = func() (string, error) { return exe, nil }
		out := waitOutcome(t, h.startCommand(context.Background(), "verify", "--final", "final.md", "--replay"))
		if out.rc != 3 || out.stdout != "" || out.stderr != c.want {
			t.Errorf("%s: rc=%d stdout=%q stderr=%q", exe, out.rc, out.stdout, out.stderr)
		}
	}
}

// 外から止められた verify --replay は、作り直しで動いている子を kill してから 128+番号で終わり、結果を出さない。
func TestReplayStopsChildrenOnSignal(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			h := recordedHangFixture(t)
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			done := h.startCommand(ctx, "verify", "--final", "final.md", "--replay", "--timeout", "60")
			pid := h.childPID(t)
			cancel(cmdenv.Signaled{Signal: sig})
			out := waitOutcome(t, done)
			want := fmt.Sprintf("yomiyasu_check.py: 止められました（%s）。動いていた検査を止めて終わります。\n", signalName(sig))
			if out.rc != 128+int(sig) || out.stderr != want || out.stdout != "" {
				t.Fatalf("rc=%d stdout=%q stderr=%q", out.rc, out.stdout, out.stderr)
			}
			if alive(pid) {
				t.Fatalf("子 %d が止まっていない", pid)
			}
		})
	}
}

// 作り直しの子が時間切れになったら kill し、incomplete (replay=timeout) の 1 で終わる。
func TestReplayKillsChildAfterTimeout(t *testing.T) {
	h := recordedHangFixture(t)
	done := h.startCommand(context.Background(), "verify", "--final", "final.md", "--replay", "--timeout", "0.5")
	pid := h.childPID(t)
	out := waitOutcome(t, done)
	if out.rc != 1 || !bytes.Contains([]byte(out.stdout), []byte("replay=timeout")) {
		t.Fatalf("rc=%d stdout=%q stderr=%q", out.rc, out.stdout, out.stderr)
	}
	testwait.Until(t, "時間切れの子が止まる", func() bool { return !alive(pid) })
}

// 止めた後に作り直しへ入った読みは、子を起こさない (untilStopped が読みの終わりを待たずに戻った後、読みの goroutine が作り直しへ進む窓。
// 起こすと、プロセスが終わった後も子が残る)。子を起こさなかったことは、本家の _ReplayError と同じ「起動できない」の誤りで出る。
func TestReplayAfterStopStartsNoChildren(t *testing.T) {
	h := recordedHangFixture(t)
	record, err := ReadRecord(h.dir + "/.yomiyasu/runs/r1/attempt-1.json")
	if err != nil || record == nil {
		t.Fatalf("記録を読めない: %v", err)
	}
	r := &replayer{ctx: context.Background(), rt: h.rt, env: cmdenv.Env{Environ: h.environ}, timeout: 60}
	r.stop()
	err = func() (err error) {
		defer catchFailure(&err)
		r.run(h.dir+"/.yomiyasu", record, Released{})
		return nil
	}()
	var re replayError
	if !errors.As(err, &re) || re.detail != "lint: 子プロセスを動かす前に止められました" {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(h.pidFile); !os.IsNotExist(statErr) {
		t.Fatalf("止めた後に子が起動した (pid のファイル: %v)", statErr)
	}
}
