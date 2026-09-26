package dispatcher

import (
	"slices"
	"strings"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/store"
)

// レビューの列に入ったカードの PG の session を止める (issue 536)。

// reviewRig は C-001 の PG が turn を終えて (idle) `card review` を打った形を作る。
func reviewRig(t *testing.T) *crashRig {
	t.Helper()
	r := newCrashRig(t)
	r.ss[0].Status = agents.StatusIdle
	submit(t, r.dir, store.Request{Kind: "review", CardID: "C-001"})
	return r
}

// stopInList は止めた session を一覧から消す (本物の `claude agents --json` は止めた session を出さない。--all は listAll が stopped で重ねる)。
func (r *crashRig) stopInList() {
	r.ss = slices.DeleteFunc(r.ss, func(s agents.Session) bool { return s.ID == "id-pc-c-001" })
}

// idle の PG はレビューの列に入ったら止め、列はレビュー待ちのまま Stopped を付けて履歴に書く。止め終えたら止め直さない。
func TestReviewStopsIdlePG(t *testing.T) {
	r := reviewRig(t)
	r.tick(t)
	if !slices.Equal(r.l.stops, []string{"id-pc-c-001"}) {
		t.Fatalf("レビュー待ちの PG を止めていない: %v", r.l.stops)
	}
	c := states(t, r.dir)["C-001"]
	if c.State != card.Review || c.StopAfterClose || !c.Stopped || !strings.Contains(lastHistory(c), "レビュー待ちの間は PG の session を止めた") {
		t.Fatalf("止めた後の形が違う: %v StopAfterClose=%v Stopped=%v 履歴=%q", c.State, c.StopAfterClose, c.Stopped, lastHistory(c))
	}
	r.stopInList()
	r.tick(t)
	if len(r.l.stops) != 1 || len(r.l.resumes) != 0 {
		t.Fatalf("止め終えたレビュー待ちの PG を止め直した / 再開した: stops=%v resumes=%v", r.l.stops, r.l.resumes)
	}
}

// `card review` の後の報告を書いている途中 (turn の途中) では止めない。turn を終えてから止め、諦めるまでの時間はそこから数える。
func TestReviewWaitsForTurnEnd(t *testing.T) {
	r := newCrashRig(t)
	r.ss[0].Status = agents.StatusBusy
	submit(t, r.dir, store.Request{Kind: "review", CardID: "C-001"})
	r.tick(t)
	if len(r.l.stopTries) != 0 {
		t.Fatalf("turn の途中の PG を止めた: %v", r.l.stopTries)
	}
	if c := states(t, r.dir)["C-001"]; !c.StopAfterClose {
		t.Fatal("turn を終えるのを待つ間に止める印を外した")
	}
	r.ss[0].Status = agents.StatusIdle
	r.tick(t)
	if !slices.Equal(r.l.stops, []string{"id-pc-c-001"}) {
		t.Fatalf("turn を終えた PG を止めない: %v", r.l.stops)
	}
}

// 止めた session の差し戻しは、止め直さずに (stopID 無しで) 同じ session を続きから再開する。自動の再開 (restartWait) も待たない。
func TestReworkAfterReviewStopResumesStoppedSession(t *testing.T) {
	r := reviewRig(t)
	r.tick(t)
	r.stopInList()
	submit(t, r.dir, store.Request{Kind: "rework", CardID: "C-001", Rework: "テストを足す"})
	r.tick(t)
	if len(r.l.resumes) != 1 || !strings.HasPrefix(r.l.resumes[0], ":") || !strings.Contains(r.l.resumes[0], "テストを足す") {
		t.Fatalf("止めた session を止め直さずに差し戻しの文で再開していない: %v", r.l.resumes)
	}
	if len(r.l.stopTries) != 1 {
		t.Fatalf("止まった session へ stop を撃った: %v", r.l.stopTries)
	}
	if c := states(t, r.dir)["C-001"]; c.State != card.Running || c.Stopped || c.StopAfterClose {
		t.Fatalf("再開した後の形が違う: %v Stopped=%v StopAfterClose=%v", c.State, c.Stopped, c.StopAfterClose)
	}
}

// 止め終える前に差し戻したら印を外す (残すと、再開した PG を次の Tick で止める)。再開は生きている session を止めてから起こす (今までどおり)。
func TestReworkBeforeReviewStopClearsMark(t *testing.T) {
	r := newCrashRig(t)
	r.ss[0].Status = agents.StatusBusy
	submit(t, r.dir, store.Request{Kind: "review", CardID: "C-001"})
	r.tick(t) // turn の途中なので止めない
	submit(t, r.dir, store.Request{Kind: "rework", CardID: "C-001", Rework: "テストを足す"})
	r.tick(t)
	c := states(t, r.dir)["C-001"]
	if c.StopAfterClose || len(r.l.resumes) != 1 || !strings.HasPrefix(r.l.resumes[0], "id-pc-c-001:") {
		t.Fatalf("差し戻しで印が残る / 生きている session を止めてから再開していない: StopAfterClose=%v resumes=%v", c.StopAfterClose, r.l.resumes)
	}
	r.tick(t)
	if len(r.l.stops) != 0 {
		t.Fatalf("差し戻しで再開した PG を止めた: %v", r.l.stops)
	}
}

// レビュー待ちの間に止めた PG へ追加オーダーが来たら、同じ session を続きから再開して届ける (止めたまま閉じられなくしない)。
func TestOrderAfterReviewStopResumes(t *testing.T) {
	r := reviewRig(t)
	r.tick(t)
	r.stopInList()
	submit(t, r.dir, store.Request{Kind: "order", CardID: "C-001", Order: card.OrderAppend, Text: "README も直して"})
	r.tick(t)
	if len(r.l.resumes) != 1 || !strings.HasPrefix(r.l.resumes[0], ":") || !strings.Contains(r.l.resumes[0], "README も直して") {
		t.Fatalf("止めた PG へ追加オーダーを届けていない: %v", r.l.resumes)
	}
}

// 追加オーダーが届いていないレビュー待ちの PG は止めずに、同じ session の再開で届ける (止めてすぐ起こし直さない)。
func TestReviewWithPendingOrderDeliversInsteadOfStop(t *testing.T) {
	r := newCrashRig(t)
	r.ss[0].Status = agents.StatusIdle
	submit(t, r.dir,
		store.Request{Kind: "review", CardID: "C-001"},
		store.Request{Kind: "order", CardID: "C-001", Order: card.OrderAppend, Text: "README も直して"})
	r.tick(t)
	r.tick(t)
	if len(r.l.stops) != 0 {
		t.Fatalf("オーダーを届ける前に PG を止めた: %v", r.l.stops)
	}
	if len(r.l.resumes) != 1 || !strings.Contains(r.l.resumes[0], "README も直して") {
		t.Fatalf("オーダーを届けていない: %v", r.l.resumes)
	}
	if c := states(t, r.dir)["C-001"]; c.StopAfterClose {
		t.Fatal("オーダーを届けた再開の後も止める印が残る")
	}
}

// レビュー待ちの間に止めた PG のカードを閉じても、止めた旨を「既に止まっていた」と重ねて書かない。
func TestCloseAfterReviewStop(t *testing.T) {
	r := reviewRig(t)
	r.tick(t)
	r.stopInList()
	submit(t, r.dir, store.Request{Kind: "close", CardID: "C-001"})
	r.tick(t)
	c := states(t, r.dir)["C-001"]
	if c.State != card.Done || c.StopAfterClose || len(r.l.stopTries) != 1 || !strings.Contains(lastHistory(c), "レビュー待ちの間に止めてある") {
		t.Fatalf("閉じた後の形が違う: %v StopAfterClose=%v tries=%v 履歴=%q", c.State, c.StopAfterClose, r.l.stopTries, lastHistory(c))
	}
}

// 止められなければ closeStopWait の間は止め直し、過ぎたら諦めて履歴に書く (447 と同じ)。Stopped は付けない (止まっていない)。
func TestReviewStopFailureGivesUp(t *testing.T) {
	r := reviewRig(t)
	r.l.stopFail = true
	now := t0
	r.d.Now = func() time.Time { return now }
	r.tick(t)
	now = t0.Add(closeStopWait)
	r.tick(t)
	c := states(t, r.dir)["C-001"]
	if c.State != card.Review || c.StopAfterClose || c.Stopped || !strings.Contains(lastHistory(c), "レビュー待ちに入ったが PG の session を止められない") {
		t.Fatalf("諦めた後の形が違う: %v StopAfterClose=%v Stopped=%v 履歴=%q", c.State, c.StopAfterClose, c.Stopped, lastHistory(c))
	}
}
