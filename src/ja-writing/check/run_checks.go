package check

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"ja-writing/cmdenv"
	"ja-writing/diff"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
	"ja-writing/lint"
	"ja-writing/visibility"
)

// run の 1 回分の入力と結果、検査の実行 (本家の _Context・_run_checks・_with_basis・_material_number_keys・_context_arguments・
// _words_arguments・_context_problem・_kept_mark_replacements・_fail_on_hits)。

// riskChecks は _RISK_CHECKS (意味が変わる恐れを出す検査)。
var riskChecks = []string{"diff", "materials"}

// runContext は _Context。
type runContext struct {
	mode             string
	source           *Input
	materials        []*Input
	final            *Input
	required         []string
	entries          map[string]*pyjson.Object
	entryOrder       []string
	outputs          map[string][]byte
	texts            map[string]string
	lintResult       *pyjson.Object
	sourceLintResult *pyjson.Object
	sourceLintStatus *string
	diffResult       *pyjson.Object
	materialsResult  *pyjson.Object
	factGaps         *pyjson.Object
	risks            []pyjson.Value
	diffRisks        []pyjson.Value // nil は本家の None
	lintFindings     string
	keptMarks        []pyjson.Value
	diffParts        *pyjson.Object
	finalChanged     bool
	noRecord         bool
	failOn           []string
	failOnHits       []string
	treated          *Input
	sourceWasFinal   bool
	stateDirOption   *string
	docType, reader  *string
	words            *pyjson.Object // context.words の 5 つか 7 つのキー (使わないときは nil)
	flagReplacements map[string]string
	keptReplacements map[string]string
}

func newRunContext() *runContext {
	return &runContext{
		entries: map[string]*pyjson.Object{}, outputs: map[string][]byte{}, texts: map[string]string{},
		risks: []pyjson.Value{}, keptMarks: []pyjson.Value{}, flagReplacements: map[string]string{},
		keptReplacements: map[string]string{},
	}
}

func (c *runContext) setEntry(name string, entry *pyjson.Object) {
	if _, ok := c.entries[name]; !ok {
		c.entryOrder = append(c.entryOrder, name)
	}
	c.entries[name] = entry
}

func (c *runContext) entryStatus(name string) pyjson.Value {
	entry, ok := c.entries[name]
	if !ok {
		return nil
	}
	return pyops.Get(entry, "status")
}

// diffSource は diff の原文 (推敲では原文、新規作成では原文とみなした資料)。
func (c *runContext) diffSource() *Input {
	if c.source != nil {
		return c.source
	}
	return c.treated
}

// textUnchanged は原文と最終稿の本文 (text_sha256) が同じか。
func (c *runContext) textUnchanged() bool {
	return c.source != nil && c.source.TextSHA256 == c.final.TextSHA256
}

// failedPair は failed の 1 つ (検査名と状態)。
type failedPair struct{ name, status string }

// failed は最後まで動かなかった検査 (と、途中で変わった最終稿)。
func (c *runContext) failed() []failedPair {
	var out []failedPair
	for _, name := range c.entryOrder {
		if status := pyops.Str(pyops.Index(c.entries[name], "status")); status != "ran" {
			out = append(out, failedPair{name, status})
		}
	}
	if c.finalChanged {
		out = append(out, failedPair{"final", "changed"})
	}
	return out
}

func (c *runContext) failedStatus(name string) (string, bool) {
	for _, f := range c.failed() {
		if f.name == name {
			return f.status, true
		}
	}
	return "", false
}

// risksComplete は意味が変わる恐れを出す検査がすべて最後まで動き、途中で最終稿が変わっていないか。
func (c *runContext) risksComplete() bool {
	if c.finalChanged {
		return false
	}
	for _, name := range c.required {
		if slices.Contains(riskChecks, name) && !pyops.Equal(c.entryStatus(name), "ran") {
			return false
		}
	}
	return true
}

// candidateTotal は 1 回目の「候補あり／なし」に使う、lint・diff・資料との照合の候補 (と事実の穴) の合計。
func (c *runContext) candidateTotal() pyjson.Value {
	var total pyjson.Value = int64(0)
	if c.factGaps != nil {
		total = pyops.Index(c.factGaps, "count")
	}
	for _, result := range []*pyjson.Object{c.lintResult, c.diffResult, c.materialsResult} {
		if result != nil {
			total = pyops.Add(total, pyInt(pyops.Or(pyops.Get(result, "candidate_count"), int64(0))))
		}
	}
	return total
}

// pyInt は int(v) (int と bool と float を int に。ほかは本家と同じく例外)。
func pyInt(v pyjson.Value) pyjson.Value {
	switch x := v.(type) {
	case bool:
		if x {
			return int64(1)
		}
		return int64(0)
	case float64:
		return int64(x)
	}
	if pyops.TypeName(v) == "int" {
		return v
	}
	pyops.Raise("TypeError", "int() argument must be a string, a bytes-like object or a real number, not '%s'", pyops.TypeName(v))
	return nil
}

// contextArguments は _context_arguments (子に渡す種類と読者)。
func contextArguments(docType, reader *string) []string {
	var out []string
	if docType != nil {
		out = append(out, "--doc-type", *docType)
	}
	if reader != nil {
		out = append(out, "--reader", *reader)
	}
	return out
}

// wordsArguments は _words_arguments (check が読んだ一覧の場所を --words で渡し、使わないときは --no-words)。
func wordsArguments(words *pyjson.Object) []string {
	if words != nil {
		return []string{"--words", pyops.Str(pyops.Index(words, "path"))}
	}
	return []string{"--no-words"}
}

// contextProblem は _context_problem (子の JSON の context が、渡した種類と読者と語の一覧と合わないときの理由。合えば "")。
func contextProblem(parsed *pyjson.Object, docType, reader *string, words *pyjson.Object) string {
	ctx := pyops.Index(parsed, "context")
	if !pyops.Equal(pyops.Get(ctx, "doc_type"), pyjson.OptionalString(docType)) || !pyops.Equal(pyops.Get(ctx, "reader"), pyjson.OptionalString(reader)) {
		return "子の JSON の context が、渡した種類と読者と違います"
	}
	found := reportWords(ctx.(*pyjson.Object))
	var mine pyjson.Value
	if words != nil {
		mine = words
	}
	if !pyops.Equal(found, mine) {
		return "子の JSON の context.words が、check が読んだ語の一覧と違います"
	}
	if pyops.Equal(pyops.Get(parsed, "format"), "yomiyasu-lint") {
		skipped := pyops.GetOr(ctx, "skipped_by_doc_type", pyjson.NewObject())
		o, ok := skipped.(*pyjson.Object)
		good := ok
		if ok {
			for _, key := range o.Keys() {
				v, _ := o.Get(key)
				if _, isBool := v.(bool); isBool || pyops.TypeName(v) != "int" || !pyops.GreaterEq(v, int64(0)) {
					good = false
				}
			}
		}
		if !good {
			return "子の JSON の context.skipped_by_doc_type の形が違います"
		}
	}
	return ""
}

// materialNumberKeys は _material_number_keys (照合の項目を diff の候補に結び付ける鍵の集まり)。
func materialNumberKeys(item pyjson.Value) []pyjson.Value {
	keys := []pyjson.Value{tuple2(pyops.Get(item, "value"), pyops.Get(item, "unit"))}
	for _, number := range mustNumbers(pyops.Str(pyops.Index(item, "text"))) {
		key := tuple2(pyops.Index(number, "value"), pyops.Index(number, "unit"))
		if !containsValue(keys, key) {
			keys = append(keys, key)
		}
	}
	return keys
}

// tuple2 は 2 つ組 (本家の tuple。鍵にするときは要素が hashable であることを確かめる)。
func tuple2(a, b pyjson.Value) pyjson.Value {
	pyops.Hashable(a)
	pyops.Hashable(b)
	return []pyjson.Value{a, b}
}

func containsValue(items []pyjson.Value, v pyjson.Value) bool {
	return slices.ContainsFunc(items, func(x pyjson.Value) bool { return pyops.Equal(x, v) })
}

// mustNumbers は diff の extract_numbers (本家の例外は pyops の例外で運ぶ)。
func mustNumbers(text string) []pyjson.Value {
	numbers, err := diff.ExtractNumberDicts(text)
	if err != nil {
		raiseFrom(err)
	}
	return numbers
}

// raiseFrom は本家の例外を表す誤り (visibility.Error・pyops.Exception) を pyops の例外として投げ直す。ほかの誤りは内部エラー。
func raiseFrom(err error) {
	var v *visibility.Error
	if errors.As(err, &v) {
		pyops.Raise(v.Type, "%s", v.Message)
	}
	var e *pyops.Exception
	if errors.As(err, &e) {
		pyops.Raise(e.Type, "%s", e.Msg)
	}
	panic(failure{err})
}

// pyErrorText は本家の "{type(error).__name__}: {error}"。
func pyErrorText(err error) string {
	var v *visibility.Error
	if errors.As(err, &v) {
		return v.Type + ": " + v.Message
	}
	var e *pyops.Exception
	if errors.As(err, &e) {
		return e.Type + ": " + e.Msg
	}
	return fmt.Sprintf("%T: %v", err, err)
}

// withBasis は _with_basis (推敲で資料があるとき、diff の数値の候補に basis を入れた写し)。
func withBasis(risks []pyjson.Value, materialsResult *pyjson.Object) []pyjson.Value {
	out := make([]pyjson.Value, 0, len(risks))
	if materialsResult == nil {
		for _, risk := range risks {
			out = append(out, copyDict(risk))
		}
		return out
	}
	type class struct {
		key   pyjson.Value
		value pyjson.Value
	}
	var classes []class
	lookup := func(key pyjson.Value) (pyjson.Value, bool) {
		for _, c := range classes {
			if pyops.Equal(c.key, key) {
				return c.value, true
			}
		}
		return nil, false
	}
	for _, item := range pyops.Iter(pyops.GetOr(materialsResult, "items", []pyjson.Value{})) {
		if !pyops.Equal(pyops.Get(item, "kind"), "number") || pyops.Equal(pyops.Get(item, "classification"), "not_in_final") {
			continue
		}
		for _, key := range materialNumberKeys(item) {
			current, ok := lookup(key)
			if pyops.Equal(current, "not_in_sources") && ok {
				continue
			}
			value := pyops.Get(item, "classification")
			replaced := false
			for i := range classes {
				if pyops.Equal(classes[i].key, key) {
					classes[i].value, replaced = value, true
				}
			}
			if !replaced {
				classes = append(classes, class{key, value})
			}
		}
	}
	for _, risk := range risks {
		r := copyDict(risk)
		if pyops.Equal(pyops.Get(r, "kind"), "number") {
			var found []pyjson.Value
			detail := pyops.Or(pyops.Get(r, "detail"), pyjson.NewObject())
			for _, text := range pyops.Iter(pyops.Or(pyops.Get(detail, "added"), []pyjson.Value{})) {
				for _, number := range mustNumbers(pyops.Str(text)) {
					key := tuple2(pyops.Index(number, "value"), pyops.Index(number, "unit"))
					value, _ := lookup(key)
					pyops.Hashable(value)
					found = append(found, value)
				}
			}
			switch {
			case containsValue(found, "not_in_sources"):
				r.Set("basis", "none")
			case containsValue(found, "from_materials"):
				r.Set("basis", "material")
			}
		}
		out = append(out, r)
	}
	return out
}

// copyDict は dict(v) (dict の浅い写し。dict でなければ本家と同じく例外)。
func copyDict(v pyjson.Value) *pyjson.Object {
	o, ok := v.(*pyjson.Object)
	if !ok {
		pyops.Raise("TypeError", "'%s' object is not iterable", pyops.TypeName(v))
	}
	return o.Clone()
}

// keptMarkReplacements は _kept_mark_replacements (lint の指摘の末尾に添えた使う語だけ)。
func keptMarkReplacements(findings pyjson.Value, replacements map[string]string) map[string]string {
	kept := map[string]string{}
	if len(replacements) == 0 {
		return kept
	}
	for _, finding := range pyops.Iter(pyops.Or(findings, []pyjson.Value{})) {
		o, ok := finding.(*pyjson.Object)
		if !ok || !pyops.Equal(pyops.Get(o, "rule"), "project_term") {
			continue
		}
		message := pyops.Str(pyops.Or(pyops.Get(o, "message"), ""))
		if strings.HasSuffix(message, projectTermPlainTail) {
			continue
		}
		head, tail, _ := strings.Cut(message, projectTermSuggestions)
		g := groups(keptMarkRules["project_term"].pattern, head)
		if g == nil || tail == "" {
			continue
		}
		suggestions := "、" + strings.TrimSuffix(tail, "。") + "、"
		for _, word := range strings.Split(g[1], "、") {
			replacement := replacements[word]
			if replacement != "" && strings.Contains(suggestions, "、"+word+" → "+replacement+"、") {
				kept[word] = replacement
			}
		}
	}
	return kept
}

// runChecks は _run_checks。lint と diff を子プロセスで (並べて) 動かし、資料があればプロセスの中で照らす。
// snapshots は入力から写しの絶対パス。ctx が取り消されたら、子を止めてから cause を返す。
func runChecks(ctx context.Context, rt Runtime, env cmdenv.Env, c *runContext, snapshots map[*Input]string, stance *string, timeout float64) error {
	exe, err := rt.Executable()
	if err != nil {
		return err
	}
	folder := visibility.PathParent(snapshots[c.final])
	names := map[*Input]string{}
	for item, path := range snapshots {
		names[item] = path[strings.LastIndex(path, "/")+1:]
	}
	finalName := names[c.final]
	args := append(contextArguments(c.docType, c.reader), wordsArguments(c.words)...)
	commands := []childCommand{{"lint", append(append([]string{exe, "lint", "--json"}, args...), finalName)}}
	if slices.Contains(c.required, "diff") {
		argv := []string{exe, "diff", "--json"}
		if stance != nil {
			argv = append(argv, "--stance", *stance)
		}
		for _, item := range c.materials {
			argv = append(argv, "--material", names[item])
		}
		argv = append(append(argv, args...), names[c.diffSource()], finalName)
		commands = append(commands, childCommand{"diff", argv})
	}
	if c.diffSource() != nil {
		commands = append(commands, childCommand{"source_lint", append(append([]string{exe, "lint", "--json"}, args...), names[c.diffSource()])})
	}
	targets := map[string]*pyjson.Object{}
	t := pyjson.NewObject()
	t.Set("final", c.final.SHA256)
	targets["lint"] = t
	if c.diffSource() != nil {
		t := pyjson.NewObject()
		t.Set("source", c.diffSource().SHA256)
		t.Set("final", c.final.SHA256)
		targets["diff"] = t
	}
	mt := pyjson.NewObject()
	if c.diffSource() != nil {
		mt.Set("source", c.diffSource().SHA256)
	}
	digests := make([]string, len(c.materials))
	for i, item := range c.materials {
		digests[i] = item.SHA256
	}
	mt.Set("materials", pyjson.Strings(digests))
	mt.Set("final", c.final.SHA256)
	targets["materials"] = mt
	for _, name := range c.required {
		c.setEntry(name, checkEntry(name, targets[name]))
	}

	results, err := newChildren(rt, timeout, folder, rt.ChildEnv(env)).run(ctx, commands, func() {
		if slices.Contains(c.required, "materials") {
			entry := c.entries["materials"]
			started := rt.Monotonic()
			texts := make([]string, len(c.materials))
			labels := make([]*string, len(c.materials))
			for i, item := range c.materials {
				texts[i], labels[i] = item.Text, item.Label
			}
			var original *string
			if s := c.diffSource(); s != nil {
				original = &s.Text
			}
			result, err := diff.Ground(c.final.Text, texts, original, labels)
			if err == nil {
				c.materialsResult = result
				entry.Set("status", "ran")
			} else {
				entry.Set("error", pyErrorText(err))
				entry.Set("stderr", clipTail(pyErrorText(err)))
			}
			entry.Set("duration_ms", int64((rt.Monotonic()-started)*1000))
		}
	})
	if err != nil {
		return err
	}

	versions := map[string]int{"lint": lint.LintFormatVersion, "diff": diff.DiffFormatVersion}
	if c.diffSource() != nil {
		r := results["source_lint"]
		status := r.status
		c.sourceLintStatus = &status
		if r.status == "ran" {
			parsed, problem := parseReport(r.stdout, "lint", versions["lint"])
			if parsed != nil {
				problem = contextProblem(parsed, c.docType, c.reader, c.words)
			}
			if problem == "" {
				c.sourceLintResult = parsed
			} else {
				errorStatus := "error"
				c.sourceLintStatus = &errorStatus
			}
		}
	}
	for _, name := range []string{"lint", "diff"} {
		r, ok := results[name]
		if !ok {
			continue
		}
		entry := c.entries[name]
		entry.Set("status", r.status)
		entry.Set("exit_code", r.code)
		entry.Set("duration_ms", r.elapsed)
		entry.Set("stderr", r.stderr)
		entry.Set("error", r.err)
		if r.status != "ran" {
			continue
		}
		parsed, problem := parseReport(r.stdout, name, versions[name])
		if parsed != nil {
			if problem = contextProblem(parsed, c.docType, c.reader, c.words); problem != "" {
				parsed = nil
			}
		}
		var text, findings string
		var parts *pyjson.Object
		var risks []pyjson.Value
		if parsed != nil {
			if err := renderChild(name, parsed, c.materialsResult, &text, &findings, &parts, &risks); err != nil {
				parsed, problem = nil, "子の JSON から人が読む形を作れません（"+pyErrorText(err)+"）"
			}
		}
		if parsed == nil {
			entry.Set("status", "error")
			entry.Set("error", problem)
			continue
		}
		c.outputs[name] = r.stdout
		c.texts[name] = text
		summary := pyjson.NewObject()
		if name == "lint" {
			c.lintResult = parsed
			c.lintFindings = findings
			c.keptMarks = pyops.Iter(keptMarks(pyops.Index(parsed, "findings")))
			c.keptReplacements = keptMarkReplacements(pyops.Index(parsed, "findings"), c.flagReplacements)
			summary.Set("candidate_count", pyops.Index(parsed, "candidate_count"))
			summary.Set("counts", pyops.Index(parsed, "counts"))
			summary.Set("kept_marks", c.keptMarks)
		} else {
			c.diffResult = parsed
			c.diffParts = parts
			c.diffRisks = risks
			for _, key := range []string{"status", "candidate_count", "meaning_risk_count", "fail_on_meaning_risk_count"} {
				summary.Set(key, pyops.Index(parsed, key))
			}
			summary.Set("hunk_count", int64(pyops.Len(pyops.Or(pyops.Get(pyops.Index(parsed, "full_diff"), "hunks"), []pyjson.Value{}))))
			for _, key := range diff.DetectorKeys {
				if section, ok := pyops.Get(parsed, key).(*pyjson.Object); ok {
					summary.Set(key+"_candidate_count", pyops.Get(section, "candidate_count"))
					summary.Set(key+"_level", pyops.Get(section, "level"))
					summary.Set(key+"_review_count", reviewCount(section))
				}
			}
		}
		entry.Set("summary", summary)
	}
	if c.materialsResult != nil {
		var classes []pyjson.Value
		for _, item := range pyops.Iter(pyops.GetOr(c.materialsResult, "items", []pyjson.Value{})) {
			classes = append(classes, pyops.Get(item, "classification"))
		}
		summary := pyjson.NewObject()
		summary.Set("candidate_count", pyops.Get(c.materialsResult, "candidate_count"))
		summary.Set("meaning_risk_count", pyops.Get(c.materialsResult, "meaning_risk_count"))
		for _, key := range []string{"from_materials", "not_in_sources", "dropped_certainty"} {
			summary.Set(key, countValue(classes, key))
		}
		c.entries["materials"].Set("summary", summary)
	}
	if c.diffResult != nil {
		c.risks = append(c.risks, c.diffRisks...)
	}
	if c.materialsResult != nil {
		c.risks = append(c.risks, pyops.Iter(pyops.Or(pyops.Get(c.materialsResult, "meaning_risks"), []pyjson.Value{}))...)
	}
	return nil
}

func countValue(items []pyjson.Value, v pyjson.Value) int64 {
	n := int64(0)
	for _, x := range items {
		if pyops.Equal(x, v) {
			n++
		}
	}
	return n
}

// clipTail は traceback.format_exc()[-_STDERR_LIMIT:] の代わり (Go はトレースバックを持たないので誤りの文の末尾)。
func clipTail(text string) string {
	runes := []rune(text)
	if len(runes) > stderrLimit {
		runes = runes[len(runes)-stderrLimit:]
	}
	return string(runes)
}

// renderChild は子の JSON から人が読む形を作る (本家の _run_checks の try の中)。本家が例外になる JSON は error。
func renderChild(name string, parsed, materialsResult *pyjson.Object, text, findings *string, parts **pyjson.Object, risks *[]pyjson.Value) (err error) {
	defer pyops.Catch(&err)
	if name == "lint" {
		report, err := lint.RenderReport(parsed)
		if err != nil {
			return err
		}
		*text = report + "\n"
		*findings, err = lint.RenderFindings(parsed)
		return err
	}
	report, err := diff.Report(parsed)
	if err != nil {
		return err
	}
	*text = report + "\n"
	if *parts, err = diff.ReportParts(parsed); err != nil {
		return err
	}
	*risks = withBasis(pyops.Iter(pyops.Index(parsed, "meaning_risks")), materialsResult)
	return nil
}

// failOnHits は _fail_on_hits (--fail-on に当たった値。指定した順)。
func failOnHits(c *runContext) []string {
	var hits []string
	var counts pyjson.Value = pyjson.NewObject()
	if c.lintResult != nil {
		counts = pyops.Or(pyops.Get(c.lintResult, "counts"), pyjson.NewObject())
	}
	for _, value := range c.failOn {
		var hit bool
		switch value {
		case "meaning-risk":
			n, err := diff.FailOnCount(c.risks)
			if err != nil {
				raiseFrom(err)
			}
			hit = n > 0
		case "warn":
			hit = pyops.Truthy(pyops.Or(pyops.Get(counts, "warn"), pyops.Get(counts, "error")))
		default:
			hit = pyops.Truthy(pyops.Get(counts, "error"))
		}
		if hit && !slices.Contains(hits, value) {
			hits = append(hits, value)
		}
	}
	return hits
}
