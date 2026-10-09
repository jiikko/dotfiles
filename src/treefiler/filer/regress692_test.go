package filer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// 開いた repo の .git/config の fsmonitor・textconv を、git の印 (status) と d の diff で走らせない (issue 692)。
func TestGitDoesNotRunRepoConfigCommands(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無い")
	}
	dir := t.TempDir()
	marker := func(name string) string { return filepath.Join(t.TempDir(), name) }
	fsmon, textconv, extdiff := marker("fsmon"), marker("textconv"), marker("extdiff")
	gitIn(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("*.txt diff=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, dir)
	gitIn(t, dir, "config", "core.fsmonitor", "touch "+fsmon)
	gitIn(t, dir, "config", "diff.x.textconv", "touch "+textconv+"; cat")
	ext := filepath.Join(t.TempDir(), "extdiff.sh") // 直接起動されるので実行できるスクリプトにする (文字列のままだと起動に失敗するだけ)
	if err := os.WriteFile(ext, []byte("#!/bin/sh\ntouch "+extdiff+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "config", "diff.external", ext)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := fetchRepo(dir); !ok {
		t.Fatal("前提: status を取れない")
	}
	fetchDiff(context.Background(), filepath.Join(dir, "f.txt"))
	for _, p := range []string{fsmon, textconv, extdiff} {
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("repo の設定のコマンドが走った: %s", filepath.Base(p))
		}
	}
}
