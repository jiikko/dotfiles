package usage

// braille は 1 セルを 2x4 のドットに分ける点描キャンバス (U+2800 ブロック)。円弧・放射線を
// セルより細かい解像度で描くために使う。dial.go 専用の小さな描画器。
//
// ⚠️ ドットの縦横比: 端末セルは概ね 横 1 : 縦 2 なので、それを 横 2 x 縦 4 に割った 1 ドットは
// ほぼ正方形になる。円をドット座標でそのまま描けば端末上でも円に見える (縦横の補正は不要)。
// ⚠️ 幅: braille は termwidth の受理表に載っていて幅 1 が保証されている (termwidth.acceptSymbol)。
// 上に載せる文字は全角も置けるが、2 セル目を「覆われている」印にして格子を保つ (putText)。

import (
	"math"
	"strings"

	"glogx/sgr"
	"glogx/termwidth"
)

// brailleBit は (セル内 x, セル内 y) → ドットのビット。
var brailleBit = [2][4]byte{
	{0x01, 0x02, 0x04, 0x40},
	{0x08, 0x10, 0x20, 0x80},
}

type braille struct {
	cols, rows int
	bits       []byte
	color      []string
	text       []rune // 0 以外ならそのセルを文字で上書きする
	textColor  []string
	textSkip   []bool // 直前のセルの全角文字が覆っているので何も出さない
}

func newBraille(cols, rows int) *braille {
	n := max(cols, 0) * max(rows, 0)
	return &braille{
		cols: max(cols, 0), rows: max(rows, 0),
		bits: make([]byte, n), color: make([]string, n),
		text: make([]rune, n), textColor: make([]string, n),
		textSkip: make([]bool, n),
	}
}

// dot は (x, y) のドットを立てる (座標はドット単位。範囲外は捨てる)。
func (b *braille) dot(x, y float64, col string) {
	ix, iy := int(x+0.5), int(y+0.5)
	if ix < 0 || iy < 0 || ix >= b.cols*2 || iy >= b.rows*4 {
		return
	}
	i := (iy/4)*b.cols + ix/2
	b.bits[i] |= brailleBit[ix%2][iy%4]
	if col != "" {
		b.color[i] = col
	}
}

// arc は中心 (cx, cy)・半径 r の円弧を、12 時を 0 とする時計回りの割合 t0..t1 で描く。
// thick はドット単位の太さ (内側へ伸ばす)、dash は 1 以外にすると破線 (下地の目盛り用)。
func (b *braille) arc(cx, cy, r, t0, t1 float64, col string, thick, dash int) {
	if t1 <= t0 || r <= 0 {
		return
	}
	steps := max(int(2*math.Pi*r*(t1-t0)*1.7), 8)
	for i := 0; i <= steps; i++ {
		if dash > 1 && (i/2)%dash != 0 {
			continue
		}
		t := t0 + (t1-t0)*float64(i)/float64(steps)
		sin, cos := math.Sincos(dialAngle(t))
		for d := range max(thick, 1) {
			rr := r - float64(d)
			b.dot(cx+rr*cos, cy+rr*sin, col)
		}
	}
}

// tick は割合 t の位置に、円周方向へ 2 ドットぶんの太さを持つ目盛りを引く。1 本の ray だけだと
// 斜めの角度でドットが飛び飛びになり、目盛りではなく「散らばった点」に見える。
func (b *braille) tick(cx, cy, r0, r1, t float64, col string) {
	if r1 <= 0 {
		return
	}
	off := 0.5 / (2 * math.Pi * r1) // 円周上で約 0.5 ドットぶんの角度
	b.ray(cx, cy, r0, r1, t-off, col)
	b.ray(cx, cy, r0, r1, t+off, col)
}

// ray は中心から見て割合 t の方向へ、半径 r0..r1 の線分を描く。
func (b *braille) ray(cx, cy, r0, r1, t float64, col string) {
	if r1 < r0 {
		r0, r1 = r1, r0
	}
	sin, cos := math.Sincos(dialAngle(t))
	steps := max(int((r1-r0)*2), 2)
	for i := 0; i <= steps; i++ {
		rr := r0 + (r1-r0)*float64(i)/float64(steps)
		b.dot(cx+rr*cos, cy+rr*sin, col)
	}
}

// dialAngle は「12 時を 0 とする時計回りの割合」をラジアンへ写す (画面座標は y が下向き)。
func dialAngle(t float64) float64 { return -math.Pi/2 + 2*math.Pi*t }

// putText は行 row・桁 col から文字列を点描の上に置く。全角は 2 セルを占め、2 セル目は
// 「覆われている」印を付けて何も出さない (点描の格子と桁がずれないため)。
func (b *braille) putText(row, col int, s, color string) {
	if row < 0 || row >= b.rows {
		return
	}
	x := col
	for _, r := range s {
		w := termwidth.Of(string(r))
		if w <= 0 {
			continue
		}
		if x >= 0 && x < b.cols {
			j := row*b.cols + x
			b.text[j], b.textColor[j], b.textSkip[j] = r, color, false
			if w == 2 && x+1 < b.cols {
				b.textSkip[j+1] = true
				b.text[j+1] = 0
			}
		}
		x += w
	}
}

// lines はキャンバスを rows 行の文字列にする。色は変わり目でだけ SGR を挟む。
func (b *braille) lines(colored bool) []string {
	out := make([]string, 0, b.rows)
	var sb strings.Builder
	for r := range b.rows {
		sb.Reset()
		cur := ""
		for c := range b.cols {
			i := r*b.cols + c
			// 空セルは U+2800 (点の無い braille) ではなく空白にする。U+2800 はフォントに
			// よって薄い点が見え、盤の外側が一面グレーになる (かつ行末が trim できない)。
			if b.textSkip[i] {
				continue // 直前の全角文字が覆っている桁 (何も出さない)
			}
			ch, col := ' ', b.color[i]
			if b.bits[i] != 0 {
				ch = rune(0x2800 + int(b.bits[i]))
			}
			if b.text[i] != 0 {
				ch, col = b.text[i], b.textColor[i]
			}
			if colored && col != cur {
				if cur != "" {
					sb.WriteString(sgr.Reset)
				}
				sb.WriteString(col)
				cur = col
			}
			sb.WriteRune(ch)
		}
		if colored && cur != "" {
			sb.WriteString(sgr.Reset)
		}
		out = append(out, strings.TrimRight(sb.String(), " "))
	}
	return out
}
