package gitx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 開いた repo の .git/config の fsmonitor を、status で走らせない (issue 692。subproc.GitArgs)。
func TestRunDoesNotRunRepoFsmonitor(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無い")
	}
	dir := t.TempDir()
	fsmon := filepath.Join(t.TempDir(), "fsmon")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "core.fsmonitor", "touch " + fsmon}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, rc, err := Run(context.Background(), dir, "status", "--porcelain"); err != nil || rc != 0 {
		t.Fatalf("前提: status rc=%d err=%v", rc, err)
	}
	if _, err := os.Stat(fsmon); err == nil {
		t.Fatal("repo の fsmonitor が走った")
	}
}

// 開いた repo が partial clone を名乗り blob が消えていても、Run (RunLazyFetch でない口) は遅延 fetch から repo の core.sshCommand を
// 走らせない (issue 739)。RunLazyFetch を対照にする (罠が作動しない fixture を緑にしない。run の分岐を取り違えた退行も見える)。
func TestRunDoesNotLazyFetch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無い")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_NO_LAZY_FETCH", "")
	dir, marks := filepath.Join(tmp, "r"), filepath.Join(tmp, "marks")
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git("init", "-q")
	write("a.txt", "line1\nline2\nline3\nline4\n")
	git("add", "a.txt")
	git("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "i")
	blob := git("rev-parse", "HEAD:a.txt")
	git("mv", "a.txt", "b.txt") // 正確でない rename の検出に旧 blob の中身が要る
	write("b.txt", "line1\nline2\nline3\nX\n")
	git("add", "b.txt")
	if err := os.Remove(filepath.Join(dir, ".git", "objects", blob[:2], blob[2:])); err != nil {
		t.Fatal(err)
	}
	git("config", "core.repositoryformatversion", "1")
	git("config", "extensions.partialClone", "origin")
	git("config", "remote.origin.promisor", "true")
	git("config", "remote.origin.url", "ssh://evil.invalid/x")
	git("config", "core.sshCommand", "touch '"+marks+"'; false")

	_, _, _ = RunLazyFetch(context.Background(), dir, "status", "--porcelain") // 罠に当たって失敗するのが正しい
	if _, err := os.Stat(marks); err != nil {
		t.Fatalf("対照: RunLazyFetch の status で罠が走らない: %v", err)
	}
	if err := os.Remove(marks); err != nil {
		t.Fatal(err)
	}
	_, _, _ = Run(context.Background(), dir, "status", "--porcelain") // blob が無いので失敗する (取りに行かない)
	if _, err := os.Stat(marks); err == nil {
		t.Fatal("Run の status で repo の core.sshCommand が遅延 fetch から走った")
	}
}
