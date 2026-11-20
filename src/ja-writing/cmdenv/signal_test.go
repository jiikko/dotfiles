package cmdenv

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

// 届いたシグナルが取り消しの cause に入り、SignalOf で取り出せる (check はこの番号で 128+番号と文を決める)。
func TestNotifyContextKeepsTheSignalAsCause(t *testing.T) {
	ctx, stop := NotifyContext(context.Background(), syscall.SIGUSR1)
	defer stop()
	if _, ok := SignalOf(ctx); ok {
		t.Fatal("シグナルの前に SignalOf が真")
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(30 * time.Second):
		t.Fatal("シグナルで ctx が取り消されない")
	}
	if sig, ok := SignalOf(ctx); !ok || sig != syscall.SIGUSR1 {
		t.Fatalf("SignalOf = %v %v", sig, ok)
	}
}

// stop で取り消した ctx はシグナルで止められたことにしない。stop は 2 回呼んでもよい。
func TestNotifyContextStopIsNotASignal(t *testing.T) {
	ctx, stop := NotifyContext(context.Background(), syscall.SIGUSR2)
	stop()
	stop()
	if ctx.Err() == nil {
		t.Fatal("stop の後も ctx が生きている")
	}
	if _, ok := SignalOf(ctx); ok {
		t.Fatal("stop の取り消しをシグナルとみなした")
	}
}
