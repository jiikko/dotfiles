package store

import (
	"slices"
	"strings"
	"testing"
	"time"

	"pro-con/card"
)

// 箱に続けて置いた並べ替えは、置いた順に適用の時点の並びへ当たる (速く続けて押しても押した回数だけ動く)。
// 端を越えた依頼は理由つきで除け、記録は開き直しても (読み直しても) 残る。
func TestMoveAppliesEachRequest(t *testing.T) {
	dir := t.TempDir()
	for range 3 {
		submit(t, dir, Request{Kind: "add", Title: "t"})
	}
	applyAll(t, dir)
	for range 3 { // 画面の古い並びでは C-003 は 3 番目のまま。3 回目は先頭を越える
		submit(t, dir, Request{Kind: "move", CardID: "C-003", Delta: -1})
	}
	res := applyAll(t, dir)
	if res[0].Err != "" || res[1].Err != "" || !strings.Contains(res[2].Err, "端") {
		t.Fatalf("2 回動いて 3 回目は端で除けるはず: %+v", res)
	}
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cs := slices.Clone(st.Cards)
	slices.SortStableFunc(cs, card.LaneCompare)
	if got := []string{cs[0].ID, cs[1].ID, cs[2].ID}; !slices.Equal(got, []string{"C-003", "C-001", "C-002"}) {
		t.Fatalf("記録の並びが先頭へ上がっていない: %v", got)
	}
	if h := cardOf(t, dir, "C-003").History; !strings.Contains(h[len(h)-1].Text, "優先度を上げた (C-001 の上へ)") {
		t.Fatalf("履歴に入れ替えが残っていない: %+v", h)
	}
}

// 順番 (--after) の前のカードが終わっていないカードを上げると、並びより順番が勝つことを履歴に書く (issue 468 と食い違ったとき)。
func TestMoveBlockedCardSaysItWaits(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "前"})
	submit(t, dir, Request{Kind: "add", Title: "後"})
	applyAll(t, dir)
	submit(t, dir, Request{Kind: "plan", CardID: "C-001"})
	submit(t, dir, Request{Kind: "plan", CardID: "C-002", After: []string{"C-001"}})
	submit(t, dir, Request{Kind: "move", CardID: "C-002", Delta: -1})
	if res := applyAll(t, dir); res[2].Err != "" {
		t.Fatalf("並べ替えを除けた: %+v", res)
	}
	if h := cardOf(t, dir, "C-002").History; !strings.Contains(h[len(h)-1].Text, "C-001 の完了を待つので、それまでは起動しない") {
		t.Fatalf("順番で起動しないことが履歴に無い: %q", h[len(h)-1].Text)
	}
}

// 押した後に列を移ったカード (回答で分解済みへ・起動して作業中へ) は、見ていない列で入れ替えない。
func TestMoveRejectsAfterLaneChange(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "a"})
	submit(t, dir, Request{Kind: "add", Title: "b"})
	applyAll(t, dir)
	seen := cardOf(t, dir, "C-002").Since
	submit(t, dir, Request{Kind: "plan", CardID: "C-001"})
	submit(t, dir, Request{Kind: "plan", CardID: "C-002"}) // 押した後に分解済みへ移った
	submit(t, dir, Request{Kind: "move", CardID: "C-002", Delta: -1, Seen: seen})
	res, err := Apply(dir, t0.Add(time.Minute), nil) // 押した時刻より後の Tick で適用する
	if err != nil || !strings.Contains(res[2].Err, "列へ移った") {
		t.Fatalf("見ていない列で入れ替えた: %+v", res)
	}
}
