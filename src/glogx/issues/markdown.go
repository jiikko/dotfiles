package issues

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"termsafe"

	"github.com/charmbracelet/x/ansi"

	"glogx/termwidth"
)

// issue 本文 (markdown) を端末行へ整形する層。
//
// 方針: 「ブロック分解 → 段落の再流し込み (reflow) → インライン解析 → 折り返し」の順に通す。
// CommonMark の完全実装は狙わず、この repo の issue が実際に使う構文だけを扱う
// (実測 31 ファイル: 見出し・箇条書き・番号付き・チェックボックス・引用・表・水平線・
// フェンスコード・インラインコード・強調・打ち消し・リンク)。
//
// 🚨 意図的に扱わない構文と理由:
//   - インデントコードブロック (4 スペース) — この repo の issue では 4 スペースは箇条書きの
//     入れ子/継続行を意味する。コード扱いにすると入れ子リストがコードに化けて壊れるため、
//     コードはフェンス (``` / ~~~) だけを認識する
//   - `_強調_` — snake_case の識別子 (`no_provider_specific_branch` 等) が本文に頻出し、
//     誤って斜体になる。強調は `*` と `**` だけを見る
//   - setext 見出し (=== / --- の下線) — `---` は常に水平線として扱う (front matter の
//     区切りと水平線の判定を単純に保つため)
type blockKind uint8

const (
	blkBlank blockKind = iota
	blkParagraph
	blkHeading
	blkCode
	blkQuote
	blkList
	blkTable
	blkRule
)

// block は markdown の 1 ブロック。text は段落として連結済み (reflow 後)、raw は行のまま
// 保持するもの (コード・表)。
type block struct {
	kind blockKind
	// src はこのブロックが始まるソース (.md) の行番号 (1 起点、0 = 不明)。本文に行番号を出す
	// ためだけに持つ。🚨 整形はソース行と 1:1 でない (段落は複数行を畳み、折り返しは 1 行を
	// 複数行に割る) ので、表示側は「ブロックの先頭の表示行にだけ番号を出す」形にしている。
	src    int
	level  int      // heading: 1..6 / list: ネスト深さ (0 起点)
	marker string   // list の行頭記号 ("•" / "1." / "☐" / "☑")
	lang   string   // code フェンスの言語
	text   string   // paragraph / heading / quote / list の本文
	raw    []string // code / table の生行
}

var (
	headingRe = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	ruleRe    = regexp.MustCompile(`^\s{0,3}(-{3,}|\*{3,}|_{3,})\s*$`)
	listRe    = regexp.MustCompile(`^(\s*)([-*+]|\d{1,9}[.)])\s+(.*)$`)
	// フェンスのインデントは任意にする: 箇条書きの中に置いたコードブロックは 4 桁以上
	// インデントされ、0-3 桁に縛ると「フェンスとして認識されず、中身が箇条書きの継続行として
	// 散文に連結される」= コードが壊れて出る (実測: 他 repo の issue 2 ファイルで発生)。
	// 開き行のインデント幅は parseFence が記録して中身から落とす。
	fenceRe   = regexp.MustCompile("^(\\s*)(```+|~~~+)\\s*([^\\s`]*)")
	sepCellRe = regexp.MustCompile(`^:?-{1,}:?$`)
)

// renderMarkdown は本文を width 桁の行へ整形する。ANSI は付けない (render.go の仕事)。
func renderMarkdown(src string, width int) []line {
	return renderBlocks(parseBlocks(src), width)
}

// parseBlocks は本文をブロックへ分解する。
func parseBlocks(src string) []block {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	// issue 本文は第三者が書いたファイル (PR 経由で入りうる) なので、ここが端末制御シーケンスの
	// 単一の関門になる。
	//
	// 🚨 2 点、掛け方に条件がある:
	//   - 分割の「後」に 1 行ずつ通す。termsafe は改行も制御文字として落とすため、分割前の
	//     全文に掛けると本文が 1 行に潰れる
	//   - タブは残す版を使う。タブの展開はこの層の expandTabs がタブストップ揃えで行うので、
	//     ここで一律 4 スペースに潰すと桁が崩れる (`ab<TAB>c` が `ab  c` → `ab    c`)
	for i, ln := range lines {
		lines[i] = termsafe.PlainLineKeepTabs(ln)
	}
	// 末尾の改行・空行は落とす (pager の末尾に空行がぶら下がると「まだ続きがある」ように見える)
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	lines, dropped := skipFrontMatter(lines)
	blocks := make([]block, 0, 32)
	for i := 0; i < len(lines); {
		ln := lines[i]
		before, start := len(blocks), i
		switch {
		case strings.TrimSpace(ln) == "":
			// 空行の連続は 1 行に畳む (ソースの段落間の空け方に表示を振り回されない)
			if n := len(blocks); n == 0 || blocks[n-1].kind != blkBlank {
				blocks = append(blocks, block{kind: blkBlank})
			}
			i++
		case fenceRe.MatchString(ln):
			b, next := parseFence(lines, i)
			blocks = append(blocks, b)
			i = next
		case headingRe.MatchString(ln):
			m := headingRe.FindStringSubmatch(ln)
			blocks = append(blocks, block{kind: blkHeading, level: len(m[1]), text: m[2]})
			i++
		case ruleRe.MatchString(ln):
			blocks = append(blocks, block{kind: blkRule})
			i++
		case isTableRow(ln):
			b, next := parseTable(lines, i)
			blocks = append(blocks, b)
			i = next
		case strings.HasPrefix(strings.TrimSpace(ln), ">"):
			b, next := parseQuote(lines, i)
			blocks = append(blocks, b)
			i = next
		case listRe.MatchString(ln):
			b, next := parseListItem(lines, i)
			blocks = append(blocks, b)
			i = next
		default:
			b, next := parseParagraph(lines, i)
			blocks = append(blocks, b)
			i = next
		}
		// 空行の畳み込みは append しないことがあるので、増えたときだけ記録する
		if len(blocks) > before {
			blocks[len(blocks)-1].src = start + 1 + dropped
		}
	}
	return blocks
}

// skipFrontMatter は先頭の YAML front matter (--- で囲まれた区間) を落とす。本文の描画では
// 出さない: メタデータは parse.go がヘッダー/バッジとして扱うため、本文に二重に出さない。
// 第 2 戻り値は落とした行数 (本文に出す行番号をソースへ合わせるオフセット)。
func skipFrontMatter(lines []string) ([]string, int) {
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return lines, 0
	}
	for i := 1; i < len(lines); i++ {
		if s := strings.TrimSpace(lines[i]); s == "---" || s == "..." {
			return lines[i+1:], i + 1
		}
	}
	return lines, 0 // 閉じが無い = front matter ではなかった (水平線始まり) と解釈する
}

// parseFence はフェンスコードブロックを読む。閉じフェンスが無い場合は EOF までをコードにする。
//
// 中身は開きフェンスと同じ深さだけ dedent する: 箇条書きの中のコードブロックをそのまま出すと
// 枠の右端へ押し出されて折り返し (実際は幅で切り落とし) が起きる。相対インデントは保つ。
func parseFence(lines []string, i int) (block, int) {
	m := fenceRe.FindStringSubmatch(lines[i])
	indent, open, lang := termwidth.Of(m[1]), m[2], m[3]
	raw := make([]string, 0, 8)
	for j := i + 1; j < len(lines); j++ {
		if c := fenceRe.FindStringSubmatch(lines[j]); c != nil && len(c[2]) >= len(open) &&
			c[2][0] == open[0] && strings.TrimSpace(c[3]) == "" {
			return block{kind: blkCode, lang: lang, raw: raw}, j + 1
		}
		raw = append(raw, dedentSpaces(lines[j], indent))
	}
	return block{kind: blkCode, lang: lang, raw: raw}, len(lines)
}

// dedentSpaces は先頭の空白を最大 n 桁だけ落とす (n 桁未満の行はそこまで)。
func dedentSpaces(s string, n int) string {
	for range n {
		if !strings.HasPrefix(s, " ") {
			break
		}
		s = s[1:]
	}
	return s
}

// isTableRow は表の行 (| で始まる) か。
func isTableRow(ln string) bool { return strings.HasPrefix(strings.TrimSpace(ln), "|") }

// parseTable は連続する表の行をまとめる。
func parseTable(lines []string, i int) (block, int) {
	raw := make([]string, 0, 8)
	j := i
	for ; j < len(lines) && isTableRow(lines[j]); j++ {
		raw = append(raw, strings.TrimSpace(lines[j]))
	}
	return block{kind: blkTable, raw: raw}, j
}

// parseQuote は連続する引用行を 1 段落へまとめる。
func parseQuote(lines []string, i int) (block, int) {
	text := ""
	j := i
	for ; j < len(lines); j++ {
		s := strings.TrimSpace(lines[j])
		if !strings.HasPrefix(s, ">") {
			break
		}
		text = reflowJoin(text, strings.TrimPrefix(strings.TrimPrefix(s, ">"), " "))
	}
	return block{kind: blkQuote, text: text}, j
}

// parseListItem は箇条書き 1 項目を読む。以降の行のうち「新しい項目でも他ブロックでもない
// 非空行」は同じ項目の継続行として連結する (ソース側で折り返された長い項目を 1 段落に戻す)。
func parseListItem(lines []string, i int) (block, int) {
	m := listRe.FindStringSubmatch(lines[i])
	indent, marker, content := m[1], m[2], m[3]
	b := block{kind: blkList, level: min(termwidth.Of(indent)/2, 4)}
	switch {
	case strings.HasPrefix(content, "[ ] "):
		b.marker, content = "☐", strings.TrimPrefix(content, "[ ] ")
	case strings.HasPrefix(content, "[x] "), strings.HasPrefix(content, "[X] "):
		b.marker, content = "☑", content[4:]
	case marker == "-" || marker == "*" || marker == "+":
		b.marker = bulletFor(b.level)
	default:
		b.marker = marker // 番号付きは原文の番号をそのまま出す
	}
	b.text = content
	j := i + 1
	for ; j < len(lines); j++ {
		ln := lines[j]
		if startsNewBlock(ln) {
			break
		}
		b.text = reflowJoin(b.text, strings.TrimSpace(ln))
	}
	return b, j
}

// startsNewBlock は「継続行の連結をここで打ち切る = 新しいブロックの先頭」の判定。
// リスト項目と段落の両方がこの境界で継続を止める (parseListItem / parseParagraph に条件の
// 順序だけ違う同一 7 条件が複製されていて、8 種類目のブロックを足すとき片方だけ更新して
// 取りこぼす構造だったのを一本化)。
func startsNewBlock(ln string) bool {
	return strings.TrimSpace(ln) == "" || listRe.MatchString(ln) || headingRe.MatchString(ln) ||
		ruleRe.MatchString(ln) || fenceRe.MatchString(ln) || isTableRow(ln) ||
		strings.HasPrefix(strings.TrimSpace(ln), ">")
}

// bulletFor はネスト深さごとの行頭記号 (幅 1 の bare 記号に限る: 絵文字は層ごとに幅解釈が
// 割れるため。width.go の VS16 の議論と同じ理由)。
func bulletFor(level int) string {
	switch level {
	case 0:
		return "•"
	case 1:
		return "◦"
	default:
		return "·"
	}
}

// parseParagraph は連続する通常行を 1 段落へ連結する。
func parseParagraph(lines []string, i int) (block, int) {
	text := ""
	j := i
	for ; j < len(lines); j++ {
		ln := lines[j]
		if startsNewBlock(ln) {
			break
		}
		text = reflowJoin(text, strings.TrimSpace(ln))
	}
	return block{kind: blkParagraph, text: text}, j
}

// reflowJoin は「ソース側で折り返された段落」を 1 本に戻す。
//
// この repo の issue 本文は 100 桁前後で手折り返しされているので、popup 幅で折り返す前に
// 段落を 1 本に戻す。連結時の空白の入れ方は「両側が日本語のときだけ空白を入れない」:
//
//   - 日本語どうし → 空白なし ("途中で" + "折り返される")。素朴に空白を入れると文の途中に
//     隙間が空く
//   - それ以外 (英字どうし / 日本語とラテン語の境界) → 空白を 1 つ。この repo の文章は
//     "popup は pane ではなく" のようにラテン語の前後へ空白を置く書き方なので、境界で
//     詰めると "popup はpane" になって元の文と食い違う
func reflowJoin(prev, next string) string {
	if prev == "" {
		return next
	}
	if next == "" {
		return prev
	}
	last, _ := utf8.DecodeLastRuneInString(prev)
	first, _ := utf8.DecodeRuneInString(next)
	if isJapanesePunct(last) || isJapanesePunct(first) {
		return prev + next // "。tmux" のように和文の句読点・括弧には空白を付けない
	}
	if isJapanese(last) && isJapanese(first) {
		return prev + next
	}
	return prev + " " + next
}

// isJapanese は「連結時に空白を挟まない字」か (仮名・漢字・和文の記号と全角形)。
//
// 🚨 仮名は Script ではなく範囲で判定する: unicode.Katakana は Script=Katakana なので
// 長音記号 ー (U+30FC) と中黒 ・ (U+30FB) を含まない (どちらも Script=Common)。
// Script で判定すると "選択コピー" + "することは" の境界に空白が入る。
func isJapanese(r rune) bool {
	switch {
	case r >= 0x3000 && r <= 0x30ff: // 和文記号 + ひらがな + カタカナ (ー ・ を含む)
		return true
	case unicode.Is(unicode.Han, r): // 漢字
		return true
	case r >= 0xff00 && r <= 0xffef: // 全角英数・全角記号・半角カナ
		return true
	default:
		return false
	}
}

// isJapanesePunct は前後に空白を置かない和文の記号 (句読点・括弧)。
func isJapanesePunct(r rune) bool {
	return strings.ContainsRune("、。，．・：；？！）」』】〉》〕｝〜～…‥", r) ||
		strings.ContainsRune("（「『【〈《〔｛", r)
}

// renderBlocks はブロック列を width 桁の行へ変換する。
func renderBlocks(blocks []block, width int) []line {
	out := make([]line, 0, len(blocks)*2)
	for _, b := range blocks {
		var ls []line
		switch b.kind {
		case blkBlank:
			ls = []line{{}}
		case blkHeading:
			ls = renderHeading(b, width)
		case blkParagraph:
			ls = renderWrapped(parseInline(b.text), width, span{}, span{})
		case blkQuote:
			mark := span{Text: "┃ ", Style: styleMarker}
			ls = renderWrapped(restyleText(parseInline(b.text), styleDim), width, mark, mark)
		case blkList:
			ls = renderList(b, width)
		case blkCode:
			ls = renderCode(b, width)
		case blkTable:
			ls = renderTable(b, width)
		case blkRule:
			ls = []line{{spans: []span{{Text: strings.Repeat("─", max(width, 1)), Style: styleMarker}}}}
		}
		// 行番号はブロックの先頭の表示行にだけ載せる (続き行は 0 = 出さない)。コード・表は
		// ソース行と 1:1 なので、自前で行ごとに番号を入れている (下の renderCode / renderTable)。
		if len(ls) > 0 && ls[0].src == 0 {
			ls[0].src = b.src
		}
		out = append(out, ls...)
	}
	return out
}

// renderWrapped は spans を折り返し、1 行目に first・2 行目以降に rest を前置する。
//
// 折り返し幅は first / rest の広い方を差し引いた値で一律に計算する (前置が同幅の使い方しか
// しないため。差が出る使い方を足すときはここを行ごとの幅計算に変える)。
func renderWrapped(spans []span, width int, first, rest span) []line {
	limit := width - max(termwidth.Of(first.Text), termwidth.Of(rest.Text))
	wrapped := wrapSpans(spans, limit)
	out := make([]line, 0, len(wrapped))
	for i, ws := range wrapped {
		pre := first
		if i > 0 {
			pre = rest
		}
		l := line{}
		if pre.Text != "" {
			l.spans = append(l.spans, pre)
		}
		l.spans = append(l.spans, ws...)
		out = append(out, l)
	}
	return out
}

// headingDecor は見出しレベルごとの行頭記号と本文スタイル。
func headingDecor(level int) (string, style) {
	switch level {
	case 1:
		return "█ ", styleH1
	case 2:
		return "■ ", styleH2
	case 3:
		return "▸ ", styleH3
	default:
		return "· ", styleH3
	}
}

// renderHeading は見出しを描く。見出し内の強調は見出し色へ潰す (`**` が二重に効くと
// かえって階層が読みにくい)。インラインコードとリンクは区別を残す。
func renderHeading(b block, width int) []line {
	marker, st := headingDecor(b.level)
	spans := parseInline(b.text)
	for i := range spans {
		switch spans[i].Style {
		case styleText, styleStrong, styleEm:
			spans[i].Style = st
		default:
		}
	}
	pre := span{Text: marker, Style: styleMarker}
	cont := span{Text: termwidth.PadSpaces(termwidth.Of(marker)), Style: styleText}
	return renderWrapped(spans, width, pre, cont)
}

// renderList は箇条書き 1 項目を描く。継続行は記号の下へぶら下げる。
func renderList(b block, width int) []line {
	pre := span{Text: strings.Repeat("  ", b.level) + b.marker + " ", Style: styleMarker}
	cont := span{Text: termwidth.PadSpaces(termwidth.Of(pre.Text)), Style: styleText}
	return renderWrapped(parseInline(b.text), width, pre, cont)
}

// renderCode はコードブロックを描く。コードは折り返さず幅で切る (インデントと桁が意味を
// 持つため、折り返すと構造が読めなくなる)。ハイライトは render.go が colored 時に行う。
func renderCode(b block, width int) []line {
	pre := span{Text: "┃ ", Style: styleMarker}
	avail := max(width-termwidth.Of(pre.Text), 1)
	out := make([]line, 0, len(b.raw))
	for i, raw := range b.raw {
		txt := expandTabs(raw)
		if termwidth.Of(txt) > avail {
			txt = ansi.Truncate(txt, avail, "…")
		}
		// コードは折り返さないのでソース行と 1:1。b.src は開きフェンスの行なので中身は +1 から
		src := 0
		if b.src > 0 {
			src = b.src + 1 + i
		}
		out = append(out, line{spans: []span{pre, {Text: txt, Style: styleCodeBlock}}, lang: b.lang, src: src})
	}
	return out
}

// renderTable は表を桁揃えして描く。折り返さず、幅が足りなければ広い列から順に詰める。
func renderTable(b block, width int) []line {
	rows, seps := tableCells(b.raw)
	if len(rows) == 0 {
		return nil
	}
	cols := 0
	for _, r := range rows {
		cols = max(cols, len(r))
	}
	colW := tableColWidths(rows, seps, cols, width)
	out := make([]line, 0, len(rows))
	for ri, row := range rows {
		l := line{spans: make([]span, 0, cols*2)}
		if b.src > 0 { // 表も折り返さないのでソース行と 1:1
			l.src = b.src + ri
		}
		for ci := range cols {
			if ci > 0 {
				l.spans = append(l.spans, span{Text: tableSep(seps[ri]), Style: styleMarker})
			}
			var cell []span
			if ci < len(row) {
				cell = row[ci]
			}
			if seps[ri] {
				l.spans = append(l.spans, span{Text: strings.Repeat("─", colW[ci]), Style: styleMarker})
				continue
			}
			cell = truncSpans(cell, colW[ci], "…")
			l.spans = append(l.spans, cell...)
			if pad := colW[ci] - spansWidth(cell); pad > 0 {
				l.spans = append(l.spans, span{Text: termwidth.PadSpaces(pad), Style: styleText})
			}
		}
		out = append(out, l)
	}
	return out
}

// tableSep は列の区切り (区切り行だけ罫線でつなぐ)。
func tableSep(isSeparator bool) string {
	if isSeparator {
		return "─┼─"
	}
	return " │ "
}

// tableCells は表の生行をセルのスパン列へ分解し、各行が区切り行 (|---|) かを返す。
func tableCells(raw []string) ([][][]span, []bool) {
	rows := make([][][]span, 0, len(raw))
	seps := make([]bool, 0, len(raw))
	for idx, ln := range raw {
		texts := splitTableRow(ln)
		// 区切り行はヘッダーの直下 (2 行目) だけ。形だけで判定すると、全セルがハイフンの
		// データ行 (空欄のプレースホルダ) が罫線に置き換わって内容が黙って消える。
		// なお実データには `|--|--|--|` という短い区切り行が実在するので、形の側は
		// ハイフン 1 個から認める (桁数で締めると本物の区切り行が壊れる)。
		sep := idx == 1 && len(texts) > 0
		for _, t := range texts {
			if !sep {
				break
			}
			if !sepCellRe.MatchString(strings.TrimSpace(t)) {
				sep = false
				break
			}
		}
		cells := make([][]span, 0, len(texts))
		for _, t := range texts {
			cells = append(cells, parseInline(strings.TrimSpace(t)))
		}
		rows = append(rows, cells)
		seps = append(seps, sep)
	}
	return rows, seps
}

// splitTableRow は 1 行を | で分割する (\| は列区切りにしない)。
func splitTableRow(ln string) []string {
	ln = strings.TrimSpace(ln)
	ln = strings.TrimPrefix(ln, "|")
	ln = strings.TrimSuffix(ln, "|")
	out := make([]string, 0, 4)
	var b strings.Builder
	esc := false
	for _, r := range ln {
		switch {
		case esc:
			b.WriteRune(r)
			esc = false
		case r == '\\':
			esc = true
		case r == '|':
			out = append(out, b.String())
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	out = append(out, b.String())
	return out
}

// tableColWidths は各列の表示幅を決める。自然幅の合計が width を超える場合、いちばん広い列を
// 1 桁ずつ削って収める (等分割にすると短い列に無駄な余白が出る)。
func tableColWidths(rows [][][]span, seps []bool, cols, width int) []int {
	const minCol = 3
	colW := make([]int, cols)
	for ri, row := range rows {
		if seps[ri] {
			continue // 区切り行の "---" は自然幅に数えない
		}
		for ci, cell := range row {
			colW[ci] = max(colW[ci], spansWidth(cell))
		}
	}
	for ci := range colW {
		colW[ci] = max(colW[ci], 1)
	}
	sepW := termwidth.Of(" │ ") * (cols - 1)
	total := sepW
	for _, w := range colW {
		total += w
	}
	for total > width {
		widest, wi := 0, -1
		for ci, w := range colW {
			if w > widest {
				widest, wi = w, ci
			}
		}
		if wi < 0 || widest <= minCol {
			break // これ以上詰められない (行が width を超えるのは許容し、切るのは呼び出し側)
		}
		colW[wi]--
		total--
	}
	return colW
}

// restyleText は素のテキストのスパンだけ style を差し替える (引用の dim 化などに使う)。
func restyleText(spans []span, to style) []span {
	for i := range spans {
		if spans[i].Style == styleText {
			spans[i].Style = to
		}
	}
	return spans
}
