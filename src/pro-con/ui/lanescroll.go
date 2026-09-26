package ui

import (
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

// laneItems はレーン col に並ぶ枚数。cards はレーンのカードの枚数で、移動元に残す点線の枠も数える (columnCells の total と同じ)。
func (m *Model) laneItems(col, cards int) int {
	n := cards
	for _, mv := range m.moves {
		if mv.from.col == col {
			n++
		}
	}
	return n
}

// laneTopOf は今の配置でのレーン col の先頭 (枠と移動中のカードの位置を出す側が使う)。
func (m *Model) laneTopOf(col int, lanes [][]*card.Card) int {
	return m.laneTop(col, m.laneItems(col, len(lanes[col])))
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
	top := m.laneTopOf(col, lanes)
	top = max(min(top, row), row-m.shownCards()+1)
	if m.tops == nil {
		m.tops = map[int]int{}
	}
	m.tops[col] = top
}
