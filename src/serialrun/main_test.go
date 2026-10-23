package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
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
	if out, err := exec.Command("go", "build", "-tags", "serialrun_testseam", "-o", bin, ".").CombinedOutput(); err != nil {
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

// env はテストの子に渡す環境。外側の make test が serial-run に包まれていると SERIAL_RUN_HELD を継承しているので外す。
// 🚨 置き場は SERIAL_RUN_STATE_DIR だけでなく HOME と XDG_STATE_HOME も一時 dir に向ける。置き場の決め方を壊す変異 (変異検証) や
// 退行で SERIAL_RUN_STATE_DIR が読まれなくなっても、本物の待ち列 (~/.local/state/serial-run) に入って他の session を止めないため
func (f *fixture) env(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "SERIAL_RUN_") || strings.HasPrefix(kv, "HOME=") || strings.HasPrefix(kv, "XDG_STATE_HOME=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "SERIAL_RUN_STATE_DIR="+f.state, "HOME="+f.dir, "XDG_STATE_HOME="+filepath.Join(f.dir, "xdg"))
	return append(env, extra...)
}

func (f *fixture) qdir() string { return filepath.Join(f.state, "default", "queue") }

// gate は子が読むまで止まる fifo を作る
func (f *fixture) gate(name string) string {
	f.t.Helper()
	p := filepath.Join(f.dir, name+".fifo")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		f.t.Fatal(err)
	}
	// テストが途中で落ちても、gate を読んで止まっている子の sh を進めて終わらせる (残すと PPID=1 の孤児として残り続ける)。
	// 読み手がいなければ O_NONBLOCK の open は ENXIO で失敗するだけ
	f.t.Cleanup(func() {
		if w, err := os.OpenFile(p, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_, _ = w.WriteString("\n")
			_ = w.Close()
		}
	})
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
			if setpgid {
				// 専用のグループで起こしたものは、子孫ごと止める (テストが途中で落ちると、ループで待つ子が孤児で残る)
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
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
	stale := heldEnv + "=" + heldToken(filepath.Join(f.state, "default"), 999, "x")
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

func firstTicket(f *fixture) string { return filepath.Join(f.qdir(), "000000000001"+ticketSuffix) }

// waitChildRecorded は A の札に子の pid と開始時刻が載るまで待つ
func waitChildRecorded(t *testing.T, f *fixture) {
	t.Helper()
	waitFor(t, "札に子の pid と開始時刻が載る", func() bool {
		s := readFile(firstTicket(f))
		return strings.Contains(s, "child_pid") && strings.Contains(s, "child_start")
	})
}

// 札を書いた serial-run と読む serial-run のロケール・タイムゾーンが違っても、孤児の子を同じ子と読む (ps の lstart の書式が変わる)
func TestOrphanSurvivesLocaleAndTimezoneDifference(t *testing.T) {
	f := newFixture(t)
	ga, gb := f.gate("a"), f.gate("b")
	a := f.held("A", ga, []string{"LC_ALL=C", "TZ=UTC"}, false)
	waitFor(t, "A が始まる", func() bool { return f.hasLog("start A") })
	waitChildRecorded(t, f)
	b := f.held("B", gb, []string{"LC_ALL=ja_JP.UTF-8", "TZ=Asia/Tokyo", "SERIAL_RUN_TEST_RECHECK=100ms"}, false)
	waitFor(t, "B の札", func() bool { return f.ticketCount() == 2 })
	if err := a.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = exitCode(t, a)
	// sleep-ok: negative: A の子が走っている間に B が入らないこと (見直しの刻み 100ms の 5 回分)
	time.Sleep(500 * time.Millisecond)
	if f.hasLog("start B") {
		t.Fatal("ロケールとタイムゾーンが違うだけで、走っている孤児の子を死んだと読んだ")
	}
	f.release(ga)
	waitFor(t, "A の子が終わってから B", func() bool { return f.hasLog("start B") })
	f.release(gb)
	_ = exitCode(t, b)
}

// ps が使えないときは、孤児の札を消さずに待つ (「子はもう居ない」と読まない)
func TestPsFailureKeepsOrphanTicket(t *testing.T) {
	f := newFixture(t)
	ga := f.gate("a")
	a := f.held("A", ga, nil, false)
	waitFor(t, "A が始まる", func() bool { return f.hasLog("start A") })
	waitChildRecorded(t, f)
	if err := a.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = exitCode(t, a)
	b := f.start([]string{"SERIAL_RUN_TEST_PS=/nonexistent/ps", "SERIAL_RUN_TEST_RECHECK=100ms"}, false,
		"--max-wait", "1", "--", "/bin/sh", "-c", "echo start B >> "+f.log)
	if rc := exitCode(t, b); rc != rcTool {
		t.Fatalf("ps が使えないのに待ちの上限まで待たなかった: rc=%d", rc)
	}
	if f.hasLog("start B") {
		t.Fatal("ps が使えないのに、走っている孤児の子の後ろで B が走った")
	}
	if _, err := os.Stat(firstTicket(f)); err != nil {
		t.Fatalf("孤児の札を消した: %v", err)
	}
	f.release(ga)
}

// 持ち主が死んで、子が停止 (SIGSTOP) のまま残った札は、後ろの待ち手が子孫ごと回収して進む
func TestStoppedOrphanIsReclaimed(t *testing.T) {
	f := newFixture(t)
	ga := f.gate("a")
	pidFile := filepath.Join(f.dir, "a.pid")
	a := f.start(nil, false, "--", "/bin/sh", "-c", "echo $$ > "+pidFile+"; read _ < "+ga)
	waitFor(t, "子の pid", func() bool { return strings.HasSuffix(readFile(pidFile), "\n") })
	waitChildRecorded(t, f)
	child, _ := strconv.Atoi(strings.TrimSpace(readFile(pidFile)))
	if err := syscall.Kill(child, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })
	if err := a.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = exitCode(t, a)
	b := f.start([]string{"SERIAL_RUN_TEST_RECHECK=100ms"}, false, "--max-wait", "5", "--", "/bin/sh", "-c", "echo B >> "+f.log)
	if rc := exitCode(t, b); rc != 0 || !f.hasLog("B") {
		t.Fatalf("停止したままの孤児の後ろで進めなかった: rc=%d", rc)
	}
	waitFor(t, "停止していた子が止められる", func() bool { return syscall.Kill(child, 0) != nil || zombie(child) })
}

// 子の開始時刻を記録できなかった札でも、子が停止したまま持ち主が死んだら、後ろが回収して進む (待ちの上限まで止まらない)
func TestStoppedOrphanWithoutStartIsReclaimed(t *testing.T) {
	f := newFixture(t)
	ga := f.gate("a")
	pidFile := filepath.Join(f.dir, "a.pid")
	errPath := filepath.Join(f.dir, "a.err")
	ef, err := os.Create(errPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ef.Close()
	a := exec.Command(bin, "--", "/bin/sh", "-c", "echo $$ > "+pidFile+"; read _ < "+ga)
	a.Env = f.env("SERIAL_RUN_TEST_PS=/nonexistent/ps")
	a.Stderr = ef
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "子の pid", func() bool { return strings.HasSuffix(readFile(pidFile), "\n") })
	waitFor(t, "札に子の pid が載り、開始時刻を書けなかった警告が出る", func() bool {
		return strings.Contains(readFile(firstTicket(f)), "child_pid") && strings.Contains(readFile(errPath), "子の情報を書けない")
	})
	if strings.Contains(readFile(firstTicket(f)), "child_start") {
		t.Fatal("前提が崩れた: ps が使えないのに開始時刻が書かれた")
	}
	child, _ := strconv.Atoi(strings.TrimSpace(readFile(pidFile)))
	if err := syscall.Kill(child, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })
	if err := a.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = exitCode(t, a)
	b := f.start([]string{"SERIAL_RUN_TEST_RECHECK=100ms"}, false, "--max-wait", "5", "--", "/bin/sh", "-c", "echo B >> "+f.log)
	if rc := exitCode(t, b); rc != 0 || !f.hasLog("B") {
		t.Fatalf("開始時刻の無い停止した孤児の後ろで進めなかった: rc=%d", rc)
	}
	// 「別人」と読んで札だけ消したのではなく、子として回収した (停止したままの子を残さない)
	waitFor(t, "停止していた子が止められる", func() bool { return syscall.Kill(child, 0) != nil || zombie(child) })
}

// 先頭が孤児 (持ち主の serial-run だけが死んだ) のとき、その子の子孫から SERIAL_RUN_HELD を落として呼んでも素通しする
func TestNestedWithoutHeldUnderOrphanFindsAncestor(t *testing.T) {
	f := newFixture(t)
	ga := f.gate("a")
	inner := "env -u SERIAL_RUN_HELD " + bin + " --max-wait 3 -- /bin/sh -c 'echo inner >> " + f.log + "'"
	a := f.start(nil, false, "--", "/bin/sh", "-c", "echo start >> "+f.log+"; read _ < "+ga+"; "+inner)
	waitFor(t, "外側が始まる", func() bool { return f.hasLog("start") })
	waitChildRecorded(t, f)
	if err := a.Process.Signal(syscall.SIGKILL); err != nil { // 外側の serial-run だけを止める (子は残る)
		t.Fatal(err)
	}
	_ = exitCode(t, a)
	f.release(ga)
	waitFor(t, "孤児の下の入れ子が素通しで走る", func() bool { return f.hasLog("inner") })
}

// lstart (LC_ALL=C TZ=UTC) を読める。読み違えると、自分の子を「札より前から居た別人」と読んで回収する
func TestStartedAfterReadsRealLstart(t *testing.T) {
	before := time.Now().Add(-time.Hour)
	s, known := procStart(os.Getpid())
	if !known || s == "" {
		t.Fatalf("自分の開始時刻を ps で取れない: %q", s)
	}
	if !startedAfter(s, before) {
		t.Fatalf("1 時間前より後に起きた自分を、後と読めない: lstart=%q", s)
	}
	if startedAfter(s, time.Now().Add(time.Hour)) {
		t.Fatalf("1 時間後より前に起きた自分を、後と読んだ: lstart=%q", s)
	}
}

// writeDeadTicket は持ち主が死んだ札 (flock なし) を 1 番として置く。子は別に起こした child
func writeDeadTicket(t *testing.T, f *fixture, info ticketInfo) {
	t.Helper()
	if err := os.MkdirAll(f.qdir(), 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(firstTicket(f), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// startSleeper は停止していない長命の子を起こす (テストの終わりに止める)
func startSleeper(t *testing.T) int {
	t.Helper()
	c := exec.Command("/bin/sleep", "30")
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
	return c.Process.Pid
}

// ロケールを固定していなかった版が書いた札 (開始時刻が ja_JP の書式) でも、走っている子を別人と読まない (2 本走らない)
func TestOldFormatChildStartIsStillTheSameChild(t *testing.T) {
	f := newFixture(t)
	start := time.Now().Add(-5 * time.Second)
	child := startSleeper(t)
	out, err := exec.Command("/usr/bin/env", "LC_ALL=ja_JP.UTF-8", "TZ=Asia/Tokyo", "/bin/ps", "-o", "lstart=", "-p", strconv.Itoa(child)).Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		t.Fatalf("古い書式の lstart を作れない: %v %q", err, out)
	}
	if _, ok := parseLstart(strings.TrimSpace(string(out))); ok {
		t.Fatalf("前提が崩れた: 古い書式が新しい書式として読めてしまう: %q", out)
	}
	writeDeadTicket(t, f, ticketInfo{Pid: 999999, Start: start, ChildPid: child, ChildStart: strings.TrimSpace(string(out))})
	b := f.start(nil, false, "--max-wait", "1", "--", "/bin/sh", "-c", "echo B >> "+f.log)
	if rc := exitCode(t, b); rc != rcTool || f.hasLog("B") {
		t.Fatalf("古い書式の札の子が走っているのに B が走った: rc=%d", rc)
	}
	if _, err := os.Stat(firstTicket(f)); err != nil {
		t.Fatalf("走っている子の札を消した: %v", err)
	}
}

// 札の child_start は持ち主の環境のままの書式で書く (版が混ざる間、古い版はこれを自分の環境の ps と文字列で比べる)
func TestTicketKeepsLegacyChildStartForOldVersions(t *testing.T) {
	f := newFixture(t)
	ga := f.gate("a")
	pidFile := filepath.Join(f.dir, "a.pid")
	a := f.start([]string{"LC_ALL=ja_JP.UTF-8", "TZ=Asia/Tokyo"}, false, "--", "/bin/sh", "-c", "echo $$ > "+pidFile+"; read _ < "+ga)
	waitFor(t, "子の pid", func() bool { return strings.HasSuffix(readFile(pidFile), "\n") })
	waitFor(t, "札に child_start_utc が載る", func() bool { return strings.Contains(readFile(firstTicket(f)), "child_start_utc") })
	child, _ := strconv.Atoi(strings.TrimSpace(readFile(pidFile)))
	out, err := exec.Command("/usr/bin/env", "LC_ALL=ja_JP.UTF-8", "TZ=Asia/Tokyo", "/bin/ps", "-o", "lstart=", "-p", strconv.Itoa(child)).Output()
	if err != nil {
		t.Fatal(err)
	}
	var info ticketInfo
	if err := json.Unmarshal([]byte(readFile(firstTicket(f))), &info); err != nil {
		t.Fatal(err)
	}
	if info.ChildStart != strings.TrimSpace(string(out)) {
		t.Fatalf("child_start が持ち主の環境の書式でない: %q want %q", info.ChildStart, strings.TrimSpace(string(out)))
	}
	f.release(ga)
	_ = exitCode(t, a)
}

// 子の同一性を開始時刻で確かめられない孤児の札は、寿命を過ぎたら消す (使い回された pid の長命なプロセスを待ち続けない)
func TestUnverifiedOrphanExpires(t *testing.T) {
	f := newFixture(t)
	other := startSleeper(t)
	writeDeadTicket(t, f, ticketInfo{Pid: 999999, Start: time.Now().Add(-unverifiedOrphanTTL - time.Minute), ChildPid: other})
	b := f.start(nil, false, "--max-wait", "3", "--", "/bin/sh", "-c", "echo B >> "+f.log)
	if rc := exitCode(t, b); rc != 0 || !f.hasLog("B") {
		t.Fatalf("寿命を過ぎた確かめられない孤児の札の後ろで待った: rc=%d", rc)
	}
	if syscall.Kill(other, 0) != nil {
		t.Fatal("寿命で札を消しただけのはずが、その pid のプロセスを止めた")
	}
}

// LANG の無い環境で古い版が書いた札 (C の書式でローカル時刻) は、時刻として読めても UTC として比べない (9 時間ずれて別人と読む)
func TestOldVersionCLocaleChildStartIsNotComparedAsUTC(t *testing.T) {
	f := newFixture(t)
	start := time.Now().Add(-5 * time.Second)
	child := startSleeper(t)
	out, err := exec.Command("/usr/bin/env", "-u", "LANG", "-u", "LC_ALL", "-u", "LC_TIME", "TZ=Asia/Tokyo", "/bin/ps", "-o", "lstart=", "-p", strconv.Itoa(child)).Output()
	if err != nil {
		t.Fatal(err)
	}
	old := strings.TrimSpace(string(out))
	if _, ok := parseLstart(old); !ok {
		t.Fatalf("前提が崩れた: C の書式の lstart を読めない: %q", old)
	}
	writeDeadTicket(t, f, ticketInfo{Pid: 999999, Start: start, ChildPid: child, ChildStart: old}) // V が無い = 古い版
	b := f.start(nil, false, "--max-wait", "1", "--", "/bin/sh", "-c", "echo B >> "+f.log)
	if rc := exitCode(t, b); rc != rcTool || f.hasLog("B") {
		t.Fatalf("古い版の札の子が走っているのに B が走った: rc=%d", rc)
	}
}

// 開始時刻の無い札の pid が、使い回された無関係の停止中のプロセスなら止めない (持ち主のプロセスグループと合わない)
func TestUnrelatedStoppedProcessIsNotReclaimed(t *testing.T) {
	f := newFixture(t)
	start := time.Now().Add(-5 * time.Second)
	other := startSleeper(t)
	if err := syscall.Kill(other, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	writeDeadTicket(t, f, ticketInfo{Pid: 999999, Start: start, ChildPid: other, Pgid: 1})
	b := f.start(nil, false, "--max-wait", "1", "--", "/bin/sh", "-c", "echo B >> "+f.log)
	_ = exitCode(t, b)
	if syscall.Kill(other, 0) != nil || zombie(other) {
		t.Fatal("持ち主のグループと合わない、使い回された pid の停止中のプロセスを止めた")
	}
}

// 大文字・先頭の . を含む lock 名は通さない (同じ置き場を別名で指す / 親の置き場の seq を上書きする)
func TestLockNameRejectsCaseAndDots(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"Default", ".", "..", ".x"} {
		if rc := exitCode(t, f.start(nil, false, "-l", name, "--", "/usr/bin/true")); rc != rcTool {
			t.Errorf("-l %q: rc=%d want %d", name, rc, rcTool)
		}
	}
}

// 子が同じ置き場を symlink 越しに指しても、入れ子として素通しする
func TestNestedThroughSymlinkedStateDirDoesNotWait(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(f.state, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(f.dir, "state-link")
	if err := os.Symlink(f.state, link); err != nil {
		t.Fatal(err)
	}
	inner := "SERIAL_RUN_STATE_DIR=" + link + " " + bin + " --max-wait 3 -- /bin/sh -c 'echo inner >> " + f.log + "'"
	if rc := exitCode(t, f.start(nil, false, "--", "/bin/sh", "-c", inner)); rc != 0 || !f.hasLog("inner") {
		t.Fatalf("symlink 越しの入れ子が待った: rc=%d", rc)
	}
}

// 同じ置き場を別の綴り (symlink 越し) で開いても、SERIAL_RUN_HELD の照合が一致する (祖先を辿る保険とは別の層)
func TestHeldMatchesAcrossSymlinkSpellings(t *testing.T) {
	d := t.TempDir()
	real := filepath.Join(d, "real")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(d, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	q1, err := openQueue(filepath.Join(real, "default"))
	if err != nil {
		t.Fatal(err)
	}
	// この test が札の flock を持つ (別の fd から見ると生きている札)
	tk, err := q1.enqueue(ticketInfo{Pid: os.Getpid(), Start: time.Now(), Nonce: "n1"}, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer tk.release(nil)
	seq := tk.seq
	q2, err := openQueue(filepath.Join(link, "default"))
	if err != nil {
		t.Fatal(err)
	}
	if !q2.heldByAncestor(heldToken(filepath.Join(real, "default"), seq, "n1")) {
		t.Fatal("symlink 越しに開いた置き場で、実パスの SERIAL_RUN_HELD を持ち主と読めない")
	}
	if !q1.heldByAncestor(heldToken(filepath.Join(link, "default"), seq, "n1")) {
		t.Fatal("実パスで開いた置き場で、symlink 越しの SERIAL_RUN_HELD を持ち主と読めない")
	}
}

// 子が SERIAL_RUN_HELD を落としても、先頭の札の持ち主が祖先なら入れ子として素通しする (待つと自分の親を待って止まる)
func TestNestedWithoutHeldEnvFindsAncestor(t *testing.T) {
	f := newFixture(t)
	inner := "env -u SERIAL_RUN_HELD " + bin + " --max-wait 3 -- /bin/sh -c 'echo inner >> " + f.log + "'"
	if rc := exitCode(t, f.start(nil, false, "--", "/bin/sh", "-c", inner)); rc != 0 || !f.hasLog("inner") {
		t.Fatalf("SERIAL_RUN_HELD を落とした入れ子が待った: rc=%d", rc)
	}
}

// 番号が合っていても nonce が違う SERIAL_RUN_HELD (置き場が作り直されて番号が使い回された後の古い値) では並ぶ
func TestHeldWithWrongNonceQueues(t *testing.T) {
	f := newFixture(t)
	ga := f.gate("a")
	a := f.held("A", ga, nil, false)
	waitFor(t, "A が始まる", func() bool { return f.hasLog("start A") })
	stale := heldEnv + "=" + heldToken(filepath.Join(f.state, "default"), 1, "not-the-nonce")
	b := f.start([]string{stale}, false, "--", "/bin/sh", "-c", "echo B >> "+f.log)
	waitFor(t, "B の札", func() bool { return f.ticketCount() == 2 })
	if f.hasLog("B") {
		t.Fatal("nonce の違う SERIAL_RUN_HELD で並ばずに走った")
	}
	f.release(ga)
	_ = exitCode(t, a)
	if rc := exitCode(t, b); rc != 0 {
		t.Fatalf("B rc=%d", rc)
	}
}

// 採番の lock が空かない間も、シグナルで抜けられる (blocking の flock で止まらない)
func TestSignalWhileWaitingForSeqLock(t *testing.T) {
	f := newFixture(t)
	pause := f.gate("pause")
	x := f.start([]string{"SERIAL_RUN_TEST_PAUSE_BEFORE_RENAME=" + pause}, false, "--", "/usr/bin/true")
	waitFor(t, "X が採番の lock を持ったまま止まる", func() bool {
		ents, _ := os.ReadDir(f.qdir())
		for _, e := range ents {
			if strings.HasPrefix(e.Name(), tmpPrefix) {
				return true
			}
		}
		return false
	})
	retries := filepath.Join(f.dir, "seq-retries")
	y := f.start([]string{"SERIAL_RUN_TEST_SEQ_RETRY_LOG=" + retries}, false, "--", "/usr/bin/true")
	// 再試行の中に居ることを印で確かめてから撃つ (入る前に撃つと、Notify の前の既定の動作で死んでも同じ rc になり、何も確かめない)
	waitFor(t, "Y が採番の lock の再試行に入る", func() bool { return countLines(retries) >= 1 })
	if err := y.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if rc := exitCode(t, y); rc != 128+int(syscall.SIGTERM) {
		t.Fatalf("採番の lock を待つ間の TERM: rc=%d", rc)
	}
	f.release(pause)
	_ = exitCode(t, x)
}

// 相対の XDG_STATE_HOME は使わない (cwd ごとに別の待ち行列ができる)。HOME の下の既定に落ちる
func TestRelativeXDGStateHomeIsIgnored(t *testing.T) {
	f := newFixture(t)
	env := []string{}
	for _, kv := range f.env() {
		if !strings.HasPrefix(kv, "SERIAL_RUN_STATE_DIR=") && !strings.HasPrefix(kv, "XDG_STATE_HOME=") {
			env = append(env, kv)
		}
	}
	cmd := exec.Command(bin, "--", "/usr/bin/true")
	cmd.Env = append(env, "XDG_STATE_HOME=rel")
	cmd.Dir = f.dir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(queue{dir: filepath.Join(f.dir, ".local", "state", "serial-run", "default")}.historyPath()); err != nil {
		t.Fatalf("既定の置き場に落ちていない: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "rel")); err == nil {
		t.Fatal("相対の XDG_STATE_HOME を cwd の下に作った")
	}
}

// 待ち列を使えない (置き場を作れない) ときは、警告して順番を待たずに走らせる (包んだコマンドを走らせないのは害が大きい)
func TestUnusableStateDirRunsUnqueued(t *testing.T) {
	f := newFixture(t)
	errPath := filepath.Join(f.dir, "err")
	ef, err := os.Create(errPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ef.Close()
	cmd := exec.Command(bin, "--", "/bin/sh", "-c", "echo RAN >> "+f.log)
	cmd.Env = append(f.env(), "SERIAL_RUN_STATE_DIR=/dev/null/serial-run")
	cmd.Stderr = ef
	if err := cmd.Run(); err != nil {
		t.Fatalf("待ち列を使えないときに子の rc で返らない: %v", err)
	}
	if !f.hasLog("RAN") || !strings.Contains(readFile(errPath), "順番を待たずに実行する") {
		t.Fatalf("警告して走らせていない: log=%q err=%q", f.logText(), readFile(errPath))
	}
	if rc := exitCode(t, f.start([]string{"SERIAL_RUN_STATE_DIR=/dev/null/serial-run"}, false, "status")); rc != rcTool {
		t.Fatalf("status は待ち列を使えないと 125: rc=%d", rc)
	}
}

// run.lock を開けないときも、警告して順番を待たずに走らせる (札は残さない)
func TestUnopenableRunLockRunsUnqueued(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(f.qdir(), 0o700); err != nil {
		t.Fatal(err)
	}
	rl := filepath.Join(f.state, "default", "run.lock")
	if err := os.WriteFile(rl, nil, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(rl, 0o600) })
	if rc := exitCode(t, f.start(nil, false, "--", "/bin/sh", "-c", "echo RAN >> "+f.log)); rc != 0 || !f.hasLog("RAN") {
		t.Fatalf("run.lock を開けないときに走らせていない: rc=%d", rc)
	}
	if n := f.ticketCount(); n != 0 {
		t.Fatalf("待たずに走った後に札が残っている: %d 枚", n)
	}
}

// 採番できない (seq も札の一覧も読めない) ときも、警告して順番を待たずに走らせる
func TestUnreadableQueueRunsUnqueued(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(f.qdir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.state, "default", "seq"), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(f.qdir(), 0o300); err != nil { // 書けるが一覧を読めない
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.qdir(), 0o700) })
	if rc := exitCode(t, f.start(nil, false, "--", "/bin/sh", "-c", "echo RAN >> "+f.log)); rc != 0 || !f.hasLog("RAN") {
		t.Fatalf("採番できないときに走らせていない: rc=%d", rc)
	}
}

// 起動時に無視されていた INT は、無視のまま子へ渡す (serial-run を挟んでも子の受け方を変えない)
func TestIgnoredIntStaysIgnoredForChild(t *testing.T) {
	f := newFixture(t)
	// 外側は素の sh (serial-run 越しにすると、内側が入れ子の素通しになり runQueued の Notify を通らない)
	script := "trap '' INT; " + bin + " -- /bin/sh -c 'kill -INT $$; echo survived >> " + f.log + "'"
	sh := exec.Command("/bin/sh", "-c", script)
	sh.Env = f.env()
	if err := sh.Run(); err != nil {
		t.Fatalf("rc: %v", err)
	}
	if !f.hasLog("survived") {
		t.Fatal("無視されていた INT で子が死んだ")
	}
}

// serial-run の pid だけに送った INT でも、猶予の後に子孫ごと止める (止められない serial-run にしない)
func TestIntToSerialRunPidStopsChildAfterGrace(t *testing.T) {
	f := newFixture(t)
	g := f.gate("g")
	pidFile := filepath.Join(f.dir, "child.pid")
	cmd := f.start([]string{"SERIAL_RUN_TEST_INT_GRACE=1s"}, true, "--", "/bin/sh", "-c", "echo $$ > "+pidFile+"; read _ < "+g)
	waitFor(t, "子の pid", func() bool { return strings.HasSuffix(readFile(pidFile), "\n") })
	child, _ := strconv.Atoi(strings.TrimSpace(readFile(pidFile)))
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	if rc := exitCode(t, cmd); rc != 128+int(syscall.SIGINT) {
		t.Fatalf("pid 宛ての INT で止まらない: rc=%d", rc)
	}
	if syscall.Kill(child, 0) == nil && !zombie(child) {
		t.Fatal("猶予の後も子が残っている")
	}
}

// グループ宛ての INT (runtimeout・CI) は子にも直接届く。serial-run は伝え直さない (2 回届けない)
func TestGroupIntIsNotForwardedTwice(t *testing.T) {
	f := newFixture(t)
	g := f.gate("g")
	counts := filepath.Join(f.dir, "ints")
	// 子は INT を受けるたびに 1 行書き、待ち続ける (INT で抜けると、伝え直しの 2 回目を受ける相手がいなくなる)
	script := "trap 'echo INT >> " + counts + "' INT; echo ready >> " + f.log + "; while :; do read _ < " + g + "; done"
	cmd := f.start([]string{"SERIAL_RUN_TEST_INT_GRACE=3s"}, true, "--", "/bin/sh", "-c", script)
	waitFor(t, "子が待ちに入る", func() bool { return f.hasLog("ready") })
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "子が INT を 1 回受ける", func() bool { return countLines(counts) >= 1 })
	// sleep-ok: negative: 伝え直しの 2 回目が来ないこと (猶予 3 秒より短い間で見る)
	time.Sleep(time.Second)
	if n := countLines(counts); n != 1 {
		t.Fatalf("グループ宛ての INT が子に %d 回届いた", n)
	}
	// 子は trap で INT をやり過ごすので、猶予の後に serial-run が子孫ごと止めて終わる (gate を解放しない: 読み手はもう居ないことがある)
	_ = exitCode(t, cmd)
	_ = g
}

// INT を受けて自分で後始末をする子は、猶予のうちなら最後まで走らせる (受けてすぐ止めると後始末が途中で切れる)
func TestIntLetsChildFinishCleanupWithinGrace(t *testing.T) {
	f := newFixture(t)
	g := f.gate("g")
	// sleep-ok: window: 後始末に 1 秒かかる子 (猶予 10 秒のうちに終わる)
	script := "trap 'sleep 1; echo cleaned >> " + f.log + "; exit 0' INT; echo ready >> " + f.log + "; while :; do read _ < " + g + "; done"
	cmd := f.start([]string{"SERIAL_RUN_TEST_INT_GRACE=10s"}, true, "--", "/bin/sh", "-c", script)
	waitFor(t, "子が待ちに入る", func() bool { return f.hasLog("ready") })
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	if rc := exitCode(t, cmd); rc != 0 {
		t.Fatalf("後始末を終えた子の rc が返らない: rc=%d", rc)
	}
	if !f.hasLog("cleaned") {
		t.Fatal("子の後始末が途中で止められた")
	}
}

// 猶予の後は子孫ごと止める (子だけ止めると、子が起こした背景のプロセスが lock の外で走り続ける)
func TestIntGraceStopsGrandchildren(t *testing.T) {
	f := newFixture(t)
	g := f.gate("g")
	gpidFile := filepath.Join(f.dir, "grandchild.pid")
	script := "/bin/sh -c 'echo $$ > " + gpidFile + "; while :; do read _ < " + g + "; done' & wait"
	cmd := f.start([]string{"SERIAL_RUN_TEST_INT_GRACE=1s"}, true, "--", "/bin/sh", "-c", script)
	waitFor(t, "孫の pid", func() bool { return strings.HasSuffix(readFile(gpidFile), "\n") })
	gpid, _ := strconv.Atoi(strings.TrimSpace(readFile(gpidFile)))
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	_ = exitCode(t, cmd)
	waitFor(t, "孫が止まる", func() bool { return syscall.Kill(gpid, 0) != nil || zombie(gpid) })
}

// openPty は pty の master と slave を開く (darwin の posix_openpt の手順。依存を足さないため ioctl を直接打つ)
func openPty(t *testing.T) (master, slave *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	ioctl := func(req uintptr, arg unsafe.Pointer) {
		if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, m.Fd(), req, uintptr(arg)); e != 0 {
			t.Fatalf("ioctl %#x: %v", req, e)
		}
	}
	ioctl(uintptr(syscall.TIOCPTYGRANT), nil)
	ioctl(uintptr(syscall.TIOCPTYUNLK), nil)
	name := make([]byte, 128)
	ioctl(uintptr(syscall.TIOCPTYGNAME), unsafe.Pointer(&name[0]))
	sl, err := os.OpenFile(string(name[:bytes.IndexByte(name, 0)]), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sl.Close() })
	return m, sl
}

// 制御端末の前景に居るときは、INT を受けても猶予で止めずに子の終わりを待つ (端末の Ctrl-C は子にも届いていて、それを扱って走り続ける子を殺さない)。
// stdin は端末にしない (前景の判定を stdin で見る退行を落とすため。制御端末だけを pty にする)
func TestIntInTerminalForegroundWaitsForChild(t *testing.T) {
	f := newFixture(t)
	_, slave := openPty(t)
	cmd := exec.Command(bin, "--", "/bin/sh", "-c", "echo ready >> "+f.log+"; i=0; while [ $i -lt 15 ]; do sleep 0.1; i=$((i+1)); done; exit 7")
	cmd.Env = f.env("SERIAL_RUN_TEST_INT_GRACE=200ms")
	cmd.ExtraFiles = []*os.File{slave}
	// 新しい session を作り、fd 3 (slave) を制御端末にする。session leader のグループが端末の前景になる
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 3}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			_ = cmd.Wait()
		}
	})
	waitFor(t, "子が始まる", func() bool { return f.hasLog("ready") })
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	if rc := exitCode(t, cmd); rc != 7 {
		t.Fatalf("端末の前景なのに猶予で止めた: rc=%d", rc)
	}
}

// 無いコマンドは、列を待ち切る前に 127 で抜ける。入れ子の直接実行でも同じ rc
func TestMissingCommandFailsBeforeWaiting(t *testing.T) {
	f := newFixture(t)
	ga := f.gate("a")
	a := f.held("A", ga, nil, false)
	waitFor(t, "A が始まる", func() bool { return f.hasLog("start A") })
	// 前確認は PATH から探す素の名前だけ (/ を含む名前は自分の番で確かめる)
	if rc := exitCode(t, f.start(nil, false, "--", "serialrun-no-such-command")); rc != rcNotFound {
		t.Fatalf("無いコマンドで待った / rc が違う: rc=%d", rc)
	}
	f.release(ga)
	_ = exitCode(t, a)
	nested := bin + " -- /nonexistent/cmd"
	if rc := exitCode(t, f.start(nil, false, "--", "/bin/sh", "-c", nested)); rc != rcNotFound {
		t.Fatalf("入れ子の無いコマンドの rc: %d want %d", rc, rcNotFound)
	}
}

// 実行できない (shebang の interpreter が無い) スクリプトの rc は、入れ子の直接実行でも通常の経路と同じ
func TestBadInterpreterRCMatchesNested(t *testing.T) {
	f := newFixture(t)
	bad := filepath.Join(f.dir, "bad.sh")
	if err := os.WriteFile(bad, []byte("#!/nonexistent/interp\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	plain := exitCode(t, f.start(nil, false, "--", bad))
	nested := exitCode(t, f.start(nil, false, "--", "/bin/sh", "-c", bin+" -- "+bad))
	if plain != nested || plain == rcTool {
		t.Fatalf("rc が経路で違う: 通常 %d / 入れ子 %d", plain, nested)
	}
}

// / を含むコマンド名は前確認で落とさない (前に並んでいる make build が作るものを指せるように)
func TestPathLikeCommandIsCheckedAtTurn(t *testing.T) {
	f := newFixture(t)
	ga := f.gate("a")
	made := filepath.Join(f.dir, "made.sh")
	a := f.start(nil, false, "--", "/bin/sh", "-c", "echo start A >> "+f.log+"; read _ < "+ga+"; printf '#!/bin/sh\\necho made >> "+f.log+"\\n' > "+made+"; chmod +x "+made)
	waitFor(t, "A が始まる", func() bool { return f.hasLog("start A") })
	b := f.start(nil, false, "--", made)
	waitFor(t, "B の札", func() bool { return f.ticketCount() == 2 })
	f.release(ga)
	_ = exitCode(t, a)
	if rc := exitCode(t, b); rc != 0 || !f.hasLog("made") {
		t.Fatalf("前の人が作ったコマンドを走らせない: rc=%d", rc)
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
	// 生きている札 7 番 (この test が flock を持つ)。seq が壊れていても、新しい番号はこれより大きくなる (生きている札と重ならない)。
	// 持ち主のいない札は採番の前に回収されうるので、回収されない生きている札で確かめる
	live, err := os.OpenFile(filepath.Join(f.qdir(), "000000000007"+ticketSuffix), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	if err := syscall.Flock(int(live.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if rc := exitCode(t, f.start(nil, false, "--max-wait", "0.3", "--", "/usr/bin/true")); rc != rcTool {
		t.Fatalf("生きている札の後ろで待たなかった: rc=%d", rc)
	}
	if got := strings.TrimSpace(readFile(filepath.Join(f.state, "default", "seq"))); got != "000000000008" {
		t.Fatalf("番号が生きている札の後ろから続いていない: seq=%q", got)
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

// status は f の置き場で `serial-run status` を打ち、rc・stdout・stderr を返す
func (f *fixture) status(args ...string) (int, string, string) {
	f.t.Helper()
	cmd := exec.Command(bin, append([]string{"status"}, args...)...)
	cmd.Env = f.env()
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	rc := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		rc = ee.ExitCode()
	} else if err != nil {
		f.t.Fatal(err)
	}
	return rc, out.String(), errb.String()
}

// 見るだけの status は置き場を作らない (typo の lock 名で空の置き場を作り「無い」と答えない)
func TestStatusDoesNotCreateQueue(t *testing.T) {
	f := newFixture(t)
	rc, out, _ := f.status("-l", "no-such-lock")
	if rc != 0 || !strings.Contains(out, "一度も使われていない") {
		t.Fatalf("使われていない lock の status: rc=%d out=%q", rc, out)
	}
	if _, err := os.Stat(filepath.Join(f.state, "no-such-lock")); !os.IsNotExist(err) {
		t.Fatalf("status が置き場を作った: %v", err)
	}
}

// 持ち主の serial-run だけが死んで子が走っている札は、status でも走っているものとして出す (待ち手が待っているのに「無い」と出さない)
func TestStatusShowsOrphanChildAsRunning(t *testing.T) {
	f := newFixture(t)
	child := startSleeper(t)
	start, known := procStart(child)
	if !known || start == "" {
		t.Fatal("子の開始時刻を取れない")
	}
	writeDeadTicket(t, f, ticketInfo{Pid: 999999, Start: time.Now().Add(-time.Minute), Cmd: "make test", ChildPid: child, ChildStartUTC: start, V: ticketVersion})
	rc, out, _ := f.status()
	if rc != 0 || !strings.Contains(out, "先頭") || !strings.Contains(out, "子 pid "+strconv.Itoa(child)+" がまだ走っている") || strings.Contains(out, "無い") {
		t.Fatalf("孤児の子を走っているものとして出さない: rc=%d out=%q", rc, out)
	}
}

// 札の一覧を読めないときは rc=125 と stderr で知らせる (rc=0 で「無い」と答えない)
func TestStatusFailsLoudWhenQueueUnreadable(t *testing.T) {
	f := newFixture(t)
	c := exec.Command(bin, "--", "/usr/bin/true") // 置き場を作る
	c.Env = f.env()
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(f.qdir(), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.qdir(), 0o700) })
	rc, out, errOut := f.status()
	if rc != rcTool || !strings.Contains(errOut, "札を読めない") || strings.Contains(out, "無い") {
		t.Fatalf("読めない置き場の status: rc=%d out=%q err=%q", rc, out, errOut)
	}
}

// 待っただけで走らなかった run も history.log に 1 行残す (計測の母集合から抜けない)
func TestHistoryRecordsRunsThatDidNotRun(t *testing.T) {
	f := newFixture(t)
	ga := f.gate("a")
	a := f.held("A", ga, nil, false)
	waitFor(t, "A が始まる", func() bool { return f.hasLog("start A") })
	if rc := exitCode(t, f.start(nil, false, "--max-wait", "1", "--", "/bin/sh", "-c", "echo B >> "+f.log)); rc != rcTool {
		t.Fatalf("待ちの上限で抜けない: rc=%d", rc)
	}
	f.release(ga)
	_ = exitCode(t, a)
	hist := readFile(queue{dir: filepath.Join(f.state, "default")}.historyPath())
	lines := strings.Split(strings.TrimSpace(hist), "\n")
	if len(lines) != 2 {
		t.Fatalf("history.log の行数が 2 でない:\n%s", hist)
	}
	if !strings.Contains(lines[0], "\tran=-\trc=125\t") || !strings.HasSuffix(lines[0], "\ttimeout") {
		t.Fatalf("上限で抜けた B の行が無い:\n%s", hist)
	}
	if !strings.Contains(lines[1], "\trc=0\t") || !strings.HasSuffix(lines[1], "\tran") || strings.Contains(lines[1], "ran=-") {
		t.Fatalf("走った A の行が無い:\n%s", hist)
	}
}

// 「長く持っている」は走り出してから数える。待って先頭に上がった札を、走り出した直後から 🚨 にしない。
// 走らないまま長い先頭 (持ち主が止まった・run_start を書かない古い版) には付け、待っている後ろの札には付けない
func TestLongHoldCountsFromRunStart(t *testing.T) {
	now := time.Now()
	for _, c := range []struct {
		name string
		info ticketInfo
		head bool
		long bool
	}{
		{"長く待って走り出した直後", ticketInfo{Start: now.Add(-2 * longHold), RunStart: now.Add(-time.Minute)}, true, false},
		{"走り出してから長い", ticketInfo{Start: now.Add(-2 * longHold), RunStart: now.Add(-longHold - time.Minute)}, true, true},
		{"走らないまま長い先頭", ticketInfo{Start: now.Add(-2 * longHold)}, true, true},
		{"長く待っている後ろの札", ticketInfo{Start: now.Add(-2 * longHold)}, false, false},
		{"札を出したばかりの先頭", ticketInfo{Start: now.Add(-time.Minute)}, true, false},
	} {
		if _, long := c.info.age(c.head); long != c.long {
			t.Errorf("%s: long=%v want %v", c.name, long, c.long)
		}
	}
}

// 子を起こしたら札に走り出した時刻を書く (「長く持っている」はこれで数える)
func TestTicketRecordsRunStart(t *testing.T) {
	f := newFixture(t)
	ga := f.gate("a")
	a := f.held("A", ga, nil, false)
	waitChildRecorded(t, f)
	info, ok := readInfoAt(firstTicket(f))
	if !ok || info.RunStart.IsZero() || info.RunStart.Before(info.Start) {
		t.Fatalf("札に走り出した時刻が無い: %+v", info)
	}
	f.release(ga)
	_ = exitCode(t, a)
}

// status は見るだけ: 止まったままの孤児の子を止めず、札も消さない (回収は待ち手が行う)
func TestStatusDoesNotReclaimStoppedOrphan(t *testing.T) {
	f := newFixture(t)
	child := startSleeper(t)
	start, known := procStart(child)
	if !known || start == "" {
		t.Fatal("子の開始時刻を取れない")
	}
	if err := syscall.Kill(child, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGCONT) })
	writeDeadTicket(t, f, ticketInfo{Pid: 999999, Start: time.Now().Add(-time.Minute), Cmd: "make test", ChildPid: child, ChildStartUTC: start, Pgid: syscall.Getpgrp(), V: ticketVersion})
	rc, out, _ := f.status()
	if rc != 0 || !strings.Contains(out, "がまだ走っている") {
		t.Fatalf("止まった孤児を出さない: rc=%d out=%q", rc, out)
	}
	if st, _ := procStat(child); !strings.HasPrefix(st, "T") {
		t.Fatalf("status が孤児の子に手を出した: stat=%q", st)
	}
	if _, err := os.Stat(firstTicket(f)); err != nil {
		t.Fatalf("status が札を消した: %v", err)
	}
}

// status は死んだ札 (持ち主も子も居ない) を消さない (見るだけの入口。消すのは待ち手)
func TestStatusDoesNotRemoveDeadTicket(t *testing.T) {
	f := newFixture(t)
	writeDeadTicket(t, f, ticketInfo{Pid: 999999, Start: time.Now().Add(-time.Minute), Cmd: "make test", V: ticketVersion})
	if rc, out, _ := f.status(); rc != 0 || !strings.Contains(out, "無い") {
		t.Fatalf("死んだ札だけの status: rc=%d out=%q", rc, out)
	}
	if _, err := os.Stat(firstTicket(f)); err != nil {
		t.Fatalf("status が死んだ札を消した: %v", err)
	}
}
