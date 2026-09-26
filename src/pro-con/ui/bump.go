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

// bumpShift は今のずれ (桁, 行)。見本と同じく偶数丸め (0.5 は 0 に寄せる)。
func (m *Model) bumpShift(now time.Time) (int, int) {
	if !m.bumping(now) {
		return 0, 0
	}
	off := bumpOffset(anim.Elapsed(m.bump.start, now, bumpDuration))
	return int(math.RoundToEven(off * bumpCols * float64(m.bump.dx))), int(math.RoundToEven(off * bumpRows * float64(m.bump.dy)))
}

// overlayBump は揺れているレーンの帯をずらす。screen はヘッダから下端までの全行で、ボードは top 行目から h 行。
// 帯はヘッダや下の群の上にも乗る (ボードの中で切るとヘッダの下に潜って見えた)。画面の外にはみ出した分は切る。
func (m *Model) overlayBump(screen []string, top, h int) []string {
	dx, dy := m.bumpShift(m.now())
	if h == 0 || dx == 0 && dy == 0 {
		return screen
	}
	w := m.colWidth()
	x0 := m.bump.col * (w + len(colSep))
	total := m.width
	band := make([]string, h)
	for r := range h { // ボードの行は先に画面の幅へ揃える (はみ出したボードの右端の「…」も帯と一緒に動く)
		screen[top+r] = fit(screen[top+r], total)
		band[r] = fit(ansi.Cut(screen[top+r], x0, x0+w), w)
	}
	// 1 行につき差し替えは 1 回 (元の帯の位置と移り先を合わせた範囲に、空白と帯を並べて入れる)。行ごとに幅を何度も数え直すと、
	// 揺れのコマの描画の大半をここが使っていた (issue 494)。触るのは揺れの届く行だけ
	blank := func(n int) string { return strings.Repeat(" ", n) }
	for y := max(top+min(0, dy), 0); y < min(top+h+max(0, dy), len(screen)); y++ {
		inBoard := y >= top && y < top+h
		src := y - top - dy
		hasBand := src >= 0 && src < h
		var x, width int
		var s string
		switch {
		case hasBand && inBoard && dx >= 0: // 空けた分の空白 + 帯
			x, width, s = x0, w+dx, blank(dx)+band[src]
		case hasBand && inBoard: // 帯 + 空けた分の空白
			x, width, s = x0+dx, w-dx, band[src]+blank(-dx)
		case hasBand: // ボードの外 (ヘッダ・下の群) へ乗る帯
			x, width, s = x0+dx, w, band[src]
		case inBoard: // 帯が出ていった行
			x, width, s = x0, w, blank(w)
		default:
			continue
		}
		if x < 0 { // 左右の端の外は切る
			s, width, x = ansi.Cut(s, -x, width), width+x, 0
		}
		if x+width > total {
			s, width = ansi.Cut(s, 0, total-x), total-x
		}
		l := screen[y]
		if !inBoard { // ボードの行は上で揃えた
			l = fit(l, total)
		}
		screen[y] = splice(l, x, width, s)
	}
	return screen
}
