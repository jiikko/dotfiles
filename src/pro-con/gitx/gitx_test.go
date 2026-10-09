package gitx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
