package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"pro-con/backend"
	"pro-con/card"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

// movingModel は M1 が「分解済み」にある状態から始め、Snapshot 上で「作業中」へ移して tick を 1 回送る。
func movingModel(t *testing.T) (*Model, *spy, *clock) {
	t.Helper()
	be := newSpy()
	be.snap.Cards = append(be.snap.Cards, card.Card{ID: "M1", Title: "動くカード", State: card.Planned, Since: be.snap.Now})
	clk := &clock{t: be.snap.Now}
	m := New(be, nil)
	m.now = clk.now
	m.resetSlots()
	be.snap.Cards[len(be.snap.Cards)-1].State = card.Running
	if _, cmd := m.Update(tickMsg{}); cmd == nil {
		t.Fatal("tick の後にコマンドが返らない")
	}
	return m, be, clk
}

// idColumn は画面の中で id が描かれている回数と、最初に見つかった表示桁を返す。
// 回数は行ではなく出現で数える (移動中のカードと移動先の場所は同じ行に並ぶことがあるので、行で数えると 2 か所を 1 と数える)。
func idColumn(m *Model, id string) (count, col int) {
	col = -1
	for _, l := range strings.Split(ansi.Strip(m.render()), "\n") {
		if i := strings.Index(l, id); i >= 0 && col < 0 {
			col = ansi.StringWidth(l[:i])
		}
		count += strings.Count(l, id)
	}
	return count, col
}

// 列が変わったカードは演出になり、移動中も画面から消えず (ちょうど 1 か所に描かれ)、途中のコマでは
// 元の列と移動先の列のあいだに居る (瞬間移動しない)。
func TestMovedCardGlidesBetweenColumns(t *testing.T) {
	m, _, clk := movingModel(t)
	if _, ok := m.moves["M1"]; !ok {
		t.Fatal("列が変わったのに演出が始まらない")
	}
	fromX, _ := m.slotXY(slot{col: 1})
	toX, _ := m.slotXY(slot{col: 2})
	// 開始の瞬間は移動中のカードが元の列にぴったり重なるので、移動先の場所が空いていなければ 2 か所に見える
	// (途中のコマでは移動中のカードが移動先の場所に被さって隠れるため、ここで見る)
	if n, _ := idColumn(m, "M1"); n != 1 {
		t.Fatalf("開始の瞬間にカードが %d か所に描かれた (移動先の場所を空けて待っていない)", n)
	}
	clk.t = clk.t.Add(animDuration() / 5)
	n, x := idColumn(m, "M1")
	if n != 1 {
		t.Fatalf("移動中のカードは画面にちょうど 1 か所のはず: %d", n)
	}
	if !(float64(x) > fromX && float64(x) < toX) {
		t.Fatalf("途中のコマで元の列 (%.0f) と移動先 (%.0f) のあいだに居ない: %d", fromX, toX, x)
	}
}

// 所要を過ぎたら演出は終わり、frame の tick も止まる (アイドルで再描画し続けない)。カードは移動先に 1 枚だけ残る。
func TestMotionEndsAndFramesStop(t *testing.T) {
	m, _, clk := movingModel(t)
	if _, cmd := m.Update(frameMsg{}); cmd == nil {
		t.Fatal("演出の途中なのに次のコマが予約されない")
	}
	clk.t = clk.t.Add(animDuration())
	if _, cmd := m.Update(frameMsg{}); cmd != nil {
		t.Fatal("演出が終わったのに次のコマが予約された")
	}
	if len(m.moves) != 0 || m.framing {
		t.Fatalf("演出が残っている: moves=%d framing=%v", len(m.moves), m.framing)
	}
	if n, _ := idColumn(m, "M1"); n != 1 {
		t.Fatalf("着地後のカードは 1 か所に描かれるはず: %d", n)
	}
	if col, _, _ := m.positionOf("M1"); card.Columns[col] != card.Running {
		t.Fatalf("着地先が作業中の列ではない: %s", card.Columns[col].Label())
	}
}

// 移動中のカードは、そのカードの無いタブへ切り替えたら描かない (別の repo のカードが画面を横切らない)。
func TestTabSwitchDropsMotionOfHiddenCard(t *testing.T) {
	be := newRepoSpy()
	be.snap.Cards[1].State = card.Planned // O1 (obaket) を分解済みから始める
	clk := &clock{t: be.snap.Now}
	m := New(be, repos("dotfiles", "obaket"))
	m.now = clk.now
	m.resetSlots()
	be.snap.Cards[1].State = card.Running
	m.Update(tickMsg{})
	if _, ok := m.moves["O1"]; !ok {
		t.Fatal("前提: O1 の演出が始まっていない")
	}
	m.Update(keyTab()) // dotfiles タブ (O1 は居ない)
	if n, _ := idColumn(m, "O1"); n != 0 {
		t.Fatalf("別のタブのカードが移動中として描かれた: %d か所", n)
	}
}

// layoutOf は id の (行, 桁) と、列の枠の下端の行 (画面で最後に ╰ が出る行) を返す。
func layoutOf(m *Model, id string) (line, col, bottom int) {
	line, col, bottom = -1, -1, -1
	for i, l := range strings.Split(ansi.Strip(m.render()), "\n") {
		if j := strings.Index(l, id); j >= 0 {
			line, col = i, ansi.StringWidth(l[:j])
		}
		if strings.Contains(l, "╰") {
			bottom = i
		}
	}
	return line, col, bottom
}

// 移動の前・始まり・途中・着地の後で、動いていないカード (R1) の位置と列の枠の下端が変わらない。
// 移ってくる M1 は Snapshot の並びでは R1 より前に居り、移動先の列はいちばん長い列になる
// (列の中を Snapshot 順に並べると M1 が R1 の上に割り込み、枠の高さを枚数に合わせると全部の枠が伸びる)。
func TestOtherCardsStayPutDuringMotion(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	be := &spy{snap: backend.Snapshot{Now: now, Limit: 2, DaemonTick: now, Cards: []card.Card{
		{ID: "M1", Title: "動く", State: card.Planned, Since: now.Add(-5 * time.Minute)},
		{ID: "R1", Title: "動かない", State: card.Running, Since: now.Add(-10 * time.Minute)},
	}}}
	clk := &clock{t: now}
	m := New(be, nil)
	m.now = clk.now
	m.resetSlots()
	m.selected = "R1"
	l0, c0, b0 := layoutOf(m, "R1")
	be.snap.Cards[0].State, be.snap.Cards[0].Since = card.Running, now
	m.Update(tickMsg{})
	for _, at := range []time.Duration{0, animDuration() / 2, animDuration()} {
		clk.t = now.Add(at)
		m.Update(frameMsg{})
		if l, c, b := layoutOf(m, "R1"); l != l0 || c != c0 || b != b0 {
			t.Fatalf("%v の時点で R1 か枠が動いた: 位置 (%d,%d)→(%d,%d) 下端 %d→%d", at, l0, c0, l, c, b0, b)
		}
	}
	if len(m.moves) != 0 {
		t.Fatal("前提: 着地後も演出が残っている")
	}
}

// 移動中のカードには枠を付けない (行き先の状態の見出し「→ …」も出さない)。カードそのものは途中の位置に見えている。
func TestMovingCardHasNoBorder(t *testing.T) {
	m, _, clk := movingModel(t)
	clk.t = clk.t.Add(animDuration() / 2)
	out := ansi.Strip(m.render())
	if strings.Contains(out, "→ ") {
		t.Fatalf("移動中のカードに行き先の見出しの枠が付いている:\n%s", out)
	}
	if n, _ := idColumn(m, "M1"); n == 0 {
		t.Fatalf("移動中のカードが見えていない:\n%s", out)
	}
}
