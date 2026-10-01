package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"pro-con/card"
	"pro-con/eventlog"
)

// 止まったかを見る間を短くした ExecRunner (実時間の 30 秒を待たない)。
func quickStop(lockman string) ExecRunner {
	return ExecRunner{Lockman: lockman, StopGrace: 200 * time.Millisecond, StopPoll: 20 * time.Millisecond}
}

// stopFromOutside は r で `wait` するコマンドを dir で走らせ、子が起きたら外から実行のグループへ SIGSTOP を送る (541 の形: 別 session の親からは SIGCONT が来ない)。
// 止め直されて返ってきた結果と子の pid を返す。止め直しの SIGTERM を受け取れた (SIGCONT で続けてから届いた) かは dir の term.got で見る。20 秒で返らなければ、グループを止めてから落とす (テストの子を残さない)。
// 🚨 コマンドの中の `kill -STOP 0` で止めると、bash だけ止まって直前に起こした子は動いたままのことがある (macOS で実測。グループの全部が止まっていないので、正しく止め直さない)
func stopFromOutside(t *testing.T, r ExecRunner, dir, runID string) (rc, child int, err error) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		rc, err = r.Run(context.Background(), dir, `trap 'echo > term.got; exit 143' TERM; echo $$ > bash.pid; sleep 60 & echo $! > child.pid; wait`, filepath.Join(t.TempDir(), "log"), runID)
	}()
	bash, child := readPid(t, filepath.Join(dir, "bash.pid")), readPid(t, filepath.Join(dir, "child.pid"))
	if err := syscall.Kill(-bash, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		// CI でだけ 10 回に 1 回ほど落ちる (手元では 55 回で 0 回)。原因が見えるまで、諦める時点のグループの様子を残す
		t.Logf("20 秒たっても返らない。bash=%d child=%d・見張りの判定 groupStopped=%v。bash のグループと child の ps:\n%s",
			bash, child, groupStopped([]int{bash}), psOf(bash, child))
		_ = syscall.Kill(-bash, syscall.SIGKILL)
		t.Fatal("グループが止まったまま、実行が終わらない")
	}
	return rc, child, err
}

// psOf は pgid のグループに居るプロセスと pid のプロセスの ps の行 (pid pgid ppid stat command)。
func psOf(pgid, pid int) string {
	out, err := exec.Command("ps", "-A", "-o", "pid=,pgid=,ppid=,stat=,command=").Output()
	if err != nil {
		return "ps が失敗した: " + err.Error()
	}
	var b strings.Builder
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && (f[1] == strconv.Itoa(pgid) || f[0] == strconv.Itoa(pid)) {
			b.WriteString(l + "\n")
		}
	}
	return b.String()
}

// 実行のグループが丸ごと止まった (SIGSTOP) ら、上限 (1 時間) を待たずに止め直し、止まっていたことを失敗として返す。止まった子も残さない。
func TestExecRunnerRestopsStoppedGroup(t *testing.T) {
	dir := t.TempDir()
	rc, child, err := stopFromOutside(t, quickStop(""), dir, "st1")
	if !errors.Is(err, errRunStopped) || rc != -1 {
		t.Fatalf("止まったグループを止まっていたとして返さない: rc=%d err=%v", rc, err)
	}
	waitGone(t, child)
	gotTerm(t, dir)
}

// gotTerm は止め直しの SIGTERM を bash の trap が受け取ったか (止まったままでは SIGTERM は届かず、猶予の後の SIGKILL で死ぬ)。
func gotTerm(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, "term.got")); err != nil {
		t.Fatal("止め直しの SIGTERM を受け取っていない (SIGCONT で続けずに SIGKILL した)")
	}
}

// lock を取って走らせたときは、lockman が動いていても (子を待っているだけ)、頼まれたコマンドのグループが止まっていれば止め直し、lock を解放する。
func TestExecRunnerLockedRestopsStoppedGroup(t *testing.T) {
	_, wt := gitRepoWithWorktree(t)
	r := quickStop(buildLockman(t))
	rc, child, err := stopFromOutside(t, r, wt, "st2")
	if !errors.Is(err, errRunStopped) || rc != -1 {
		t.Fatalf("lock を取って止まったグループを止まっていたとして返さない: rc=%d err=%v", rc, err)
	}
	waitGone(t, child)
	gotTerm(t, wt)
	if rc, err := r.Run(context.Background(), wt, "true", filepath.Join(t.TempDir(), "log"), "st3"); rc != 0 || err != nil {
		t.Fatalf("止め直した後に lock が残った (次の実行が外の使用中で待たされる): rc=%d err=%v", rc, err)
	}
}

// 一部だけ止まっている (コマンドが子を止めて自分は動いている) のは、grace を超えても止め直さない。
func TestExecRunnerKeepsPartlyStopped(t *testing.T) {
	dir := t.TempDir()
	rc, err := quickStop("").Run(context.Background(), dir, `sleep 30 & kill -STOP $!; sleep 1; kill -CONT $!; kill $!`, filepath.Join(dir, "log"), "st4")
	if rc != 0 || err != nil {
		t.Fatalf("一部だけ止まっているのを止め直した: rc=%d err=%v", rc, err)
	}
}

// 止まっているかは、先頭から見て最初に (ゾンビ以外が) 居るグループが全部 STAT T かで決める。
func TestStoppedIn(t *testing.T) {
	ps := strings.Join([]string{"  10 T+", "  10 Ts", "  20 S", "  20 T", "  30 Z", "  30 T", "  40 Z", "  50 R"}, "\n")
	for _, c := range []struct {
		pgids []int
		want  bool
	}{
		{[]int{10}, true},
		{[]int{20}, false},     // 一部だけ止まっている
		{[]int{30}, true},      // ゾンビは数えない
		{[]int{0, 10}, true},   // pgid がまだ書かれていなければ次のグループ
		{[]int{40, 10}, true},  // ゾンビだけのグループ (抜けた後) は次のグループで見る
		{[]int{50, 10}, false}, // 最初に居るグループが動いていれば、次は見ない
		{[]int{99}, false},     // 居ない
		{[]int{1}, false},
	} {
		if got := stoppedIn(ps, c.pgids); got != c.want {
			t.Errorf("stoppedIn(%v) = %v, want %v", c.pgids, got, c.want)
		}
	}
}

// 止め直した実行は失敗として PG に返し (ログの末尾つき)、出来事に残し、順番を待っていたカードの実行を始める。
func TestStoppedRunIsReportedAndQueueMoves(t *testing.T) {
	r, fr, _ := runRig(t, 2)
	askRun(t, r.dir, "C-001", "make test", t0)
	askRun(t, r.dir, "C-002", "go test ./...", t0.Add(time.Second))
	r.tick(t)
	fr.waitStarted(t)
	fr.relErr = fmt.Errorf("%w (テスト)", errRunStopped)
	fr.release <- -1
	var notes []eventlog.Event
	if !pollUntil(t, 5*time.Second, func() bool { return r.d.active == nil || len(r.d.active.done) != 0 }) {
		t.Fatal("実行が 5 秒たっても終わらない")
	}
	notes = r.tick(t)
	fr.waitStarted(t) // 2 本目が始まった
	if len(r.l.resumes) != 1 || !strings.Contains(r.l.resumes[0], "止まったまま") || !strings.Contains(r.l.resumes[0], "FAIL: TestFoo") {
		t.Fatalf("止まっていたことをログの末尾つきで PG に返さない: %v", r.l.resumes)
	}
	if !slicesContainsText(notes, eventlog.KindRun, "C-001 のコマンドが止まったまま (STAT T) 続かなかったので止め直した") {
		t.Fatalf("止め直したことが出来事に残らない: %+v", notes)
	}
	if c := states(t, r.dir)["C-002"]; len(fr.commands) != 2 || !c.Exec.Active() || c.Wait.Kind == card.WaitResource {
		t.Fatalf("止め直した後に次の実行を始めない: %v exec=%v wait=%+v", fr.commands, c.Exec, c.Wait)
	}
}

func slicesContainsText(notes []eventlog.Event, kind, text string) bool {
	for _, n := range notes {
		if n.Kind == kind && strings.Contains(n.Reason, text) {
			return true
		}
	}
	return false
}

// 前の実行が残した pgid の置き場を、この実行の bash が書く前に読まない: 残った値のグループが止まっていても、この実行を止め直さず、そのグループも撃たない。
func TestExecRunnerIgnoresLeftoverPgidFile(t *testing.T) {
	_, wt := gitRepoWithWorktree(t)
	other := exec.Command("sleep", "60")
	other.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-other.Process.Pid, syscall.SIGKILL); _ = other.Wait() })
	if err := syscall.Kill(-other.Process.Pid, syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "log")
	if err := os.WriteFile(log+".pgid", []byte(strconv.Itoa(other.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	// lockman が bash を起こす前 (pgid がまだ書かれていない間) に見張りが残った値を読むと、止まっているとして取り消す
	r := ExecRunner{Lockman: buildLockman(t), StopGrace: 20 * time.Millisecond, StopPoll: 5 * time.Millisecond}
	if rc, err := r.Run(context.Background(), wt, "sleep 0.5", log, "st5"); rc != 0 || err != nil {
		t.Fatalf("残った pgid の置き場を読んで止め直した: rc=%d err=%v", rc, err)
	}
	if !alive(other.Process.Pid) {
		t.Fatal("残った pgid のグループ (別のもの) を撃った")
	}
}
