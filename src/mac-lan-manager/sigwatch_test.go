package main

import (
	"io"
	"os"
	"syscall"
	"testing"
	"time"
)

// fakeExec は ssh に端末を渡している間の代わり。Run の中で during を呼ぶ。
type fakeExec struct{ during func() }

func (f fakeExec) Run() error        { f.during(); return nil }
func (fakeExec) SetStdin(io.Reader)  {}
func (fakeExec) SetStdout(io.Writer) {}
func (fakeExec) SetStderr(io.Writer) {}

// 期待の表 (sigWatch の注記) の行ごと: ssh に端末を渡している間 (Guard した Run の中) の合図は捨てる / 画面の中・戻った後の合図で閉じる
func TestSigWatchIgnoresSignalsWhileExec(t *testing.T) {
	closed := func(w *sigWatch) bool {
		select {
		case <-w.Done():
			return true
		case <-time.After(200 * time.Millisecond): // 合図を読む goroutine が回るまでの上限 (閉じないことの確認には待つしかない)
			return false
		}
	}

	sigs := make(chan os.Signal, 1)
	w := newSigWatch(sigs)
	var during bool
	if err := w.Guard(fakeExec{during: func() {
		sigs <- os.Interrupt // ssh への Ctrl-C (同じプロセスグループなのでこちらにも届く)
		sigs <- syscall.SIGTERM
		during = closed(w)
	}}).Run(); err != nil {
		t.Fatal(err)
	}
	if during {
		t.Fatal("ssh に端末を渡している間の合図で「止める」が立った (戻った後に q で終えても待たない)")
	}
	sigs <- syscall.SIGHUP
	if !closed(w) {
		t.Fatal("ssh から戻った後の合図で「止める」が立たない (Guard が Run の出口で下ろしていない)")
	}
	sigs <- os.Interrupt // 2 度目で close を重ねて panic しない
	sigs <- os.Interrupt

	sigs2 := make(chan os.Signal, 1)
	w2 := newSigWatch(sigs2)
	sigs2 <- syscall.SIGTERM
	if !closed(w2) {
		t.Fatal("画面の中の合図で「止める」が立たない (裏の ssh を最大 20 秒待つ)")
	}
}
