package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLogPlain(t *testing.T) {
	out := "\x1e" + strings.Join([]string{
		strings.Repeat("a", 40), "aaaaaaa", "Fix invoice calculation", "koji", "2 hours ago", "HEAD -> master, origin/master",
	}, "\x1f") + "\x1f" +
		"\x1e" + strings.Join([]string{
		strings.Repeat("b", 40), "bbbbbbb", "Update README", "koji", "1 day ago", "",
	}, "\x1f") + "\x1f"
	commits, err := ParseLog(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("len = %d; want 2", len(commits))
	}
	c := commits[0]
	if c.ShortSHA != "aaaaaaa" || c.Subject != "Fix invoice calculation" || c.Author != "koji" ||
		c.RelDate != "2 hours ago" || c.Decoration != "HEAD -> master, origin/master" || c.Body != "" {
		t.Errorf("commits[0] = %+v", c)
	}
	if commits[1].Decoration != "" {
		t.Errorf("commits[1].Decoration = %q; want empty", commits[1].Decoration)
	}
}

func TestParseLogWithBody(t *testing.T) {
	// --stat / -p の本文は最後のフィールドセパレータ以降 (issue のテスト方針: 本文を壊さない)
	body := "\n file.go | 12 ++++--\n 1 file changed\n"
	out := "\x1e" + strings.Join([]string{
		strings.Repeat("a", 40), "aaaaaaa", "subject", "koji", "now", "",
	}, "\x1f") + "\x1f" + body
	commits, err := ParseLog(out)
	if err != nil {
		t.Fatal(err)
	}
	want := " file.go | 12 ++++--\n 1 file changed"
	if commits[0].Body != want {
		t.Errorf("Body = %q; want %q", commits[0].Body, want)
	}
}

func TestParseLogEmpty(t *testing.T) {
	commits, err := ParseLog("")
	if err != nil || commits != nil {
		t.Errorf("ParseLog(\"\") = %v, %v", commits, err)
	}
}

func TestParseLogMalformed(t *testing.T) {
	if _, err := ParseLog("\x1eonly-one-field"); err == nil {
		t.Errorf("フィールド不足の出力がエラーにならない")
	}
}

func TestBuildLogArgs(t *testing.T) {
	opts := &Options{MaxCount: 5, Stat: true, Revs: []string{"main"}, Paths: []string{"src/"}}
	args := BuildLogArgs(opts, false)
	joined := strings.Join(args, " ")
	for _, want := range []string{"--max-count=5", "--stat", "--color=never", "main", "-- src/"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args に %q がありません: %v", want, args)
		}
	}
	if strings.Contains(joined, "--patch") {
		t.Errorf("指定していない --patch が入っている: %v", args)
	}
}

// ---- integration: 一時 Git リポジトリで実際の git log を解析する ----

// newTempRepo はコミット付きの一時リポジトリを作り、そのディレクトリへ chdir する。
func newTempRepo(t *testing.T, subjects []string) string {
	t.Helper()
	dir := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=tester", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=tester", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	for i, subject := range subjects {
		file := filepath.Join(dir, "file.txt")
		if err := os.WriteFile(file, []byte(strings.Repeat("line\n", i+1)), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", ".")
		git("commit", "-q", "-m", subject)
	}
	return dir
}

func TestIntegrationLoadCommitsOrderAndCount(t *testing.T) {
	newTempRepo(t, []string{"first", "second", "third"})
	commits, err := LoadCommits(&Options{MaxCount: defaultMaxCount}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 3 {
		t.Fatalf("len = %d; want 3", len(commits))
	}
	// git log は新しい順
	if commits[0].Subject != "third" || commits[2].Subject != "first" {
		t.Errorf("順序が git log と一致しない: %v, %v", commits[0].Subject, commits[2].Subject)
	}
	one, err := LoadCommits(&Options{MaxCount: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 || one[0].Subject != "third" {
		t.Errorf("-n1 相当で %d 件 (%v)", len(one), one)
	}
}

func TestIntegrationStatBodyIntact(t *testing.T) {
	newTempRepo(t, []string{"first", "second"})
	commits, err := LoadCommits(&Options{MaxCount: defaultMaxCount, Stat: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range commits {
		if !strings.Contains(c.Body, "file.txt") || !strings.Contains(c.Body, "file changed") {
			t.Errorf("--stat 本文が壊れている (%s): %q", c.Subject, c.Body)
		}
	}
}

func TestIntegrationPatchBodyIntact(t *testing.T) {
	newTempRepo(t, []string{"first"})
	commits, err := LoadCommits(&Options{MaxCount: defaultMaxCount, Patch: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	body := commits[0].Body
	for _, want := range []string{"diff --git", "+line"} {
		if !strings.Contains(body, want) {
			t.Errorf("-p の patch 本文に %q がありません: %q", want, body)
		}
	}
}

func TestIntegrationStagedDiff(t *testing.T) {
	dir := newTempRepo(t, []string{"first"})
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	diff, err := LoadStagedDiff(&Options{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "staged.txt") {
		t.Errorf("staged diff に staged.txt がありません: %q", diff)
	}
	head, err := LoadHeadCommit()
	if err != nil {
		t.Fatal(err)
	}
	if head.Subject != "first" {
		t.Errorf("HEAD subject = %q", head.Subject)
	}
}

func TestIntegrationOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CEILING_DIRECTORIES", dir)
	_, err = LoadCommits(&Options{MaxCount: 1}, false)
	if err == nil {
		t.Fatal("リポジトリ外でエラーになっていない")
	}
	var gitErr *GitExitError
	if !errors.As(err, &gitErr) {
		t.Fatalf("GitExitError ではない: %T %v", err, err)
	}
	if gitErr.Code == 0 || gitErr.Stderr == "" {
		t.Errorf("git の終了コード/stderr が伝播していない: %+v", gitErr)
	}
}
