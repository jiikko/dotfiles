//go:build darwin

package check

import (
	"errors"
	"io/fs"
	"os"
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

// 一時フォルダの中に BSD のフラグ (uchg) が付いたファイルとフォルダがあっても、フラグを外して消す (本家の _remove_temporary の chflags(0))。
func TestRemoveTemporaryClearsFlags(t *testing.T) {
	dir := t.TempDir() + "/yomiyasu-check-x"
	if err := os.MkdirAll(dir+"/sub", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/sub/copy.bin", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { // 消せなかったときに t.TempDir の後片付けが落ちないよう、フラグを外しておく
		_ = filepath.WalkDir(dir, func(p string, _ fs.DirEntry, _ error) error { _ = syscall.Chflags(p, 0); return nil })
	})
	for _, p := range []string{dir + "/sub/copy.bin", dir + "/sub"} {
		if err := syscall.Chflags(p, 0x2); err != nil { // UF_IMMUTABLE (uchg)
			t.Fatal(err)
		}
	}
	var warned []string
	removeTemporary(dir, func(m string) { warned = append(warned, m) })
	if _, err := os.Lstat(dir); !errors.Is(err, fs.ErrNotExist) || len(warned) != 0 {
		t.Fatalf("消えていない: %v 知らせ=%q", err, warned)
	}
}

// 一時フォルダそのものを、親が書けない (0o555)・親に uchg が付いているので消せないときは、本家の again と同じく親の権限とフラグも
// 戻して消す (本家は Python 3.15 で同じ 2 つを消し、親を 0o700 にする)。親のほかの中身には触らない。
func TestRemoveTemporaryRestoresParentOfRoot(t *testing.T) {
	for name, lock := range map[string]func(string) error{
		"read-only": func(p string) error { return os.Chmod(p, 0o555) },
		"uchg":      func(p string) error { return syscall.Chflags(p, 0x2) },
	} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir() + "/parent"
			dir := parent + "/yomiyasu-check-x"
			if err := os.MkdirAll(dir+"/sub", 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dir+"/sub/copy.bin", []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = syscall.Chflags(parent, 0); _ = os.Chmod(parent, 0o700) })
			if err := lock(parent); err != nil {
				t.Fatal(err)
			}
			var warned []string
			removeTemporary(dir, func(m string) { warned = append(warned, m) })
			if _, err := os.Lstat(dir); !errors.Is(err, fs.ErrNotExist) || len(warned) != 0 {
				t.Fatalf("消えていない: %v 知らせ=%q", err, warned)
			}
			info, err := os.Stat(parent)
			if err != nil || info.Mode().Perm() != 0o700 {
				t.Fatalf("親の権限 %v %v (本家と同じ 0o700 を期待)", info, err)
			}
		})
	}
}
