package store

import (
	"strings"
	"testing"

	"pro-con/card"
)

// PM は依頼の列のカードについて人に聞ける (issue 498): 質問待ちの列の人の番になり、PM は自分の問いに答えられない。
// 人の回答は PG の再開の文 (Resume) にせず、依頼の列へ戻して PMAnswer と履歴に原文で残す。分けたら PMAnswer は消える。
// 分解済みの列のカードには聞けない (PM が聞くのは分ける前。作業の後は PG が聞く)。
func TestPMAsksOnRequestedCard(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "469 の続き"})
	applyAll(t, dir)
	qs := []card.Question{{Question: "どうする", Options: []card.Option{{Label: "閉じる", Recommended: true}, {Label: "直す"}}}}
	submit(t, dir, Request{Kind: "ask", CardID: "C-001", Question: "469 は実装済み", Questions: qs})
	applyAll(t, dir)
	c := cardOf(t, dir, "C-001")
	if c.State != card.Waiting || !c.Wait.FromPM() || len(c.Wait.Questions) != 1 || c.Turn(card.Roles{}) != card.TurnHuman || !c.Answerable() {
		t.Fatalf("PM の問いが人の番の質問待ちになっていない: %v %+v turn=%v", c.State, c.Wait, c.Turn(card.Roles{}))
	}
	if w := c.WaitingOn(nil, card.Roles{}); !strings.Contains(w, "PM の質問に答える") {
		t.Fatalf("今の待ちが PM の問いと分からない: %q", w)
	}
	submit(t, dir, Request{Kind: "answer", CardID: "C-001", Answer: "閉じる", From: card.PMName})
	if res := applyAll(t, dir); len(res) != 1 || res[0].Err == "" {
		t.Fatalf("PM が自分の問いに答えられた: %+v", res)
	}
	answer := "1. どうする: 直す\n補足: 残りのリスクだけ"
	submit(t, dir, Request{Kind: "answer", CardID: "C-001", Answer: answer})
	applyAll(t, dir)
	c = cardOf(t, dir, "C-001")
	if c.State != card.Requested || c.Wait.Kind != card.WaitNone || c.PMAnswer != answer || c.Resume != "" {
		t.Fatalf("回答で依頼の列へ戻っていない: %v %+v PMAnswer=%q Resume=%q", c.State, c.Wait, c.PMAnswer, c.Resume)
	}
	if last := c.History[len(c.History)-1].Text; !strings.Contains(last, answer) {
		t.Fatalf("履歴に回答の原文が無い: %q", last)
	}
	submit(t, dir, Request{Kind: "plan", CardID: "C-001", Issues: []card.IssueRef{{Repo: "dotfiles", Number: 1}}})
	applyAll(t, dir)
	if c := cardOf(t, dir, "C-001"); c.State != card.Planned || c.PMAnswer != "" {
		t.Fatalf("分けた後に PM への回答が残る: %v %q", c.State, c.PMAnswer)
	}
	submit(t, dir, Request{Kind: "ask", CardID: "C-001", Question: "もう一度"})
	if res := applyAll(t, dir); len(res) != 1 || res[0].Err == "" {
		t.Fatalf("分解済みのカードで ask を受けた: %+v", res)
	}
}
