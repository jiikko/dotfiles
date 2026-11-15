//go:build darwin

package check

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// 書き手のいない FIFO を O_NONBLOCK なしで開くと open が戻らない。待ち時間の長さは合否の基準でなく、止まったままのときに suite ごと止めないための guard。
func TestDiskPathDoesNotBlockOnFifoWithoutWriter(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "Pipe.fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	type result struct {
		path string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		path, err := DiskPath(fifo)
		done <- result{path, err}
	}()
	select {
	case got := <-done:
		if got.err != nil || filepath.Base(got.path) != "pipe.fifo" {
			t.Fatalf("DiskPath(FIFO) = %q, %v。末尾は casefold した pipe.fifo のはず", got.path, got.err)
		}
	case <-time.After(60 * time.Second):
		// 止まった open を、書き手を開いて解く (待つ goroutine を残さない)
		if fd, err := syscall.Open(fifo, syscall.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = syscall.Close(fd)
		}
		t.Fatal("DiskPath が書き手のいない FIFO の open で止まった (O_NONBLOCK が要る)")
	}
}
