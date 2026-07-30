package issues

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// 表示幅の計算はこの 2 関数に一本化する。glogx 本体 (width.go) と同じ ansi.StringWidth
// (GraphemeWidth モデル) に揃えないと、このパッケージが整えた行を glogx 側が測り直した
// ときに桁がずれる。幅モデルを選んだ理由と層ごとの実測値は width.go 冒頭が一次情報。

// dispWidth は文字列の端末表示幅を返す (ANSI エスケープは幅 0)。
func dispWidth(s string) int { return ansi.StringWidth(s) }

// dropVS16 は絵文字異体字セレクタ VS16 (U+FE0F) を除去して ⚠️ を bare な ⚠ へ倒す。
// issue 本文は「表示に出る外部由来テキスト」なので、glogx の他の入口 (git 由来・CI ログ) と
// 同じくここで正規化し、層ごとに幅解釈が割れる字を表示に出さない。一次情報は
// width.go の dropEmojiVS16 (色が落ちるトレードオフの経緯もそこ)。
func dropVS16(s string) string {
	const vs16 = "\xef\xb8\x8f" // U+FE0F の UTF-8 バイト列 (直接書くと不可視文字になる)
	if !strings.Contains(s, vs16) {
		return s
	}
	return strings.ReplaceAll(s, vs16, "")
}

// style は 1 スパンの意味。ANSI への変換は render.go が担う。
//
// 意味と ANSI を分けるのは wrap のため: ANSI 混じりの文字列を grapheme 単位で折り返すと
// SGR を分断し、色が行末まで漏れたり途中で切れたりする。折り返しは常に素のテキストに対して
// 行い、色は折り返し後に塗る。
type style uint8

const (
	styleText style = iota
	styleH1
	styleH2
	styleH3         // H3 以降 (見出しの階層は marker 記号で示すので色は 3 段で足る)
	styleCodeSpan   // インラインコード
	styleCodeBlock  // コードブロック本文 (colored なら render.go が chroma に回す)
	styleStrong
	styleEm
	styleStrike
	styleLink
	styleMarker // 箇条書き記号・引用の縦線・表の罫線・水平線
	styleDim
)

// span は同じ style が続く区間。
//
// ⚠️ 不変条件: Text は ANSI エスケープを含まない素のテキスト。この不変条件が崩れると
// wrap が SGR の途中で分割しうる (styleCodeBlock だけは render.go が塗る段で chroma の
// ANSI を載せるため、その 1 スパンで 1 行を占める形にしてから渡す)。
type span struct {
	Text  string
	Style style
}

// line は端末 1 行分のスパン列。
type line struct {
	spans []span
	lang  string // styleCodeBlock のスパンを chroma へ渡すときの言語 ("" = 素通し)
}

// 禁則処理: 日本語は空白が無いのでクラスタ間で折り返すが、行頭・行末に置くと読みにくい字は
// 避ける。完全な JIS 準拠は狙わず、実際の issue 本文 (句読点・括弧・長音・繰り返し記号) で
// 目に付くものだけを対象にする。
const (
	// noLineStart は行頭に置かない字 (句読点・閉じ括弧・小書き仮名相当・繰り返し記号)。
	noLineStart = "、。，．・：；？！ヽヾゝゞ々ー〜～)）]］｝〕〉》」』】’”…‥"
	// noLineEnd は行末に置かない字 (開き括弧類)。
	noLineEnd = "([［｛〔〈《「『【‘“（"
)

// cell は折り返しの最小単位 (grapheme クラスタ 1 個)。
type cell struct {
	text  string
	w     int
	style style
}

// flattenSpans はスパン列を grapheme クラスタ単位へ展開する。rune 単位にしないのは
// ⚠ + VS16 や ZWJ 絵文字のような複数 rune のクラスタを分断しないため。
func flattenSpans(spans []span) []cell {
	n := 0
	for _, sp := range spans {
		n += len(sp.Text)
	}
	cells := make([]cell, 0, n)
	for _, sp := range spans {
		g := uniseg.NewGraphemes(sp.Text)
		for g.Next() {
			c := g.Str()
			cells = append(cells, cell{text: c, w: dispWidth(c), style: sp.Style})
		}
	}
	return cells
}

// mergeCells は cell 列を同じ style ごとにまとめてスパン列へ戻す。
func mergeCells(cells []cell) []span {
	if len(cells) == 0 {
		return nil
	}
	spans := make([]span, 0, 8)
	var b strings.Builder
	cur := cells[0].style
	for _, c := range cells {
		if c.style != cur {
			spans = append(spans, span{Text: b.String(), Style: cur})
			b.Reset()
			cur = c.style
		}
		b.WriteString(c.text)
	}
	spans = append(spans, span{Text: b.String(), Style: cur})
	return spans
}

// isSpaceCell は空白 (折り返し時に行末へ落とす字) か。
func isSpaceCell(c cell) bool { return c.text == " " || c.text == "\t" }

// canBreakBetween は prev と cur の間で行を分けてよいか。
//
//   - 空白の直後は分けられる (英文の通常の折り返し)
//   - 幅 2 の字 (CJK・絵文字) が隣接するときはクラスタ間で分けられる。日本語には空白が
//     無いため、この規則が無いと 1 段落が 1 行に収まらないまま強制改行される
//   - 禁則対象は分けない (行頭に句読点/閉じ括弧、行末に開き括弧を作らない)
func canBreakBetween(prev, cur cell) bool {
	if prev.text == "" || cur.text == "" {
		return false
	}
	pr, _ := utf8.DecodeRuneInString(prev.text)
	cr, _ := utf8.DecodeRuneInString(cur.text)
	if strings.ContainsRune(noLineEnd, pr) || strings.ContainsRune(noLineStart, cr) {
		return false
	}
	if isSpaceCell(prev) {
		return true
	}
	return prev.w == 2 || cur.w == 2
}

// wrapSpans はスパン列を表示幅 limit で折り返す。limit <= 0 なら折り返さない (1 行で返す)。
//
// 空白で分けた場合、行末の空白は落とし、次行頭の空白も飛ばす (行頭が空白でずれない)。
// 1 クラスタが limit より広い場合はその 1 個だけで 1 行にする (前進を保証して無限ループを防ぐ)。
func wrapSpans(spans []span, limit int) [][]span {
	cells := flattenSpans(spans)
	if len(cells) == 0 {
		return [][]span{nil}
	}
	if limit <= 0 {
		return [][]span{mergeCells(cells)}
	}
	out := make([][]span, 0, 4)
	for start := 0; start < len(cells); {
		end, w, brk := len(cells), 0, -1
		for i := start; i < len(cells); i++ {
			if i > start && canBreakBetween(cells[i-1], cells[i]) {
				brk = i
			}
			if w+cells[i].w > limit {
				switch {
				case brk > start:
					end = brk
				case i > start:
					end = i
				default:
					end = start + 1 // 1 クラスタが limit 超: 単独で 1 行にして前進する
				}
				break
			}
			w += cells[i].w
		}
		seg := cells[start:end]
		for len(seg) > 0 && isSpaceCell(seg[len(seg)-1]) {
			seg = seg[:len(seg)-1] // 行末の空白は落とす
		}
		out = append(out, mergeCells(seg))
		start = end
		for start < len(cells) && isSpaceCell(cells[start]) {
			start++ // 折り返し直後の空白は行頭に出さない
		}
	}
	return out
}

// truncSpans はスパン列を表示幅 limit まで切り詰める。切った場合は末尾を tail に置き換える
// (表のセルのように折り返せない場所で使う)。
func truncSpans(spans []span, limit int, tail string) []span {
	cells := flattenSpans(spans)
	total := 0
	for _, c := range cells {
		total += c.w
	}
	if total <= limit {
		return mergeCells(cells)
	}
	tw := dispWidth(tail)
	if limit <= tw {
		return []span{{Text: ansi.Truncate(tail, max(limit, 0), ""), Style: styleDim}}
	}
	w, cut := 0, 0
	for i, c := range cells {
		if w+c.w > limit-tw {
			cut = i
			break
		}
		w += c.w
	}
	kept := mergeCells(cells[:cut])
	return append(kept, span{Text: tail, Style: styleDim})
}

// spansWidth はスパン列の表示幅。
func spansWidth(spans []span) int {
	w := 0
	for _, sp := range spans {
		w += dispWidth(sp.Text)
	}
	return w
}

// expandTabs はタブを空白へ展開する。端末のタブ位置は枠の中では合わないため、整形の入口で
// 空白に倒して幅計算とレンダリングを一致させる。
func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	const tabWidth = 4
	var b strings.Builder
	col := 0
	for _, r := range s {
		if r == '\t' {
			n := tabWidth - col%tabWidth
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(r)
		col += dispWidth(string(r))
	}
	return b.String()
}
