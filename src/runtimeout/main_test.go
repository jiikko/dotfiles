package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

var bin string // テスト用にビルドした runtimeout の絶対パス

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "runtimeout-test")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "runtimeout")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		panic(string(out))
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// runTool は runtimeout を起動して rc と stdout を返す
func runTool(t *testing.T, stdin string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), string(out)
	} else if err != nil {
		t.Fatalf("runtimeout を起動できない: %v", err)
	}
	return 0, string(out)
}

// runToolNoOut は出力を取らずに起動して rc を返す。止め方を見るテストで使う
// (出力を取ると、止め損ねた孫がパイプを握り続けて Wait が戻らず、FAIL ではなく hang になる)
func runToolNoOut(t *testing.T, args ...string) int {
	t.Helper()
	err := exec.Command(bin, args...).Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	} else if err != nil {
		t.Fatalf("runtimeout を起動できない: %v", err)
	}
	return 0
}

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
		if err != nil {
			return false
		}
		s := strings.TrimSpace(string(b))
		if !strings.HasSuffix(string(b), "\n") {
			return false
		}
		pid, err = strconv.Atoi(s)
		return err == nil
	})
	return pid
}

func killIfAlive(pid int) {
	if pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

func TestPassesChildRC(t *testing.T) {
	rc, _ := runTool(t, "", "5", "/bin/sh", "-c", "exit 3")
	if rc != 3 {
		t.Fatalf("子の rc をそのまま返さない: got %d want 3", rc)
	}
}

func TestPassesStdinStdout(t *testing.T) {
	rc, out := runTool(t, "hello\n", "5", "cat")
	if rc != 0 || out != "hello\n" {
		t.Fatalf("stdin/stdout を通さない: rc=%d out=%q", rc, out)
	}
}

func TestZeroMeansNoLimit(t *testing.T) {
	rc, out := runTool(t, "", "0", "/bin/sh", "-c", "sleep 0.3; echo done") // sleep-ok: dummy: 上限 0 (無制限) で走り切ることを見る子
	if rc != 0 || out != "done\n" {
		t.Fatalf("0 を上限なしとして扱わない: rc=%d out=%q", rc, out)
	}
}

func TestSignaledChildIs128PlusN(t *testing.T) {
	rc, _ := runTool(t, "", "5", "/bin/sh", "-c", "kill -TERM $$")
	if rc != 128+int(syscall.SIGTERM) {
		t.Fatalf("シグナルで死んだ子の rc: got %d want %d", rc, 128+int(syscall.SIGTERM))
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"5"},
		{"-1", "true"},
		{"abc", "true"},
		{"--__autobuild_warmup__"}, // go_autobuild の温めは未知のフラグで即終了することを要求する
		{"-k"},
	} {
		if rc, _ := runTool(t, "", args...); rc != rcUsage {
			t.Errorf("%q: rc=%d want %d", args, rc, rcUsage)
		}
	}
}

func TestStartErrors(t *testing.T) {
	if rc, _ := runTool(t, "", "5", "/nonexistent/cmd"); rc != rcNotFound {
		t.Errorf("見つからない: rc=%d want %d", rc, rcNotFound)
	}
	if rc, _ := runTool(t, "", "5", "no-such-command-runtimeout"); rc != rcNotFound {
		t.Errorf("PATH に無い: rc=%d want %d", rc, rcNotFound)
	}
	noexec := filepath.Join(t.TempDir(), "noexec")
	if err := os.WriteFile(noexec, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rc, _ := runTool(t, "", "5", noexec); rc != rcCannotExec {
		t.Errorf("実行権が無い: rc=%d want %d", rc, rcCannotExec)
	}
}

// 時間切れ: rc=124 で、次が全部止まる。子 / グループの中の孫 / setsid でグループを抜けた孫 (木でたどる) /
// 親が先に死んで init の子になった孫 (グループ宛てで届く) / その init の子になった孫がさらに setsid で抜けた子
// (グループの全員から木でたどる。グループの全員を集める処理を外すと残る)
func TestTimeoutStopsWholeTree(t *testing.T) {
	dir := t.TempDir()
	f := func(n string) string { return filepath.Join(dir, n) }
	setsidSleep := `perl -e 'use POSIX; POSIX::setsid(); open(my $f, ">", $ARGV[0]); print $f "$$\n"; close $f; exec "sleep", "300"'` // sleep-ok: dummy: 時間切れまで居座る孫
	script := `echo $$ > "$1"
sleep 300 & echo $! > "$2"
` + setsidSleep + ` "$3" &
( sleep 300 & echo $! > "$4" )
( /bin/sh -c 'echo $$ > "$1"; ` + strings.ReplaceAll(setsidSleep, "'", `'"'"'`) + ` "$2" & wait' sh "$5" "$6" & )
wait`
	type result struct{ rc int }
	res := make(chan result, 1)
	go func() {
		res <- result{runToolNoOut(t, "-k", "1", "1", "/bin/sh", "-c", script, "sh", f("child"), f("grand"), f("escaped"), f("orphan"), f("orphan-sh"), f("orphan-escaped"))}
	}()
	names := []string{"子", "グループの中の孫", "setsid で抜けた孫", "親が先に死んだ孫", "親が先に死んだ sh", "親が先に死んだ sh の setsid した子"}
	files := []string{"child", "grand", "escaped", "orphan", "orphan-sh", "orphan-escaped"}
	var pids []int
	for _, n := range files {
		pids = append(pids, readPid(t, f(n)))
	}
	defer func() {
		for _, p := range pids {
			killIfAlive(p)
		}
	}()
	r := <-res
	if r.rc != rcTimeout {
		t.Fatalf("時間切れの rc: got %d want %d", r.rc, rcTimeout)
	}
	for i, name := range names {
		pid := pids[i]
		waitFor(t, name+"が止まる", func() bool { return !alive(pid) })
	}
}

// TERM を無視する子は、猶予の後に KILL で止まる
func TestTimeoutKillsAfterGrace(t *testing.T) {
	pidf := filepath.Join(t.TempDir(), "pid")
	res := make(chan int, 1)
	go func() {
		res <- runToolNoOut(t, "-k", "0.3", "0.5", "perl", "-e",
			`$SIG{TERM} = "IGNORE"; open(my $f, ">", $ARGV[0]); print $f "$$\n"; close $f; sleep 300`, pidf) // sleep-ok: dummy: TERM を無視して居座る子
	}()
	pid := readPid(t, pidf)
	defer killIfAlive(pid)
	select {
	case rc := <-res:
		if rc != rcTimeout {
			t.Fatalf("rc: got %d want %d", rc, rcTimeout)
		}
	case <-time.After(30 * time.Second): // hang の安全網 (判定ではない)
		t.Fatal("TERM を無視する子を止められず、道具が戻らない")
	}
	waitFor(t, "TERM を無視する子が KILL で止まる", func() bool { return !alive(pid) })
}

// 時間切れでは、グループの中の子にも KILL より先に TERM が届く (後始末の機会がある)
func TestTimeoutDeliversTermFirst(t *testing.T) {
	dir := t.TempDir()
	got := filepath.Join(dir, "got-term")
	rc := runToolNoOut(t, "-k", "5", "0.5", "/bin/sh", "-c", `trap 'echo term > "$1"; exit 0' TERM; sleep 300 & wait`, "sh", got) // sleep-ok: dummy: 時間切れまで居座る孫
	if rc != rcTimeout {
		t.Fatalf("rc: got %d want %d", rc, rcTimeout)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("子の TERM の trap が走っていない (KILL だけで止めた): %v", err)
	}
}

// 道具が受けた TERM で子と孫が止まり、道具も TERM で死に直す (呼び出し元の bash に「中断された」を伝える)
func TestTermStopsTreeAndReraises(t *testing.T) {
	dir := t.TempDir()
	child, grand := filepath.Join(dir, "child"), filepath.Join(dir, "grand")
	cmd := exec.Command(bin, "-k", "1", "0", "/bin/sh", "-c", `echo $$ > "$1"; sleep 300 & echo $! > "$2"; wait`, "sh", child, grand)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pids := []int{readPid(t, child), readPid(t, grand)}
	defer func() {
		for _, p := range pids {
			killIfAlive(p)
		}
	}()
	_ = cmd.Process.Signal(syscall.SIGTERM)
	_ = cmd.Wait()
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
		t.Fatalf("TERM を受けた道具が TERM で死に直していない: %v", cmd.ProcessState)
	}
	for i, name := range []string{"子", "孫"} {
		pid := pids[i]
		waitFor(t, name+"が止まる", func() bool { return !alive(pid) })
	}
}

// INT を無視して起動された (bash の非対話の & の形) ときも、子には既定の INT が渡る
func TestChildGetsDefaultINT(t *testing.T) {
	// perl で INT を無視してから runtimeout を exec し、子の sh が自分に INT を撃って死ぬかを見る
	rc, _ := runTool(t, "", "5", "/bin/sh", "-c", "kill -INT $$; sleep 5; exit 0") // sleep-ok: dummy: INT で死ななかったときに居座る
	if rc != 128+int(syscall.SIGINT) {
		t.Fatalf("前提: INT の既定で死ぬはず: rc=%d", rc)
	}
	cmd := exec.Command("perl", "-e", `$SIG{INT} = "IGNORE"; exec @ARGV or die`, bin, "5", "/bin/sh", "-c", "kill -INT $$; sleep 5; exit 0") // sleep-ok: dummy: 同上
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 128+int(syscall.SIGINT) {
		t.Fatalf("INT を無視した親から起動したとき、子に既定の INT が渡らない: err=%v", err)
	}
}

func TestParseSeconds(t *testing.T) {
	for in, want := range map[string]time.Duration{"0": 0, "1": time.Second, "0.5": 500 * time.Millisecond, "60": time.Minute} {
		got, err := parseSeconds(in)
		if err != nil || got != want {
			t.Errorf("%q: got %v,%v want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "-1", "NaN", "Inf", "1e30", "x"} {
		if _, err := parseSeconds(in); err == nil {
			t.Errorf("%q を受け付けた", in)
		}
	}
}

// -f: 子は呼び出し元 (道具) と同じグループに居て、時間切れでは子と孫が止まる
func TestForegroundKeepsGroupAndStopsTree(t *testing.T) {
	dir := t.TempDir()
	child, grand := filepath.Join(dir, "child"), filepath.Join(dir, "grand")
	cmd := exec.Command(bin, "-f", "-k", "1", "1", "/bin/sh", "-c", `echo $$ > "$1"; sleep 300 & echo $! > "$2"; wait`, "sh", child, grand) // sleep-ok: dummy: 時間切れまで居座る孫
	// 道具を専用グループに置き、その中に子が留まるかを見る (テストのプロセスを巻き込まない)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pids := []int{readPid(t, child), readPid(t, grand)}
	defer func() {
		for _, p := range pids {
			killIfAlive(p)
		}
	}()
	pgid, err := syscall.Getpgid(pids[0])
	if err != nil || pgid != cmd.Process.Pid {
		t.Errorf("-f の子が道具のグループに居ない: pgid=%d err=%v tool=%d", pgid, err, cmd.Process.Pid)
	}
	_ = cmd.Wait()
	if rc := cmd.ProcessState.ExitCode(); rc != rcTimeout {
		t.Fatalf("rc: got %d want %d", rc, rcTimeout)
	}
	for i, name := range []string{"子", "孫"} {
		pid := pids[i]
		waitFor(t, name+"が止まる", func() bool { return !alive(pid) })
	}
}

// 既定では子は専用グループ (pgid = 子の pid) に居る
func TestDefaultPutsChildInOwnGroup(t *testing.T) {
	pidf := filepath.Join(t.TempDir(), "pid")
	cmd := exec.Command(bin, "5", "/bin/sh", "-c", `echo $$ > "$1"; sleep 300`, "sh", pidf) // sleep-ok: dummy: グループを見る間だけ居座る子
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := readPid(t, pidf)
	defer killIfAlive(pid)
	pgid, err := syscall.Getpgid(pid)
	_ = syscall.Kill(pid, syscall.SIGKILL)
	_ = cmd.Wait()
	if err != nil || pgid != pid {
		t.Fatalf("子が専用グループに居ない: pgid=%d pid=%d err=%v", pgid, pid, err)
	}
}

// 起動時に HUP を無視されていたら (nohup)、子にも無視のまま渡る
func TestIgnoredHUPStaysIgnored(t *testing.T) {
	cmd := exec.Command("perl", "-e", `$SIG{HUP} = "IGNORE"; exec @ARGV or die`, bin, "5", "/bin/sh", "-c", "kill -HUP $$; echo survived")
	out, err := cmd.Output()
	if err != nil || string(out) != "survived\n" {
		t.Fatalf("nohup の HUP 無視が子に渡っていない: err=%v out=%q", err, out)
	}
}

// -f で ps が使えなくても、TERM を無視する子を猶予の後に KILL で止める
func TestForegroundStopWithoutPS(t *testing.T) {
	old := psPath
	psPath = "/nonexistent/ps"
	defer func() { psPath = old }()
	pidf := filepath.Join(t.TempDir(), "pid")
	cmd := exec.Command("perl", "-e", `$SIG{TERM} = "IGNORE"; open(my $f, ">", $ARGV[0]); print $f "$$\n"; close $f; sleep 300`, pidf) // sleep-ok: dummy: TERM を無視して居座る子
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := readPid(t, pidf)
	defer killIfAlive(pid)
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	target{root: pid, group: false}.stop(200 * time.Millisecond)
	select {
	case <-done:
	case <-time.After(10 * time.Second): // hang の安全網 (判定ではない)
		t.Fatal("ps が使えない -f の stop で、TERM を無視する子が止まらない")
	}
}

// fork し続ける子: 止めている間に setsid した子が逃げない (root を凍らせてから集める)。
// 逃げた子は各自が pid を dir に書くので、止め終えた後に生き残りが居ないかを数える
// 逃げるのは窓に当たった子だけなので、root を凍らせない変異でも 1 回に残るのは 100 個強のうち数個 (確率的。2026-10-07 の実測で
// 2/113・4/115)。緑が 1 回出ても変異を検出できないとは限らない
func TestTimeoutStopsForkingChild(t *testing.T) {
	for _, fg := range []bool{false, true} {
		dir := t.TempDir()
		forker := `while :; do perl -MPOSIX -e 'POSIX::setsid(); open(my $f, ">", "$ARGV[0]/$$"); close $f; sleep 100' "$1" & sleep 0.003; done` // sleep-ok: dummy: fork し続けて居座る子
		args := []string{"-k", "1", "0.5", "/bin/bash", "-c", forker, "bash", dir}
		if fg {
			args = append([]string{"-f"}, args...)
		}
		if rc := runToolNoOut(t, args...); rc != rcTimeout {
			t.Fatalf("-f=%v: rc=%d want %d", fg, rc, rcTimeout)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) == 0 {
			t.Fatalf("-f=%v: 前提: setsid した子が 1 つも起動していない (err=%v)", fg, err)
		}
		var left []int
		for _, e := range entries {
			pid, err := strconv.Atoi(e.Name())
			if err == nil && alive(pid) {
				left = append(left, pid)
			}
		}
		for _, p := range left {
			killIfAlive(p)
		}
		if len(left) > 0 {
			t.Errorf("-f=%v: setsid した子が %d/%d 個残った", fg, len(left), len(entries))
		}
	}
}
