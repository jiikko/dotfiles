package clipboard

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeBin は name の偽物を PATH の先頭に置く。偽物は stdin を dir/<name>.in へ書き、exit code rc で終わる
// (本物のクリップボードと本物の tmux サーバには触らない)。
func fakeBin(t *testing.T, dir, name string, rc int) string {
	t.Helper()
	out := filepath.Join(dir, name+".in")
	script := "#!/bin/sh\ncat > '" + out + "'\nexit " + string(rune('0'+rc)) + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return out
}

func osCopyCommand() string {
	if runtime.GOOS == "darwin" {
		return "pbcopy"
	}
	return "xclip"
}

func setup(t *testing.T, tmux bool) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if tmux {
		t.Setenv("TMUX", "/fake/socket,1,0")
	} else {
		t.Setenv("TMUX", "")
	}
	return dir
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%s が呼ばれていない: %v", filepath.Base(p), err)
	}
	return string(b)
}

func TestCopyWritesToOSClipboard(t *testing.T) {
	dir := setup(t, false)
	in := fakeBin(t, dir, osCopyCommand(), 0)
	tmuxIn := fakeBin(t, dir, "tmux", 0)
	if err := Copy("/a/b c"); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if got := read(t, in); got != "/a/b c" {
		t.Fatalf("クリップボードへ渡した文字列 = %q", got)
	}
	if _, err := os.Stat(tmuxIn); err == nil {
		t.Fatal("tmux の外なのに tmux を呼んだ")
	}
}

func TestCopyReturnsOSClipboardFailure(t *testing.T) {
	dir := setup(t, false)
	fakeBin(t, dir, osCopyCommand(), 1)
	var exitErr *exec.ExitError
	if err := Copy("x"); !errors.As(err, &exitErr) {
		t.Fatalf("本命の失敗が返らない: %v", err)
	}
}

// tmux の中ではおまけとして tmux バッファへも積む。おまけの失敗は本命の結果を変えない。
func TestCopyInTmuxAlsoLoadsBufferAndIgnoresItsFailure(t *testing.T) {
	dir := setup(t, true)
	in := fakeBin(t, dir, osCopyCommand(), 0)
	tmuxIn := fakeBin(t, dir, "tmux", 1)
	if err := Copy("hello"); err != nil {
		t.Fatalf("tmux の失敗が返った: %v", err)
	}
	if got := read(t, tmuxIn); got != "hello" {
		t.Fatalf("tmux へ渡した文字列 = %q", got)
	}
	if got := read(t, in); got != "hello" {
		t.Fatalf("クリップボードへ渡した文字列 = %q", got)
	}
}
