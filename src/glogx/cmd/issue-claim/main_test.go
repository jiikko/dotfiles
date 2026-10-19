package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"glogx/internal/testenv"
)

// TestMain は隔離した環境でテストを走らせる (前処理の中身と理由は internal/testenv の Main が正本)。
func TestMain(m *testing.M) { testenv.Main(m, "glogx-issue-claim", nil) }

// 制御文字を含むパスの issue は claim しない (stdout は git add に渡る実パスなので書き換えられず、端末にも出せない)。
// stderr に出すパスは無害化する。
func TestRunRefusesPathWithControlCharacters(t *testing.T) {
	dir := t.TempDir()
	name := "001-feat-a\x1b]0;evil\x07.md"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("# 001\n\n本文\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errw strings.Builder
	rc := run([]string{dir, "001"}, &out, &errw)
	if rc != 1 {
		t.Fatalf("rc = %d (1 = 拒否のはず)。stdout=%q stderr=%q", rc, out.String(), errw.String())
	}
	if out.String() != "" {
		t.Errorf("拒否したのに stdout にパスを出した: %q", out.String())
	}
	if testenv.HasControlExceptNewline(errw.String()) {
		t.Errorf("stderr に制御文字が残った: %q", errw.String())
	}
	if _, err := os.Lstat(filepath.Join(dir, "next", name)); err == nil {
		t.Error("拒否したのに目印を置いた")
	}
}

// ふつうのパスは今までどおり claim し、stdout に本文と目印のパスを出す。
func TestRunClaimsPlainPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "002-feat-b.md"), []byte("# 002\n\n本文\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errw strings.Builder
	if rc := run([]string{dir, "002"}, &out, &errw); rc != 0 {
		t.Fatalf("rc = %d stderr=%q", rc, errw.String())
	}
	if out.String() != "002-feat-b.md\nnext/002-feat-b.md\n" {
		t.Errorf("stdout = %q", out.String())
	}
}

// 無いディレクトリの名前も無害化して出す (引数の ESC が端末を操作しない)。
func TestRunSanitizesMissingDirectoryName(t *testing.T) {
	var stdout, stderr strings.Builder
	if rc := run([]string{"missing\x1b]0;evil\x07", "001"}, &stdout, &stderr); rc == 0 {
		t.Fatal("無いディレクトリで成功した")
	}
	if strings.ContainsAny(stderr.String(), "\x1b\x07") {
		t.Errorf("stderr に制御文字がそのまま出た: %q", stderr.String())
	}
}
