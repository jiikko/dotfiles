package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"tuikit/anim"

	"pro-con/card"
)

// カードが列 (フェーズ) を移るときの演出。いきなり消えて別の列に現れると追えないので、
// 元の位置から移動先の位置まで画面上を滑らせる。
//
// - 元の場所には点線の枠 (ghost)、移動先の場所は空けて待つ (着地前に他のカードが詰まってずれないように)
// - 移動中のカードは地の色 (カード固有) を変えず、枠も付けない (2026-09-24 の指示。以前は行き先の状態の色の枠を付けていた)
// - 所要は 800ms、終わり際に減速 (EaseOutCubic)。memory の好み「600ms〜1s・主役を高コントラスト」に合わせた
// - 演出中だけ frameInterval で再描画し、動くものが無ければ止める (アイドルで CPU を使わない)

const frameInterval = 33 * time.Millisecond

// animDuration は演出の所要 (2026-09-24 に 800ms で合意)。
func animDuration() time.Duration { return 800 * time.Millisecond }

// slot はカンバンの中の位置 (列と、列の中の何枚目か)。
type slot struct{ col, row int }

type move struct {
	c        card.Card // 移動先の状態のカード (描画に使う)
	from, to slot
	// fromX/fromY は開始位置 (画面の桁と行の相対値)。途中でもう一度動いたときは今いる位置から向かい直す
	fromX, fromY float64
	start        time.Time
}

type frameMsg struct{}

func frame() tea.Cmd { return tea.Tick(frameInterval, func(time.Time) tea.Msg { return frameMsg{} }) }

// slots は今のタブで、各カードがどこに居るか。
func (m *Model) slots() map[string]slot {
	out := map[string]slot{}
	for i, cs := range m.columns() {
		for j, c := range cs {
			out[c.ID] = slot{i, j}
		}
	}
	return out
}

// trackMoves は前の配置と比べて列が変わったカードの演出を始める。
// 呼ぶのは Snapshot を取り替えた直後。タブの切り替えは「カードが動いた」ではないので呼ばずに resetSlots する。
func (m *Model) trackMoves() tea.Cmd {
	now := m.now()
	cur := m.slots()
	byID := map[string]card.Card{}
	for _, c := range m.visible() {
		byID[c.ID] = c
	}
	for id, to := range cur {
		from, ok := m.prevSlots[id]
		if !ok || from.col == to.col {
			continue
		}
		mv := &move{c: byID[id], from: from, to: to, start: now}
		mv.fromX, mv.fromY = m.slotXY(from)
		if old, ok := m.moves[id]; ok { // 着地前にまた動いた: 今いる位置から向かい直す
			mv.fromX, mv.fromY = old.pos(m, now)
			mv.from = old.from
		}
		m.moves[id] = mv
	}
	m.prevSlots = cur
	m.pruneMoves(now)
	return m.startFrames()
}

// startFrames は動くもの (カードの移動・板の開閉) があれば frame の tick を回し始める (二重には回さない)。
func (m *Model) startFrames() tea.Cmd {
	if !m.animating() || m.framing {
		return nil
	}
	m.framing = true
	return frame()
}

func (m *Model) animating() bool {
	return len(m.moves) > 0 || m.drawer.Animating(m.now()) || m.set.anim.Animating(m.now()) || m.pager.Animating() || m.cursorGliding(m.now()) || m.laneFading(m.now()) || m.bumping(m.now()) || m.toasts.Animating()
}

func (m *Model) resetSlots() {
	m.prevSlots = m.slots()
	m.moves = map[string]*move{}
	m.cursor.valid = false // 配置が丸ごと変わった (タブの切り替え等): 枠は滑らせずに置き直す
	m.lane.shown = -1      // レーンの色も移さずに点け直す
	m.bump = bump{}        // 揺れていたレーンは別の配置のもの
}

func (m *Model) pruneMoves(now time.Time) {
	d := animDuration()
	for id, mv := range m.moves {
		if anim.Elapsed(mv.start, now, d) >= 1 {
			delete(m.moves, id)
		}
	}
}

// onFrame は演出の 1 コマ。動くものが残っていれば次のコマを予約し、無ければ止める。
func (m *Model) onFrame() tea.Cmd {
	m.pruneMoves(m.now())
	m.set.anim.Settle(m.now())
	m.settleDrawer(m.now())
	m.pager.Advance()
	var hold tea.Cmd
	if m.toasts.Animating() {
		hold = toastTimers(m.toasts.Advance()) // 滑り込み終えた toast の「静止の後に引っ込む」合図
	}
	if !m.animating() {
		m.framing = false
		return hold
	}
	return tea.Batch(frame(), hold)
}

// slotXY は slot の画面上の位置 (ボードの左上からの桁と行)。枠の内側の左上。
func (m *Model) slotXY(s slot) (float64, float64) {
	w := m.colWidth()
	return float64(s.col*(w+len(colSep)) + 1), float64(1 + cardGap + s.row*perCardLines)
}

func (mv *move) pos(m *Model, now time.Time) (float64, float64) {
	p := anim.EaseOutCubic(anim.Elapsed(mv.start, now, animDuration()))
	tx, ty := m.slotXY(mv.to)
	return mv.fromX + (tx-mv.fromX)*p, mv.fromY + (ty-mv.fromY)*p
}

// overlayMoves は移動中のカードをボードの行の上に重ねる。レーンの中と同じ幅・同じ見た目のカードを今の位置に置くだけ。
func (m *Model) overlayMoves(board []string) []string {
	if len(m.moves) == 0 {
		return board
	}
	now := m.now()
	inner := m.colWidth() - 2
	for _, mv := range m.moves {
		x, y := mv.pos(m, now)
		col, row := int(x+0.5), int(y+0.5)
		for i, l := range m.cardCell(mv.c, inner) {
			if r := row + i; r >= 0 && r < len(board) {
				board[r] = splice(board[r], col, inner, l)
			}
		}
	}
	return board
}

// splice は line の表示桁 x から幅 w を s で置き換える。s の前後で SGR を戻す (移動中のカードの色を周りへ漏らさない)。
func splice(line string, x, w int, s string) string {
	total := ansi.StringWidth(line)
	if x < 0 || x >= total {
		return line
	}
	left := fit(ansi.Cut(line, 0, x), x)
	right := ""
	if x+w < total {
		right = ansi.Cut(line, x+w, total)
	}
	return left + sgrReset + s + sgrReset + right
}

// ghostLines は元の場所に一瞬残す点線の枠 (cardLines 行)。
func ghostLines(w int) []string {
	out := []string{fg(240) + "┌" + strings.Repeat("┄", max(0, w-2)) + "┐" + sgrReset}
	for range cardLines - 2 {
		out = append(out, fg(240)+"┆"+strings.Repeat(" ", max(0, w-2))+"┆"+sgrReset)
	}
	return append(out, fg(240)+"└"+strings.Repeat("┄", max(0, w-2))+"┘"+sgrReset)
}
