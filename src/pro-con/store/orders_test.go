package store

import (
	"strings"
	"testing"
	"time"

	"pro-con/card"
)

// setDone は記録のカードを完了にする (その場で回答した形。完了への遷移は PM の close で、ここでは経緯を問わない)。
func setDone(t *testing.T, dir, id string) {
	t.Helper()
	if err := Update(dir, time.Now(), func(st *State) error {
		for i := range st.Cards {
			if st.Cards[i].ID == id {
				st.Cards[i].State, st.Cards[i].Ending = card.Done, card.EndAnswered
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// 追加オーダー (追記・方針変更) はカードに未達で積む。完了のカード・別件・空の本文は除ける。レビュー待ちは受ける
// (PG の review と同じ Apply で来たオーダーを捨てない。issue 438)。
func TestOrderIsQueuedOnCard(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "a"})
	submit(t, dir, Request{Kind: "add", Title: "b"})
	applyAll(t, dir)
	setState(t, dir, "C-001", card.Review)
	setDone(t, dir, "C-002")
	submit(t, dir, Request{Kind: "order", CardID: "C-001", Order: card.OrderRedirect, Text: "青にして"})
	submit(t, dir, Request{Kind: "order", CardID: "C-002", Text: "x"})
	submit(t, dir, Request{Kind: "order", CardID: "C-001", Order: card.OrderSeparate, Text: "x"})
	submit(t, dir, Request{Kind: "order", CardID: "C-001", Text: " "})
	res := applyAll(t, dir)
	if len(res) != 4 || res[0].Err != "" || res[1].Err == "" || res[2].Err == "" || res[3].Err == "" {
		t.Fatalf("受ける / 除けるの振り分け: %+v", res)
	}
	c := cardOf(t, dir, "C-001")
	if len(c.Orders) != 1 || c.Orders[0].Kind != card.OrderRedirect || c.Orders[0].Text != "青にして" || c.Orders[0].Delivered || !c.Orders[0].At.Equal(t0) {
		t.Fatalf("未達のオーダーとして積まれていない: %+v", c.Orders)
	}
}

// 別件は元のカードの子の新しい依頼になり、元のカードの履歴にも残る。元のカードが無ければ除ける (不変条件: 親カードが存在しない)。
func TestAddWithParent(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "元"})
	applyAll(t, dir)
	submit(t, dir, Request{Kind: "add", ParentID: "C-001", Request: "別の話"})
	submit(t, dir, Request{Kind: "add", ParentID: "C-099", Request: "迷子"})
	res := applyAll(t, dir)
	if len(res) != 2 || res[0].Err != "" || res[1].Err == "" {
		t.Fatalf("親の無い別件を通した / 親のある別件を除けた: %+v", res)
	}
	child, parent := cardOf(t, dir, "C-002"), cardOf(t, dir, "C-001")
	if child.ParentID != "C-001" || !strings.Contains(parent.History[len(parent.History)-1].Text, "C-002") {
		t.Fatalf("親子がつながっていない: child=%+v parent=%+v", child, parent.History)
	}
}

// btw はどの列のカードでも未回答で積む。空の質問は除ける。
func TestBtwIsQueued(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "a"})
	applyAll(t, dir)
	submit(t, dir, Request{Kind: "btw", CardID: "C-001", Question: "今どう?"})
	submit(t, dir, Request{Kind: "btw", CardID: "C-001", Question: ""})
	res := applyAll(t, dir)
	if len(res) != 2 || res[0].Err != "" || res[1].Err == "" {
		t.Fatalf("%+v", res)
	}
	if c := cardOf(t, dir, "C-001"); len(c.Btws) != 1 || c.Btws[0].Question != "今どう?" || !c.Btws[0].Answered.IsZero() {
		t.Fatalf("未回答の btw として積まれていない: %+v", c.Btws)
	}
}

// 片付けは、頼まれたカードのうち今も完了のものだけを Archived にする (頼まれていない完了のカード・完了でないカードは触らない)。
func TestClearArchivesOnlyListedDone(t *testing.T) {
	dir := t.TempDir()
	for range 3 {
		submit(t, dir, Request{Kind: "add", Title: "a"})
	}
	applyAll(t, dir)
	setDone(t, dir, "C-001")
	setDone(t, dir, "C-003")
	submit(t, dir, Request{Kind: "clear", Cards: []string{"C-001", "C-002", "C-404"}})
	submit(t, dir, Request{Kind: "clear"})
	res := applyAll(t, dir)
	if len(res) != 2 || res[0].Err != "" || res[1].Err == "" {
		t.Fatalf("%+v", res)
	}
	if !cardOf(t, dir, "C-001").Archived || cardOf(t, dir, "C-002").Archived || cardOf(t, dir, "C-003").Archived {
		t.Fatal("頼まれた完了のカードだけを片付けていない")
	}
}

// 未達の追加オーダーが残るカードは閉じない (同じ Apply でオーダーの後に close が来ても、オーダーを完了のカードに埋めない)。
func TestCloseRefusedWithPendingOrder(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "a"})
	applyAll(t, dir)
	setState(t, dir, "C-001", card.Review)
	submit(t, dir, Request{Kind: "order", CardID: "C-001", Text: "README も"})
	submit(t, dir, Request{Kind: "close", CardID: "C-001", Ending: card.EndAnswered})
	res := applyAll(t, dir)
	if len(res) != 2 || res[0].Err != "" || res[1].Err == "" {
		t.Fatalf("未達のオーダーを残したまま閉じた: %+v", res)
	}
	if c := cardOf(t, dir, "C-001"); c.State != card.Review {
		t.Fatalf("%v", c.State)
	}
}
