package filer

import (
	"strconv"
	"strings"

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
}

func newCanvas(w, h int) *canvas {
	c := &canvas{w: w, h: h, cells: make([]cell, w*h)}
	for i := range c.cells {
		c.cells[i] = cell{s: " ", bg: cBg, fg: cText}
	}
	return c
}

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
		if p := c.at(x, y); p != nil && x+w <= c.w {
			p.s, p.fg, p.bold, p.cont, p.ul, p.rev = cl, fg, bold, false, false, false
			if w == 2 {
				if q := c.at(x+1, y); q != nil {
					q.s, q.cont = "", true
				}
			}
		}
		x += w
	}
	return x
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
		b = strconv.AppendInt(b, int64(v+0.5), 10)
	}
	return b
}

// lines は格子を行ごとの文字列 (24bit の SGR つき) にする。
func (c *canvas) lines() []string {
	out := make([]string, c.h)
	var b []byte
	for y := range c.h {
		b = b[:0]
		var last cell
		first := true
		for x := range c.w {
			p := c.cells[y*c.w+x]
			if p.cont {
				continue
			}
			if first || p.fg != last.fg || p.bg != last.bg || p.bold != last.bold || p.ul != last.ul || p.rev != last.rev {
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
				last, first = p, false
			}
			b = append(b, p.s...)
		}
		b = append(b, "\x1b[0m"...)
		out[y] = string(b)
	}
	return out
}

// plain は格子の文字だけを返す (テストと撮影の照合用)。
func (c *canvas) plain() []string {
	out := make([]string, c.h)
	for y := range c.h {
		var b strings.Builder
		for x := range c.w {
			p := c.cells[y*c.w+x]
			if !p.cont {
				b.WriteString(p.s)
			}
		}
		out[y] = b.String()
	}
	return out
}
