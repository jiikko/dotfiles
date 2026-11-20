package lint

import (
	"ja-writing/internal/pyjson"
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
	context.Set("doc_type", pyjson.OptionalString(r.docType))
	context.Set("reader", pyjson.OptionalString(r.reader))
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

func skipValue(items []skipCount) pyjson.Value {
	o := pyjson.NewObject()
	for _, s := range items {
		o.Set(s.rule, int64(s.count))
	}
	return o
}

// value は words_context の 5 つか 7 つの鍵 (この順) のあとに allow_removed を足したもの。
func (w *wordsReport) value() pyjson.Value {
	o := w.context.Value()
	o.Set("allow_removed", skipValue(w.allowRemoved))
	return o
}

// renderReport は通常の出力の全体。--json の dict と同じ値を RenderReport に通す (check が子の lint --json を読んで使う口と同じ実装)。
func renderReport(r *result) (string, error) { return RenderReport(r.value().(*pyjson.Object)) }

// dumpResult は json.dumps(result, ensure_ascii=True, indent=2)。
func dumpResult(r *result) (string, error) {
	return pyjson.Dumps(r.value(), pyjson.Options{Indented: true, Indent: 2})
}
