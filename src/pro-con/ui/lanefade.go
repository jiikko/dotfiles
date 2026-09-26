package ui

// 選んでいるレーンの枠の色の移り変わり。フォーカスが移ると、離れるレーンは現在地の色 (202) から平常の色 (240) へ、
// 入るレーンは逆へ、選択の枠が滑るのと同じ所要で移す (その場で切り替えると色がパッと変わる)。
//
// 🚨 主環境は 256 色 (docs/theme-colors.md の「制約」)。途中の色は RGB で混ぜてから一番近い 256 色に丸めて出し、
// truecolor の SGR は使わない (tmux の現在地の点灯 scripts/tmux_ignite_current.sh も 256 色の中で補間している)。

import (
	"time"

	"tuikit/anim"

	"pro-con/card"
)

const (
	laneOn  = 202 // 現在地のレーンの枠
	laneOff = 240 // それ以外のレーンの枠
)

// laneFade はレーンごとの点灯の度合い (0 = 平常、1 = 現在地) の遷移。from から to へ start から cursorDuration で移る。
type laneFade struct {
	shown    int // 最後に点灯先として扱ったレーン (-1 = まだ無い / 置き直す)
	from, to []float64
	start    time.Time
}

// trackLane はフォーカスの移動を見て色の遷移を始める。Update の出口 (trackCursor と同じ所) から呼ぶ。
// 連打で途中から向きが変わっても、各レーンの今の度合いから向かい直す。
func (m *Model) trackLane() bool {
	n := len(card.Columns)
	if m.lane.shown < 0 || len(m.lane.to) != n { // 初めて / 置き直す (タブの切り替え等): 移さずに点ける
		m.lane = laneFade{shown: m.col, from: litOnly(n, m.col), to: litOnly(n, m.col)}
		return false
	}
	if m.col == m.lane.shown {
		return false
	}
	now := m.now()
	from := make([]float64, n)
	for i := range from {
		from[i] = m.laneLit(i, now)
	}
	m.lane = laneFade{shown: m.col, from: from, to: litOnly(n, m.col), start: now}
	return true
}

func litOnly(n, col int) []float64 {
	out := make([]float64, n)
	if col >= 0 && col < n {
		out[col] = 1
	}
	return out
}

// laneLit はレーン i の今の点灯の度合い。
func (m *Model) laneLit(i int, now time.Time) float64 {
	if i >= len(m.lane.to) {
		return 0
	}
	p := 1.0
	if !m.lane.start.IsZero() {
		p = anim.EaseOutCubic(anim.Elapsed(m.lane.start, now, cursorDuration))
	}
	return m.lane.from[i] + (m.lane.to[i]-m.lane.from[i])*p
}

func (m *Model) laneFading(now time.Time) bool {
	return !m.lane.start.IsZero() && anim.Elapsed(m.lane.start, now, cursorDuration) < 1
}

// laneColor はレーン i の枠の色 (256 色の番号)。
func (m *Model) laneColor(i int) int {
	return mix256(laneOff, laneOn, m.laneLit(i, m.now()))
}

// mix256 は 256 色の a と b を t (0..1) で RGB で混ぜ、一番近い 256 色 (6×6×6 の cube か 24 段の灰) を返す。
func mix256(a, b int, t float64) int { //nolint:unparam // 256 色の汎用の混色 (両端を引数で受け、lanefade_test.go が t=0 / 1 / 0.5 を固定する)
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	ra, ga, ba := rgb256(a)
	rb, gb, bb := rgb256(b)
	lerp := func(x, y int) int { return x + int(float64(y-x)*t+0.5) }
	return nearest256(lerp(ra, rb), lerp(ga, gb), lerp(ba, bb))
}

// cubeLevels は xterm の 6×6×6 cube の各段の値。
var cubeLevels = [6]int{0, 95, 135, 175, 215, 255}

// rgb256 は 256 色の番号 (16 以上。基本 16 色は端末ごとに違うので扱わない) の RGB。
func rgb256(n int) (int, int, int) {
	if n >= 232 {
		v := 8 + (n-232)*10
		return v, v, v
	}
	n -= 16
	return cubeLevels[n/36], cubeLevels[n/6%6], cubeLevels[n%6]
}

// nearest256 は RGB に一番近い 256 色 (cube と灰の段のうち、距離の小さい方)。
func nearest256(r, g, b int) int {
	best, bestD := 16, -1
	consider := func(n int) {
		cr, cg, cb := rgb256(n)
		d := (cr-r)*(cr-r) + (cg-g)*(cg-g) + (cb-b)*(cb-b)
		if bestD < 0 || d < bestD {
			best, bestD = n, d
		}
	}
	for n := 16; n <= 255; n++ {
		consider(n)
	}
	return best
}
