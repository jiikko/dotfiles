package diff

import (
	"fmt"
	"strconv"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pyunicode"
)

// 群 AD: 全文差分の節 (本家の _full_diff_section・_diff_line・_short_diff・_hunk_heading・_visible)。

const reportLongLine = 160 // これを超える行の「差:」は、変わった文のまわりだけを出す (本家の _LONG_LINE)

var blockNames = map[string]string{
	"paragraph": "本文", "list": "箇条書き", "quote": "引用", "heading": "見出し", "heading_underline": "見出し",
	"table": "表", "code": "コード", "frontmatter": "先頭の設定", "html_block": "HTML",
	"reference_definition": "リンクの定義", "blank": "空行", "thematic_break": "区切り線",
}

var eolNames = map[string]string{"lf": "LF", "crlf": "CRLF", "cr": "CR", "mixed": "混在"}

var visibleNames = map[rune]string{' ': "〔空白〕", '\u3000': "〔全角空白〕", '\t': "〔タブ〕", '\n': "〔改行〕", '\ufeff': "〔BOM〕"}

// visibleText は _visible (「差:」の印の中の見えない字を名前にする。空白は〔空白 U+00A0〕の形、幅のない書式の字 (分類 Cf) は〔U+200B〕の形)。
func visibleText(text string) string {
	var out strings.Builder
	for _, ch := range pyunicode.Runes(text) {
		if name, ok := visibleNames[ch]; ok {
			out.WriteString(name)
		} else if pyunicode.IsSpace(ch) {
			fmt.Fprintf(&out, "〔空白 U+%04X〕", ch)
		} else if pyunicode.Category(ch) == "Cf" {
			fmt.Fprintf(&out, "〔U+%04X〕", ch)
		} else {
			out.WriteString(pyunicode.FromRunes([]rune{ch}))
		}
	}
	return out.String()
}

func marked(tag, text string) string {
	if tag == "-" {
		return "[-" + visibleText(text) + "-]"
	}
	return "{+" + visibleText(text) + "+}"
}

// showLine は _show_line (「元:」「後:」の行の中身。空の行は〔空行〕、空白だけの行は見える名前)。
func showLine(line string) string {
	if line == "" {
		return "〔空行〕"
	}
	if isBlank(line) {
		return visibleText(line)
	}
	return line
}

type reportSegment struct{ tag, text string }

func segmentsOf(v pyjson.Value) []reportSegment {
	list, _ := v.([]pyjson.Value)
	out := make([]reportSegment, len(list))
	for i, s := range list {
		pair, _ := s.([]pyjson.Value)
		out[i] = reportSegment{pyStr(pair[0]), pyStr(pair[1])}
	}
	return out
}

// diffRows は _diff_rows (印の外の改行で行を分ける。印の中の改行は〔改行〕)。
func diffRows(segments []reportSegment) []string {
	rows := []string{""}
	for _, s := range segments {
		if s.tag == "=" {
			pieces := strings.Split(s.text, "\n")
			rows[len(rows)-1] += pieces[0]
			rows = append(rows, pieces[1:]...)
		} else {
			rows[len(rows)-1] += marked(s.tag, s.text)
		}
	}
	return rows
}

// shortDiff は _short_diff (長い行の「差:」。印の外の文の切れ目で文に分け、変わった文だけを残し、変わらない文の並びは「…」にする)。
func shortDiff(segments []reportSegment) string {
	type unit struct {
		changed bool
		text    string
	}
	units := []*unit{{}}
	for _, s := range segments {
		last := units[len(units)-1]
		if s.tag != "=" {
			last.changed = true
			last.text += marked(s.tag, s.text)
			continue
		}
		runes := pyunicode.Runes(s.text)
		position := 0
		for _, m := range mustFinditer(sentenceEnd, pyre.NewText(s.text), 0, pyre.NoEnd) {
			units[len(units)-1].text += pyunicode.FromRunes(runes[position:m.End(0)])
			units = append(units, &unit{})
			position = m.End(0)
		}
		units[len(units)-1].text += pyunicode.FromRunes(runes[position:])
	}
	var out []string
	skipped, omitted := false, false
	for _, u := range units {
		if u.text == "" {
			continue
		}
		if u.changed {
			out = append(out, u.text)
			skipped = false
		} else if !skipped {
			out = append(out, "…")
			skipped, omitted = true, true
		}
	}
	if !omitted {
		return strings.Join(out, "")
	}
	return strings.Join(out, "") + "（変わらない文は省略。元と後は上に全文）"
}

// diffLine は _diff_line (1 行が 1 行に変わった塊の「差:」。出さないときは ok が偽)。
func diffLine(h *pyjson.Object) (string, bool) {
	oldLines, newLines := fieldList(h, "old_lines"), fieldList(h, "new_lines")
	if fieldStr(h, "tag") != "replace" || field(h, "segments") == nil || len(oldLines) != 1 || len(newLines) != 1 {
		return "", false
	}
	segments := segmentsOf(field(h, "segments"))
	places, inside, changed := 0, false, 0
	for _, s := range segments {
		if s.tag == "=" {
			inside = false
			continue
		}
		if !inside {
			places++
			inside = true
		}
		changed += pyunicode.RuneCount(s.text)
	}
	oldLine, newLine := pyStr(oldLines[0]), pyStr(newLines[0])
	// 変わった箇所が 5 か所以上か、消した字と足した字の合計が元の行の半分を超えるときは、元と後で読むほうが速い
	if places >= 5 || float64(changed) > float64(pyunicode.RuneCount(oldLine))/2 {
		return "", false
	}
	if pyunicode.RuneCount(oldLine) > reportLongLine || pyunicode.RuneCount(newLine) > reportLongLine {
		return shortDiff(segments), true
	}
	return diffRows(segments)[0], true
}

func rangeText(start, end int) string {
	if start == end {
		return strconv.Itoa(start)
	}
	return fmt.Sprintf("%d-%d", start, end)
}

func blockKinds(v pyjson.Value) string {
	var names []string
	seen := map[string]bool{}
	for _, k := range strList(v) {
		name, ok := blockNames[k]
		if !ok {
			name = "その他"
		}
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return strings.Join(names, "・")
}

// hunkHeading は _hunk_heading。
func hunkHeading(h *pyjson.Object) string {
	oldKinds, newKinds := blockKinds(field(h, "old_blocks")), blockKinds(field(h, "new_blocks"))
	var head, kinds string
	switch fieldStr(h, "tag") {
	case "insert":
		head, kinds = fmt.Sprintf("@ 後%s行目を追加", rangeText(fieldInt(h, "new_start"), fieldInt(h, "new_end"))), newKinds
	case "delete":
		head, kinds = fmt.Sprintf("@ 元%s行目を削除", rangeText(fieldInt(h, "old_start"), fieldInt(h, "old_end"))), oldKinds
	default:
		head = fmt.Sprintf("@ 元%s行目 → 後%s行目", rangeText(fieldInt(h, "old_start"), fieldInt(h, "old_end")), rangeText(fieldInt(h, "new_start"), fieldInt(h, "new_end")))
		kinds = oldKinds
		if oldKinds != newKinds {
			kinds = oldKinds + " → " + newKinds
		}
	}
	notes := []string{kinds}
	for _, n := range fieldList(h, "notes") {
		note := asObj(n)
		switch fieldStr(note, "kind") {
		case "whitespace_only":
			notes = append(notes, "空白だけ")
		case "heading_level":
			notes = append(notes, fmt.Sprintf("見出しのレベル %s → %s", pyStr(field(note, "old")), pyStr(field(note, "new"))))
		case "moved_to":
			notes = append(notes, fmt.Sprintf("移した行。後%d行目へ", fieldInt(note, "line")))
		case "moved_from":
			notes = append(notes, fmt.Sprintf("移した行。元%d行目から", fieldInt(note, "line")))
		}
	}
	return head + "（" + strings.Join(notes, "、") + "）"
}

func yes(v pyjson.Value) string {
	if truthy(v) {
		return "あり"
	}
	return "なし"
}

func eolName(v pyjson.Value) string {
	if name, ok := eolNames[pyStr(v)]; ok {
		return name
	}
	return pyStr(v)
}

// fullDiffSection は _full_diff_section。
func fullDiffSection(d *pyjson.Object) string {
	full := fieldObj(d, "full_diff")
	hunks := fieldList(full, "hunks")
	var notes []string
	for _, n := range fieldList(full, "encoding_notes") {
		note := asObj(n)
		switch fieldStr(note, "kind") {
		case "eol":
			notes = append(notes, fmt.Sprintf("- 改行コード: %s → %s（行ごとの差には出していません）", eolName(field(note, "old")), eolName(field(note, "new"))))
		case "bom":
			notes = append(notes, fmt.Sprintf("- 先頭のBOM: %s → %s", yes(field(note, "old")), yes(field(note, "new"))))
		}
	}
	finalNewline := fieldObj(full, "final_newline")
	if truthy(field(finalNewline, "old")) != truthy(field(finalNewline, "new")) {
		notes = append(notes, fmt.Sprintf("- 末尾の改行: %s → %s", yes(field(finalNewline, "old")), yes(field(finalNewline, "new"))))
	}
	if len(hunks) == 0 && len(notes) == 0 {
		return "■ 全文差分: 変わった箇所はありません。"
	}
	lines := append([]string{fmt.Sprintf("■ 全文差分（変更 %dか所。省略していません）", len(hunks))}, notes...)
	for _, hv := range hunks {
		h := asObj(hv)
		lines = append(lines, hunkHeading(h))
		if truthy(field(h, "whitespace_only")) && fieldStr(h, "tag") == "replace" && field(h, "segments") != nil {
			// 空白だけが違う行の組は、元と後が同じに見えるので、差だけを出す (5 か所・半分の条件を当てない)
			for _, row := range diffRows(segmentsOf(field(h, "segments"))) {
				lines = append(lines, "  差: "+row)
			}
			continue
		}
		for _, l := range strList(field(h, "old_lines")) {
			lines = append(lines, "  元: "+showLine(l))
		}
		for _, l := range strList(field(h, "new_lines")) {
			lines = append(lines, "  後: "+showLine(l))
		}
		if row, ok := diffLine(h); ok {
			lines = append(lines, "  差: "+row)
		}
	}
	return strings.Join(lines, "\n")
}
