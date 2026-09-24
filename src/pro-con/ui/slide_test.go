package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// settle は開閉の演出を終わらせる (演出の途中の見た目を見ないテスト用)。
func settle(m *Model) { m.slides = map[panel]*slide{} }

func slideModel(t *testing.T) (*Model, *clock) {
	t.Helper()
	be := newSpy()
	clk := &clock{t: be.snap.Now}
	m := New(be, nil)
	m.now = clk.now
	m.width, m.height = 120, 60
	return m, clk
}

// pgRows は画面の下端 (案内の上) に出ている PG の一覧の板の行数。
func pgRows(t *testing.T, m *Model) int {
	t.Helper()
	lines := strings.Split(ansi.Strip(m.render()), "\n")
	if len(lines) != m.height {
		t.Fatalf("画面の行数 %d が高さ %d と違う (板の開閉の途中で画面からはみ出した)", len(lines), m.height)
	}
	n := 0
	for i := len(lines) - 2; i >= 0; i-- { // 最下行は案内。その上に板が下から積まれている
		if !strings.HasPrefix(lines[i], "╭") && !strings.HasPrefix(lines[i], "│") && !strings.HasPrefix(lines[i], "╰") {
			break
		}
		n++
		if strings.HasPrefix(lines[i], "╭") {
			break
		}
	}
	return n
}

// s で PG の一覧が下から生える: 押した瞬間は 0 行、途中は一部、所要の後は全部。閉じるときは逆に沈む。
func TestPGPanelSlidesUpFromFooter(t *testing.T) {
	m, clk := slideModel(t)
	if cmd := press(m, "s"); cmd == nil {
		t.Fatal("開いたのに演出の tick が回らない")
	}
	if n := pgRows(t, m); n != 0 {
		t.Fatalf("押した瞬間に %d 行出ている (生えてこない)", n)
	}
	clk.t = clk.t.Add(slideDuration() / 4)
	mid := pgRows(t, m)
	clk.t = clk.t.Add(slideDuration())
	full := pgRows(t, m)
	if full < 3 || mid <= 0 || mid >= full {
		t.Fatalf("途中で一部だけ見えるはず: 途中 %d 行 / 開き切って %d 行", mid, full)
	}
	m.onFrame()
	if len(m.slides) != 0 {
		t.Fatal("開き切った後も演出が残っている (tick が止まらない)")
	}

	press(m, "s")
	if n := pgRows(t, m); n != full {
		t.Fatalf("閉じ始めの瞬間は開き切った高さのはず: %d / %d", n, full)
	}
	clk.t = clk.t.Add(slideDuration() / 4)
	if n := pgRows(t, m); n <= 0 || n >= full {
		t.Fatalf("閉じる途中は一部だけ残るはず: %d / %d", n, full)
	}
	clk.t = clk.t.Add(slideDuration())
	if n := pgRows(t, m); n != 0 {
		t.Fatalf("閉じ切ったのに %d 行残っている", n)
	}
}

// 開く途中で閉じたら、今の高さから沈む (いったん全開や 0 に飛ばない)。
func TestPGPanelSlideReversesFromCurrentHeight(t *testing.T) {
	m, clk := slideModel(t)
	press(m, "s")
	clk.t = clk.t.Add(slideDuration() / 4)
	before := pgRows(t, m)
	press(m, "s")
	if after := pgRows(t, m); after != before {
		t.Fatalf("反転した瞬間に高さが飛んだ: %d → %d", before, after)
	}
}
