package proctree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func alive(pid int) bool {
	out, err := exec.Command("/bin/ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return false
	}
	s := strings.TrimSpace(string(out))
	return s != "" && !strings.HasPrefix(s, "Z")
}

// waitFor は cond が成立するまで待つ。上限は判定ではなく hang の安全網
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for range 400 {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond) // sleep-ok: tick: 条件のポーリングの刻み
	}
	t.Fatalf("10 秒待っても成立しない: %s", what)
}

func readPid(t *testing.T, path string) int {
	t.Helper()
	var pid int
	waitFor(t, "pid ファイル "+path, func() bool {
		b, err := os.ReadFile(path)
		if err != nil || !strings.HasSuffix(string(b), "\n") {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(b)))
		return err == nil
	})
	return pid
}

// startTree は根 (/bin/sh) を起こし、グループの中の孫と setsid で抜けた孫の pid を返す。group なら根を専用グループに置く
func startTree(t *testing.T, group bool) (root *exec.Cmd, pids []int) {
	t.Helper()
	dir := t.TempDir()
	f := func(n string) string { return filepath.Join(dir, n) }
	script := `echo $$ > "$1"
sleep 300 & echo $! > "$2"
perl -e 'use POSIX; POSIX::setsid(); open(my $f, ">", $ARGV[0]); print $f "$$\n"; close $f; exec "sleep", "300"' "$3" &
wait` // sleep-ok: dummy: 止められるまで居座る孫
	cmd := exec.Command("/bin/sh", "-c", script, "sh", f("root"), f("grand"), f("escaped"))
	if group {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"root", "grand", "escaped"} {
		pids = append(pids, readPid(t, f(n)))
	}
	t.Cleanup(func() {
		for _, p := range pids {
			_ = syscall.Kill(p, syscall.SIGKILL)
		}
		_ = cmd.Wait()
	})
	return cmd, pids
}

// Stop は根・グループの中の孫・setsid でグループを抜けた孫を止める (グループ版と、呼び出し元のグループに居る版の両方)
func TestStopTree(t *testing.T) {
	for _, group := range []bool{true, false} {
		cmd, pids := startTree(t, group)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		Target{Root: cmd.Process.Pid, Group: group}.Stop(time.Second)
		for i, name := range []string{"根", "グループの中の孫", "setsid で抜けた孫"} {
			pid := pids[i]
			waitFor(t, name+"が止まる (group="+strconv.FormatBool(group)+")", func() bool { return !alive(pid) })
		}
		<-done
	}
}

// Group が false で ps が使えなくても、TERM を無視する根を猶予の後に KILL で止める
func TestStopWithoutPS(t *testing.T) {
	old := PSPath
	PSPath = "/nonexistent/ps"
	defer func() { PSPath = old }()
	pidf := filepath.Join(t.TempDir(), "pid")
	cmd := exec.Command("perl", "-e", `$SIG{TERM} = "IGNORE"; open(my $f, ">", $ARGV[0]); print $f "$$\n"; close $f; sleep 300`, pidf) // sleep-ok: dummy: TERM を無視して居座る子
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := readPid(t, pidf)
	defer func() { _ = syscall.Kill(pid, syscall.SIGKILL) }()
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	Target{Root: pid, Group: false}.Stop(200 * time.Millisecond)
	select {
	case <-done:
	case <-time.After(10 * time.Second): // hang の安全網 (判定ではない)
		t.Fatal("ps が使えない Stop で、TERM を無視する子が止まらない")
	}
}
