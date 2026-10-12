package filer

import (
	"strconv"
	"strings"

	"github.com/jiikko/dotfiles/src/tuikit/sgr"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// render.go は「セルの格子 → 文字列」の純粋な描画層 (I/O をしない。.golangci.yml の render-pure)。

type cell struct {
	s             string // 書記素クラスタ 1 つ。全角の右半分は空で cont
	fg, bg        rgb
	bold, ul, rev bool
	cont          bool
}

type canvas struct {
	w, h  int
	cells []cell
	blank []cell // 地の色の空白で埋めた見本 (reset が 1 回の copy で戻す)
}

func newCanvas(w, h int) *canvas {
	c := &canvas{w: w, h: h, cells: make([]cell, w*h), blank: make([]cell, w*h)}
	for i := range c.blank {
		c.blank[i] = cell{s: " ", bg: cBg, fg: cText}
	}
	c.reset()
	return c
}

// reset は格子を地の色の空白に戻す (毎フレーム作り直さず使い回すため。glogx の TestFrameAllocBudget で
// 作り直しは 1 フレーム 330KB だった)。セルごとに代入せず見本からまとめて写す (issue 724)。
func (c *canvas) reset() { copy(c.cells, c.blank) }

func (c *canvas) at(x, y int) *cell {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return nil
	}
	return &c.cells[y*c.w+x]
}

// put は s を (x, y) から書き、書き終えた次の x を返す。幅は termwidth (この repo の幅の単一の出典)。
// 画面の右端をはみ出す全角は書かない (半分だけ出すと端末の桁がずれる)。
func (c *canvas) put(x, y int, s string, fg rgb, bold bool) int {
	for s != "" {
		cl, w := termwidth.FirstCluster(s)
		s = s[len(cl):]
		if w == 0 {
			continue
		}
		c.putCluster(x, y, cl, w, fg, bold, false)
		x += w
	}
	return x
}

func (c *canvas) putCluster(x, y int, cl string, w int, fg rgb, bold, ul bool) {
	if p := c.at(x, y); p != nil && x+w <= c.w {
		p.s, p.fg, p.bold, p.cont, p.ul, p.rev = cl, fg, bold, false, ul, false
		if w == 2 {
			if q := c.at(x+1, y); q != nil {
				q.s, q.cont = "", true
			}
		}
	}
}

// putSGR は SGR (色・太字・下線) を含む s を (x, y) から最大 maxw 桁書く。fg は既定の文字色 (SGR 0 / 39 で戻る色)。
// 解釈するのは前景色・太字・下線だけで、背景色は捨てる (タイルの地の色を保つ)。m 以外の CSI と孤立した ESC も捨てる。
// 🚨 s の ESC は tuikit/highlight と tuikit/markdown が付けたものだけ (本文は読んだときに termsafe を通している)。
func (c *canvas) putSGR(x, y int, s string, maxw int, fg rgb) {
	cur, bold, ul := fg, false, false
	end := x + maxw
	for s != "" {
		if s[0] == 0x1b {
			j := 1
			if len(s) > 1 && s[1] == '[' {
				for j = 2; j < len(s) && (s[j] < 0x40 || s[j] > 0x7e); j++ {
				}
				if j < len(s) && s[j] == 'm' {
					cur, bold, ul = applySGR(s[2:j], fg, cur, bold, ul)
				}
				j++
			}
			s = s[min(j, len(s)):]
			continue
		}
		cl, w := termwidth.FirstCluster(s)
		s = s[len(cl):]
		if w == 0 {
			continue
		}
		if x+w > end {
			return
		}
		c.putCluster(x, y, cl, w, cur, bold, ul)
		x += w
	}
}

// applySGR は 1 つの SGR の引数 (";" 区切り) を今の状態に当てる。
func applySGR(params string, def, cur rgb, bold, ul bool) (rgb, bool, bool) {
	var ns [8]int
	n := 0
	v, has := 0, false
	flush := func() {
		if n < len(ns) {
			ns[n] = v
			n++
		}
		v, has = 0, false
	}
	for i := range len(params) {
		switch ch := params[i]; {
		case ch >= '0' && ch <= '9':
			v, has = v*10+int(ch-'0'), true
		case ch == ';' || ch == ':':
			flush()
		}
	}
	if has || n == 0 || params[len(params)-1] == ';' {
		flush()
	}
	for i := 0; i < n; i++ {
		switch p := ns[i]; {
		case p == 0:
			cur, bold, ul = def, false, false
		case p == 1:
			bold = true
		case p == 22:
			bold = false
		case p == 4:
			ul = true
		case p == 24:
			ul = false
		case p == 39:
			cur = def
		case p >= 30 && p <= 37:
			cur = xterm256(p - 30)
		case p >= 90 && p <= 97:
			cur = xterm256(p - 90 + 8)
		case (p == 38 || p == 48) && i+2 < n && ns[i+1] == 5:
			if p == 38 {
				cur = xterm256(ns[i+2])
			}
			i += 2
		case (p == 38 || p == 48) && i+4 < n && ns[i+1] == 2:
			if p == 38 {
				cur = rgb{float64(ns[i+2]), float64(ns[i+3]), float64(ns[i+4])}
			}
			i += 4
		}
	}
	return cur, bold, ul
}

// xterm256 は 256 色の番号を rgb にする (範囲の外は黒)。
func xterm256(n int) rgb {
	r, g, b := sgr.XtermRGB(n)
	return rgb{float64(r), float64(g), float64(b)}
}

func (c *canvas) fillBg(x0, y0, x1, y1 int, bg rgb) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			if p := c.at(x, y); p != nil {
				p.bg = bg
			}
		}
	}
}

func (c *canvas) clear(x0, y0, x1, y1 int, bg rgb) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			if p := c.at(x, y); p != nil {
				*p = cell{s: " ", bg: bg, fg: cText}
			}
		}
	}
}

type rect struct{ x, y, w, h int }

// frame は r を浮かぶ板の地 (cPop) で塗り、丸い角の枠を col で描く (キー一覧・設定の板・タイルの共通の外形)。題は呼び出し側が載せる。
func (c *canvas) frame(r rect, col rgb) {
	c.clear(r.x, r.y, r.x+r.w-1, r.y+r.h-1, cPop)
	c.put(r.x, r.y, "╭"+strings.Repeat("─", r.w-2)+"╮", col, false)
	for y := r.y + 1; y < r.y+r.h-1; y++ {
		c.put(r.x, y, "│", col, false)
		c.put(r.x+r.w-1, y, "│", col, false)
	}
	c.put(r.x, r.y+r.h-1, "╰"+strings.Repeat("─", r.w-2)+"╯", col, false)
}

func (r rect) has(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

// dim は keep の外を地の色へ t だけ寄せる (treebeard の dim_backdrop。spec §4.6)。
func (c *canvas) dim(keep rect, t float64) {
	for y := range c.h {
		for x := range c.w {
			if keep.has(x, y) {
				continue
			}
			p := &c.cells[y*c.w+x]
			p.fg, p.bg = mix(p.fg, cBg, t), mix(p.bg, cBg, t)
		}
	}
}

func appendRGB(b []byte, sel string, c rgb) []byte {
	b = append(b, sel...)
	for i, v := range c {
		if i > 0 {
			b = append(b, ';')
		}
		if n := int64(v + 0.5); n >= 0 && n < int64(len(decimals)) {
			b = append(b, decimals[n]...)
		} else {
			b = strconv.AppendInt(b, n, 10) // 0..255 の外 (混ぜた色のはみ出し)。表を引かない
		}
	}
	return b
}

// decimals は 0..255 の 10 進の文字列 (色の成分を毎回 strconv で組まない。issue 724)。
var decimals = func() [256]string {
	var t [256]string
	for i := range t {
		t[i] = strconv.Itoa(i)
	}
	return t
}()

// lines は格子を行ごとの文字列 (24bit の SGR つき) にする。
func (c *canvas) lines() []string {
	out := make([]string, c.h)
	var b []byte
	for y := range c.h {
		b = b[:0]
		var last *cell
		for x := range c.w {
			p := &c.cells[y*c.w+x] // 値で写さない (1 セル 72 バイト。issue 724)
			if p.cont {
				continue
			}
			if last == nil || p.fg != last.fg || p.bg != last.bg || p.bold != last.bold || p.ul != last.ul || p.rev != last.rev {
				b = append(b, "\x1b[0"...)
				if p.bold {
					b = append(b, ";1"...)
				}
				if p.ul {
					b = append(b, ";4"...)
				}
				if p.rev {
					b = append(b, ";7"...)
				}
				b = appendRGB(b, ";38;2;", p.fg)
				b = appendRGB(b, ";48;2;", p.bg)
				b = append(b, 'm')
				last = p
			}
			b = append(b, p.s...)
		}
		b = append(b, sgr.Reset...)
		out[y] = string(b)
	}
	return out
}
