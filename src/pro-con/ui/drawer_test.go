package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"pro-con/card"
)

// drawerModel は質問待ちのレーンに W1 (履歴 60 件) と W2 を置き、W1 を選んだ状態。
func drawerModel(t *testing.T) (*Model, *spy, *clock) {
	t.Helper()
	be := newSpy()
	for i := range be.snap.Cards {
		if be.snap.Cards[i].ID == "W1" {
			for n := range 60 {
				be.snap.Cards[i].History = append(be.snap.Cards[i].History, card.Event{At: be.snap.Now, Text: fmt.Sprintf("履歴-%02d", n)})
			}
		}
	}
	clk := &clock{t: be.snap.Now}
	m := New(be, nil)
	m.now = clk.now
	m.width, m.height = 120, 30
	m.selected = "W1"
	return m, be, clk
}

func screen(m *Model) string { return ansi.Strip(m.render()) }

// open は Enter で開き、演出を終わらせる。
func open(t *testing.T, m *Model, clk *clock) {
	t.Helper()
	press(m, "enter")
	clk.t = clk.t.Add(drawerDuration)
	m.onFrame()
	if !m.showDetail || !strings.Contains(screen(m), "履歴-00") {
		t.Fatalf("enter で W1 の詳細が開かない:\n%s", screen(m))
	}
}

// Enter で詳細が右から重なる。押した瞬間は見えず、開き切るとカンバンの上に出て、左にカンバンの端が残る。
func TestDrawerSlidesInFromRight(t *testing.T) {
	m, _, clk := drawerModel(t)
	if cmd := press(m, "enter"); cmd == nil {
		t.Fatal("開いたのに演出の tick が回らない")
	}
	if strings.Contains(screen(m), "履歴-00") {
		t.Fatal("押した瞬間に詳細が出ている (滑り込んでこない)")
	}
	clk.t = clk.t.Add(drawerDuration)
	out := screen(m)
	if !strings.Contains(out, "履歴-00") || !strings.Contains(out, "▏") {
		t.Fatalf("開き切っても詳細が重ならない:\n%s", out)
	}
	if lines := strings.Split(out, "\n"); len(lines) != m.height {
		t.Fatalf("画面の行数 %d が高さ %d と違う", len(lines), m.height)
	}
	if !strings.Contains(out, "╭─") {
		t.Fatal("左にカンバンの端 (列の枠) が残っていない")
	}
}

// 開いている間は j / G が本文のスクロールに効く。履歴は切り出さずに全部出る (最後の 1 件まで届く)。
func TestDrawerScrollsWithMotionKeys(t *testing.T) {
	m, _, clk := drawerModel(t)
	open(t, m, clk)
	if strings.Contains(screen(m), "履歴-59") {
		t.Fatal("前提: 画面の高さ 30 では最後の履歴は最初は見えないはず")
	}
	press(m, "j")
	if m.pager.Offset != 1 {
		t.Fatalf("j で 1 行スクロールしない: offset=%d", m.pager.Offset)
	}
	press(m, "G")
	if !strings.Contains(screen(m), "履歴-59") {
		t.Fatalf("G で末尾 (最後の履歴) まで届かない:\n%s", screen(m))
	}
	press(m, "g")
	if m.pager.Offset != 0 {
		t.Fatalf("g で先頭へ戻らない: offset=%d", m.pager.Offset)
	}
}

// 開いている間はカンバンの移動を飲み込む (詳細の下で選択が動くと、開いているカードと操作の対象が食い違う)。
func TestDrawerSwallowsBoardMoves(t *testing.T) {
	m, _, clk := drawerModel(t)
	open(t, m, clk)
	col, sel := m.col, m.selected
	for _, k := range []string{"l", "tab", "1"} {
		press(m, k)
	}
	if m.col != col || m.selected != sel || m.tab != "" {
		t.Fatalf("詳細の下でカンバンが動いた: col %d→%d / 選択 %s→%s / tab %q", col, m.col, sel, m.selected, m.tab)
	}
}

// J / K は開いたまま同じレーンの隣のカードへ送る。本文は先頭へ戻す。端では止めて知らせる。
func TestDrawerStepsToNextCardInLane(t *testing.T) {
	m, _, clk := drawerModel(t)
	open(t, m, clk)
	press(m, "j", "j")
	press(m, "J")
	if m.selected != "W2" || m.drawerCard != "W2" || m.pager.Offset != 0 || !m.showDetail {
		t.Fatalf("J で W2 へ送られない: 選択 %s / 表示 %s / offset %d / 開いている %v", m.selected, m.drawerCard, m.pager.Offset, m.showDetail)
	}
	press(m, "J")
	if m.selected != "W2" || !strings.Contains(m.flash, "最後") {
		t.Fatalf("端で止まって知らせるはず: 選択 %s / %q", m.selected, m.flash)
	}
	press(m, "K")
	if m.selected != "W1" {
		t.Fatalf("K で戻らない: %s", m.selected)
	}
}

// カードへの操作は開いたまま効く (r で回答の入力欄が開く)。
func TestDrawerPassesCardActions(t *testing.T) {
	m, _, clk := drawerModel(t)
	open(t, m, clk)
	press(m, "r")
	if m.mode != modeInput || m.inputKind != inputAnswer {
		t.Fatalf("詳細を開いたまま r で回答できない: mode=%v", m.mode)
	}
}

// q で閉じる。閉じる途中は中身を残し (逆再生で見えている)、閉じ切ってから捨てる。
func TestDrawerClosesAndKeepsContentWhileClosing(t *testing.T) {
	for _, k := range []string{"q", "esc", "h", "enter"} {
		m, _, clk := drawerModel(t)
		open(t, m, clk)
		press(m, k)
		if m.showDetail {
			t.Fatalf("%s で閉じない", k)
		}
		clk.t = clk.t.Add(drawerDuration / 4)
		if !strings.Contains(screen(m), "履歴-00") {
			t.Fatalf("%s: 閉じる途中で中身が消えた (逆再生にならない)", k)
		}
		clk.t = clk.t.Add(drawerDuration)
		m.onFrame()
		if m.drawerCard != "" || strings.Contains(screen(m), "履歴-00") {
			t.Fatalf("%s: 閉じ切った後も中身が残っている", k)
		}
	}
}

// 開いているカードが Snapshot から消えたら (片付け等)、引き出しを閉じる。
func TestDrawerClosesWhenCardVanishes(t *testing.T) {
	m, be, clk := drawerModel(t)
	open(t, m, clk)
	for i := range be.snap.Cards {
		if be.snap.Cards[i].ID == "W1" {
			be.snap.Cards[i].State, be.snap.Cards[i].Ending, be.snap.Cards[i].Archived = card.Done, card.EndAnswered, true
		}
	}
	m.Update(tickMsg{})
	if m.showDetail || m.drawerCard != "" {
		t.Fatalf("消えたカードの詳細が開いたまま: showDetail=%v drawerCard=%q", m.showDetail, m.drawerCard)
	}
}
