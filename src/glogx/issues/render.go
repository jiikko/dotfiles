package issues

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"

	"glogx/sgr"
	"glogx/termwidth"
)

// 純粋描画層: 意味付きスパン列へ ANSI を塗る。I/O・プロセス起動・非同期はここに置かない
// (depguard の render-pure ルールが機械的に禁止している)。
//
// 256色主環境 (docs/theme-colors.md) なので formatter は terminal256、スタイルは
// glogx 本体の diff ハイライトと同じ gruvbox で揃える。
var (
	hlFormatter = formatters.Get("terminal256")
	hlStyle     = styles.Get("gruvbox")
)

// RenderBody は issue 本文 (markdown) を width 桁の端末行へ整形する。
// colored=false なら ANSI を一切付けない (テストと非 TTY 出力のため)。
//
// 第 2 戻り値は各行に対応するソース (.md) の行番号 (0 = 出さない。理由は line.src の doc)。
// 呼び出し側が左の溝に出す。
func RenderBody(src string, width int, colored bool) (out []string, srcLines []int) {
	lines := renderMarkdown(src, width)
	out = make([]string, 0, len(lines))
	srcLines = make([]int, 0, len(lines))
	for _, l := range lines {
		out = append(out, clipToWidth(paintLine(l, colored), width))
		srcLines = append(srcLines, l.src)
	}
	return out, srcLines
}

// clipToWidth は「出力は width 桁を超えない」という RenderBody の契約を、**出口 1 箇所で**
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
// ⚠️ ここは Body.Lines のキャッシュ越しなので毎フレームは走らない (width か colored が
// 変わったときだけ)。とはいえ ANSI 無しで byte 長が収まる行は最も多いので fast-path は残す。
func clipToWidth(line string, width int) string {
	if width <= 0 {
		return "" // 幅 0 以下に収まる表示は空しかない (本体の clipToWidth と同じ契約)
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
			b.WriteString(highlightCode(l.lang, sp.Text))
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

// highlightCode はコード 1 行を chroma でハイライトする。言語不明・トークナイズ失敗時は
// 素のまま返す (ハイライトは常に best-effort)。
//
// glogx 本体の highlight.go と同じ「行単位トークナイズ」の割り切り: 複数行コメントや
// 文字列リテラルの状態は行を跨いで持たない (行ごとに独立して色を付ける)。
func highlightCode(lang, code string) string {
	lex := lexerFor(lang)
	if lex == nil || code == "" {
		return code
	}
	it, err := lex.Tokenise(nil, code)
	if err != nil {
		return code
	}
	var b strings.Builder
	if err := hlFormatter.Format(&b, hlStyle, it); err != nil {
		return code
	}
	// chroma は行末に改行を足すことがあるため落とす (呼び出し側は行単位で管理)
	return strings.TrimRight(b.String(), "\n")
}

// lexerFor はフェンスの言語名から lexer を引く。エイリアス (sh / zsh / golang 等) の解決は
// chroma 側の表に委ね、未知の言語は nil (素通し) にする。
func lexerFor(lang string) chroma.Lexer {
	if lang == "" {
		return nil
	}
	return lexers.Get(lang)
}
