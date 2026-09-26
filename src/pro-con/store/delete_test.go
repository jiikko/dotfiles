package store

import (
	"strings"
	"testing"
	"time"

	"pro-con/card"
)

// 依頼の列のカードは適用ですぐ記録から外れ、消したこと (題名と依頼した人) を結果に残す。
func TestDeleteRequestedRemovesAtOnce(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "消す依頼"})
	submit(t, dir, Request{Kind: "add", Title: "残す依頼"})
	applyAll(t, dir)
	submit(t, dir, Request{Kind: "delete", CardID: "C-001", From: "PM"})
	res := applyAll(t, dir)
	if len(res) != 1 || res[0].Err != "" || res[0].CardID != "C-001" || !strings.Contains(res[0].Note, "「消す依頼」を削除した (依頼の列。PM が依頼)") {
		t.Fatalf("削除の結果: %+v", res)
	}
	st, _ := Load(dir)
	if len(st.Cards) != 1 || st.Cards[0].ID != "C-002" || st.NextID != 3 {
		t.Fatalf("依頼の列のカードだけを外していない / 番号を戻した: %+v next=%d", st.Cards, st.NextID)
	}
}

// 依頼の列より右のカードは消さずに削除の印を付ける。印の間は、PG の質問・完了・実行の頼みで列を動かさない。二重の削除は何もしない。
func TestDeleteInProgressMarksAndFreezes(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "t"})
	applyAll(t, dir)
	setState(t, dir, "C-001", card.Running)
	submit(t, dir, Request{Kind: "delete", CardID: "C-001"})
	res := applyAll(t, dir)
	c := cardOf(t, dir, "C-001")
	if res[0].Err != "" || !c.Deleting() || c.DeleteBy != "人間" || c.State != card.Running || !strings.Contains(res[0].Note, "削除の依頼を受けた") {
		t.Fatalf("作業中のカードに削除の印を付けていない: %+v %+v", res, c)
	}
	for _, r := range []Request{
		{Kind: "run", CardID: "C-001", Command: "make test"},
		{Kind: "ask", CardID: "C-001", Question: "q"},
		{Kind: "review", CardID: "C-001"},
	} {
		submit(t, dir, r)
		if res := applyAll(t, dir); res[0].Err == "" {
			t.Fatalf("削除の印の付いたカードが %s を受けた", r.Kind)
		}
	}
	submit(t, dir, Request{Kind: "delete", CardID: "C-001"})
	res, err := Apply(dir, t0.Add(time.Minute))
	if again := cardOf(t, dir, "C-001"); err != nil || res[0].Err != "" || !again.DeleteAt.Equal(c.DeleteAt) || len(again.History) != len(c.History) {
		t.Fatalf("二重の削除で印を付け直した / 除けた: %+v %v", res, err)
	}
}

// 子カードの親は消せない (子が親を失う)。理由を付けて除ける。
func TestDeleteRefusesParent(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "親"})
	submit(t, dir, Request{Kind: "add", Title: "子"})
	applyAll(t, dir)
	if err := Update(dir, time.Now(), func(s *State) error { s.Cards[1].ParentID = "C-001"; return nil }); err != nil {
		t.Fatal(err)
	}
	for _, s := range []card.State{card.Requested, card.Running} {
		setState(t, dir, "C-001", s)
		submit(t, dir, Request{Kind: "delete", CardID: "C-001"})
		if res := applyAll(t, dir); !strings.Contains(res[0].Err, "子カード C-002") || cardOf(t, dir, "C-001").Deleting() {
			t.Fatalf("%s の親カードの削除を受けた: %+v", s.Label(), res)
		}
	}
}
