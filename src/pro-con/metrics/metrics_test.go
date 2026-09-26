package metrics_test

import (
	"testing"
	"time"

	"pro-con/card"
	"pro-con/metrics"
	"pro-con/store"
)

var t0 = time.Date(2026, 9, 25, 1, 0, 0, 0, time.UTC)

func at(n int) time.Time { return t0.Add(time.Duration(n) * time.Minute) }

// walk は 1 枚のカードを本物の遷移 (store.Apply と、dispatcher が使う card の口) で動かし、閉じたカードを返す。
// 依頼 10 分 → 分解済み 5 分 → 作業中 15 分 → 質問待ち 20 分 (うち 5 分目に PM が人に回した) → 分解済み 5 分 → 作業中 15 分 →
// レビュー 10 分 → 差し戻しで分解済み 5 分 → 作業中 15 分 (途中でテストの係に頼む) → レビュー 10 分 → 完了。
func walk(t *testing.T) card.Card {
	t.Helper()
	dir := t.TempDir()
	submit := func(at time.Time, r store.Request) {
		t.Helper()
		if _, err := store.Submit(dir, r); err != nil {
			t.Fatal(err)
		}
		res, err := store.Apply(dir, at)
		if err != nil || len(res) != 1 || res[0].Err != "" {
			t.Fatalf("%s を当てられない: %+v %v", r.Kind, res, err)
		}
	}
	settle := func(at time.Time, how string) { // dispatcher の settle と同じ形 (履歴を書いてから作業中へ)
		t.Helper()
		if err := store.Update(dir, func(s *store.State) error {
			c := &s.Cards[0]
			c.Session = "aaaa0001"
			c.History = append(c.History, card.Event{At: at, Text: card.LaunchedText(how, "aaaa0001")})
			c.Enter(card.Running, at)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	submit(at(0), store.Request{Kind: "add", Title: "t", Repo: "dotfiles"})
	submit(at(10), store.Request{Kind: "plan", CardID: "C-001", Points: 3, Issues: []card.IssueRef{{Repo: "dotfiles", Number: 516}}})
	settle(at(15), "起動")
	submit(at(30), store.Request{Kind: "ask", CardID: "C-001", Question: "どちら?"})
	submit(at(35), store.Request{Kind: "handoff", CardID: "C-001", From: card.PMName, Text: "人が決めること"})
	submit(at(50), store.Request{Kind: "answer", CardID: "C-001", Answer: "A", From: "人間"})
	settle(at(55), "再開")
	submit(at(70), store.Request{Kind: "review", CardID: "C-001"})
	submit(at(80), store.Request{Kind: "rework", CardID: "C-001", Rework: "直して"})
	settle(at(85), "再開")
	submit(at(90), store.Request{Kind: "run", CardID: "C-001", Command: "make test"})
	if err := store.Update(dir, func(s *store.State) error { s.Cards[0].DropRun(); return nil }); err != nil { // 結果が届いた (列は作業中のまま)
		t.Fatal(err)
	}
	submit(at(100), store.Request{Kind: "review", CardID: "C-001"})
	submit(at(110), store.Request{Kind: "close", CardID: "C-001"})
	st, err := store.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return st.Cards[0]
}

// 列ごとの時間・人の番の時間・回数・時刻が、カードの履歴 (本物の遷移で作ったもの) と一致する。
func TestFromCardMatchesHistory(t *testing.T) {
	c := walk(t)
	r := metrics.FromCard(c, c.Since, "", card.Roles{})
	m := int64(60)
	if want := (metrics.Stays{Requested: 10 * m, Planned: 15 * m, Running: 45 * m, Waiting: 20 * m, Review: 20 * m}); r.Stay == nil || *r.Stay != want {
		t.Fatalf("列ごとの時間 = %+v, want %+v", r.Stay, want)
	}
	if want := (metrics.Stays{Waiting: 15 * m}); r.Human == nil || *r.Human != want { // PM が人に回してから答えるまで
		t.Fatalf("人の番の時間 = %+v, want %+v", r.Human, want)
	}
	if want := (metrics.Counts{Resumes: 2, Reworks: 1, Questions: 1, AnsweredByHuman: 1, Runs: 1}); r.Counts != want {
		t.Fatalf("回数 = %+v, want %+v", r.Counts, want)
	}
	if !r.RequestedAt.Equal(at(0)) || !r.PlannedAt.Equal(at(10)) || !r.StartedAt.Equal(at(15)) || !r.ReviewAt.Equal(at(70)) || !r.ClosedAt.Equal(at(110)) {
		t.Fatalf("時刻 = 依頼 %v 分けた %v 起きた %v レビュー %v 閉じた %v", r.RequestedAt, r.PlannedAt, r.StartedAt, r.ReviewAt, r.ClosedAt)
	}
	if r.Points != 3 || r.Ending != metrics.EndIssue || len(r.Issues) != 1 || r.Issues[0] != "dotfiles#516" {
		t.Fatalf("ポイント・終わり方・issue = %d %q %v", r.Points, r.Ending, r.Issues)
	}
}

// 役を起こさない設定のもとでは、その役の番も人の番に数える (card.Turn と同じ)。
func TestFromCardHumanUnderRolesOff(t *testing.T) {
	c := walk(t)
	r := metrics.FromCard(c, c.Since, "", card.Roles{PMOff: true, IntegratorOff: true})
	m := int64(60)
	if want := (metrics.Stays{Requested: 10 * m, Waiting: 20 * m, Review: 20 * m}); r.Human == nil || *r.Human != want {
		t.Fatalf("人の番の時間 = %+v, want %+v", r.Human, want)
	}
}

// 足跡の無いカード (この issue より前に閉じた) は列ごとの時間を 0 ではなく「無い」にする。回数は履歴から数える。
func TestFromCardWithoutTrail(t *testing.T) {
	c := card.Card{ID: "C-001", State: card.Done, Ending: card.EndAnswered, Since: at(30),
		History: []card.Event{{At: at(0), Text: "依頼を受けた"}, {At: at(5), Text: card.PMName + card.AnsweredMark + "B"}, {At: at(6), Text: "人間 が回答した: X"}, {At: at(7), Text: store.AttachPrefix + "田中 が回答した: Y"}}}
	r := metrics.FromCard(c, c.Since, "", card.Roles{})
	if r.Stay != nil || r.Human != nil {
		t.Fatalf("足跡が無いのに列ごとの時間がある: %+v %+v", r.Stay, r.Human)
	}
	if !r.RequestedAt.Equal(at(0)) || r.Ending != "回答済み" || r.Counts.AnsweredByPM != 1 || r.Counts.AnsweredByHuman != 1 {
		t.Fatalf("行 = %+v", r) // attach の指示の中の同じ文は数えない
	}
}

// 足跡が依頼の列から始まっていないカード (入れた時点で動いていた) は、途中からの時間を全部の時間として出さない。
func TestFromCardIgnoresPartialTrail(t *testing.T) {
	c := card.Card{ID: "C-001", State: card.Done, Ending: card.EndAnswered, Since: at(30),
		History: []card.Event{{At: at(0), Text: "依頼を受けた"}}, Trail: []card.Step{{At: at(20), State: card.Review}, {At: at(30), State: card.Done}}}
	r := metrics.FromCard(c, c.Since, "", card.Roles{})
	if r.Stay != nil || r.Human != nil || !r.RequestedAt.Equal(at(0)) || !r.ReviewAt.IsZero() {
		t.Fatalf("途中からの足跡を使った: %+v", r)
	}
}
