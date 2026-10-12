package monitor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 本物の partial clone (clone --filter=blob:none) でも MergeTree が衝突を衝突として返す (エラーにしない)。相手 (origin/master) が変えた
// ファイルの blob は手元に無く、merge-tree がそれを取りに行く (git に --no-lazy-fetch を付けたままだと rc=128 で落ちる。issue 739)。
func TestMergeTreeInPartialClone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無い")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, part := filepath.Join(tmp, "srv"), filepath.Join(tmp, "part")
	git(tmp, "init", "-q", "-b", "master", srv)
	write(filepath.Join(srv, "f"), "base\n")
	git(srv, "add", "f")
	git(srv, "commit", "-qm", "i")
	git(srv, "config", "uploadpack.allowFilter", "true")
	git(tmp, "clone", "-q", "--filter=blob:none", "file://"+srv, part)
	write(filepath.Join(srv, "f"), "theirs\n")
	git(srv, "commit", "-qam", "theirs")
	write(filepath.Join(part, "f"), "ours\n")
	git(part, "commit", "-qam", "ours")
	git(part, "fetch", "-q")

	conflict, files, err := ExecGit{}.MergeTree(context.Background(), part, "HEAD", "origin/master")
	if err != nil {
		t.Fatalf("partial clone で merge-tree がエラーになった: %v", err)
	}
	if !conflict || len(files) != 1 || files[0] != "f" {
		t.Fatalf("衝突を返さない: conflict=%v files=%q", conflict, files)
	}
}
