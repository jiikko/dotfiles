package supervisor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// rig は sh の台本を子にして、起こした回数と出来事を数える。
type rig struct {
	mu     sync.Mutex
	starts int
	events []Event
}

func (r *rig) spec(script string) Spec {
	return Spec{
		Command: func() (*exec.Cmd, error) {
			r.mu.Lock()
			r.starts++
			r.mu.Unlock()
			return exec.Command("sh", "-c", script), nil
		},
		OnEvent: func(e Event) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.events = append(r.events, e)
		},
		RestartWait: time.Millisecond, RetryWait: time.Millisecond,
		CrashLimit: 2, CrashWindow: time.Hour, StopWait: 5 * time.Second,
	}
}

func (r *rig) snapshot() (int, []Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.starts, append([]Event(nil), r.events...)
}

func kinds(evs []Event) []EventKind {
	out := make([]EventKind, len(evs))
	for i, e := range evs {
		out[i] = e.Kind
	}
	return out
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

// 落ち続ける子は CrashLimit 回まで起こし直し、超えたら諦める。
func TestCrashLoopGivesUp(t *testing.T) {
	r := &rig{}
	res := Run(context.Background(), r.spec("exit 1"))
	starts, evs := r.snapshot()
	if res.Reason != ReasonGaveUp || ExitCode(res.Err) != 1 {
		t.Fatalf("諦めない: %+v", res)
	}
	if starts != 3 || !slices.Equal(kinds(evs), []EventKind{EventCrashed, EventCrashed, EventGaveUp}) || evs[2].Crashes != 3 || evs[0].Wait != time.Millisecond {
		t.Fatalf("起こし直しの回数 / 出来事が違う: starts=%d events=%+v", starts, evs)
	}
}

// CrashWindow より前に落ちた分は数えない (間を空けて落ちるだけなら起こし直し続ける)。
func TestCrashWindowForgetsOldCrashes(t *testing.T) {
	r := &rig{}
	s := r.spec("exit 1")
	var mu sync.Mutex
	clock := time.Unix(0, 0)
	s.Now = func() time.Time { // 起こすたびに 1 時間進む
		mu.Lock()
		defer mu.Unlock()
		clock = clock.Add(time.Hour + time.Second)
		return clock
	}
	s.Continue = func() bool { n, _ := r.snapshot(); return n < 6 }
	res := Run(context.Background(), s)
	if starts, evs := r.snapshot(); res.Reason != ReasonHalted || starts != 6 {
		t.Fatalf("窓の外の落ちを数えて諦めた: %+v starts=%d events=%+v", res, starts, evs)
	}
}

// Retry は数えずに起こし直し続け、続いている間は Repeat を立てる。
func TestRetryIsNotCounted(t *testing.T) {
	r := &rig{}
	s := r.spec("exit 3")
	s.Classify = func(err error) Outcome {
		if ExitCode(err) == 3 {
			return Retry
		}
		return Crash
	}
	s.Continue = func() bool { n, _ := r.snapshot(); return n < 6 }
	res := Run(context.Background(), s)
	_, evs := r.snapshot()
	if res.Reason != ReasonHalted || len(evs) != 6 {
		t.Fatalf("Retry を落ちたと数えた: %+v events=%+v", res, evs)
	}
	for i, e := range evs {
		if e.Kind != EventRetrying || e.Repeat != (i > 0) {
			t.Fatalf("%d 番目の出来事が違う: %+v", i, e)
		}
	}
}

// Done なら起こし直さずに終える (rc=0 を「自分で終えた」と扱う呼び出し方)。
func TestDoneStops(t *testing.T) {
	r := &rig{}
	s := r.spec("exit 0")
	s.Classify = func(err error) Outcome {
		if err == nil {
			return Done
		}
		return Crash
	}
	res := Run(context.Background(), s)
	if starts, evs := r.snapshot(); res.Reason != ReasonDone || res.Err != nil || starts != 1 || len(evs) != 0 {
		t.Fatalf("終えた子を起こし直した: %+v starts=%d events=%+v", res, starts, evs)
	}
}

// Continue は初回の起動の前には呼ばず、起こし直す前に呼ぶ。偽なら起こさない。
func TestContinueIsAskedBeforeRestartOnly(t *testing.T) {
	r := &rig{}
	s := r.spec("exit 1")
	asked := 0
	s.Continue = func() bool { asked++; return false }
	res := Run(context.Background(), s)
	if starts, _ := r.snapshot(); res.Reason != ReasonHalted || starts != 1 || asked != 1 {
		t.Fatalf("初回に Continue を聞いた / 偽でも起こした: %+v starts=%d asked=%d", res, starts, asked)
	}
}

// 起こせないときは出来事にして諦める (起こし直さない)。
func TestStartFailureGivesUp(t *testing.T) {
	for name, cmd := range map[string]func() (*exec.Cmd, error){
		"Command のエラー": func() (*exec.Cmd, error) { return nil, errors.New("no exe") },
		"実行ファイルが無い":    func() (*exec.Cmd, error) { return exec.Command("/nonexistent/x"), nil },
	} {
		for _, lifeline := range []bool{false, true} {
			r := &rig{}
			s := r.spec("")
			s.Command, s.Lifeline = cmd, lifeline
			res := Run(context.Background(), s)
			if _, evs := r.snapshot(); res.Reason != ReasonStartFailed || res.Err == nil || len(evs) != 1 || evs[0].Kind != EventStartFailed {
				t.Fatalf("%s (lifeline=%v): 起こせないのを 1 度だけ知らせて諦めない: %+v events=%+v", name, lifeline, res, evs)
			}
		}
	}
}

// 止める合図で子に StopSignal を送り、子が抜けるのを待つ。止めた後は起こし直さず、落ちたとも知らせない。
func TestStopSendsSignalAndWaits(t *testing.T) {
	dir := t.TempDir()
	mark := filepath.Join(dir, "term")
	ready := filepath.Join(dir, "ready")
	r := &rig{}
	s := r.spec(`trap 'touch "` + mark + `"; exit 0' TERM; touch "` + ready + `"; while :; do sleep 0.01; done`)
	stop := Start(context.Background(), s)
	eventually(t, "子が trap を入れない", func() bool { _, err := os.Stat(ready); return err == nil })
	res := stop()
	if _, err := os.Stat(mark); err != nil {
		t.Fatalf("SIGTERM が子に届かない / 抜けるのを待たずに戻った: %v", err)
	}
	if starts, evs := r.snapshot(); res.Reason != ReasonStopped || starts != 1 || len(evs) != 0 {
		t.Fatalf("止めた後に起こし直した / 落ちたと知らせた: %+v starts=%d events=%+v", res, starts, evs)
	}
	if again := stop(); again != res {
		t.Fatalf("2 度目の stop が違う結果を返した: %+v", again)
	}
}

// StopSignal を無視する子は StopWait の後に SIGKILL する。
func TestStopKillsAfterStopWait(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	r := &rig{}
	s := r.spec(`trap "" TERM; touch "` + ready + `"; while :; do sleep 0.01; done`)
	s.StopWait = 100 * time.Millisecond
	stop := Start(context.Background(), s)
	eventually(t, "子が trap を入れない", func() bool { _, err := os.Stat(ready); return err == nil })
	start := time.Now()
	res := stop()
	if ExitCode(res.Err) != -1 || res.Reason != ReasonStopped {
		t.Fatalf("SIGKILL で止めていない: %+v (%s)", res, time.Since(start))
	}
}

// 生命線: 止めるときに stdin のパイプを閉じる。SIGTERM を無視する子も EOF で抜ける (kill まで待たない)。
func TestLifelineClosesStdinOnStop(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	r := &rig{}
	s := r.spec(`trap "" TERM; touch "` + ready + `"; cat >/dev/null`)
	s.Lifeline, s.StopWait = true, time.Minute
	stop := Start(context.Background(), s)
	eventually(t, "子が trap を入れない", func() bool { _, err := os.Stat(ready); return err == nil })
	done := make(chan Result, 1)
	go func() { done <- stop() }()
	select {
	case res := <-done:
		if res.Err != nil {
			t.Fatalf("EOF で抜けていない: %+v", res)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("stdin を閉じず、kill まで待った")
	}
}

// 生命線: 見張る側が kill -9 で死んでも、子は stdin の EOF で抜ける (このテストのバイナリを見張る側として起こし直して確かめる)。
func TestLifelineSurvivesParentKill(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	parent := exec.Command(os.Args[0], "-test.run=^TestHelperSupervisor$")
	parent.Env = append(os.Environ(), "PROCESS_SUPERVISOR_HELPER="+pidFile)
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	var child int
	eventually(t, "子が立たない", func() bool {
		b, err := os.ReadFile(pidFile)
		if err != nil || !strings.HasSuffix(string(b), "\n") {
			return false
		}
		child, err = strconv.Atoi(strings.TrimSpace(string(b)))
		return err == nil
	})
	_ = parent.Process.Kill()
	_ = parent.Wait()
	eventually(t, "見張る側が死んでも子が残った", func() bool { return syscall.Kill(child, 0) != nil })
}

// TestHelperSupervisor は TestLifelineSurvivesParentKill が起こす見張る側 (PROCESS_SUPERVISOR_HELPER が無ければ何もしない)。
// 子は SIGTERM を無視し、stdin の EOF でだけ抜ける。
func TestHelperSupervisor(t *testing.T) {
	pidFile := os.Getenv("PROCESS_SUPERVISOR_HELPER")
	if pidFile == "" {
		return
	}
	Run(context.Background(), Spec{
		Command: func() (*exec.Cmd, error) {
			return exec.Command("sh", "-c", `trap "" TERM; echo $$ > "`+pidFile+`.tmp"; mv "`+pidFile+`.tmp" "`+pidFile+`"; cat >/dev/null`), nil
		},
		Lifeline: true, CrashLimit: 0, StopWait: time.Minute,
	})
}

func TestExitCode(t *testing.T) {
	if ExitCode(nil) != 0 || ExitCode(errors.New("x")) != -1 {
		t.Fatal("nil / 終了コードを持たないエラーの扱いが違う")
	}
	if err := exec.Command("sh", "-c", "exit 5").Run(); ExitCode(err) != 5 {
		t.Fatalf("終了コードを読めない: %v", err)
	}
}
