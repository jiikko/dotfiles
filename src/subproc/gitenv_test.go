package subproc

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// UnsetGitLocationEnv は repo の場所・設定を変える環境変数を外し、関係の無い GIT_* は残す。外した後の `git -C <dir> init` は
// 継承した GIT_DIR の repo ではなく dir に repo を作る (issue 740)。
func TestUnsetGitLocationEnv(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無い")
	}
	victim, dir := t.TempDir(), t.TempDir()
	if out, err := exec.Command("git", "-C", victim, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0"} {
		t.Setenv(k, "x")
	}
	t.Setenv("GIT_DIR", filepath.Join(victim, ".git"))
	t.Setenv("GIT_AUTHOR_NAME", "keep")
	if err := UnsetGitLocationEnv(); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0"} {
		if v, ok := os.LookupEnv(k); ok {
			t.Errorf("%s が残っている: %q", k, v)
		}
	}
	if os.Getenv("GIT_AUTHOR_NAME") != "keep" {
		t.Error("関係の無い GIT_AUTHOR_NAME まで外した")
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatalf("外した後の git -C dir init が dir に repo を作らない (継承した GIT_DIR を向いた): %v", err)
	}
}

// 一覧が、手元の git が「repo に固有」と言う環境変数 (`git rev-parse --local-env-vars`) を全部覆う。git の版が上がって増えたら落ちる
// (GIT_GRAFT_FILE・GIT_SHALLOW_FILE などが抜けていて、継承するとテストが別の repo の履歴を読んだ。issue 740 の敵対的レビュー)。
func TestGitLocationEnvCoversLocalEnvVars(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無い")
	}
	out, err := exec.Command("git", "rev-parse", "--local-env-vars").Output()
	if err != nil {
		t.Fatalf("git rev-parse --local-env-vars: %v", err)
	}
	vars := strings.Fields(string(out))
	if len(vars) < 10 {
		t.Fatalf("--local-env-vars が %d 個しか返さない (出力の読み方が壊れている): %q", len(vars), out)
	}
	for _, k := range vars {
		if !IsGitLocationEnv(k) {
			t.Errorf("%s を外さない (gitLocationEnv に足す)", k)
		}
	}
}
