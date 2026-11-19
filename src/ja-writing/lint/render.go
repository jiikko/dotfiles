package lint

import (
	"fmt"
	"slices"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pystr"
	"ja-writing/visibility"
)

// check が lint から使う口 (本家 check の _lint_module() が取る名前)。どれも --json の dict (lint_text の戻り値と同じ形) を読む。

// LintFormatVersion は本家の LINT_FORMAT_VERSION (--json の format_version)。
const LintFormatVersion = formatVersion

func objInt(o *pyjson.Object, key string) int {
	n, _ := o.Field(key).(int64)
	return int(n)
}

func objStr(o *pyjson.Object, key string) string {
	s, _ := o.Field(key).(string)
	return s
}

func objPtr(o *pyjson.Object, key string) *string {
	s, ok := o.Field(key).(string)
	if !ok {
		return nil
	}
	return &s
}

// RenderReport は本家の render_report (1〜3 行目、種類か読者を指定したときの 4 行目、語の一覧を使ったときの行、候補があれば内訳と候補の並び)。末尾に改行は付けない。
func RenderReport(d *pyjson.Object) string {
	header := fmt.Sprintf("表現・書式の検査を実行しました。\n見直し候補: %d件\nこの検査では、意味の保持と読者にとっての分かりやすさは判定していません。", objInt(d, "candidate_count"))
	context, _ := d.Field("context").(*pyjson.Object)
	for _, line := range []string{contextLine(context), wordsLine(context)} {
		if line != "" {
			header += "\n" + line
		}
	}
	if body := RenderFindings(d); body != "" {
		return header + "\n\n" + body
	}
	return header
}

// contextLine は本家の _context_line (種類か読者を指定したときの 4 行目。指定しないときは空)。
func contextLine(context *pyjson.Object) string {
	docType, reader := objPtr(context, "doc_type"), objPtr(context, "reader")
	if docType == nil && reader == nil {
		return ""
	}
	line, err := visibility.ContextLabel(docType, reader)
	if err != nil {
		panic(err) // 値は CLI が正規化済み
	}
	if docType != nil {
		skipped, _ := context.Field("skipped_by_doc_type").(*pyjson.Object)
		if skipped == nil || skipped.Len() == 0 {
			line += "この種類で外す規則はありません。"
		} else {
			total := 0
			var parts []string
			for _, rule := range skipped.Keys() {
				n := objInt(skipped, rule)
				total += n
				parts = append(parts, fmt.Sprintf("%s %d件", rule, n))
			}
			line += fmt.Sprintf("種類に合わせて外した候補 %d件（%s）。", total, strings.Join(parts, "、"))
		}
	}
	if reader != nil {
		line += "読者では候補を変えていません。"
	}
	return line
}

// wordsLine は本家の _words_line (語の一覧を使ったときの行。使わないときは空)。
func wordsLine(context *pyjson.Object) string {
	words, ok := context.Field("words").(*pyjson.Object)
	if !ok {
		return ""
	}
	total := 0
	if removed, ok := words.Field("allow_removed").(*pyjson.Object); ok {
		for _, rule := range removed.Keys() {
			total += objInt(removed, rule)
		}
	}
	return visibility.WordsLabel(visibility.WordsContextFromValue(words)) + fmt.Sprintf("allow で外した候補 %d件。", total)
}

// RenderFindings は本家の render_findings (内訳の行と候補の並び。候補が 0 件なら空)。候補は行番号の順に並べ、同じ行の中は検出の順のまま。
func RenderFindings(d *pyjson.Object) string {
	findings, _ := d.Field("findings").([]pyjson.Value)
	if len(findings) == 0 {
		return ""
	}
	counts, _ := d.Field("counts").(*pyjson.Object)
	blocks := []string{fmt.Sprintf("内訳: error %d件、warn %d件、info %d件", objInt(counts, "error"), objInt(counts, "warn"), objInt(counts, "info"))}
	items := make([]*pyjson.Object, 0, len(findings))
	for _, f := range findings {
		if o, ok := f.(*pyjson.Object); ok {
			items = append(items, o)
		}
	}
	slices.SortStableFunc(items, func(a, b *pyjson.Object) int { return objInt(a, "line") - objInt(b, "line") })
	for _, f := range items {
		lines := []string{fmt.Sprintf("L%d [%s] %s: %s", objInt(f, "line"), strings.ToUpper(objStr(f, "severity")), objStr(f, "rule"), objStr(f, "message"))}
		for _, snippetLine := range strings.Split(objStr(f, "snippet"), "\n") {
			if snippetLine = pystr.RStrip(snippetLine); snippetLine != "" {
				lines = append(lines, "  > "+snippetLine)
			}
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	return strings.Join(blocks, "\n\n")
}
