package check

import (
	"errors"
	"slices"
	"sort"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
	"ja-writing/internal/pyos"
	"ja-writing/internal/pyre"
)

// verify が記録の件数をレポートと照らす所 (本家の _summary_problems・_report_value・_kept_marks とそれが呼ぶもの)。

// keptMarkRules は _KEPT_MARK_RULES (規則の名前 → 人が読む名前と、message から語を取る正規表現)。
var keptMarkRules = map[string]struct {
	label   string
	pattern *pyre.Lazy
}{
	"calque":       {"直訳語", pyre.NewLazy(`当たった言い方: (.*)）の候補です`, 0)},
	"project_term": {"語の一覧", pyre.NewLazy(`語の一覧（flag(?:\.txt|-patterns\.txt|\.txt、flag-patterns\.txt)）の語（(.*)）の候補です`, 0)},
}

const (
	projectTermSuggestions = "。使う語: "
	projectTermPlainTail   = "）の候補です。文脈を見て、誰が何をどうするかを書き直すか確かめてください。"
	keptMarkShownLines     = 5
	keptMarkShownWords     = 10
)

// reportValue は _report_value (ハッシュを確かめた検査のレポートの値。読めない・ハッシュが合わない・JSON の dict でなければ nil)。
func reportValue(state string, entry pyjson.Value) *pyjson.Object {
	report, ok := pyops.Get(entry, "report").(string)
	if !ok || !strings.HasPrefix(report, "runs/") {
		return nil
	}
	data, err := pyos.ReadFile(pathParts(state, report))
	if err != nil || !pyops.Equal(SHA256(data), pyops.Get(entry, "report_sha256")) {
		return nil
	}
	value, err := pyjson.LoadsBytes(data)
	if err != nil {
		var loadErr *pyjson.Error
		if errors.As(err, &loadErr) && loadErr.IsValueError() {
			return nil
		}
		panic(failure{err})
	}
	o, _ := value.(*pyjson.Object)
	return o
}

// recordWords は _record_words (記録の options.words。dict でなければ nil)。
func recordWords(record *pyjson.Object) pyjson.Value {
	if words, ok := pyops.Get(pyops.Index(record, "options"), "words").(*pyjson.Object); ok {
		return words
	}
	return nil
}

// reportWords は _report_words (子の JSON の context.words から check が比べるキー。無いときは nil)。
func reportWords(context *pyjson.Object) pyjson.Value {
	words, ok := pyops.Get(context, "words").(*pyjson.Object)
	if !ok {
		return nil
	}
	keys := slices.Clone(wordsKeys)
	if slices.ContainsFunc(wordsPatternKeys, func(k string) bool { _, ok := words.Get(k); return ok }) {
		keys = append(keys, wordsPatternKeys...)
	}
	out := pyjson.NewObject()
	for _, key := range keys {
		out.Set(key, pyops.Get(words, key))
	}
	return out
}

// reviewCount は _review_count (見直し候補 (level が review) の数。候補の並びがなければ nil)。
func reviewCount(section pyjson.Value) pyjson.Value {
	o, ok := section.(*pyjson.Object)
	if !ok {
		return nil
	}
	candidates, ok := pyops.Get(o, "candidates").([]pyjson.Value)
	if !ok {
		return nil
	}
	n := int64(0)
	for _, c := range candidates {
		if co, ok := c.(*pyjson.Object); ok && pyops.Equal(pyops.Get(co, "level"), "review") {
			n++
		}
	}
	return n
}

// summaryProblems は _summary_problems (記録の件数を、ハッシュを確かめたレポートの件数と照らす。食い違ったものの名前の並び)。
func summaryProblems(state string, record *pyjson.Object, required []string) []string {
	var wrong []string
	type report struct {
		entry pyjson.Value
		value *pyjson.Object
	}
	reports := map[string]report{}
	for _, entry := range pyops.Iter(pyops.Index(record, "checks")) {
		if pyops.Equal(pyops.Index(entry, "status"), "ran") {
			if value := reportValue(state, entry); value != nil {
				name := pyops.Index(entry, "name")
				pyops.Hashable(name)
				reports[pyops.Str(name)] = report{entry, value}
			}
		}
	}
	compare := func(name string, recorded, expected pyjson.Value) {
		if !pyops.Equal(recorded, expected) || pyops.TypeName(recorded) != pyops.TypeName(expected) {
			wrong = append(wrong, name)
		}
	}
	var risks, failOn pyjson.Value = int64(0), int64(0)
	docType, reader := recordContext(record)
	context := []pyjson.Value{docType, reader}
	for _, name := range []string{"lint", "diff", "materials"} {
		r, ok := reports[name]
		if !ok {
			continue
		}
		entry, value := r.entry, r.value
		summary := dictOrNil(pyops.Index(entry, "summary"))
		if name == "lint" || name == "diff" {
			found := dictOrNil(pyops.Get(value, "context"))
			compare(name+".context", context, []pyjson.Value{pyops.Get(found, "doc_type"), pyops.Get(found, "reader")})
			compare(name+".context.words", recordWords(record), reportWords(found))
		}
		switch name {
		case "lint":
			compare("lint.candidate_count", pyops.Get(summary, "candidate_count"), pyops.Get(value, "candidate_count"))
			compare("lint.counts", pyops.Get(summary, "counts"), pyops.Get(value, "counts"))
			if kept, ok := summary.Get("kept_marks"); ok {
				compare("lint.kept_marks", kept, keptMarks(pyops.Or(pyops.Get(value, "findings"), pyops.EmptyList())))
			}
		case "diff":
			var hunks pyjson.Value
			if full, ok := pyops.Get(value, "full_diff").(*pyjson.Object); ok {
				hunks = pyops.Or(pyops.Get(full, "hunks"), pyops.EmptyList())
			}
			for _, key := range []string{"status", "candidate_count", "meaning_risk_count", "fail_on_meaning_risk_count"} {
				compare("diff."+key, pyops.Get(summary, key), pyops.Get(value, key))
			}
			for _, key := range []string{"stiffness", "tone", "hedges", "immutable", "politeness", "register"} {
				section := dictOrNil(pyops.Get(value, key))
				for _, field := range []string{"candidate_count", "level"} {
					name := key + "_" + field
					if _, ok := summary.Get(name); ok {
						compare("diff."+name, pyops.Get(summary, name), pyops.Get(section, field))
					}
				}
				name := key + "_review_count"
				if _, ok := summary.Get(name); ok {
					compare("diff."+name, pyops.Get(summary, name), reviewCount(section))
				}
			}
			var hunkCount pyjson.Value
			if list, ok := hunks.([]pyjson.Value); ok {
				hunkCount = int64(len(list))
			}
			compare("diff.hunk_count", pyops.Get(summary, "hunk_count"), hunkCount)
			if found, ok := pyops.Get(value, "meaning_risks").([]pyjson.Value); ok {
				risks = pyops.Add(risks, int64(len(found)))
			}
			if count := pyops.Get(value, "fail_on_meaning_risk_count"); isPyInt(count) {
				failOn = pyops.Add(failOn, count)
			}
		default:
			items, _ := pyops.Get(value, "items").([]pyjson.Value)
			var classes []pyjson.Value
			for _, item := range items {
				if o, ok := item.(*pyjson.Object); ok {
					classes = append(classes, pyops.Get(o, "classification"))
				}
			}
			for _, key := range []string{"candidate_count", "meaning_risk_count"} {
				compare("materials."+key, pyops.Get(summary, key), pyops.Get(value, key))
			}
			for _, key := range []string{"from_materials", "not_in_sources", "dropped_certainty"} {
				count := int64(0)
				for _, c := range classes {
					if pyops.Equal(c, key) {
						count++
					}
				}
				compare("materials."+key, pyops.Get(summary, key), count)
				recorded := dictOrNil(pyops.Index(record, "materials_summary"))
				compare("materials_summary."+key, pyops.Get(recorded, key), count)
			}
			found, _ := pyops.Get(value, "meaning_risks").([]pyjson.Value)
			risks = pyops.Add(risks, int64(len(found)))
			n := int64(0)
			for _, risk := range found {
				if o, ok := risk.(*pyjson.Object); ok && pyops.Equal(pyops.Get(o, "subkind"), "number") {
					n++
				}
			}
			failOn = pyops.Add(failOn, n)
		}
	}
	if !slices.ContainsFunc(required, func(name string) bool { _, ok := reports[name]; return !ok }) {
		result := pyops.Index(record, "result")
		compare("result.meaning_risk_count", pyops.Get(result, "meaning_risk_count"), risks)
		if _, ok := dictOrNil(result).Get("fail_on_meaning_risk_count"); ok {
			compare("result.fail_on_meaning_risk_count", pyops.Get(result, "fail_on_meaning_risk_count"), failOn)
		}
	}
	return wrong
}

// isPyInt は type(v) is int (真偽を除く)。
func isPyInt(v pyjson.Value) bool { return pyops.TypeName(v) == "int" }

// recordContext は _record_context (記録の options の種類と読者)。
func recordContext(record *pyjson.Object) (pyjson.Value, pyjson.Value) {
	options := pyops.Index(record, "options")
	return pyops.Get(options, "doc_type"), pyops.Get(options, "reader")
}

// keptMarks は _kept_marks (最終稿の lint の候補から、残った直訳語と語の一覧の語を語ごとにまとめ、最初に出た行の順に並べる)。
func keptMarks(findings pyjson.Value) pyjson.Value {
	type mark struct {
		rules []string
		lines []pyjson.Value
		word  pyjson.Value
	}
	var order []string
	marks := map[string]*mark{}
	for _, finding := range pyops.Iter(findings) {
		o, ok := finding.(*pyjson.Object)
		if !ok {
			continue
		}
		rule := pyops.Get(o, "rule")
		pyops.Hashable(rule)
		ruleName, isStr := rule.(string)
		spec, known := keptMarkRules[ruleName]
		if !isStr || !known {
			continue
		}
		message := pyops.Str(pyops.Or(pyops.Get(o, "message"), ""))
		if ruleName == "project_term" && !strings.HasSuffix(message, projectTermPlainTail) {
			message, _, _ = strings.Cut(message, projectTermSuggestions)
		}
		var words []string
		if g := groups(spec.pattern, message); g != nil {
			for _, w := range strings.Split(g[1], "、") {
				if w != "" {
					words = append(words, w)
				}
			}
		}
		var line pyjson.Value
		if l := pyops.Get(o, "line"); isPyInt(l) {
			line = l
		}
		keys := make([]string, 0, len(words))
		values := make([]pyjson.Value, 0, len(words))
		for _, w := range words {
			keys, values = append(keys, "w"+w), append(values, w)
		}
		if len(keys) == 0 {
			keys, values = []string{"r" + ruleName}, []pyjson.Value{nil}
		}
		for i, key := range keys {
			m := marks[key]
			if m == nil {
				m = &mark{word: values[i]}
				marks[key] = m
				order = append(order, key)
			}
			if !slices.Contains(m.rules, ruleName) {
				m.rules = append(m.rules, ruleName)
			}
			if line != nil && !slices.ContainsFunc(m.lines, func(x pyjson.Value) bool { return pyops.Equal(x, line) }) {
				m.lines = append(m.lines, line)
			}
		}
	}
	rows := make([]pyjson.Value, 0, len(order))
	first := make([]pyjson.Value, 0, len(order))
	for _, key := range order {
		m := marks[key]
		lines := slices.Clone(m.lines)
		sort.SliceStable(lines, func(i, j int) bool { return pyops.Less(lines[i], lines[j]) })
		if lines == nil {
			lines = []pyjson.Value{}
		}
		rule := "calque"
		if slices.Contains(m.rules, "project_term") {
			rule = "project_term"
		}
		row := pyjson.NewObject()
		row.Set("rule", rule)
		row.Set("word", m.word)
		row.Set("lines", lines)
		rows = append(rows, row)
		if len(lines) > 0 {
			first = append(first, lines[0])
		} else {
			first = append(first, int64(0))
		}
	}
	index := make([]int, len(rows))
	for i := range index {
		index[i] = i
	}
	sort.SliceStable(index, func(i, j int) bool { return pyops.Less(first[index[i]], first[index[j]]) })
	out := make([]pyjson.Value, len(rows))
	for i, k := range index {
		out[i] = rows[k]
	}
	return out
}

// recordKeptMarks は _record_kept_marks (記録の lint の要約にある、残した指摘の語。無ければ nil)。
func recordKeptMarks(record *pyjson.Object) []pyjson.Value {
	for _, entry := range pyops.Iter(pyops.Index(record, "checks")) {
		if !pyops.Equal(pyops.Index(entry, "name"), "lint") {
			continue
		}
		summary, ok := pyops.Get(entry, "summary").(*pyjson.Object)
		if !ok {
			continue
		}
		if marks, ok := pyops.Get(summary, "kept_marks").([]pyjson.Value); ok && !slices.ContainsFunc(marks, badKeptMark) {
			return marks
		}
	}
	return nil
}

// badKeptMark は記録の残した指摘の語 1 つが、dict で、規則が知っている名前で、行が list の形でないか (規則が鍵になれない値なら TypeError)。
func badKeptMark(m pyjson.Value) bool {
	o, ok := m.(*pyjson.Object)
	if !ok {
		return true
	}
	rule := pyops.Get(o, "rule")
	pyops.Hashable(rule)
	if name, isStr := rule.(string); !isStr || keptMarkRules[name].pattern == nil {
		return true
	}
	_, isList := pyops.Get(o, "lines").([]pyjson.Value)
	return !isList
}

// keptMarksText は _kept_marks_text (「語（語の一覧か直訳語。行番号。使う語）」を「、」でつなぐ)。replacements は語の一覧の使う語 (verify は使わないので nil)。
func keptMarksText(marks []pyjson.Value, replacements map[string]string) string {
	var shown []string
	for _, m := range marks[:min(len(marks), keptMarkShownWords)] {
		lines := pyops.Index(m, "lines").([]pyjson.Value)
		var parts []string
		for _, line := range lines[:min(len(lines), keptMarkShownLines)] {
			parts = append(parts, "L"+pyops.Format(line))
		}
		where := strings.Join(parts, "、")
		if len(lines) > keptMarkShownLines {
			where += "、ほか" + pyops.Format(int64(len(lines)-keptMarkShownLines)) + "行"
		}
		name := pyops.Str(pyops.Index(m, "rule"))
		word := pyops.Index(m, "word")
		text := "語の取れない指摘"
		if pyops.Truthy(word) {
			text = pyops.Format(word)
		}
		if where != "" {
			where = "。" + where
		}
		used := ""
		if w, ok := word.(string); ok && name == "project_term" && replacements[w] != "" {
			used = "。使う語: " + replacements[w]
		}
		shown = append(shown, text+"（"+keptMarkRules[name].label+where+used+"）")
	}
	if len(marks) > keptMarkShownWords {
		shown = append(shown, "ほか"+pyops.Format(int64(len(marks)-keptMarkShownWords))+"語")
	}
	return strings.Join(shown, "、")
}
