package check

import (
	"context"
	"os"
	"testing"
)

// 締め切りを使い切ってから動かす子は、起動せずに時間切れにする (本家の remaining <= 0 の枝)。
func TestChildrenDoNotStartAfterDeadline(t *testing.T) {
	h := newHangFixture(t)
	clock := []float64{0, 100}
	h.rt.Monotonic = func() float64 {
		now := clock[0]
		if len(clock) > 1 {
			clock = clock[1:]
		}
		return now
	}
	kids := newChildren(h.rt, 1, h.dir)
	kids.start("lint", []string{testExecutable(t), "lint"})
	results, err := kids.wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r := results["lint"]
	if r.status != "timeout" || r.err != "時間切れ（--timeout の秒数を使い切った）" || r.code != nil {
		t.Fatalf("結果 %+v", r)
	}
	if _, err := os.Stat(h.pidFile); err == nil {
		t.Fatal("締め切りの後に子を起動した")
	}
}

// 止めた後に頼まれた子は起動しない (止める間に別の子を起こさない)。
func TestChildrenDoNotStartAfterStop(t *testing.T) {
	h := newHangFixture(t)
	kids := newChildren(h.rt, 60, h.dir)
	kids.stop()
	kids.start("diff", []string{testExecutable(t), "diff"})
	results, err := kids.wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r := results["diff"]; r.status != "error" || r.err != "子プロセスを動かす前に止められました" {
		t.Fatalf("結果 %+v", r)
	}
	if _, err := os.Stat(h.pidFile); err == nil {
		t.Fatal("止めた後に子を起動した")
	}
}

// 上限を超える秒数は、待つ時間の上限なしにする (本家の _wait_seconds。time.Duration にすると溢れる)。
func TestWaitSecondsDropsHugeLimits(t *testing.T) {
	if got := waitSeconds(waitLimit + 1); got >= 0 {
		t.Fatalf("waitSeconds(上限+1) = %v", got)
	}
	if got := waitSeconds(waitLimit); got != waitLimit {
		t.Fatalf("waitSeconds(上限) = %v", got)
	}
}

// 親の中の仕事 (資料との照合) が panic しても、動いている子を止めてから panic を流し直す (本家の except BaseException の枝)。
func TestChildrenStopWhenParentWorkPanics(t *testing.T) {
	h := newHangFixture(t)
	kids := newChildren(h.rt, 60, h.dir)
	pid := 0
	recovered := func() (r any) {
		defer func() { r = recover() }()
		_, _ = kids.run(context.Background(), []childCommand{{"lint", []string{testExecutable(t), "lint"}}}, func() {
			pid = h.childPID(t)
			panic("照合の途中の panic")
		})
		return nil
	}()
	if recovered != "照合の途中の panic" {
		t.Fatalf("panic が流し直されない: %v", recovered)
	}
	if alive(pid) {
		t.Fatalf("panic の後に子 %d が止まっていない", pid)
	}
}
