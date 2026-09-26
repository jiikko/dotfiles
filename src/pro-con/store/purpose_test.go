package store

import (
	"strings"
	"testing"
	"time"

	"pro-con/card"
)

// 確認のカード (問いだけ。issue 531) は種類を記録に残し、PG を付けない (plan を受けない)。人に聞いて、答えを受けたら依頼の列から閉じられる。
// 知らない種類の add は受けない (箱に手で置かれた依頼)。
func TestQuestionCardTakesNoPG(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "確かめる", Purpose: card.ForQuestion, At: t0})
	submit(t, dir, Request{Kind: "add", Title: "作業", At: t0.Add(time.Second)})
	submit(t, dir, Request{Kind: "add", Title: "変な種類", Purpose: card.Purpose(9), At: t0.Add(2 * time.Second)})
	if res := applyAll(t, dir); len(res) != 3 || res[0].Err != "" || res[1].Err != "" || !strings.Contains(res[2].Err, "種類") {
		t.Fatalf("add の結果: %+v", res)
	}
	if q, w := cardOf(t, dir, "C-001"), cardOf(t, dir, "C-002"); q.Purpose != card.ForQuestion || w.Purpose != card.ForWork {
		t.Fatalf("種類を記録に残さない: %v / %v", q.Purpose, w.Purpose)
	}
	issue := []card.IssueRef{{Repo: "dotfiles", Number: 1, Status: "open"}}
	submit(t, dir, Request{Kind: "plan", CardID: "C-001", Issues: issue})
	submit(t, dir, Request{Kind: "plan", CardID: "C-002", Issues: issue})
	if res := applyAll(t, dir); len(res) != 2 || !strings.Contains(res[0].Err, "PG を付けない") || res[1].Err != "" {
		t.Fatalf("確認のカードに plan を受けた / 作業のカードの plan を断った: %+v", res)
	}
	submit(t, dir, Request{Kind: "ask", CardID: "C-001", Question: "issue にしますか"})
	submit(t, dir, Request{Kind: "answer", CardID: "C-001", Answer: "する", From: "人間"})
	submit(t, dir, Request{Kind: "close", CardID: "C-001", Ending: card.EndAnswered, Issues: issue})
	if res := applyAll(t, dir); len(res) != 3 || res[0].Err != "" || res[1].Err != "" || res[2].Err != "" {
		t.Fatalf("確認のカードを聞いて閉じられない: %+v", res)
	}
	if c := cardOf(t, dir, "C-001"); c.State != card.Done || len(c.Issues) != 1 || c.Session != "" {
		t.Fatalf("閉じた確認のカード: state=%v issues=%v session=%q", c.State, c.Issues, c.Session)
	}
}
