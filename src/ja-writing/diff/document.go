package diff

import (
	"slices"

	"ja-writing/internal/bold"
	"ja-writing/internal/pyjson"
	"ja-writing/visibility"
)

// 群 AC: 本家の diff() (JSON の形式 2 の組み立て)。キーの順は本家の辞書の挿入の順で、--json の出力の順になる (記録の突き合わせは鍵を並べ替えるので、順は CLI の突き合わせで守る)。

// failOnKinds は --fail-on=meaning-risk が数える恐れの kind (本家の FAIL_ON_KINDS。このほか unsupported の number)。
var failOnKinds = []string{"number", "negation", "swap", "sentence_type", "citation", "certainty", "state", "promise", "modality", "choice", "causality", "number_qualifier"}

func failOnCount(risks []*pyjson.Object) int {
	n := 0
	for _, r := range risks {
		if slices.Contains(failOnKinds, riskKind(r)) || (riskKind(r) == "unsupported" && riskString(r, "subkind") == "number") {
			n++
		}
	}
	return n
}

func typeKindHitsValue(list []KindHits) pyjson.Value {
	return pyjson.MapList(list, func(k KindHits) pyjson.Value {
		o := pyjson.NewObject()
		o.Set("kind", k.Kind)
		o.Set("hits", pyjson.Strings(k.Hits))
		return o
	})
}

func endingFlagsOf(flags []EndingFlag) pyjson.Value {
	return pyjson.MapList(flags, func(f EndingFlag) pyjson.Value {
		o := pyjson.NewObject()
		o.Set("note", f.Note)
		o.Set("sentences", pyjson.Strings(f.Sentences))
		return o
	})
}

// ruleCandidatesInto は _rule_candidates の 9 つの点検を、本家の辞書の挿入の順で out に足す。
func ruleCandidatesInto(out *pyjson.Object, c *RuleCandidates) {
	out.Set("markers", pyjson.MapList(c.Markers, func(m MarkerChange) pyjson.Value {
		o := pyjson.NewObject()
		o.Set("kind", m.Kind)
		o.Set("orig", int64(m.Orig))
		o.Set("rewrite", int64(m.Rewrite))
		o.Set("orig_hits", pyjson.Strings(m.OrigHits))
		o.Set("rewrite_hits", pyjson.Strings(m.RewriteHits))
		return o
	}))
	out.Set("new_words", pyjson.Strings(c.NewWords))
	out.Set("lost_words", pyjson.Strings(c.LostWords))
	out.Set("structure", pyjson.Strings(c.Structure))
	out.Set("spans", pyjson.MapList(c.Spans, func(sp SpanChange) pyjson.Value {
		o := pyjson.NewObject()
		o.Set("added", sp.Added)
		o.Set("was", sp.Was)
		o.Set("kinds", pyjson.Strings(sp.Kinds))
		o.Set("new_words", pyjson.Strings(sp.NewWords))
		o.Set("where", sp.Where)
		return o
	}))
	out.Set("logic", pyjson.MapList(c.Logic, func(p LogicPoint) pyjson.Value {
		o := pyjson.NewObject()
		o.Set("kind", p.Kind)
		o.Set("sentence", p.Sentence)
		return o
	}))
	out.Set("bold", pyjson.MapList(c.Bold, func(p bold.Problem) pyjson.Value {
		o := pyjson.NewObject()
		o.Set("line", int64(p.Line))
		o.Set("found", p.Found)
		o.Set("suggest", p.Suggest)
		o.Set("how", p.How)
		return o
	}))
	endings := pyjson.NewObject()
	if c.Endings.Stance == "" {
		endings.Set("stance", nil)
	} else {
		endings.Set("stance", c.Endings.Stance)
	}
	endings.Set("changes", pyjson.MapList(c.Endings.Changes, func(e EndingChange) pyjson.Value {
		o := pyjson.NewObject()
		o.Set("orig", e.Orig)
		o.Set("orig_kind", e.OrigKind)
		o.Set("rewrite", e.Rewrite)
		o.Set("rewrite_kind", e.RewriteKind)
		return o
	}))
	endings.Set("flags", endingFlagsOf(c.Endings.Flags))
	endings.Set("orig_flags", endingFlagsOf(c.Endings.OrigFlags))
	out.Set("endings", endings)
	out.Set("type_changes", pyjson.MapList(c.TypeChanges, func(t TypeChange) pyjson.Value {
		o := pyjson.NewObject()
		o.Set("orig", t.Orig)
		o.Set("rewrite", t.Rewrite)
		o.Set("lost", typeKindHitsValue(t.Lost))
		o.Set("added", typeKindHitsValue(t.Added))
		return o
	}))
}

// ruleCount は規則の見直し候補の数 (本家の _rule_count。レポートの行の数に合わせる)。rewrittenOnly は書き直した文だけを見る候補 (logic・bold・endings.flags) も数えるか。
func ruleCount(c *RuleCandidates, rewrittenOnly bool) int {
	count := len(c.Markers) + len(c.Structure) + len(c.Endings.Changes) + len(c.TypeChanges)
	if len(c.NewWords) > 0 {
		count++
	}
	if len(c.LostWords) > 0 {
		count++
	}
	if rewrittenOnly {
		count += len(c.Logic) + len(c.Endings.Flags) + len(c.Bold)
	}
	return count
}

// diffDocument は本家の diff(orig_raw, rw_raw, stance, encoding_notes, doc_type, reader, words, materials)。渡された文字列をそのまま使う。
// 型の寿命: 範囲づくりの結果 (typeCache) は 1 回の呼び出しの中だけで使い回し、次の文書には持ち越さない (本家は finally でグローバルを空に戻す)。
func diffDocument(req request) (doc *pyjson.Object, err error) {
	defer recoverPyException(&err)
	context := pyjson.NewObject()
	context.Set("doc_type", pyjson.OptionalString(req.docType))
	context.Set("reader", pyjson.OptionalString(req.reader))
	if req.words != nil {
		context.Set("words", visibility.WordsContextOf(req.words).Value())
	}
	docType := ""
	if req.docType != nil {
		docType = *req.docType
	}
	cache := &typeCache{}
	rules, err := RuleCandidatesOf(req.old, req.new, req.stance, cache)
	if err != nil {
		return nil, err
	}
	o, r := Normalize(req.old), Normalize(req.new)
	modality, logic := ModalityChanges(o, r, cache), LogicChanges(o, r, cache)
	full, s, err := buildFullDiff(req.old, req.new, req.notes)
	if err != nil {
		return nil, err
	}
	shapes := []HeadingShape{}
	rules.Structure = append(rules.Structure, headingStructure(s, full.hunks, &shapes)...)
	groups := s.sentencePairs(full.hunks)
	result := s.meaningRisks(full, groups, rules.TypeChanges, docType, modality, logic)
	risks := result.risks

	status, count := "no_change", 0
	if req.old != req.new || len(full.encodingNotes) > 0 {
		for _, risk := range risks {
			if riskKind(risk) != "sentence_type" || riskString(risk, "subkind") != "marks" {
				count++
			}
		}
		count += ruleCount(rules, len(full.hunks) > 0)
		status = "changed_without_candidates"
		if count > 0 {
			status = "has_candidates"
		}
	}
	out := pyjson.NewObject()
	out.Set("format", "yomiyasu-diff")
	out.Set("format_version", int64(formatVersion))
	out.Set("tool_version", visibility.Version)
	out.Set("status", status)
	out.Set("changed", status != "no_change")
	out.Set("candidate_count", int64(count))
	out.Set("meaning_risk_count", int64(len(risks)))
	out.Set("fail_on_meaning_risk_count", int64(failOnCount(risks)))
	out.Set("not_judged", pyjson.Strings([]string{"meaning_equivalence", "reader_comprehension"}))
	out.Set("context", context)
	out.Set("meaning_risks", riskObjectsValue(risks))
	ruleCandidatesInto(out, rules)
	out.Set("full_diff", full.value())
	dt := req.docType
	out.Set("stiffness", s.stiffness(full, groups, dt))
	tone, err := s.tone(full, dt, groups)
	if err != nil {
		return nil, err
	}
	out.Set("tone", tone)
	feeling := pyjson.NewObject()
	feeling.Set("list", ListSource("feelings"))
	feeling.Set("list_problem", nil)
	out.Set("feeling", feeling)
	out.Set("hedges", s.hedges(groups, slices.Concat(risks, result.quiet)))
	out.Set("immutable", result.immutable)
	politeness := s.politeness(docType)
	out.Set("politeness", politeness)
	out.Set("register", s.registerChanges(groups, politeness))
	out.Set("added_actors", s.addedActors(groups, req.materials))
	out.Set("heading_shapes", pyjson.MapList(shapes, HeadingShape.Value))
	out.Set("comma_increase", s.commaIncrease(full, groups))
	return out, nil
}
