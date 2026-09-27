package backend

import (
	"slices"
	"testing"
)

// 枠に数えたカードを読めたら、PG が 1 本も無くても作業中と待機中の区分を出す (設定の枠と比べる数を 0 でも出す。issue 557)。
// 読めなければ「判定できない」の区分だけ。PG より前の行と後ろの行 (テストの係・画面) は区分の外に元の並びで残す。
func TestSplitPGsSections(t *testing.T) {
	rows := []Proc{{Role: "dispatcher"}, {Role: "PM"}, {Role: "テストの係"}, {Role: "画面"}}
	slotsOf := func(secs []PGSection) []string {
		var out []string
		for _, s := range secs {
			out = append(out, s.Slot)
		}
		return out
	}
	head, secs, tail := SplitPGs(rows, true)
	if len(head) != 2 || len(tail) != 2 || !slices.Equal(slotsOf(secs), []string{SlotHeld, SlotIdle}) {
		t.Fatalf("head=%v secs=%v tail=%v", head, slotsOf(secs), tail)
	}
	if got := secs[0].Title(2); got != "PG 作業中 (枠を使う) 0 / 枠 2" {
		t.Fatalf("見出し = %q", got)
	}
	if _, secs, _ = SplitPGs(rows, false); !slices.Equal(slotsOf(secs), []string{SlotUnknown}) {
		t.Fatalf("読めないのに枠で分けた: %v", slotsOf(secs))
	}
}
