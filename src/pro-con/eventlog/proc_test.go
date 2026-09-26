package eventlog

import (
	"testing"
	"time"
)

// 役は種類とカードの欄から決める。カードの状態の変化・見張りの知らせはプロセスの出来事ではない (AnyRole では役を付けて出す)。
func TestRole(t *testing.T) {
	for _, tc := range []struct {
		e    Event
		role string
		ok   bool
	}{
		{Event{Kind: KindSupervisor, Reason: "dispatcher が落ちた"}, RoleSupervisor, true},
		{Event{Kind: KindDispatcher, Reason: "dispatcher が起きた"}, RoleDispatcher, true},
		{Event{Kind: KindUpgrade}, RoleDispatcher, true},
		{Event{Kind: KindMonitor, Reason: "見張りが抜けた (rc=0)"}, RoleMonitor, true},
		{Event{Kind: KindMonitor, Card: "C-001", Reason: "取り込みで衝突する"}, RoleMonitor, false},
		{Event{Kind: KindScreen}, RoleScreen, true},
		{Event{Kind: KindLaunch, Card: "PM"}, RolePM, true},
		{Event{Kind: KindLaunch, Card: "INT"}, RoleIntegrator, true},
		{Event{Kind: KindStop, Card: "C-012"}, RolePG, true},
		{Event{Kind: KindLaunch, Reason: "claude は … を使う"}, RoleDispatcher, true},
		{Event{Kind: KindRegister}, RolePG, true},
		{Event{Kind: KindApply, Card: "C-012"}, "", false},
		{Event{Kind: KindRun, Card: "C-012"}, "", false},
	} {
		if r, ok := Role(tc.e); r != tc.role || ok != tc.ok {
			t.Errorf("Role(%+v) = %q %v, want %q %v", tc.e, r, ok, tc.role, tc.ok)
		}
	}
	if r, ok := AnyRole(Event{Kind: KindApply, Card: "C-012"}); r != RolePG || !ok {
		t.Errorf("AnyRole(apply) = %q %v", r, ok)
	}
	if r, ok := AnyRole(Event{Kind: KindMonitor, Card: "C-001", Reason: "取り込みで衝突する"}); r != RoleMonitor || !ok {
		t.Errorf("AnyRole(見張りの知らせ) = %q %v", r, ok)
	}
}

// 同じ出来事 (数字だけ違う) の FoldGap 以内の繰り返しは 1 行に畳み、最後の 1 件の位置に並べる。間が空く・カードが違う・種類が違うものは畳まない。
// ファイルの中で前後した出来事 (受付の箱を経たもの) は時刻の順に並べ直す。
func TestFold(t *testing.T) {
	t0 := time.Date(2026, 9, 26, 21, 0, 0, 0, time.UTC)
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	fail := func(s, pid int, cardID string) Event {
		return Event{At: at(s), Kind: KindLaunch, Card: cardID, Reason: cardID + " の PG を再開できない: pid が記録 (" + itoa(pid) + ") と違う"}
	}
	evs := []Event{
		fail(0, 1, "C-001"),
		{At: at(1), Kind: KindApply, Card: "C-001", Reason: "適用した"}, // プロセスの出来事ではない: 落ちる (畳みも切らない)
		fail(3, 2, "C-001"),
		fail(4, 3, "C-002"), // 別のカード
		{At: at(5), Kind: KindSupervisor, Reason: "dispatcher が落ちた"},
		fail(6, 4, "C-001"),
		fail(6+int(FoldGap/time.Second)+1, 5, "C-001"), // 間が空いた: 別の行
		{At: at(2), Kind: KindDispatcher, Reason: "dispatcher が起きた"}, // 後から書かれた: 時刻の順へ
	}
	got := Fold(evs, Role)
	type row struct {
		reason string
		n      int
	}
	want := []row{
		{"dispatcher が起きた", 1},
		{"C-002 の PG を再開できない: pid が記録 (3) と違う", 1},
		{"dispatcher が落ちた", 1},
		{"C-001 の PG を再開できない: pid が記録 (4) と違う", 3},
		{"C-001 の PG を再開できない: pid が記録 (5) と違う", 1},
	}
	if len(got) != len(want) {
		t.Fatalf("行の数 %d: %+v", len(got), got)
	}
	for i, w := range want {
		if got[i].Last.Reason != w.reason || got[i].N != w.n {
			t.Errorf("%d 行目 = %q ×%d, want %q ×%d", i, got[i].Last.Reason, got[i].N, w.reason, w.n)
		}
	}
	if !got[3].First.Equal(at(0)) || got[3].Role != RolePG {
		t.Errorf("畳んだ行の最初の時刻・役: %v %q", got[3].First, got[3].Role)
	}
	if n := len(Fold(evs, AnyRole)); n != 6 { // カードの出来事も出すと、適用した 1 件が加わる
		t.Errorf("AnyRole で %d 行", n)
	}
}

func itoa(n int) string { return string(rune('0' + n)) }
