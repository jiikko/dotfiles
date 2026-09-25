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
	"pro-con/live"
	"pro-con/store"
)

// closeCard は PG が終えた (review) カードを PM が閉じる (close) 依頼を置く。
func closeCard(t *testing.T, dir, id string) {
	t.Helper()
	for _, kind := range []string{"review", "close"} {
		if _, err := store.Submit(dir, store.Request{Kind: kind, CardID: id}); err != nil {
			t.Fatal(err)
		}
	}
}

func lastHistory(c card.Card) string {
	if len(c.History) == 0 {
		return ""
	}
	return c.History[len(c.History)-1].Text
}

// 閉じたカードの PG だけを止める (ほかのカードの PG・pro-con が起動していない session には触らない)。止めたら印を外して履歴に書く。
func TestCloseStopsOnlyThatCardsPG(t *testing.T) {
	r := newCrashRig(t) // C-001 が作業中・登録済み
	planned(t, r.dir, 1)
	r.tick(t) // C-002 を起動
	r.ss = append(r.ss,
		agents.Session{ID: "id-pc-c-002", SessionID: "S2", PID: 52, Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-002", StartedAt: t0.Add(time.Second).UnixMilli()},
		agents.Session{ID: "other01", SessionID: "X", PID: 77, Kind: "background", State: "working"})
	r.tick(t) // C-002 を登録
	closeCard(t, r.dir, "C-001")
	r.tick(t)
	if !slices.Equal(r.l.stops, []string{"id-pc-c-001"}) {
		t.Fatalf("閉じたカードの PG だけを止めていない: %v", r.l.stops)
	}
	c := states(t, r.dir)["C-001"]
	if c.State != card.Done || c.StopAfterClose || !strings.Contains(lastHistory(c), "PG の session を止めた") {
		t.Fatalf("止めた後の形が違う: %v StopAfterClose=%v 履歴=%q", c.State, c.StopAfterClose, lastHistory(c))
	}
	r.tick(t)
	if len(r.l.stops) != 1 {
		t.Fatalf("止め終えたカードの PG を次の Tick でまた止めた: %v", r.l.stops)
	}
}

// 再開で入れ替わった前の session (sessions-retired.json) が生きていれば、それも止める。
func TestCloseStopsRetiredSession(t *testing.T) {
	r := newCrashRig(t)
	if err := live.ReplaceCard(filepath.Join(r.dir, live.RegistryFile), live.Owned{SessionID: "S2", ID: "id-new", PID: 43, CardID: "C-001"}); err != nil {
		t.Fatal(err)
	}
	r.ss = append(r.ss, agents.Session{ID: "id-new", SessionID: "S2", PID: 43, Kind: "background"})
	setCard(t, r.dir, "C-001", func(c *card.Card) { c.Session = "id-new" })
	closeCard(t, r.dir, "C-001")
	r.tick(t)
	if !slices.Contains(r.l.stops, "id-pc-c-001") || !slices.Contains(r.l.stops, "id-new") {
		t.Fatalf("入れ替わった前の session / 今の session を止めない: %v", r.l.stops)
	}
}

// 止めるのに失敗しても close は成立させる。closeStopWait の間は Tick ごとに止め直し、過ぎたら失敗を履歴に書いて諦める。
func TestCloseStopFailureKeepsCloseAndIsRecorded(t *testing.T) {
	r := newCrashRig(t)
	r.l.stopFail = true
	now := t0
	r.d.Now = func() time.Time { return now }
	closeCard(t, r.dir, "C-001")
	r.tick(t)
	r.tick(t)
	c := states(t, r.dir)["C-001"]
	if c.State != card.Done || !c.StopAfterClose || len(r.l.stopTries) < 2 {
		t.Fatalf("止められない間は close のまま止め直し続ける形になっていない: %v StopAfterClose=%v tries=%v", c.State, c.StopAfterClose, r.l.stopTries)
	}
	now = t0.Add(closeStopWait)
	r.tick(t)
	c = states(t, r.dir)["C-001"]
	if c.State != card.Done || c.StopAfterClose || !strings.Contains(lastHistory(c), "止められない") {
		t.Fatalf("諦めたときに失敗を履歴に書かない / 印が残る: %v StopAfterClose=%v 履歴=%q", c.State, c.StopAfterClose, lastHistory(c))
	}
	tries := len(r.l.stopTries)
	r.tick(t)
	if len(r.l.stopTries) != tries {
		t.Fatal("諦めた後も止め続ける")
	}
}

// 止めたと返っても一覧で止まっていなければ、止まったとは書かない (判定は終了のときと同じ agents.Session.Stopped)。
// 止める要求を出した後の Tick で止まったのを見たら「止めた」と書く (既に止まっていた、と取り違えない)。
func TestCloseChecksStoppedInList(t *testing.T) {
	r := newCrashRig(t)
	r.d.ListAll = func(ctx context.Context) ([]agents.Session, error) { return r.d.List(ctx) } // 止めても一覧がすぐには変わらない
	closeCard(t, r.dir, "C-001")
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; !c.StopAfterClose {
		t.Fatalf("止まっていない PG を止めた扱いにした: 履歴=%q", lastHistory(c))
	}
	r.ss[0].PID, r.ss[0].State = 0, agents.StateStopped // 止める要求が効いて止まった
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.StopAfterClose || c.StopSent || !strings.Contains(lastHistory(c), "PG の session を止めた") {
		t.Fatalf("止めた PG の履歴が違う / 印が残る: %v %v %q", c.StopAfterClose, c.StopSent, lastHistory(c))
	}
}

// 閉じたときに PG が既に止まっていれば、止める要求を出さず「既に止まっていた」と書く。
func TestCloseRecordsAlreadyStoppedPG(t *testing.T) {
	r := newCrashRig(t)
	r.ss[0].PID, r.ss[0].State = 0, agents.StateStopped
	closeCard(t, r.dir, "C-001")
	r.tick(t)
	c := states(t, r.dir)["C-001"]
	if len(r.l.stopTries) != 0 || c.StopAfterClose || !strings.Contains(lastHistory(c), "既に止まっていた") {
		t.Fatalf("既に止まっていた PG の扱いが違う: tries=%v %v %q", r.l.stopTries, c.StopAfterClose, lastHistory(c))
	}
}

// PG を起動していないカード (依頼の列から閉じた) は止めるものが無い。
func TestCloseWithoutPGStopsNothing(t *testing.T) {
	dir := t.TempDir()
	if _, err := store.Submit(dir, store.Request{Kind: "add", Title: "t", Repo: "dotfiles"}); err != nil {
		t.Fatal(err)
	}
	l := &fakeLauncher{}
	d := newDispatcher(t, dir, l, nil)
	d.Limit = 0
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Submit(dir, store.Request{Kind: "close", CardID: "C-001", Ending: card.EndAnswered}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := states(t, dir)["C-001"]; c.State != card.Done || c.StopAfterClose || len(l.stopTries) != 0 {
		t.Fatalf("PG の無いカードを閉じた形が違う: %v %v %v", c.State, c.StopAfterClose, l.stopTries)
	}
}
