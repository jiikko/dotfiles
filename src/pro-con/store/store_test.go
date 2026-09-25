package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/card"
	"pro-con/wake"
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

// setState は記録のカードの状態を直接書き換える (作業中への遷移は dispatcher の仕事で、3a の箱からは起こせないため)。
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

// 依頼の流れ: add → plan → (dispatcher が作業中へ) → ask → answer (分解済みへ戻る) / review → close。どの時点でも不変条件を破らない。
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

// 差し戻し (rework) はレビュー待ちから分解済みへ戻し、同じ session を再開する文を持たせる。履歴には直してほしい点を原文のまま残す。
func TestReworkReturnsReviewToPlanned(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "x"})
	applyAll(t, dir)
	setState(t, dir, "C-001", card.Review)
	fix := strings.Repeat("テストが変異で red になるかを確かめていない。", 5) // 80 文字を超える (切り詰めたら落ちる)
	submit(t, dir, Request{Kind: "rework", CardID: "C-001", Rework: fix})
	if res := applyAll(t, dir); len(res) != 1 || res[0].Err != "" {
		t.Fatalf("rework: %+v", res)
	}
	c := cardOf(t, dir, "C-001")
	if c.State != card.Planned || !strings.Contains(c.Resume, fix) || !strings.Contains(c.Resume, "pro-con card review C-001") {
		t.Fatalf("rework の後: %v Resume=%q", c.State, c.Resume)
	}
	if got := c.History[len(c.History)-1].Text; got != "差し戻した: "+fix {
		t.Fatalf("履歴に原文が残らない: %q", got)
	}
}

// レビュー待ちでないカードへの差し戻しと、直してほしい点が空の差し戻しは断る。
func TestReworkRejectsOutsideReview(t *testing.T) {
	for _, tc := range []struct {
		state card.State
		fix   string
		want  string
	}{
		{card.Running, "直して", "レビュー待ちではない"},
		{card.Done, "直して", "レビュー待ちではない"},
		{card.Review, " ", "直してほしい点が空"},
	} {
		dir := t.TempDir()
		submit(t, dir, Request{Kind: "add", Title: "x"})
		applyAll(t, dir)
		setState(t, dir, "C-001", tc.state)
		submit(t, dir, Request{Kind: "rework", CardID: "C-001", Rework: tc.fix})
		if res := applyAll(t, dir); len(res) != 1 || !strings.Contains(res[0].Err, tc.want) {
			t.Fatalf("%v %q: %+v", tc.state, tc.fix, res)
		}
		if c := cardOf(t, dir, "C-001"); c.State != tc.state || c.Resume != "" {
			t.Fatalf("断った差し戻しでカードが変わった: %v Resume=%q", c.State, c.Resume)
		}
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

// 除けた依頼を rejected/ へ移す前に落ちても、次の Apply がもう一度判定して理由つきで除ける (理由を残さずに消さない)。
func TestRejectedSurvivesCrashBeforeMove(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "x"})
	applyAll(t, dir)
	id := submit(t, dir, Request{Kind: "answer", CardID: "C-001", Answer: "y"}) // 質問待ちではない
	box := filepath.Join(dir, InboxDir)
	data, _ := os.ReadFile(filepath.Join(box, id+".json"))
	applyAll(t, dir)
	// 記録を書いた後、rejected/ へ移す前に落ちた形
	if err := os.RemoveAll(filepath.Join(box, RejectedDir)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(box, id+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	applyAll(t, dir)
	why, err := os.ReadFile(filepath.Join(box, RejectedDir, id+".reason"))
	if err != nil || !strings.Contains(string(why), "質問待ちではない") {
		t.Fatalf("落ちた後の除けた依頼が理由を残さずに消えた: %q %v", why, err)
	}
}

// 1 回の Apply で適用するのは perApply 件まで (残りは次の Apply)。控えより多く適用して、落ちたときに二重適用する形を作らない。
func TestApplyCapsPerCall(t *testing.T) {
	dir := t.TempDir()
	for i := range perApply + 1 {
		submit(t, dir, Request{Kind: "add", Title: "x", At: t0.Add(time.Duration(i) * time.Millisecond)})
	}
	if res := applyAll(t, dir); len(res) != perApply {
		t.Fatalf("1 回で %d 件を適用した (上限 %d)", len(res), perApply)
	}
	if res := applyAll(t, dir); len(res) != 1 {
		t.Fatalf("残りを次の Apply で適用していない: %d", len(res))
	}
}

// 除けた依頼は、rejected/ へ移す前に落ちても判定し直さない。後の依頼で状態が変わった後に判定し直すと、
// 除けたはずの回答が別の質問への回答として適用される (順序が入れ替わる)。
func TestRejectedIsNotRejudgedAfterCrash(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "x"})
	applyAll(t, dir)
	setState(t, dir, "C-001", card.Running)
	box := filepath.Join(dir, InboxDir)
	ans := submit(t, dir, Request{Kind: "answer", CardID: "C-001", Answer: "古い回答", At: t0}) // 作業中なので除ける
	data, _ := os.ReadFile(filepath.Join(box, ans+".json"))
	submit(t, dir, Request{Kind: "ask", CardID: "C-001", Question: "新しい質問", At: t0.Add(time.Second)})
	applyAll(t, dir)
	// 記録を書いた後、rejected/ へ移す前に落ちた形
	if err := os.RemoveAll(filepath.Join(box, RejectedDir)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(box, ans+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	applyAll(t, dir)
	if c := cardOf(t, dir, "C-001"); c.State != card.Waiting || c.Resume != "" {
		t.Fatalf("除けた回答を判定し直して適用した: %v Resume=%q", c.State, c.Resume)
	}
	if _, err := os.Stat(filepath.Join(box, RejectedDir, ans+".json")); err != nil {
		t.Fatalf("判定し直さずに rejected/ へ移していない: %v", err)
	}
}

// テストの係への頼みは作業中のカードだけ。結果を返す前にもう 1 本は頼めない。
func TestRunRequest(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "x"})
	applyAll(t, dir)
	submit(t, dir, Request{Kind: "run", CardID: "C-001", Command: "make test"})
	if res := applyAll(t, dir); !strings.Contains(res[0].Err, "作業中の列に無い") {
		t.Fatalf("作業中でないカードの頼みを受けた: %+v", res)
	}
	setState(t, dir, "C-001", card.Running)
	submit(t, dir, Request{Kind: "run", CardID: "C-001", Command: "make test"})
	submit(t, dir, Request{Kind: "run", CardID: "C-001", Command: "make lint"})
	res := applyAll(t, dir)
	c := cardOf(t, dir, "C-001")
	if res[0].Err != "" || !strings.Contains(res[1].Err, "まだ返していない") || c.Run != "make test" || c.Wait.Kind != card.WaitResource {
		t.Fatalf("頼みを受けない / 2 本目を受けた: %+v Run=%q wait=%v", res, c.Run, c.Wait.Kind)
	}
}

// global で受けた依頼 (repo 無し) は、PM が分けた issue の repo で作業する。repo のある依頼は変えない。
func TestPlanSetsRepoFromIssue(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "global", At: t0})
	submit(t, dir, Request{Kind: "add", Title: "scoped", Repo: "dotfiles", At: t0.Add(time.Second)})
	applyAll(t, dir)
	for _, id := range []string{"C-001", "C-002"} {
		submit(t, dir, Request{Kind: "plan", CardID: id, Issues: []card.IssueRef{{Repo: "glogx", Number: 3, Status: "open"}}})
	}
	applyAll(t, dir)
	if c := cardOf(t, dir, "C-001"); c.Repo != "glogx" {
		t.Fatalf("global の依頼に issue の repo を付けない: %q", c.Repo)
	}
	if c := cardOf(t, dir, "C-002"); c.Repo != "dotfiles" {
		t.Fatalf("repo のある依頼の repo を変えた: %q", c.Repo)
	}
}

// 箱に置いたら dispatcher を起こす (package wake)。
func TestSubmitPokesDispatcher(t *testing.T) {
	dir := t.TempDir()
	srv, err := wake.Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Close() }()
	if _, err := Submit(dir, Request{Kind: "add", Title: "t"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-srv.Wakes():
	case <-time.After(10 * time.Second):
		t.Fatal("箱に置いても dispatcher を起こさない")
	}
}

// attach の間の指示は、どの列のカードにも原文と打った時刻で履歴に足す。状態は変えない。空の指示は受けない。
func TestAttachAppendsSaidVerbatim(t *testing.T) {
	dir := t.TempDir()
	submit(t, dir, Request{Kind: "add", Title: "x"})
	applyAll(t, dir)
	said := t0.Add(-time.Minute)
	long := strings.Repeat("長い指示", 50)
	submit(t, dir, Request{Kind: "attach", CardID: "C-001", Said: []card.Event{{At: said, Text: "a"}, {At: said.Add(time.Second), Text: long}}})
	submit(t, dir, Request{Kind: "attach", CardID: "C-001"})
	submit(t, dir, Request{Kind: "attach", CardID: "C-001", Said: []card.Event{{At: said, Text: " "}}})
	res := applyAll(t, dir)
	if res[0].Err != "" || res[1].Err == "" || res[2].Err == "" {
		t.Fatalf("指示を受けない / 空の指示を受けた: %+v", res)
	}
	c := cardOf(t, dir, "C-001")
	h := c.History[len(c.History)-2:]
	if c.State != card.Requested || h[0].Text != AttachPrefix+"a" || !h[0].At.Equal(said) || h[1].Text != AttachPrefix+long {
		t.Fatalf("状態を変えた / 原文と打った時刻のまま残していない: %v %+v", c.State, h)
	}
}
