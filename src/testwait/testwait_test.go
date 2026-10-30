package testwait

import (
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 成り立つまで step を回し、成り立ったら戻る。
func TestUntilStepsUntilCond(t *testing.T) {
	n := 0
	Until(t, "3 回", func() bool { return n >= 3 }, Within(time.Second), Step(func() { n++ }))
	if n != 3 {
		t.Errorf("step を %d 回回した, want 3", n)
	}
}

// 条件が最初から成り立っているなら step を回さない (step の Advance は状態を進める)。
func TestUntilChecksCondBeforeStep(t *testing.T) {
	n := 0
	Until(t, "最初から成り立つ", func() bool { return true }, Step(func() { n++ }))
	if n != 0 {
		t.Errorf("成り立っている条件の前に step を %d 回回した", n)
	}
}

// 上限を超えたら黙って戻らず、what・呼び出し側の診断・子孫のプロセス・goroutine を出して落とす。
func TestUntilFailsAtLimitWithDiagnostics(t *testing.T) {
	child := exec.Command("/bin/sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	msg := fatalOf(t, func(ft testing.TB) {
		Until(ft, "成り立たない条件", func() bool { return false }, Within(20*time.Millisecond), Diag(func() string { return "段: 送った後" }))
	})
	for _, want := range []string{"成り立たない条件 が 20ms 待っても成り立たない", "段: 送った後", strconv.Itoa(child.Process.Pid), "/bin/sleep 30", "goroutine "} {
		if !strings.Contains(msg, want) {
			t.Errorf("時間切れの文に %q が無い:\n%s", want, msg)
		}
	}
}

func TestPollReportsWithoutFailing(t *testing.T) {
	if Poll(20*time.Millisecond, func() bool { return false }) {
		t.Error("成り立たない条件で true")
	}
	n := 0
	if !Poll(time.Second, func() bool { n++; return n >= 2 }, Tick(time.Millisecond)) {
		t.Error("成り立つ条件で false")
	}
}

// fatalOf は f を別の goroutine で走らせ、Fatalf の文を返す (本物の Fatalf はこのテストごと落とすので差し替える)。
func fatalOf(t *testing.T, f func(testing.TB)) string {
	t.Helper()
	ft := &fatalT{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		f(ft)
	}()
	<-done
	if ft.msg == "" {
		t.Fatal("上限を超えても Fatal しない")
	}
	return ft.msg
}

type fatalT struct {
	testing.TB
	msg string
}

func (f *fatalT) Helper() {}

func (f *fatalT) Fatalf(format string, args ...any) {
	f.msg = format
	if len(args) > 0 {
		f.msg = args[0].(string)
	}
	runtime.Goexit()
}
