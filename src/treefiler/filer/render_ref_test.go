package filer

import (
	"math/rand/v2"
	"slices"
	"strconv"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/sgr"
)

// linesRef / appendRGBRef は lines と appendRGB の素朴な版 (セルを値で写す・色を毎回 strconv で組む)。今の lines の正解役。
func linesRef(c *canvas) []string {
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
				b = appendRGBRef(b, ";38;2;", p.fg)
				b = appendRGBRef(b, ";48;2;", p.bg)
				b = append(b, 'm')
				last, first = p, false
			}
			b = append(b, p.s...)
		}
		b = append(b, sgr.Reset...)
		out[y] = string(b)
	}
	return out
}

func appendRGBRef(b []byte, sel string, c rgb) []byte {
	b = append(b, sel...)
	for i, v := range c {
		if i > 0 {
			b = append(b, ';')
		}
		b = strconv.AppendInt(b, int64(v+0.5), 10)
	}
	return b
}

// 乱数の格子 (はみ出した色・端の色・全角の右半分・属性) で、今の lines が前の lines と 1 バイトも違わない。
func TestLinesMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewPCG(724, 1))
	comp := []float64{0, 0.49, 0.5, 254.4, 254.5, 255, 255.49, 255.5, 255.6, 256.4, 300, -0.4, -0.6, -1.6, -3, 17.5, 128.25} // -1.6 → -1・255.6 → 256 (表の端の外)
	color := func() rgb {
		if rng.IntN(3) == 0 {
			return rgb{comp[rng.IntN(len(comp))], comp[rng.IntN(len(comp))], comp[rng.IntN(len(comp))]}
		}
		return rgb{rng.Float64() * 255, rng.Float64() * 255, rng.Float64() * 255}
	}
	for range 300 {
		c := newCanvas(1+rng.IntN(40), 1+rng.IntN(6))
		var pal []rgb
		for range 1 + rng.IntN(4) {
			pal = append(pal, color())
		}
		for i := range c.cells {
			p := &c.cells[i]
			if rng.IntN(3) == 0 {
				continue // reset のまま
			}
			p.s = []string{"a", "あ", "", " ", "─", "\x1b"}[rng.IntN(6)]
			p.fg, p.bg = pal[rng.IntN(len(pal))], pal[rng.IntN(len(pal))]
			p.bold, p.ul, p.rev, p.cont = rng.IntN(4) == 0, rng.IntN(5) == 0, rng.IntN(6) == 0, rng.IntN(8) == 0
		}
		if got, want := c.lines(), linesRef(c); !slices.Equal(got, want) {
			t.Fatalf("lines が前と違う:\n got %q\nwant %q", got, want)
		}
	}
}

// 本物の画面 (開閉・移動・タイル・検索・設定の板を混ぜた操作の各フレーム) でも前と同じ文字列を出し、reset は新しい格子と同じ中身に戻す。
func TestViewMatchesReferenceOnRealFrames(t *testing.T) {
	m := newTest(t)
	keys := []string{"l", "j", "j", "space", "l", "k", "enter", "q", "/", "f", "esc", ",", "j", "esc", ".", "C", "h", "?", "x"}
	for i, k := range keys {
		m.HandleKey(k)
		settle(m)
		cv := m.draw()
		if got, want := cv.lines(), linesRef(cv); !slices.Equal(got, want) {
			t.Fatalf("%d 打鍵目 (%s) の画面が前と違う", i, k)
		}
		cv.reset()
		for j, p := range cv.cells { // 見本と比べない (見本が誤っていれば同じ誤りどうしで一致する)
			if p != (cell{s: " ", bg: cBg, fg: cText}) {
				t.Fatalf("%d 打鍵目: reset の後のセル %d = %+v (地の色の空白に戻すこと)", i, j, p)
			}
		}
	}
}
