package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"pro-con/backend"
	"pro-con/card"
	"pro-con/diskuse"
)

// inspSpy は見る所を読む backend (backend.Inspector)。読んだ回数を数える。
type inspSpy struct {
	*spy
	procs     []backend.Proc
	disk      diskuse.Usage
	procCalls int
	diskCalls int
}

func (s *inspSpy) Procs() ([]backend.Proc, error)    { s.procCalls++; return s.procs, nil }
func (s *inspSpy) DiskUsage() (diskuse.Usage, error) { s.diskCalls++; return s.disk, nil }

// viewInspSpy は見ているだけの画面 (--view) の backend。
type viewInspSpy struct{ *inspSpy }

func (viewInspSpy) Accepts(backend.Op) bool { return false }
func (viewInspSpy) ReadOnly()               {}

func newInspSpy() *inspSpy {
	s := &inspSpy{spy: newSpy()}
	s.snap.LimitMax = 2
	s.procs = []backend.Proc{
		{Role: "dispatcher", PID: 100, State: "動いている"},
		{Role: "PM", PID: 101, State: "動いている", Session: "pm1"},
		{Role: "PG", PID: 102, State: "作業中", Card: "R1", Session: "s-r1"},
	}
	for i := 2; i <= 9; i++ {
		s.procs = append(s.procs, backend.Proc{Role: "PG", PID: 200 + i, State: backend.ProcStopped, Card: fmt.Sprintf("C-%03d", i)})
	}
	var items []diskuse.Item
	for i := 1; i <= 5; i++ {
		items = append(items, diskuse.Item{Name: fmt.Sprintf("pc-c-%03d", i), Bytes: int64(40-i) << 20, Card: fmt.Sprintf("C-%03d", i), Done: i != 5})
	}
	s.disk = diskuse.Usage{MeasuredAt: s.snap.Now, Took: 1900 * time.Millisecond, Total: 210 << 20, Groups: []diskuse.Group{
		{Name: diskuse.GroupWorktrees, Bytes: 180 << 20, Count: 5, Items: items},
		{Name: diskuse.GroupState, Bytes: 30 << 20, Count: 1, Items: []diskuse.Item{{Name: "live/runs/", Bytes: 30 << 20}}},
	}}
	return s
}

// run は cmd を走らせ、返ってきた見る所の知らせを画面へ戻す (frame / tick などほかの知らせは捨てる)。
func run(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			run(m, c)
		}
	case procsMsg, diskMsg, eventsMsg, scheduleMsg:
		m.Update(msg)
	}
}

func openSettingsFor(t *testing.T, be backend.Backend) *Model {
	t.Helper()
	m := New(be, nil)
	m.width, m.height = 140, 50
	run(m, press(m, "s"))
	m.set.anim.Finish()
	return m
}

func setScreen(m *Model) string { return ansi.Strip(m.render()) }

// toTab は t のタブに着くまで tab を押す (並びを変えてもテストが押す回数を数え直さない)。
func toTab(tb testing.TB, m *Model, t settingsTab) {
	tb.Helper()
	for range len(m.settingsTabs()) {
		if m.set.tab == t {
			return
		}
		tabKey(m)
	}
	if m.set.tab != t {
		tb.Fatalf("タブ %v に着かない (今 %v)", t, m.set.tab)
	}
}

func tabKey(m *Model) {
	run(m, func() tea.Cmd { _, c := m.Update(tea.KeyPressMsg{Code: tea.KeyTab}); return c }())
}

// s で開いたときに見る所を裏で 1 回ずつ読み、描き直しでは読まない (ディスクは数秒かかる)。r で測り直す。s で閉じる。
func TestSettingsReadsOnOpenNotOnRender(t *testing.T) {
	be := newInspSpy()
	m := openSettingsFor(t, be)
	if !m.set.open || be.procCalls != 1 || be.diskCalls != 1 {
		t.Fatalf("開いたときに 1 回ずつ読むはず: open=%v procs=%d disk=%d", m.set.open, be.procCalls, be.diskCalls)
	}
	for range 5 {
		_ = m.render()
		m.Update(tickMsg{}) // 毎秒の読み直し・dispatcher の知らせでも読み始めない (読み始めると loading が立つ)
		m.Update(changedMsg{})
	}
	if be.procCalls != 1 || be.diskCalls != 1 || m.set.procsLoading || m.set.diskLoading {
		t.Fatalf("描き直し・tick で読んだ: procs=%d disk=%d loading=%v/%v", be.procCalls, be.diskCalls, m.set.procsLoading, m.set.diskLoading)
	}
	toTab(t, m, tabDisk)
	if m.set.tab != tabDisk {
		t.Fatalf("tab でディスクのタブへ移らない: %v", m.set.tab)
	}
	run(m, press(m, "r"))
	if be.diskCalls != 2 {
		t.Fatalf("r で測り直さない: %d", be.diskCalls)
	}
	press(m, "s")
	if m.set.open {
		t.Fatal("s で閉じない")
	}
}

// ← → は受付の箱に置き (SetConfig)、続けて押すと置いた値から数える (dispatcher の適用を待たない)。1 より下げない。
func TestSettingsStepsLimit(t *testing.T) {
	be := newInspSpy()
	m := openSettingsFor(t, be)
	press(m, "l", "l")
	var got []string
	for _, c := range be.applied {
		if sc, ok := c.(backend.SetConfig); ok {
			got = append(got, sc.Key+"="+sc.Value)
		}
	}
	if strings.Join(got, ",") != "limit=3,limit=4" {
		t.Fatalf("置いた設定 = %v", got)
	}
	if !strings.Contains(setScreen(m), "‹ 4 ›") || !strings.Contains(setScreen(m), "適用待ち") {
		t.Fatalf("置いた値を適用待ちとして出さない:\n%s", setScreen(m))
	}
	// dispatcher が適用した (Snapshot の設定が置いた値になった) ら、適用待ちを外す
	// 値が一致しても、依頼が残っている間は外さない (→ ← と続けて押すと、途中の値が出てから戻る)
	be.snap.Config.Limit, be.snap.Config.Pending = 4, 1
	m.setSnap(be.Snapshot())
	if !strings.Contains(setScreen(m), "適用待ち") {
		t.Fatal("依頼が残っているのに適用待ちを外した")
	}
	be.snap.Config.Pending = 0
	m.setSnap(be.Snapshot())
	if strings.Contains(setScreen(m), "適用待ち") {
		t.Fatal("適用されたのに適用待ちのまま")
	}
	be.snap.Config.Limit = 1
	m.setSnap(be.Snapshot())
	n := len(be.applied)
	press(m, "h")
	if len(be.applied) != n || !strings.Contains(m.toasts.Text(), "1 以上") {
		t.Fatalf("1 より下げようとして置いた / 断らない: %d → %d %q", n, len(be.applied), m.toasts.Text())
	}
}

// 見ているだけの画面 (--view) は変える所 (設定のタブ) を出さず、← → でも何も置かない。見る所は出す。
func TestSettingsViewOnlyShowsInspectOnly(t *testing.T) {
	be := viewInspSpy{newInspSpy()}
	m := openSettingsFor(t, be)
	out := setScreen(m)
	if m.set.tab != tabSchedule || strings.Contains(out, "変える所") || strings.Contains(out, " 設定 ") {
		t.Fatalf("--view で変える所を出した (tab=%v):\n%s", m.set.tab, out)
	}
	press(m, "l", "h")
	if len(be.applied) != 0 {
		t.Fatalf("--view で設定を置いた: %v", be.applied)
	}
}

// 終わったカードの止まった PG は出さず数えもしない。カードと食い違う PG は出し、止まっている役は 1 行に畳んで Enter で開く (issue 497)。
func TestSettingsProcsShowOnlyLiveAndMismatch(t *testing.T) {
	be := newInspSpy()
	be.snap.Cards[0].Title, be.snap.Cards[0].Issues = "491: 番号が二重に出る", []card.IssueRef{{Number: 491}} // R1
	be.procs = append(be.procs,
		backend.Proc{Role: "PG", PID: 301, State: backend.ProcStopped, Card: "C-021", Mismatch: "止まっている (カードは作業中)"},
		backend.Proc{Role: "PG", PID: 302, State: "動いている", Card: "C-022", Mismatch: "動いている (カードは完了)"},
		backend.Proc{Role: "取り込み", PID: 303, State: backend.ProcStopped})
	m := openSettingsFor(t, be)
	toTab(t, m, tabProcs)
	out := setScreen(m)
	// カードの列は前の PG の一覧と同じ cardHeading (タイトルの頭の issue 番号を落とす。issue 491)
	if !strings.Contains(out, "R1 #491 番号が二重に出る") || strings.Contains(out, "491: ") {
		t.Fatalf("カードの列がタイトルを出さない / issue 番号を二重に出す:\n%s", out)
	}
	for _, want := range []string{"! 止まっている (カードは作業中)", "C-021", "! 動いている (カードは完了)", "C-022", "止まっている役 1 本"} {
		if !strings.Contains(out, want) {
			t.Fatalf("%q が無い:\n%s", want, out)
		}
	}
	if strings.Contains(out, "C-005") || strings.Contains(out, "8 本") {
		t.Fatalf("終わったカードの止まった PG を出した / 数えた:\n%s", out)
	}
	press(m, "enter")
	if out := setScreen(m); !strings.Contains(out, "取り込み") || strings.Contains(out, "C-005") {
		t.Fatalf("enter で止まっている役を開かない / 止まった PG まで開いた:\n%s", out)
	}
	if got := m.procsSummary(); got != "PG 作業中 1 / 枠 2 · 待機 0 · 動いている 3・食い違い 2" {
		t.Errorf("要約 = %q", got)
	}
}

// PG は枠を使うもの (dispatcher が枠に数えたカード) と待機中のものを別の区分に出し、要約は「作業中 / 枠 · 待機」(issue 557)。
// dispatcher が数え直していなければ分けずに「判定できない」と出す (読めないのを 0 に見せない)。
func TestSettingsProcsSplitsPGsBySlot(t *testing.T) {
	be := newInspSpy()
	be.snap.Slots = []string{"R1", "C-030"}
	be.procs = []backend.Proc{
		{Role: "dispatcher", PID: 100, State: "動いている"},
		{Role: "PG", PID: 102, State: "作業中", Card: "R1", Session: "s-r1", Slot: backend.SlotHeld},
		{Role: "PG", PID: 103, State: "質問待ち", Card: "W1", Session: "s-w1", Slot: backend.SlotIdle},
		{Role: "PG", State: backend.ProcStopped, Card: "C-030", Session: "s-c30", Slot: backend.SlotHeld}, // 枠に数えたが止まっている: 出す
		{Role: "テストの係", PID: 104, State: "実行中", Card: "R1", Command: "make test"},
	}
	m := openSettingsFor(t, be)
	toTab(t, m, tabProcs)
	out := setScreen(m)
	order := []string{"dispatcher", "PG 作業中 (枠を使う) 2 / 枠 2", "s-r1", "s-c30", "PG 待機中 (枠を使わない) 1", "s-w1", "make test"}
	at := -1
	for _, want := range order {
		i := strings.Index(out, want)
		if i <= at {
			t.Fatalf("%q が無い / 並びが違う (%v の順のはず):\n%s", want, order, out)
		}
		at = i
	}
	if got := m.procsSummary(); !strings.HasPrefix(got, "PG 作業中 2 / 枠 2 · 待機 1 · ") {
		t.Errorf("要約 = %q", got)
	}

	be.snap.SlotsAt = be.snap.Now.Add(-dispatcherStale - time.Second)
	m = openSettingsFor(t, be)
	toTab(t, m, tabProcs)
	out = setScreen(m)
	if !strings.Contains(out, "PG 3 (枠を使っているか判定できない") || strings.Contains(out, "作業中 (枠を使う)") || strings.Contains(out, "待機中 (枠を使わない)") {
		t.Fatalf("dispatcher が古いのに枠で分けた:\n%s", out)
	}
	if got := ansi.Strip(m.procsSummary()); !strings.HasPrefix(got, "PG の枠は判定できない") {
		t.Errorf("要約 = %q", got)
	}
}

// ディスクのタブは置き場ごとに大きい順の 3 つまで内訳を出し、Enter で全部 (完了したカードの印つき)。
func TestSettingsDiskBreakdown(t *testing.T) {
	be := newInspSpy()
	m := openSettingsFor(t, be)
	toTab(t, m, tabDisk)
	out := setScreen(m)
	for _, want := range []string{"合計 210M", "PG・役の worktree", "うち完了したカード 4 個", "pc-c-001", "C-001 完了", "ほか 2 個", "状態の置き場"} {
		if !strings.Contains(out, want) {
			t.Fatalf("ディスクのタブに %q が無い:\n%s", want, out)
		}
	}
	if strings.Contains(out, "pc-c-005") {
		t.Fatalf("閉じた内訳に 4 つ目以降を出した:\n%s", out)
	}
	press(m, "enter")
	if out := setScreen(m); !strings.Contains(out, "pc-c-005") || strings.Contains(out, "ほか 2 個") {
		t.Fatalf("enter で内訳を全部出さない:\n%s", out)
	}
}

// 設定画面から Q で終了の入力欄を開いて取り消しても、演出の tick が止まったままにならない (閉じる演出の tick を捨てると framing が
// 立ったまま残り、以後の演出がすべて止まっていた。敵対的レビュー 2026-09-26)。
func TestQuitFromSettingsKeepsFrames(t *testing.T) {
	m := openSettingsFor(t, newInspSpy())
	m.framing = false // 開く演出の tick は終わった
	cmd := press(m, "Q")
	if m.set.open {
		t.Fatal("終了の入力欄を開いても設定画面が開いたまま")
	}
	if m.framing && cmd == nil { // framing は「tick が 1 本飛んでいる」印。立てたのに tick を返さないと、以後 startFrames が何も返さない
		t.Fatal("演出の tick を捨てた (framing が立ったまま残る)")
	}
}

// 揺れの途中で設定画面を開いても、揺れたレーンが全幅の板の上 (ヘッダの行) にはみ出さない (揺れの帯はヘッダの上にも乗る。issue 436)。
func TestSettingsCoversBump(t *testing.T) {
	m, clk := cursorModel(t)
	start := clk.t
	lines := strings.Split(ansi.Strip(m.render()), "\n")
	header := strings.Join(lines[:m.headerRows()], "\n")
	press(m, "k") // 端でぶつかって上へ揺れる
	press(m, "s")
	m.set.anim.Finish()
	clk.t = start.Add(peak)
	if got := strings.Join(strings.Split(ansi.Strip(m.render()), "\n")[:m.headerRows()], "\n"); got != header {
		t.Fatalf("設定画面を開いているのに、揺れたレーンがヘッダに乗った:\n%s\n---\n%s", got, header)
	}
}

// 利用枠で絞るかはチェックボックス: Enter でも ← → でも入れ替え、on / off を受付の箱に置く (issue 536)。
func TestSettingsTogglesUsageCheckbox(t *testing.T) {
	be := newInspSpy()
	m := openSettingsFor(t, be)
	press(m, "j") // PG の枠の次の行
	if !strings.Contains(setScreen(m), "[x]") {
		t.Fatalf("既定 (絞る) でチェックが入っていない:\n%s", setScreen(m))
	}
	press(m, "enter")
	be.snap.Config.UsageOff = true
	m.setSnap(be.Snapshot())
	press(m, "l")
	var got []string
	for _, c := range be.applied {
		if sc, ok := c.(backend.SetConfig); ok {
			got = append(got, sc.Key+"="+sc.Value)
		}
	}
	if strings.Join(got, ",") != "usage=off,usage=on" {
		t.Fatalf("置いた設定 = %v", got)
	}
}

// 敵対的レビューの担い手 (514) は ← → で claude / codex を巡り、受付の箱に置く。設定が無ければ dispatcher が使っている値 (config.toml) を出す。
func TestSettingsStepsReview(t *testing.T) {
	be := newInspSpy()
	m := openSettingsFor(t, be) // dispatcher がまだ回っていない: config.toml の値は分からないので claude と出さない
	if !strings.Contains(setScreen(m), "‹ ? ›") || !strings.Contains(setScreen(m), "dispatcher がまだ回っていない") {
		t.Fatalf("分からない担い手を claude と出した:\n%s", setScreen(m))
	}
	be.snap.Config.ReviewNow, be.snap.Config.ReviewFrom = "codex", "config.toml"
	m.setSnap(be.Snapshot())
	if !strings.Contains(setScreen(m), "‹ codex ›") || !strings.Contains(setScreen(m), "設定なし (config.toml)") {
		t.Fatalf("config.toml の担い手を出さない:\n%s", setScreen(m))
	}
	press(m, "j", "j", "j", "l", "l") // 枠 → 利用枠 → PM → 担い手
	var got []string
	for _, c := range be.applied {
		if sc, ok := c.(backend.SetConfig); ok {
			got = append(got, sc.Key+"="+sc.Value)
		}
	}
	if strings.Join(got, ",") != "review=claude,review=codex" {
		t.Fatalf("置いた設定 = %v", got)
	}
	press(m, "h")
	if last := be.applied[len(be.applied)-1].(backend.SetConfig); last.Value != "claude" {
		t.Fatalf("← で逆に巡らない: %+v", last)
	}
}

// schedSpy は予定を読む backend (backend.ScheduleReader)。読んだ回数を数える。
type schedSpy struct {
	*inspSpy
	rows  []backend.ScheduleRow
	calls int
}

func (s *schedSpy) Schedule() ([]backend.ScheduleRow, error) { s.calls++; return s.rows, nil }

func newSchedSpy() *schedSpy {
	i := newInspSpy()
	return &schedSpy{inspSpy: i, rows: []backend.ScheduleRow{{When: "毎日 04:00", Command: "pro-con worktree clean --yes",
		Last: "09-27 04:00:03〜04:01:15 rc=0 結果: worktree 消した 66・残した 26・失敗 0", Next: "09-28 04:00 (dispatcher が止まっていれば、次に起きた最初の Tick で 1 回だけ回す)",
		Out: "/state/schedule/worktree-clean.out"}}}
}

// 予定のタブ (issue 550): dispatcher が起こすコマンドの字面 (--yes まで) と、いつ・前回・次回を出す。開いたときと r で読み、描き直しでは読まない。
func TestSettingsScheduleTab(t *testing.T) {
	be := newSchedSpy()
	m := openSettingsFor(t, be)
	if be.calls != 1 {
		t.Fatalf("開いたときに 1 回読むはず: %d", be.calls)
	}
	tabKey(m)
	if m.set.tab != tabSchedule { // 並びも見る (設定の次 = 変える所のすぐ隣に置く)
		t.Fatalf("設定の次が予定のタブでない: %v", m.set.tab)
	}
	calls := be.calls
	out := setScreen(m)
	for _, want := range []string{" スケジューラージョブ ", "毎日 04:00  pro-con worktree clean --yes", "前回  09-27 04:00:03〜04:01:15 rc=0 結果: worktree 消した 66", "次回  09-28 04:00", "出力  /state/schedule/worktree-clean.out / .err"} {
		if !strings.Contains(out, want) {
			t.Errorf("予定のタブに %q が無い:\n%s", want, out)
		}
	}
	_ = m.render()
	if be.calls != calls {
		t.Errorf("描き直しで読んだ: %d → %d", calls, be.calls)
	}
	run(m, press(m, "r"))
	if be.calls != calls+1 {
		t.Errorf("r で読み直さない: %d → %d", calls, be.calls)
	}
	be.snap.Config.ScheduleOff = true
	m.setSnap(be.Snapshot())
	if !strings.Contains(setScreen(m), "止めている") {
		t.Errorf("schedule off を予定のタブに出さない:\n%s", setScreen(m))
	}
	be.snap.Config.Err = "壊れている"
	m.setSnap(be.Snapshot())
	if !strings.Contains(setScreen(m), "設定を読めないので回さない") {
		t.Errorf("設定を読めないことを予定のタブに出さない:\n%s", setScreen(m))
	}
}

// 予定を回すかは設定のタブのチェックボックス: Enter でも ← → でも入れ替え、schedule=on / off を受付の箱に置く (issue 550)。
func TestSettingsTogglesScheduleCheckbox(t *testing.T) {
	be := newSchedSpy()
	m := openSettingsFor(t, be)
	for m.set.cursor < len(configKeys)-1 && configKeys[m.set.cursor] != backend.ConfigSchedule {
		press(m, "j")
	}
	if configKeys[m.set.cursor] != backend.ConfigSchedule || !strings.Contains(setScreen(m), "スケジューラージョブを回す") {
		t.Fatalf("スケジューラージョブを回す の行が無い:\n%s", setScreen(m))
	}
	press(m, "enter")
	be.snap.Config.ScheduleOff = true
	m.setSnap(be.Snapshot())
	delete(m.set.want, backend.ConfigSchedule) // 適用を待つ値ではなく Snapshot の値を描かせる
	for _, l := range strings.Split(setScreen(m), "\n") {
		if strings.Contains(l, "スケジューラージョブを回す") && !strings.Contains(l, "[ ]") {
			t.Errorf("off なのにチェックが入っている: %q", l)
		}
	}
	press(m, "l")
	var got []string
	for _, c := range be.applied {
		if sc, ok := c.(backend.SetConfig); ok {
			got = append(got, sc.Key+"="+sc.Value)
		}
	}
	if strings.Join(got, ",") != "schedule=off,schedule=on" {
		t.Fatalf("置いた設定 = %v", got)
	}
}
