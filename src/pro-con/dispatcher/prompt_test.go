package dispatcher

import (
	"context"
	"strings"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/store"
)

// measured は `claude agents --json` の PG の session の様子を、実測の値のまま持つ (Claude Code 2.1.283 / 2026-09-26 / C-054。
// haiku の bg session を権限の確認と AskUserQuestion で止め、pty の attach で答えて採った。値の説明は agents.StatusWaiting)。
// 🚨 値を足すときは実測してから (推測の値で fake を作ると、本物で人の番にならない / 戻らない判定が緑になる)
var measured = map[string]struct{ status, state, waitingFor string }{
	"作業中":             {"busy", "working", ""},
	"権限の確認":           {"waiting", "blocked", "permission prompt"},
	"AskUserQuestion": {"waiting", "blocked", "input needed"},
	"答えた直後":           {"busy", "working", ""},
	"turn を終えた":       {"idle", "done", ""},
}

// be は rig の PG の session (ss[0]) を実測の形 name にする (pid はそのまま = 生きている)。
func (r *crashRig) be(t *testing.T, name string) {
	t.Helper()
	m, ok := measured[name]
	if !ok {
		t.Fatalf("実測の表に %q が無い", name)
	}
	r.ss[0].Status, r.ss[0].State, r.ss[0].WaitingFor = m.status, m.state, m.waitingFor
}

func countHold(notes []string, sub string) int {
	n := 0
	for _, s := range notes {
		if strings.Contains(s, sub) {
			n++
		}
	}
	return n
}

// 権限の確認で止まった PG のカードは質問待ち (WaitPermission) = 人の番になり、知らせは入るたびに 1 度だけ。
// attach して答えられて動き出したら (busy) 作業中へ戻る。また止まれば、また人の番として知らせる。
func TestPermissionPromptIsHumanTurnUntilAnswered(t *testing.T) {
	r := newCrashRig(t)
	var rec recorder
	rec.rig(r.d)
	r.be(t, "作業中")
	r.tick(t)
	r.be(t, "権限の確認")
	var events []string
	for range 3 { // 同じ様子が続く Tick で出し直さない
		for _, e := range r.tick(t) {
			events = append(events, e.Reason)
		}
	}
	c := states(t, r.dir)["C-001"]
	if c.State != card.Waiting || c.Wait.Kind != card.WaitPermission || c.Turn(card.Roles{}) != card.TurnHuman || !strings.Contains(c.Wait.Question, "権限の確認") {
		t.Fatalf("権限の確認で止まった PG が人の番になっていない: %v %+v turn=%v", c.State, c.Wait, c.Turn(card.Roles{}))
	}
	if n := countHold(events, "入力待ち (権限の確認)"); n != 1 {
		t.Fatalf("入力待ちの出来事が %d 回 (1 回のはず): %v", n, events)
	}
	if len(rec.notified) != 1 {
		t.Fatalf("人の番の通知が %d 回 (1 回のはず): %v", len(rec.notified), rec.notified)
	}
	if len(r.l.stops) != 0 || len(r.l.resumes) != 0 {
		t.Fatalf("入力待ちの PG を止めた / 再開した (問いが消える): stops=%v resumes=%v", r.l.stops, r.l.resumes)
	}
	if err := card.Check([]card.Card{c}); len(err) != 0 {
		t.Fatalf("不変条件が破れた: %v", err)
	}
	r.be(t, "答えた直後")
	r.tick(t)
	c = states(t, r.dir)["C-001"]
	if c.State != card.Running || c.Wait.Kind != card.WaitNone || c.Turn(card.Roles{}) != card.TurnPG {
		t.Fatalf("答えられて動き出した PG を作業中へ戻していない: %v %+v", c.State, c.Wait)
	}
	r.be(t, "権限の確認")
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.State != card.Waiting || len(rec.notified) != 2 {
		t.Fatalf("また止まった PG を人の番として知らせ直していない: %v notified=%v", c.State, rec.notified)
	}
}

// AskUserQuestion で止まった PG も attach しないと答えられない (SendMessage も回答の再開も届かない = 425 の結果 4) ので、同じく人の番。
// 答えた後に turn を終えた (idle) なら作業中へ戻す (作業中の PG が idle になった後の扱いは今までどおり)。
func TestAskUserQuestionPromptIsHumanTurn(t *testing.T) {
	r := newCrashRig(t)
	r.be(t, "AskUserQuestion")
	r.tick(t)
	c := states(t, r.dir)["C-001"]
	if !c.WaitsOnPrompt() || c.Turn(card.Roles{}) != card.TurnHuman || !strings.Contains(c.Wait.Question, "AskUserQuestion") {
		t.Fatalf("AskUserQuestion で止まった PG が人の番になっていない: %v %+v", c.State, c.Wait)
	}
	r.be(t, "turn を終えた")
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.State != card.Running || c.Wait.Kind != card.WaitNone {
		t.Fatalf("turn を終えた PG を作業中へ戻していない: %v %+v", c.State, c.Wait)
	}
}

// 作業中 (busy) と turn を終えた (idle) PG は人の番にしない。
func TestBusyAndIdlePGStayPGTurn(t *testing.T) {
	for _, name := range []string{"作業中", "turn を終えた"} {
		r := newCrashRig(t)
		r.be(t, name)
		r.tick(t)
		c := states(t, r.dir)["C-001"]
		if c.State != card.Running || c.Turn(card.Roles{}) != card.TurnPG {
			t.Fatalf("%s の PG の列を変えた: %v %+v", name, c.State, c.Wait)
		}
		// 最後の列だけでは足りない: 一度入って戻った形 (出来事と通知が出る) も弾く
		for _, e := range c.History {
			if strings.Contains(e.Text, "入力待ち") {
				t.Fatalf("%s の PG を一度入力待ちへ移した: %q", name, e.Text)
			}
		}
	}
}

// 入力待ちの間に PG が落ちた (pid 無し) ら作業中へ戻し、落ちた PG の扱い (自動の再開を待つ・消えたら再開) に任せる。人の番のまま残さない。
func TestPromptWaitLeavesWhenSessionDies(t *testing.T) {
	r := newCrashRig(t)
	r.be(t, "権限の確認")
	r.tick(t)
	r.ss[0].PID = 0
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.State != card.Running || c.Wait.Kind != card.WaitNone || c.DeadSince.IsZero() {
		t.Fatalf("session が生きていない入力待ちを作業中へ戻していない / 落ちた時刻が無い: %v %+v dead=%v", c.State, c.Wait, c.DeadSince)
	}
}

// 入力待ちの間は watchdog の停滞に数えない。答えて戻った直後にも、待っていた時間で停滞にしない。
func TestPromptWaitIsNotStall(t *testing.T) {
	r := newCrashRig(t)
	r.be(t, "権限の確認")
	r.tick(t)
	later := t0.Add(2 * defaultStallAfter)
	r.d.Now = func() time.Time { return later }
	r.tick(t)
	r.be(t, "答えた直後")
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; c.State != card.Running || c.Stalled {
		t.Fatalf("入力待ちの時間を停滞にした: %v stalled=%v", c.State, c.Stalled)
	}
}

// 答えられた PG が、dispatcher が一覧で戻す前に review を出しても受ける (同じ Tick の頭の Apply が先に見る。受け損ねると review が消える)。
func TestPGRequestWhilePromptWaitIsAccepted(t *testing.T) {
	r := newCrashRig(t)
	r.be(t, "権限の確認")
	r.tick(t)
	if _, err := store.Submit(r.dir, store.Request{Kind: "review", CardID: "C-001"}); err != nil {
		t.Fatal(err)
	}
	r.tick(t) // 一覧はまだ waiting のまま (一覧を採った後に答えて review した形)
	if c := states(t, r.dir)["C-001"]; c.State != card.Review || c.Wait.Kind != card.WaitNone {
		t.Fatalf("入力待ちの間に届いた PG の review を受けていない: %v %+v", c.State, c.Wait)
	}
}

// 入力待ちの PG には回答を受けない (答えは attach して返す。回答で再開すると問いを殺す)。
func TestAnswerRefusedWhilePromptWait(t *testing.T) {
	r := newCrashRig(t)
	r.be(t, "権限の確認")
	r.tick(t)
	if _, err := store.Submit(r.dir, store.Request{Kind: "answer", CardID: "C-001", Answer: "はい"}); err != nil {
		t.Fatal(err)
	}
	r.tick(t)
	if c := states(t, r.dir)["C-001"]; !c.WaitsOnPrompt() || c.Answerable() || c.Resume != "" || len(r.l.resumes) != 0 {
		t.Fatalf("入力待ちの PG への回答を受けた: %v Resume=%q resumes=%v", c.State, c.Resume, r.l.resumes)
	}
}

// 入力待ちで止まった PG は枠を使っている扱い (答えられると再開の列を通らずに作業中へ戻る)。待つ間に別の PG を起こして上限を超えない。
func TestPromptWaitHoldsPGSlot(t *testing.T) {
	r := newCrashRig(t)
	r.d.Limit = 1
	planned(t, r.dir, 1) // C-002
	r.be(t, "権限の確認")
	r.tick(t)
	r.tick(t)
	if c := states(t, r.dir)["C-002"]; c.State != card.Planned || len(r.l.starts) != 1 {
		t.Fatalf("入力待ちの PG を枠に数えずに別の PG を起こした: %v starts=%v", c.State, r.l.starts)
	}
}

// 方針変更は入力待ちを待たずに止めて、指示を差し替えて同じ session を再開する (作業中のまま入力待ちだった頃と同じ)。
func TestRedirectStopsPromptWaitingPG(t *testing.T) {
	r := newCrashRig(t)
	r.be(t, "権限の確認")
	r.tick(t)
	order(t, r.dir, "C-001", card.OrderRedirect, "やっぱり青")
	r.tick(t)
	c := states(t, r.dir)["C-001"]
	if len(r.l.resumes) != 1 || !strings.Contains(r.l.resumes[0], "やっぱり青") || c.State != card.Running || c.Wait.Kind != card.WaitNone {
		t.Fatalf("入力待ちの PG へ方針変更を届けていない: resumes=%v %v %+v", r.l.resumes, c.State, c.Wait)
	}
}

// 終了は入力待ちの PG も turn の途中として止め、次の起動で続きから再開する形 (着手待ち・再開の文) にする。
func TestShutdownRequeuesPromptWaitingPG(t *testing.T) {
	r := newCrashRig(t)
	r.be(t, "権限の確認")
	r.tick(t)
	if _, err := r.d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := states(t, r.dir)["C-001"]
	if c.State != card.Planned || c.Resume != resumeAfterStop || c.Wait.Kind != card.WaitNone || !c.Stopped {
		t.Fatalf("入力待ちの PG を続きから再開できる形にしていない: %v %+v Resume=%q Stopped=%v", c.State, c.Wait, c.Resume, c.Stopped)
	}
}

// テストの係の結果を待つカードは入力待ちへ移さない (待ち = WaitResource を上書きすると頼みを見失う)。
func TestPromptWaitSkipsCardAwaitingRun(t *testing.T) {
	r := newCrashRig(t)
	if err := r.d.update("C-001", func(c *card.Card) {
		c.Run, c.RunAt, c.Wait = "make test", t0, card.Wait{Kind: card.WaitResource, Resource: store.RunResource}
	}); err != nil {
		t.Fatal(err)
	}
	r.be(t, "権限の確認")
	if _, err := r.d.trackPrompts(t0, r.ss); err != nil {
		t.Fatal(err)
	}
	if c := states(t, r.dir)["C-001"]; c.State != card.Running || c.Wait.Kind != card.WaitResource || c.Run == "" {
		t.Fatalf("テストの係の結果を待つカードの待ちを書き換えた: %v %+v run=%q", c.State, c.Wait, c.Run)
	}
}

// 実測の表は agents.Session の判定と食い違わない (表の値で Waiting が立つのは入力待ちの 2 形だけ)。
func TestMeasuredTableMatchesWaiting(t *testing.T) {
	for name, m := range measured {
		want := name == "権限の確認" || name == "AskUserQuestion"
		if got := (agents.Session{Status: m.status, State: m.state, WaitingFor: m.waitingFor}).Waiting(); got != want {
			t.Errorf("%s: Waiting()=%v (want %v)", name, got, want)
		}
	}
}

// 入力待ちの PG が落ちて、自動の再開でまた入力待ちへ戻るのを繰り返しても、落ちた回数の上限で止めて人の番 (WaitCrashed) にする
// (作業中の列だけを見ると、入力待ちの Tick で数えても止め損ねる。敵対レビュー C-054 の 1)。
func TestCrashingWhilePromptWaitStops(t *testing.T) {
	r := newCrashRig(t)
	r.be(t, "権限の確認")
	r.tick(t)
	r.crash(t0.Add(time.Minute), 43) // 自動の再開は同じ入力待ちへ戻る
	r.tick(t)
	r.crash(t0.Add(2*time.Minute), 44)
	r.d.Now = func() time.Time { return t0.Add(3 * time.Minute) }
	r.tick(t)
	c := states(t, r.dir)["C-001"]
	if len(r.l.stops) != 1 || c.State != card.Waiting || c.Wait.Kind != card.WaitCrashed {
		t.Fatalf("入力待ちのまま落ち続ける PG を止めていない: stops=%v %v %+v", r.l.stops, c.State, c.Wait)
	}
}

// 入力待ちのまま中身が変わったら文面だけ直す (列と知らせはそのまま)。
func TestPromptLabelFollowsWaitingFor(t *testing.T) {
	r := newCrashRig(t)
	var rec recorder
	rec.rig(r.d)
	r.be(t, "権限の確認")
	r.tick(t)
	r.be(t, "AskUserQuestion")
	r.tick(t)
	c := states(t, r.dir)["C-001"]
	if !c.WaitsOnPrompt() || !strings.Contains(c.Wait.Question, "AskUserQuestion") || len(rec.notified) != 1 {
		t.Fatalf("入力待ちの中身の変化に文面が追いつかない / 知らせ直した: %+v notified=%v", c.Wait, rec.notified)
	}
}
