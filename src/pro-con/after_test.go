package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"pro-con/card"
	"pro-con/store"
)

// plan --after <カード> は複数受け、依頼に順番を載せる (issue 468)。値の無い --after は使い方の誤り。
func TestCardPlanAfterParse(t *testing.T) {
	r, _, err := parseCardWait([]string{"plan", "C-003", "--issue", "dotfiles#1", "--after", "C-001", "--after", "C-002"})
	if err != nil || r.Kind != "plan" || r.CardID != "C-003" || !slices.Equal(r.After, []string{"C-001", "C-002"}) || len(r.Issues) != 1 {
		t.Fatalf("plan --after を読めない: %+v %v", r, err)
	}
	if _, _, err := parseCardWait([]string{"plan", "C-003", "--after", " "}); err == nil {
		t.Fatal("空の --after を読んだ")
	}
}

// 一覧と詳細に「C-001 の後」が出る (順番で起動を待っている理由が見える)。前のカードが完了したら待ちとしては出さない。
func TestCardListShowsAfter(t *testing.T) {
	env := viewFixture(t) // C-001 は質問待ち、C-002 は依頼
	mustSubmit(t, env.dir, store.Request{Kind: "plan", CardID: "C-002", After: []string{"C-001"}})
	mustApply(t, env.dir)
	if _, out, _ := viewCmd(t, env, "list"); !strings.Contains(out, "待ち: C-001 の後") {
		t.Fatalf("順番の待ちが一覧で見えない: %q", out)
	}
	_, out, _ := viewCmd(t, env, "list", "--state", "planned", "--json")
	var got []cardSummary
	if json.Unmarshal([]byte(out), &got) != nil || len(got) != 1 || got[0].Waiting != "C-001 の後" {
		t.Fatalf("--json に順番の待ちが無い: %q", out)
	}
	if _, out, _ := viewCmd(t, env, "show", "C-002"); !strings.Contains(out, "順番: C-001 の後") || !strings.Contains(out, "待ち: C-001 の後") {
		t.Fatalf("詳細に順番が無い: %q", out)
	}
	if err := store.Update(env.dir, func(st *store.State) error {
		for i := range st.Cards {
			if st.Cards[i].ID == "C-001" {
				st.Cards[i].State, st.Cards[i].Wait, st.Cards[i].Ending = card.Done, card.Wait{}, card.EndAnswered
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, out, _ := viewCmd(t, env, "show", "C-002"); !strings.Contains(out, "順番: C-001 の後") || strings.Contains(out, "待ち: C-001") {
		t.Fatalf("前が完了した後も待ちとして出す / 順番を消した: %q", out)
	}
}
