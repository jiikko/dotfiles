package dispatcher

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/store"
)

// fakeRunner は実行を記録し、release に終了コードが来るまで終わらない。ログに 1 行書く。
type fakeRunner struct {
	dirs     []string
	commands []string
	release  chan int
	started  chan struct{} // 実行を始めた (記録を足した) 知らせ。テストはこれを待ってから dirs / commands を読む (別の goroutine で走るため)
	canceled bool          // 取り消し (ctx.Done) を見た
}

// waitStarted は n 本目の実行が始まるまで待つ (上限 5 秒)。
func (f *fakeRunner) waitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-f.started:
	case <-time.After(5 * time.Second):
		t.Fatal("実行が 5 秒たっても始まらない")
	}
}

func (f *fakeRunner) Run(ctx context.Context, dir, command, logPath, _ string) (int, error) {
	f.dirs, f.commands = append(f.dirs, dir), append(f.commands, command)
	_ = os.WriteFile(logPath, []byte("FAIL: TestFoo (0.01s)\n--- 出力の末尾 ---\n"), 0o600)
	f.started <- struct{}{}
	select {
	case rc := <-f.release:
		return rc, nil
	case <-ctx.Done():
		f.canceled = true
		return -1, ctx.Err()
	}
}

// runRig は作業中・登録済みの PG を n 本 (C-001〜) 用意し、テストの係を偽物にする。
func runRig(t *testing.T, n int) (*crashRig, *fakeRunner, *[]string) {
	t.Helper()
	r := newCrashRig(t)
	for i := 2; i <= n; i++ {
		planned(t, r.dir, 1)
		r.tick(t) // 起動
		id := "id-pc-c-00" + string(rune('0'+i))
		r.ss = append(r.ss, agents.Session{ID: id, SessionID: "S" + string(rune('0'+i)), PID: 40 + i, Kind: "background",
			Cwd: "/w/dotfiles/.claude/worktrees/pc-c-00" + string(rune('0'+i)), StartedAt: t0.Add(time.Second).UnixMilli()})
		r.tick(t) // 登録
	}
	fr := &fakeRunner{release: make(chan int, 1), started: make(chan struct{}, 4)}
	var summarized []string
	r.d.Runner = fr
	r.d.Summarize = func(_ context.Context, tail string) (string, error) {
		summarized = append(summarized, tail)
		return "TestFoo が落ちた", nil
	}
	return r, fr, &summarized
}

func askRun(t *testing.T, dir, id, cmd string, at time.Time) {
	t.Helper()
	wt := "/w/dotfiles/.claude/worktrees/pc-" + strings.ToLower(id) // そのカードの PG の worktree から頼む
	if _, err := store.Submit(dir, store.Request{Kind: "run", CardID: id, Command: cmd, Cwd: wt, At: at}); err != nil {
		t.Fatal(err)
	}
}

// waitDone は実行中の 1 本が終わる (結果が done に入る) まで Tick を回す。上限を超えたら落とす。
func waitDone(t *testing.T, r *crashRig) {
	t.Helper()
	for i := 0; r.d.active != nil && len(r.d.active.done) == 0; i++ {
		if i > 500 {
			t.Fatal("実行が 5 秒たっても終わらない")
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.tick(t)
}

// 頼まれたコマンドは PG の worktree で 1 本ずつ順に実行し、待っている方には順番を出す。成功なら要約せず、結果を渡して同じ PG を再開する。
func TestRunsSeriallyInWorktreeAndResumes(t *testing.T) {
	r, fr, summarized := runRig(t, 2)
	askRun(t, r.dir, "C-001", "make test", t0)
	askRun(t, r.dir, "C-002", "go test ./...", t0.Add(time.Second))
	r.tick(t)
	fr.waitStarted(t)
	if c := states(t, r.dir)["C-001"]; c.Exec.RunID == "" || !strings.HasPrefix(c.Exec.RunID, "C-001-") {
		t.Fatalf("実行を始める前に印を記録しない: %q", c.Exec.RunID)
	}
	cs := states(t, r.dir)
	if len(fr.commands) != 1 || fr.commands[0] != "make test" || fr.dirs[0] != "/w/dotfiles/.claude/worktrees/pc-c-001" {
		t.Fatalf("先に頼んだ方を PG の worktree で実行していない: %v %v", fr.commands, fr.dirs)
	}
	if c := cs["C-002"]; c.Wait.Kind != card.WaitResource || c.Wait.Position != 1 || !cs["C-001"].Exec.Active() {
		t.Fatalf("後の方に順番を出さない / 実行中を記録しない: %+v exec=%v", c.Wait, cs["C-001"].Exec)
	}
	r.tick(t) // 1 本目の実行中に Tick が回っても、2 本目は始めない
	if len(fr.commands) != 1 {
		t.Fatalf("実行中に 2 本目を始めた (直列でない): %v", fr.commands)
	}
	fr.release <- 0
	waitDone(t, r)
	fr.waitStarted(t) // 2 本目が始まった
	c := states(t, r.dir)["C-001"]
	if len(r.l.resumes) != 1 || !strings.Contains(r.l.resumes[0], "rc=0") || !strings.Contains(r.l.resumes[0], "成功") || len(*summarized) != 0 {
		t.Fatalf("成功の結果を渡して再開しない / 成功なのに要約した: resumes=%v summarized=%d", r.l.resumes, len(*summarized))
	}
	if c.Run != "" || c.Exec.Active() || c.State != card.Running {
		t.Fatalf("結果を渡した後も頼みが残る: Run=%q exec=%v %v", c.Run, c.Exec, c.State)
	}
	if len(fr.commands) != 2 || fr.commands[1] != "go test ./..." {
		t.Fatalf("前の実行が終わった後に次を始めない: %v", fr.commands)
	}
}

// 失敗したら要約させ、要約とログの末尾を渡す。要約できなければログの末尾だけ渡す。
func TestRunFailureIsSummarized(t *testing.T) {
	for _, summarizeFails := range []bool{false, true} {
		r, fr, _ := runRig(t, 1)
		if summarizeFails {
			r.d.Summarize = func(context.Context, string) (string, error) { return "", errors.New("haiku が落ちた") }
		}
		askRun(t, r.dir, "C-001", "make test", t0)
		r.tick(t)
		fr.release <- 2
		waitDone(t, r)
		if len(r.l.resumes) != 1 {
			t.Fatalf("失敗の結果を渡して再開しない: %v", r.l.resumes)
		}
		got := r.l.resumes[0]
		if !strings.Contains(got, "rc=2") || !strings.Contains(got, "FAIL: TestFoo") || strings.Contains(got, "TestFoo が落ちた") == summarizeFails {
			t.Fatalf("失敗の結果 (要約 / ログの末尾) が足りない (要約の失敗=%v):\n%s", summarizeFails, got)
		}
	}
}

// 実行の途中で dispatcher が止まって (実行していない dispatcher が) 実行中の記録を見たら、残ったコマンドを止めてから、結果が無いことを渡して再開する。
func TestInterruptedRunIsReported(t *testing.T) {
	r, fr, _ := runRig(t, 1)
	var killed []string
	old := killStaleFn
	killStaleFn = func(runID string) { killed = append(killed, runID) }
	t.Cleanup(func() { killStaleFn = old })
	setCard(t, r.dir, "C-001", func(c *card.Card) {
		c.Run, c.RunAt, c.Exec = "make test", t0, card.Exec{Command: "make test", Since: t0, RunID: "C-001-777.log"}
	})
	r.tick(t)
	if len(killed) != 1 || killed[0] != "C-001-777.log" {
		t.Fatalf("前の dispatcher が残した実行を止めにいかない: %v", killed)
	}
	if len(fr.commands) != 0 || len(r.l.resumes) != 1 || !strings.Contains(r.l.resumes[0], "結果が無い") {
		t.Fatalf("途中で止まった実行を知らせて再開しない: commands=%v resumes=%v", fr.commands, r.l.resumes)
	}
}

// テストの係の実行を待っている間は、PG の進み具合を見ない (長いテストを停滞にしない)。
func TestWatchdogSkipsCardWaitingForRun(t *testing.T) {
	r, fr, _ := runRig(t, 1)
	r.d.StallAfter = 10 * time.Minute
	askRun(t, r.dir, "C-001", "make test", t0)
	r.tick(t)
	r.d.Now = func() time.Time { return t0.Add(time.Hour) }
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.Stalled {
		t.Fatal("テストの係の実行を待っている PG を停滞にした")
	}
	fr.release <- 0
	waitDone(t, r)
}

// 終了で止めるとき、実行中のコマンドを取り消し、頼みも取り下げる (再開した PG が続きから頼み直す)。
func TestShutdownCancelsRun(t *testing.T) {
	r, fr, _ := runRig(t, 1)
	askRun(t, r.dir, "C-001", "make test", t0)
	r.tick(t)
	fr.waitStarted(t)
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := states(t, r.dir)["C-001"]
	if r.d.active != nil || c.Run != "" || c.Exec.Active() || c.State != card.Planned {
		t.Fatalf("終了で実行を取り消さない / 頼みが残る: active=%v Run=%q exec=%v %v", r.d.active != nil, c.Run, c.Exec, c.State)
	}
	if len(fr.commands) != 1 || !fr.canceled {
		t.Fatalf("実行が始まっていない / 取り消していない: %v canceled=%v", fr.commands, fr.canceled)
	}
}

// 実行を待っているカードが作業中の列を離れたら (質問した等)、実行を取り消して頼みを取り下げる。
// 回答で再開した PG に、後から届いた結果や「結果が無い」を渡して止め直さない。
func TestRunDroppedWhenCardLeavesRunning(t *testing.T) {
	r, fr, _ := runRig(t, 1)
	askRun(t, r.dir, "C-001", "make test", t0)
	r.tick(t) // 実行を始める
	fr.waitStarted(t)
	if _, err := store.Submit(r.dir, store.Request{Kind: "ask", CardID: "C-001", Question: "q"}); err != nil {
		t.Fatal(err)
	}
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.Run != "" || c.Exec.Active() || r.d.active != nil || !fr.canceled {
		t.Fatalf("質問で列を離れたのに頼みが残る / 実行を取り消さない: Run=%q exec=%v active=%v canceled=%v", c.Run, c.Exec, r.d.active != nil, fr.canceled)
	}
	if _, err := store.Submit(r.dir, store.Request{Kind: "answer", CardID: "C-001", Answer: "a"}); err != nil {
		t.Fatal(err)
	}
	r.tick(t) // 回答で再開
	r.tick(t)
	if len(r.l.resumes) != 1 || !strings.HasSuffix(r.l.resumes[0], ":a") {
		t.Fatalf("回答で再開した PG を、テストの係の結果で止め直した: %v", r.l.resumes)
	}
}

// 別のカードの worktree から頼まれた実行はしない (C-001 の PG が C-002 の名前で頼んでも、C-002 の worktree で走らせない)。
func TestRunRefusesRequestFromOtherWorktree(t *testing.T) {
	r, fr, _ := runRig(t, 1)
	if _, err := store.Submit(r.dir, store.Request{Kind: "run", CardID: "C-001", Command: "rm -rf x", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-002", At: t0}); err != nil {
		t.Fatal(err)
	}
	r.tick(t)
	if len(fr.commands) != 0 || len(r.l.resumes) != 1 || !strings.Contains(r.l.resumes[0], "worktree ではない") {
		t.Fatalf("別の worktree からの頼みを実行した / 断ったことを渡さない: %v %v", fr.commands, r.l.resumes)
	}
}

// 出力の無い失敗は要約させず、出力が無かったことを渡す (空のログを要約させると、要約の代わりに問い返しが入る)。
func TestRunFailureWithoutOutputIsNotSummarized(t *testing.T) {
	r, fr, summarized := runRig(t, 1)
	r.d.Runner = silentRunner{fr}
	askRun(t, r.dir, "C-001", "false", t0)
	r.tick(t)
	fr.release <- 1
	waitDone(t, r)
	if len(*summarized) != 0 || len(r.l.resumes) != 1 || !strings.Contains(r.l.resumes[0], "出力なし") {
		t.Fatalf("空のログを要約させた / 出力が無いことを渡さない: summarized=%d %v", len(*summarized), r.l.resumes)
	}
}

// silentRunner は何も出力しない (ログを空で作る) 実行。
type silentRunner struct{ f *fakeRunner }

func (s silentRunner) Run(ctx context.Context, dir, command, logPath, _ string) (int, error) {
	_ = os.WriteFile(logPath, nil, 0o600)
	s.f.started <- struct{}{}
	select {
	case rc := <-s.f.release:
		return rc, nil
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}

// 落ち続けて止めた (回答待ちへ送った) カードでも、テストの係への頼みを取り下げる。
func TestRunDroppedWhenCrashStopped(t *testing.T) {
	r, _, _ := runRig(t, 1)
	askRun(t, r.dir, "C-001", "make test", t0)
	r.d.Runner = nil // 実行は始めない (頼みだけ残っている形)
	r.tick(t)
	r.crash(t0.Add(time.Minute), 43)
	r.tick(t)
	r.crash(t0.Add(2*time.Minute), 44)
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.State != card.Waiting || c.Run != "" {
		t.Fatalf("落ちて止めたカードに頼みが残る: %v Run=%q", c.State, c.Run)
	}
}

// worktree の下のディレクトリから頼んだものは受け、頼んだ場所で実行する。結果を渡したら頼んだ場所の記録も消す。
func TestRunFromWorktreeSubdirectory(t *testing.T) {
	r, fr, _ := runRig(t, 1)
	sub := "/w/dotfiles/.claude/worktrees/pc-c-001/src/pro-con"
	if _, err := store.Submit(r.dir, store.Request{Kind: "run", CardID: "C-001", Command: "go test ./...", Cwd: sub, At: t0}); err != nil {
		t.Fatal(err)
	}
	r.tick(t)
	fr.waitStarted(t)
	if len(fr.dirs) != 1 || fr.dirs[0] != sub {
		t.Fatalf("worktree の下からの頼みを断った / 頼んだ場所で実行しない: %v", fr.dirs)
	}
	fr.release <- 0
	waitDone(t, r)
	if c := states(t, r.dir)["C-001"]; c.RunCwd != "" {
		t.Fatalf("結果を渡した後も頼んだ場所の記録が残る: %q", c.RunCwd)
	}
}
