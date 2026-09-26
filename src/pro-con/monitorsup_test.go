package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pro-con/dispatcher"
	"pro-con/monitor"
	"pro-con/store"
	supervisor "process_supervisor"
)

// supRig は起こし直しの係を、sh の台本を見張りの代わりにして回す。
type supRig struct {
	mu     sync.Mutex
	said   []string
	starts int
}

func (r *supRig) sup(script string) supervisor.Spec {
	s := monitorSpec(func(text string) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.said = append(r.said, text)
	}, nil, nil)
	s.Command = func() (*exec.Cmd, error) {
		r.mu.Lock()
		r.starts++
		r.mu.Unlock()
		return exec.Command("sh", "-c", script), nil
	}
	s.RestartWait, s.RetryWait, s.CrashLimit = time.Millisecond, time.Millisecond, 2
	return s
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

// 落ち続ける見張りは crashLimit 回まで起こし直し、超えたら起こし直さずに出来事にする。rc=0 で抜けたのも落ちたと数える。
func TestMonitorCrashLoopStops(t *testing.T) {
	for script, why := range map[string]string{"exit 1": "exit status 1", "exit 0": "rc=0"} {
		r := &supRig{}
		stop := superviseMonitor(context.Background(), r.sup(script))
		eventually(t, "起こし直すのをやめない", func() bool {
			_, said := r.snapshot()
			return len(said) > 0 && strings.Contains(said[len(said)-1], "起こし直さない")
		})
		stop()
		starts, said := r.snapshot()
		if starts != 3 || len(said) != 3 || !strings.Contains(said[0], "見張りが抜けた ("+why+")") || !strings.Contains(said[2], "3 回抜けた") {
			t.Fatalf("%s: 起こし直しの回数 / 出来事が違う: starts=%d said=%v", script, starts, said)
		}
	}
}

// 前の見張りが lock を持っていて抜けた (exitLockHeld) のは落ちたと数えず、出来事も 1 度だけにして起こし直し続ける。
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

// 見張りは生命線の stdin を受け取る (dispatcher が死んだら抜ける = --until-stdin-closes と対)。
func TestMonitorSpecUsesLifeline(t *testing.T) {
	if s := monitorSpec(func(string) {}, nil, nil); !s.Lifeline {
		t.Fatal("見張りに生命線を渡していない (dispatcher が kill -9 で死んでも見張りが残る)")
	}
}

// 起こせない (実行ファイルが無い) ときは、出来事にして起こし直さない。
func TestMonitorStartFailureIsReported(t *testing.T) {
	r := &supRig{}
	s := r.sup("")
	s.Command = func() (*exec.Cmd, error) { return exec.Command("/nonexistent/pro-con"), nil }
	stop := superviseMonitor(context.Background(), s)
	defer stop()
	eventually(t, "起こせないと書かない", func() bool { _, said := r.snapshot(); return len(said) == 1 })
	time.Sleep(20 * time.Millisecond)
	if _, said := r.snapshot(); len(said) != 1 || !strings.Contains(said[0], "見張りを起こせない") {
		t.Fatalf("起こせないのを 1 度だけ書かない: %v", said)
	}
}

// 別の見張りが lock を持っていたら、何もせずに exitLockHeld で抜ける (起こした dispatcher が落ちたと数えない印)。
func TestRunMonitorExitsHeldWhenLocked(t *testing.T) {
	dir := t.TempDir()
	unlock, err := dispatcher.LockMonitor(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	var out, errOut bytes.Buffer
	if rc := runMonitor([]string{"--once"}, dir, nil, &out, &errOut); rc != exitLockHeld {
		t.Fatalf("lock を持たれているのに rc=%d: %s", rc, errOut.String())
	}
	unlock()
	if rc := runMonitor([]string{"--once"}, dir, nil, &out, &errOut); rc != 0 {
		t.Fatalf("lock が空いたのに見ない: rc=%d: %s", rc, errOut.String())
	}
}

// syncBuffer は子の stdout を並行に書かれてよい形で受ける。
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (w *syncBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (w *syncBuffer) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}

// 見張りは同じ失敗を毎回ログに書かない (取り込む先の無い repo は直すまで毎分失敗する)。直ったら 1 度書く。
func TestWatchLogsSameErrorOnce(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, store.StateFile)
	if err := os.WriteFile(broken, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &monitor.Monitor{Dir: dir}
	ctx, cancel := context.WithCancel(context.Background())
	var out, errOut syncBuffer
	done := make(chan int)
	go func() { done <- watch(ctx, m, time.Millisecond, false, &out, &errOut) }()
	eventually(t, "失敗を書かない", func() bool { return strings.Contains(errOut.String(), "読めない") })
	time.Sleep(30 * time.Millisecond) // 何周も失敗させる
	if err := os.Remove(broken); err != nil {
		t.Fatal(err)
	}
	eventually(t, "直ったと書かない", func() bool { return strings.Contains(errOut.String(), "前の失敗は直った") })
	cancel()
	<-done
	if n := strings.Count(errOut.String(), "読めない"); n != 1 {
		t.Fatalf("同じ失敗を %d 回書いた:\n%s", n, errOut.String())
	}
}
