package termwidth

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Wrap は s を表示幅 width 以下の行に折る。s の中の改行はそのまま行の区切りにする。
//
//   - 切る位置は書記素クラスタの境界 (Cut と同じ)。各行の幅は Of で width 以下を保証する
//   - 行をまたぐ色は、次の行の頭で SGR を張り直す (DropColumns と同じ)。行ごとに別の板の行へ置いても色が続く
//   - trimSpace なら、折り返しで始まった行の頭の空白を落とす (改行で始まった行の頭は落とさない)
//   - 1 つのクラスタが width より広い (幅 1 に全角) ときは、その行だけ width を超える (割る方が壊れる)
//
// x/ansi の Hardwrap / Wrap を使わない理由: キーキャップ (ASCII + VS16 + U+20E3) を含む行を、x/ansi 自身の幅でも
// 指定幅を超える長さで返し、Wrap はクラスタも割る (issue 590 の実測: 幅 4〜12 で 7 種の入力を折り、キーキャップの 2 種だけが超えた)。
func Wrap(s string, width int, trimSpace bool) []string {
	var out []string
	for _, para := range strings.Split(s, "\n") {
		out = wrapLine(out, para, width, trimSpace)
	}
	return out
}

// WordWrap は Wrap と同じ契約で、なるべく単語の境目 (空白と '-') で折る。単語が width より長ければ単語の途中で折る。
// 境目の決め方は x/ansi の Wordwrap に任せ、幅を超えた行だけ Wrap で切り直す (Wordwrap はキーキャップの幅を少なく数えるので、
// その行は Wrap が単語の途中で切る)。
func WordWrap(s string, width int) []string {
	if width <= 0 {
		return strings.Split(s, "\n")
	}
	var out []string
	for _, l := range strings.Split(ansi.Wordwrap(s, width, ""), "\n") {
		out = wrapLine(out, l, width, false)
	}
	return out
}

// wrapLine は改行を含まない s を折って out へ足す。s を前から 1 回だけ走査する (線形)。
//
//   - ESC 列と制御文字は幅 0 として今の行へそのまま入れる。長さは x/ansi のパーサ (DecodeSequence) に読ませる:
//     Of (= ansi.StringWidth) と同じパーサなので、閉じていない OSC や中間バイト付きの ESC 列でも読み方が食い違わない
//     (自前で読むと、Of と違う長さで切って行が幅を超えた・ESC 列が割れた。issue 590 の敵対的レビュー 2 周目)
//   - 見える字は FirstCluster で書記素ごとに切って測る (幅は Of と同じエンジン)
//   - 色は次の行で張り直さない (x/ansi の Hardwrap と同じ)。張り直しは SGR の引数を解釈して状態を畳まないと、
//     部分的な解除 (ESC[39m 等) で行ごとに積み上がり出力が 2 乗に膨らむ (同レビューの実測: 100 KB → 68 MB)
//
// 🚨 Cut で切って DropColumns で残りを作る形にしない: 残り全体をコピーし直すので 2 乗になり、先頭が幅 0 の字だと
// Cut が何も切らず進まなくなる (無限ループ。同レビュー 1 周目)。この走査は 1 回の反復で必ず 1 byte 以上進む。
func wrapLine(out []string, s string, width int, trimSpace bool) []string {
	if width <= 0 {
		return append(out, s)
	}
	var line strings.Builder
	lineW, wrapped := 0, false // wrapped は今の行が折り返しで始まったか
	var state byte
	for len(s) > 0 {
		// 既知の食い違い (直していない): 閉じていない APC / SOS / PM (ESC _ / ESC ^ / ESC X) は、DecodeSequence が末尾まで
		// 幅 0 の 1 列と読むのに、Of (ansi.StringWidth) は中身の非 ASCII の幅を数える。そういう行は折られず Of が width を超える
		// (敵対的レビュー 3 周目: Wrap("a\x1b^xあb", 1, false) が 1 行で Of=4)。x/ansi の 2 つの関数の不整合で、届くのは
		// 無害化していない壊れた端末出力だけ (消費者の入力は termsafe を通り ESC 列が残らない)。起きても 1 行が幅を超え、
		// Panel の切り詰めが吸収する。無害化を通さない入力を折る消費者が出たら、行を出す前に Of で測り直す形を足す
		if b := s[0]; b < 0x20 || b == 0x7f || (b >= 0x80 && b <= 0x9f) {
			seq, _, n, newState := ansi.DecodeSequence(s, state, nil)
			state = newState
			if n <= 0 || n > len(s) {
				seq, n = s[:1], 1
			}
			line.WriteString(seq)
			s = s[n:]
			continue
		}
		c, cw := FirstCluster(s)
		s = s[len(c):]
		if wrapped && lineW == 0 && trimSpace && c == " " {
			continue // 折り返しで始まった行の頭の空白を落とす
		}
		if lineW > 0 && lineW+cw > width {
			out = append(out, line.String())
			line.Reset()
			lineW, wrapped = 0, true
			if trimSpace && c == " " {
				continue
			}
		}
		line.WriteString(c)
		lineW += cw
	}
	if wrapped && lineW == 0 {
		// 折り返した残りに見える字が無い (空白だけ・ESC 列だけ)。空の行は足さず、ESC 列は直前の行の末尾へ付ける
		// (reset や hyperlink の閉じを捨てると、色やリンクが後ろへ漏れる)
		out[len(out)-1] += line.String()
		return out
	}
	return append(out, line.String())
}
