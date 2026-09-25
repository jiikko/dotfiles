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
	go func() {
		rc, _ := ExecRunner{}.Run(ctx, dir, `trap "" TERM; sleep 60 & echo $! > child.pid; wait`, filepath.Join(dir, "log"), "t1")
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
}

// bash が抜けた後に残った子も止める。
func TestExecRunnerKillsLeftoverChild(t *testing.T) {
	dir := t.TempDir()
	rc, err := ExecRunner{}.Run(context.Background(), dir, `sleep 60 & echo $! > child.pid`, filepath.Join(dir, "log"), "t2")
	if rc != 0 || err != nil {
		t.Fatalf("rc=%d err=%v", rc, err)
	}
	waitGone(t, readPid(t, filepath.Join(dir, "child.pid")))
}

// 前の daemon が残した実行は、実行ごとの印で見つけて止める。単純なコマンド (bash が exec で置き換わる形) でも印が残ること、
// 印の違う実行は撃たないことを、本物の bash で確かめる。生死は Wait の完了で見る (撃たれた子はゾンビで残り kill(pid, 0) が成功する)。
func TestKillStaleByRunMarker(t *testing.T) {
	start := func(command, runID string) chan struct{} {
		t.Helper()
		cmd := runCommand(context.Background(), command, runID)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); <-done })
		return done
	}
	other := start("sleep 61", "other-run.log")
	mine := start("sleep 62", "mine-run.log") // 単純なコマンド (印が無ければ exec で消える形)
	waitMarker(t, "other-run.log")            // 否定の確認の前提: 撃たれうる形で ps に出ている
	waitMarker(t, "mine-run.log")
	killStale("mine-run.log")
	select {
	case <-mine:
	case <-time.After(5 * time.Second):
		t.Fatal("印で残った自分の実行を止めない")
	}
	select {
	case <-other:
		t.Fatal("印の違う実行を撃った")
	case <-time.After(300 * time.Millisecond): // 否定の確認 (起きないことに待つ条件は無い)
	}
}

// waitMarker は印の実行が ps に出るまで待つ (上限 5 秒)。
func waitMarker(t *testing.T, runID string) {
	t.Helper()
	for range 250 {
		out, _ := exec.Command("ps", "-A", "-ww", "-o", "command=").Output()
		if strings.Contains(string(out), runMarkerPrefix+runID) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("印 %s の実行が ps に出ない", runID)
}

// 実行の終了コードは、印のために足した `exit $?` の行を通っても変わらない。
func TestRunCommandKeepsExitCode(t *testing.T) {
	dir := t.TempDir()
	rc, err := ExecRunner{}.Run(context.Background(), dir, "exit 3", filepath.Join(dir, "log"), "t3")
	if rc != 3 || err != nil {
		t.Fatalf("終了コードが変わった: rc=%d err=%v", rc, err)
	}
}
