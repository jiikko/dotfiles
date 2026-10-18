package gitdata

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// main のテスト (browseModel と組み合わせるもの) と同じヘルパーの、中のテスト用の写し (両方のテストが使っていたもの)。

// newTempRepo はコミット付きの一時リポジトリを作り、そのディレクトリへ chdir する。
func newTempRepo(t *testing.T, subjects []string) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	gitInRepo(t, dir, "init", "-q", "-b", "main")
	for i, subject := range subjects {
		commitLines(t, dir, i+1, subject)
	}
	return dir
}

// gitInRepo は dir を作業ディレクトリとして git を走らせる (author / committer を固定)。
func gitInRepo(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=tester", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=tester", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// commitLines は file.txt を lines 行にして subject でコミットする (コミットを 1 本足す)。
func commitLines(t *testing.T, dir string, lines int, subject string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte(strings.Repeat("line\n", lines)), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInRepo(t, dir, "add", ".")
	gitInRepo(t, dir, "commit", "-q", "-m", subject)
}

// headSHA は現在の HEAD の完全 SHA を返すテストヘルパー。
func headSHA(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// fakeRunner は fixture を返す CommandRunner。通常テストで外部通信しない (issue のテスト方針)。
func fakeRunner(stdout, stderr string, err error) CommandRunner {
	return func(_ context.Context, _ string, _ ...string) ([]byte, []byte, error) {
		return []byte(stdout), []byte(stderr), err
	}
}
