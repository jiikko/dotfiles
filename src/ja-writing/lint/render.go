package lint

import (
	"fmt"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// check が lint から使う口 (本家 check の _lint_module() が取る名前)。どれも --json の dict (lint_text の戻り値と同じ形) を読む。

// LintFormatVersion は本家の LINT_FORMAT_VERSION (--json の format_version)。
const LintFormatVersion = formatVersion

// RenderReport は本家の render_report (1〜3 行目、種類か読者を指定したときの 4 行目、語の一覧を使ったときの行、候補があれば内訳と候補の並び)。末尾に改行は付けない。
// 本家が例外になる dict (欄が欠ける・型が違う) は error (check はそれを「子の JSON から人が読む形を作れません」にする)。
func RenderReport(d *pyjson.Object) (out string, err error) {
	defer pyops.Catch(&err)
	header := "表現・書式の検査を実行しました。\n見直し候補: " + pyops.Format(pyops.Index(d, "candidate_count")) +
		"件\nこの検査では、意味の保持と読者にとっての分かりやすさは判定していません。"
	for _, line := range []string{contextLine(d), wordsLine(d)} {
		if line != "" {
			header += "\n" + line
		}
	}
	if body := renderFindings(d); body != "" {
		return header + "\n\n" + body, nil
	}
	return header, nil
}

// contextOf は本家の result.get("context") or {}。
func contextOf(d *pyjson.Object) pyjson.Value {
	return pyops.Or(pyops.Get(d, "context"), pyops.EmptyDict())
}

// contextLine は本家の _context_line (種類か読者を指定したときの 4 行目。指定しないときは空)。
func contextLine(d *pyjson.Object) string {
	context := contextOf(d)
	docType, reader := pyops.Get(context, "doc_type"), pyops.Get(context, "reader")
	if docType == nil && reader == nil {
		return ""
	}
	line := visibility.ContextLabelOf(docType, reader)
	if docType != nil {
		skipped := pyops.Or(pyops.Get(context, "skipped_by_doc_type"), pyops.EmptyDict())
		if pyops.Truthy(skipped) {
			total := pyops.Format(pyops.Sum(pyops.Values(skipped)))
			var parts []string
			for _, rule := range pyops.Iter(skipped) {
				parts = append(parts, pyops.Format(rule)+" "+pyops.Format(pyops.Index(skipped, rule.(string)))+"件")
			}
			line += "種類に合わせて外した候補 " + total + "件（" + strings.Join(parts, "、") + "）。"
		} else {
			line += "この種類で外す規則はありません。"
		}
	}
	if reader != nil {
		line += "読者では候補を変えていません。"
	}
	return line
}

// wordsLine は本家の _words_line (語の一覧を使ったときの行。使わないときは空)。
func wordsLine(d *pyjson.Object) string {
	words, ok := pyops.Get(contextOf(d), "words").(*pyjson.Object)
	if !ok {
		return ""
	}
	removed := pyops.Or(pyops.Get(words, "allow_removed"), pyops.EmptyDict())
	return visibility.WordsLabelOf(words) + "allow で外した候補 " + pyops.Format(pyops.Sum(pyops.Values(removed))) + "件。"
}

// RenderFindings は本家の render_findings (内訳の行と候補の並び。候補が 0 件なら空)。候補は行番号の順に並べ、同じ行の中は検出の順のまま。
// 本家が例外になる dict は error。
func RenderFindings(d *pyjson.Object) (out string, err error) {
	defer pyops.Catch(&err)
	return renderFindings(d), nil
}

func renderFindings(d *pyjson.Object) string {
	findings := pyops.Index(d, "findings")
	if !pyops.Truthy(findings) {
		return ""
	}
	counts := pyops.Index(d, "counts")
	blocks := []string{fmt.Sprintf("内訳: error %s件、warn %s件、info %s件", pyops.Format(pyops.Index(counts, "error")),
		pyops.Format(pyops.Index(counts, "warn")), pyops.Format(pyops.Index(counts, "info")))}
	sorted := pyops.SortedBy(pyops.Iter(findings), func(f pyjson.Value) pyjson.Value { return pyops.Index(f, "line") })
	for _, f := range sorted {
		line := pyops.Format(pyops.Index(f, "line"))
		severity := pyunicode.Upper(pyops.Str(pyops.Index(f, "severity")))
		lines := []string{fmt.Sprintf("L%s [%s] %s: %s", line, severity, pyops.Format(pyops.Index(f, "rule")), pyops.Format(pyops.Index(f, "message")))}
		for _, snippetLine := range strings.Split(pyops.Format(pyops.Index(f, "snippet")), "\n") {
			if snippetLine = pystr.RStrip(snippetLine); snippetLine != "" {
				lines = append(lines, "  > "+snippetLine)
			}
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	return strings.Join(blocks, "\n\n")
}
