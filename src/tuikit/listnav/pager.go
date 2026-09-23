package listnav

import (
	"tuikit/anim"
	"tuikit/layout"
)

// Pager は本文 (カーソルの無いスクロール) の offset。zero value = 先頭。
//
// 半ページ移動だけを滑らせる (anim.ScrollGlide)。1 行移動は距離 1 行で滑らせる意味が無く、
// 端へのジャンプ (Top / Bottom) は距離が不定なので即時のまま。
type Pager struct {
	Offset int // 論理 offset (0..total-rows)
	glide  anim.ScrollGlide
}

// Scroll は本文の offset に移動 m を適用した結果を返す (0..total-rows に収める)。glide は
// 「この移動を滑らせてよいか」: 半ページ移動だけが true (1 行は距離 1 行で滑らせる意味が無く、
// 端へのジャンプは距離が不定なので即時のまま)。
//
// Pager を使わず offset と glide を自分で持つ画面は、これを通して計算を 1 箇所に揃える。
func Scroll(m Motion, offset, rows, total int) (next int, glide bool) {
	maxOff := max(total-rows, 0)
	switch m {
	case Down:
		return min(offset+1, maxOff), false
	case Up:
		return max(offset-1, 0), false
	case HalfDown:
		return min(offset+Half(rows), maxOff), true
	case HalfUp:
		return max(offset-Half(rows), 0), true
	case Top:
		return 0, false
	case Bottom:
		return maxOff, false
	case None:
	}
	return offset, false
}

// Move は移動 m を適用する。frames は半ページ移動で滑らせるフレーム数 (0 なら滑らせない)。
// offset が動いたら true。連打中の半ページ移動は積み上げずに即時へ倒す (ScrollGlide.Start)。
// 端へのジャンプは進行中の滑走を捨てる。
func (p *Pager) Move(m Motion, total, rows, frames int) bool {
	if m == None {
		return false
	}
	prev := p.Offset
	next, glide := Scroll(m, p.Offset, rows, total)
	p.Offset = next
	switch {
	case m == Top || m == Bottom:
		p.glide.Stop()
	case glide && frames > 0:
		p.glide.Start(prev, next, frames)
	}
	return next != prev
}

// DrawOffset は描画に使う offset (滑走中は途中位置)。行数が縮んでいても範囲へ収める。
func (p *Pager) DrawOffset(total, rows int) int {
	return layout.ClampOffset(p.glide.Offset(p.Offset), total, rows)
}

// Reset は先頭へ戻す (別の本文へ差し替えたとき。前の位置も滑走も持ち越さない)。
func (p *Pager) Reset() { p.Offset = 0; p.glide.Stop() }

// Animating は滑走中か。
func (p *Pager) Animating() bool { return p.glide.Active() }

// Advance は滑走を 1 フレーム進める (tick のたびに呼ぶ)。
func (p *Pager) Advance() {
	if p.glide.Active() {
		p.glide.Advance(p.Offset)
	}
}

// Stop は滑走を捨てて即時表示へ倒す。
func (p *Pager) Stop() { p.glide.Stop() }
