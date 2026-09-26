package card

import (
	"testing"
	"time"
)

// 担当は今手を動かす者。分解済みは記録の Owner (既定は PM) に関わらず PG 待ち。PM が手に取っていれば仕事の名前を添える (476)。
func TestAssignee(t *testing.T) {
	t1 := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	busy := []RoleState{{Name: PMName, Phase: RoleBusy, Cards: []string{"C-001", "C-002"}, Current: "C-001"}}
	cases := []struct {
		c    Card
		r    Roles
		ss   []RoleState
		want string
	}{
		{Card{ID: "C-001", State: Planned, Owner: "PM"}, Roles{}, busy, "PG 待ち"},
		{Card{ID: "C-001", State: Running, Owner: "PM"}, Roles{}, busy, "PG"},
		{Card{ID: "C-001", State: Requested, Owner: "PM"}, Roles{}, nil, "PM"},
		{Card{ID: "C-001", State: Requested, Owner: "PM"}, Roles{}, busy, "PM 分解中"},
		{Card{ID: "C-002", State: Requested, Owner: "PM"}, Roles{}, busy, "PM"},
		{Card{ID: "C-001", State: Waiting, Wait: Wait{Kind: WaitQuestion}}, Roles{}, busy, "PM 回答中"},
		{Card{ID: "C-001", State: Requested, Owner: "PM"}, Roles{PMOff: true}, busy, "人"},
		{Card{ID: "C-001", State: Waiting, Since: t1, Wait: Wait{Kind: WaitQuestion}, History: []Event{{At: t1, Text: HandoffText("PM", "人が決める")}}}, Roles{}, busy, "人"},
		{Card{ID: "C-001", State: Done, Owner: "PM"}, Roles{}, busy, ""},
	}
	for _, tc := range cases {
		if got := tc.c.Assignee(tc.r, tc.ss); got != tc.want {
			t.Errorf("%s %s (Owner %s): 担当 %q (want %q)", tc.c.ID, tc.c.State.Label(), tc.c.Owner, got, tc.want)
		}
	}
}

// 扱っているのは、turn の途中の役が今の turn で扱っている 1 枚 (Current) だけ。知らせ済みでも Current でなければ扱っていない (480)。
// 起動・再開の最中は知らせを渡しているだけ、入力待ち・idle・落ちた役は手を止めている。
func TestHandling(t *testing.T) {
	c := Card{ID: "C-001", State: Requested}
	for _, tc := range []struct {
		p       RolePhase
		current string
		want    string
	}{{RoleBusy, "C-001", "分解中"}, {RoleBusy, "C-002", ""}, {RoleBusy, "", ""}, {RoleLaunch, "C-001", ""}, {RoleIdle, "C-001", ""},
		{RoleAsking, "C-001", ""}, {RoleDead, "C-001", ""}} {
		ss := []RoleState{{Name: PMName, Phase: tc.p, Cards: []string{"C-001", "C-002"}, Current: tc.current}}
		if got := c.Handling(Roles{}, ss); got != tc.want {
			t.Errorf("%s (扱っている %q): Handling = %q (want %q)", tc.p, tc.current, got, tc.want)
		}
		if got := (Card{ID: "C-003", State: Requested}).Handling(Roles{}, ss); got != "" {
			t.Errorf("%s: 知らせていないカードを扱っている扱い: %q", tc.p, got)
		}
	}
}

// 依頼の列のカードごとに PM の段階を出す: 知らせ待ち / 知らせた / 分解中 / 入力待ち / 手を止めた (480)。
func TestRoleStep(t *testing.T) {
	req := func(id string) Card { return Card{ID: id, State: Requested} }
	pm := func(p RolePhase, current string) []RoleState {
		return []RoleState{{Name: PMName, Phase: p, Cards: []string{"C-001", "C-002"}, Current: current, Last: "Bash: pro-con card show C-001"}}
	}
	cases := []struct {
		name string
		c    Card
		r    Roles
		ss   []RoleState
		want string
	}{
		{"まだ知らせていない", req("C-003"), Roles{}, pm(RoleBusy, "C-001"), "PM 知らせ待ち"},
		{"PM が落ちていても知らせ待ち", req("C-003"), Roles{}, pm(RoleDead, ""), "PM 知らせ待ち"},
		{"扱っている", req("C-001"), Roles{}, pm(RoleBusy, "C-001"), "PM 分解中"},
		{"同じ turn の別のカード", req("C-002"), Roles{}, pm(RoleBusy, "C-001"), "PM 知らせた"},
		{"知らせを渡している最中", req("C-001"), Roles{}, pm(RoleLaunch, ""), "PM 知らせた"},
		{"扱っている最中に止まった", req("C-001"), Roles{}, pm(RoleAsking, "C-001"), "PM 入力待ち"},
		{"turn を終えたのに残っている", req("C-001"), Roles{}, pm(RoleIdle, ""), "PM 知らせた (手を止めた)"},
		{"PM を起こさない (人の番)", req("C-001"), Roles{PMOff: true}, pm(RoleBusy, "C-001"), ""},
		{"PM の様子が無い (古い dispatcher)", req("C-001"), Roles{}, nil, ""},
		{"分解済みは役の番ではない", Card{ID: "C-001", State: Planned}, Roles{}, pm(RoleBusy, "C-001"), ""},
	}
	for _, tc := range cases {
		if got := tc.c.RoleStep(tc.r, tc.ss); got != tc.want {
			t.Errorf("%s: RoleStep = %q (want %q)", tc.name, got, tc.want)
		}
		last, _, ok := tc.c.LastCall(tc.r, tc.ss)
		if want := tc.want == "PM 分解中"; ok != want || (ok && last != "Bash: pro-con card show C-001") {
			t.Errorf("%s: 最後の道具の呼び出し = %q, %v (扱っているカードにだけ添える)", tc.name, last, ok)
		}
	}
}
