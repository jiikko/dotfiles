package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"pro-con/backend"
	"pro-con/card"
)

// frameCol は画面に描かれた枠の左辺の表示桁 (左上の角 ┏ で測る。滑走中のカードの行には縦線を描かないため)。見つからなければ -1。
func frameCol(m *Model) int {
	for _, l := range strings.Split(ansi.Strip(m.render()), "\n") {
		if i := strings.Index(l, "┏"); i >= 0 {
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

// レーンを跨いで滑る途中でも、枠はカードの中身の桁に描かない (縦線が中身を掃くと、カードが一瞬消えて見える)。
// 全角の混ざったカード (W1 の「?質問」) を通るので、全角を途中で切って空白に化かす形も捕まえる。
func TestCursorGlideKeepsCardContent(t *testing.T) {
	m, clk := cursorModel(t)
	press(m, "l")
	w := m.colWidth()
	drawn := 0
	for step := range 6 {
		want := strings.Split(ansi.Strip(strings.Join(m.boardLines(), "\n")), "\n")
		got := strings.Split(ansi.Strip(strings.Join(m.overlayCursor(m.boardLines()), "\n")), "\n")
		for r := range want {
			if isGapRow(r) || r == 0 || r == len(want)-1 {
				continue
			}
			for col := range len(card.Columns) {
				x := col*(w+len(colSep)) + 1 // レーンの中身 (外枠の内側)
				g, wn := ansi.Cut(got[r], x, x+w-2), ansi.Cut(want[r], x, x+w-2)
				if g != wn {
					t.Fatalf("滑走 %d コマ目、行 %d・レーン %d の中身が枠で書き換わった:\n got %q\nwant %q", step, r, col+1, g, wn)
				}
			}
		}
		if strings.Contains(strings.Join(got, "\n"), "━") {
			drawn++
		}
		clk.t = clk.t.Add(cursorDuration / 6)
	}
	if drawn == 0 {
		t.Fatal("前提: 滑走中に枠が一度も描かれていない (何も検査していない)")
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

// unframe は画面から選択の枠を取り除いた形 (枠の縦線は列の罫線に、横線と角は空白と罫線に戻す)。
// 枠がカードの文字を消していなければ、枠を出さないときの画面と一致する。
func unframe(s string) string {
	return strings.NewReplacer("┃", "│", "━", " ", "┏", "│", "┓", "│", "┗", "│", "┛", "│").Replace(s)
}

// 上下に滑っている途中でも、枠がカードの行を消さない (横線でカードの行を丸ごと消すと、動かすたびにカードが一瞬消えて見える)。
func TestCursorGlideNeverHidesCards(t *testing.T) {
	be := newSpy()
	be.snap.Cards = append(be.snap.Cards,
		card.Card{ID: "R2", Title: "二枚目", State: card.Running, Session: "s-r2", Since: be.snap.Now.Add(1)},
		card.Card{ID: "R3", Title: "三枚目", State: card.Running, Session: "s-r3", Since: be.snap.Now.Add(2)})
	clk := &clock{t: be.snap.Now}
	m := New(be, nil)
	m.now = clk.now
	m.width, m.height = 120, 30
	m.selected = "R1"
	m.Update(frameMsg{})
	press(m, "j")
	for _, f := range []int{1, 2, 3, 4, 5} { // 所要のあいだの何コマか
		clk.t = be.snap.Now.Add(cursorDuration * time.Duration(f) / 6)
		lines := strings.Split(ansi.Strip(m.render()), "\n")
		valid := m.cursor.valid
		m.cursor.valid = false
		bare := strings.Split(ansi.Strip(m.render()), "\n")
		m.cursor.valid = valid
		for i := range bare {
			// 枠の横線は空き行 (空白) にしか描かないので、戻すと罫線の違いだけになる。文字の行は完全に一致するはず
			if got, want := unframe(lines[i]), unframe(bare[i]); strings.TrimSpace(strings.Trim(got, "│ ")) != strings.TrimSpace(strings.Trim(want, "│ ")) &&
				strings.ContainsAny(want, "0123456789RW枚") {
				t.Fatalf("コマ %d/6 で %d 行目のカードが枠に消された:\n枠あり %q\n枠なし %q", f, i, lines[i], bare[i])
			}
		}
	}
}

// レーンを移っても、見出しの名前の位置は変わらない (▶ の有無で名前が前後に動かない)。
func TestLaneLabelDoesNotShift(t *testing.T) {
	m, _ := cursorModel(t)
	col := func() int {
		head := strings.Split(ansi.Strip(m.render()), "\n")[headerRows]
		i := strings.Index(head, "作業中")
		if i < 0 {
			t.Fatalf("見出しに 作業中 が無い: %q", head)
		}
		return ansi.StringWidth(head[:i])
	}
	before := col() // 作業中のレーンに居る (▶ 付き)
	press(m, "l")
	if after := col(); after != before {
		t.Fatalf("レーンを移ったら 作業中 の見出しが %d → %d 桁に動いた", before, after)
	}
}
