package filer

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// needGit は git の無い環境ではテストを飛ばす。CI (macos runner には git がある) で無いのは環境の壊れなので落とす
// (Skip のまま緑にすると、git を使う検査が CI で 1 本も走らないことが見えない。issue 695 の 3)。
func needGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("CI に git が無い")
		}
		t.Skip("git が無い")
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// commitAll は dir の変更を全部 stage して commit する。
func commitAll(t *testing.T, dir string) {
	t.Helper()
	gitIn(t, dir, "add", ".")
	gitCommit(t, dir)
}

// gitCommit は stage 済みのものを commit する (作者は固定。利用者の設定は読まない)。
func gitCommit(t *testing.T, dir string) {
	t.Helper()
	gitIn(t, dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "x")
}

// waitFor は cond が true になるまで刻んで待つ。10 秒は hang guard (実時間の合否ではない。止まらないときだけ落とす)。
// cond の中で Advance を呼んでよい (裏の結果の取り込みと待ちを 1 つの条件にする)。
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s が 10 秒で終わらない", what)
		}
		time.Sleep(5 * time.Millisecond) // sleep-ok: tick: 裏の goroutine の完了を条件で待つループの刻み
	}
}

// settle は動きを終点まで進める (Advance で裏の結果を取り込み、アニメーションを飛ばす)。
func settle(m *Model) {
	m.Advance(fixedNow)
	m.snapAll()
}

// newAt は dir を root にした Model (時刻は fixedNow に固定)。大きさは呼び出し側が決める。
func newAt(t *testing.T, dir string) *Model {
	t.Helper()
	m, err := New(dir, Options{Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// mustWrite は path に body を書く (0o644)。
func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// initRepo は dir に branch を初期の枝にした repo を作る。
func initRepo(t *testing.T, dir, branch string) {
	t.Helper()
	gitIn(t, dir, "init", "-q", "-b", branch)
}
