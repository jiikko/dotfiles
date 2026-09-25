package store

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// ロックのファイルの pid が居なければ「dispatcher が動いていない」と言える。居る・ファイルが無い・読めないなら言えない (483)。
func TestDispatcherGone(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(dir, DispatcherLockFile), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if DispatcherGone(dir) {
		t.Fatal("ロックのファイルが無いのに居ないと言った")
	}
	write(strconv.Itoa(os.Getpid()) + "\n")
	if DispatcherGone(dir) {
		t.Fatal("居るプロセスを居ないと言った")
	}
	cmd := exec.Command("true") // 終わったプロセスの pid (待ち終えたので居ない)
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	write(strconv.Itoa(cmd.Process.Pid) + "\n")
	if !DispatcherGone(dir) {
		t.Fatal("終わったプロセスの pid を居ないと言わない")
	}
	write("abc\n")
	if DispatcherGone(dir) {
		t.Fatal("読めない pid を居ないと言った")
	}
}
