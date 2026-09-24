package card

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func reasons(vs []Violation) []string {
	var out []string
	for _, v := range vs {
		out = append(out, v.CardID+": "+v.Reason)
	}
	return out
}

func TestCheckAcceptsValidCards(t *testing.T) {
	cards := []Card{
		{ID: "A", State: Done, Issues: []IssueRef{{Repo: "r", Number: 1}}},
		{ID: "B", State: Done, Ending: EndAnswered},
		{ID: "C", State: Waiting, Wait: Wait{Kind: WaitQuestion}, ParentID: "A"},
		{ID: "D", State: Running, Wait: Wait{Kind: WaitResource}},
	}
	if vs := Check(cards); len(vs) != 0 {
		t.Fatalf("違反が無いはずの集合で違反が出た: %v", reasons(vs))
	}
}

// 不変条件ごとに、それだけを破ったカードを 1 枚ずつ置き、そのカードだけが違反として出ることを見る。
func TestCheckReportsEachViolation(t *testing.T) {
	cases := []struct {
		name   string
		bad    Card
		reason string // 違反の理由に含まれるはずの語 (どの検査が拾ったかを区別する)
	}{
		{"完了したのに issue も終わり方も無い", Card{ID: "X", State: Done}, "終わり方"},
		{"親が存在しない", Card{ID: "X", State: Planned, ParentID: "nope"}, "親カード"},
		{"質問待ちの列なのに回答の要らない待ち", Card{ID: "X", State: Waiting, Wait: Wait{Kind: WaitResource}}, "回答の要る待ちではない"},
		{"回答の要る待ちなのに作業中の列", Card{ID: "X", State: Running, Wait: Wait{Kind: WaitPermission}}, "質問待ちの列に居ない"},
		{"ID の重複", Card{ID: "OK", State: Planned}, "重複"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cards := []Card{{ID: "OK", State: Planned}, tc.bad}
			vs := Check(cards)
			if len(vs) != 1 {
				t.Fatalf("違反はちょうど 1 件のはず: %v", reasons(vs))
			}
			if vs[0].CardID != tc.bad.ID || !strings.Contains(vs[0].Reason, tc.reason) {
				t.Fatalf("違反のカードか理由が違う (want %s / %q): %v", tc.bad.ID, tc.reason, reasons(vs))
			}
		})
	}
}

func TestColumnsCoverEveryState(t *testing.T) {
	for st := Requested; st <= Done; st++ {
		if !slices.Contains(Columns, st) {
			t.Fatalf("%s がカンバンの列に無い (その状態のカードが画面から消える)", st.Label())
		}
	}
}

// コマンドの実行中は見込みの 2 倍 (base より短ければ base)、実行していなければ base。
func TestStallThreshold(t *testing.T) {
	base := 5 * time.Minute
	cases := []struct {
		name string
		exec Exec
		want time.Duration
	}{
		{"実行していない", Exec{}, base},
		{"長いテスト", Exec{Command: "make e2e", Expected: 8 * time.Minute}, 16 * time.Minute},
		{"短いテストは base", Exec{Command: "make lint", Expected: time.Minute}, base},
		{"見込み不明は base", Exec{Command: "make test"}, base},
	}
	for _, tc := range cases {
		if got := StallThreshold(Card{Exec: tc.exec}, base); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}
