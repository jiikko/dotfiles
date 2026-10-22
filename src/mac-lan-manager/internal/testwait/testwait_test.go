package testwait

import (
	"runtime"
	"testing"
	"time"
)

// 成り立つまで step を回し、成り立ったら戻る。
func TestUntilStepsUntilCond(t *testing.T) {
	n := 0
	Until(t, "3 回", time.Second, func() { n++ }, func() bool { return n >= 3 })
	if n != 3 {
		t.Errorf("step を %d 回回した, want 3", n)
	}
}

// 上限を超えたら黙って戻らず Fatal する。
func TestUntilFailsAtLimit(t *testing.T) {
	ft := &fatalT{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		Until(ft, "成り立たない条件", 20*time.Millisecond, nil, func() bool { return false })
	}()
	<-done
	if ft.msg == "" {
		t.Fatal("上限を超えても Fatal しない")
	}
}

// fatalT は Fatalf の文を控えて goroutine を抜ける TB (本物の Fatalf はこのテストごと落とすので差し替える)。
type fatalT struct {
	testing.TB
	msg string
}

func (f *fatalT) Helper() {}

func (f *fatalT) Fatalf(format string, _ ...any) {
	f.msg = format
	runtime.Goexit()
}
