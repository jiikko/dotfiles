package store

import (
	"strings"
	"testing"

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
