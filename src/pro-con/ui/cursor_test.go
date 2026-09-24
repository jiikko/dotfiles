package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"pro-con/backend"
	"pro-con/card"
)

// frameCol は画面に描かれた枠の左辺 (┃) の表示桁。見つからなければ -1。
func frameCol(m *Model) int {
	for _, l := range strings.Split(ansi.Strip(m.render()), "\n") {
		if i := strings.Index(l, "┃"); i >= 0 {
			return ansi.StringWidth(l[:i])
		}
	}
	return -1
}

func cursorModel(t *testing.T) (*Model, *clock) {
	t.Helper()
	be := newSpy()
	clk := &clock{t: be.snap.Now}
	m := New(be, nil)
	m.now = clk.now
	m.width, m.height = 120, 30
	m.selected = "R1" // 作業中のレーン (3 列目)
	m.Update(frameMsg{})
	return m, clk
}

// 選択中のカードを枠が囲む。枠の左辺は選んだカードのレーンの外枠に重なる。
func TestCursorFramesSelectedCard(t *testing.T) {
	m, _ := cursorModel(t)
	want := 2 * (m.colWidth() + len(colSep)) // 3 列目の外枠の桁
	if got := frameCol(m); got != want {
		t.Fatalf("枠の左辺が %d 桁目 (期待 %d = 作業中のレーン)", got, want)
	}
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "┗") {
		t.Fatalf("枠の下辺が無い:\n%s", out)
	}
}

// l でレーンを移ると、枠は元の位置から行き先まで滑る (押した瞬間は元の位置、途中はあいだ、所要の後に行き先)。
func TestCursorGlidesAcrossLanes(t *testing.T) {
	m, clk := cursorModel(t)
	from := frameCol(m)
	if cmd := press(m, "l"); cmd == nil {
		t.Fatal("選択が動いたのに演出の tick が回らない")
	}
	if m.selected == "R1" {
		t.Fatal("前提: l で質問待ちのレーンへ移るはず")
	}
	to := 3 * (m.colWidth() + len(colSep))
	if got := frameCol(m); got != from {
		t.Fatalf("押した瞬間に枠が飛んだ: %d → %d (滑らない)", from, got)
	}
	clk.t = clk.t.Add(cursorDuration / 3)
	if mid := frameCol(m); mid <= from || mid >= to {
		t.Fatalf("途中の枠が元と行き先のあいだに居ない: %d (元 %d / 行き先 %d)", mid, from, to)
	}
	clk.t = clk.t.Add(cursorDuration)
	if got := frameCol(m); got != to {
		t.Fatalf("所要の後に行き先へ着いていない: %d (期待 %d)", got, to)
	}
	m.onFrame()
	if m.animating() {
		t.Fatal("着いた後も演出が残っている (tick が止まらない)")
	}
}

// タブの切り替えは「選択が動いた」ではないので、枠を滑らせずに置き直す。
func TestCursorSnapsOnTabSwitch(t *testing.T) {
	be := newSpy()
	be.snap.Cards = append(be.snap.Cards, card.Card{ID: "D1", Repo: "dotfiles", State: card.Planned, Since: be.snap.Now})
	clk := &clock{t: be.snap.Now}
	m := New(be, []backend.Repo{{Name: "dotfiles"}})
	m.now = clk.now
	m.width, m.height = 120, 30
	m.selected = "R1" // 切り替え先 (dotfiles の D1) と違うレーンに居る: 置き直さなければ滑って見える
	m.Update(frameMsg{})
	press(m, "tab")
	if m.tab != "dotfiles" {
		t.Fatalf("前提: tab で dotfiles のタブへ移るはず: %q", m.tab)
	}
	if m.cursorGliding(clk.t) {
		t.Fatal("タブの切り替えで枠が滑っている")
	}
}

// 枠はカードの上下の空き行に描くので、選んだカードの上下のカードも見えたまま。見出しの文字も消さない。
func TestCursorLeavesNeighborsVisible(t *testing.T) {
	be := newSpy()
	be.snap.Cards = append(be.snap.Cards,
		card.Card{ID: "R2", State: card.Running, Session: "s-r2", Since: be.snap.Now.Add(1)},
		card.Card{ID: "R3", State: card.Running, Session: "s-r3", Since: be.snap.Now.Add(2)})
	clk := &clock{t: be.snap.Now}
	m := New(be, nil)
	m.now = clk.now
	m.width, m.height = 120, 30
	m.selected = "R2" // 作業中のレーンの真ん中
	m.Update(frameMsg{})
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "┃") {
		t.Fatalf("前提: 枠が描かれていない:\n%s", out)
	}
	for _, want := range []string{"R1", "R2", "R3", "3 作業中"} {
		if !strings.Contains(out, want) {
			t.Fatalf("枠で %q が隠れた:\n%s", want, out)
		}
	}
}

// 列の見出しが列の幅より長くても、枠の幅を超えない (超えると、その行の右の列がすべてずれ、枠の角も外枠からずれる)。
func TestBoxTopFitsWidth(t *testing.T) {
	for _, w := range []int{8, 17, 30} {
		got := ansi.StringWidth(boxTop(fg(240), "▶ 4 質問待ちのとても長い見出し (12)", w))
		if got != w {
			t.Fatalf("幅 %d の枠の上辺が %d 桁", w, got)
		}
	}
}

// 狭い列でも、見出しのキー (1〜6) と枚数は残し、状態の名前の方を削る。
func TestLaneLabelKeepsKeyAndCount(t *testing.T) {
	m, _ := cursorModel(t)
	m.width = 110 // 列の幅 17。「▶ 3 作業中 (1)」がそのままでは入らない
	head := strings.Split(ansi.Strip(m.render()), "\n")[headerRows]
	for _, want := range []string{"3 作", "(1)", "4 質", "(2)"} {
		if !strings.Contains(head, want) {
			t.Fatalf("見出しに %q が無い: %q", want, head)
		}
	}
}
