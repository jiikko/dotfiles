package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildLockman は本物の lockman (src/lockman) をテストの一時ディレクトリへ build する (偽物では lockman の契約 = 121・子のグループ・シグナルの受け渡しを確かめられない)。
func buildLockman(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "lockman")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = filepath.Join("..", "..", "lockman")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("lockman を build できない: %v\n%s", err, out)
	}
	return bin
}

// gitRepoWithWorktree は一時の repo と、その worktree を 1 つ作る (本物の repo には触らない)。
func gitRepoWithWorktree(t *testing.T) (repo, wt string) {
	t.Helper()
	root := t.TempDir()
	repo, wt = filepath.Join(root, "repo"), filepath.Join(root, "wt")
	for _, args := range [][]string{
		{"init", "-q", repo},
		{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x"},
		{"-C", repo, "worktree", "add", "-q", wt},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return repo, wt
}

// outsideShell は pro-con の外の session が、issue 471 に書いた形で repo の lock の置き場を決めて script を走らせる。
func outsideShell(t *testing.T, dir, script string) {
	t.Helper()
	cmd := exec.Command("/bin/bash", "-c", `d="$(git rev-parse --path-format=absolute --git-common-dir)/pro-con-locks/test" && mkdir -p "$d" && `+script)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("外の session の %q: %v\n%s", script, err, out)
	}
}

// pro-con の外 (別の worktree の session・人) が repo の lock を持っている間、テストの係はコマンドを始めず「他が持っている」を返す。
// 空いたら始め、終了コードは頼まれたコマンドのもの (121 で抜けても lock の busy と取り違えない)。
func TestExecRunnerWaitsForOutsideHolderOfRepoLock(t *testing.T) {
	lockman := buildLockman(t)
	repo, wt := gitRepoWithWorktree(t)
	tok := filepath.Join(t.TempDir(), "tok")
	t.Setenv("PATH", filepath.Dir(lockman)+":"+os.Getenv("PATH"))   // 外の session は PATH の lockman を使う
	outsideShell(t, repo, `lockman acquire "$d" --token-file `+tok) // 外は repo の本体の worktree から取る
	r := ExecRunner{Lockman: lockman}
	log := filepath.Join(t.TempDir(), "log")
	rc, err := r.Run(context.Background(), wt, "touch ran", log, "lk1")
	if !errors.Is(err, errRunLockBusy) || rc != -1 {
		t.Fatalf("外が lock を持っているのに、他が持っているを返さない: rc=%d err=%v", rc, err)
	}
	if _, err := os.Stat(filepath.Join(wt, "ran")); err == nil {
		t.Fatal("外が lock を持っているのにコマンドを走らせた (直列になっていない)")
	}
	outsideShell(t, repo, `lockman release "$d" --token-file `+tok)
	rc, err = r.Run(context.Background(), wt, `touch ran; echo "${`+runPgidEnv+`-渡っていない}"`, log, "lk2")
	if rc != 0 || err != nil {
		t.Fatalf("空いた lock で走らない: rc=%d err=%v", rc, err)
	}
	if _, err := os.Stat(filepath.Join(wt, "ran")); err != nil {
		t.Fatal("空いた lock でコマンドを走らせていない")
	}
	if out, _ := os.ReadFile(log); strings.TrimSpace(string(out)) != "渡っていない" {
		t.Fatalf("pgid の置き場を頼まれたコマンドへ渡した (入れ子の実行が書き換える): %q", out)
	}
	if rc, err := r.Run(context.Background(), wt, "exit 121", log, "lk3"); rc != 121 || err != nil {
		t.Fatalf("頼まれたコマンドの 121 を lock の busy と取り違えた: rc=%d err=%v", rc, err)
	}
}

// lock を取って走らせたときも、bash が抜けた後に残った子を止める (lockman は子を別のグループに置くので、先頭のグループを撃つだけでは届かない)。
func TestExecRunnerLockedKillsLeftoverChild(t *testing.T) {
	_, wt := gitRepoWithWorktree(t)
	rc, err := ExecRunner{Lockman: buildLockman(t)}.Run(context.Background(), wt, `sleep 60 & echo $! > child.pid`, filepath.Join(t.TempDir(), "log"), "lk4")
	if rc != 0 || err != nil {
		t.Fatalf("rc=%d err=%v", rc, err)
	}
	waitGone(t, readPid(t, filepath.Join(wt, "child.pid")))
}

// lock を取って走らせたときも、取り消したら SIGTERM を無視する子まで止め、lock を解放する
// (lockman 本体を SIGKILL すると lock が TTL まで残り、repo の次の実行が「外が使用中」で待たされる)。
func TestExecRunnerLockedCancelKillsStubbornChild(t *testing.T) {
	_, wt := gitRepoWithWorktree(t)
	r := ExecRunner{Lockman: buildLockman(t)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		rc, _ := r.Run(ctx, wt, `trap "" TERM; sleep 60 & echo $! > child.pid; wait`, filepath.Join(t.TempDir(), "log"), "lk5")
		done <- rc
	}()
	child := readPid(t, filepath.Join(wt, "child.pid"))
	cancel()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("取り消しても実行が終わらない")
	}
	waitGone(t, child)
	if rc, err := r.Run(context.Background(), wt, "true", filepath.Join(t.TempDir(), "log"), "lk6"); rc != 0 || err != nil {
		t.Fatalf("取り消した後に lock が残っている: rc=%d err=%v", rc, err)
	}
}

// lockman が子を起こす前に失敗したら (lock の置き場を読めない等)、その rc を頼まれたコマンドの結果にしない。
func TestExecRunnerLockmanFailureIsNotCommandResult(t *testing.T) {
	_, wt := gitRepoWithWorktree(t)
	rc, err := ExecRunner{Lockman: "false"}.Run(context.Background(), wt, "touch ran", filepath.Join(t.TempDir(), "log"), "lk7")
	if rc != -1 || err == nil || !strings.Contains(err.Error(), "コマンドは走っていない") || errors.Is(err, errRunLockBusy) {
		t.Fatalf("lockman の失敗をコマンドの結果にした: rc=%d err=%v", rc, err)
	}
}

// lock を取って走らせた実行も、前の dispatcher が残したものとして印で見つけて止められる (印は lockman の下の bash に載る)。
func TestKillStaleFindsLockedRun(t *testing.T) {
	_, wt := gitRepoWithWorktree(t)
	r := ExecRunner{Lockman: buildLockman(t)}
	runID := fmt.Sprintf("%s-%d", t.Name(), os.Getpid()) // 並行する別の session の同じテストと撃ち合わない
	done := make(chan int, 1)
	go func() {
		rc, _ := r.Run(context.Background(), wt, "echo $$ > started; sleep 60", filepath.Join(t.TempDir(), "log"), runID)
		done <- rc
	}()
	readPid(t, filepath.Join(wt, "started")) // bash が起動した (印の文字列は lockman の引数にも載るので、ps の一致では bash の起動を待てない)
	killStale(runID)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("印で止めても lock を取った実行が終わらない")
	}
}

// runLockDir は同じ repo の worktree どうしで同じ置き場になり、別の repo なら別になる。
func TestRunLockDirIsPerRepo(t *testing.T) {
	repo, wt := gitRepoWithWorktree(t)
	other, _ := gitRepoWithWorktree(t)
	a, errA := runLockDir(context.Background(), repo)
	b, errB := runLockDir(context.Background(), wt)
	c, errC := runLockDir(context.Background(), other)
	if errA != nil || errB != nil || errC != nil {
		t.Fatal(errA, errB, errC)
	}
	if a != b || a == c {
		t.Fatalf("置き場が repo ごとにならない: 本体=%s worktree=%s 別の repo=%s", a, b, c)
	}
	if _, err := runLockDir(context.Background(), t.TempDir()); err == nil {
		t.Fatal("repo の外なのに置き場を決めた (lock を取らずに走ってしまう)")
	}
}
