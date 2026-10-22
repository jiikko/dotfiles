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

var bin string // テスト用にビルドした serial-run の絶対パス

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "serialrun-test")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "serial-run")
	// 🚨 本物の待ち行列 (~/.local/state/serial-run) を読ませない・消させない (死んだ札の回収は os.Remove する)。
	// 個々のテストが置き場を渡し忘れても、既定がこの一時ディレクトリになる
	if err := os.Setenv("SERIAL_RUN_STATE_DIR", filepath.Join(dir, "state")); err != nil {
		panic(err)
	}
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		panic(string(out))
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// fixture は 1 本のテストの状態の置き場と、子が書くログ
type fixture struct {
	t     *testing.T
	state string
	dir   string
	log   string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	d := t.TempDir()
	return &fixture{t: t, state: filepath.Join(d, "state"), dir: d, log: filepath.Join(d, "log")}
}

// env はテストの子に渡す環境。外側の make test が serial-run に包まれていると SERIAL_RUN_HELD を継承しているので外す
func (f *fixture) env(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, heldEnv+"=") || strings.HasPrefix(kv, "SERIAL_RUN_") {
			continue
		}
		env = append(env, kv)
	}
	return append(append(env, "SERIAL_RUN_STATE_DIR="+f.state), extra...)
}

func (f *fixture) qdir() string { return filepath.Join(f.state, "default", "queue") }

// gate は子が読むまで止まる fifo を作る
func (f *fixture) gate(name string) string {
	f.t.Helper()
	p := filepath.Join(f.dir, name+".fifo")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		f.t.Fatal(err)
	}
	return p
}

// release は gate を読んで止まっている子を進める (読み手が開くまで open は返らない)
func (f *fixture) release(gate string) {
	f.t.Helper()
	w, err := os.OpenFile(gate, os.O_WRONLY, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	_, _ = w.WriteString("\n")
	_ = w.Close()
}

// held は「開始を書き、gate を読むまで止まり、終了を書く」子を serial-run で起こす
func (f *fixture) held(name, gate string, extraEnv []string, setpgid bool, args ...string) *exec.Cmd {
	f.t.Helper()
	script := "echo start " + name + " >> " + f.log + "; read _ < " + gate + "; echo end " + name + " >> " + f.log
	argv := append(append([]string{}, args...), "--", "/bin/sh", "-c", script)
	return f.start(extraEnv, setpgid, argv...)
}

func (f *fixture) start(extraEnv []string, setpgid bool, args ...string) *exec.Cmd {
	f.t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = f.env(extraEnv...)
	if setpgid {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	if err := cmd.Start(); err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	return cmd
}

func (f *fixture) logLines() []string {
	b, _ := os.ReadFile(f.log)
	return strings.Fields(strings.ReplaceAll(strings.TrimSpace(string(b)), "\n", "|"))
}

func (f *fixture) logText() string {
	b, _ := os.ReadFile(f.log)
	return string(b)
}

func (f *fixture) ticketCount() int {
	ents, _ := os.ReadDir(f.qdir())
	n := 0
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ticketSuffix) {
			n++
		}
	}
	return n
}

func (f *fixture) hasLog(line string) bool { return strings.Contains(f.logText(), line+"\n") }

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

func exitCode(t *testing.T, cmd *exec.Cmd) int {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				return 128 + int(ws.Signal())
			}
			return ee.ExitCode()
		}
		if err != nil {
			t.Fatal(err)
		}
		return 0
	case <-time.After(10 * time.Second):
		t.Fatal("serial-run が 10 秒で終わらない")
		return -1
	}
}

func TestRunsOneAtATimeInTicketOrder(t *testing.T) {
	f := newFixture(t)
	ga, gb, gc := f.gate("a"), f.gate("b"), f.gate("c")
	a := f.held("A", ga, nil, false)
	waitFor(t, "A が始まる", func() bool { return f.hasLog("start A") })
	b := f.held("B", gb, nil, false)
	waitFor(t, "B の札", func() bool { return f.ticketCount() == 2 })
	c := f.held("C", gc, nil, false)
	waitFor(t, "C の札", func() bool { return f.ticketCount() == 3 })

	f.release(ga)
	waitFor(t, "B が始まる", func() bool { return f.hasLog("start B") })
	if f.hasLog("start C") {
		t.Fatalf("B が走っている間に C も始まった: %q", f.logText())
	}
	f.release(gb)
	waitFor(t, "C が始まる", func() bool { return f.hasLog("start C") })
	f.release(gc)
	for _, cmd := range []*exec.Cmd{a, b, c} {
		if rc := exitCode(t, cmd); rc != 0 {
			t.Fatalf("rc=%d", rc)
		}
	}
	want := "start A end A start B end B start C end C"
	if got := strings.Join(strings.Fields(f.logText()), " "); got != want {
		t.Fatalf("区間が重なったか順番が違う:\n got %s\nwant %s", got, want)
	}
	if n := f.ticketCount(); n != 0 {
		t.Fatalf("終わった後に札が %d 枚残っている", n)
	}
}

// 採番と札の公開の間に来た人が、先に走らない (seq の flock を札の rename まで持つ)
func TestLaterArrivalCannotOvertakeUnpublishedTicket(t *testing.T) {
	f := newFixture(t)
	pause := f.gate("pause")
	x := f.start([]string{"SERIAL_RUN_TEST_PAUSE_BEFORE_RENAME=" + pause}, false, "--", "/bin/sh", "-c", "echo X >> "+f.log)
	waitFor(t, "X が札を作りかけで止まる", func() bool {
		ents, _ := os.ReadDir(f.qdir())
		for _, e := range ents {
			if strings.HasPrefix(e.Name(), tmpPrefix) {
				return true
			}
		}
		return false
	})
	y := f.start(nil, false, "--", "/bin/sh", "-c", "echo Y >> "+f.log)
	// sleep-ok: negative: Y が先に走らないこと (起きないことの確認なので、待つべき成立条件が無い)
	time.Sleep(500 * time.Millisecond)
	if f.hasLog("Y") {
		t.Fatalf("札の公開を待たずに Y が先に走った: %q", f.logText())
	}
	f.release(pause)
	if rc := exitCode(t, x); rc != 0 {
		t.Fatalf("X rc=%d", rc)
	}
	if rc := exitCode(t, y); rc != 0 {
		t.Fatalf("Y rc=%d", rc)
	}
	if got := strings.Join(f.logLines(), " "); got != "X|Y" {
		t.Fatalf("順番: got %q want X|Y", got)
	}
}

// 持ち主がグループごと SIGKILL されたら、子も残らず、次の待ち手が入る
func TestKilledHolderGroupLetsNextRun(t *testing.T) {
	f := newFixture(t)
	ga, gb := f.gate("a"), f.gate("b")
	pidFile := filepath.Join(f.dir, "a.pid")
	a := f.start(nil, true, "--", "/bin/sh", "-c", "echo $$ > "+pidFile+"; echo start A >> "+f.log+"; read _ < "+ga)
	waitFor(t, "A が始まる", func() bool { return f.hasLog("start A") })
	b := f.held("B", gb, nil, false)
	waitFor(t, "B の札", func() bool { return f.ticketCount() == 2 })

	childPid, _ := strconv.Atoi(strings.TrimSpace(readFile(pidFile)))
	if err := syscall.Kill(-a.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = exitCode(t, a)
	waitFor(t, "B が始まる", func() bool { return f.hasLog("start B") })
	if childPid > 0 && syscall.Kill(childPid, 0) == nil && !zombie(childPid) {
		t.Fatalf("SIGKILL された持ち主の子 (pid %d) が lock の外で残っている", childPid)
	}
	f.release(gb)
	if rc := exitCode(t, b); rc != 0 {
		t.Fatalf("B rc=%d", rc)
	}
	if n := f.ticketCount(); n != 0 {
		t.Fatalf("死んだ持ち主の札が残っている: %d 枚", n)
	}
}

// 待っている間は、前の札の持ち主が動かない限り起きない (イベント駆動。ポーリングしない)
func TestWaitWakesOnlyWhenPredecessorReleases(t *testing.T) {
	f := newFixture(t)
	ga, gb := f.gate("a"), f.gate("b")
	wakes := filepath.Join(f.dir, "wakes")
	a := f.held("A", ga, nil, false)
	waitFor(t, "A が始まる", func() bool { return f.hasLog("start A") })
	b := f.held("B", gb, []string{"SERIAL_RUN_TEST_WAKE_LOG=" + wakes, "SERIAL_RUN_TEST_REPORT=50ms"}, false)
	waitFor(t, "B の札", func() bool { return f.ticketCount() == 2 })
	waitFor(t, "B の最初の見直し", func() bool { return countLines(wakes) >= 1 })
	// sleep-ok: negative: 前の持ち主が動かない間に B が起きないこと (表示の刻み 50ms の 20 回分。起きないことの確認)
	time.Sleep(time.Second)
	if n := countLines(wakes); n != 1 {
		t.Fatalf("前の持ち主が動かない間に %d 回起きた (ポーリングしている)", n)
	}
	f.release(ga)
	waitFor(t, "B が始まる", func() bool { return f.hasLog("start B") })
	if n := countLines(wakes); n != 2 {
		t.Fatalf("前の 1 本が終わって起きた回数: got %d want 2 (最初 + 終わったとき)", n)
	}
	f.release(gb)
	_ = exitCode(t, a)
	_ = exitCode(t, b)
}

// blocking の待ちが起こされなくても、保険の見直しで進む
func TestFallbackPollProceedsWhenBlockingWaitIsBroken(t *testing.T) {
	f := newFixture(t)
	ga, gb := f.gate("a"), f.gate("b")
	a := f.held("A", ga, nil, false)
	waitFor(t, "A が始まる", func() bool { return f.hasLog("start A") })
	b := f.held("B", gb, []string{"SERIAL_RUN_TEST_NO_BLOCK=1", "SERIAL_RUN_TEST_FALLBACK=200ms"}, false)
	waitFor(t, "B の札", func() bool { return f.ticketCount() == 2 })
	f.release(ga)
	waitFor(t, "B が保険の見直しで始まる", func() bool { return f.hasLog("start B") })
	f.release(gb)
	_ = exitCode(t, a)
	if rc := exitCode(t, b); rc != 0 {
		t.Fatalf("B rc=%d", rc)
	}
}

// 鎖の途中の待ち手が死んだら、その後ろはさらに前の札で眠り直す (前の持ち主より先に走らない)
func TestMiddleWaiterDeathKeepsOrder(t *testing.T) {
	f := newFixture(t)
	ga, gb, gc := f.gate("a"), f.gate("b"), f.gate("c")
	a := f.held("A", ga, nil, false)
	waitFor(t, "A が始まる", func() bool { return f.hasLog("start A") })
	b := f.held("B", gb, nil, true)
	waitFor(t, "B の札", func() bool { return f.ticketCount() == 2 })
	c := f.held("C", gc, nil, false)
	waitFor(t, "C の札", func() bool { return f.ticketCount() == 3 })

	_ = syscall.Kill(-b.Process.Pid, syscall.SIGKILL)
	_ = exitCode(t, b)
	waitFor(t, "B の札が消える", func() bool { return f.ticketCount() == 2 })
	if f.hasLog("start C") {
		t.Fatalf("A が走っている間に C が始まった: %q", f.logText())
	}
	f.release(ga)
	waitFor(t, "C が始まる", func() bool { return f.hasLog("start C") })
	f.release(gc)
	_ = exitCode(t, a)
	if rc := exitCode(t, c); rc != 0 {
		t.Fatalf("C rc=%d", rc)
	}
}

// 実行権を持っている serial-run の子から呼ばれた serial-run は待たない (待つとデッドロックする)
func TestNestedCallUnderHolderDoesNotWait(t *testing.T) {
	f := newFixture(t)
	inner := bin + " --max-wait 5 -- /bin/sh -c 'echo inner >> " + f.log + "'"
	outer := f.start(nil, false, "--", "/bin/sh", "-c", inner)
	if rc := exitCode(t, outer); rc != 0 {
		t.Fatalf("入れ子が待って上限で落ちた: rc=%d log=%q", rc, f.logText())
	}
	if !f.hasLog("inner") {
		t.Fatal("入れ子の子が走っていない")
	}
}

// 持ち主でない SERIAL_RUN_HELD (親が終わった後の孫など) では普通に並ぶ
func TestStaleHeldEnvStillQueues(t *testing.T) {
	f := newFixture(t)
	ga := f.gate("a")
	a := f.held("A", ga, nil, false)
	waitFor(t, "A が始まる", func() bool { return f.hasLog("start A") })
	stale := heldEnv + "=" + filepath.Join(f.state, "default") + ":999"
	b := f.start([]string{stale}, false, "--", "/bin/sh", "-c", "echo B >> "+f.log)
	waitFor(t, "B の札", func() bool { return f.ticketCount() == 2 })
	if f.hasLog("B") {
		t.Fatal("持ち主でない SERIAL_RUN_HELD で並ばずに走った")
	}
	f.release(ga)
	_ = exitCode(t, a)
	if rc := exitCode(t, b); rc != 0 || !f.hasLog("B") {
		t.Fatalf("B rc=%d", rc)
	}
}

func TestMaxWaitGivesUpWithoutRunning(t *testing.T) {
	f := newFixture(t)
	ga := f.gate("a")
	a := f.held("A", ga, nil, false)
	waitFor(t, "A が始まる", func() bool { return f.hasLog("start A") })
	b := f.start(nil, false, "--max-wait", "0.3", "--", "/bin/sh", "-c", "echo B >> "+f.log)
	if rc := exitCode(t, b); rc != rcTool {
		t.Fatalf("待ちの上限: rc=%d want %d", rc, rcTool)
	}
	if f.hasLog("B") {
		t.Fatal("上限を超えたのに子を起こした")
	}
	if n := f.ticketCount(); n != 1 {
		t.Fatalf("上限で抜けた待ち手の札が残っている: %d 枚", n)
	}
	f.release(ga)
	_ = exitCode(t, a)
}

// 開けない札 (権限) が前にあっても空回りしない (blocking の待ちが即座に起き返して列挙し直す形にしない)
func TestUnopenableTicketDoesNotSpin(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(f.qdir(), 0o700); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(f.qdir(), "000000000001"+ticketSuffix)
	if err := os.WriteFile(bad, nil, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o600) })
	wakes := filepath.Join(f.dir, "wakes")
	b := f.start([]string{"SERIAL_RUN_TEST_WAKE_LOG=" + wakes}, false, "--max-wait", "1", "--", "/bin/sh", "-c", "echo B >> "+f.log)
	if rc := exitCode(t, b); rc != rcTool {
		t.Fatalf("開けない札の後ろで上限まで待たずに抜けた: rc=%d", rc)
	}
	if f.hasLog("B") {
		t.Fatal("生死を決められない札を飛ばして走った")
	}
	// 見直しの既定は 5 秒なので、1 秒の待ちで起きるのは最初の 1 回だけ (空回りなら数万回)
	if n := countLines(wakes); n > 3 {
		t.Fatalf("開けない札の後ろで %d 回起きた (空回り)", n)
	}
}

// 持ち主の serial-run だけが SIGKILL されても、子が走っている間は次の待ち手が入らない
func TestOrphanedChildKeepsTheTurn(t *testing.T) {
	f := newFixture(t)
	ga, gb := f.gate("a"), f.gate("b")
	a := f.held("A", ga, nil, false)
	waitFor(t, "A が始まり、札に子の pid が載る", func() bool {
		return f.hasLog("start A") && strings.Contains(readFile(filepath.Join(f.qdir(), "000000000001"+ticketSuffix)), "child_pid")
	})
	b := f.held("B", gb, []string{"SERIAL_RUN_TEST_RECHECK=100ms"}, false)
	waitFor(t, "B の札", func() bool { return f.ticketCount() == 2 })
	if err := a.Process.Signal(syscall.SIGKILL); err != nil { // 子は殺さない (pid 宛ての KILL)
		t.Fatal(err)
	}
	_ = exitCode(t, a)
	// sleep-ok: negative: A の子が走っている間に B が入らないこと (見直しの刻み 100ms の 5 回分)
	time.Sleep(500 * time.Millisecond)
	if f.hasLog("start B") {
		t.Fatalf("持ち主だけが死んだ後、A の子が走っている間に B が始まった: %q", f.logText())
	}
	f.release(ga)
	waitFor(t, "A の子が終わってから B が始まる", func() bool { return f.hasLog("start B") })
	f.release(gb)
	if rc := exitCode(t, b); rc != 0 {
		t.Fatalf("B rc=%d", rc)
	}
	if got := strings.Join(strings.Fields(f.logText()), " "); got != "start A end A start B end B" {
		t.Fatalf("区間が重なった: %s", got)
	}
}

// 走っている持ち主の札が外から消されても、run.lock が 2 本目を止める
func TestRunLockExcludesEvenIfHolderTicketIsRemoved(t *testing.T) {
	f := newFixture(t)
	ga, gb := f.gate("a"), f.gate("b")
	a := f.held("A", ga, nil, false)
	waitFor(t, "A が始まる", func() bool { return f.hasLog("start A") })
	b := f.held("B", gb, []string{"SERIAL_RUN_TEST_FALLBACK=100ms"}, false)
	waitFor(t, "B の札", func() bool { return f.ticketCount() == 2 })
	if err := os.Remove(filepath.Join(f.qdir(), "000000000001"+ticketSuffix)); err != nil {
		t.Fatal(err)
	}
	// sleep-ok: negative: 札が消えても B が入らないこと (保険の見直し 100ms の 5 回分。B は前の札が無いと読んで run.lock を待つ)
	time.Sleep(500 * time.Millisecond)
	if f.hasLog("start B") {
		t.Fatal("持ち主の札が消えただけで、A が走っている間に B が始まった")
	}
	f.release(ga)
	waitFor(t, "B が始まる", func() bool { return f.hasLog("start B") })
	f.release(gb)
	_ = exitCode(t, a)
	_ = exitCode(t, b)
}

// グループへの SIGQUIT で、rc が Go のダンプの 2 に化けない
func TestQuitToGroupKeepsSignalRC(t *testing.T) {
	f := newFixture(t)
	g := f.gate("g")
	pidFile := filepath.Join(f.dir, "child.pid")
	cmd := f.start(nil, true, "--", "/bin/sh", "-c", "echo $$ > "+pidFile+"; read _ < "+g)
	waitFor(t, "子の pid", func() bool { return strings.HasSuffix(readFile(pidFile), "\n") })
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGQUIT); err != nil {
		t.Fatal(err)
	}
	if rc := exitCode(t, cmd); rc != 128+int(syscall.SIGQUIT) {
		t.Fatalf("QUIT の後の rc=%d want %d", rc, 128+int(syscall.SIGQUIT))
	}
}

// 待ちの最初の 1 行にだけ、外から様子を見る口 (serial-run status) を添える
func TestFirstWaitLineShowsStatusHint(t *testing.T) {
	f := newFixture(t)
	ga := f.gate("a")
	a := f.held("A", ga, nil, false)
	waitFor(t, "A が始まる", func() bool { return f.hasLog("start A") })
	errPath := filepath.Join(f.dir, "b.err")
	ef, err := os.Create(errPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ef.Close()
	b := exec.Command(bin, "--", "/bin/sh", "-c", "echo B >> "+f.log)
	b.Env = f.env("SERIAL_RUN_TEST_REPORT=50ms")
	b.Stderr = ef
	if err := b.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "B の定期の待ちの行が 2 行以上出る", func() bool { return strings.Count(readFile(errPath), "serial-run: 待ち 1 番目") >= 2 })
	f.release(ga)
	_ = exitCode(t, a)
	if rc := exitCode(t, b); rc != 0 {
		t.Fatalf("B rc=%d", rc)
	}
	s := readFile(errPath)
	if n := strings.Count(s, "serial-run status"); n != 1 {
		t.Fatalf("案内が %d 回出た (最初の 1 行だけのはず):\n%s", n, s)
	}
	if !strings.Contains(strings.SplitN(s, "\n", 2)[0], "serial-run status") {
		t.Fatalf("最初の行に案内が無い:\n%s", s)
	}
}

func TestExitCodes(t *testing.T) {
	f := newFixture(t)
	cases := []struct {
		args []string
		want int
	}{
		{[]string{"--", "/bin/sh", "-c", "exit 3"}, 3},
		{[]string{"--", "/nonexistent/cmd"}, rcNotFound},
		{[]string{"--", "/bin/sh", "-c", "kill -TERM $$"}, 128 + int(syscall.SIGTERM)},
		{[]string{"--bogus"}, rcTool},
		{[]string{"--"}, rcTool},
	}
	for _, c := range cases {
		if rc := exitCode(t, f.start(nil, false, c.args...)); rc != c.want {
			t.Errorf("%v: rc=%d want %d", c.args, rc, c.want)
		}
	}
}

// lock と札の fd は子へ渡らない (渡ると子が残した背景プロセスが lock を握ったまま残る)
func TestLockFdsAreNotInherited(t *testing.T) {
	f := newFixture(t)
	out := filepath.Join(f.dir, "lsof")
	cmd := f.start(nil, false, "--", "/bin/sh", "-c", "/usr/sbin/lsof -p $$ -Fn > "+out)
	if rc := exitCode(t, cmd); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	s := readFile(out)
	if !strings.Contains(s, "n/") {
		t.Fatalf("lsof の出力が読めない (較正): %q", s)
	}
	if strings.Contains(s, f.state) {
		t.Fatalf("子が状態の置き場の fd を持っている:\n%s", s)
	}
}

// seq が壊れていても、残っている札より大きい番号から続ける
func TestSeqRecoversFromCorruptionAboveExistingTickets(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(f.qdir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.state, "default", "seq"), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 持ち主のいない札 (持ち主が死んだ残り)
	if err := os.WriteFile(filepath.Join(f.qdir(), "000000000007"+ticketSuffix), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if rc := exitCode(t, f.start(nil, false, "--", "/usr/bin/true")); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	hist := readFile(filepath.Join(f.state, "default", "history.log"))
	if !strings.HasPrefix(hist, "8\t") {
		t.Fatalf("番号が残っている札の後ろから続いていない: %q", hist)
	}
	if n := f.ticketCount(); n != 0 {
		t.Fatalf("持ち主のいない札が回収されていない: %d 枚", n)
	}
}

// 実行中の serial-run への SIGTERM は子孫ごと止め、札を残さない
func TestTermStopsChildAndLeavesNoTicket(t *testing.T) {
	f := newFixture(t)
	g := f.gate("g")
	pidFile := filepath.Join(f.dir, "child.pid")
	cmd := f.start(nil, false, "--", "/bin/sh", "-c", "echo $$ > "+pidFile+"; read _ < "+g)
	waitFor(t, "子の pid", func() bool { return strings.HasSuffix(readFile(pidFile), "\n") })
	child, _ := strconv.Atoi(strings.TrimSpace(readFile(pidFile)))
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	_ = exitCode(t, cmd)
	if syscall.Kill(child, 0) == nil && !zombie(child) {
		t.Fatalf("SIGTERM の後に子 (pid %d) が残っている", child)
	}
	if n := f.ticketCount(); n != 0 {
		t.Fatalf("札が残っている: %d 枚", n)
	}
}

func TestDisplayCmdShowsWrappedCommand(t *testing.T) {
	if got := displayCmd([]string{"/bin/zsh", "-c", "make  test\n"}); got != "make test" {
		t.Fatalf("got %q", got)
	}
	if got := displayCmd([]string{"go", "test", "./..."}); got != "go test ./..." {
		t.Fatalf("got %q", got)
	}
}

func readFile(p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}

func countLines(p string) int {
	return strings.Count(readFile(p), "\n")
}

func zombie(pid int) bool {
	out, err := exec.Command("/bin/ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return true // ps に出ない = もう居ない
	}
	return strings.HasPrefix(strings.TrimSpace(string(out)), "Z")
}
