package dispatcher

import (
	"context"
	"fmt"
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

// 取り消したら、SIGTERM を無視する子もプロセスグループごと止める (dispatcher の後にコマンドを残さない)。
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

// 前の dispatcher が残した実行は、実行ごとの印で見つけて止める。単純なコマンド (bash が exec で置き換わる形) でも印が残ること、
// 印の違う実行と、同じ印を引数の最後に置いた他のプロセス (dispatcher が起こした形ではないもの) は撃たないことを、本物のプロセスで確かめる。
// 生死は Wait の完了で見る (撃たれた子はゾンビで残り kill(pid, 0) が成功する)。印はテストごとに一意にする (並行するテストと撃ち合わない)。
func TestKillStaleByRunMarker(t *testing.T) {
	id := func(s string) string { return fmt.Sprintf("%s-%s-%d.log", t.Name(), s, os.Getpid()) }
	start := func(cmd *exec.Cmd) chan struct{} {
		t.Helper()
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); <-done })
		return done
	}
	other := start(runCommand(context.Background(), "sleep 61", id("other")))
	mine := start(runCommand(context.Background(), "sleep 62", id("mine"))) // 単純なコマンド (印が無ければ exec で消える形)
	// 偽物は exec されない形 (2 つの文) にする。sh が sleep に置き換わると印が消えて、照合の候補にすらならない (偽物の意味が無い)
	imposter := start(exec.Command("/bin/sh", "-c", "sleep 63; true", runMarkerPrefix+id("mine")))
	// `/bin/bash` でも `-c` ではない形 (照合の f[3] を外すと撃たれる)
	notC := start(exec.Command("/bin/bash", "-x", "-c", "sleep 64; true", runMarkerPrefix+id("mine")))
	// グループの先頭ではない `/bin/bash -c … 印` (先頭は別の bash。照合の pid = pgid を外すとグループごと撃たれる)
	notLeader := start(exec.Command("/bin/bash", "-c", runCommandEnv+"='sleep 65' /bin/bash -c 'eval \"$"+runCommandEnv+"\"' "+runMarkerPrefix+id("mine")+"; true"))
	waitMarker(t, id("other")) // 否定の確認の前提: 撃たれうる形で ps に出ている
	waitMarker(t, id("mine"))
	waitPS(t, "/bin/sh -c sleep 63; true "+runMarkerPrefix+id("mine")) // 偽物も ps に出ている
	waitPS(t, "/bin/bash -x -c sleep 64; true "+runMarkerPrefix+id("mine"))
	waitPS(t, "/bin/bash -c eval \"$"+runCommandEnv+"\" "+runMarkerPrefix+id("mine")) // 先頭ではない方 (mine 本体と同じ形)
	killStale(id("mine"))
	select {
	case <-mine:
	case <-time.After(5 * time.Second):
		t.Fatal("印で残った自分の実行を止めない")
	}
	select {
	case <-other:
		t.Fatal("印の違う実行を撃った")
	case <-imposter:
		t.Fatal("dispatcher が起こした形ではないプロセスを、印が同じというだけで撃った")
	case <-notC:
		t.Fatal("`-c` ではない bash を、印が同じというだけで撃った")
	case <-notLeader:
		t.Fatal("グループの先頭ではない bash の印で、そのグループを撃った")
	case <-time.After(300 * time.Millisecond): // 否定の確認 (起きないことに待つ条件は無い)
	}
}

// waitMarker は印の実行が ps に出るまで待つ (上限 5 秒)。
func waitMarker(t *testing.T, runID string) {
	t.Helper()
	waitPS(t, "/bin/bash -c eval \"$"+runCommandEnv+"\" "+runMarkerPrefix+runID)
}

// waitPS は ps の command にその文が出るまで待つ (上限 5 秒)。
func waitPS(t *testing.T, want string) {
	t.Helper()
	for range 250 {
		out, _ := exec.Command("ps", "-A", "-ww", "-o", "command=").Output()
		if strings.Contains(string(out), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("ps に出ない: %s", want)
}

// 頼まれたコマンドの意味と終了コードは、台本で包んでも変わらない (末尾のバックスラッシュも元の bash -c と同じに読む)。
// 子がシグナルで死んだときは bash が 128+n で返す。
func TestRunCommandKeepsMeaningAndExitCode(t *testing.T) {
	for _, tc := range []struct {
		command string
		rc      int
		out     string
	}{
		{"exit 3", 3, ""},
		{`echo a \`, 0, "a\n"},
		{`sh -c 'kill -SEGV $$'`, 139, ""},
		{`echo 'unterminated`, 1, ""}, // 構文エラーは rc=1 (直接の bash -c は 2。runScript の注記)
	} {
		dir := t.TempDir()
		log := filepath.Join(dir, "log")
		rc, err := ExecRunner{}.Run(context.Background(), dir, tc.command, log, "t3")
		out, _ := os.ReadFile(log)
		if rc != tc.rc || err != nil || (tc.out != "" && string(out) != tc.out) {
			t.Fatalf("%q: rc=%d err=%v 出力=%q (期待 rc=%d 出力=%q)", tc.command, rc, err, out, tc.rc, tc.out)
		}
	}
}
