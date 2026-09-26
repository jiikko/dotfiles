// Package markdown は markdown の本文を width 桁の端末行へ整形する (見出し・箇条書き・表・
// フェンスコードの chroma ハイライト)。glogx の issues viewer の本文と、pro-con の詳細の
// 応答の文が同じ整形器を使う (glogx/issues から移した。dotfiles issue 486)。
package markdown

import (
	"strings"

	"tuikit/highlight"
	"tuikit/sgr"
	"tuikit/termwidth"
)

// 純粋描画層: 意味付きスパン列へ ANSI を塗る。I/O・プロセス起動・非同期はここに置かない
// (depguard の render-pure ルールが機械的に禁止している)。フェンスコードの色は diff の板と同じ tuikit/highlight。

// Render は markdown の本文 (issue 本文・PG の応答の文) を width 桁の端末行へ整形する。
// colored=false なら ANSI を一切付けない (テストと非 TTY 出力のため)。
//
// 第 2 戻り値は各行に対応するソース (.md) の行番号 (0 = 出さない。理由は line.src の doc)。
// 呼び出し側が左の溝に出す。
func Render(src string, width int, colored bool) (out []string, srcLines []int) {
	lines := renderMarkdown(src, width)
	out = make([]string, 0, len(lines))
	srcLines = make([]int, 0, len(lines))
	for _, l := range lines {
		out = append(out, clipToWidth(paintLine(l, colored), width))
		srcLines = append(srcLines, l.src)
	}
	return out, srcLines
}

// clipToWidth は「出力は width 桁を超えない」という Render の契約を、**出口 1 箇所で**
// 無条件に守る (issue 116)。
//
// なぜ必要か: 整形 (renderMarkdown) は箇条書きの記号・番号・表の罫線・入れ子のインデントを
// 先に置いてから残り幅へ本文を詰めるので、**幅がそれらの固定分より狭いと構造の側が溢れる**。
// 実測 2026-08-27: 幅 1 で 80 行・幅 3 で 42 行が溢れていた (幅 20 以上では 0 件。テストが
// {20,40,60,86,120} しか掃いていなかったので 20 未満を一度も見ていなかった)。
//
// 「幅 N 未満は畳む」という下限を決める案は採らなかった: 下限を跨ぐ入力ごとに畳み方を決める
// ことになり、ここで一律に切る方が契約が単純 (glogx 本体も同名の関数で同じことをしており、
// 今日この溢れが表に出ていないのはその下流 clip が吸収していたから)。
//
// 🚨 glogx は Body.Lines のキャッシュ越しに呼ぶので毎フレームは走らない (width か colored が
// 変わったときだけ)。とはいえ ANSI 無しで byte 長が収まる行は最も多いので fast-path は残す。
func clipToWidth(line string, width int) string {
	if width <= 0 {
		return "" // 幅 0 以下に収まる表示は空しかない (glogx 本体の clipToWidth と同じ契約)
	}
	if len(line) <= width && strings.IndexByte(line, '\x1b') < 0 {
		return line
	}
	if termwidth.Of(line) <= width {
		return line
	}
	return termwidth.Truncate(line, width, "…")
}

// paintLine は 1 行のスパン列を ANSI 付き文字列へ変換する。
func paintLine(l line, colored bool) string {
	var b strings.Builder
	for _, sp := range l.spans {
		switch {
		case !colored:
			b.WriteString(sp.Text)
		case sp.Style == styleCodeBlock:
			b.WriteString(highlight.Lang(l.lang, sp.Text))
		default:
			if seq := sgrFor(sp.Style); seq != "" {
				b.WriteString(seq)
				b.WriteString(sp.Text)
				b.WriteString(sgr.Reset)
				continue
			}
			b.WriteString(sp.Text)
		}
	}
	return b.String()
}

// sgrFor は style に対応する SGR を返す ("" = 装飾なし)。
func sgrFor(st style) string {
	switch st {
	case styleH1:
		return sgr.Bold + sgr.Yellow
	case styleH2:
		return sgr.Bold + sgr.Cyan
	case styleH3:
		return sgr.Bold
	case styleCodeSpan:
		return sgr.Green
	case styleStrong:
		return sgr.Bold
	case styleEm:
		return sgr.Italic
	case styleStrike:
		return sgr.Strike
	case styleLink:
		return sgr.Underline + sgr.Cyan
	case styleMarker, styleDim:
		return sgr.Dim
	case styleText, styleCodeBlock:
		return ""
	default:
		return ""
	}
}
