package lint

import (
	"fmt"
	"slices"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pystr"
	"ja-writing/visibility"
)

// 出力の形式の定数 (本家の LINT_FORMAT_VERSION と __version__)。キーを足すだけなら formatVersion は上げない。
const (
	formatVersion = 2
	toolVersion   = visibility.Version
)

// value は --json に出す結果の全体。鍵の順は本家の lint_text の戻り値の dict の順。
func (r *result) value() pyjson.Value {
	counts := pyjson.NewObject()
	counts.Set("error", int64(r.counts[0]))
	counts.Set("warn", int64(r.counts[1]))
	counts.Set("info", int64(r.counts[2]))
	context := pyjson.NewObject()
	context.Set("doc_type", optionalString(r.docType))
	context.Set("reader", optionalString(r.reader))
	context.Set("skipped_by_doc_type", skipValue(r.skipped))
	if r.words != nil {
		context.Set("words", r.words.value())
	}
	findings := make([]pyjson.Value, len(r.findings))
	for i, f := range r.findings {
		findings[i] = f.publicValue()
	}
	o := pyjson.NewObject()
	o.Set("format", "yomiyasu-lint")
	o.Set("format_version", int64(formatVersion))
	o.Set("tool_version", toolVersion)
	o.Set("candidate_count", int64(r.candidateCount))
	o.Set("counts", counts)
	o.Set("no_rule_findings", r.candidateCount == 0)
	o.Set("rule_penalty_score", int64(r.penaltyScore))
	o.Set("not_judged", []pyjson.Value{"meaning_preservation", "reader_comprehension"})
	o.Set("context", context)
	o.Set("metrics", r.metrics.value())
	o.Set("findings", findings)
	return o
}

func optionalString(s *string) (v pyjson.Value) {
	if s != nil {
		v = *s
	}
	return v
}

func skipValue(items []skipCount) pyjson.Value {
	o := pyjson.NewObject()
	for _, s := range items {
		o.Set(s.rule, int64(s.count))
	}
	return o
}

// value は words_context の 5 つか 7 つの鍵 (この順) のあとに allow_removed を足したもの。
func (w *wordsReport) value() pyjson.Value {
	c := w.context
	o := pyjson.NewObject()
	o.Set("path", c.Path)
	o.Set("allow_lines", int64(c.AllowLines))
	o.Set("flag_lines", int64(c.FlagLines))
	o.Set("allow_sha256", optionalString(c.AllowSHA256))
	o.Set("flag_sha256", optionalString(c.FlagSHA256))
	if c.PatternsSHA256 != nil {
		o.Set("flag_patterns_lines", int64(c.PatternsLines))
		o.Set("flag_patterns_sha256", *c.PatternsSHA256)
	}
	o.Set("allow_removed", skipValue(w.allowRemoved))
	return o
}

// renderReport は通常の出力の全体 (1〜3 行目、種類か読者を指定したときの 4 行目、語の一覧を使ったときの行、候補があれば内訳と候補の並び)。末尾に改行は付けない。
func renderReport(r *result) string {
	header := fmt.Sprintf("表現・書式の検査を実行しました。\n見直し候補: %d件\nこの検査では、意味の保持と読者にとっての分かりやすさは判定していません。", r.candidateCount)
	for _, line := range []string{r.contextLine(), r.wordsLine()} {
		if line != "" {
			header += "\n" + line
		}
	}
	if body := renderFindings(r); body != "" {
		return header + "\n\n" + body
	}
	return header
}

// contextLine は種類か読者を指定したときの 4 行目。指定しないときは空。
func (r *result) contextLine() string {
	if r.docType == nil && r.reader == nil {
		return ""
	}
	line, err := visibility.ContextLabel(r.docType, r.reader)
	if err != nil {
		panic(err) // 値は CLI が正規化済み
	}
	if r.docType != nil {
		if len(r.skipped) == 0 {
			line += "この種類で外す規則はありません。"
		} else {
			total := 0
			parts := make([]string, len(r.skipped))
			for i, s := range r.skipped {
				total += s.count
				parts[i] = fmt.Sprintf("%s %d件", s.rule, s.count)
			}
			line += fmt.Sprintf("種類に合わせて外した候補 %d件（%s）。", total, strings.Join(parts, "、"))
		}
	}
	if r.reader != nil {
		line += "読者では候補を変えていません。"
	}
	return line
}

// wordsLine は語の一覧を使ったときの行。使わないときは空。
func (r *result) wordsLine() string {
	if r.words == nil {
		return ""
	}
	total := 0
	for _, s := range r.words.allowRemoved {
		total += s.count
	}
	return visibility.WordsLabel(r.words.context) + fmt.Sprintf("allow で外した候補 %d件。", total)
}

// renderFindings は内訳の行と候補の並び (候補が 0 件なら空)。候補は行番号の順に並べ、同じ行の中は検出の順のまま。
func renderFindings(r *result) string {
	if len(r.findings) == 0 {
		return ""
	}
	blocks := []string{fmt.Sprintf("内訳: error %d件、warn %d件、info %d件", r.counts[0], r.counts[1], r.counts[2])}
	for _, f := range slices.SortedStableFunc(slices.Values(r.findings), func(a, b finding) int { return a.Line - b.Line }) {
		lines := []string{fmt.Sprintf("L%d [%s] %s: %s", f.Line, strings.ToUpper(f.Severity), f.Rule, f.Message)}
		for _, snippetLine := range strings.Split(f.Snippet, "\n") {
			if snippetLine = pystr.RStrip(snippetLine); snippetLine != "" {
				lines = append(lines, "  > "+snippetLine)
			}
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	return strings.Join(blocks, "\n\n")
}

// dumpResult は json.dumps(result, ensure_ascii=True, indent=2)。
func dumpResult(r *result) (string, error) {
	return pyjson.Dumps(r.value(), pyjson.Options{Indented: true, Indent: 2})
}
