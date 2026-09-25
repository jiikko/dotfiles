package ui

// 選択中のカードを囲む枠 (Excel のセルのカーソル)。選択が移ると、枠が今の位置から行き先まで画面上を滑る (レーンを跨いでも)。
// 枠はカードの上下の空き行 (view.go の cardGap) に描くので、隣のカードを隠さない。

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"tuikit/anim"
)

// 選択の枠の文字と色 (issue 472: 赤い二重線。ユーザーの指定)。赤は docs/theme-colors.md の 196 (sync の枠と同じ番号。
// 選んでいるレーンの枠の現在地の色 202 (lanefade.go) とは分けてある)。テストもこの定数で枠を探す。
const (
	frameColor = 196
	frameTL    = "╔"
	frameTR    = "╗"
	frameBL    = "╚"
	frameBR    = "╝"
	frameH     = "═"
	frameV     = "║"
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
	border := fg(frameColor) + sgrBold
	top, bottom := row-cardGap, row+cardLines
	// 滑っている途中も枠を丸ごと描く (2026-09-25 にユーザーが見本 tmp/pro-con-cursor-sample.py の B を選んだ。空き行と外枠の上だけに描くと、途中で枠がほぼ消えてちらつく)。
	// ただしカードの字の上では線で置き換えず、字を残したまま色だけ変える (同じ日に見本の D。置き換えると、線が字を跨ぐ一瞬その字が消えて見える):
	// 横の辺は赤の上線 (上辺) / 下線 (下辺)、縦の辺は赤の背景。止まっているときは枠が空き行と外枠 (空白と罫線) の上なので、ふつうの二重線になる
	for r := top; r <= bottom; r++ {
		if r < 0 || r >= len(board) {
			continue
		}
		switch r {
		case top:
			board[r] = softEdge(board[r], left, w, frameTL, frameH, frameTR, border, border+sgrOverline)
		case bottom:
			board[r] = softEdge(board[r], left, w, frameBL, frameH, frameBR, border, border+sgrUnderline)
		default:
			for _, x := range []int{left, left + w - 1} {
				board[r] = softSide(board[r], x, border, sgrFrameBg)
			}
		}
	}
	return board
}

// 枠が字の上に乗ったときの色 (上線 = SGR 53。端末によっては出ない)。縦の辺は赤の背景に白の字。
const (
	sgrOverline = "\x1b[53m"
	sgrFrameBg  = "\x1b[48;5;196m\x1b[38;5;231m"
)

// frameCell は行の表示の 1 桁。全角の字は前半 (start) と後半の 2 桁に同じ字を入れる。
type frameCell struct {
	r     string
	start bool
}

// cellsOf は行の字を表示の桁ごとに並べる (色の指定は落とす)。幅 0 の字 (結合文字) は前の字に付ける。
func cellsOf(line string) []frameCell {
	var cs []frameCell
	for _, ru := range ansi.Strip(line) {
		s := string(ru)
		switch w := ansi.StringWidth(s); w {
		case 0:
			if n := len(cs); n > 0 {
				cs[n-1].r += s
			}
		case 1:
			cs = append(cs, frameCell{s, true})
		default:
			cs = append(cs, frameCell{s, true}, frameCell{s, false})
		}
	}
	return cs
}

// underFrame は枠の線を置いてよい (空白か罫線) か。カードの字なら偽 (色だけ変える)。
func underFrame(cs []frameCell, x int) bool {
	if x < 0 || x >= len(cs) {
		return true
	}
	r := []rune(cs[x].r)
	return len(r) == 0 || r[0] == ' ' || (r[0] >= 0x2500 && r[0] <= 0x257F)
}

// softEdge は横の辺を left から w 桁に描く。空白と罫線の上は線 (l / mid / r)、字の上は字を残して style で色を変える。
// 全角の字の半分だけが辺にかかるときは、その半分を style の空白にする (字を 2 つに割らない)。
func softEdge(line string, left, w int, l, mid, r, border, style string) string {
	cs := cellsOf(line)
	var b strings.Builder
	for x := left; x < left+w; x++ {
		i := x - left
		if underFrame(cs, x) {
			ch := mid
			switch i {
			case 0:
				ch = l
			case w - 1:
				ch = r
			}
			b.WriteString(border + ch + sgrReset)
			continue
		}
		c := cs[x]
		cw := ansi.StringWidth(c.r)
		switch {
		case !c.start && x != left: // 全角の後半 (前半で書いた)
		case !c.start || x+cw > left+w: // 前半が辺の外 / 後半が辺の外
			b.WriteString(style + " " + sgrReset)
		default:
			b.WriteString(style + c.r + sgrReset)
		}
	}
	return splice(line, left, w, b.String())
}

// softSide は縦の辺を x の 1 桁に描く。空白と罫線の上は線、字の上は字を残して背景を赤にする (全角なら字の 2 桁ごと)。
func softSide(line string, x int, border, bgStyle string) string {
	cs := cellsOf(line)
	if underFrame(cs, x) {
		return splice(line, x, 1, border+frameV)
	}
	c, at := cs[x], x
	if !c.start {
		at = x - 1
	}
	return splice(line, at, ansi.StringWidth(c.r), bgStyle+c.r)
}
