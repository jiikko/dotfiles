package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/store"
)

// 2.1.281 の `claude -p /usage` の stdout (2026-09-25 に実測した形。使用率は差し替えた)
const usageOut = `You are currently using your subscription to power your Claude Code usage

Current session: 12% used · resets Sep 25 at 4:40pm (Asia/Tokyo)
Current week (all models): 8% used · resets Oct 2 at 8am (Asia/Tokyo)
Current week (Fable): 0% used · resets Oct 2 at 8am (Asia/Tokyo)
`

func TestParseUsage(t *testing.T) {
	u, err := ParseUsage(usageOut)
	if err != nil || u.Session != 12 || u.Week != 8 {
		t.Fatalf("session 12 / week 8 のはず: %+v %v", u, err)
	}
	// 片方の行だけ無ければ、読める側で絞る (無い側は 0)
	if u, err := ParseUsage(strings.Replace(usageOut, "Current session", "Session", 1)); err != nil || u.Session != 0 || u.Week != 8 || u.Missing != "5 時間の枠" {
		t.Fatalf("5 時間の枠の行が無いと週の枠も捨てた: %+v %v", u, err)
	}
	if u, err := ParseUsage(strings.Replace(usageOut, "Current week (all models)", "This week", 1)); err != nil || u.Session != 12 || u.Week != 0 || u.Missing != "週の枠" {
		t.Fatalf("週の行が無いと 5 時間の枠も捨てた: %+v %v", u, err)
	}
	// 両方無ければ誤り (0% と区別する)
	both := strings.NewReplacer("Current session", "Session", "Current week (all models)", "This week").Replace(usageOut)
	if _, err := ParseUsage(both); err == nil {
		t.Fatal("使用率の行が無いのに読めたことにした (0% と区別できない)")
	}
	// 小数・「<1%」・行頭の空白 (表示の揺れ)。モデル別の週の枠は見ない
	odd := "  Current session: 81.5% used · resets x\nCurrent week (all models): <1% used · resets y\nCurrent week (Fable): 99% used\n"
	if u, err := ParseUsage(odd); err != nil || u.Session != 81 || u.Week != 1 {
		t.Fatalf("表示の揺れを読めない / モデル別の枠を読んだ: %+v %v", u, err)
	}
	odd = "Current session: <1% used · resets x\n  Current week (all models): 12.5% used · resets y\n"
	if u, err := ParseUsage(odd); err != nil || u.Session != 1 || u.Week != 12 {
		t.Fatalf("表示の揺れを読めない: %+v %v", u, err)
	}
}

// usageRig は使用率を差し替えられる dispatcher (上限 3、分解済みのカード 3 枚)。
type usageRig struct {
	d     *Dispatcher
	l     *fakeLauncher
	now   time.Time
	pct   int // 週の枠
	sess  int // 5 時間の枠
	err   error
	calls int
}

func newUsageRig(t *testing.T) *usageRig {
	t.Helper()
	dir := t.TempDir()
	planned(t, dir, 3)
	r := &usageRig{l: &fakeLauncher{}, now: t0}
	r.d = newDispatcher(t, dir, r.l, nil)
	r.d.Limit = 3
	r.d.Now = func() time.Time { return r.now }
	r.d.Usage = func(context.Context) (Usage, error) {
		r.calls++
		if r.err != nil {
			return Usage{}, r.err
		}
		return Usage{Session: r.sess, Week: r.pct}, nil
	}
	return r
}

func (r *usageRig) tick(t *testing.T) []eventlog.Event {
	t.Helper()
	notes, err := r.d.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return notes
}

// 枠の残量で同時に起動する数を絞る: 5 時間と週の大きい方が 80% 以上は 1 本、95% 以上は 0 本、それ未満は上限まで (境界は含む)。
func TestUsageCapsLaunches(t *testing.T) {
	for _, tc := range []struct {
		sess, pct, starts int
	}{{10, 79, 3}, {10, 80, 1}, {10, 94, 1}, {10, 95, 0}, {80, 10, 1}, {95, 10, 0}} {
		r := newUsageRig(t)
		r.sess, r.pct = tc.sess, tc.pct
		r.tick(t)
		if len(r.l.starts) != tc.starts {
			t.Fatalf("5 時間 %d%% / 週 %d%% で %d 本起動した (%d 本のはず)", tc.sess, tc.pct, len(r.l.starts), tc.starts)
		}
		st, ok, err := store.LoadDispatcherState(r.d.Dir)
		if err != nil || !ok || !st.Tick.Equal(t0) || st.Limit != 3 || st.Cap != tc.starts || st.UsageWeek != tc.pct || st.UsageSession != tc.sess {
			t.Fatalf("dispatcher の様子が違う (週 %d%%): %+v ok=%v %v", tc.pct, st, ok, err)
		}
		if (tc.starts < 3) != (st.Why != "") {
			t.Fatalf("絞った理由の有無が違う (週 %d%%): %q", tc.pct, st.Why)
		}
	}
}

// 枠を読めないときは上限のまま動かし、読めないことを様子に出す (文面が変わっただけで全部の作業を止めない)。
func TestUsageUnreadableKeepsLimit(t *testing.T) {
	r := newUsageRig(t)
	r.err = errors.New("claude が無い")
	r.tick(t)
	st, _, _ := store.LoadDispatcherState(r.d.Dir)
	if len(r.l.starts) != 3 || st.Cap != 3 || !strings.Contains(st.Why, "claude が無い") {
		t.Fatalf("読めないのに絞った / 理由を出さない: starts=%d %+v", len(r.l.starts), st)
	}
}

// 読み直すのは usageEvery ごと。読めない間は最後に読めた値を usageStale まで使い、それを過ぎたら上限に戻す。
func TestUsageRefreshAndStale(t *testing.T) {
	r := newUsageRig(t)
	r.pct = 96
	r.tick(t)
	r.now = t0.Add(time.Minute)
	r.tick(t)
	if r.calls != 1 {
		t.Fatalf("usageEvery の前に読み直した: %d 回", r.calls)
	}
	r.err = errors.New("読めない")
	r.now = t0.Add(usageEvery)
	r.tick(t)
	if r.calls != 2 || len(r.l.starts) != 0 {
		t.Fatalf("読み直さない / 読めないと前の値 (96%%) を捨てた: calls=%d starts=%d", r.calls, len(r.l.starts))
	}
	r.now = t0.Add(usageStale + time.Minute)
	r.tick(t)
	if len(r.l.starts) != 3 {
		t.Fatalf("読めないまま usageStale を過ぎても古い値で止めている: starts=%d", len(r.l.starts))
	}
}

// 枠で待たせていることは、変わったときだけ書く (Tick ごとにログを埋めない)。
func TestUsageHoldNotedOnce(t *testing.T) {
	r := newUsageRig(t)
	r.pct = 96
	held := func(notes []eventlog.Event) int {
		n := 0
		for _, e := range notes {
			if e.Kind == eventlog.KindHold && strings.Contains(e.Reason, "起動・再開しない") {
				n++
			}
		}
		return n
	}
	if n := held(r.tick(t)); n != 1 {
		t.Fatalf("待たせたことを書かない: %d", n)
	}
	r.now = t0.Add(time.Second)
	if n := held(r.tick(t)); n != 0 {
		t.Fatalf("変わっていないのにまた書いた: %d", n)
	}
}

// session の一覧を取れずに途中で抜けた Tick でも、回ったことは書く (画面が dispatcher を止まっていると誤らない)。
func TestStateWrittenWhenListFails(t *testing.T) {
	r := newUsageRig(t)
	r.d.List = func(context.Context) ([]agents.Session, error) { return nil, errors.New("timeout") }
	r.tick(t)
	if st, ok, _ := store.LoadDispatcherState(r.d.Dir); !ok || !st.Tick.Equal(t0) {
		t.Fatalf("途中で抜けた Tick で様子を書かない: %+v ok=%v", st, ok)
	}
}

// 上限 1 は 80% でも 1 本のまま (絞る先が同じ)。95% で 0 本。
func TestUsageWithLimitOne(t *testing.T) {
	for _, tc := range []struct{ pct, starts int }{{85, 1}, {95, 0}} {
		r := newUsageRig(t)
		r.d.Limit, r.pct = 1, tc.pct
		r.tick(t)
		st, _, _ := store.LoadDispatcherState(r.d.Dir)
		if len(r.l.starts) != tc.starts || st.Cap != tc.starts || (tc.starts == 1) != (st.Why == "") {
			t.Fatalf("上限 1・週 %d%%: starts=%d %+v", tc.pct, len(r.l.starts), st)
		}
	}
}

// まだ枠を読みに行っていない (最初の Tick が一覧の失敗で抜けた) ときは、理由の無い「読めない」を出さない。
func TestNoUsageReasonBeforeFirstRead(t *testing.T) {
	r := newUsageRig(t)
	r.d.List = func(context.Context) ([]agents.Session, error) { return nil, errors.New("timeout") }
	r.tick(t)
	if st, _, _ := store.LoadDispatcherState(r.d.Dir); r.calls != 0 || st.Why != "" || st.Cap != 3 {
		t.Fatalf("読みに行く前に理由を出した: calls=%d %+v", r.calls, st)
	}
}

// 枠を読んでいる間に止められたら (ctx が取り消された)、その Tick では起動しない (取り消された ctx の失敗を履歴に残さない)。
func TestNoDispatchAfterCancelDuringUsage(t *testing.T) {
	r := newUsageRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	r.d.Usage = func(context.Context) (Usage, error) { cancel(); return Usage{}, nil }
	if _, err := r.d.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(r.l.starts) != 0 {
		t.Fatalf("取り消された後に起動した: %v", r.l.starts)
	}
}

// 枠で絞らなくても上限で止まっていたなら、枠のせいにしない。
func TestHoldNotBlamedOnUsageAtFullLimit(t *testing.T) {
	r := newUsageRig(t)
	r.d.Limit = 2
	r.tick(t) // 2 本起動して上限に達する
	r.pct = 85
	r.now = t0.Add(usageEvery)
	for _, s := range r.tick(t) {
		if strings.Contains(s.Reason, "起動・再開しない") {
			t.Fatalf("上限で止まっているのに枠のせいにした: %q", s.Reason)
		}
	}
}

// 回答を受けた再開は、それより古い新しいカードの起動より先 (枠で 1 本に絞ったときに、途中まで進んだ作業を待たせない)。
func TestResumeBeforeOlderStart(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{}
	d := newDispatcher(t, dir, l, []agents.Session{{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t0.Add(time.Second).UnixMilli()}})
	d.Limit = 1
	for range 2 { // C-001 を起動 → 登録
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []store.Request{{Kind: "add", Title: "新", Repo: "dotfiles"}} {
		if _, err := store.Submit(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Apply(dir, t0); err != nil {
		t.Fatal(err)
	}
	for _, r := range []store.Request{
		{Kind: "plan", CardID: "C-002", Issues: []card.IssueRef{{Repo: "dotfiles", Number: 2, Status: "open"}}},
		{Kind: "ask", CardID: "C-001", Question: "赤か青か"}, {Kind: "answer", CardID: "C-001", Answer: "青"},
	} {
		if _, err := store.Submit(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Apply(dir, t0); err != nil {
		t.Fatal(err)
	}
	setCard(t, dir, "C-002", func(c *card.Card) { c.Since = t0.Add(-time.Hour) }) // 再開を待つ C-001 より古い
	if c := states(t, dir); c["C-001"].State != card.Planned || !resumes(c["C-001"]) || c["C-002"].State != card.Planned {
		t.Fatalf("前提: 両方とも分解済みで C-001 は再開待ちのはず: %+v", c)
	}
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(l.resumes) != 1 || len(l.starts) != 1 {
		t.Fatalf("上限 1 で再開を先にするはず: starts=%v resumes=%v", l.starts, l.resumes)
	}
}

// 失敗し続ける再開は、2 回目からは古い順に戻る (1 本の枠を永久に占めて、ほかのカードを起動させなくしない)。
func TestFailingResumeDoesNotStarveOthers(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	l := &fakeLauncher{}
	now := t0
	d := newDispatcher(t, dir, l, []agents.Session{{ID: "id-pc-c-001", SessionID: "S1", PID: 42, Kind: "background", Cwd: "/w/dotfiles/.claude/worktrees/pc-c-001", StartedAt: t0.Add(time.Second).UnixMilli()}})
	d.Limit = 1
	d.Now = func() time.Time { return now }
	tick := func() {
		t.Helper()
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	tick()
	tick() // C-001 を起動 → 登録
	if _, err := store.Submit(dir, store.Request{Kind: "add", Title: "新", Repo: "dotfiles"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(dir, now); err != nil {
		t.Fatal(err)
	}
	for _, r := range []store.Request{
		{Kind: "plan", CardID: "C-002", Issues: []card.IssueRef{{Repo: "dotfiles", Number: 2, Status: "open"}}},
		{Kind: "ask", CardID: "C-001", Question: "赤か青か"}, {Kind: "answer", CardID: "C-001", Answer: "青"},
	} {
		if _, err := store.Submit(dir, r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Apply(dir, now); err != nil {
		t.Fatal(err)
	}
	setCard(t, dir, "C-002", func(c *card.Card) { c.Since = t0.Add(-time.Hour) })
	l.resumeFail = true
	for range 3 {
		now = now.Add(launchGrace + time.Second)
		tick()
	}
	if len(l.starts) != 2 || states(t, dir)["C-002"].State != card.Running {
		t.Fatalf("失敗し続ける再開が枠を占めて、古い C-002 を起動しない: starts=%v resumes=%d", l.starts, len(l.resumes))
	}
}

// 片方の行だけ読めたら、絞らなくても読めない側を理由に出す (表示の変化を黙らせない)。
func TestPartialUsageIsShown(t *testing.T) {
	r := newUsageRig(t)
	r.d.Usage = func(context.Context) (Usage, error) {
		return ParseUsage(strings.Replace(usageOut, "Current session", "Session", 1))
	}
	r.tick(t)
	st, _, _ := store.LoadDispatcherState(r.d.Dir)
	if st.Cap != 3 || !strings.Contains(st.Why, "5 時間の枠の行を読めない") {
		t.Fatalf("読めない側を出さない: %+v", st)
	}
}

// 絞ったときも、読めない側の行を理由に添える。
func TestPartialUsageNotedWhenCapped(t *testing.T) {
	for _, tc := range []struct{ pct, cap int }{{85, 1}, {96, 0}} {
		r := newUsageRig(t)
		r.d.Usage = func(context.Context) (Usage, error) { return Usage{Week: tc.pct, Missing: "5 時間の枠"}, nil }
		r.tick(t)
		st, _, _ := store.LoadDispatcherState(r.d.Dir)
		if st.Cap != tc.cap || !strings.Contains(st.Why, "5 時間の枠の行を読めない") {
			t.Fatalf("週 %d%%: 絞ったときに読めない側を出さない: %+v", tc.pct, st)
		}
	}
}

// 誤りに添える 1 行目は 80 文字で切る。
func TestParseUsageErrorQuoteIsBounded(t *testing.T) {
	_, err := ParseUsage(strings.Repeat("あ", 200))
	if err == nil || strings.Count(err.Error(), "あ") != 80 || !strings.Contains(err.Error(), "…") {
		t.Fatalf("1 行目を切らない: %v", err)
	}
}

// 使用率の行が無い (未認証など) ときの誤りには、実際に返った文の 1 行目を添える (原因を取り違えて見せない)。
func TestParseUsageErrorQuotesOutput(t *testing.T) {
	if _, err := ParseUsage("Total cost: $0.00\nTotal duration: 1s\n"); err == nil || !strings.Contains(err.Error(), "Total cost: $0.00") {
		t.Fatalf("返った文を添えない: %v", err)
	}
}

// 枠を読むコマンド: 読むたびに session を残さない / user の hook を走らせない / 状態の置き場で動かす / 子孫が stdout を握っても戻る。
func TestUsageCmd(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1/default,1,0")
	t.Setenv("TMUX_PANE", "%1")
	t.Setenv("PRO_CON_KEEP", "1")
	cmd := usageCmd(context.Background(), "/bin/claude", "/state")
	if cmd.Env == nil || !slices.Contains(cmd.Env, "PRO_CON_KEEP=1") {
		t.Fatalf("親の環境を引き継いでいない / Env が nil (親の TMUX がそのまま渡る): %d 個", len(cmd.Env))
	}
	want := []string{"/bin/claude", "-p", "--no-session-persistence", "--setting-sources", "", "/usage"}
	if strings.Join(cmd.Args, "\x00") != strings.Join(want, "\x00") || cmd.Dir != "/state" || cmd.WaitDelay != time.Second {
		t.Fatalf("args=%q dir=%q waitDelay=%v", cmd.Args, cmd.Dir, cmd.WaitDelay)
	}
	for _, e := range cmd.Env {
		if strings.HasPrefix(e, "TMUX=") || strings.HasPrefix(e, "TMUX_PANE=") {
			t.Fatalf("TMUX を落としていない: %q", e)
		}
	}
}

// 箱の依頼を適用したら、session の一覧を取る (最大 10 秒) より前に画面へ知らせる。知らせる時点でカードは記録にある。
func TestChangedAfterApplyBeforeList(t *testing.T) {
	dir := t.TempDir()
	var order []string
	d := newDispatcher(t, dir, &fakeLauncher{}, nil)
	d.List = func(context.Context) ([]agents.Session, error) { order = append(order, "list"); return nil, nil }
	d.Changed = func() {
		st, _ := store.Load(dir)
		order = append(order, fmt.Sprintf("changed:%d", len(st.Cards)))
	}
	if _, err := store.Submit(dir, store.Request{Kind: "add", Title: "t", Repo: "dotfiles"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(order) < 2 || order[0] != "changed:1" || order[1] != "list" {
		t.Fatalf("適用の直後 (一覧の前) に知らせない: %v", order)
	}
	order = nil
	if _, err := d.Tick(context.Background()); err != nil { // 何もしない Tick は知らせない (画面を無駄に読み直させない)
		t.Fatal(err)
	}
	if len(order) != 1 {
		t.Fatalf("何もしない Tick で知らせた: %v", order)
	}
}

// 箱の依頼の適用が無くても、何かをした Tick (起動・登録など) の後は画面へ知らせる。
func TestChangedAfterLaunch(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1) // 箱は空 (適用済み)
	d := newDispatcher(t, dir, &fakeLauncher{}, nil)
	var changed int
	d.Changed = func() { changed++ }
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if changed != 1 {
		t.Fatalf("PG を起動した Tick の後に知らせない: %d 回", changed)
	}
}

// 起動して最初の Tick は、何もしなくても画面へ知らせる (先に開いていた画面の「dispatcher 未起動」を 3 秒待たせない)。
func TestChangedAfterFirstTick(t *testing.T) {
	d := newDispatcher(t, t.TempDir(), &fakeLauncher{}, nil)
	changed := 0
	d.Changed = func() { changed++ }
	for range 3 {
		if _, err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if changed != 1 {
		t.Fatalf("何もしない Tick 3 回で %d 回知らせた (最初の 1 回だけのはず)", changed)
	}
}
