package ui

// 選択中のカードを囲む枠 (Excel のセルのカーソル)。選択が移ると、枠が今の位置から行き先まで画面上を滑る (レーンを跨いでも)。
// 枠はカードの上下の空き行 (view.go の cardGap) に描くので、隣のカードを隠さない。

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"tuikit/anim"
)

// cursorDuration はカーソルの移動の所要。押すたびに動くので、カードが列を移る演出 (800ms) より短い。
const cursorDuration = 180 * time.Millisecond

// cursorGlide は枠の位置。start がゼロなら滑っていない (to に居る)。
type cursorGlide struct {
	valid        bool
	to           slot
	fromX, fromY float64
	start        time.Time
}

// cursorTarget は枠を置く場所。カードが列を移っている最中は出さない (移動中のカードには枠を付けない)。
func (m *Model) cursorTarget() (slot, bool) {
	if m.picker.open {
		return slot{}, false
	}
	if _, moving := m.moves[m.selected]; moving {
		return slot{}, false
	}
	col, row, ok := m.position()
	if !ok {
		return slot{}, false
	}
	return slot{col, row}, true
}

// trackCursor は選択の移動を見て枠を滑らせ始める。Update の出口の 1 か所から呼ぶ (キーでもカードの移動でも同じ経路を通す)。
// 途中でまた動いたら、今描いている位置から向かい直す。
func (m *Model) trackCursor() tea.Cmd {
	to, ok := m.cursorTarget()
	if !ok {
		m.cursor.valid = false
		return nil
	}
	if !m.cursor.valid { // 初めて置く / 置き直す (タブの切り替え等): 滑らせない
		m.cursor = cursorGlide{valid: true, to: to}
		return nil
	}
	if to == m.cursor.to {
		return nil
	}
	now := m.now()
	x, y := m.cursorPos(now)
	m.cursor = cursorGlide{valid: true, to: to, fromX: x, fromY: y, start: now}
	return m.startFrames()
}

// cursorPos は枠の今の位置 (slotXY と同じ座標)。
func (m *Model) cursorPos(now time.Time) (float64, float64) {
	tx, ty := m.slotXY(m.cursor.to)
	if m.cursor.start.IsZero() {
		return tx, ty
	}
	p := anim.EaseOutCubic(anim.Elapsed(m.cursor.start, now, cursorDuration))
	return m.cursor.fromX + (tx-m.cursor.fromX)*p, m.cursor.fromY + (ty-m.cursor.fromY)*p
}

func (m *Model) cursorGliding(now time.Time) bool {
	return m.cursor.valid && !m.cursor.start.IsZero() && anim.Elapsed(m.cursor.start, now, cursorDuration) < 1
}

// overlayCursor はボードの行に枠を重ねる。行き先のカードが列の表示に収まっていない (「… 他 N 枚」の先) なら出さない。
func (m *Model) overlayCursor(board []string) []string {
	if !m.cursor.valid {
		return board
	}
	if _, ok := m.cursorTarget(); !ok {
		return board
	}
	shown := m.shownCards()
	if n := len(m.columns()[m.cursor.to.col]); n > shown && m.cursor.to.row >= shown-1 {
		return board
	}
	x, y := m.cursorPos(m.now())
	left, row := int(x+0.5)-1, int(y+0.5) // slotXY は枠の内側の左上。枠は列の外枠に重ねる
	w := m.colWidth()
	border := fg(202) + sgrBold
	top, bottom := row-cardGap, row+cardLines
	for r := top; r <= bottom; r++ {
		if r < 0 || r >= len(board) {
			continue
		}
		// 上辺・下辺は空き行に乗ったときだけ描く。滑っている途中でカードの行にかかったら縦線だけにする
		// (横線でカードの行を丸ごと消すと、上下に動かすたびにカードが一瞬消えて見える。2026-09-24 の報告)
		edge := isGapRow(r) || r == len(board)-1
		switch {
		case r == top && edge:
			board[r] = splice(board[r], left, w, border+"┏"+strings.Repeat("━", max(w-2, 0))+"┓")
		case r == bottom && edge:
			board[r] = splice(board[r], left, w, border+"┗"+strings.Repeat("━", max(w-2, 0))+"┛")
		default:
			board[r] = splice(board[r], left, 1, border+"┃")
			board[r] = splice(board[r], left+w-1, 1, border+"┃")
		}
	}
	return board
}

// isGapRow はボードの行 r (0 = 列の見出し) がカードの間の空き行か。
func isGapRow(r int) bool { return r >= 1 && (r-1)%perCardLines < cardGap }
