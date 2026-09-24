package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/card"
)

var t0 = time.Date(2026, 9, 25, 1, 0, 0, 0, time.UTC)

func submit(t *testing.T, dir string, r Request) string {
	t.Helper()
	id, err := Submit(dir, r)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func applyAll(t *testing.T, dir string) []Result {
	t.Helper()
	res, err := Apply(dir, t0)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func cardOf(t *testing.T, dir, id string) card.Card {
	t.Helper()
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range st.Cards {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("%s が記録に無い", id)
	return card.Card{}
}

// setState は記録のカードの状態を直接書き換える (作業中への遷移は daemon の仕事で、3a の箱からは起こせないため)。
func setState(t *testing.T, dir, id string, s card.State) {
	t.Helper()
	st, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := range st.Cards {
		if st.Cards[i].ID == id {
			st.Cards[i].State = s
		}
	}
	data, _ := json.Marshal(st)
	if err := os.WriteFile(filepath.Join(dir, StateFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// 依頼の流れ: add → plan → (daemon が作業中へ) → ask → answer (分解済みへ戻る) / review → close。どの時点でも不変条件を破らない。
func TestLifecycle(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "色を直す", Request: "statusline の色が見えづらい", Repo: "dotfiles"})
	if res := applyAll(t, dir); len(res) != 1 || res[0].Err != "" || res[0].CardID != "C-001" {
		t.Fatalf("add: %+v", res)
	}
	submit(t, dir, Request{Kind: "plan", CardID: "C-001", Issues: []card.IssueRef{{Repo: "dotfiles", Number: 900, Status: "open"}}})
	applyAll(t, dir)
	if c := cardOf(t, dir, "C-001"); c.State != card.Planned || len(c.Issues) != 1 {
		t.Fatalf("plan の後: %v issues=%d", c.State, len(c.Issues))
	}
	setState(t, dir, "C-001", card.Running)
	submit(t, dir, Request{Kind: "ask", CardID: "C-001", Question: "赤と青どちら?"})
	applyAll(t, dir)
	if c := cardOf(t, dir, "C-001"); c.State != card.Waiting || c.Wait.Question != "赤と青どちら?" {
		t.Fatalf("ask の後: %v %q", c.State, c.Wait.Question)
	}
	submit(t, dir, Request{Kind: "answer", CardID: "C-001", Answer: "青", From: "人間"})
	applyAll(t, dir)
	if c := cardOf(t, dir, "C-001"); c.State != card.Planned || c.Wait.Kind != card.WaitNone {
		t.Fatalf("answer の後は分解済みへ戻る (PG の空きで resume): %v", c.State)
	}
	setState(t, dir, "C-001", card.Running)
	submit(t, dir, Request{Kind: "review", CardID: "C-001"})
	submit(t, dir, Request{Kind: "close", CardID: "C-001"})
	if res := applyAll(t, dir); len(res) != 2 || res[0].Err != "" || res[1].Err != "" {
		t.Fatalf("review → close: %+v", res)
	}
	st, _ := Load(dir)
	if c := cardOf(t, dir, "C-001"); c.State != card.Done || len(card.Check(st.Cards)) != 0 {
		t.Fatalf("close の後: %v 違反 %v", c.State, card.Check(st.Cards))
	}
}

// 規則に反する依頼は記録に入れず、理由つきで rejected/ へ除ける (黙って捨てない)。カードは変わらない。
func TestRejectsInvalidTransition(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "x"})
	applyAll(t, dir)
	id := submit(t, dir, Request{Kind: "answer", CardID: "C-001", Answer: "y"}) // 質問待ちではない
	res := applyAll(t, dir)
	if len(res) != 1 || !strings.Contains(res[0].Err, "質問待ちではない") {
		t.Fatalf("質問待ちでないカードへの回答を受けた: %+v", res)
	}
	if c := cardOf(t, dir, "C-001"); c.State != card.Requested {
		t.Fatalf("除けた依頼でカードが変わった: %v", c.State)
	}
	why, err := os.ReadFile(filepath.Join(dir, InboxDir, RejectedDir, id+".reason"))
	if err != nil || !strings.Contains(string(why), "質問待ちではない") {
		t.Fatalf("除けた理由が残っていない: %q %v", why, err)
	}
	if _, err := os.Stat(filepath.Join(dir, InboxDir, id+".json")); !os.IsNotExist(err) {
		t.Fatal("除けた依頼が箱に残っている (次の Apply でまた除ける)")
	}
}

// 記録を書いた後、箱のファイルを片付ける前に落ちても、次の Apply で二重に適用しない。
func TestNoDoubleApplyAfterCrash(t *testing.T) {
	dir := t.TempDir()
	id := submit(t, dir, Request{Kind: "add", Title: "x"})
	data, _ := os.ReadFile(filepath.Join(dir, InboxDir, id+".json"))
	applyAll(t, dir)
	// 片付ける前に落ちた形: 同じ依頼のファイルが箱に戻っている
	if err := os.WriteFile(filepath.Join(dir, InboxDir, id+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	applyAll(t, dir)
	if st, _ := Load(dir); len(st.Cards) != 1 {
		t.Fatalf("同じ依頼を二重に適用した: %d 枚", len(st.Cards))
	}
}

// 壊れた依頼 (読めない JSON) は除ける。完了に issue も終わり方も無い依頼は、不変条件で拒む。
func TestRejectsBrokenAndInvariantBreaking(t *testing.T) {
	dir := t.TempDir()
	box := filepath.Join(dir, InboxDir)
	if err := os.MkdirAll(box, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(box, "00000000000000000001-bad.json"), []byte("{壊れた"), 0o600); err != nil {
		t.Fatal(err)
	}
	submit(t, dir, Request{Kind: "add", Title: "x"})
	submit(t, dir, Request{Kind: "close", CardID: "C-001"}) // 依頼の列から、終わり方も issue も無く閉じる
	res := applyAll(t, dir)
	if len(res) != 3 || res[0].Err == "" || res[1].Err != "" || !strings.Contains(res[2].Err, "不変条件") {
		t.Fatalf("壊れた依頼 / 不変条件を破る完了を除けていない: %+v", res)
	}
	if c := cardOf(t, dir, "C-001"); c.State != card.Requested {
		t.Fatalf("不変条件を破る完了が記録に入った: %v", c.State)
	}
	submit(t, dir, Request{Kind: "close", CardID: "C-001", Ending: card.EndAnswered}) // 終わり方があれば閉じられる
	if res := applyAll(t, dir); res[0].Err != "" {
		t.Fatalf("終わり方つきの完了を拒んだ: %+v", res)
	}
}

// 箱の依頼は置いた順に適用する (採番も置いた順)。Submit は一時ファイルを残さない。
func TestAppliesInSubmitOrder(t *testing.T) {
	dir := t.TempDir()
	for i, title := range []string{"一", "二", "三"} {
		submit(t, dir, Request{Kind: "add", Title: title, At: t0.Add(time.Duration(i) * time.Second)})
	}
	if ms, _ := filepath.Glob(filepath.Join(dir, InboxDir, ".*tmp-*")); len(ms) != 0 {
		t.Fatalf("一時ファイルが残った: %v", ms)
	}
	applyAll(t, dir)
	for i, title := range []string{"一", "二", "三"} {
		if c := cardOf(t, dir, []string{"C-001", "C-002", "C-003"}[i]); c.Title != title {
			t.Fatalf("置いた順に採番していない: %s は %q", c.ID, c.Title)
		}
	}
}

// 壊れた記録は空と区別する (空として扱うと、次の Apply が全部のカードを消して書き直す)。
func TestBrokenStateIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, StateFile), []byte("{壊れた"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("壊れた記録を空として読んだ")
	}
	submit(t, dir, Request{Kind: "add", Title: "x"})
	if _, err := Apply(dir, t0); err == nil {
		t.Fatal("壊れた記録の上に適用した")
	}
}
