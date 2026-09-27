package wtclean

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/card"
)

func (f *fixture) opt() Options {
	return Options{Fresh: func(context.Context) (Inputs, error) { return f.in, nil }}
}

// 人が決められる (Ask) のは、残す理由が「取り込む先に無い commit」か「記録に無いカード」だけのもの。ほかの止め具に掛かるものは Ask にしない。
func TestJudgeAsk(t *testing.T) {
	f := newFixture(t, sandbox)
	unmerged := f.add("C-001")
	f.commit(unmerged, "only-here.txt")
	f.add("C-002")
	delete(f.in.Cards, "C-002") // 削除したカード (中身は取り込み済み)
	orphanUnmerged := f.add("C-003")
	f.commit(orphanUnmerged, "orphan.txt")
	delete(f.in.Cards, "C-003")
	orphanBusy := f.add("C-004")
	delete(f.in.Cards, "C-004")
	f.in.Sessions = []agents.Session{{Name: "x", Cwd: orphanBusy}}
	orphanDirty := f.add("C-005")
	delete(f.in.Cards, "C-005")
	write(t, filepath.Join(orphanDirty, "new.txt"), "x\n")
	f.add("C-006") // 取り込み済み (自動で消す)
	notDone := f.add("C-007")
	f.commit(notDone, "wip.txt")
	f.in.Cards["C-007"] = card.Card{ID: "C-007", Repo: "r", State: card.Running}
	humanLock := f.add("C-008")
	f.commit(humanLock, "locked.txt")
	git(t, f.repo, "worktree", "lock", "--reason", "人が見ている", humanLock)

	got := f.scan()
	want := map[string]struct {
		ask bool
		why string
	}{
		"pc-c-001": {true, "origin/master に無い commit が 1 本"},
		"pc-c-002": {true, "記録に無いカードの worktree (先端が origin/master の祖先)"},
		"pc-c-003": {true, "記録に無いカードの worktree (origin/master に無い commit が 1 本"},
		"pc-c-004": {false, "session が動いている"},
		"pc-c-005": {false, "未 commit の変更"},
		"pc-c-006": {false, "祖先"},
		"pc-c-007": {false, "完了していない"},
		"pc-c-008": {false, "人が見ている"},
	}
	for name, w := range want {
		v := got[name]
		if v.Ask != w.ask || !strings.Contains(v.Why, w.why) {
			t.Errorf("%s = Ask %v / %q, want %v / %q を含む", name, v.Ask, v.Why, w.ask, w.why)
		}
		if v.Ask && v.Removable() {
			t.Errorf("%s は Ask なのに自動で消す側になっている", name)
		}
	}
}

// 消すと決めたら、取り込んでいない commit を refs/pro-con/removed に残してから worktree とブランチを消す。
func TestDiscard(t *testing.T) {
	f := newFixture(t, sandbox)
	wt := f.add("C-001")
	head := f.commit(wt, "only-here.txt")
	orphan := f.add("C-002")
	delete(f.in.Cards, "C-002")
	vs := f.scan()
	for _, name := range []string{"pc-c-001", "pc-c-002"} {
		r := Discard(context.Background(), vs[name], f.opt())
		if r.Outcome != Removed {
			t.Fatalf("%s = %s (%s)", name, r.Outcome, r.Detail)
		}
		if exists(filepath.Join(f.repo, ".claude", "worktrees", name)) || f.hasBranch(BranchName(name)) {
			t.Errorf("%s の worktree かブランチが残っている", name)
		}
	}
	if got := git(t, f.repo, "rev-parse", RemovedRef("pc-c-001")+head); got != head {
		t.Errorf("取り込んでいない先端が残っていない: %s", got)
	}
	if exists(orphan) {
		t.Error("記録に無いカードの worktree が残っている")
	}
}

// 見た後に変わったもの (先端が動いた・session が動き出した・未 commit の変更ができた) と、人が決めるものでないもの (自動で消す・
// 完了していない) は、消すと決めても消さない。
func TestDiscardRejudges(t *testing.T) {
	f := newFixture(t, sandbox)
	moved := f.add("C-001")
	f.commit(moved, "a1.txt")
	busy := f.add("C-002")
	f.commit(busy, "b1.txt")
	dirty := f.add("C-003")
	f.commit(dirty, "c1.txt")
	auto := f.add("C-004")
	vs := f.scan()
	vs["pc-c-004"] = Verdict{Repo: "r", Path: auto, Name: "pc-c-004", Head: vs["pc-c-004"].Head, Ask: true} // 画面が古い一覧を持っていた
	f.commit(moved, "a2.txt")
	write(t, filepath.Join(dirty, "late.txt"), "x\n")
	f.in.Sessions = []agents.Session{{Name: "pc-c-002", Cwd: busy}}
	for name, why := range map[string]string{"pc-c-001": "先端が動いた", "pc-c-002": "session", "pc-c-003": "未 commit", "pc-c-004": "祖先"} {
		r := Discard(context.Background(), vs[name], f.opt())
		if r.Outcome != Skipped || !strings.Contains(r.Detail, why) {
			t.Errorf("%s = %s (%s), want %s (%s)", name, r.Outcome, r.Detail, Skipped, why)
		}
	}
	for _, wt := range []string{moved, busy, dirty, auto} {
		if !exists(wt) {
			t.Errorf("%s を消した", wt)
		}
	}
}

// 残すと決めたら lock を掛け、判定は「人が残した」になって、自動の片付けも消すの決め直しも触らない。
func TestHold(t *testing.T) {
	f := newFixture(t, sandbox)
	wt := f.add("C-001")
	f.commit(wt, "only-here.txt")
	stale := f.add("C-002") // Claude Code が残した lock (持ち主はもう居ない) は掛け替える
	f.commit(stale, "stale.txt")
	git(t, f.repo, "worktree", "lock", "--reason", "claude session pc-c-002 (pid 999999 start Fri Sep 25 14:15:38 2026)", stale)
	vs := f.scan()
	now := time.Date(2026, 9, 27, 14, 0, 0, 0, time.Local)
	for _, name := range []string{"pc-c-001", "pc-c-002"} {
		if r := Hold(context.Background(), vs[name], f.opt(), now); r.Outcome != Held {
			t.Fatalf("%s = %s (%s)", name, r.Outcome, r.Detail)
		}
	}
	after := f.scan()
	for _, name := range []string{"pc-c-001", "pc-c-002"} {
		v := after[name]
		if v.Ask || v.Action != Keep || !strings.Contains(v.Why, "人が残すと決めた (2026-09-27 14:00)") || !strings.Contains(v.Why, "git worktree unlock") {
			t.Errorf("%s = Ask %v / %s / %q", name, v.Ask, v.Action, v.Why)
		}
		if r := Discard(context.Background(), vs[name], f.opt()); r.Outcome != Skipped {
			t.Errorf("%s: 残した後に消すと決め直せた: %s (%s)", name, r.Outcome, r.Detail)
		}
	}
	f.clean(after, Options{})
	if !exists(wt) || !exists(stale) {
		t.Error("残したものを自動の片付けが消した")
	}
}

// Unlanded は worktree の先端とブランチのどちらかに取り込み先に無い commit があれば理由を返す (判定は片付けと同じ inBase)。
func TestUnlanded(t *testing.T) {
	f := newFixture(t, sandbox)
	landed := f.add("C-001")
	f.land(f.commit(landed, "a.txt"), true) // cherry-pick で入った (祖先ではない)
	open := f.add("C-002")
	f.commit(open, "b.txt")
	branchOnly := f.add("C-003")
	git(t, branchOnly, "checkout", "-q", "--detach")
	git(t, f.repo, "branch", "-f", BranchName("pc-c-003"), f.commit(branchOnly, "c.txt"))
	git(t, branchOnly, "checkout", "-q", "--detach", "master")
	ctx := context.Background()
	for name, want := range map[string]string{"pc-c-001": "", "pc-c-002": "origin/master に無い commit が 1 本", "pc-c-003": "origin/master に無い commit が 1 本", "pc-c-404": ""} {
		got, err := Unlanded(ctx, f.repo, name)
		if err != nil {
			t.Fatal(err)
		}
		if (want == "") != (got == "") || !strings.Contains(got, want) {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

// Settle は、取り込み済みは自動の片付けと同じく、取り込み先に無い commit・記録に無いカードは残してから消す。ほかの理由のものと
// 無い worktree には触らない。
func TestSettle(t *testing.T) {
	f := newFixture(t, sandbox)
	landed := f.add("C-001")
	f.land(f.commit(landed, "a.txt"), false)
	open := f.add("C-002")
	head := f.commit(open, "b.txt")
	f.in.Cards["C-002"] = card.Card{ID: "C-002", Repo: "r", State: card.Done, Ending: card.EndRejected}
	gone := f.add("C-003")
	delete(f.in.Cards, "C-003")
	dirty := f.add("C-004")
	f.commit(dirty, "d.txt")
	delete(f.in.Cards, "C-004")
	write(t, filepath.Join(dirty, "wip.txt"), "x\n")
	ctx := context.Background()
	for name, want := range map[string]Outcome{"pc-c-001": Removed, "pc-c-002": Removed, "pc-c-003": Removed, "pc-c-004": Skipped} {
		r := Settle(ctx, "r", filepath.Join(f.repo, ".claude", "worktrees", name), f.opt())
		if r.Outcome != want {
			t.Errorf("%s = %s (%s), want %s", name, r.Outcome, r.Detail, want)
		}
	}
	if got := git(t, f.repo, "rev-parse", RemovedRef("pc-c-002")+head); got != head {
		t.Errorf("取り込んでいない先端を残していない: %s", got)
	}
	if exists(landed) || exists(open) || exists(gone) || !exists(dirty) {
		t.Errorf("残る・消えるが違う: landed=%v open=%v gone=%v dirty=%v", exists(landed), exists(open), exists(gone), exists(dirty))
	}
	if r := Settle(ctx, "r", filepath.Join(f.repo, ".claude", "worktrees", "pc-c-404"), f.opt()); r.Outcome != Skipped || r.Detail != NoWorktree {
		t.Errorf("無い worktree = %s (%s)", r.Outcome, r.Detail)
	}
}
