package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"pro-con/card"
)

// pmSnap は依頼の列に 2 枚 (R1 は PM が分けている最中)・着手待ちに記録の担当が PM のまま 1 枚を置いた画面 (issue 476)。
func pmSnap(t *testing.T, pm card.RoleState) *Model {
	t.Helper()
	be := newSpy()
	now := be.snap.Now
	be.snap.Cards = []card.Card{
		{ID: "Q1", State: card.Requested, Owner: "PM", Since: now},
		{ID: "Q2", State: card.Requested, Owner: "PM", Since: now},
		{ID: "P1", State: card.Planned, Owner: "PM", Since: now},
	}
	pm.Name, pm.Max = card.PMName, 1
	be.snap.RoleStates = []card.RoleState{pm}
	return New(be, nil)
}

// ゲージに PM の数 / 上限と様子を出す。作業中なら手元のカード、起こせなければ理由も。
func TestGaugeShowsPM(t *testing.T) {
	cases := []struct {
		pm   card.RoleState
		want []string
	}{
		{card.RoleState{Phase: card.RoleBusy, Cards: []string{"Q1", "Q2", "W1"}}, []string{"PM 1/1 作業中 Q1 Q2 ほか 1"}},
		{card.RoleState{Phase: card.RoleIdle, Cards: []string{"Q1"}}, []string{"PM 1/1 idle"}},
		{card.RoleState{Phase: card.RoleNone}, []string{"PM 0/1 未起動"}},
		{card.RoleState{Phase: card.RoleBlocked, Why: "枠 96%: 新しく起動・再開しない"}, []string{"PM 0/1 起こせない", "枠 96%"}},
		{card.RoleState{Phase: card.RoleAsking}, []string{"PM 1/1 入力待ち", "attach"}},
		{card.RoleState{Phase: card.RoleOff}, []string{"PM off"}},
	}
	for _, tc := range cases {
		g := ansi.Strip(pmSnap(t, tc.pm).gauge())
		for _, w := range tc.want {
			if !strings.Contains(g, w) {
				t.Errorf("%s: ゲージに %q が無い: %q", tc.pm.Phase, w, g)
			}
		}
		if tc.pm.Phase == card.RoleIdle && strings.Contains(g, "Q1") {
			t.Errorf("idle の PM の手元のカードを作業中のように出した: %q", g)
		}
	}
	if g := ansi.Strip(New(newSpy(), nil).gauge()); strings.Contains(g, "PM") {
		t.Fatalf("PM の様子を書かない (古い) dispatcher なのに PM を出した: %q", g)
	}
}

// 止まった dispatcher の最後の様子は今の様子として出さない (ゲージ・カードの印・詳細の担当)。
func TestPMStaleWhenDispatcherStopped(t *testing.T) {
	m := pmSnap(t, card.RoleState{Phase: card.RoleBusy, Cards: []string{"Q1"}, Current: "Q1", Last: "Bash: pro-con card show Q1"})
	m.snap.DispatcherTick = m.snap.Now.Add(-dispatcherStale - time.Second)
	if g := ansi.Strip(m.gauge()); !strings.Contains(g, "PM 様子不明") || strings.Contains(g, "Q1") {
		t.Fatalf("止まった dispatcher の PM の様子を今のものとして出した: %q", g)
	}
	q1 := m.snap.Cards[0]
	if b := m.badge(q1); strings.Contains(b, "PM ") || strings.Contains(b, "▸") {
		t.Fatalf("止まった dispatcher の様子でカードに分解中を付けた: %q", b)
	}
	if a := m.assignee(q1); a != "PM" {
		t.Fatalf("止まった dispatcher の様子で担当に仕事を添えた: %q", a)
	}
}

// 依頼の列のカードごとに PM の段階を出す。1 回の turn で 2 枚知らせても、分解中と最後の道具の呼び出しは今扱っている 1 枚だけ (480)。
// 着手待ちの担当は記録の Owner (PM) ではなく PG 待ち (476)。
func TestLaneShowsPMWork(t *testing.T) {
	m := pmSnap(t, card.RoleState{Phase: card.RoleBusy, Cards: []string{"Q1", "Q2"}, Current: "Q2",
		Last: "Bash: pro-con card show Q2", LastAt: newSpy().snap.Now.Add(-5 * time.Second)})
	byID := map[string]card.Card{}
	for _, c := range m.snap.Cards {
		byID[c.ID] = c
	}
	if b := ansi.Strip(m.badgeColored(byID["Q2"])); !strings.Contains(b, "PM 分解中 ▸ Bash: pro-con card show Q2 5秒") {
		t.Fatalf("PM が扱っているカードに段階と最後の道具の呼び出しが無い: %q", b)
	}
	if b := m.badge(byID["Q1"]); !strings.Contains(b, "PM 知らせた") || strings.Contains(b, "分解中") || strings.Contains(b, "▸") {
		t.Fatalf("同じ turn でまだ扱っていないカードの段階: %q", b)
	}
	for id, want := range map[string]string{"Q1": "PM", "Q2": "PM 分解中", "P1": "PG 待ち"} {
		if got := m.assignee(byID[id]); got != want {
			t.Errorf("%s の担当 = %q (want %q)", id, got, want)
		}
	}
	m.snap.RoleStates[0].Cards = []string{"Q2"} // Q1 はまだ PM に知らせていない
	if b := m.badge(byID["Q1"]); !strings.Contains(b, "PM 知らせ待ち") {
		t.Fatalf("まだ知らせていないカードの段階: %q", b)
	}
}
