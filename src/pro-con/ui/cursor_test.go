package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"pro-con/backend"
	"pro-con/card"
)

// frameCol は画面に描かれた枠の左辺の表示桁 (左上の角 frameTL で測る。滑走中のカードの行には縦線を描かないため)。見つからなければ -1。
func frameCol(m *Model) int {
	for _, l := range strings.Split(ansi.Strip(m.render()), "\n") {
		if i := strings.Index(l, frameTL); i >= 0 {
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
	if !strings.Contains(out, frameBL) {
		t.Fatalf("枠の下辺が無い:\n%s", out)
	}
}

// 選択の枠はユーザーが指定した赤い二重線 (issue 472)。定数で枠を探す他のテストでは、定数の値そのものが戻っても気づけないので、ここで字面と色を固定する。
func TestCursorFrameIsRedDoubleLine(t *testing.T) {
	m, _ := cursorModel(t)
	raw := m.render()
	for _, want := range []string{"╔", "═", "╗", "║", "╚", "╝"} {
		if !strings.Contains(ansi.Strip(raw), want) {
			t.Fatalf("枠に二重線の %q が無い", want)
		}
	}
	if !strings.Contains(raw, fg(196)+sgrBold+"╔") {
		t.Fatalf("枠の左上が赤 (196) の太字で描かれていない")
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

// 滑る途中のどのコマでも、枠は丸ごと見えている。途中で枠が消えて行き先に現れる「ちらつき」を戻さない (2026-09-25 にユーザーが見本の B を選んだ)。
// 辺が字の上に乗ったコマでは二重線の代わりに上線 / 下線 / 赤の背景で描く (見本の D) ので、どちらかが出ていればよい。レーンを跨ぐ (l) と上下 (j) で見る
func TestCursorGlideKeepsWholeFrame(t *testing.T) {
	for _, key := range []string{"l", "j"} {
		m, clk := glideModel(t, key)
		for step := range 6 {
			raw := strings.Join(m.overlayCursor(m.boardLines()), "\n")
			for _, e := range []struct{ name, line, soft string }{{"上辺", frameH, sgrOverline}, {"下辺", frameBL, sgrUnderline}, {"縦の辺", frameV, sgrFrameBg}} {
				if !strings.Contains(raw, e.line) && !strings.Contains(raw, e.soft) {
					t.Fatalf("%s: 滑走 %d コマ目で枠の%sが見えない", key, step, e.name)
				}
			}
			clk.t = clk.t.Add(cursorDuration / 6)
		}
	}
}

// 滑る途中のどのコマでも、枠はカードの字を消さない (字の上では色だけ変える。見本の D)。各行の字 (空白と罫線以外) が、枠を重ねる前と同じ並びで残る。
func TestCursorGlideKeepsText(t *testing.T) {
	text := func(line string) string {
		var b strings.Builder
		for _, c := range cellsOf(line) {
			if c.start && !underFrame([]frameCell{c}, 0) {
				b.WriteString(c.r)
			}
		}
		return b.String()
	}
	for _, key := range []string{"l", "j"} {
		m, clk := glideModel(t, key)
		for step := range 6 {
			bare := m.boardLines()
			over := m.overlayCursor(m.boardLines())
			for r := range bare {
				if got, want := text(over[r]), text(bare[r]); got != want {
					t.Fatalf("%s: 滑走 %d コマ目、行 %d の字が枠で消えた:\n got %q\nwant %q", key, step, r, got, want)
				}
			}
			clk.t = clk.t.Add(cursorDuration / 6)
		}
	}
}

// glideModel は key で選択を動かし、枠が滑り始めた所の Model を返す (l = レーンを跨ぐ / j = 質問待ちのレーンの W1 から下へ)。
func glideModel(t *testing.T, key string) (*Model, *clock) {
	t.Helper()
	m, clk := cursorModel(t)
	if key == "j" {
		m.selected = "W1"
		m.Update(frameMsg{})
	}
	press(m, key)
	if !m.cursorGliding(clk.t) {
		t.Fatalf("%s: 前提: 枠が滑り始めていない", key)
	}
	return m, clk
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
	if !strings.Contains(out, frameV) {
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
