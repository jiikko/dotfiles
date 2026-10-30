package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"subproc"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"testwait"
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
	// テストの git (gitRepo) が継承した repo の場所を見ないように外す
	if err := subproc.UnsetGitLocationEnv(); err != nil {
		panic(err)
	}
	// 既存のテストはマシン全体の列 (default) で見る。repo ごとの列は TestRepoScope* が git の repo を作って見る
	if err := os.Setenv(scopeEnv, "machine"); err != nil {
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
	env = append(env, "SERIAL_RUN_STATE_DIR="+f.state, "HOME="+f.dir, "XDG_STATE_HOME="+filepath.Join(f.dir, "xdg"), scopeEnv+"=machine")
	return append(env, extra...)
}

func (f *fixture) qdir() string { return f.queue().qdir() }

// queue は f の既定の lock の置き場 (札・history の名前は本体の関数で作る。テストが書式を写さない)
func (f *fixture) queue() queue { return queue{dir: filepath.Join(f.state, defaultLock)} }

func (f *fixture) ticketPath(seq int64) string { return f.queue().ticketPath(seq) }

// readPid は子が書いた pid のファイルを読む。読めなければ落とす (0 を pid として扱うと、生存の検査が黙って飛ぶ)
func readPid(t *testing.T, path string) int {
	t.Helper()
	pid, err := strconv.Atoi(strings.TrimSpace(readFile(path)))
	if err != nil || pid <= 0 {
		t.Fatalf("pid を読めない: %s: %q", path, readFile(path))
	}
	return pid
}

// waitStarted は held で起こした name の子が始まるまで待つ
func (f *fixture) waitStarted(name string) {
	f.t.Helper()
	testwait.Until(f.t, name+" が始まる", func() bool { return f.hasLog("start " + name) })
}

// waitTickets は札が n 枚並ぶまで待つ (n 本目の待ち手が札を出し終えた = 待ちに入った)
func (f *fixture) waitTickets(n int) {
	f.t.Helper()
	testwait.Until(f.t, fmt.Sprintf("札が %d 枚並ぶ", n), func() bool { return f.ticketCount() == n })
}

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
func (f *fixture) held(name, gate string, extraEnv []string, args ...string) *exec.Cmd {
	f.t.Helper()
	script := "echo start " + name + " >> " + f.log + "; read _ < " + gate + "; echo end " + name + " >> " + f.log
	argv := append(append([]string{}, args...), "--", "/bin/sh", "-c", script)
	return f.start(extraEnv, argv...)
}

func (f *fixture) start(extraEnv []string, args ...string) *exec.Cmd {
	f.t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = f.env(extraEnv...)
	f.track(cmd)
	if err := cmd.Start(); err != nil {
		f.t.Fatal(err)
	}
	return cmd
}

// track は test が起こすプロセスを専用のプロセスグループに置き、test の終わりにそのグループの残りを止める
// (テストが途中で落ちると、fifo で止まった子や、親が先に死んで init へ移った孫が PPID=1 で残り続ける。
// pid の木をたどると、親を失った孫を取りこぼす)。Start の前に呼ぶ。
// 🚨 グループの番号は、持ち主が全員死ぬと別のプロセスに使い回されうる。止めるのは、そのグループに居て、
// コマンド行にこの test の一時 dir か test 用の serial-run を含むものだけにする
func (f *fixture) track(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	if !cmd.SysProcAttr.Setsid {
		cmd.SysProcAttr.Setpgid = true
	}
	f.t.Cleanup(func() {
		if cmd.Process == nil {
			return
		}
		pgid := cmd.Process.Pid
		if cmd.ProcessState == nil {
			// 先頭がまだ回収されていない間は、グループの番号は使い回されない: グループごと止める
			// (コマンド行に一時 dir を含まない子孫 (`sleep 41 &` など) もここで止まる)
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			_ = cmd.Wait()
		}
		out, err := exec.Command("/bin/ps", "-A", "-ww", "-o", "pid=,pgid=,command=").Output()
		if err != nil {
			return
		}
		for _, l := range strings.Split(string(out), "\n") {
			fs := strings.Fields(l)
			if len(fs) < 3 || fs[1] != strconv.Itoa(pgid) {
				continue
			}
			if c := strings.Join(fs[2:], " "); strings.Contains(c, f.dir) || strings.Contains(c, bin) {
				if pid, err := strconv.Atoi(fs[0]); err == nil {
					_ = syscall.Kill(pid, syscall.SIGKILL)
				}
			}
		}
	})
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
// テストで縮める待ちの刻み。否定の確認の窓は刻みの何回分かで作る (刻みだけ伸ばすと、窓の中で見直しが来ない弱い検査になる)
const (
	testTick   = 100 * time.Millisecond // 見直し (RECHECK) と保険 (FALLBACK)
	reportTick = 50 * time.Millisecond  // 待ちの行 (REPORT)
)

func tickEnv(name string, d time.Duration) string { return name + "=" + d.String() }

// negWindow は否定の確認 (起きないことを見る) の窓: 刻み n 回分
func negWindow(tick time.Duration, n int) time.Duration { return tick * time.Duration(n) }

func exitCode(t *testing.T, cmd *exec.Cmd) int {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	err := testwait.Recv(t, "serial-run が 10 秒で終わらない", done, testwait.Within(10*time.Second))
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
}

func TestRunsOneAtATimeInTicketOrder(t *testing.T) {
	f := newFixture(t)
	ga, gb, gc := f.gate("a"), f.gate("b"), f.gate("c")
	a := f.held("A", ga, nil)
	f.waitStarted("A")
	b := f.held("B", gb, nil)
	f.waitTickets(2)
	c := f.held("C", gc, nil)
	f.waitTickets(3)

	f.release(ga)
	f.waitStarted("B")
	if f.hasLog("start C") {
		t.Fatalf("B が走っている間に C も始まった: %q", f.logText())
	}
	f.release(gb)
	f.waitStarted("C")
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
	x := f.start([]string{"SERIAL_RUN_TEST_PAUSE_BEFORE_RENAME=" + pause}, "--", "/bin/sh", "-c", "echo X >> "+f.log)
	testwait.Until(t, "X が札を作りかけで止まる", func() bool {
		ents, _ := os.ReadDir(f.qdir())
		for _, e := range ents {
			if strings.HasPrefix(e.Name(), tmpPrefix) {
				return true
			}
		}
		return false
	})

	y := f.start(nil, "--", "/bin/sh", "-c", "echo Y >> "+f.log)
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
	a := f.start(nil, "--", "/bin/sh", "-c", "echo $$ > "+pidFile+"; echo start A >> "+f.log+"; read _ < "+ga)
	f.waitStarted("A")
	b := f.held("B", gb, nil)
	f.waitTickets(2)

	childPid := readPid(t, pidFile)
	if err := syscall.Kill(-a.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = exitCode(t, a)
	f.waitStarted("B")
	if !gone(childPid) {
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
	a := f.held("A", ga, nil)
	f.waitStarted("A")
	b := f.held("B", gb, []string{"SERIAL_RUN_TEST_WAKE_LOG=" + wakes, tickEnv("SERIAL_RUN_TEST_REPORT", reportTick)})
	f.waitTickets(2)
	testwait.Until(t, "B の最初の見直し", func() bool { return countLines(wakes) >= 1 })
	// sleep-ok: negative: 前の持ち主が動かない間に B が起きないこと (表示の刻み 50ms の 20 回分。起きないことの確認)
	time.Sleep(negWindow(reportTick, 20))
	if n := countLines(wakes); n != 1 {
		t.Fatalf("前の持ち主が動かない間に %d 回起きた (ポーリングしている)", n)
	}
	f.release(ga)
	f.waitStarted("B")
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
	a := f.held("A", ga, nil)
	f.waitStarted("A")
	b := f.held("B", gb, []string{"SERIAL_RUN_TEST_NO_BLOCK=1", "SERIAL_RUN_TEST_FALLBACK=200ms"})
	f.waitTickets(2)
	f.release(ga)
	testwait.Until(t, "B が保険の見直しで始まる", func() bool { return f.hasLog("start B") })
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
	a := f.held("A", ga, nil)
	f.waitStarted("A")
	b := f.held("B", gb, nil)
	f.waitTickets(2)
	c := f.held("C", gc, nil)
	f.waitTickets(3)

	_ = syscall.Kill(-b.Process.Pid, syscall.SIGKILL)
	_ = exitCode(t, b)
	f.waitTickets(2)
	if f.hasLog("start C") {
		t.Fatalf("A が走っている間に C が始まった: %q", f.logText())
	}
	f.release(ga)
	f.waitStarted("C")
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
	outer := f.start(nil, "--", "/bin/sh", "-c", inner)
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
	a := f.held("A", ga, nil)
	f.waitStarted("A")
	stale := heldEnv + "=" + heldToken(filepath.Join(f.state, "default"), 999, "x")
	b := f.start([]string{stale}, "--", "/bin/sh", "-c", "echo B >> "+f.log)
	f.waitTickets(2)
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
	a := f.held("A", ga, nil)
	f.waitStarted("A")
	b := f.start(nil, "--max-wait", "0.3", "--", "/bin/sh", "-c", "echo B >> "+f.log)
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
	bad := f.ticketPath(1)
	if err := os.WriteFile(bad, nil, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o600) })
	wakes := filepath.Join(f.dir, "wakes")
	b := f.start([]string{"SERIAL_RUN_TEST_WAKE_LOG=" + wakes}, "--max-wait", "1", "--", "/bin/sh", "-c", "echo B >> "+f.log)
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
	a := f.held("A", ga, nil)
	testwait.Until(t, "A が始まり、札に子の pid が載る", func() bool {
		return f.hasLog("start A") && strings.Contains(readFile(f.ticketPath(1)), "child_pid")
	})

	wakes := filepath.Join(f.dir, "wakes")
	b := f.held("B", gb, []string{tickEnv("SERIAL_RUN_TEST_RECHECK", testTick), "SERIAL_RUN_TEST_WAKE_LOG=" + wakes})
	f.waitTickets(2)
	if err := a.Process.Signal(syscall.SIGKILL); err != nil { // 子は殺さない (pid 宛ての KILL)
		t.Fatal(err)
	}
	_ = exitCode(t, a)
	before := countLines(wakes)
	// sleep-ok: negative: A の子が走っている間に B が入らないこと (見直しの刻み 100ms の 5 回分)
	time.Sleep(negWindow(testTick, 5))
	if f.hasLog("start B") {
		t.Fatalf("持ち主だけが死んだ後、A の子が走っている間に B が始まった: %q", f.logText())
	}
	// 孤児の札は flock では待てない (持ち主が死んで lock は外れている) ので、見直しの刻みで起きる。blocking で眠ると空回りする
	// (2026-10-23 の実測: 正しい版は 0.5 秒で 4 回 (3 回測って 3 回とも)、blocking で眠る変異は 1.5 秒で 705 回 (監査の報告))
	if n := countLines(wakes) - before; n > 20 {
		t.Fatalf("孤児の札の後ろで空回りした: 0.5 秒で %d 回起きた", n)
	}
	f.release(ga)
	testwait.Until(t, "A の子が終わってから B が始まる", func() bool { return f.hasLog("start B") })
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
	a := f.held("A", ga, nil)
	f.waitStarted("A")
	b := f.held("B", gb, []string{tickEnv("SERIAL_RUN_TEST_FALLBACK", testTick)})
	f.waitTickets(2)
	if err := os.Remove(f.ticketPath(1)); err != nil {
		t.Fatal(err)
	}
	// sleep-ok: negative: 札が消えても B が入らないこと (保険の見直し 100ms の 5 回分。B は前の札が無いと読んで run.lock を待つ)
	time.Sleep(negWindow(testTick, 5))
	if f.hasLog("start B") {
		t.Fatal("持ち主の札が消えただけで、A が走っている間に B が始まった")
	}
	f.release(ga)
	f.waitStarted("B")
	f.release(gb)
	_ = exitCode(t, a)
	_ = exitCode(t, b)
}

// グループへの SIGQUIT で、rc が Go のダンプの 2 に化けない
func TestQuitToGroupKeepsSignalRC(t *testing.T) {
	f := newFixture(t)
	g := f.gate("g")
	pidFile := filepath.Join(f.dir, "child.pid")
	cmd := f.start(nil, "--", "/bin/sh", "-c", "echo $$ > "+pidFile+"; read _ < "+g)
	testwait.Until(t, "子の pid", func() bool { return strings.HasSuffix(readFile(pidFile), "\n") })
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
	a := f.held("A", ga, nil)
	f.waitStarted("A")
	errPath := filepath.Join(f.dir, "b.err")
	ef, err := os.Create(errPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ef.Close()
	b := exec.Command(bin, "--", "/bin/sh", "-c", "echo B >> "+f.log)
	b.Env = f.env(tickEnv("SERIAL_RUN_TEST_REPORT", reportTick))
	b.Stderr = ef
	f.track(b)
	if err := b.Start(); err != nil {
		t.Fatal(err)
	}
	testwait.Until(t, "B の定期の待ちの行が 2 行以上出る", func() bool { return strings.Count(readFile(errPath), "serial-run: 待ち 1 番目") >= 2 })
	f.release(ga)
	_ = exitCode(t, a)
	if rc := exitCode(t, b); rc != 0 {
		t.Fatalf("B rc=%d", rc)
	}
	s := readFile(errPath)
	if n := strings.Count(s, "serial-run status"); n != 1 {
		t.Fatalf("案内が %d 回出た (最初の 1 行だけのはず):\n%s", n, s)
	}
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if !strings.Contains(lines[0], "serial-run status") || !strings.Contains(lines[0], " 前: ") {
		t.Fatalf("最初の行に案内か先頭の説明が無い:\n%s", s)
	}
	// 順番が変わらない周期の行は、先頭の説明を繰り返さない (1 時間待つと tool の結果が数十 KB になる)
	if strings.Contains(lines[1], " 前: ") {
		t.Fatalf("周期の行が先頭の説明を繰り返した:\n%s", s)
	}
}

func firstTicket(f *fixture) string { return f.ticketPath(1) }

// waitChildRecorded は A の札に子の pid と開始時刻が載るまで待つ
func waitChildRecorded(t *testing.T, f *fixture) {
	t.Helper()
	testwait.Until(t, "札に子の pid と開始時刻が載る", func() bool {
		s := readFile(firstTicket(f))
		return strings.Contains(s, "child_pid") && strings.Contains(s, "child_start")
	})

}

// 札を書いた serial-run と読む serial-run のロケール・タイムゾーンが違っても、孤児の子を同じ子と読む (ps の lstart の書式が変わる)
func TestOrphanSurvivesLocaleAndTimezoneDifference(t *testing.T) {
	f := newFixture(t)
	ga, gb := f.gate("a"), f.gate("b")
	a := f.held("A", ga, []string{"LC_ALL=C", "TZ=UTC"})
	f.waitStarted("A")
	waitChildRecorded(t, f)
	b := f.held("B", gb, []string{"LC_ALL=ja_JP.UTF-8", "TZ=Asia/Tokyo", tickEnv("SERIAL_RUN_TEST_RECHECK", testTick)})
	f.waitTickets(2)
	if err := a.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = exitCode(t, a)
	// sleep-ok: negative: A の子が走っている間に B が入らないこと (見直しの刻み 100ms の 5 回分)
	time.Sleep(negWindow(testTick, 5))
	if f.hasLog("start B") {
		t.Fatal("ロケールとタイムゾーンが違うだけで、走っている孤児の子を死んだと読んだ")
	}
	f.release(ga)
	testwait.Until(t, "A の子が終わってから B", func() bool { return f.hasLog("start B") })
	f.release(gb)
	_ = exitCode(t, b)
}

// ps が使えないときは、孤児の札を消さずに待つ (「子はもう居ない」と読まない)
func TestPsFailureKeepsOrphanTicket(t *testing.T) {
	f := newFixture(t)
	ga := f.gate("a")
	a := f.held("A", ga, nil)
	f.waitStarted("A")
	waitChildRecorded(t, f)
	if err := a.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = exitCode(t, a)
	b := f.start([]string{"SERIAL_RUN_TEST_PS=/nonexistent/ps", tickEnv("SERIAL_RUN_TEST_RECHECK", testTick)},
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
	a := f.start(nil, "--", "/bin/sh", "-c", "echo $$ > "+pidFile+"; read _ < "+ga)
	testwait.Until(t, "子の pid", func() bool { return strings.HasSuffix(readFile(pidFile), "\n") })
	waitChildRecorded(t, f)
	child := readPid(t, pidFile)
	if err := syscall.Kill(child, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })
	if err := a.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = exitCode(t, a)
	b := f.start([]string{tickEnv("SERIAL_RUN_TEST_RECHECK", testTick)}, "--max-wait", "5", "--", "/bin/sh", "-c", "echo B >> "+f.log)
	if rc := exitCode(t, b); rc != 0 || !f.hasLog("B") {
		t.Fatalf("停止したままの孤児の後ろで進めなかった: rc=%d", rc)
	}
	testwait.Until(t, "停止していた子が止められる", func() bool { return gone(child) })
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
	f.track(a)
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	testwait.Until(t, "子の pid", func() bool { return strings.HasSuffix(readFile(pidFile), "\n") })
	testwait.Until(t, "札に子の pid が載り、開始時刻を書けなかった警告が出る", func() bool {
		return strings.Contains(readFile(firstTicket(f)), "child_pid") && strings.Contains(readFile(errPath), "子の情報を書けない")
	})

	if strings.Contains(readFile(firstTicket(f)), "child_start") {
		t.Fatal("前提が崩れた: ps が使えないのに開始時刻が書かれた")
	}
	child := readPid(t, pidFile)
	if err := syscall.Kill(child, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })
	if err := a.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = exitCode(t, a)
	b := f.start([]string{tickEnv("SERIAL_RUN_TEST_RECHECK", testTick)}, "--max-wait", "5", "--", "/bin/sh", "-c", "echo B >> "+f.log)
	if rc := exitCode(t, b); rc != 0 || !f.hasLog("B") {
		t.Fatalf("開始時刻の無い停止した孤児の後ろで進めなかった: rc=%d", rc)
	}
	// 「別人」と読んで札だけ消したのではなく、子として回収した (停止したままの子を残さない)
	testwait.Until(t, "停止していた子が止められる", func() bool { return gone(child) })
}

// 先頭が孤児 (持ち主の serial-run だけが死んだ) のとき、その子の子孫から SERIAL_RUN_HELD を落として呼んでも素通しする
func TestNestedWithoutHeldUnderOrphanFindsAncestor(t *testing.T) {
	f := newFixture(t)
	ga := f.gate("a")
	inner := "env -u SERIAL_RUN_HELD " + bin + " --max-wait 3 -- /bin/sh -c 'echo inner >> " + f.log + "'"
	a := f.start(nil, "--", "/bin/sh", "-c", "echo start >> "+f.log+"; read _ < "+ga+"; "+inner)
	testwait.Until(t, "外側が始まる", func() bool { return f.hasLog("start") })
	waitChildRecorded(t, f)
	if err := a.Process.Signal(syscall.SIGKILL); err != nil { // 外側の serial-run だけを止める (子は残る)
		t.Fatal(err)
	}
	_ = exitCode(t, a)
	f.release(ga)
	testwait.Until(t, "孤児の下の入れ子が素通しで走る", func() bool { return f.hasLog("inner") })
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
	b := f.start(nil, "--max-wait", "1", "--", "/bin/sh", "-c", "echo B >> "+f.log)
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
	a := f.start([]string{"LC_ALL=ja_JP.UTF-8", "TZ=Asia/Tokyo"}, "--", "/bin/sh", "-c", "echo $$ > "+pidFile+"; read _ < "+ga)
	testwait.Until(t, "子の pid", func() bool { return strings.HasSuffix(readFile(pidFile), "\n") })
	testwait.Until(t, "札に child_start_utc が載る", func() bool { return strings.Contains(readFile(firstTicket(f)), "child_start_utc") })
	child := readPid(t, pidFile)
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
	b := f.start(nil, "--max-wait", "3", "--", "/bin/sh", "-c", "echo B >> "+f.log)
	if rc := exitCode(t, b); rc != 0 || !f.hasLog("B") {
		t.Fatalf("寿命を過ぎた確かめられない孤児の札の後ろで待った: rc=%d", rc)
	}
	if gone(other) {
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
	b := f.start(nil, "--max-wait", "1", "--", "/bin/sh", "-c", "echo B >> "+f.log)
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
	b := f.start(nil, "--max-wait", "1", "--", "/bin/sh", "-c", "echo B >> "+f.log)
	_ = exitCode(t, b)
	if gone(other) {
		t.Fatal("持ち主のグループと合わない、使い回された pid の停止中のプロセスを止めた")
	}
}

// 大文字・先頭の . を含む lock 名は通さない (同じ置き場を別名で指す / 親の置き場の seq を上書きする)
func TestLockNameRejectsCaseAndDots(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"Default", ".", "..", ".x"} {
		if rc := exitCode(t, f.start(nil, "-l", name, "--", "/usr/bin/true")); rc != rcTool {
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
	if rc := exitCode(t, f.start(nil, "--", "/bin/sh", "-c", inner)); rc != 0 || !f.hasLog("inner") {
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
	if rc := exitCode(t, f.start(nil, "--", "/bin/sh", "-c", inner)); rc != 0 || !f.hasLog("inner") {
		t.Fatalf("SERIAL_RUN_HELD を落とした入れ子が待った: rc=%d", rc)
	}
}

// 番号が合っていても nonce が違う SERIAL_RUN_HELD (置き場が作り直されて番号が使い回された後の古い値) では並ぶ
func TestHeldWithWrongNonceQueues(t *testing.T) {
	f := newFixture(t)
	ga := f.gate("a")
	a := f.held("A", ga, nil)
	f.waitStarted("A")
	stale := heldEnv + "=" + heldToken(filepath.Join(f.state, "default"), 1, "not-the-nonce")
	b := f.start([]string{stale}, "--", "/bin/sh", "-c", "echo B >> "+f.log)
	f.waitTickets(2)
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
	x := f.start([]string{"SERIAL_RUN_TEST_PAUSE_BEFORE_RENAME=" + pause}, "--", "/usr/bin/true")
	testwait.Until(t, "X が採番の lock を持ったまま止まる", func() bool {
		ents, _ := os.ReadDir(f.qdir())
		for _, e := range ents {
			if strings.HasPrefix(e.Name(), tmpPrefix) {
				return true
			}
		}
		return false
	})

	retries := filepath.Join(f.dir, "seq-retries")
	y := f.start([]string{"SERIAL_RUN_TEST_SEQ_RETRY_LOG=" + retries}, "--", "/usr/bin/true")
	// 再試行の中に居ることを印で確かめてから撃つ (入る前に撃つと、Notify の前の既定の動作で死んでも同じ rc になり、何も確かめない)
	testwait.Until(t, "Y が採番の lock の再試行に入る", func() bool { return countLines(retries) >= 1 })
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
	if rc := exitCode(t, f.start([]string{"SERIAL_RUN_STATE_DIR=/dev/null/serial-run"}, "status")); rc != rcTool {
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
	if rc := exitCode(t, f.start(nil, "--", "/bin/sh", "-c", "echo RAN >> "+f.log)); rc != 0 || !f.hasLog("RAN") {
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
	if rc := exitCode(t, f.start(nil, "--", "/bin/sh", "-c", "echo RAN >> "+f.log)); rc != 0 || !f.hasLog("RAN") {
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
	cmd := f.start([]string{"SERIAL_RUN_TEST_INT_GRACE=1s"}, "--", "/bin/sh", "-c", "echo $$ > "+pidFile+"; read _ < "+g)
	testwait.Until(t, "子の pid", func() bool { return strings.HasSuffix(readFile(pidFile), "\n") })
	child := readPid(t, pidFile)
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	if rc := exitCode(t, cmd); rc != 128+int(syscall.SIGINT) {
		t.Fatalf("pid 宛ての INT で止まらない: rc=%d", rc)
	}
	if !gone(child) {
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
	cmd := f.start([]string{"SERIAL_RUN_TEST_INT_GRACE=3s"}, "--", "/bin/sh", "-c", script)
	testwait.Until(t, "子が待ちに入る", func() bool { return f.hasLog("ready") })
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	testwait.Until(t, "子が INT を 1 回受ける", func() bool { return countLines(counts) >= 1 })
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
	cmd := f.start([]string{"SERIAL_RUN_TEST_INT_GRACE=10s"}, "--", "/bin/sh", "-c", script)
	testwait.Until(t, "子が待ちに入る", func() bool { return f.hasLog("ready") })
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
	cmd := f.start([]string{"SERIAL_RUN_TEST_INT_GRACE=1s"}, "--", "/bin/sh", "-c", script)
	testwait.Until(t, "孫の pid", func() bool { return strings.HasSuffix(readFile(gpidFile), "\n") })
	gpid := readPid(t, gpidFile)
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	_ = exitCode(t, cmd)
	testwait.Until(t, "孫が止まる", func() bool { return gone(gpid) })
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
	f.track(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	testwait.Until(t, "子が始まる", func() bool { return f.hasLog("ready") })
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
	a := f.held("A", ga, nil)
	f.waitStarted("A")
	// 前確認は PATH から探す素の名前だけ (/ を含む名前は自分の番で確かめる)
	if rc := exitCode(t, f.start(nil, "--", "serialrun-no-such-command")); rc != rcNotFound {
		t.Fatalf("無いコマンドで待った / rc が違う: rc=%d", rc)
	}
	f.release(ga)
	_ = exitCode(t, a)
	nested := bin + " -- /nonexistent/cmd"
	if rc := exitCode(t, f.start(nil, "--", "/bin/sh", "-c", nested)); rc != rcNotFound {
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
	plain := exitCode(t, f.start(nil, "--", bad))
	nested := exitCode(t, f.start(nil, "--", "/bin/sh", "-c", bin+" -- "+bad))
	if plain != nested || plain == rcTool {
		t.Fatalf("rc が経路で違う: 通常 %d / 入れ子 %d", plain, nested)
	}
}

// / を含むコマンド名は前確認で落とさない (前に並んでいる make build が作るものを指せるように)
func TestPathLikeCommandIsCheckedAtTurn(t *testing.T) {
	f := newFixture(t)
	ga := f.gate("a")
	made := filepath.Join(f.dir, "made.sh")
	a := f.start(nil, "--", "/bin/sh", "-c", "echo start A >> "+f.log+"; read _ < "+ga+"; printf '#!/bin/sh\\necho made >> "+f.log+"\\n' > "+made+"; chmod +x "+made)
	f.waitStarted("A")
	b := f.start(nil, "--", made)
	f.waitTickets(2)
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
		if rc := exitCode(t, f.start(nil, c.args...)); rc != c.want {
			t.Errorf("%v: rc=%d want %d", c.args, rc, c.want)
		}
	}
}

// lock と札の fd は子へ渡らない (渡ると子が残した背景プロセスが lock を握ったまま残る)
func TestLockFdsAreNotInherited(t *testing.T) {
	f := newFixture(t)
	out := filepath.Join(f.dir, "lsof")
	cmd := f.start(nil, "--", "/bin/sh", "-c", "/usr/sbin/lsof -p $$ -Fn > "+out)
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
	if rc := exitCode(t, f.start(nil, "--max-wait", "0.3", "--", "/usr/bin/true")); rc != rcTool {
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
	cmd := f.start(nil, "--", "/bin/sh", "-c", "echo $$ > "+pidFile+"; read _ < "+g)
	testwait.Until(t, "子の pid", func() bool { return strings.HasSuffix(readFile(pidFile), "\n") })
	child := readPid(t, pidFile)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	_ = exitCode(t, cmd)
	if !gone(child) {
		t.Fatalf("SIGTERM の後に子 (pid %d) が残っている", child)
	}
	if n := f.ticketCount(); n != 0 {
		t.Fatalf("札が残っている: %d 枚", n)
	}
}

// 表示のコマンド行は空白を畳み、制御文字を落とす。人が打った sh -c は剥がさない (何を走らせたかが表示から消える)
func TestDisplayCmdIsOneSafeLine(t *testing.T) {
	if got := displayCmd([]string{"/bin/zsh", "-c", "make  test\n"}); got != "/bin/zsh -c make test" {
		t.Fatalf("got %q", got)
	}
	if got := displayCmd([]string{"echo", "\x1b]0;PWNED\x07"}); got != "echo ?]0;PWNED?" {
		t.Fatalf("制御文字が残った: %q", got)
	}
	if got := displayCmd([]string{"echo", "a\u202eb", "日本語"}); got != "echo a?b 日本語" {
		t.Fatalf("双方向の上書きが残った / 日本語を壊した: %q", got)
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
	a := f.held("A", ga, nil)
	f.waitStarted("A")
	if rc := exitCode(t, f.start(nil, "--max-wait", "1", "--", "/bin/sh", "-c", "echo B >> "+f.log)); rc != rcTool {
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
	a := f.held("A", ga, nil)
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

// -h / --help は usage を stdout に出して rc=0 (help の要求を「道具の失敗」の 125 にしない)
func TestHelpPrintsUsage(t *testing.T) {
	for _, a := range []string{"-h", "--help"} {
		out, err := exec.Command(bin, a).Output()
		if err != nil || !strings.HasPrefix(string(out), "usage: serial-run") {
			t.Fatalf("%s: err=%v out=%q", a, err, out)
		}
	}
}

// usage の既定値は定数と一致する (文面への複写が定数とずれない)
func TestUsageMatchesConstants(t *testing.T) {
	for _, want := range []string{"マシン全体の " + defaultLock + ")", "既定 " + strconv.Itoa(int(defaultMaxWait.Seconds())) + "。", "rc=" + strconv.Itoa(rcTool)} {
		if !strings.Contains(usage, want) {
			t.Fatalf("usage に %q が無い:\n%s", want, usage)
		}
	}
}

// 待っている間のシグナルで抜けるときも、実行していないことを 1 行出す (rc だけだと子が失敗したのと見分けられない)
func TestSignalWhileWaitingSaysNotRun(t *testing.T) {
	f := newFixture(t)
	ga := f.gate("a")
	a := f.held("A", ga, nil)
	f.waitStarted("A")
	errPath := filepath.Join(f.dir, "b.err")
	ef, err := os.Create(errPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ef.Close()
	b := exec.Command(bin, "--", "/bin/sh", "-c", "echo B >> "+f.log)
	b.Env = f.env()
	b.Stderr = ef
	f.track(b)
	if err := b.Start(); err != nil {
		t.Fatal(err)
	}
	testwait.Until(t, "B が待ちに入る", func() bool { return strings.Contains(readFile(errPath), "待ち 1 番目") })
	if err := b.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	_ = exitCode(t, b)
	if !strings.Contains(readFile(errPath), "待ちをやめた") || f.hasLog("B") {
		t.Fatalf("シグナルで抜けたときの案内が無い:\n%s", readFile(errPath))
	}
	f.release(ga)
	_ = exitCode(t, a)
}

// 待ちの上限で抜けたときは、上限を読める形で出し、次に取れる手を添える
func TestMaxWaitMessageIsReadable(t *testing.T) {
	f := newFixture(t)
	ga := f.gate("a")
	a := f.held("A", ga, nil)
	f.waitStarted("A")
	errPath := filepath.Join(f.dir, "b.err")
	ef, err := os.Create(errPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ef.Close()
	b := exec.Command(bin, "--max-wait", "1", "--", "/usr/bin/true")
	b.Env = f.env()
	b.Stderr = ef
	if err := b.Run(); err == nil {
		t.Fatal("待ちの上限で rc=0 になった")
	}
	s := readFile(errPath)
	if !strings.Contains(s, "待ちの上限 (1 秒)") || !strings.Contains(s, "コマンドは実行していない") {
		t.Fatalf("上限切れの文言:\n%s", s)
	}
	f.release(ga)
	_ = exitCode(t, a)
}

// 他の session の cwd とコマンド行の制御文字は、status にそのまま出さない (端末の ESC シーケンスになる)
func TestStatusDropsControlCharacters(t *testing.T) {
	f := newFixture(t)
	child := startSleeper(t)
	start, _ := procStart(child)
	writeDeadTicket(t, f, ticketInfo{Pid: 999999, Start: time.Now(), Cwd: "/x/\x1b]0;PWNED\x07", Cmd: "make\x1btest", ChildPid: child, ChildStartUTC: start, V: ticketVersion})
	_, out, _ := f.status()
	if strings.ContainsAny(out, "\x1b\x07") || !strings.Contains(out, "/x/?]0;PWNED?") {
		t.Fatalf("制御文字が status に出た: %q", out)
	}
}

func TestFmtDurHours(t *testing.T) {
	for d, want := range map[time.Duration]string{5 * time.Second: "5 秒", 90 * time.Second: "1 分 30 秒", time.Hour + 5*time.Minute: "1 時間 05 分"} {
		if got := fmtDur(d); got != want {
			t.Errorf("fmtDur(%v) = %q want %q", d, got, want)
		}
	}
}

// 先頭が孤児 (持ち主の serial-run だけが死んで子が走っている) になったら、順番が変わらなくても周期の行で知らせる
func TestWaitLineReportsHeadBecomingOrphan(t *testing.T) {
	f := newFixture(t)
	ga := f.gate("a")
	a := f.held("A", ga, nil)
	waitChildRecorded(t, f)
	errPath := filepath.Join(f.dir, "b.err")
	ef, err := os.Create(errPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ef.Close()
	b := exec.Command(bin, "--", "/bin/sh", "-c", "echo B >> "+f.log)
	b.Env = f.env(tickEnv("SERIAL_RUN_TEST_REPORT", reportTick), "SERIAL_RUN_TEST_RECHECK=50ms")
	b.Stderr = ef
	f.track(b)
	if err := b.Start(); err != nil {
		t.Fatal(err)
	}
	testwait.Until(t, "B が待ちに入る", func() bool { return strings.Contains(readFile(errPath), "待ち 1 番目") })
	// A の serial-run だけを殺す (子は同じグループに居るが、pid 宛てなので生き残る)
	if err := a.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = a.Wait()
	testwait.Until(t, "孤児になった先頭を待ちの行で知らせる", func() bool { return strings.Contains(readFile(errPath), "がまだ走っている") })
	f.release(ga)
	if rc := exitCode(t, b); rc != 0 {
		t.Fatalf("B rc=%d", rc)
	}
}

// 引数がフラグでないのに -- が無いときは「フラグ」と言わない
func TestMissingDashDashMessage(t *testing.T) {
	var errb strings.Builder
	cmd := exec.Command(bin, "/bin/echo", "-h")
	cmd.Stderr = &errb
	_ = cmd.Run()
	if !strings.Contains(errb.String(), "コマンドは -- の後に書く: /bin/echo") {
		t.Fatalf("stderr: %q", errb.String())
	}
}

// 先頭が長く持っているときは、最初に注意の全文を 1 回出し、その後の周期の行は短い 🚨 だけにする。
// 長く持っているかは文面から読まない (先頭のコマンド行に 🚨 があっても、長く持っていると言わない)
func TestWaitLineLongHoldAndEmojiInCommand(t *testing.T) {
	for _, c := range []struct {
		name     string
		runStart time.Duration
		cmd      string
		long     bool
	}{
		{"長く持っている", longHold + time.Minute, "make test", true},
		{"コマンド行に 🚨", time.Minute, "echo 🚨xx", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			q, err := openQueue(filepath.Join(f.state, defaultLock))
			if err != nil {
				t.Fatal(err)
			}
			// この test が札の flock を持つ (別のプロセスから見ると生きている先頭)。pid は test の外のプロセスにする
			// (test 自身の pid だと、B は持ち主を自分の祖先と読み、入れ子として待たずに走る)
			now := time.Now()
			tk, err := q.enqueue(ticketInfo{Pid: startSleeper(t), Start: now.Add(-c.runStart - time.Minute), RunStart: now.Add(-c.runStart), Cwd: "/x", Cmd: c.cmd}, func() error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			errPath := filepath.Join(f.dir, "b.err")
			ef, err := os.Create(errPath)
			if err != nil {
				t.Fatal(err)
			}
			defer ef.Close()
			b := exec.Command(bin, "--", "/usr/bin/true")
			b.Env = f.env(tickEnv("SERIAL_RUN_TEST_REPORT", reportTick))
			b.Stderr = ef
			f.track(b)
			if err := b.Start(); err != nil {
				t.Fatal(err)
			}
			testwait.Until(t, "周期の行が 3 行以上出る", func() bool { return strings.Count(readFile(errPath), "待ち 1 番目") >= 3 })
			tk.release(nil)
			_ = exitCode(t, b)
			lines := strings.Split(strings.TrimSpace(readFile(errPath)), "\n")
			full, short := strings.Count(lines[0], "🚨 長く持っている (pid"), strings.Contains(lines[1], "🚨 先頭が長く持っている")
			if c.long && (full != 1 || !short || strings.Contains(lines[1], "(pid")) {
				t.Fatalf("長く持っている注意の出し方が違う:\n%s", readFile(errPath))
			}
			if !c.long && (strings.Contains(lines[0], "長く持っている") || strings.Contains(lines[1], "長く持っている")) {
				t.Fatalf("コマンド行の 🚨 を長く持っていると読んだ:\n%s", readFile(errPath))
			}
		})
	}
}

// status の hook.log の行は、欄の区切りのタブを残したまま制御文字だけを落とす
func TestStatusHookLogKeepsTabs(t *testing.T) {
	f := newFixture(t)
	c := exec.Command(bin, "--", "/usr/bin/true") // 置き場を作る
	c.Env = f.env()
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hookLogPath(f.state), []byte("2026-10-23T10:00:00+09:00\twrapped\tmake\x1btest\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, out, _ := f.status()
	if !strings.Contains(out, "  2026-10-23T10:00:00+09:00\twrapped\tmake?test") {
		t.Fatalf("hook.log の行: %q", out)
	}
}

// TERM を受けたら子孫ごと止め、serial-run 自身も TERM で死ぬ (rc=143 で返ると、呼び出し元の bash は「子がシグナルを処理した」と読む)
func TestTermStopsDescendantsAndDiesBySignal(t *testing.T) {
	f := newFixture(t)
	g := f.gate("g")
	gpidFile := filepath.Join(f.dir, "grandchild.pid")
	cmd := f.start(nil, "--", "/bin/sh", "-c", "/bin/sh -c 'echo $$ > "+gpidFile+"; while :; do read _ < "+g+"; done' & wait")
	testwait.Until(t, "孫の pid", func() bool { return strings.HasSuffix(readFile(gpidFile), "\n") })
	gpid, err := strconv.Atoi(strings.TrimSpace(readFile(gpidFile)))
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	_ = exitCode(t, cmd)
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
		t.Fatalf("serial-run が TERM で死んでいない: %v", cmd.ProcessState)
	}
	testwait.Until(t, "孫が止まる", func() bool { return gone(gpid) })
}

// 待っている間の TERM で、札を消して抜ける (札を残すと後ろが待つ)
func TestTermWhileWaitingRemovesTicket(t *testing.T) {
	f := newFixture(t)
	ga := f.gate("a")
	a := f.held("A", ga, nil)
	f.waitStarted("A")
	b := f.start(nil, "--", "/bin/sh", "-c", "echo B >> "+f.log)
	f.waitTickets(2)
	if err := b.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if rc := exitCode(t, b); rc != 128+int(syscall.SIGTERM) {
		t.Fatalf("B rc=%d", rc)
	}
	if n := f.ticketCount(); n != 1 || f.hasLog("B") {
		t.Fatalf("TERM で抜けた B の札が残った / B が走った: 札 %d 枚", n)
	}
	f.release(ga)
	_ = exitCode(t, a)
}

// 前に札が無くても run.lock を持つ者がいれば待ち、待ちの上限はその待ちにも効く
func TestMaxWaitAppliesWhileWaitingForRunLock(t *testing.T) {
	f := newFixture(t)
	q, err := openQueue(filepath.Join(f.state, defaultLock))
	if err != nil {
		t.Fatal(err)
	}
	rf, err := os.OpenFile(q.runPath(), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer rf.Close()
	if err := syscall.Flock(int(rf.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	b := f.start(nil, "--max-wait", "1", "--", "/bin/sh", "-c", "echo B >> "+f.log)
	if rc := exitCode(t, b); rc != rcTool || f.hasLog("B") {
		t.Fatalf("run.lock の待ちに上限が効かない: rc=%d", rc)
	}
}

// 作りかけの札の残り (持ち主が rename の前に死んだ .tmp-) は、古ければ待ち手が消す。新しいものは消さない (作っている最中かもしれない)
func TestStaleTmpTicketIsSwept(t *testing.T) {
	f := newFixture(t)
	c := exec.Command(bin, "--", "/usr/bin/true") // 置き場を作る
	c.Env = f.env()
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	old, fresh := filepath.Join(f.qdir(), tmpPrefix+"old"), filepath.Join(f.qdir(), tmpPrefix+"fresh")
	for _, p := range []string{old, fresh} {
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-staleTmp - time.Second)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	if rc := exitCode(t, f.start(nil, "--", "/usr/bin/true")); rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("古い作りかけの札が残った: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("新しい作りかけの札を消した: %v", err)
	}
}

// history.log の rc は子の rc
func TestHistoryRecordsChildRC(t *testing.T) {
	f := newFixture(t)
	if rc := exitCode(t, f.start(nil, "--", "/bin/sh", "-c", "exit 3")); rc != 3 {
		t.Fatalf("rc=%d", rc)
	}
	lines := strings.Split(strings.TrimSpace(readFile(queue{dir: filepath.Join(f.state, defaultLock)}.historyPath())), "\n")
	if !strings.Contains(lines[len(lines)-1], "\trc=3\t") {
		t.Fatalf("history.log の rc: %q", lines[len(lines)-1])
	}
}

// SERIAL_RUN_HELD が指す札が、待っている札 (前に札がある) なら入れ子として素通しせず並ぶ (待っているだけの人の子は順番を持っていない)
func TestHeldTokenOfWaitingTicketDoesNotBypass(t *testing.T) {
	f := newFixture(t)
	ga, gb := f.gate("a"), f.gate("b")
	a := f.held("A", ga, nil)
	f.waitStarted("A")
	b := f.held("B", gb, nil)
	f.waitTickets(2)
	q, err := openQueue(filepath.Join(f.state, defaultLock))
	if err != nil {
		t.Fatal(err)
	}
	info, ok := q.readInfo(2)
	if !ok {
		t.Fatal("B の札を読めない")
	}
	c := f.start([]string{heldEnv + "=" + heldToken(q.dir, 2, info.Nonce)}, "--max-wait", "1", "--", "/bin/sh", "-c", "echo C >> "+f.log)
	if rc := exitCode(t, c); rc != rcTool || f.hasLog("C") {
		t.Fatalf("待っている札の SERIAL_RUN_HELD で素通しした: rc=%d", rc)
	}
	f.release(ga)
	_ = exitCode(t, a)
	f.waitStarted("B")
	f.release(gb)
	_ = exitCode(t, b)
}

// gone は pid のプロセスがもう居ないか (居ない・ゾンビ)。ps が失敗したら「居ない」と読む (止まったことを待つ・生存を確かめる検査では、
// 緑の側に倒れる。ps が失敗する環境では、止まらない退行を見逃しうる)
func gone(pid int) bool { return syscall.Kill(pid, 0) != nil || zombie(pid) }

// run.lock を待っている間の TERM でも、待ちをやめて TERM で死ぬ (run.lock の fd を閉じようとして止まらない)
func TestTermWhileWaitingForRunLock(t *testing.T) {
	f := newFixture(t)
	q, err := openQueue(filepath.Join(f.state, defaultLock))
	if err != nil {
		t.Fatal(err)
	}
	rf, err := os.OpenFile(q.runPath(), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer rf.Close()
	if err := syscall.Flock(int(rf.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	errPath := filepath.Join(f.dir, "b.err")
	ef, err := os.Create(errPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ef.Close()
	b := exec.Command(bin, "--", "/bin/sh", "-c", "echo B >> "+f.log)
	b.Env = f.env(tickEnv("SERIAL_RUN_TEST_REPORT", reportTick))
	b.Stderr = ef
	f.track(b)
	if err := b.Start(); err != nil {
		t.Fatal(err)
	}
	testwait.Until(t, "B が run.lock を待つ", func() bool { return strings.Contains(readFile(errPath), "実行権を待っている") })
	if err := b.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if rc := exitCode(t, b); rc != 128+int(syscall.SIGTERM) || f.hasLog("B") {
		t.Fatalf("run.lock の待ちで TERM を受けた B: rc=%d", rc)
	}
}

// gitRepo は t の一時 dir に git の repo を作り、そのパスを返す (repo ごとの列のテスト用)
func gitRepo(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	for _, args := range [][]string{{"init", "-q", dir}, {"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x"}} {
		cmd := exec.Command("git", args...)
		cmd.Env = gitTestEnv()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

// gitTestEnv は継承した git の repo の場所 (GIT_DIR など) を外した環境 (テストの git が外側の repo を触らない)
func gitTestEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	return env
}

// startIn は dir を cwd にして、既定の範囲 (repo ごと) の serial-run を起こす
func (f *fixture) startIn(dir string, extraEnv []string, args ...string) *exec.Cmd {
	f.t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	// 元の環境の範囲の指定 (テストの既定の machine) と git の repo の場所だけを外し、渡された extraEnv はそのまま足す
	var env []string
	for _, kv := range f.env() {
		if !strings.HasPrefix(kv, scopeEnv+"=") && !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, extraEnv...)
	f.track(cmd)
	if err := cmd.Start(); err != nil {
		f.t.Fatal(err)
	}
	return cmd
}

// 既定では repo ごとの列: 別の repo のコマンドは同時に走り、同じ repo (別の worktree を含む) のコマンドは 1 本ずつ
func TestRepoScopeSerializesPerRepo(t *testing.T) {
	f := newFixture(t)
	r1, r2 := gitRepo(t, "one"), gitRepo(t, "two")
	wt := filepath.Join(t.TempDir(), "one-wt")
	cmd := exec.Command("git", "-C", r1, "worktree", "add", "-q", "--detach", wt)
	cmd.Env = gitTestEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worktree: %v\n%s", err, out)
	}
	ga := f.gate("a")
	a := f.startIn(r1, nil, "--", "/bin/sh", "-c", "echo start A >> "+f.log+"; read _ < "+ga)
	f.waitStarted("A")
	// 別の repo は待たずに走る
	if rc := exitCode(t, f.startIn(r2, nil, "--max-wait", "5", "--", "/bin/sh", "-c", "echo B >> "+f.log)); rc != 0 || !f.hasLog("B") {
		t.Fatalf("別の repo のコマンドが待たされた: rc=%d", rc)
	}
	// 同じ repo の別の worktree は待つ (上限で抜けて走らない)
	if rc := exitCode(t, f.startIn(wt, nil, "--max-wait", "1", "--", "/bin/sh", "-c", "echo C >> "+f.log)); rc != rcTool || f.hasLog("C") {
		t.Fatalf("同じ repo の worktree のコマンドが待たずに走った: rc=%d", rc)
	}
	f.release(ga)
	_ = exitCode(t, a)
	if _, err := os.Stat(filepath.Join(f.state, defaultLock)); !os.IsNotExist(err) {
		t.Fatalf("repo ごとの範囲なのにマシン全体の列を使った: %v", err)
	}
}

// SERIAL_RUN_SCOPE=machine なら別の repo でも 1 本ずつ (マシン全体の列)
func TestMachineScopeSerializesAcrossRepos(t *testing.T) {
	f := newFixture(t)
	r1, r2 := gitRepo(t, "one"), gitRepo(t, "two")
	ga := f.gate("a")
	a := f.startIn(r1, []string{scopeEnv + "=machine"}, "--", "/bin/sh", "-c", "echo start A >> "+f.log+"; read _ < "+ga)
	f.waitStarted("A")
	if rc := exitCode(t, f.startIn(r2, []string{scopeEnv + "=machine"}, "--max-wait", "1", "--", "/bin/sh", "-c", "echo B >> "+f.log)); rc != rcTool || f.hasLog("B") {
		t.Fatalf("マシン全体の範囲で別の repo のコマンドが待たずに走った: rc=%d", rc)
	}
	f.release(ga)
	_ = exitCode(t, a)
}

// 範囲の値を間違えても、包んだコマンドは走らせる (警告してマシン全体の列に並ぶ。125 で落とすと包んだテストが落ちたと見える)
func TestScopeUnknownValueWarnsAndUsesMachine(t *testing.T) {
	f := newFixture(t)
	errPath := filepath.Join(f.dir, "x.err")
	ef, err := os.Create(errPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ef.Close()
	x := exec.Command(bin, "--", "/bin/sh", "-c", "echo X >> "+f.log)
	x.Dir = gitRepo(t, "one")
	x.Env = append(f.env(), scopeEnv+"=Machine")
	x.Stderr = ef
	f.track(x)
	if err := x.Start(); err != nil {
		t.Fatal(err)
	}
	if rc := exitCode(t, x); rc != 0 || !f.hasLog("X") || !strings.Contains(readFile(errPath), "マシン全体の列に並ぶ") {
		t.Fatalf("範囲の誤りで走らなかった / 警告が無い: rc=%d err=%q", rc, readFile(errPath))
	}
	if _, err := os.Stat(filepath.Join(f.state, defaultLock)); err != nil {
		t.Fatalf("マシン全体の列に並んでいない: %v", err)
	}
}

// 別の repo へ cd して打った serial-run は、外側の列を持ったまま別の列を待たずに素通しにする
// (待つと、逆向きの入れ子と組んで互いに待ち合い、両方が待ちの上限まで止まる)
func TestNestedInOtherRepoPassesThrough(t *testing.T) {
	f := newFixture(t)
	r1, r2 := gitRepo(t, "one"), gitRepo(t, "two")
	// r2 の列を誰かが持っている
	gb := f.gate("b")
	b := f.startIn(r2, nil, "--", "/bin/sh", "-c", "echo start B >> "+f.log+"; read _ < "+gb)
	f.waitStarted("B")
	// r1 の列を取った中から r2 へ cd して serial-run を打つ: 中は素通しで、r2 の持ち主を待たない
	inner := bin + " --max-wait 1 -- /bin/sh -c 'echo inner >> " + f.log + "'"
	if rc := exitCode(t, f.startIn(r1, nil, "--", "/bin/sh", "-c", "cd "+r2+" && "+inner)); rc != 0 || !f.hasLog("inner") {
		t.Fatalf("別の repo への入れ子が素通しにならなかった: rc=%d", rc)
	}
	// 環境変数を落とした入れ子 (祖先のプロセスで見る) も同じ
	inner2 := "env -u " + heldEnv + " " + bin + " --max-wait 1 -- /bin/sh -c 'echo inner2 >> " + f.log + "'"
	if rc := exitCode(t, f.startIn(r1, nil, "--", "/bin/sh", "-c", "cd "+r2+" && "+inner2)); rc != 0 || !f.hasLog("inner2") {
		t.Fatalf("環境変数を落とした別の repo への入れ子が素通しにならなかった: rc=%d", rc)
	}
	f.release(gb)
	_ = exitCode(t, b)
}

// 継承した GIT_DIR (相対・別の綴り) で、同じ repo が別の列にならない (repo は cwd だけで決める)
func TestRepoLockIgnoresInheritedGitDir(t *testing.T) {
	r := gitRepo(t, "one")
	sub := filepath.Join(r, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(scopeEnv, "repo")
	want, err := lockFor(r)
	if err != nil || want == defaultLock {
		t.Fatalf("repo の列にならない: %q %v", want, err)
	}
	t.Setenv("GIT_DIR", ".git")
	if got, _ := lockFor(sub); got != want {
		t.Fatalf("相対の GIT_DIR を継承したサブディレクトリで列が変わった: %q want %q", got, want)
	}
}

// repo ごとの列の名前は dir 名を読める形にし、同じ dir 名の別の repo を hash で分ける
func TestRepoLockName(t *testing.T) {
	a, b := repoLockName("/x/My Repo/.git"), repoLockName("/y/My Repo/.git")
	if !strings.HasPrefix(a, "repo-my-repo-") || a == b || !lockNameRe.MatchString(a) {
		t.Fatalf("lock 名: %q %q", a, b)
	}
}

// 列を持ったまま別の列を待たない: 名指しの列 (-l) と範囲の列 (repo ごと) を逆の順で入れ子にした 2 本が、互いに待ち合わずに両方走る
func TestNestedNamedAndScopeLocksDoNotDeadlock(t *testing.T) {
	f := newFixture(t)
	r := gitRepo(t, "one")
	gx, gy := f.gate("x"), f.gate("y")
	// X: sim の列を持ち、gate の後で repo の列のコマンドを打つ / Y: repo の列を持ち、gate の後で sim の列のコマンドを打つ
	x := f.startIn(r, nil, "-l", "sim", "--", "/bin/sh", "-c", "echo start X >> "+f.log+"; read _ < "+gx+"; "+bin+" --max-wait 3 -- /bin/sh -c 'echo inner-x >> "+f.log+"'")
	f.waitStarted("X")
	y := f.startIn(r, nil, "--", "/bin/sh", "-c", "echo start Y >> "+f.log+"; read _ < "+gy+"; "+bin+" -l sim --max-wait 3 -- /bin/sh -c 'echo inner-y >> "+f.log+"'")
	f.waitStarted("Y")
	f.release(gx)
	f.release(gy)
	if rx, ry := exitCode(t, x), exitCode(t, y); rx != 0 || ry != 0 || !f.hasLog("inner-x") || !f.hasLog("inner-y") {
		t.Fatalf("名指しの列と範囲の列の入れ子が互いに待ち合った: X rc=%d Y rc=%d\n%s", rx, ry, f.logText())
	}
}

// 列をまたぐ素通しは、持ち主が生きている先頭だけで見る。確かめられない孤児の札 (子の pid が自分の祖先と一致しうる) では素通しにしない
func TestUnverifiedOrphanInOtherLockDoesNotBypass(t *testing.T) {
	f := newFixture(t)
	r := gitRepo(t, "one")
	// 別の範囲の列に、子の pid がこの test のプロセス (serial-run の祖先) を指す、開始時刻の無い孤児の札を置く
	other := queue{dir: filepath.Join(f.state, "repo-zzz-00000000")}
	if err := os.MkdirAll(other.qdir(), 0o700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(ticketInfo{Pid: 999999, Start: time.Now().Add(-time.Hour), ChildPid: os.Getpid()})
	if err := os.WriteFile(other.ticketPath(1), b, 0o600); err != nil {
		t.Fatal(err)
	}
	ga := f.gate("a")
	a := f.startIn(r, nil, "--", "/bin/sh", "-c", "echo start A >> "+f.log+"; read _ < "+ga)
	f.waitStarted("A")
	if rc := exitCode(t, f.startIn(r, nil, "--max-wait", "1", "--", "/bin/sh", "-c", "echo B >> "+f.log)); rc != rcTool || f.hasLog("B") {
		t.Fatalf("別の列の確かめられない孤児の札で素通しになった: rc=%d", rc)
	}
	f.release(ga)
	_ = exitCode(t, a)
}

// 名指しの列 (-l) の中から範囲の列のコマンドを打つと、その範囲の列を他の人が持っていても待たずに素通しになる (列を持ったまま待たない)
func TestNestedFromNamedLockPassesThrough(t *testing.T) {
	f := newFixture(t)
	r := gitRepo(t, "one")
	gz := f.gate("z")
	z := f.startIn(r, nil, "--", "/bin/sh", "-c", "echo start Z >> "+f.log+"; read _ < "+gz)
	f.waitStarted("Z")
	inner := bin + " --max-wait 1 -- /bin/sh -c 'echo inner >> " + f.log + "'"
	if rc := exitCode(t, f.startIn(r, nil, "-l", "sim", "--", "/bin/sh", "-c", inner)); rc != 0 || !f.hasLog("inner") {
		t.Fatalf("名指しの列の中からの範囲の列のコマンドが、列を持ったまま待った: rc=%d", rc)
	}
	f.release(gz)
	_ = exitCode(t, z)
}
