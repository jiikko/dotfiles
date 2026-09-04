package docker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLookPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "docker")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	notExec := filepath.Join(dir, "notexec")
	if err := os.WriteFile(notExec, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if got, err := lookPath("docker"); err != nil || got != bin {
		t.Fatalf("lookPath = %q, %v", got, err)
	}
	// 実行ビットが無いものは「在る」にしない (exec.LookPath と同じ意味論)
	if _, err := lookPath("notexec"); err == nil {
		t.Fatalf("実行できないファイルを見つけたことにした")
	}
	if _, err := lookPath("nosuchcommand"); err == nil {
		t.Fatalf("無いものを見つけたことにした")
	}
	// ディレクトリを含む名前は PATH を見ない
	if got, err := lookPath(bin); err != nil || got != bin {
		t.Fatalf("絶対パス: %q, %v", got, err)
	}
	if _, err := lookPath(filepath.Join(dir, "nope")); err == nil {
		t.Fatalf("無い絶対パスを見つけたことにした")
	}
}
