package daemon

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
}

func (f *fakeRunner) Run(ctx context.Context, dir, command, logPath string) (int, error) {
	f.dirs, f.commands = append(f.dirs, dir), append(f.commands, command)
	_ = os.WriteFile(logPath, []byte("FAIL: TestFoo (0.01s)\n--- 出力の末尾 ---\n"), 0o600)
	select {
	case rc := <-f.release:
		return rc, nil
	case <-ctx.Done():
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
	fr := &fakeRunner{release: make(chan int, 1)}
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
	if _, err := store.Submit(dir, store.Request{Kind: "run", CardID: id, Command: cmd, At: at}); err != nil {
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

// 実行の途中で daemon が止まって (実行していない daemon が) 実行中の記録を見たら、結果が無いことを渡して再開する。
func TestInterruptedRunIsReported(t *testing.T) {
	r, fr, _ := runRig(t, 1)
	setCard(t, r.dir, "C-001", func(c *card.Card) {
		c.Run, c.RunAt, c.Exec = "make test", t0, card.Exec{Command: "make test", Since: t0}
	})
	r.tick(t)
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
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := states(t, r.dir)["C-001"]
	if r.d.active != nil || c.Run != "" || c.Exec.Active() || c.State != card.Planned {
		t.Fatalf("終了で実行を取り消さない / 頼みが残る: active=%v Run=%q exec=%v %v", r.d.active != nil, c.Run, c.Exec, c.State)
	}
	if len(fr.commands) != 1 {
		t.Fatalf("前提: 実行が始まっていない: %v", fr.commands)
	}
}
