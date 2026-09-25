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

// planAfter は依頼の列にカードを 1 枚足し、前のカード after の後に回して分解済みにする (issue 468)。
func planAfter(t *testing.T, dir string, after ...string) {
	t.Helper()
	if _, err := store.Submit(dir, store.Request{Kind: "add", Title: "後", Repo: "dotfiles"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(dir, t0); err != nil {
		t.Fatal(err)
	}
	st, _ := store.Load(dir)
	id := st.Cards[len(st.Cards)-1].ID
	if _, err := store.Submit(dir, store.Request{Kind: "plan", CardID: id, After: after, Issues: []card.IssueRef{{Repo: "dotfiles", Number: 1, Status: "open"}}}); err != nil {
		t.Fatal(err)
	}
	res, err := store.Apply(dir, t0)
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
	if !hasNote(notes, eventlog.KindHold, "C-002: C-001 の完了を待つ") {
		t.Fatalf("待っている理由を出来事の記録に書かない: %q", notes)
	}
	again, err := d.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c := states(t, dir)["C-002"]
	n := 0
	for _, e := range c.History {
		if strings.Contains(e.Text, "C-001 の完了を待つ") {
			n++
		}
	}
	if n != 1 || hasNote(again, eventlog.KindHold, "C-001 の完了を待つ") {
		t.Fatalf("待っている理由を Tick ごとに書き足した: 履歴 %d 回 / %q", n, again)
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
	p := Prompt(card.Card{ID: "C-002", Title: "t", After: []string{"C-001"}})
	if !strings.Contains(p, "C-001 の後") {
		t.Fatalf("起動の指示に前のカードが無い: %s", p)
	}
}
