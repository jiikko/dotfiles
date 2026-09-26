package ui

// 選択が端でぶつかったときの演出 (issue 436)。それ以上動けない向きへ 1 枚 / 1 列だけ動くキーを押したら、
// 選択中のカードのレーンごと、押した向きへ小さく揺らす (上下は 2 行、左右は 2 桁)。見た目は見本 C (1000 ms) で決めた。
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

// bumpCols / bumpRows は出きった所のずれ (左右は桁、上下は行)。端末のマスは縦が横の約 2 倍なので、行で 2 は桁で 2 の倍の距離になる。
// 上下を 1 行にすると丸めた後の値が 0 と 1 の 2 つしかなく、1 行跳ねて戻るだけに見えた (2026-09-26)。
const (
	bumpCols = 2
	bumpRows = 2
)

// bumpShift は今のずれ。dx は桁、dy は行 (押した向きの符号)、eighths は壁の方へもう 1 行ぶん進んだ端数 (0..7、1/8 行単位)。
// 文字は 1 行より細かくずらせないので、端数は帯の先 (壁の側) の 1 行に 1/8 ブロックの帯として描く (overlayBump)。
// 見本と同じく偶数丸め (0.5 は 0 に寄せる)。
func (m *Model) bumpShift(now time.Time) (dx, dy, eighths int) {
	if !m.bumping(now) {
		return 0, 0, 0
	}
	off := bumpOffset(anim.Elapsed(m.bump.start, now, bumpDuration))
	dx = int(math.RoundToEven(off * bumpCols * float64(m.bump.dx)))
	if m.bump.dy == 0 {
		return dx, 0, 0
	}
	p := int(math.RoundToEven(off * bumpRows * 8)) // 壁の方へ何 1/8 行か (跳ね返りでは負)
	n := p / 8
	if p%8 < 0 { // 負は床へ寄せる (帯は常に壁の側に置き、端数は 0..7 にする)
		n--
	}
	return dx, n * m.bump.dy, p - n*8
}

// lowerEighths は下から k/8 を塗るブロック (k = 1..7)。
var lowerEighths = []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇"}

// overlayBump は揺れているレーンの帯をずらす。screen はヘッダから下端までの全行で、ボードは top 行目から h 行。
// 帯はヘッダや下の群の上にも乗る (ボードの中で切るとヘッダの下に潜って見えた)。画面の外にはみ出した分は切る。
func (m *Model) overlayBump(screen []string, top, h int) []string {
	dx, dy, eighths := m.bumpShift(m.now())
	if h == 0 || dx == 0 && dy == 0 && eighths == 0 {
		return screen
	}
	w := m.colWidth()
	x0 := m.bump.col * (w + len(colSep))
	total := m.width
	band := make([]string, h)
	for r := range h {
		l := fit(screen[top+r], total)
		band[r] = fit(ansi.Cut(l, x0, x0+w), w)
		screen[top+r] = splice(l, x0, w, strings.Repeat(" ", w))
	}
	for r, s := range band {
		y, x := top+r+dy, x0+dx
		if y < 0 || y >= len(screen) {
			continue
		}
		if x < 0 {
			s, x = ansi.Cut(s, -x, w), 0
		}
		if x+w > total {
			s = ansi.Cut(s, 0, total-x)
		}
		screen[y] = splice(fit(screen[y], total), x, ansi.StringWidth(s), s)
	}
	if eighths == 0 {
		return screen
	}
	// 端数の帯: 上へぶつかったら帯の上の行に下から、下へぶつかったら帯の下の行に上から塗る (上から塗る 1/8 ブロックは
	// 無いので、下から (8-k)/8 のブロックを反転して描く)。幅は枠の角の内側、色はレーンの枠と同じ
	y, bar := top+dy+h, sgrReverse+fg(m.laneColor(m.bump.col))+strings.Repeat(lowerEighths[7-eighths], w-2)
	if m.bump.dy < 0 {
		y, bar = top+dy-1, fg(m.laneColor(m.bump.col))+strings.Repeat(lowerEighths[eighths-1], w-2)
	}
	if y >= 0 && y < len(screen) {
		screen[y] = splice(fit(screen[y], total), x0+1, w-2, bar)
	}
	return screen
}
