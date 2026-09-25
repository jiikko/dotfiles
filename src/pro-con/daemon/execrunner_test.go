package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// alive は pid のプロセスがまだ居るか (ゾンビは reap されるまで居る扱いになるので、上限つきで居なくなるのを待つ側で使う)。
func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// waitGone は pid が居なくなるまで待つ (上限 5 秒)。
func waitGone(t *testing.T, pid int) {
	t.Helper()
	for i := 0; alive(pid); i++ {
		if i > 250 {
			_ = syscall.Kill(pid, syscall.SIGKILL) // 後始末 (テストの子を残さない)
			t.Fatalf("pid %d が 5 秒たっても残っている", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func readPid(t *testing.T, path string) int {
	t.Helper()
	for range 250 {
		if b, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(b)) != "" {
			pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
			if err != nil {
				t.Fatal(err)
			}
			return pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("子の pid が書かれない")
	return 0
}

// 取り消したら、SIGTERM を無視する子もプロセスグループごと止める (daemon の後にコマンドを残さない)。
func TestExecRunnerCancelKillsStubbornChild(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	var pgid int
	go func() {
		rc, _ := ExecRunner{}.Run(ctx, dir, `trap "" TERM; sleep 60 & echo $! > child.pid; wait`, filepath.Join(dir, "log"), func(p int) { pgid = p })
		done <- rc
	}()
	child := readPid(t, pidFile)
	cancel()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("取り消しても実行が終わらない")
	}
	waitGone(t, child)
	if pgid == 0 {
		t.Fatal("プロセスグループを知らせない")
	}
}

// bash が抜けた後に残った子も止める。
func TestExecRunnerKillsLeftoverChild(t *testing.T) {
	dir := t.TempDir()
	rc, err := ExecRunner{}.Run(context.Background(), dir, `sleep 60 & echo $! > child.pid`, filepath.Join(dir, "log"), nil)
	if rc != 0 || err != nil {
		t.Fatalf("rc=%d err=%v", rc, err)
	}
	waitGone(t, readPid(t, filepath.Join(dir, "child.pid")))
}

// 前の daemon が残したコマンドは、グループの先頭がまだそのコマンドを実行しているときだけ止める (pid の使い回しで別のものを撃たない)。
// 生死は Wait の完了で見る (撃たれた子は reap されるまでゾンビで残り、kill(pid, 0) が成功する)。
func TestKillStaleOnlyOwnCommand(t *testing.T) {
	start := func(command string) (*exec.Cmd, chan struct{}) {
		t.Helper()
		cmd := exec.Command("/bin/bash", "-c", command)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); <-done })
		return cmd, done
	}
	other, otherDone := start("sleep 61; true")
	killStale(other.Process.Pid, "make test") // 違うコマンド
	select {
	case <-otherDone:
		t.Fatal("別のコマンドのプロセスグループを撃った")
	case <-time.After(300 * time.Millisecond): // 否定の確認 (起きないことに待つ条件は無い)
	}
	mine, mineDone := start("sleep 62; true")
	killStale(mine.Process.Pid, "sleep 62; true")
	select {
	case <-mineDone:
	case <-time.After(5 * time.Second):
		t.Fatal("残った自分のコマンドを止めない")
	}
}
