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

// 届いた値を返す (双方向のチャネルをそのまま渡せる)。
func TestRecvReturnsValue(t *testing.T) {
	ch := make(chan int, 1)
	ch <- 7
	if got := Recv(t, "値", ch); got != 7 {
		t.Errorf("Recv = %d, want 7", got)
	}
}

// 届かなければ黙って戻らず、what と診断を出して落とす。
func TestRecvFailsAtLimitWithDiagnostics(t *testing.T) {
	ch := make(chan struct{})
	msg := fatalOf(t, func(ft testing.TB) {
		Recv(ft, "終わりの合図", ch, Within(20*time.Millisecond), Diag(func() string { return "段: 起動した後" }))
	})
	for _, want := range []string{"終わりの合図 が 20ms 待っても届かない", "段: 起動した後", "goroutine "} {
		if !strings.Contains(msg, want) {
			t.Errorf("時間切れの文に %q が無い:\n%s", want, msg)
		}
	}
}

// 閉じたチャネルからは ok=false で返る (閉じるのを待つ使い方)。
func TestRecvOKReportsClosed(t *testing.T) {
	ch := make(chan int)
	close(ch)
	if _, ok := RecvOK(t, "閉じる", ch); ok {
		t.Error("閉じたチャネルで ok=true")
	}
}

// deadlineT は Deadline だけを差し替えた T (go test -timeout の値に依らずに deadline を決める)。
type deadlineT struct {
	*testing.T
	dl time.Time
	ok bool
}

func (d deadlineT) Deadline() (time.Time, bool) { return d.dl, d.ok }

// 子を止める時刻は、テストの deadline から猶予 (残りの 1/20、最低 1 秒) を引いた時刻。deadline ちょうどにはしない。
func TestChildContextStopsBeforeTestDeadline(t *testing.T) {
	dl := time.Now().Add(40 * time.Second)
	got, ok := ChildContext(deadlineT{T: t, dl: dl, ok: true}).Deadline()
	if !ok {
		t.Fatal("deadline のあるテストで、ChildContext に deadline が無い")
	}
	if lead := dl.Sub(got); lead < 1900*time.Millisecond || lead > 2100*time.Millisecond {
		t.Fatalf("テストの deadline より %v 手前で止める。期待は 2 秒 (40 秒の 1/20)", lead)
	}

	short := time.Now().Add(5 * time.Second)
	got, _ = ChildContext(deadlineT{T: t, dl: short, ok: true}).Deadline()
	if lead := short.Sub(got); lead < 950*time.Millisecond || lead > 1050*time.Millisecond {
		t.Fatalf("残りが短いときの猶予が %v。期待は最低の 1 秒", lead)
	}
}

// deadline の無いテストでは、テストの context をそのまま返す (勝手な上限を付けない)。
func TestChildContextWithoutDeadline(t *testing.T) {
	if _, ok := ChildContext(deadlineT{T: t, ok: false}).Deadline(); ok {
		t.Fatal("deadline の無いテストで ChildContext に deadline が付いた")
	}
}
