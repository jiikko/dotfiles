package dispatcher

import (
	"context"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/card"
)

// PG の枠は turn の途中の PG だけを数える (issue 455)。テストの係の結果を待って idle と一覧で確かめた PG は枠を使わないので、
// 分解済みのカードを起動する。idle と確かめられない (まだ busy / 一覧に居ない) うちは数える。
func TestSlotSkipsIdlePGAwaitingRun(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status string // C-001 の PG の一覧の status。空なら一覧に居ない
		run    bool   // C-001 がテストの係に頼んでいる
		start  bool   // C-002 を起動するか
	}{
		{"結果待ちで idle", agents.StatusIdle, true, true},
		{"頼んだが turn の途中", "busy", true, false},
		{"頼んでいない idle", agents.StatusIdle, false, false},
		{"結果待ちで一覧に居ない", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			planned(t, dir, 1)
			s := agents.Session{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", Status: "busy",
				Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t0.Add(time.Second).UnixMilli()}
			ss := []agents.Session{s}
			l := &fakeLauncher{}
			d := newDispatcher(t, dir, l, nil)
			d.List = func(context.Context) ([]agents.Session, error) { return ss, nil }
			d.Limit = 1
			for range 2 { // 起動 → 登録
				if _, err := d.Tick(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			planned(t, dir, 1) // C-002
			if tc.run {
				setCard(t, dir, "C-001", func(c *card.Card) {
					c.Run, c.RunAt = "make test", t0
					c.Wait = card.Wait{Kind: card.WaitResource, Resource: "make test"}
				})
			}
			ss = nil
			if tc.status != "" {
				s.Status = tc.status
				ss = []agents.Session{s}
			}
			if _, err := d.Tick(context.Background()); err != nil {
				t.Fatal(err)
			}
			cs := states(t, dir)
			if got := cs["C-002"].State == card.Running; got != tc.start {
				t.Fatalf("C-002 を起動した=%v、期待=%v (starts=%v C-001=%v)", got, tc.start, l.starts, cs["C-001"].State)
			}
		})
	}
}

// 結果が届いて分解済みへ戻った PG は、枠が埋まっていれば空きを待つ (枠を超えて再開しない)。空いたら新しい起動より先に再開する。
func TestRunResultWaitsForSlotThenResumesFirst(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	idleS := agents.Session{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", Status: agents.StatusIdle,
		Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t0.Add(time.Second).UnixMilli()}
	l := &fakeLauncher{}
	d := newDispatcher(t, dir, l, []agents.Session{idleS})
	d.Limit = 1
	for range 2 { // 起動 → 登録
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	setCard(t, dir, "C-001", func(c *card.Card) { c.Run, c.RunAt = "make test", t0 })
	planned(t, dir, 1) // C-002: C-001 が idle で結果を待つ間に起動する
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cs := states(t, dir); cs["C-002"].State != card.Running {
		t.Fatalf("結果待ちで idle の PG が枠を使っている: C-002=%v", cs["C-002"].State)
	}
	setCard(t, dir, "C-001", func(c *card.Card) { // 結果が届いた (finishRun と同じ形)
		c.DropRun()
		c.State, c.Resume = card.Planned, "テストの係の結果: rc=0"
	})
	planned(t, dir, 1) // C-003: 新しい起動
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cs := states(t, dir); len(l.resumes) != 0 || len(l.starts) != 2 || cs["C-001"].State != card.Planned {
		t.Fatalf("枠が埋まっているのに再開・起動した: resumes=%v starts=%v", l.resumes, l.starts)
	}
	setCard(t, dir, "C-002", func(c *card.Card) { c.State = card.Review })
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cs := states(t, dir); len(l.resumes) != 1 || len(l.starts) != 2 || cs["C-001"].State != card.Running || cs["C-003"].State != card.Planned {
		t.Fatalf("空いた枠で結果を渡す再開を先にしない: resumes=%v starts=%v C-001=%v C-003=%v", l.resumes, l.starts, cs["C-001"].State, cs["C-003"].State)
	}
}
