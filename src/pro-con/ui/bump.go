package ui

// 選択が端でぶつかったときの演出 (issue 436)。それ以上動けない向きへ 1 枚 / 1 列だけ動くキーを押したら、
// 選択中のカードのレーンごと、押した向きへ小さく揺らす (上下は 1 行、左右は 2 桁)。見た目は見本 C (1000 ms) で決めた。
// 描き方は、ボードを組んで枠と移動中のカードを重ねた後に、そのレーンの帯 (桁の範囲) を丸ごとずらすだけ。
// 帯には選択の枠も入っているので枠も一緒に揺れ、cursor.go の描き方には手を入れない。

import (
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"tuikit/anim"
)

const bumpDuration = 1000 * time.Millisecond

// bump はレーン col が (dx, dy) の向き (どちらかが ±1) へぶつかった演出。start がゼロなら揺れていない。
type bump struct {
	col, dx, dy int
	start       time.Time
}

// bumpRepeatGuard は連打を抑える間隔。同じレーン・同じ向きの揺れを始めてからこの間の押下では始め直さない
// (始め直すと、押すたびに揺れが頭から出て 1 回ぶんも見えない)。2026-09-25 のユーザーの指定 (500ms)。
const bumpRepeatGuard = 500 * time.Millisecond

// startBump は押した向きへの揺れを始める。同じレーン・同じ向きで、前の揺れを始めてから bumpRepeatGuard 以内の押下 (連打) は無視する。
// 向きが違う・別のレーンでぶつかったときは、すぐ新しく揺らす。
func (m *Model) startBump(dx, dy int) tea.Cmd {
	if !m.bump.start.IsZero() && m.now().Sub(m.bump.start) < bumpRepeatGuard && m.bump.col == m.col && m.bump.dx == dx && m.bump.dy == dy {
		return nil // 今の揺れの tick は回っている
	}
	m.bump = bump{col: m.col, dx: dx, dy: dy, start: m.now()}
	return m.startFrames()
}

func (m *Model) bumping(now time.Time) bool {
	return !m.bump.start.IsZero() && anim.Elapsed(m.bump.start, now, bumpDuration) < 1
}

// bumpOffset は 0..1 の時間で、壁の方への出っ張り (-1..1)。最初の 15% で出きって、減衰しながら 2.5 回ほど跳ね返る。
func bumpOffset(t float64) float64 {
	if t >= 1 {
		return 0
	}
	if t < 0.15 {
		return t / 0.15
	}
	u := (t - 0.15) / 0.85
	return math.Cos(u*math.Pi*2.5) * math.Exp(-4.5*u)
}

// bumpShift は今のずれ (桁, 行)。見本と同じく偶数丸め (0.5 は 0 に寄せる)。
func (m *Model) bumpShift(now time.Time) (int, int) {
	if !m.bumping(now) {
		return 0, 0
	}
	off := bumpOffset(anim.Elapsed(m.bump.start, now, bumpDuration))
	return int(math.RoundToEven(off * 2 * float64(m.bump.dx))), int(math.RoundToEven(off * float64(m.bump.dy)))
}

// overlayBump は揺れているレーンの帯をずらす。はみ出した分は切る (左右の端の外・ボードの上端の外)。
// 下へずれるときは、ボードが rows 行より短ければ 1 行伸ばして下枠を残す (rows を超えて伸ばすと画面が 1 行増える。
// 画面が低いと shownCards の下限 1 枚でボードの下の空き行が無くなるので、そのときは下枠を切る)。
func (m *Model) overlayBump(board []string, rows int) []string {
	dx, dy := m.bumpShift(m.now())
	if dx == 0 && dy == 0 {
		return board
	}
	w := m.colWidth()
	x0 := m.bump.col * (w + len(colSep))
	total := 0
	for _, l := range board {
		total = max(total, ansi.StringWidth(l))
	}
	if dy > 0 && len(board) < rows {
		board = append(board, strings.Repeat(" ", total))
	}
	band := make([]string, len(board))
	for r, l := range board {
		band[r] = fit(ansi.Cut(l, x0, x0+w), w)
		board[r] = splice(l, x0, w, strings.Repeat(" ", w))
	}
	for r := range board {
		src := r - dy
		if src < 0 || src >= len(band) {
			continue
		}
		s, x := band[src], x0+dx
		if x < 0 {
			s, x = ansi.Cut(s, -x, w), 0
		}
		if x+w > total {
			s = ansi.Cut(s, 0, total-x)
		}
		board[r] = splice(board[r], x, ansi.StringWidth(s), s)
	}
	return board
}
