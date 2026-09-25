package main

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"pro-con/dispatcher"
)

// supRig は起こし直しの係を、sh の台本を見張りの代わりにして回す。
type supRig struct {
	mu     sync.Mutex
	said   []string
	starts int
}

func (r *supRig) sup(script string) monitorSup {
	return monitorSup{
		command: func() (*exec.Cmd, error) {
			r.mu.Lock()
			r.starts++
			r.mu.Unlock()
			return exec.Command("sh", "-c", script), nil
		},
		say: func(s string) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.said = append(r.said, s)
		},
		now: time.Now, restartWait: time.Millisecond, heldWait: time.Millisecond,
		crashLimit: 2, crashWindow: time.Hour, stopWait: 5 * time.Second,
	}
}

func (r *supRig) snapshot() (int, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.starts, append([]string(nil), r.said...)
}

// eventually は cond が真になるまで待つ (上限 5 秒)。
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; !cond(); i++ {
		if i > 500 {
			t.Fatalf("5 秒たっても %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// 落ち続ける見張りは crashLimit 回まで起こし直し、超えたら起こし直さずに出来事にする。
func TestMonitorCrashLoopStops(t *testing.T) {
	r := &supRig{}
	stop := superviseMonitor(context.Background(), r.sup("exit 1"))
	defer stop()
	eventually(t, "起こし直すのをやめない", func() bool {
		_, said := r.snapshot()
		return len(said) > 0 && strings.Contains(said[len(said)-1], "起こし直さない")
	})
	starts, said := r.snapshot()
	if starts != 3 || len(said) != 3 || !strings.Contains(said[0], "見張りが抜けた (exit status 1)") {
		t.Fatalf("起こし直しの回数 / 出来事が違う: starts=%d said=%v", starts, said)
	}
}

// 前の見張りが lock を持っていて抜けた (monitorExitHeld) のは落ちたと数えず、出来事も 1 度だけにして起こし直し続ける。
func TestMonitorHeldIsNotCounted(t *testing.T) {
	r := &supRig{}
	stop := superviseMonitor(context.Background(), r.sup("exit 3"))
	eventually(t, "起こし直さない", func() bool { n, _ := r.snapshot(); return n >= 6 })
	stop()
	_, said := r.snapshot()
	if len(said) != 1 || !strings.Contains(said[0], "前の見張りが lock を持っている") {
		t.Fatalf("lock を持たれていたのを落ちたと数えた / 何度も書いた: %v", said)
	}
}

// 止めるときは stdin (パイプ) を閉じる: SIGTERM を無視する見張りも EOF で抜ける。止めた後は起こし直さず、落ちたとも書かない。
func TestMonitorStopClosesStdin(t *testing.T) {
	r := &supRig{}
	stop := superviseMonitor(context.Background(), r.sup(`trap "" TERM; cat >/dev/null`))
	eventually(t, "起こさない", func() bool { n, _ := r.snapshot(); return n == 1 })
	time.Sleep(50 * time.Millisecond) // cat が stdin を読み始める
	start := time.Now()
	stop()
	if d := time.Since(start); d >= 5*time.Second {
		t.Fatalf("stdin を閉じず、kill まで待った (%s)", d)
	}
	starts, said := r.snapshot()
	if starts != 1 || len(said) != 0 {
		t.Fatalf("止めた後に起こし直した / 落ちたと書いた: starts=%d said=%v", starts, said)
	}
}

// 起こせない (実行ファイルが無い) ときは、出来事にして起こし直さない。
func TestMonitorStartFailureIsReported(t *testing.T) {
	r := &supRig{}
	s := r.sup("")
	s.command = func() (*exec.Cmd, error) { return exec.Command("/nonexistent/pro-con"), nil }
	stop := superviseMonitor(context.Background(), s)
	defer stop()
	eventually(t, "起こせないと書かない", func() bool { _, said := r.snapshot(); return len(said) == 1 })
	time.Sleep(20 * time.Millisecond)
	if _, said := r.snapshot(); len(said) != 1 || !strings.Contains(said[0], "見張りを起こせない") {
		t.Fatalf("起こせないのを 1 度だけ書かない: %v", said)
	}
}

// 別の見張りが lock を持っていたら、何もせずに monitorExitHeld で抜ける (起こした dispatcher が落ちたと数えない印)。
func TestRunMonitorExitsHeldWhenLocked(t *testing.T) {
	dir := t.TempDir()
	unlock, err := dispatcher.LockMonitor(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	var out, errOut bytes.Buffer
	if rc := runMonitor([]string{"--once"}, dir, nil, &out, &errOut); rc != monitorExitHeld {
		t.Fatalf("lock を持たれているのに rc=%d: %s", rc, errOut.String())
	}
	unlock()
	if rc := runMonitor([]string{"--once"}, dir, nil, &out, &errOut); rc != 0 {
		t.Fatalf("lock が空いたのに見ない: rc=%d: %s", rc, errOut.String())
	}
}
