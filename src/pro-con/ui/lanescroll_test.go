package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"tuikit/layout"

	"pro-con/card"
)

// doneLaneModel は完了のレーンに n 枚 (タイトル T0..) を積んで、そのレーンの先頭のカードを選んだ画面。
func doneLaneModel(t *testing.T, n int) *Model {
	t.Helper()
	be := newSpy()
	for i := range n {
		be.snap.Cards = append(be.snap.Cards, card.Card{ID: fmt.Sprintf("D%02d", i), Title: fmt.Sprintf("T%02d", i), State: card.Done, Ending: card.EndAnswered, Since: be.snap.Now})
	}
	m := New(be, nil)
	m.now = (&clock{t: be.snap.Now}).now
	m.width, m.height = 180, 30
	m.selected = "D00"
	m.ensureSelection()
	m.Update(frameMsg{})
	return m
}

// doneLaneRows はボードの各行のうち完了のレーンの帯 (枠の内側)。
func doneLaneRows(m *Model) []string {
	col := slices.Index(card.Columns, card.Done)
	w := m.colWidth()
	x := col * (w + len(colSep))
	var out []string
	for _, l := range m.overlayCursor(m.boardLines()) {
		out = append(out, ansi.Strip(ansi.Cut(l, x+1, x+w-1)))
	}
	return out
}

// 選択を下へ進めると完了のレーンが追ってスクロールし、選択中のカードと枠が見えている (issue 499)。
func TestDoneLaneScrollsToSelection(t *testing.T) {
	m := doneLaneModel(t, 30)
	shown := m.shownCards()
	if shown >= 30 {
		t.Fatalf("前提: 30 枚が入り切らない高さにする (shown=%d)", shown)
	}
	for range 29 {
		press(m, "j")
	}
	if m.selected != "D29" {
		t.Fatalf("末尾まで進めたのに選択が %s", m.selected)
	}
	band := strings.Join(doneLaneRows(m), "\n")
	if !strings.Contains(band, "T29") {
		t.Fatalf("選択中のカード T29 がレーンに出ていない:\n%s", band)
	}
	if strings.Contains(band, "T00") {
		t.Fatalf("スクロールしたのに先頭の T00 が見えている:\n%s", band)
	}
	if frameCol(m) < 0 {
		t.Fatalf("スクロールした先で選択の枠が消えた")
	}
	for range 29 {
		press(m, "k")
	}
	if band := strings.Join(doneLaneRows(m), "\n"); !strings.Contains(band, "T00") {
		t.Fatalf("先頭へ戻したのに T00 が見えない:\n%s", band)
	}
}

// 入り切らないレーンだけ右端にスクロールバーを出し、末尾では thumb が下端に着く。入り切るレーンには出さない。
func TestDoneLaneScrollbarOnlyWhenOverflowing(t *testing.T) {
	m := doneLaneModel(t, 30)
	bar := func(rows []string) string {
		var b strings.Builder
		for _, r := range rows[1 : len(rows)-1] { // 上枠と下枠を除く
			b.WriteString(ansi.Cut(r, ansi.StringWidth(r)-1, ansi.StringWidth(r)))
		}
		return b.String()
	}
	col := bar(doneLaneRows(m))
	if !strings.HasPrefix(col, layout.ScrollbarThumb) || strings.HasSuffix(col, layout.ScrollbarThumb) {
		t.Fatalf("先頭では thumb が上端にあるはず: %q", col)
	}
	press(m, "G")
	col = bar(doneLaneRows(m))
	if strings.HasPrefix(col, layout.ScrollbarThumb) || !strings.HasSuffix(col, layout.ScrollbarThumb) {
		t.Fatalf("末尾では thumb が下端に着くはず: %q", col)
	}

	few := doneLaneModel(t, 2)
	if col := bar(doneLaneRows(few)); strings.Contains(col, layout.ScrollbarThumb) || strings.Contains(col, layout.ScrollbarTrack) {
		t.Fatalf("入り切るレーンにスクロールバーが出ている: %q", col)
	}
}

// 選択より手前のカードが別のレーンへ移っている途中は、移動元に点線の枠が残って並びが 1 段ずれる。追従はその段で数える
// (カードの添字で数えると、末尾まで進めた選択中のカードが見える範囲の 1 段下に落ちる)。
func TestLaneScrollCountsGhostOfMovingCard(t *testing.T) {
	m := doneLaneModel(t, 30)
	be := m.be.(*spy)
	for i := range be.snap.Cards {
		if be.snap.Cards[i].ID == "D05" {
			be.snap.Cards[i].State = card.Planned
		}
	}
	m.Update(tickMsg{})
	if len(m.moves) != 1 {
		t.Fatalf("前提: D05 の移動の演出が始まっていない (moves=%d)", len(m.moves))
	}
	press(m, "G") // 演出の途中 (時計は止めてある) で末尾へ
	if band := strings.Join(doneLaneRows(m), "\n"); !strings.Contains(band, "T29") {
		t.Fatalf("点線の枠が手前に入ったら選択中の T29 が見えなくなった:\n%s", band)
	}
}

// 下端までスクロールしたレーンの末尾のカードが出ていくとき、滑り出しの始点は移動元に残す点線の枠の段 (1 段ずれない)。
func TestMoveStartsAtGhostInScrolledLane(t *testing.T) {
	m := doneLaneModel(t, 30)
	press(m, "G")
	top := m.laneTop(slices.Index(card.Columns, card.Done), 30)
	be := m.be.(*spy)
	for i := range be.snap.Cards {
		if be.snap.Cards[i].ID == "D29" {
			be.snap.Cards[i].State = card.Planned
		}
	}
	m.Update(tickMsg{})
	mv, ok := m.moves["D29"]
	if !ok {
		t.Fatalf("前提: D29 の移動の演出が始まっていない")
	}
	if want := float64(1 + cardGap + (29-top)*perCardLines); mv.fromY != want {
		t.Fatalf("滑り出しの始点の行 %v (期待 %v = 点線の枠の段)", mv.fromY, want)
	}
}
