package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGitPullRebase を実 git で検証する。守りたい不変条件は「pull の後始末の abort は、pull が起こした rebase だけに
// 掛ける」で、rebase の途中かどうかは git dir の中身 (rebase-merge) で決まるため、stub では確かめられない。
//
// 🚨 このテストは `git rebase --abort` を本物で走らせる。継承した GIT_DIR / GIT_WORK_TREE があると、cwd ではなく
// その repo を相手にする (hook から起動された go test 等)。下の pullSandbox が環境変数を外し、cwd の repo が
// 使い捨ての repo であることを確かめてから走らせる (違えば実行しない)。

// pullSandbox は ./tmp に bare の origin と clone を作り、clone へ cd して clone のパスを返す。
func pullSandbox(t *testing.T) string {
	t.Helper()
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_COMMON_DIR", "GIT_PREFIX"} {
		t.Setenv(k, "")
		if err := os.Unsetenv(k); err != nil {
			t.Fatal(err)
		}
	}
	base, err := os.MkdirTemp(repoTmpDir(t), "glogx-pull-real") //nolint:usetesting // 使い捨て repo は ./tmp 規約 (worktree_status_real_test.go 冒頭の doc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	origin, work := filepath.Join(base, "origin.git"), filepath.Join(base, "work")
	gitIn(t, base, "init", "-q", "--bare", "-b", "master", origin)
	gitIn(t, base, "clone", "-q", origin, work)
	gitIn(t, work, "config", "user.email", "t@example.com")
	gitIn(t, work, "config", "user.name", "t")
	for _, name := range []string{"a", "b", "c"} {
		writeFileIn(t, work, name, name+"\n")
		gitIn(t, work, "add", name)
		gitIn(t, work, "commit", "-q", "-m", name)
	}
	gitIn(t, work, "push", "-q", "origin", "master")
	t.Chdir(work)
	// 🚨 実行前の拒否: cwd の repo が使い捨ての clone でなければ、破壊的な git を 1 つも走らせない
	top, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("cwd の repo を解決できない: %v", err)
	}
	gotTop, _ := filepath.EvalSymlinks(strings.TrimSpace(string(top)))
	wantTop, _ := filepath.EvalSymlinks(work)
	if gotTop != wantTop {
		t.Fatalf("cwd の repo が使い捨ての clone ではない (got %s, want %s)。実 repo を触らないよう中止する", gotTop, wantTop)
	}
	return work
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeFileIn(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rebaseMergeExists(t *testing.T, work string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(work, ".git", "rebase-merge"))
	return err == nil
}

// ユーザーが `git rebase -i` を edit で止めたまま u を押しても、pull を断り、その rebase を abort しない。
func TestPullRebaseRefusesWhileUsersRebaseInProgress(t *testing.T) {
	work := pullSandbox(t)
	cmd := exec.Command("git", "rebase", "-i", "HEAD~2")
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "GIT_SEQUENCE_EDITOR=sed -i.bak s/^pick/edit/", "GIT_EDITOR=true")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("前提: rebase -i を edit で止められない: %v\n%s", err, out)
	}
	if !rebaseMergeExists(t, work) {
		t.Fatal("前提: rebase-merge が無い (rebase が止まっていない)")
	}

	err := runGitPullRebase(context.Background())
	if err == nil || !strings.Contains(err.Error(), "rebase の途中") {
		t.Fatalf("rebase の途中なのに pull を断らなかった: %v", err)
	}
	if !rebaseMergeExists(t, work) {
		t.Fatal("🚨 ユーザーが止めていた rebase を abort した (rebase-merge が消えた)")
	}
}

// pull 自身が conflict で止めた rebase は、これまでどおり abort して元に戻す。
func TestPullRebaseAbortsItsOwnConflict(t *testing.T) {
	work := pullSandbox(t)
	other := filepath.Join(filepath.Dir(work), "other")
	gitIn(t, filepath.Dir(work), "clone", "-q", filepath.Join(filepath.Dir(work), "origin.git"), other)
	gitIn(t, other, "config", "user.email", "t@example.com")
	gitIn(t, other, "config", "user.name", "t")
	writeFileIn(t, other, "a", "theirs\n")
	gitIn(t, other, "commit", "-q", "-am", "theirs")
	gitIn(t, other, "push", "-q", "origin", "master")

	writeFileIn(t, work, "a", "ours\n")
	gitIn(t, work, "commit", "-q", "-am", "ours")
	head := strings.TrimSpace(gitIn(t, work, "rev-parse", "HEAD"))

	err := runGitPullRebase(context.Background())
	if err == nil || !strings.Contains(err.Error(), "中断して元に戻しました") {
		t.Fatalf("pull の conflict を abort しなかった: %v", err)
	}
	if rebaseMergeExists(t, work) {
		t.Fatal("pull が起こした rebase が残っている (abort されていない)")
	}
	if got := strings.TrimSpace(gitIn(t, work, "rev-parse", "HEAD")); got != head {
		t.Fatalf("abort 後の HEAD が pull 前と違う: got %s, want %s", got, head)
	}
}

// 空になった cherry-pick で止まった状態 (CHERRY_PICK_HEAD があり作業ツリーは clean) でも pull を断り、その状態を消さない。
func TestPullRebaseRefusesWhileCherryPickInProgress(t *testing.T) {
	work := pullSandbox(t)
	cmd := exec.Command("git", "cherry-pick", "HEAD")
	cmd.Dir = work
	_ = cmd.Run() // 空の cherry-pick は rc≠0 で止まる。止まったことは下の CHERRY_PICK_HEAD で確かめる
	head := filepath.Join(work, ".git", "CHERRY_PICK_HEAD")
	if _, err := os.Stat(head); err != nil {
		t.Fatalf("前提: CHERRY_PICK_HEAD が無い (cherry-pick が止まっていない): %v", err)
	}

	err := runGitPullRebase(context.Background())
	if err == nil || !strings.Contains(err.Error(), "cherry-pick の途中") {
		t.Fatalf("cherry-pick の途中なのに pull を断らなかった: %v", err)
	}
	if _, statErr := os.Stat(head); statErr != nil {
		t.Fatal("🚨 ユーザーが止めていた cherry-pick の状態を消した (CHERRY_PICK_HEAD が無い)")
	}
}
