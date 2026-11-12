package ctxwait

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestSleepReturnsCtxErrWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("取り消した ctx で %v (context.Canceled のはず)", err)
	}
	if err := Sleep(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Errorf("取り消した ctx で 0 の待ちが %v (context.Canceled のはず)", err)
	}
	if err := Sleep(context.Background(), -time.Second); err != nil {
		t.Errorf("0 以下の待ちが %v (待たずに nil のはず)", err)
	}
	if err := Sleep(context.Background(), time.Millisecond); err != nil {
		t.Errorf("取り消していない待ちが %v", err)
	}
}

func openTwice(t *testing.T) (holder, waiter *os.File) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "lock")
	var err error
	if holder, err = os.OpenFile(p, os.O_RDWR|os.O_CREATE, 0o600); err != nil {
		t.Fatal(err)
	}
	if waiter, err = os.OpenFile(p, os.O_RDWR, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Close(); _ = waiter.Close() })
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	return holder, waiter
}

// 他が持っている間は wait で待ち、放されたら取る。
func TestFlockWaitsUntilReleased(t *testing.T) {
	holder, waiter := openTwice(t)
	waits := 0
	err := Flock(int(waiter.Fd()), syscall.LOCK_EX, 7*time.Millisecond, func(d time.Duration) error {
		if d != 7*time.Millisecond {
			t.Errorf("待ちの間が %v", d)
		}
		waits++
		if waits == 3 {
			_ = syscall.Flock(int(holder.Fd()), syscall.LOCK_UN)
		}
		return nil
	})
	if err != nil || waits != 3 {
		t.Fatalf("err=%v waits=%d (放した後の 1 回で取るはず)", err, waits)
	}
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); !errors.Is(err, syscall.EWOULDBLOCK) {
		t.Errorf("取ったはずの flock を別のファイルが取れた: %v", err)
	}
}

// wait の誤り (取り消し) はそのまま返し、flock は取らない。
func TestFlockReturnsWaitError(t *testing.T) {
	_, waiter := openTwice(t)
	stop := errors.New("やめた")
	if err := Flock(int(waiter.Fd()), syscall.LOCK_EX, time.Millisecond, func(time.Duration) error { return stop }); !errors.Is(err, stop) {
		t.Fatalf("err=%v (wait の誤りのはず)", err)
	}
}

// EWOULDBLOCK 以外の失敗は待たずに syscall.Errno で返す。
func TestFlockReturnsOtherErrorsWithoutWaiting(t *testing.T) {
	err := Flock(-1, syscall.LOCK_EX, time.Millisecond, func(time.Duration) error {
		t.Error("取れない理由が他のプロセスでないのに待った")
		return errors.New("待った") // 抜けさせる (待ち続ける退行でテストが止まらないように)
	})
	var errno syscall.Errno
	if !errors.As(err, &errno) || errno != syscall.EBADF {
		t.Fatalf("err=%v (EBADF のはず)", err)
	}
}

// how を渡したとおりに取る: 共有ロック (LOCK_SH) どうしは待たない。
func TestFlockSharedDoesNotWaitForShared(t *testing.T) {
	p := filepath.Join(t.TempDir(), "lock")
	a, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.OpenFile(p, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	if err := syscall.Flock(int(a.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err := Flock(int(b.Fd()), syscall.LOCK_SH, time.Millisecond, func(time.Duration) error { return errors.New("待った") }); err != nil {
		t.Fatalf("共有ロックどうしで待った: %v", err)
	}
}
