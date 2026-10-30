package main

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/jiikko/dotfiles/src/tuikit/tickchain"

	"glogx/internal/app"
	"glogx/internal/gitdata"
	"glogx/internal/term"
)

// fireTick は張ってある tick を届けたことにする (張っていなければ maybeTick で張ってから)。tickMsg{} を直に流すと
// seq 0 の古い tick として onTick に捨てられる。
func fireTick(m *browseModel) (tea.Model, tea.Cmd) {
	if !tickPending(m) {
		m.maybeTick()
	}
	_, seq, _ := m.ticks.Pending()
	return m.Update(tickMsg{seq: seq})
}

// tickPending は tick のチェーンが張ってあるか。
func tickPending(m *browseModel) bool {
	_, _, ok := m.ticks.Pending()
	return ok
}

// holdTick は今すぐ届く tick が張ってある状態にする (maybeTick がどの周期でも新しく張らない)。
func holdTick(m *browseModel) { m.ticks.Want(app.TimeNow(), 0) }

// clearTick は tick のチェーンを空にする (張っていない状態)。
func clearTick(m *browseModel) { m.ticks = tickchain.Chain{} }

// スピナーの遅い tick が張ってある間に動き (一覧の glide) が始まったら、動きの周期の tick を張り直す
// (bool の single-flight だと、動きの次のフレームが張ってある 80ms の tick まで止まる)。
// 追い越された遅い tick が後から届いても、フレームの数で進む動きを余分に進めない。
func TestAnimStartDuringSpinnerTickRearms(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	orig := app.TimeNow
	app.TimeNow = func() time.Time { return now }
	t.Cleanup(func() { app.TimeNow = orig })

	m := newTestBrowse(t, 30, map[string]gitdata.CIState{}, nil)
	m.statuses = statusesFor(m, gitdata.StateSuccess)
	m.usageOv.Dismiss()
	m.height = 12
	clearTick(m)
	slowSeq, _ := m.ticks.Want(now, spinnerInterval) // スピナーの tick が張ってある
	now = now.Add(10 * time.Millisecond)
	m.handleKey("ctrl+d")
	if !m.glide.Active() {
		t.Fatal("ctrl+d で glide が立たない (テスト前提の破れ)")
	}
	due, _, ok := m.ticks.Pending()
	if want := now.Add(m.tickInterval()); !ok || !due.Equal(want) {
		t.Fatalf("glide が始まったのに、次の tick は %v 後 (%v 後に張り直していない)", due.Sub(now), m.tickInterval())
	}
	// glide は届いた tick の数で進み、ScrollAnimFrames 回で着く。古い tick を数えると 1 回早く着く
	// (ease-in の 1 フレーム目は表示の位置が動かないことがあるので、位置ではなく着くまでの回数で見る)
	now = now.Add(spinnerInterval)
	m.Update(tickMsg{seq: slowSeq})
	for i := 1; i < term.ScrollAnimFrames; i++ {
		now = now.Add(m.tickInterval())
		fireTick(m)
	}
	if !m.glide.Active() {
		t.Fatalf("追い越された古い tick を 1 フレームに数えた (新しい tick %d 回で glide が着いた。%d 回で着くはず)", term.ScrollAnimFrames-1, term.ScrollAnimFrames)
	}
	fireTick(m)
	if m.glide.Active() {
		t.Fatalf("新しい tick %d 回で glide が着かない (テストの前提の破れ)", term.ScrollAnimFrames)
	}
}
