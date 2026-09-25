package dispatcher

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/live"
)

// unregisteredRig は、作業中の C-001 の PG が一覧に出ているのに記録 (sessions.json) に取り込まれないまま launchGrace を過ぎた形を作る (issue 457)。
// broken が一覧の session を取り込まれない形に変える。取り込まなかった Tick の出来事を返す。
func unregisteredRig(t *testing.T, broken func(*agents.Session)) (*crashRig, []eventlog.Event) {
	t.Helper()
	r := &crashRig{dir: t.TempDir(), l: &fakeLauncher{}}
	planned(t, r.dir, 1)
	r.d = newDispatcher(t, r.dir, r.l, nil)
	r.d.List = func(context.Context) ([]agents.Session, error) { return r.ss, nil }
	r.tick(t) // 起動 (C-001 は作業中・Session id-pc-c-001・LaunchedAt t0)
	s := agents.Session{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t0.Add(time.Second).UnixMilli()}
	broken(&s)
	r.ss = []agents.Session{s}
	notes := r.tick(t)
	if reg, _ := live.LoadRegistry(filepath.Join(r.dir, live.RegistryFile)); len(reg) != 0 {
		t.Fatalf("前提が違う: 記録に取り込まれた %+v", reg)
	}
	if c := states(t, r.dir)["C-001"]; c.State != card.Running || c.Session != "id-pc-c-001" {
		t.Fatalf("前提が違う: %v %q", c.State, c.Session)
	}
	r.d.Now = func() time.Time { return t0.Add(launchGrace + time.Minute) }
	return r, notes
}

// unregisteredForms は、一覧に出ている PG が register に取り込まれない形 (issue 457 の発火条件)。
var unregisteredForms = []struct {
	name   string
	broken func(*agents.Session)
	why    string // 取り込まなかった理由として出来事に出る文
}{
	{"kind が background でない", func(s *agents.Session) { s.Kind = "bg" }, `kind が "bg"`},
	{"開始が最後の起動より前", func(s *agents.Session) { s.StartedAt = t0.Add(-2 * time.Second).UnixMilli() }, "最後の起動・再開より前に始まっている"},
}

// 記録に取り込まれなかった PG でも、カードの session の短い id と、そのカードの worktree の cwd が一致すれば終了で止める。
// 「既に止まっていた」とは書かない。取り込まなかった理由と、記録に無い PG を止めたことは出来事に出る。
func TestShutdownStopsUnregisteredPG(t *testing.T) {
	for _, f := range unregisteredForms {
		t.Run(f.name, func(t *testing.T) {
			r, tickNotes := unregisteredRig(t, f.broken)
			notes, err := r.d.Shutdown(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(r.l.stops, "id-pc-c-001") {
				t.Fatalf("記録に無い PG を止めない: stops=%v", r.l.stops)
			}
			c := states(t, r.dir)["C-001"]
			if h := lastHistory(c); strings.Contains(h, "既に止まっていた") || !c.Stopped || c.State != card.Planned {
				t.Fatalf("止めた PG の形が違う: %v Stopped=%v 履歴=%q", c.State, c.Stopped, h)
			}
			if !strings.Contains(joinNotes(tickNotes), f.why) || !strings.Contains(joinNotes(notes), f.why) {
				t.Fatalf("取り込まなかった理由を出来事に出さない: tick=%s shutdown=%s", joinNotes(tickNotes), joinNotes(notes))
			}
			if !strings.Contains(joinNotes(notes), "記録に無かった") {
				t.Fatalf("記録に無い PG を止めたことを出来事に出さない: %s", joinNotes(notes))
			}
		})
	}
}

// 記録に無く、pro-con が起動したと示せない (cwd がカードの worktree でない) 生きている session は止めない。
// ただし「既に止まっていた」とは書かず、名指しして失敗を返す (stop-result を ok にしない)。
func TestShutdownNamesUnprovenLiveSession(t *testing.T) {
	r, _ := unregisteredRig(t, func(s *agents.Session) { s.Kind, s.Cwd = "bg", "/w/dotfiles" })
	_, err := r.d.Shutdown(context.Background())
	if len(r.l.stopTries) != 0 {
		t.Fatalf("pro-con が起動したと示せない session を止めた: %v", r.l.stopTries)
	}
	if err == nil || !strings.Contains(err.Error(), "C-001 (id-pc-c-001") {
		t.Fatalf("記録に無い生きている session を名指ししない: %v", err)
	}
	if h := lastHistory(states(t, r.dir)["C-001"]); strings.Contains(h, "既に止まっていた") {
		t.Fatalf("止めていないのに既に止まっていたと書いた: %q", h)
	}
}

// 記録に無い PG の session が一覧で止まっていれば、既に止まっている (止める要求を出さず ok)。
func TestShutdownUnregisteredStoppedIsStopped(t *testing.T) {
	r, _ := unregisteredRig(t, func(s *agents.Session) { s.Kind, s.PID, s.State = "bg", 0, agents.StateStopped })
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.l.stopTries) != 0 || !strings.Contains(lastHistory(states(t, r.dir)["C-001"]), "既に止まっていた") {
		t.Fatalf("止まっている session の扱いが違う: tries=%v", r.l.stopTries)
	}
}

// 記録に取り込まれなかった PG も、閉じたら止める (判定は終了と同じ部品)。
func TestCloseStopsUnregisteredPG(t *testing.T) {
	for _, f := range unregisteredForms {
		t.Run(f.name, func(t *testing.T) {
			r, _ := unregisteredRig(t, f.broken)
			closeCard(t, r.dir, "C-001")
			r.tick(t)
			c := states(t, r.dir)["C-001"]
			if !slices.Contains(r.l.stops, "id-pc-c-001") || strings.Contains(lastHistory(c), "既に止まっていた") {
				t.Fatalf("閉じたカードの記録に無い PG を止めない: stops=%v 履歴=%q", r.l.stops, lastHistory(c))
			}
		})
	}
}

// 記録に無く示せない生きている session は、閉じても止めず「既に止まっていた」と書かない (名指しして止められないと書く)。
func TestCloseNamesUnprovenLiveSession(t *testing.T) {
	r, _ := unregisteredRig(t, func(s *agents.Session) { s.Kind, s.Cwd = "bg", "/w/dotfiles" })
	closeCard(t, r.dir, "C-001")
	r.tick(t)
	r.d.Now = func() time.Time { return t0.Add(launchGrace + time.Minute + closeStopWait) }
	r.tick(t)
	c := states(t, r.dir)["C-001"]
	if len(r.l.stopTries) != 0 || !strings.Contains(lastHistory(c), "止められない") || !strings.Contains(lastHistory(c), "id-pc-c-001") {
		t.Fatalf("示せない session の扱いが違う: tries=%v 履歴=%q", r.l.stopTries, lastHistory(c))
	}
}
