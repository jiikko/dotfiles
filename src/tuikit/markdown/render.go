// Package markdown は markdown の本文を width 桁の端末行へ整形する (見出し・箇条書き・表・
// フェンスコードの chroma ハイライト)。glogx の issues viewer の本文と、pro-con の詳細の
// 応答の文が同じ整形器を使う (glogx/issues から移した。dotfiles issue 486)。
package markdown

import (
	"strings"

	"github.com/jiikko/dotfiles/src/tuikit/highlight"
	"github.com/jiikko/dotfiles/src/tuikit/sgr"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
)

// 純粋描画層: 意味付きスパン列へ ANSI を塗る。I/O・プロセス起動・非同期はここに置かない
// (depguard の render-pure ルールが機械的に禁止している)。フェンスコードの色は diff の板と同じ github.com/jiikko/dotfiles/src/tuikit/highlight。

// Render は markdown の本文 (issue 本文・PG の応答の文) を width 桁の端末行へ整形する。
// colored=false なら ANSI を一切付けない (テストと非 TTY 出力のため)。
//
// 第 2 戻り値は各行に対応するソース (.md) の行番号 (0 = 出さない。理由は line.src の doc)。
// 呼び出し側が左の溝に出す。
func Render(src string, width int, colored bool) (out []string, srcLines []int) {
	out, srcLines, _ = RenderLinks(src, width, colored, nil)
	return out, srcLines
}

// RenderLinks は Render に加えて、本文中のリンク候補 (links.go の Link) の表示位置を返し、
// mark が返す強調をリンクへ塗る (mark=nil なら塗らない = Render と同じ出力)。
//
// 🚨 強調は整形 (折り返し) の後に塗るだけなので、mark の有無で行数・桁は変わらない。
// 呼び出し側は mark なしで取った Link の位置を、mark ありの出力にそのまま当ててよい。
func RenderLinks(src string, width int, colored bool, mark func(i int) LinkMark) (out []string, srcLines []int, links []Link) {
	lines := renderMarkdown(src, width)
	links, index := collectLinks(lines, width)
	markOf := func(*linkRef) LinkMark { return LinkPlain }
	if mark != nil {
		markOf = func(r *linkRef) LinkMark {
			if i, ok := index[r]; ok {
				return mark(i)
			}
			return LinkPlain // 切り詰めで画面に 1 桁も出ないリンク (links に載らない)
		}
	}
	out = make([]string, 0, len(lines))
	srcLines = make([]int, 0, len(lines))
	for _, l := range lines {
		// 「出力は width 桁を超えない」という Render の契約を、出口のここ 1 箇所で無条件に守る (issue 116)。
		// 整形は箇条書きの記号・番号・表の罫線・入れ子のインデントを先に置いてから残り幅へ本文を詰めるので、
		// 幅がそれらの固定分より狭いと構造の側が溢れる (実測 2026-08-27: 幅 1 で 80 行・幅 3 で 42 行)。
		// 「幅 N 未満は畳む」下限の案は、下限を跨ぐ入力ごとに畳み方を決めることになるので採らなかった。
		out = append(out, termwidth.Clip(paintLine(l, colored, markOf), width))
		srcLines = append(srcLines, l.src)
	}
	return out, srcLines, links
}

// paintLine は 1 行のスパン列を ANSI 付き文字列へ変換する。markOf はリンクのスパンに足す強調。
func paintLine(l line, colored bool, markOf func(*linkRef) LinkMark) string {
	var b strings.Builder
	for _, sp := range l.spans {
		switch {
		case !colored:
			b.WriteString(sp.Text)
		case sp.Style == styleCodeBlock:
			b.WriteString(highlight.Lang(l.lang, sp.Text))
		default:
			seq := sgrFor(sp.Style)
			if sp.link != nil {
				seq += sgrForMark(markOf(sp.link))
			}
			if seq != "" {
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

// sgrForMark はリンクの強調に足す SGR。style の色の上に重ねる (コードスパンの緑は緑のまま)。
func sgrForMark(m LinkMark) string {
	switch m {
	case LinkMarked:
		return sgr.Underline
	case LinkSelected:
		return sgr.Reverse + sgr.Bold
	case LinkPlain:
		return ""
	default:
		return ""
	}
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
