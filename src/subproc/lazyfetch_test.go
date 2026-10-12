package subproc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// partial clone を名乗る repo で消えた blob を読ませても、遅延 fetch から repo の core.sshCommand / remote.<名前>.uploadpack を
// 走らせない (issue 739)。対照として、素の git では同じ罠が走ることを見る (罠が作動しない fixture を緑にしない)。
func TestGitDoesNotLazyFetchFromRepoConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無い")
	}
	for _, via := range []string{"sshCommand", "uploadpack"} {
		t.Run(via, func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("HOME", tmp)
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
			t.Setenv("GIT_CONFIG_PARAMETERS", "")
			t.Setenv("GIT_NO_LAZY_FETCH", "") // 親の環境から持ち込まない (持ち込むと対照も罠を踏まない)
			repo, marks := filepath.Join(tmp, "r"), filepath.Join(tmp, "marks")
			git := func(args ...string) string {
				t.Helper()
				out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			if err := os.MkdirAll(repo, 0o755); err != nil {
				t.Fatal(err)
			}
			git("init", "-q")
			writeFile(t, filepath.Join(repo, "a.txt"), "line1\nline2\nline3\nline4\n")
			git("add", "a.txt")
			git("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "i")
			blob := git("rev-parse", "HEAD:a.txt")
			// 正確でない rename: 検出に旧 blob の中身が要る (status も diff HEAD も読みに行く)
			git("mv", "a.txt", "b.txt")
			writeFile(t, filepath.Join(repo, "b.txt"), "line1\nline2\nline3\nX\n")
			git("add", "b.txt")
			if err := os.Remove(filepath.Join(repo, ".git", "objects", blob[:2], blob[2:])); err != nil {
				t.Fatal(err)
			}
			git("config", "core.repositoryformatversion", "1")
			git("config", "extensions.partialClone", "origin")
			git("config", "remote.origin.promisor", "true")
			trap := "touch '" + marks + "'; false"
			if via == "sshCommand" {
				git("config", "remote.origin.url", "ssh://evil.invalid/x")
				git("config", "core.sshCommand", trap)
			} else {
				up := filepath.Join(tmp, "up")
				if err := os.MkdirAll(up, 0o755); err != nil {
					t.Fatal(err)
				}
				git("config", "remote.origin.url", up)
				git("config", "remote.origin.uploadpack", trap)
			}

			_ = exec.Command("git", "-C", repo, "status", "--porcelain").Run() // 罠に当たって失敗するのが正しい
			if _, err := os.Stat(marks); err != nil {
				t.Fatalf("対照: 素の git status で罠が走らない: %v", err)
			}
			if err := os.Remove(marks); err != nil {
				t.Fatal(err)
			}

			ctx := context.Background()
			viaRepo, err := GitRepoCommand(ctx, repo, "status", "--porcelain")
			if err != nil {
				t.Fatal(err)
			}
			for _, cmd := range []*exec.Cmd{
				GitCommand(ctx, "-C", repo, "status", "--porcelain"),
				GitCommand(ctx, "-C", repo, "diff", "--no-ext-diff", "--no-textconv", "HEAD"),
				viaRepo,
			} {
				_ = cmd.Run() // blob が無いので失敗する (取りに行かない)
				if _, err := os.Stat(marks); err == nil {
					t.Fatalf("repo の設定のコマンドが遅延 fetch から走った: %q", cmd.Args)
				}
			}
		})
	}
}
