package filer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// 開いた repo の .git/config の fsmonitor・textconv を、git の印 (status) と d の diff で走らせない (issue 692)。
func TestGitDoesNotRunRepoConfigCommands(t *testing.T) {
	needGit(t)
	dir := t.TempDir()
	marker := func(name string) string { return filepath.Join(t.TempDir(), name) }
	fsmon, textconv, extdiff := marker("fsmon"), marker("textconv"), marker("extdiff")
	initRepo(t, dir, "main")
	mustWrite(t, filepath.Join(dir, "f.txt"), "a\n")
	mustWrite(t, filepath.Join(dir, ".gitattributes"), "*.txt diff=x\n")
	commitAll(t, dir)
	gitIn(t, dir, "config", "core.fsmonitor", "touch "+fsmon)
	gitIn(t, dir, "config", "diff.x.textconv", "touch "+textconv+"; cat")
	ext := filepath.Join(t.TempDir(), "extdiff.sh") // 直接起動されるので実行できるスクリプトにする (文字列のままだと起動に失敗するだけ)
	if err := os.WriteFile(ext, []byte("#!/bin/sh\ntouch "+extdiff+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "config", "diff.external", ext)
	mustWrite(t, filepath.Join(dir, "f.txt"), "b\n")
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
