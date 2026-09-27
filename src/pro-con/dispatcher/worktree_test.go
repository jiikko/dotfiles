package dispatcher

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/store"
	"pro-con/wtclean"
)

// fakeWorktrees は WorktreeOps の偽物 (git を触らない)。unlanded はカードごとの取り込み先に無い commit の理由。
type fakeWorktrees struct {
	unlanded map[string]string
	err      error
	settle   wtclean.Result
	settled  []string
}

func (f *fakeWorktrees) Unlanded(_ context.Context, _ string, c card.Card) (string, error) {
	return f.unlanded[c.ID], f.err
}

func (f *fakeWorktrees) Settle(_ context.Context, _ string, c card.Card) wtclean.Result {
	f.settled = append(f.settled, c.ID)
	return f.settle
}

func worktreeRig(t *testing.T) (*crashRig, *fakeWorktrees) {
	t.Helper()
	r := newCrashRig(t) // C-001 が作業中・登録済み
	f := &fakeWorktrees{unlanded: map[string]string{}, settle: wtclean.Result{Outcome: wtclean.Removed, Detail: "取り込んでいない commit は …"}}
	r.d.Worktrees = f
	c := states(t, r.dir)["C-001"]
	if r.d.Repos == nil {
		r.d.Repos = map[string]string{}
	}
	if r.d.Repos[c.Repo] == "" {
		r.d.Repos[c.Repo] = "/w/" + c.Repo
	}
	return r, f
}

func closeWith(t *testing.T, dir, id string, e card.Ending) {
	t.Helper()
	for _, kind := range []string{"review", "close"} {
		req := store.Request{Kind: kind, CardID: id}
		if kind == "close" {
			req.Ending = e
		}
		if _, err := store.Submit(dir, req); err != nil {
			t.Fatal(err)
		}
	}
}

// 取り込み先に無い commit がある PG のカードは、取り込まない終わり方を付けずには閉じられない (理由を出して除ける)。
// 確かめられないときも除ける (取り込んだと読まない)。worktree は片付けない。
func TestCloseRefusesUnlandedWork(t *testing.T) {
	for name, set := range map[string]func(*fakeWorktrees){
		"取り込んでいない": func(f *fakeWorktrees) { f.unlanded["C-001"] = "origin/master に無い commit が 2 本ある" },
		"確かめられない":  func(f *fakeWorktrees) { f.err = errors.New("git が無い") },
	} {
		t.Run(name, func(t *testing.T) {
			r, f := worktreeRig(t)
			set(f)
			closeWith(t, r.dir, "C-001", card.EndNone)
			notes := r.tick(t)
			c := states(t, r.dir)["C-001"]
			if c.State != card.Review {
				t.Fatalf("閉じた: %v", c.State)
			}
			if !hasNote(notes, eventlog.KindReject, "--ending rejected") {
				t.Errorf("除けた理由に取り込まずに閉じる方法が無い: %q", notes)
			}
			if len(f.settled) != 0 {
				t.Errorf("閉じていないカードの worktree を片付けた: %v", f.settled)
			}
		})
	}
}

// 取り込み済みなら閉じる。worktree は取り込みの係が見た後も残す (片付けは予定の worktree clean)。
func TestCloseLandedKeepsWorktree(t *testing.T) {
	r, f := worktreeRig(t)
	closeWith(t, r.dir, "C-001", card.EndNone)
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.State != card.Done {
		t.Fatalf("取り込み済みなのに閉じない: %v", c.State)
	}
	if len(f.settled) != 0 {
		t.Errorf("取り込んで閉じたカードの worktree をその場で片付けた: %v", f.settled)
	}
}

// 取り込まない終わり方で閉じたら、取り込み先に無い commit があっても閉じ、PG を止め終えた後に worktree を片付けて履歴に書く。
func TestCloseDiscardingSettles(t *testing.T) {
	r, f := worktreeRig(t)
	f.unlanded["C-001"] = "origin/master に無い commit が 2 本ある"
	closeWith(t, r.dir, "C-001", card.EndRejected)
	notes := r.tick(t)
	c := states(t, r.dir)["C-001"]
	if c.State != card.Done || !slices.Equal(f.settled, []string{"C-001"}) {
		t.Fatalf("閉じて片付けていない: %v settled=%v", c.State, f.settled)
	}
	if !strings.Contains(lastHistory(c), "PG の worktree とブランチを消した") || !hasNote(notes, eventlog.KindStop, "PG の worktree とブランチを消した") {
		t.Errorf("片付けたことを履歴・出来事に書かない: %q / %q", lastHistory(c), notes)
	}
	r.tick(t)
	if len(f.settled) != 1 {
		t.Errorf("次の Tick でまた片付けた: %v", f.settled)
	}
}

// 削除したカードは、記録から外した直後に worktree を片付ける。片付けられなければ理由を出来事に書く (受け皿はディスクのタブ)。
func TestDeleteSettles(t *testing.T) {
	r, f := worktreeRig(t)
	f.settle = wtclean.Result{Outcome: wtclean.Skipped, Detail: "未 commit の変更・追跡していないファイルが 1 件 (x.txt)"}
	deleteCard(t, r.dir, "C-001")
	notes := r.tick(t)
	if _, ok := states(t, r.dir)["C-001"]; ok || !slices.Equal(f.settled, []string{"C-001"}) {
		t.Fatalf("外して片付けていない: settled=%v", f.settled)
	}
	if !hasNote(notes, eventlog.KindDelete, "PG の worktree は残した: 未 commit の変更") {
		t.Errorf("残した理由を出来事に書かない: %q", notes)
	}
}

// 片付ける worktree が無ければ何も書かない。
func TestSettleNoWorktreeIsQuiet(t *testing.T) {
	r, f := worktreeRig(t)
	f.settle = wtclean.Result{Outcome: wtclean.Skipped, Detail: wtclean.NoWorktree}
	deleteCard(t, r.dir, "C-001")
	notes := r.tick(t)
	if hasNote(notes, eventlog.KindDelete, "worktree") {
		t.Errorf("worktree が無いのに書いた: %q", notes)
	}
}

// 閉じる検査の後・PG を止め終える前に PG が commit を足したら、止め終えた後の見直しで出来事と履歴に書く (黙って宙に浮かせない)。
func TestCloseRechecksAfterStop(t *testing.T) {
	r, f := worktreeRig(t)
	closeWith(t, r.dir, "C-001", card.EndNone)
	r.l.stopFail = true // 1 回目の Tick では止まらない (閉じた後も PG が動いている)
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.State != card.Done || !c.StopAfterClose {
		t.Fatalf("閉じて止めに入っていない: %v %v", c.State, c.StopAfterClose)
	}
	f.unlanded["C-001"] = "origin/master に無い commit が 1 本ある" // 止め終える前に PG が commit した
	r.l.stopFail = false
	notes := r.tick(t)
	c := states(t, r.dir)["C-001"]
	if !hasNote(notes, eventlog.KindError, "閉じた後に PG の作業に取り込まれていないものができた") || !strings.Contains(lastHistory(c), "取り込まれていない") {
		t.Errorf("見直しで書かない: %q / %q", notes, lastHistory(c))
	}
	if len(f.settled) != 0 {
		t.Errorf("取り込んで閉じたカードの worktree を消した: %v", f.settled)
	}
}
