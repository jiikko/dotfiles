package testenv_test

import (
	"os"
	"testing"

	"glogx/internal/testenv"
)

// TestMain は継承した GIT_DIR を置いてから Main を通す。Main が m.Run の前にそれを外したことを下のテストが見る。
// tests/scripts/test_git_tests_unset_location_env.sh は Main の本文の字面しか見ず、guard が m.Run の後に走る形
// (defer・m.Run を別の関数の中で呼ぶ) を通すので、実行時に確かめる。
func TestMain(m *testing.M) {
	if err := os.Setenv("GIT_DIR", "/nonexistent/glogx-testenv-inherited-git-dir"); err != nil {
		panic(err)
	}
	testenv.Main(m, "glogx-testenv", nil)
}

func TestMainUnsetsInheritedGitDirBeforeRun(t *testing.T) {
	if v, ok := os.LookupEnv("GIT_DIR"); ok {
		t.Fatalf("Main が m.Run の前に継承した GIT_DIR を外していない: %q", v)
	}
}
