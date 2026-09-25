package store

import (
	"slices"
	"strings"
	"testing"
)

// plan --after は順番を付ける (二重に書いた相手は 1 つにまとめる)。記録に無い相手・循環は除ける (issue 468)。
func TestPlanAfter(t *testing.T) {
	dir := t.TempDir()
	for range 3 {
		submit(t, dir, Request{Kind: "add", Title: "t"})
	}
	applyAll(t, dir)
	submit(t, dir, Request{Kind: "plan", CardID: "C-002", After: []string{"C-001", "C-001"}})
	if res := applyAll(t, dir); res[0].Err != "" {
		t.Fatalf("plan --after を除けた: %+v", res)
	}
	if c := cardOf(t, dir, "C-002"); !slices.Equal(c.After, []string{"C-001"}) || !strings.Contains(c.History[len(c.History)-1].Text, "C-001 の後") {
		t.Fatalf("順番が付いていない / 履歴に無い: %+v", c)
	}
	submit(t, dir, Request{Kind: "plan", CardID: "C-003", After: []string{"C-009"}})
	if res := applyAll(t, dir); res[0].Err == "" || !strings.Contains(res[0].Err, "C-009") {
		t.Fatalf("記録に無い相手を受けた: %+v", res)
	}
	submit(t, dir, Request{Kind: "plan", CardID: "C-001", After: []string{"C-002"}})
	if res := applyAll(t, dir); res[0].Err == "" || !strings.Contains(res[0].Err, "循環") {
		t.Fatalf("循環する順番を受けた: %+v", res)
	}
}

// 依頼の列の前のカードを消したら、後のカードの順番から外れる (外さないと相手の無い順番で後のカードが除けられ続ける)。
func TestDeletePredecessorStripsAfter(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "前"})
	submit(t, dir, Request{Kind: "add", Title: "後"})
	applyAll(t, dir)
	submit(t, dir, Request{Kind: "plan", CardID: "C-002", After: []string{"C-001"}})
	submit(t, dir, Request{Kind: "delete", CardID: "C-001"})
	if res := applyAll(t, dir); res[0].Err != "" || res[1].Err != "" {
		t.Fatalf("前のカードを消せない: %+v", res)
	}
	if c := cardOf(t, dir, "C-002"); len(c.After) != 0 {
		t.Fatalf("消したカードが順番に残った: %+v", c.After)
	}
}
