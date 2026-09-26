package store

import (
	"strings"
	"testing"
	"time"

	"pro-con/card"
)

// plan --points は見積もりをカードに載せ、履歴に残す。使えない値は除ける (箱に手で置いた依頼も。issue 490)。
func TestPlanPoints(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "a", Repo: "dotfiles"})
	submit(t, dir, Request{Kind: "add", Title: "b", Repo: "dotfiles"})
	applyAll(t, dir)
	submit(t, dir, Request{Kind: "plan", CardID: "C-001", Points: 5})
	submit(t, dir, Request{Kind: "plan", CardID: "C-002", Points: 4})
	res := applyAll(t, dir)
	if res[0].Err != "" || res[1].Err == "" {
		t.Fatalf("5 は通り 4 は除ける: %+v", res)
	}
	c := cardOf(t, dir, "C-001")
	if c.Points != 5 || !strings.Contains(c.History[len(c.History)-1].Text, "見積もり 5pt") {
		t.Fatalf("見積もりが載っていない: %d %q", c.Points, c.History[len(c.History)-1].Text)
	}
	if c := cardOf(t, dir, "C-002"); c.State != card.Requested || c.Points != 0 {
		t.Fatalf("除けた plan で動いた: %v %d", c.State, c.Points)
	}
}

// 作業中だった時間は、記録の書き手 (Update / Apply) が書くときに閉じる。質問待ち・テストの係の結果待ち・分解済みの間は数えず、
// 作業中へ戻ったら続きから足す (issue 490)。
func TestWorkedAccumulatesAcrossWriters(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "a", Repo: "dotfiles"})
	applyAll(t, dir)
	submit(t, dir, Request{Kind: "plan", CardID: "C-001", Points: 3})
	applyAll(t, dir)
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	toRunning := func(now time.Time) { // dispatcher の起動・再開 (settle) の形
		t.Helper()
		if err := Update(dir, now, func(s *State) error {
			s.Cards[0].State, s.Cards[0].Since = card.Running, now
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	applyAt := func(now time.Time, r Request) {
		t.Helper()
		submit(t, dir, r)
		res, err := Apply(dir, now)
		if err != nil || len(res) != 1 || res[0].Err != "" {
			t.Fatalf("%s: %v %+v", r.Kind, err, res)
		}
	}
	worked := func(now time.Time) time.Duration { return cardOf(t, dir, "C-001").WorkedAt(now) }

	toRunning(at(0))
	if got := worked(at(7)); got != 7*time.Minute {
		t.Fatalf("作業中の今の分が足されていない: %v", got)
	}
	applyAt(at(10), Request{Kind: "ask", CardID: "C-001", Question: "q"}) // 10 分で質問待ちへ
	if got := worked(at(40)); got != 10*time.Minute {
		t.Fatalf("質問待ちの間も数えた: %v", got)
	}
	applyAt(at(40), Request{Kind: "answer", CardID: "C-001", Answer: "a"}) // 分解済みへ (数えない)
	toRunning(at(50))
	applyAt(at(55), Request{Kind: "run", CardID: "C-001", Command: "make test"}) // 5 分でテストの係へ (455 の「待っている」)
	if got := worked(at(70)); got != 15*time.Minute {
		t.Fatalf("テストの係の結果待ちを数えた / 続きから足していない: %v", got)
	}
	if err := Update(dir, at(70), func(s *State) error { // 結果が届いて分解済みへ (runner の finishRun の形)
		s.Cards[0].DropRun()
		s.Cards[0].State, s.Cards[0].Since = card.Planned, at(70)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	toRunning(at(80))
	applyAt(at(92), Request{Kind: "review", CardID: "C-001"})
	c := cardOf(t, dir, "C-001")
	if got := c.WorkedAt(at(200)); got != 27*time.Minute || !c.WorkFrom.IsZero() {
		t.Fatalf("合計が合わない (10 + 5 + 12 = 27 分): %v workFrom=%v", got, c.WorkFrom)
	}
}
