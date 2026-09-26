package dispatcher

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/store"
)

// planAfter は依頼の列にカードを 1 枚足し、前のカード after の後に回して着手待ちにする (issue 468)。
func planAfter(t *testing.T, dir string, after ...string) {
	t.Helper()
	if _, err := store.Submit(dir, store.Request{Kind: "add", Title: "後", Repo: "dotfiles"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(dir, t0, nil); err != nil {
		t.Fatal(err)
	}
	st, _ := store.Load(dir)
	id := st.Cards[len(st.Cards)-1].ID
	if _, err := store.Submit(dir, store.Request{Kind: "plan", CardID: id, After: after, Issues: []card.IssueRef{{Repo: "dotfiles", Number: 1, Status: "open"}}}); err != nil {
		t.Fatal(err)
	}
	res, err := store.Apply(dir, t0, nil)
	if err != nil || len(res) != 1 || res[0].Err != "" {
		t.Fatalf("plan --after を適用できない: %+v %v", res, err)
	}
}

// 前のカードが完了するまで、枠が空いていても後のカードを起動しない。待っている理由は履歴と出来事の記録に 1 度だけ書く。
// 前のカードが完了したら起動する。
func TestDispatchWaitsForPredecessor(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1) // C-001
	planAfter(t, dir, "C-001")
	l := &fakeLauncher{}
	d := newDispatcher(t, dir, l, nil)
	notes, err := d.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(l.starts, []string{"pc-c-001"}) {
		t.Fatalf("前のカードが終わる前に後のカードを起動した: %v", l.starts)
	}
	if !hasNote(notes, eventlog.KindHold, "C-002: 順番: C-001 の完了を待つ") {
		t.Fatalf("待っている理由を出来事の記録に書かない: %q", notes)
	}
	again, err := d.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c := states(t, dir)["C-002"]
	n := 0
	for _, e := range c.History {
		if strings.HasPrefix(e.Text, "順番: C-001 の完了を待つ") {
			n++
		}
	}
	if n != 1 || hasNote(again, eventlog.KindHold, "C-001 の完了を待つ") {
		t.Fatalf("待っている理由を Tick ごとに書き足した: 履歴 %d 回 / %q", n, again)
	}
	// 待っている間に別の出来事 (btw) が履歴に入っても、同じ待ちは書き直さない
	if _, err := store.Submit(dir, store.Request{Kind: "btw", CardID: "C-002", Question: "今どう?"}); err != nil {
		t.Fatal(err)
	}
	if notes, err := d.Tick(context.Background()); err != nil || hasNote(notes, eventlog.KindHold, "C-001 の完了を待つ") {
		t.Fatalf("別の出来事の後に同じ待ちを書き直した: %q %v", notes, err)
	}
	if err := store.Update(dir, func(s *store.State) error {
		for i := range s.Cards {
			if s.Cards[i].ID == "C-001" {
				s.Cards[i].State, s.Cards[i].Ending = card.Done, card.EndAnswered
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	d.Now = func() time.Time { return t0.Add(time.Minute) }
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(l.starts, "pc-c-002") {
		t.Fatalf("前のカードが完了しても後のカードを起動しない: %v", l.starts)
	}
}

// 作業中の前のカードを消したら (PG を止めて記録から外したら)、後のカードの待ちが外れて起動する。
func TestDeletedPredecessorReleasesSuccessor(t *testing.T) {
	r := newCrashRig(t) // C-001 が作業中・登録済み
	planAfter(t, r.dir, "C-001")
	r.tick(t)
	if slices.Contains(r.l.starts, "pc-c-002") {
		t.Fatalf("前のカードが作業中なのに起動した: %v", r.l.starts)
	}
	deleteCard(t, r.dir, "C-001")
	r.tick(t)
	cs := states(t, r.dir)
	if _, ok := cs["C-001"]; ok {
		t.Fatalf("前のカードが消えていない: %v", cs)
	}
	if c := cs["C-002"]; len(c.After) != 0 || !slices.Contains(r.l.starts, "pc-c-002") {
		t.Fatalf("前のカードを消しても待ちが外れない / 起動しない: %+v %v", c.After, r.l.starts)
	}
}

// 起動の指示に、前に回したカードを書く (PG が前の変更を読んでから作る)。
func TestPromptNamesPredecessors(t *testing.T) {
	p := Prompt(card.Card{ID: "C-002", Title: "t", After: []string{"C-001"}}, Review{})
	if !strings.Contains(p, "C-001 の後") {
		t.Fatalf("起動の指示に前のカードが無い: %s", p)
	}
}

// 前のカードが完了して 24 時間たち書庫へ移っても、完了として読む (記録に無い = 消えた、と読まない)。書庫へ移すことも止めない
// (後のカードの順番が残っていても、他の終えたカードまで道連れで移せなくならない)。後のカードの順番は表示のために残す。
func TestArchivedPredecessorReleasesSuccessor(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1) // C-001
	planAfter(t, dir, "C-001")
	l := &fakeLauncher{}
	d := newDispatcher(t, dir, l, nil)
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(dir, func(s *store.State) error {
		for i := range s.Cards {
			if s.Cards[i].ID == "C-001" {
				s.Cards[i].State, s.Cards[i].Ending, s.Cards[i].Since = card.Done, card.EndAnswered, t0
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	d.Now = func() time.Time { return t0.Add(store.AutoClearAfter + time.Minute) }
	notes, err := d.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(notes, func(e eventlog.Event) bool { return e.Kind == eventlog.KindError }) {
		t.Fatalf("書庫へ移せない: %q", notes)
	}
	cs := states(t, dir)
	if _, ok := cs["C-001"]; ok {
		t.Fatalf("完了から 24 時間たった前のカードが書庫へ移っていない: %v", cs)
	}
	if !slices.Contains(l.starts, "pc-c-002") || !slices.Equal(cs["C-002"].After, []string{"C-001"}) {
		t.Fatalf("書庫へ移った前のカードを待ち続ける / 順番を消した: %v %+v", l.starts, cs["C-002"].After)
	}
}
