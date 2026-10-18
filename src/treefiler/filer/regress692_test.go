package filer

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"subproc"
)

// 開いた repo の .git/config の fsmonitor・textconv を、git の印 (status) と d の diff で走らせない。
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

// 開いた repo の .git/config の filter (clean / process) を、git の印 (status) と d の diff (作業ツリーとの差分) で走らせない。
// attributes は `.git/info/attributes` に置く (作業ツリーの .gitattributes を読まない対策では止まらない形)。
func TestGitDoesNotRunRepoFilters(t *testing.T) {
	needGit(t)
	for _, kind := range []string{"clean", "process"} {
		t.Run(kind, func(t *testing.T) {
			dir, marks := t.TempDir(), filepath.Join(t.TempDir(), "marks")
			initRepo(t, dir, "main")
			mustWrite(t, filepath.Join(dir, "f.txt"), "a\n")
			commitAll(t, dir)
			trap := filepath.Join(t.TempDir(), "trap.sh")
			if err := os.WriteFile(trap, []byte("#!/bin/sh\ntouch "+marks+"\ncat\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			gitIn(t, dir, "config", "filter.x."+kind, trap)
			mustWrite(t, filepath.Join(dir, ".git", "info", "attributes"), "* filter=x\n")
			mustWrite(t, filepath.Join(dir, "f.txt"), "b\n")
			// index と別の秒の過去に打つ: stat が食い違うので、status / diff は毎回 filter を通して内容を比べる
			old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			if err := os.Chtimes(filepath.Join(dir, "f.txt"), old, old); err != nil {
				t.Fatal(err)
			}
			if out, err := subproc.GitCommand(context.Background(), "-C", dir, "status", "--porcelain").CombinedOutput(); err != nil {
				t.Fatalf("対照の status: %v\n%s", err, out)
			}
			if _, err := os.Stat(marks); err != nil {
				t.Fatalf("対照: filter を止めない status で罠が走らない: %v", err)
			}
			if err := os.Remove(marks); err != nil {
				t.Fatal(err)
			}
			snap, ok := fetchRepo(dir)
			if !ok {
				t.Fatal("status を取れない")
			}
			if snap.files["f.txt"] == 0 {
				t.Fatalf("f.txt が変更として出ない: %v", snap.files)
			}
			lines := fetchDiff(context.Background(), filepath.Join(dir, "f.txt"))
			if !slices.ContainsFunc(lines, func(l string) bool { return strings.Contains(l, "+b") }) {
				t.Fatalf("diff が取れていない (git を起こさずに終わっても緑にしない): %q", lines)
			}
			if _, err := os.Stat(marks); err == nil {
				t.Fatal("repo の filter が走った")
			}
		})
	}
}

// submodule の中の設定 (.git/modules/<名前>/config) の filter を、status と diff で走らせない。
// 子の git は submodule 自身の設定を読むので、親の設定の列挙と -c の上書きは届かない。--ignore-submodules=dirty で子を起こさない
func TestGitDoesNotRunSubmoduleFilters(t *testing.T) {
	needGit(t)
	src, dir, marks := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "marks")
	initRepo(t, src, "main")
	mustWrite(t, filepath.Join(src, "s.txt"), "a\n")
	commitAll(t, src)
	initRepo(t, dir, "main")
	mustWrite(t, filepath.Join(dir, "f.txt"), "a\n")
	commitAll(t, dir)
	gitIn(t, dir, "-c", "protocol.file.allow=always", "submodule", "add", "-q", src, "sub")
	commitAll(t, dir)
	trap := filepath.Join(t.TempDir(), "trap.sh")
	if err := os.WriteFile(trap, []byte("#!/bin/sh\ntouch "+marks+"\ncat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, filepath.Join(dir, "sub"), "config", "filter.evil.clean", trap) // .git/modules/sub/config に入る
	mustWrite(t, filepath.Join(dir, "sub", ".gitattributes"), "* filter=evil\n")
	mustWrite(t, filepath.Join(dir, "sub", "s.txt"), "b\n")
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(dir, "sub", "s.txt"), old, old); err != nil {
		t.Fatal(err)
	}
	if out, err := subproc.GitCommand(context.Background(), "-C", dir, "status", "--porcelain").CombinedOutput(); err != nil {
		t.Fatalf("対照の status: %v\n%s", err, out)
	}
	if _, err := os.Stat(marks); err != nil {
		t.Fatalf("対照: submodule の中を見る status で罠が走らない: %v", err)
	}
	if err := os.Remove(marks); err != nil {
		t.Fatal(err)
	}
	if _, ok := fetchRepo(dir); !ok {
		t.Fatal("status を取れない")
	}
	if _, err := gitDiffCommand(context.Background(), dir, "sub", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marks); err == nil {
		t.Fatal("submodule の filter が走った")
	}
}
