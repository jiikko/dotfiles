package card

import (
	"testing"
	"time"
)

// 担当は今手を動かす者。分解済みは記録の Owner (既定は PM) に関わらず PG 待ち。PM が手に取っていれば仕事の名前を添える (476)。
func TestAssignee(t *testing.T) {
	t1 := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	busy := []RoleState{{Name: PMName, Phase: RoleBusy, Cards: []string{"C-001"}}}
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

// 手に取っているのは、知らせ済みのカードを turn の途中か起動・再開の最中の役だけ (idle・入力待ち・落ちた役は手を止めている)。
func TestHandling(t *testing.T) {
	c := Card{ID: "C-001", State: Requested}
	for _, tc := range []struct {
		p    RolePhase
		want string
	}{{RoleBusy, "分解中"}, {RoleLaunch, "分解中"}, {RoleIdle, ""}, {RoleAsking, ""}, {RoleDead, ""}} {
		ss := []RoleState{{Name: PMName, Phase: tc.p, Cards: []string{"C-001"}}}
		if got := c.Handling(Roles{}, ss); got != tc.want {
			t.Errorf("%s: Handling = %q (want %q)", tc.p, got, tc.want)
		}
		if got := (Card{ID: "C-002", State: Requested}).Handling(Roles{}, ss); got != "" {
			t.Errorf("%s: 知らせていないカードを手に取っている扱い: %q", tc.p, got)
		}
	}
}
