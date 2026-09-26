package ui

import (
	"fmt"
	"strings"
	"testing"

	"pro-con/card"
)

// 待っているカード (テストの係の結果待ち・再開待ち) は、そのカード固有の色のまま明度を waitDim 倍にした地で描く (issue 455)。
// 動いているカード・まだ起動していない着手待ちのカードは、固有の 256 色の地のまま。選択中でも暗さは変えない。
func TestWaitingCardDimsOwnColor(t *testing.T) {
	m := New(newSpy(), nil)
	for _, tc := range []struct {
		name string
		c    card.Card
		dim  bool
	}{
		{"作業中 (turn の途中)", card.Card{ID: "C-001", State: card.Running, Session: "s1"}, false},
		{"テストの係の順番待ち", card.Card{ID: "C-002", State: card.Running, Session: "s2", Run: "make test"}, true},
		{"テストの係が実行中", card.Card{ID: "C-003", State: card.Running, Session: "s3", Run: "make test", Exec: card.Exec{Command: "make test"}}, true},
		{"再開待ち", card.Card{ID: "C-004", State: card.Planned, Session: "s4", Resume: "テストの係の結果: rc=0"}, true},
		{"空き待ち (未起動)", card.Card{ID: "C-005", State: card.Planned}, false},
		{"質問待ち", card.Card{ID: "C-006", State: card.Waiting, Session: "s6"}, false},
	} {
		for _, sel := range []bool{false, true} {
			m.selected = ""
			if sel {
				m.selected = tc.c.ID
			}
			line := m.cardCell(tc.c, 30)[0]
			n := cardColor(tc.c.ID)
			r, g, b := rgb256(n)
			dimmed := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", int(float64(r)*waitDim+0.5), int(float64(g)*waitDim+0.5), int(float64(b)*waitDim+0.5))
			own := fmt.Sprintf("\x1b[48;5;%dm", n)
			if got := strings.HasPrefix(line, dimmed); got != tc.dim {
				t.Fatalf("%s (選択中=%v): 固有の色を暗くした地 = %v (期待 %v): %q", tc.name, sel, got, tc.dim, line)
			}
			if !tc.dim && !strings.HasPrefix(line, own) {
				t.Fatalf("%s (選択中=%v): 固有の色の地で描いていない: %q", tc.name, sel, line)
			}
		}
	}
}
