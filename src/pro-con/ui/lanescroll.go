package ui

import (
	"slices"

	"tuikit/layout"

	"pro-con/card"
)

// レーンのスクロール (issue 499)。入り切らないレーンは「何枚目から見せるか」(先頭) を持ち、選択が見える所まで追って動かす。
// 入り切らないときだけ、レーンの右端に引き出しと同じスクロールバー (tuikit/layout.Scrollbar) を出す。以前の「… 他 N 枚」の行は置き換えた
// (バーが残りの量と位置を見せる)。
// 先頭はレーンごとに持つ (選択の無いレーンも、最後に見ていた所のまま描く)。単位はカード 1 枚で、選択が見える範囲を出たぶんだけ動かす。

// laneTop はレーン col の先頭が何枚目か。n はレーンに並ぶ枚数 (laneItems)。画面の高さや枚数が変わっても末尾を越えないよう、読むたびに寄せる。
func (m *Model) laneTop(col, n int) int {
	return max(0, min(m.tops[col], n-m.shownCards()))
}

// laneOrder はレーン col に並ぶものの順。値はレーンのカード (枚数 cards) の添字で、-1 は移動元に残す点線の枠 (motion.go)。
// 描く側 (columnCells) と位置を数える側 (slotRow / followSelection) が同じ並びを使う (点線の枠の手前のカードと後ろのカードで段が 1 つずれる)。
// 枠は移動元の段の小さい順に差し込む (m.moves は map なので、順を決めないと呼ぶたびに並びが変わる)。
func (m *Model) laneOrder(col, cards int) []int {
	items := make([]int, cards)
	for i := range items {
		items[i] = i
	}
	var ghosts []int
	for _, mv := range m.moves {
		if mv.from.col == col {
			ghosts = append(ghosts, mv.from.row)
		}
	}
	slices.Sort(ghosts)
	for _, r := range ghosts {
		items = slices.Insert(items, min(r, len(items)), -1)
	}
	return items
}

// laneItems はレーン col に並ぶ枚数 (len(laneOrder) と同じ。並びを組まずに数える)。
func (m *Model) laneItems(col, cards int) int {
	n := cards
	for _, mv := range m.moves {
		if mv.from.col == col {
			n++
		}
	}
	return n
}

// itemRow はレーン col の row 枚目のカードが並びの何段目か (点線の枠も数える)。そのカードが無ければ row のまま
// (移動を始めたカードの移動元の slot: 抜けたカードの段は、後ろのカードが詰めずに待つので row のままで合う)。
func (m *Model) itemRow(col, cards, row int) int {
	if i := slices.Index(m.laneOrder(col, cards), row); i >= 0 {
		return i
	}
	return row
}

// slotRow は slot の見えている段 (レーンの見えている先頭から何段目)。見える範囲の外なら 0 未満か shownCards 以上になる。
func (m *Model) slotRow(s slot, lanes [][]*card.Card) int {
	n := len(lanes[s.col])
	return m.itemRow(s.col, n, s.row) - m.laneTop(s.col, m.laneItems(s.col, n))
}

// cardWidth は枚数 n のレーンでカードを組む幅。スクロールバーを出すレーンはその分を先に引く (layout.ScrollbarWidth の注意)。
func (m *Model) cardWidth(n int) int {
	inner := m.colWidth() - 2
	if n > m.shownCards() {
		return inner - layout.ScrollbarWidth
	}
	return inner
}

// followSelection は選択中のカードが見えるようにレーンの先頭を動かす。Update の出口で毎回呼ぶ
// (キー・カードの移動・画面の大きさの変化を同じ経路で拾う。枠を滑らせる trackCursor より先に呼ぶ)。
func (m *Model) followSelection() {
	lanes := m.lanes()
	col, row, ok := m.position()
	if !ok {
		return
	}
	n := len(lanes[col])
	top, item := m.laneTop(col, m.laneItems(col, n)), m.itemRow(col, n, row)
	top = max(min(top, item), item-m.shownCards()+1)
	if m.tops == nil {
		m.tops = map[int]int{}
	}
	m.tops[col] = top
}
