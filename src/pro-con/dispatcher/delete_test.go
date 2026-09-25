package dispatcher

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/store"
)

func deleteCard(t *testing.T, dir, id string) {
	t.Helper()
	if _, err := store.Submit(dir, store.Request{Kind: "delete", CardID: id, From: "人間"}); err != nil {
		t.Fatal(err)
	}
}

// hasNote は出来事のうち、種類が kind で理由に sub を含むものがあるか。
func hasNote(notes []eventlog.Event, kind, sub string) bool {
	return slices.ContainsFunc(notes, func(e eventlog.Event) bool { return e.Kind == kind && strings.Contains(e.Reason, sub) })
}

// 作業中のカードの削除: そのカードの PG だけを止め、止まったのを一覧で確かめてから記録から外す。消したことは dispatcher の記録に残る。
func TestDeleteStopsThatCardsPGThenRemoves(t *testing.T) {
	r := newCrashRig(t) // C-001 が作業中・登録済み
	planned(t, r.dir, 1)
	r.tick(t) // C-002 を起動
	r.ss = append(r.ss, agents.Session{ID: "id-pc-c-002", SessionID: "S2", PID: 52, Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-002", StartedAt: t0.Add(time.Second).UnixMilli()})
	r.tick(t) // C-002 を登録
	deleteCard(t, r.dir, "C-001")
	notes := r.tick(t)
	if !slices.Equal(r.l.stops, []string{"id-pc-c-001"}) {
		t.Fatalf("削除したカードの PG だけを止めていない: %v", r.l.stops)
	}
	cs := states(t, r.dir)
	if _, ok := cs["C-001"]; ok || cs["C-002"].State != card.Running {
		t.Fatalf("止めた後にカードを外さない / 別のカードに触った: %v", cs)
	}
	if !hasNote(notes, eventlog.KindDelete, "C-001: 「t」を削除した (人間 が依頼。PG の session を止めた") {
		t.Fatalf("消したことを記録に残さない: %q", notes)
	}
}

// 止めたと返っても一覧で止まっていなければ消さない (判定は閉じたとき・終了のときと同じ)。止まったのを見たら「止めた」と書いて消す。
func TestDeleteWaitsUntilStoppedInList(t *testing.T) {
	r := newCrashRig(t)
	r.d.ListAll = func(ctx context.Context) ([]agents.Session, error) { return r.d.List(ctx) } // 止めても一覧がすぐには変わらない
	deleteCard(t, r.dir, "C-001")
	r.tick(t)
	c, ok := states(t, r.dir)["C-001"]
	if !ok || !c.Deleting() || !c.StopSent {
		t.Fatalf("止まっていない PG のカードを消した / 止める要求を出した印が無い: ok=%v %+v", ok, c)
	}
	r.ss[0].PID, r.ss[0].State = 0, agents.StateStopped
	notes := r.tick(t)
	if _, ok := states(t, r.dir)["C-001"]; ok || !hasNote(notes, eventlog.KindDelete, "PG の session を止めた") {
		t.Fatalf("止まったのを見ても消さない / 「止めた」と書かない: %q", notes)
	}
}

// 止められないまま closeStopWait を過ぎたら諦めて、カードは消さずに残し、理由を履歴に書く (印も外す = もう一度頼める)。
func TestDeleteGivesUpAndKeepsCard(t *testing.T) {
	r := newCrashRig(t)
	r.l.stopFail = true
	now := t0
	r.d.Now = func() time.Time { return now }
	deleteCard(t, r.dir, "C-001")
	r.tick(t)
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; !c.Deleting() || len(r.l.stopTries) < 2 {
		t.Fatalf("止められない間は印を保って止め直し続ける形になっていない: %+v tries=%v", c, r.l.stopTries)
	}
	now = t0.Add(closeStopWait)
	notes := r.tick(t)
	c, ok := states(t, r.dir)["C-001"]
	if !ok || c.Deleting() || c.StopSent || !strings.Contains(lastHistory(c), "削除できない") || !hasNote(notes, eventlog.KindDelete, "削除できない") {
		t.Fatalf("諦めたときにカードを残して理由を書く形になっていない: ok=%v %+v %q", ok, c, notes)
	}
	tries := len(r.l.stopTries)
	r.tick(t)
	if len(r.l.stopTries) != tries {
		t.Fatal("諦めた後も止め続ける")
	}
}

// 削除の依頼を受けたカードは、回答で分解済みへ戻っていても PG を再開しない (止めて消す)。
func TestDeleteDoesNotResume(t *testing.T) {
	r := newCrashRig(t)
	if _, err := store.Submit(r.dir, store.Request{Kind: "ask", CardID: "C-001", Question: "q"}); err != nil {
		t.Fatal(err)
	}
	r.tick(t)
	if _, err := store.Submit(r.dir, store.Request{Kind: "answer", CardID: "C-001", Answer: "a"}); err != nil {
		t.Fatal(err)
	}
	deleteCard(t, r.dir, "C-001") // 同じ Tick で回答と削除を適用する (回答で開いた再開の口を、削除が閉じる)
	r.tick(t)
	if len(r.l.resumes) != 0 || len(r.l.starts) != 1 {
		t.Fatalf("削除の依頼を受けたカードの PG を再開・起動した: resumes=%v starts=%v", r.l.resumes, r.l.starts)
	}
	if _, ok := states(t, r.dir)["C-001"]; ok {
		t.Fatal("止めた後にカードを外さない")
	}
}

// 起動の結果が分からないカード (印の直後で一覧にまだ出ない) は、すぐには消さない (立っていた PG を見張れなくなる)。
// 一覧に出たら、その session を止めてから消す。起動し直しはしない。
func TestDeleteWaitsForUnconfirmedLaunch(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{fail: true} // 失敗と返るが、実際には立っている形
	d := newDispatcher(t, dir, l, nil)
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	l.fail = false
	deleteCard(t, dir, "C-001")
	d.Now = func() time.Time { return t0.Add(launchGrace / 2) }
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c, ok := states(t, dir)["C-001"]; !ok || !c.Deleting() || len(l.starts) != 0 {
		t.Fatalf("起動の結果を確かめる前に消した / 起動し直した: ok=%v starts=%v", ok, l.starts)
	}
	ss := []agents.Session{{ID: "ab12", SessionID: "S1", PID: 5, Name: "pc-c-001", Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t0.Add(time.Second).UnixMilli()}}
	d.List = func(context.Context) ([]agents.Session, error) { return ss, nil }
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := states(t, dir)["C-001"]; ok || !slices.Equal(l.stops, []string{"ab12"}) || len(l.starts) != 0 {
		t.Fatalf("一覧に出た PG を止めてから消していない: stops=%v starts=%v", l.stops, l.starts)
	}
}

// テストの係が実行中のカードを削除したら、実行を取り消し、結果で PG を再開しない。頼みを取り下げてから消す。
func TestDeleteCancelsRunAndDoesNotResume(t *testing.T) {
	r, fr, _ := runRig(t, 1)
	old := killStaleFn
	killStaleFn = func(string) {} // この dispatcher の実行は取り消しで止める (印で撃ちに行かない)
	t.Cleanup(func() { killStaleFn = old })
	askRun(t, r.dir, "C-001", "make test", t0)
	r.tick(t) // 実行を始める
	fr.waitStarted(t)
	deleteCard(t, r.dir, "C-001")
	r.tick(t) // 頼みが残っている間は消さない。この Tick で取り消して取り下げる
	if r.d.active != nil || !fr.canceled {
		t.Fatalf("削除したカードの実行を取り消さない: active=%v canceled=%v", r.d.active != nil, fr.canceled)
	}
	r.tick(t)
	if _, ok := states(t, r.dir)["C-001"]; ok || len(r.l.resumes) != 0 {
		t.Fatalf("取り下げた後に消さない / 結果で再開した: resumes=%v", r.l.resumes)
	}
}

// 前の dispatcher が実行の途中で落ちて残した実行は、削除のカードでも止めてから取り下げる (印を消して止め損ねない)。
func TestDeleteKillsStaleRun(t *testing.T) {
	r, _, _ := runRig(t, 1)
	var killed []string
	old := killStaleFn
	killStaleFn = func(id string) { killed = append(killed, id) }
	t.Cleanup(func() { killStaleFn = old })
	r.d.Runner = nil // この dispatcher は実行していない
	askRun(t, r.dir, "C-001", "make test", t0)
	r.tick(t)
	setCard(t, r.dir, "C-001", func(c *card.Card) { c.Exec = card.Exec{Command: "make test", Since: t0, RunID: "stale-1"} })
	deleteCard(t, r.dir, "C-001")
	r.tick(t)
	if !slices.Equal(killed, []string{"stale-1"}) {
		t.Fatalf("残った実行を止めずに取り下げた: %v", killed)
	}
	r.tick(t)
	if _, ok := states(t, r.dir)["C-001"]; ok || len(r.l.resumes) != 0 {
		t.Fatalf("残った実行を止めた後に消さない / 再開した: resumes=%v", r.l.resumes)
	}
}

// PG を起動していない分解済みのカードは、止めるものが無いのでその Tick で消える (起動もしない)。
func TestDeletePlannedWithoutPG(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	deleteCard(t, dir, "C-001")
	l := &fakeLauncher{}
	d := newDispatcher(t, dir, l, nil)
	notes, err := d.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := states(t, dir)["C-001"]; ok || len(l.starts) != 0 || len(l.stopTries) != 0 || !hasNote(notes, eventlog.KindDelete, "PG の session は動いていなかった") {
		t.Fatalf("PG の無い分解済みのカードの削除: starts=%v stops=%v notes=%q", l.starts, l.stopTries, notes)
	}
}

// 依頼の列のカードは dispatcher の適用ですぐ消え、消したことを記録に残す。
func TestDeleteRequestedIsImmediate(t *testing.T) {
	dir := t.TempDir()
	if _, err := store.Submit(dir, store.Request{Kind: "add", Title: "消す依頼", Repo: "dotfiles"}); err != nil {
		t.Fatal(err)
	}
	d := newDispatcher(t, dir, &fakeLauncher{}, nil)
	d.Limit = 0
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	deleteCard(t, dir, "C-001")
	notes, err := d.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := states(t, dir)["C-001"]; ok || !hasNote(notes, eventlog.KindDelete, "C-001: 「消す依頼」を削除した (依頼の列。人間 が依頼)") {
		t.Fatalf("依頼の列のカードをすぐ消さない / 記録に残さない: %q", notes)
	}
}
