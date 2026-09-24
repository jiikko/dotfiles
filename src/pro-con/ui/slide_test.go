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

// detailRows は画面に出ている詳細の板の行数 (上枠の見出しに選択中のカード ID が出る)。
func detailRows(t *testing.T, m *Model) int {
	t.Helper()
	lines := strings.Split(ansi.Strip(m.render()), "\n")
	if len(lines) != m.height {
		t.Fatalf("画面の行数 %d が高さ %d と違う (板の開閉の途中で画面からはみ出した)", len(lines), m.height)
	}
	n := 0
	for i := len(lines) - 2; i >= 0; i-- { // 最下行は案内。その上に詳細が下から積まれている
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

// enter で詳細が下から生える: 押した瞬間は 0 行、途中は一部、所要の後は全部。閉じるときは逆に沈む。
func TestDetailSlidesUpFromFooter(t *testing.T) {
	m, clk := slideModel(t)
	if cmd := press(m, "enter"); cmd == nil {
		t.Fatal("開いたのに演出の tick が回らない")
	}
	if n := detailRows(t, m); n != 0 {
		t.Fatalf("押した瞬間に詳細が %d 行出ている (生えてこない)", n)
	}
	clk.t = clk.t.Add(slideDuration() / 4)
	mid := detailRows(t, m)
	clk.t = clk.t.Add(slideDuration())
	full := detailRows(t, m)
	if full < 3 || mid <= 0 || mid >= full {
		t.Fatalf("途中で一部だけ見えるはず: 途中 %d 行 / 開き切って %d 行", mid, full)
	}
	m.onFrame()
	if len(m.slides) != 0 {
		t.Fatal("開き切った後も演出が残っている (tick が止まらない)")
	}

	press(m, "enter")
	if n := detailRows(t, m); n != full {
		t.Fatalf("閉じ始めの瞬間は開き切った高さのはず: %d / %d", n, full)
	}
	clk.t = clk.t.Add(slideDuration() / 4)
	if n := detailRows(t, m); n <= 0 || n >= full {
		t.Fatalf("閉じる途中は一部だけ残るはず: %d / %d", n, full)
	}
	clk.t = clk.t.Add(slideDuration())
	if n := detailRows(t, m); n != 0 {
		t.Fatalf("閉じ切ったのに %d 行残っている", n)
	}
}

// 開く途中で閉じたら、今の高さから沈む (いったん全開や 0 に飛ばない)。
func TestDetailSlideReversesFromCurrentHeight(t *testing.T) {
	m, clk := slideModel(t)
	press(m, "enter")
	clk.t = clk.t.Add(slideDuration() / 4)
	before := detailRows(t, m)
	press(m, "enter")
	if after := detailRows(t, m); after != before {
		t.Fatalf("反転した瞬間に高さが飛んだ: %d → %d", before, after)
	}
}

// s の PG の一覧も同じ演出で生える。
func TestPGPanelSlidesUp(t *testing.T) {
	m, clk := slideModel(t)
	press(m, "s")
	if strings.Contains(ansi.Strip(m.render()), "PG (consumer)") {
		t.Fatal("押した瞬間に PG の一覧の見出しまで出ている (生えてこない)")
	}
	clk.t = clk.t.Add(slideDuration())
	if !strings.Contains(ansi.Strip(m.render()), "PG (consumer)") {
		t.Fatal("所要の後も PG の一覧が出ない")
	}
}
