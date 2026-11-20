//go:build darwin

package check

import (
	"bytes"
	"ja-writing/internal/pyos"
	"syscall"
	"unsafe"
)

// fgetpath は macOS の値 50 (F_GETPATH)。
const fgetpath = 50

// diskSpelling は path を開いて F_GETPATH で取った、ディスク上の綴り (開けないか取れなければ空)。読むだけで中身は読まない。
// O_NONBLOCK で開く: パイプなどでも止まらないように (本家と同じ)。
func diskSpelling(path string) string {
	fd, err := pyos.OpenFD(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return ""
	}
	defer func() { _ = syscall.Close(fd) }()
	buf := make([]byte, 1024)
	if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), fgetpath, uintptr(unsafe.Pointer(&buf[0]))); errno != 0 {
		return ""
	}
	name, _, _ := bytes.Cut(buf, []byte{0})
	return pyos.Decode(string(name))
}

// isDarwin は _disk_path が F_GETPATH の綴りを使う OS か。
const isDarwin = true

// clearFlags は os.chflags(path, 0) (BSD のフラグ。uchg などが付いたファイルとフォルダは、権限を戻しても消せない)。外せないときは何もしない。
func clearFlags(path string) { _ = pyos.Chflags(path, 0) }
