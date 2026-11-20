package diff

import (
	"fmt"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
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

type reportSegment struct{ tag, text pyjson.Value }

// segmentsOf は本家の for tag, text in segments の組 (要素がちょうど 2 つでなければ ValueError)。
func segmentsOf(v pyjson.Value) []reportSegment {
	items := pyops.Iter(v)
	out := make([]reportSegment, len(items))
	for i, s := range items {
		out[i].tag, out[i].text = pyops.Unpack2(s)
	}
	return out
}

// visibleOf は本家の _visible(text) を dict の値に当てる (text は字の並び。str のほか、list・dict も 1 字ずつの str の並びなら通る)。
func visibleOf(text pyjson.Value) string {
	if s, ok := text.(string); ok {
		return visibleText(s)
	}
	var out strings.Builder
	for _, ch := range pyops.Iter(text) {
		pyops.Hashable(ch)
		s, ok := ch.(string)
		if !ok || pyunicode.RuneCount(s) != 1 {
			pyops.Raise("TypeError", "expected a character, but %s found", pyops.TypeName(ch))
		}
		out.WriteString(visibleText(s))
	}
	return out.String()
}

func marked(tag, text pyjson.Value) string {
	if pyops.Equal(tag, "-") {
		return "[-" + visibleOf(text) + "-]"
	}
	return "{+" + visibleOf(text) + "+}"
}

// diffRows は _diff_rows (印の外の改行で行を分ける。印の中の改行は〔改行〕)。
func diffRows(segments []reportSegment) []string {
	rows := []string{""}
	for _, s := range segments {
		if pyops.Equal(s.tag, "=") {
			pieces := strings.Split(pyops.Str(s.text), "\n")
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
		if !pyops.Equal(s.tag, "=") {
			last.changed = true
			last.text += marked(s.tag, s.text)
			continue
		}
		text := pyops.Str(s.text)
		runes := pyunicode.Runes(text)
		position := 0
		for _, m := range mustFinditer(sentenceEnd, pyre.NewText(text), 0, pyre.NoEnd) {
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
func diffLine(h pyjson.Value) (string, bool) {
	segmentsValue := idx(h, "segments")
	if !pyops.Equal(idx(h, "tag"), "replace") || segmentsValue == nil || pyops.Len(idx(h, "old_lines")) != 1 || pyops.Len(idx(h, "new_lines")) != 1 {
		return "", false
	}
	segments := segmentsOf(segmentsValue)
	places, inside, changed := 0, false, 0
	for _, s := range segments {
		if pyops.Equal(s.tag, "=") {
			inside = false
			continue
		}
		if !inside {
			places++
			inside = true
		}
		changed += pyops.Len(s.text)
	}
	oldLine, newLine := pyops.At(idx(h, "old_lines"), 0), pyops.At(idx(h, "new_lines"), 0)
	// 変わった箇所が 5 か所以上か、消した字と足した字の合計が元の行の半分を超えるときは、元と後で読むほうが速い
	if places >= 5 || float64(changed) > float64(pyops.Len(oldLine))/2 {
		return "", false
	}
	if pyops.Len(oldLine) > reportLongLine || pyops.Len(newLine) > reportLongLine {
		return shortDiff(segments), true
	}
	return diffRows(segments)[0], true
}

// rangeText は _range (同じ行なら 1 つ、違えば「開始-終わり」)。
func rangeText(start, end pyjson.Value) string {
	if pyops.Equal(start, end) {
		return pyStr(start)
	}
	return pyStr(start) + "-" + pyStr(end)
}

func blockKinds(v pyjson.Value) string {
	var names []string
	seen := map[string]bool{}
	for _, k := range pyops.Iter(v) {
		name := pyStr(pyops.Lookup(blockNames, k, "その他"))
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return strings.Join(names, "・")
}

// hunkHeading は _hunk_heading。
func hunkHeading(h pyjson.Value) string {
	oldKinds, newKinds := blockKinds(idx(h, "old_blocks")), blockKinds(idx(h, "new_blocks"))
	var head, kinds string
	switch tag := idx(h, "tag"); {
	case pyops.Equal(tag, "insert"):
		head, kinds = "@ 後"+rangeText(idx(h, "new_start"), idx(h, "new_end"))+"行目を追加", newKinds
	case pyops.Equal(tag, "delete"):
		head, kinds = "@ 元"+rangeText(idx(h, "old_start"), idx(h, "old_end"))+"行目を削除", oldKinds
	default:
		head = "@ 元" + rangeText(idx(h, "old_start"), idx(h, "old_end")) + "行目 → 後" + rangeText(idx(h, "new_start"), idx(h, "new_end")) + "行目"
		kinds = oldKinds
		if oldKinds != newKinds {
			kinds = oldKinds + " → " + newKinds
		}
	}
	notes := []string{kinds}
	for _, note := range pyops.Iter(idx(h, "notes")) {
		switch kind := idx(note, "kind"); {
		case pyops.Equal(kind, "whitespace_only"):
			notes = append(notes, "空白だけ")
		case pyops.Equal(kind, "heading_level"):
			notes = append(notes, "見出しのレベル "+pyStr(idx(note, "old"))+" → "+pyStr(idx(note, "new")))
		case pyops.Equal(kind, "moved_to"):
			notes = append(notes, "移した行。後"+pyStr(idx(note, "line"))+"行目へ")
		case pyops.Equal(kind, "moved_from"):
			notes = append(notes, "移した行。元"+pyStr(idx(note, "line"))+"行目から")
		}
	}
	return head + "（" + strings.Join(notes, "、") + "）"
}

func yes(v pyjson.Value) string {
	if pyops.Truthy(v) {
		return "あり"
	}
	return "なし"
}

func eolName(v pyjson.Value) string { return pyStr(pyops.Lookup(eolNames, v, v)) }

// fullDiffSection は _full_diff_section。
func fullDiffSection(d *pyjson.Object) string {
	full := idx(d, "full_diff")
	hunks := idx(full, "hunks")
	var notes []string
	for _, note := range pyops.Iter(getList(full, "encoding_notes")) {
		switch kind := idx(note, "kind"); {
		case pyops.Equal(kind, "eol"):
			notes = append(notes, "- 改行コード: "+eolName(idx(note, "old"))+" → "+eolName(idx(note, "new"))+"（行ごとの差には出していません）")
		case pyops.Equal(kind, "bom"):
			notes = append(notes, "- 先頭のBOM: "+yes(idx(note, "old"))+" → "+yes(idx(note, "new")))
		}
	}
	finalNewline := idx(full, "final_newline")
	if !pyops.Equal(idx(finalNewline, "old"), idx(finalNewline, "new")) {
		notes = append(notes, "- 末尾の改行: "+yes(idx(finalNewline, "old"))+" → "+yes(idx(finalNewline, "new")))
	}
	if !pyops.Truthy(hunks) && len(notes) == 0 {
		return "■ 全文差分: 変わった箇所はありません。"
	}
	lines := append([]string{fmt.Sprintf("■ 全文差分（変更 %dか所。省略していません）", pyops.Len(hunks))}, notes...)
	for _, h := range pyops.Iter(hunks) {
		lines = append(lines, hunkHeading(h))
		if pyops.Truthy(idx(h, "whitespace_only")) && pyops.Equal(idx(h, "tag"), "replace") && idx(h, "segments") != nil {
			// 空白だけが違う行の組は、元と後が同じに見えるので、差だけを出す (5 か所・半分の条件を当てない)
			for _, row := range diffRows(segmentsOf(idx(h, "segments"))) {
				lines = append(lines, "  差: "+row)
			}
			continue
		}
		for _, l := range pyops.Iter(idx(h, "old_lines")) {
			lines = append(lines, "  元: "+showLine(pyops.Str(l)))
		}
		for _, l := range pyops.Iter(idx(h, "new_lines")) {
			lines = append(lines, "  後: "+showLine(pyops.Str(l)))
		}
		if row, ok := diffLine(h); ok {
			lines = append(lines, "  差: "+row)
		}
	}
	return strings.Join(lines, "\n")
}
