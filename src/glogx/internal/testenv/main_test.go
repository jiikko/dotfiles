package testenv_test

import (
	"os"
	"testing"

	tea "charm.land/bubbletea/v2"

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

// recordTB は Fatalf / Skipf を呼ばれたことだけを記録する (SkipEnv の分岐を、本物の t を止めずに見る)。
type recordTB struct {
	testing.TB
	fatal, skip bool
	errors      int
}

func (r *recordTB) Errorf(string, ...any) { r.errors++ }
func (r *recordTB) Fatal(...any)          { r.fatal = true }

func (r *recordTB) Helper()               {}
func (r *recordTB) Fatalf(string, ...any) { r.fatal = true }
func (r *recordTB) Skipf(string, ...any)  { r.skip = true }

// SkipEnv は CI (環境変数 CI が立っている) では落とし、それ以外では skip にする。
func TestSkipEnvFailsOnlyInCI(t *testing.T) {
	t.Setenv("CI", "true")
	r := &recordTB{TB: t}
	testenv.SkipEnv(r, "x")
	if !r.fatal || r.skip {
		t.Errorf("CI で fatal=%v skip=%v (落とすはず)", r.fatal, r.skip)
	}
	t.Setenv("CI", "")
	r = &recordTB{TB: t}
	testenv.SkipEnv(r, "x")
	if r.fatal || !r.skip {
		t.Errorf("CI の外で fatal=%v skip=%v (skip のはず)", r.fatal, r.skip)
	}
}

// Restore はテストの終わりに、呼んだ時点の値へ戻す (差し替えた後の値ではなく)。
func TestRestoreRestoresValueAtCall(t *testing.T) {
	v := "orig"
	t.Run("sub", func(t *testing.T) {
		testenv.Restore(t, &v)
		v = "fake"
	})
	if v != "orig" {
		t.Errorf("v = %q, want orig (サブテストの終わりに戻す)", v)
	}
}

// ContainsAll は無いものの数だけ失敗を出す (全部あれば出さない)。
func TestContainsAllReportsEachMissing(t *testing.T) {
	r := &recordTB{TB: t}
	testenv.ContainsAll(r, "x", "abc", "a", "z", "c", "y")
	if r.errors != 2 {
		t.Errorf("errors = %d, want 2 (z と y)", r.errors)
	}
	r = &recordTB{TB: t}
	testenv.ContainsAll(r, "x", "abc", "a", "b")
	if r.errors != 0 {
		t.Errorf("errors = %d, want 0", r.errors)
	}
}

type msgA struct{ n int }
type msgB struct{}

// TakeMsg は単発の Cmd からも Batch の中からも型で Msg を取り出し、無ければ落とす。
func TestTakeMsgFindsByType(t *testing.T) {
	single := func() tea.Msg { return msgA{1} }
	if got := testenv.TakeMsg[msgA](t, single); got.n != 1 {
		t.Errorf("単発: %+v", got)
	}
	batch := tea.Batch(func() tea.Msg { return msgB{} }, func() tea.Msg { return msgA{2} })
	if got := testenv.TakeMsg[msgA](t, batch); got.n != 2 {
		t.Errorf("Batch: %+v", got)
	}
	r := &recordTB{TB: t}
	testenv.TakeMsg[msgA](r, func() tea.Msg { return msgB{} })
	if !r.fatal {
		t.Error("無いのに落ちない")
	}
}
