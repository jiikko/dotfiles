package check

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"ja-writing/diff"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// run の出力 (本家の _finalize_text から _materials_text まで)。

const (
	headLimit      = 3  // _HEAD_LIMIT
	changedWindow  = 60 // _CHANGED_WINDOW
	changedContext = 30 // _CHANGED_CONTEXT
	changedLimit   = 10 // _CHANGED_LIMIT
)

var (
	headStructureLine = []*pyre.Lazy{pyre.NewLazy(`(後)(\d+)行目`, 0), pyre.NewLazy(`(元)(\d+)行目`, 0)}
	headingRequest    = pyre.NewLazy(`見出し|まとめ|[0-9０-９〇一二三四五六七八九十百千]+\s*文?字|文?字数|字以内|字まで|字程度|字前後`, 0)
	// headRuleNames は _HEAD_RULE_NAMES。
	headRuleNames = [][2]string{{"structure", "構造"}, {"logic", "つながり"}, {"endings.changes", "文末の種類"},
		{"type_changes", "文の種類を表す語"}, {"endings.flags", "文末の立場"}, {"bold", "太字"}}
	// detectorLabels は _detector_lines の検査と名前。
	detectorLabels = [][2]string{{"stiffness", "硬化の検査"}, {"tone", "勢いの検査"}, {"hedges", "慎重表現の検査"},
		{"immutable", "値の検査"}, {"politeness", "敬語の検査"}, {"register", "文体の検査"}}
)

// finalizeText は _finalize_text (1 行目の {仕上げ})。
func finalizeText(c *runContext) string {
	switch {
	case c.noRecord:
		return finalizeNoRecord
	case c.final.Via == "stdin" && c.stateDirOption != nil:
		return fmt.Sprintf(finalizeStdinStateDir, `"`+*c.stateDirOption+`"`)
	case c.final.Via == "stdin":
		return finalizeStdin
	case c.stateDirOption != nil:
		return fmt.Sprintf(finalizeStateDir, `"`+*c.stateDirOption+`"`)
	}
	return finalizeFile
}

// treatedNoteOf は _treated_note。
func treatedNoteOf(c *runContext) string {
	inherited := c.treated.TreatedBy != nil && *c.treated.TreatedBy == "previous_attempt"
	if pyops.Equal(c.entryStatus("diff"), "ran") {
		if inherited {
			return treatedInheritedNote
		}
		return treatedNote
	}
	if inherited {
		return treatedInheritedPendingNote
	}
	return treatedPendingNote
}

// decision は _decide の戻り値 (状態・1 行目・next・状態の行)。
type decision struct {
	status, line, next, token string
}

// decideArgs は _decide の引数 (記録を書いたか・書けなかった理由・回・実行 ID・修正の回数・前回と同じ最終稿か・書き込みを拒まれたか)。
type decideArgs struct {
	recorded       bool
	problem        string
	attempt        attemptNo
	runID          string
	fixCount       int
	sameAsPrevious bool
	denied         bool
}

// decide は _decide (状態、1 行目、next、状態の行)。
func decide(c *runContext, a decideArgs) decision {
	finalize := finalizeText(c)
	failed := c.failed()
	var line, following string
	switch {
	case c.finalChanged:
		line, following = finalChangedLine, "rerun"
	case len(failed) > 0:
		var order []string
		groups := map[string][]string{}
		var items []string
		for _, f := range failed {
			if f.name == "final" {
				continue
			}
			why := failedNames[f.status]
			if _, ok := groups[why]; !ok {
				order = append(order, why)
			}
			groups[why] = append(groups[why], label(f.name))
			items = append(items, label(f.name)+"（"+why+"）")
		}
		clauses := make([]string, len(order))
		for i, why := range order {
			clauses[i] = strings.Join(groups[why], "、") + "が" + why
		}
		line = "検査の一部を実行できませんでした（" + strings.Join(clauses, "、") + "）。完了報告の「実行しなかった検査」に「" +
			strings.Join(items, "、") + "」と書いてください。" + partialTail
		following = "report_partial"
	case !a.recorded && !c.noRecord:
		if a.denied {
			line = "検査を実行しましたが、記録を書けませんでした（" + a.problem + "）。" + deniedStateNote + "--state-dir か " +
				"YOMIYASU_STATE_DIR で自分が書ける場所を指定してもう一度実行するか、完了報告の「実行しなかった検査」に" +
				"「記録（" + a.problem + "）」と書いてください。"
		} else {
			line = "検査を実行しましたが、記録を書けませんでした（" + a.problem + "）。--state-dir に書ける場所を指定してもう一度実行するか、" +
				"完了報告の「実行しなかった検査」に「記録（" + a.problem + "）」と書いてください。"
		}
		following = "report_unrecorded"
	default:
		line, following = decideExecuted(c, a, finalize)
		if c.noRecord {
			following = "report_unrecorded"
		}
	}
	if c.treated != nil && !c.finalChanged {
		note := treatedNoteOf(c)
		if len(failed) > 0 {
			line = note + line
		} else {
			line += note
		}
	}
	if c.sourceWasFinal {
		line = sourceWasFinalNote + line
	}
	if len(c.failOnHits) > 0 && len(failed) == 0 {
		line = "--fail-on=" + strings.Join(c.failOnHits, ",") + " の条件に当たりました（終了コード1）。" + line
	}
	status := "unrecorded"
	switch {
	case len(failed) > 0:
		status = "partial"
	case a.recorded:
		status = "executed"
	}
	var reason *string
	if !a.recorded {
		r := "not_writable"
		if c.noRecord {
			r = "no_record_option"
		}
		reason = &r
	}
	s := Status{State: status, Via: "run", Final: c.final.Token(), Mode: &c.mode, DocType: c.docType, Reader: c.reader, Reason: reason}
	if a.recorded {
		s.RunID, s.Attempt = a.runID, a.attempt
	}
	checks := strings.Join(c.required, ",")
	s.Checks = &checks
	if c.risksComplete() {
		risk := strconv.Itoa(len(c.risks))
		s.MeaningRisk = &risk
	}
	if len(failed) > 0 {
		parts := make([]string, len(failed))
		for i, f := range failed {
			parts[i] = f.name + "(" + f.status + ")"
		}
		joined := strings.Join(parts, ",")
		s.Failed = &joined
	}
	return decision{status, line, following, StatusLine(s)}
}

// decideExecuted は _decide の、検査を最後まで実行して記録を書けた (か書かない指定の) ときの 1 行目と next。
func decideExecuted(c *runContext, a decideArgs, finalize string) (string, string) {
	switch {
	case c.textUnchanged() || (c.source != nil && c.diffResult != nil && pyops.Equal(pyops.Get(c.diffResult, "status"), "no_change")):
		note := ""
		if c.source.SHA256 != c.final.SHA256 {
			note = textSameNote
		}
		return fmt.Sprintf(noChangeLine, note) + finalize, "finalize"
	case a.sameAsPrevious:
		return "検査を実行しました（前回と同じ最終稿）。" + finalize, "finalize"
	case a.fixCount >= 2:
		return fmt.Sprintf("検査を実行しました（修正%d回目のあと）。修正は1回までの決まりを超えています（このコマンドは止めません）。", a.fixCount) +
			"利用者から新しい依頼がない限り本文はもう変えず、残った候補は完了報告に書いてください。" + finalize, "finalize_over_limit"
	case a.fixCount == 1:
		return "検査を実行しました（修正後の検査）。本文はもう変えず、残った候補は完了報告に書いてください。" + finalize, "finalize"
	}
	var line string
	switch {
	case c.mode == "compose":
		line = "検査を実行しました（新規作成、1回目）。資料にない数値と名前を確かめ、主張・条件・担当・未確認の事項を" +
			"資料と読み比べてください。直すのは1回までです。直したら同じコマンドをもう一度実行し、直さないなら" + finalize
	case pyops.Truthy(c.candidateTotal()):
		line = "検査を実行しました（1回目）。下の候補を原文と資料に照らして確かめてください。直すのは1回までです。" +
			"直したら同じコマンドをもう一度実行し、直さないなら" + finalize
	case detectorReviews(c) > 0 || addedActorCount(c) > 0 || diffListCount(c, "heading_shapes") > 0 || diffListCount(c, "comma_increase") > 0:
		var found []string
		if n := detectorReviews(c); n > 0 {
			found = append(found, fmt.Sprintf("文体の硬化と勢いの見直し候補が%d件", n))
		}
		if n := addedActorCount(c); n > 0 {
			found = append(found, fmt.Sprintf("原文にない人や担当を足した文が%d件", n))
		}
		if n := diffListCount(c, "heading_shapes"); n > 0 {
			found = append(found, fmt.Sprintf("形が変わった見出しが%d件", n))
		}
		if diffListCount(c, "comma_increase") > 0 {
			found = append(found, "読点が増えた候補が1件")
		}
		against := ""
		if addedActorCount(c) > 0 {
			against = "資料と依頼に"
		}
		line = "検査を実行しました（1回目）。規則に当たる候補はありませんが、" + strings.Join(found, "、") + "あります。" +
			"その節を原文と" + against + "照らし、全文差分で意図しない変更がないかを確かめ、直すなら1回まで、直さないなら" + finalize
	default:
		line = "検査を実行しました（1回目）。規則に当たる候補はありません。全文差分で意図しない変更がないかを確かめ、" +
			"直すなら1回まで、直さないなら" + finalize
	}
	if c.diffResult != nil && c.diffResult.Len() > 0 {
		hit := false
		for _, key := range []string{"hedges", "immutable", "politeness", "register"} {
			if pyops.Truthy(pyops.GetOr(pyops.Or(pyops.Get(c.diffResult, key), pyjson.NewObject()), "candidate_count", int64(0))) {
				hit = true
				break
			}
		}
		if hit {
			name := "文体の硬化・勢い・慎重表現・値・敬語"
			if pyops.Truthy(pyops.GetOr(pyops.Or(pyops.Get(c.diffResult, "register"), pyjson.NewObject()), "candidate_count", int64(0))) {
				name += "・文体の切り替え"
			}
			line = strings.ReplaceAll(line, "文体の硬化と勢いの見直し候補", name+"の見直し候補")
		}
	}
	return line, "review"
}

// diffRan は diff が最後まで動いたか (_detector_value・_detector_reviews・_diff_list_count・_added_actor_count の共通の条件)。
func diffRan(c *runContext) bool {
	return !c.finalChanged && c.diffResult != nil && slices.Contains(c.required, "diff") && pyops.Equal(c.entryStatus("diff"), "ran")
}

// detectorValue は _detector_value (硬化などの検査の結果の値。diff が最後まで動かなかったときは nil)。
func detectorValue(c *runContext, key string) pyjson.Value {
	if !diffRan(c) {
		return nil
	}
	summary, err := diff.DetectorSummary(c.diffResult, key)
	if err != nil {
		raiseFrom(err)
	}
	return pyjson.OptionalString(summary)
}

// detectorReviews は _detector_reviews (文体の硬化などの見直し候補 (level review) の数)。
func detectorReviews(c *runContext) int {
	if !diffRan(c) {
		return 0
	}
	count := 0
	for _, key := range diff.DetectorKeys {
		section, ok := pyops.Get(c.diffResult, key).(*pyjson.Object)
		if !ok {
			continue
		}
		candidates, ok := pyops.Get(section, "candidates").([]pyjson.Value)
		if !ok {
			continue
		}
		for _, candidate := range candidates {
			if o, ok := candidate.(*pyjson.Object); ok && pyops.Equal(pyops.Get(o, "level"), "review") {
				count++
			}
		}
	}
	return count
}

// headingRequested は _heading_request (資料に見出し・まとめ・字数の指定の語があるか)。
func headingRequested(c *runContext) bool {
	return slices.ContainsFunc(c.materials, func(item *Input) bool { return headingRequest.MustSearch(item.Text) })
}

// diffListCount は _diff_list_count。
func diffListCount(c *runContext, key string) int {
	if !diffRan(c) {
		return 0
	}
	value := pyops.Get(c.diffResult, key)
	if key == "comma_increase" {
		if _, ok := value.(*pyjson.Object); ok {
			return 1
		}
		return 0
	}
	if items, ok := value.([]pyjson.Value); ok {
		return len(items)
	}
	return 0
}

// addedActorCount は _added_actor_count。
func addedActorCount(c *runContext) int {
	if !diffRan(c) {
		return 0
	}
	if items, ok := pyops.Get(c.diffResult, "added_actors").([]pyjson.Value); ok {
		return len(items)
	}
	return 0
}

// detectorLines は _detector_lines (硬化・勢い・慎重表現・値・敬語・文体の検査の行)。
func detectorLines(c *runContext) []string {
	var lines []string
	for _, kv := range detectorLabels {
		if kv[0] == "register" {
			var result pyjson.Value = pyjson.NewObject()
			if c.diffResult != nil && c.diffResult.Len() > 0 {
				result = c.diffResult
			}
			if !pyops.Truthy(pyops.GetOr(pyops.GetOr(result, "register", pyjson.NewObject()), "candidate_count", int64(0))) {
				continue
			}
		}
		if value := detectorValue(c, kv[0]); value != nil {
			lines = append(lines, kv[1]+": "+pyops.Format(value))
		}
	}
	return lines
}

// unsupportedCount は _unsupported_count (3 行目の q。資料にない数値・名前の数)。diffRisks は diff が最後まで動いたときだけ渡す (nil は本家の None)。
func unsupportedCount(result *pyjson.Object, diffRisks []pyjson.Value, useDiff bool) int {
	risks := pyops.Iter(pyops.Or(pyops.Get(result, "meaning_risks"), []pyjson.Value{}))
	unsupported := func(subkinds ...string) int {
		n := 0
		for _, risk := range risks {
			if pyops.Equal(pyops.Get(risk, "kind"), "unsupported") && slices.ContainsFunc(subkinds, func(s string) bool { return pyops.Equal(pyops.Get(risk, "subkind"), s) }) {
				n++
			}
		}
		return n
	}
	if !pyops.Equal(pyops.Get(result, "mode"), "rewrite") {
		return unsupported("number", "name")
	}
	names := unsupported("name")
	var numbers []pyjson.Value
	for _, item := range pyops.Iter(pyops.GetOr(result, "items", []pyjson.Value{})) {
		if pyops.Equal(pyops.Get(item, "kind"), "number") && pyops.Equal(pyops.Get(item, "classification"), "not_in_sources") {
			numbers = append(numbers, item)
		}
	}
	if !useDiff {
		return names + len(numbers)
	}
	var shown []pyjson.Value
	for _, risk := range diffRisks {
		if pyops.Equal(pyops.Get(risk, "kind"), "number") && pyops.Equal(pyops.Get(risk, "basis"), "none") {
			detail := pyops.Or(pyops.Get(risk, "detail"), pyjson.NewObject())
			for _, text := range pyops.Iter(pyops.Or(pyops.Get(detail, "added"), []pyjson.Value{})) {
				for _, number := range mustNumbers(pyops.Str(text)) {
					if key := tuple2(pyops.Index(number, "value"), pyops.Index(number, "unit")); !containsValue(shown, key) {
						shown = append(shown, key)
					}
				}
			}
		}
	}
	count := names
	for _, item := range numbers {
		if slices.ContainsFunc(materialNumberKeys(item), func(k pyjson.Value) bool { return containsValue(shown, k) }) {
			count++
		}
	}
	return count
}

// summaryLine は _summary_line (3 行目)。
func summaryLine(c *runContext) string {
	if c.finalChanged {
		return finalChangedSummary
	}
	var parts []string
	_, lintFailed := c.failedStatus("lint")
	if !lintFailed {
		where := "最終稿"
		if c.docType != nil {
			skipped := pyops.Or(pyops.Get(pyops.Index(c.lintResult, "context"), "skipped_by_doc_type"), pyjson.NewObject())
			where += "、種類 " + *c.docType + "、外した候補" + pyops.Format(pyops.Sum(pyops.Values(skipped))) + "件"
		}
		parts = append(parts, "lint（"+where+"）候補"+pyops.Format(pyops.GetOr(c.lintResult, "candidate_count", int64(0)))+"件")
	}
	_, diffFailed := c.failedStatus("diff")
	if slices.Contains(c.required, "diff") && !diffFailed {
		result := c.diffResult
		where := "原文→最終稿"
		if c.source == nil {
			where = fmt.Sprintf("資料%d→最終稿", c.treated.Index)
		}
		changed := "変更あり"
		if c.textUnchanged() || pyops.Equal(pyops.Get(result, "status"), "no_change") {
			changed = "変更なし"
		}
		parts = append(parts, "diff（"+where+"）"+changed+" 候補"+pyops.Format(pyops.GetOr(result, "candidate_count", int64(0)))+
			"件（うち意味が変わる恐れ"+pyops.Format(pyops.GetOr(result, "meaning_risk_count", int64(0)))+"件）")
	}
	_, materialsFailed := c.failedStatus("materials")
	if slices.Contains(c.required, "materials") && !materialsFailed {
		where := fmt.Sprintf("資料%d件", len(c.materials))
		if c.mode != "rewrite" {
			where += "→最終稿"
		}
		diffDone := slices.Contains(c.required, "diff") && !diffFailed && c.diffRisks != nil
		parts = append(parts, "資料との照合（"+where+"）資料にない数値・名前"+strconv.Itoa(unsupportedCount(c.materialsResult, c.diffRisks, diffDone))+"件")
	}
	summary := "実行した検査: なし"
	if len(parts) > 0 {
		summary = "実行した検査: " + strings.Join(parts, "、")
	}
	if c.factGaps != nil {
		if skipped := pyops.Index(c.factGaps, "skipped"); pyops.Truthy(skipped) {
			summary += "、事実の穴の候補（種類 " + pyops.Format(skipped) + " では出していません）"
		} else {
			summary += "、事実の穴の候補" + pyops.Format(pyops.Index(c.factGaps, "count")) + "件"
		}
	}
	return summary
}

// headWhere は _head_where (冒頭の一覧の行番号)。
func headWhere(oldLine, newLine pyjson.Value) string {
	if newLine != nil {
		return "後" + pyops.Format(newLine) + "行目 "
	}
	if oldLine != nil {
		return "元" + pyops.Format(oldLine) + "行目 "
	}
	return ""
}

// headLineOf は _head_line_of。
func headLineOf(oldLine, newLine pyjson.Value) pyjson.Value {
	if newLine != nil {
		return newLine
	}
	return oldLine
}

// intOrNil は v if isinstance(v, int) else None (bool も int に数える)。
func intOrNil(v pyjson.Value) pyjson.Value {
	if _, ok := v.(bool); ok || pyops.TypeName(v) == "int" {
		return v
	}
	return nil
}

// headEntry は冒頭の一覧の 1 つ (表示・件数・並べる行)。
type headEntry struct {
	name   string
	weight int64
	line   pyjson.Value
}

// headPart は _head_part。
func headPart(label string, entries []headEntry, total pyjson.Value) string {
	sorted := slices.Clone(entries)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i].line, sorted[j].line
		if (a == nil) != (b == nil) {
			return a != nil
		}
		if a == nil {
			return false
		}
		return pyops.Less(a, b)
	})
	shown := sorted[:min(len(sorted), headLimit)]
	if len(shown) == 0 {
		return label + ": " + pyops.Format(total) + "件"
	}
	var used int64
	names := make([]string, len(shown))
	for i, e := range shown {
		names[i], used = e.name, used+e.weight
	}
	text := label + ": " + strings.Join(names, "、")
	if rest := pyops.Sub(total, used); pyops.Less(int64(0), rest) {
		text += "（ほか" + pyops.Format(rest) + "件）"
	}
	return text
}

// classified は照合の項目の classification が name か。
func classified(name string) func(pyjson.Value) bool {
	return func(item pyjson.Value) bool { return pyops.Equal(pyops.Get(item, "classification"), name) }
}

// materialsLineNames は _materials_line_names。
func materialsLineNames(result *pyjson.Object) []string {
	items := pyops.Iter(pyops.Or(pyops.Get(result, "items"), []pyjson.Value{}))
	rewrite := pyops.Equal(pyops.Get(result, "mode"), "rewrite")
	anyItem := func(f func(pyjson.Value) bool) bool { return slices.ContainsFunc(items, f) }
	var names []string
	if anyItem(classified("from_materials")) && rewrite {
		names = append(names, "資料に基づく補足")
	}
	if !rewrite && anyItem(func(i pyjson.Value) bool {
		return pyops.Equal(pyops.Get(i, "kind"), "word") && pyops.Equal(pyops.Get(i, "classification"), "not_in_sources")
	}) {
		names = append(names, "資料にない語")
	}
	if anyItem(classified("dropped_certainty")) {
		names = append(names, "最終稿にない確度の語")
	}
	return names
}

// headLines は _head_lines (3 行目のすぐあとに足す行)。
func headLines(c *runContext) []string {
	if c.finalChanged {
		return nil
	}
	var parts []string
	lintResult := c.lintResult
	if lintResult != nil && pyops.Truthy(pyops.Get(lintResult, "candidate_count")) {
		var entries []headEntry
		for _, finding := range pyops.Iter(pyops.Or(pyops.Get(lintResult, "findings"), []pyjson.Value{})) {
			line := pyops.Get(finding, "line")
			entries = append(entries, headEntry{"L" + pyops.Format(line) + " " + pyops.Format(pyops.Get(finding, "rule")), 1, intOrNil(line)})
		}
		parts = append(parts, headPart("lint の候補", entries, pyops.Index(lintResult, "candidate_count")))
	}
	if result := c.diffResult; result != nil && c.diffParts != nil {
		parts = append(parts, diffHeadParts(c, result)...)
	}
	if materials := c.materialsResult; materials != nil && pyops.Truthy(pyops.Get(materials, "candidate_count")) {
		names := map[string]string{"number": "資料にない数値", "name": "資料にない名前"}
		var entries []headEntry
		for _, risk := range pyops.Iter(pyops.Or(pyops.Get(materials, "meaning_risks"), []pyjson.Value{})) {
			oldLine, newLine := pyops.Get(risk, "old_line"), pyops.Get(risk, "new_line")
			name := pyops.Format(pyops.Lookup(names, pyops.Get(risk, "subkind"), pyops.Format(pyops.Get(risk, "kind"))))
			entries = append(entries, headEntry{headWhere(oldLine, newLine) + name, 1, headLineOf(oldLine, newLine)})
		}
		for _, name := range materialsLineNames(materials) {
			entries = append(entries, headEntry{name, 1, nil})
		}
		parts = append(parts, headPart("資料との照合の候補", entries, pyops.Index(materials, "candidate_count")))
	}
	if gaps := c.factGaps; gaps != nil && !pyops.Truthy(pyops.Index(gaps, "skipped")) && pyops.Truthy(pyops.Index(gaps, "count")) {
		var entries []headEntry
		for _, item := range pyops.Iter(pyops.Index(gaps, "items")) {
			line := pyops.Index(item, "line")
			entries = append(entries, headEntry{"後" + pyops.Format(line) + "行目「" + pyops.Format(pyops.Index(item, "match")) + "」", 1, intOrNil(pyops.Get(item, "line"))})
		}
		parts = append(parts, headPart("事実の穴の候補", entries, pyops.Index(gaps, "count")))
	}
	if len(parts) == 0 {
		return nil
	}
	lines := []string{strings.Join(parts, "。")}
	if lintResult != nil && len(c.keptMarks) > 0 {
		lines = append(lines, fmt.Sprintf(keptMarkHead, keptMarksText(c.keptMarks, c.keptReplacements)))
	}
	return append(lines, headNote)
}

// diffHeadParts は _head_lines の diff の分 (意味が変わる恐れと規則の見直し候補・原文にない人や担当・見出しの形・読点・文体などの候補)。
func diffHeadParts(c *runContext, result *pyjson.Object) []string {
	var parts []string
	var entries []headEntry
	risks := c.diffRisks
	if risks == nil {
		risks = pyops.Iter(pyops.Or(pyops.Get(result, "meaning_risks"), []pyjson.Value{}))
	}
	for _, risk := range risks {
		if pyops.Equal(pyops.Get(risk, "kind"), "sentence_type") && pyops.Equal(pyops.Get(risk, "subkind"), "marks") {
			continue
		}
		oldLine, newLine := pyops.Get(risk, "old_line"), pyops.Get(risk, "new_line")
		entries = append(entries, headEntry{headWhere(oldLine, newLine) + pyops.Format(pyops.Get(risk, "kind")), 1, headLineOf(oldLine, newLine)})
	}
	for _, marker := range pyops.Iter(pyops.Or(pyops.Get(result, "markers"), []pyjson.Value{})) {
		entries = append(entries, headEntry{pyops.Format(pyops.Get(marker, "kind")) + "の増減", 1, nil})
	}
	if pyops.Truthy(pyops.Get(result, "new_words")) {
		entries = append(entries, headEntry{"元の文にない語", 1, nil})
	}
	if pyops.Truthy(pyops.Get(result, "lost_words")) {
		entries = append(entries, headEntry{"消えた語", 1, nil})
	}
	rewrittenOnly, err := diff.ShowsRewrittenOnly(result)
	if err != nil {
		raiseFrom(err)
	}
	endings := pyops.Or(pyops.Get(result, "endings"), pyjson.NewObject())
	for _, kv := range headRuleNames {
		key, name := kv[0], kv[1]
		if (key == "logic" || key == "endings.flags" || key == "bold") && !rewrittenOnly {
			continue
		}
		var items pyjson.Value
		if sub, ok := strings.CutPrefix(key, "endings."); ok {
			items = pyops.Get(endings, sub)
		} else {
			items = pyops.Get(result, key)
		}
		if pyops.Truthy(items) && key == "structure" {
			var plain []pyjson.Value
			for _, item := range pyops.Iter(items) {
				var g []string
				if s, ok := item.(string); ok {
					if g = groups(headStructureLine[0], s); g == nil {
						g = groups(headStructureLine[1], s)
					}
				}
				if g == nil {
					plain = append(plain, item)
					continue
				}
				number, _ := pystr.DecimalInt(g[2])
				var oldLine, newLine pyjson.Value
				if g[1] == "元" {
					oldLine = int64(number)
				} else {
					newLine = int64(number)
				}
				entries = append(entries, headEntry{headWhere(oldLine, newLine) + name, 1, int64(number)})
			}
			items = plainList(plain)
		}
		if pyops.Truthy(items) {
			n := pyops.Len(items)
			shown := name
			if n > 1 {
				shown += fmt.Sprintf("%d件", n)
			}
			entries = append(entries, headEntry{shown, int64(n), nil})
		}
	}
	var total int64
	for _, e := range entries {
		total += e.weight
	}
	if total != 0 {
		parts = append(parts, headPart("diff の候補", entries, total))
	}
	if actors, ok := pyops.Get(result, "added_actors").([]pyjson.Value); ok && len(actors) > 0 {
		var entries []headEntry
		for _, item := range actors {
			o, ok := item.(*pyjson.Object)
			if !ok {
				continue
			}
			var words []string
			for _, actor := range pyops.Iter(pyops.Or(pyops.Get(o, "actors"), []pyjson.Value{})) {
				if a, ok := actor.(*pyjson.Object); ok {
					words = append(words, pyops.Format(pyops.Get(a, "word")))
				}
			}
			newLine := pyops.Get(o, "new_line")
			entries = append(entries, headEntry{headWhere(nil, newLine) + strings.Join(words, "、"), 1, intOrNil(newLine)})
		}
		if len(entries) > 0 {
			parts = append(parts, headPart("原文にない人や担当", entries, int64(len(entries))))
		}
	}
	if shapes, ok := pyops.Get(result, "heading_shapes").([]pyjson.Value); ok && len(shapes) > 0 {
		var entries []headEntry
		for _, item := range shapes {
			if o, ok := item.(*pyjson.Object); ok {
				oldLine, newLine := pyops.Get(o, "old_line"), pyops.Get(o, "new_line")
				entries = append(entries, headEntry{headWhere(oldLine, newLine) + "見出し", 1, headLineOf(oldLine, newLine)})
			}
		}
		if len(entries) > 0 {
			parts = append(parts, headPart("見出しの形", entries, int64(len(entries))))
		}
	}
	if commas, ok := pyops.Get(result, "comma_increase").(*pyjson.Object); ok {
		parts = append(parts, "読点の増加: "+pyops.Format(pyops.Get(commas, "old_count"))+" → "+pyops.Format(pyops.Get(commas, "new_count"))+"個")
	}
	entries = nil
	for _, key := range diff.DetectorKeys {
		section, ok := pyops.Get(result, key).(*pyjson.Object)
		if !ok {
			continue
		}
		candidates, ok := pyops.Get(section, "candidates").([]pyjson.Value)
		if !ok {
			continue
		}
		for _, candidate := range candidates {
			if o, ok := candidate.(*pyjson.Object); ok && pyops.Equal(pyops.Get(o, "level"), "review") {
				oldLine, newLine := pyops.Get(o, "old_line"), pyops.Get(o, "new_line")
				entries = append(entries, headEntry{headWhere(oldLine, newLine) + key, 1, headLineOf(oldLine, newLine)})
			}
		}
	}
	if len(entries) > 0 {
		parts = append(parts, headPart("文体などの候補", entries, int64(len(entries))))
	}
	return parts
}

// plainList は items = plain の list (空でも list)。
func plainList(items []pyjson.Value) pyjson.Value {
	if items == nil {
		return []pyjson.Value{}
	}
	return items
}

// candidateCounts は _candidate_counts (--json の candidate_counts)。
func candidateCounts(c *runContext) *pyjson.Object {
	var source *pyjson.Object
	if !c.finalChanged {
		source = c.sourceLintResult
	}
	o := pyjson.NewObject()
	o.Set("basis", "lint")
	o.Set("source", nil)
	if source != nil {
		o.Set("source", pyops.Index(source, "candidate_count"))
	}
	o.Set("final", nil)
	if c.lintResult != nil {
		o.Set("final", pyops.Index(c.lintResult, "candidate_count"))
	}
	var kind pyjson.Value
	switch {
	case c.source != nil:
		kind = "original"
	case c.treated != nil:
		kind = "material"
	}
	o.Set("source_kind", kind)
	var index pyjson.Value
	if s := c.diffSource(); s != nil {
		index = int64(s.Index)
	}
	o.Set("source_index", index)
	var status pyjson.Value
	if !c.finalChanged && c.sourceLintStatus != nil {
		status = *c.sourceLintStatus
	}
	o.Set("source_status", status)
	var counts pyjson.Value
	if source != nil {
		counts = pyops.Index(source, "counts")
	}
	o.Set("source_counts", counts)
	return o
}

// candidateLines は _candidate_lines。
func candidateLines(c *runContext, counts *pyjson.Object) []string {
	final := pyops.Index(counts, "final")
	if c.finalChanged || final == nil {
		return nil
	}
	kind := pyops.Index(counts, "source_kind")
	if kind == nil {
		return []string{"見直し候補: 最終稿 " + pyops.Format(final) + "件（" + composeCountNote + "）"}
	}
	name := "原文"
	if !pyops.Equal(kind, "original") {
		name = "資料" + pyops.Format(pyops.Index(counts, "source_index")) + "（原文とみなした資料）"
	}
	var number string
	if source := pyops.Index(counts, "source"); source != nil {
		number = pyops.Format(source) + "件"
	} else {
		number = "数えられず（" + failedNames[pyops.Str(pyops.Index(counts, "source_status"))] + "）"
	}
	return []string{"見直し候補: " + name + " " + number + " → 最終稿 " + pyops.Format(final) + "件（" + countNote + "）"}
}

// joinedLines は _joined_lines。
func joinedLines(lines pyjson.Value) string {
	var kept []string
	for _, line := range pyops.Iter(lines) {
		if s := pyops.Str(line); pystr.Strip(s) != "" {
			kept = append(kept, s)
		}
	}
	if len(kept) == 0 {
		return "〔空行〕"
	}
	return strings.Join(kept, "〔改行〕")
}

var (
	breakMark    = pyunicode.Runes("〔改行〕")
	blankMark    = pyunicode.Runes("〔空行〕")
	sentenceEnds = pyunicode.Runes("。！？!?")
)

func hasAt(text []rune, at int, mark []rune) bool {
	return at+len(mark) <= len(text) && slices.Equal(text[at:at+len(mark)], mark)
}

// changeWindowStart は _change_window_start (共通する先頭部分の終わりから、変わり始めた文の初めへ戻る)。
func changeWindowStart(old, new []rune) int {
	prefix := 0
	for prefix < min(len(old), len(new)) && old[prefix] == new[prefix] {
		prefix++
	}
	if shorter := min(len(old), len(new)); shorter > 0 && prefix >= shorter {
		prefix = shorter - 1
	}
	start := 0
	head := old[:prefix]
	for i := 0; i < len(head); {
		switch {
		case slices.Contains(sentenceEnds, head[i]):
			i++
			start = i
		case hasAt(head, i, breakMark):
			i += len(breakMark)
			start = i
		default:
			i++
		}
	}
	if prefix-start > changedContext {
		comma := -1
		for i := prefix - 1; i >= prefix-changedContext; i-- {
			if old[i] == '、' {
				comma = i
				break
			}
		}
		if comma >= 0 {
			start = comma + 1
		} else {
			start = prefix - changedContext
		}
	}
	return start
}

// markers は 〔改行〕 と 〔空行〕 の位置 ([始まり, 終わり])。
func markers(text []rune) [][2]int {
	var out [][2]int
	for i := 0; i < len(text); {
		if hasAt(text, i, breakMark) || hasAt(text, i, blankMark) {
			out = append(out, [2]int{i, i + len(breakMark)})
			i += len(breakMark)
			continue
		}
		i++
	}
	return out
}

// windowText は _window_text (印を分断しない、およそ 60 字の抜き出し)。
func windowText(text []rune, start int, other []rune) (string, bool) {
	end := min(len(text), start+changedWindow)
	starts := append(markers(text), markers(other)...)
	for {
		expanded := start
		for _, m := range starts {
			if m[0] < start && start < m[1] {
				expanded = min(expanded, m[0])
			}
		}
		if expanded == start {
			break
		}
		start = expanded
	}
	for _, m := range markers(text) {
		if m[0] < end && end < m[1] {
			end = m[1]
		}
	}
	clipped := start > 0 || end < len(text)
	body := ""
	if start < end {
		body = pyunicode.FromRunes(text[start:end])
	}
	if start > 0 {
		body = "…" + body
	}
	if end < len(text) {
		body += "…"
	}
	return body, clipped
}

// changedPlaces は _changed_places (全文差分と同じ組の短い表示と、JSON 用の件数)。
func changedPlaces(c *runContext, recorded bool) ([]string, *pyjson.Object) {
	if c.finalChanged || !slices.Contains(c.required, "diff") || c.diffResult == nil {
		return nil, nil
	}
	full := pyops.Index(c.diffResult, "full_diff")
	hunks := pyops.Iter(pyops.Index(full, "hunks"))
	count := len(hunks)
	shown := min(count, changedLimit)
	summary := pyjson.NewObject()
	summary.Set("count", int64(count))
	summary.Set("shown", int64(shown))
	summary.Set("omitted", int64(count-shown))
	summary.Set("clipped", false)
	lines := []string{fmt.Sprintf("■ 直した箇所（%dか所。元と後）", count)}
	if count == 0 {
		newline := pyops.Index(full, "final_newline")
		if pyops.Truthy(pyops.Get(full, "encoding_notes")) || !pyops.Equal(pyops.Index(newline, "old"), pyops.Index(newline, "new")) {
			lines = append(lines, "- 行の中身は変わっていません。改行コード、BOM、末尾の改行のどれかだけが違います（下の「■ 全文差分」）。")
		} else {
			lines = append(lines, "- 原文から変わった箇所はありません。")
		}
		return []string{strings.Join(lines, "\n")}, summary
	}
	uneven, clippedAny := false, false
	for _, hunk := range hunks[:shown] {
		h, ok := hunk.(*pyjson.Object)
		if !ok {
			pyops.Raise("TypeError", "'%s' object is not subscriptable", pyops.TypeName(hunk))
		}
		heading, err := diff.HunkHeading(h)
		if err != nil {
			raiseFrom(err)
		}
		lines = append(lines, heading)
		tag := pyops.Index(hunk, "tag")
		if pyops.Equal(tag, "replace") && pyops.Truthy(pyops.Index(hunk, "whitespace_only")) {
			lines = append(lines, "  （空白だけの変更。下の「■ 全文差分」の「差:」で見られます）")
			continue
		}
		oldText, newText := joinedLines(pyops.Index(hunk, "old_lines")), joinedLines(pyops.Index(hunk, "new_lines"))
		if pyops.Equal(tag, "replace") && oldText == newText {
			lines = append(lines, "  （この形では元と後が同じに見えます。空行や「〔改行〕」の字の違いは、下の「■ 全文差分」で見られます）")
			continue
		}
		if pyops.Equal(tag, "replace") && nonBlank(pyops.Index(hunk, "old_lines")) != nonBlank(pyops.Index(hunk, "new_lines")) {
			uneven = true
		}
		old, new := pyunicode.Runes(oldText), pyunicode.Runes(newText)
		windowed := pyops.Equal(tag, "replace") && max(len(old), len(new)) > changedWindow
		start := 0
		if windowed {
			start = changeWindowStart(old, new)
		}
		for _, side := range []struct {
			label        string
			value, other []rune
			present      bool
		}{{"元", old, new, !pyops.Equal(tag, "insert")}, {"後", new, old, !pyops.Equal(tag, "delete")}} {
			if !side.present {
				continue
			}
			other := []rune(nil)
			if windowed {
				other = side.other
			}
			excerpt, clipped := windowText(side.value, start, other)
			clippedAny = clippedAny || clipped
			lines = append(lines, "  "+side.label+": "+excerpt)
		}
	}
	summary.Set("clipped", clippedAny)
	guide := ""
	if clippedAny {
		guide += "長い所は、変わり始めの少し前からおよそ60字だけを出しています（「…」は省いた所）。"
	}
	if uneven {
		guide += "元と後で行の数が違う所は、別の段落どうしが並んでいることがあります。"
	}
	if count > shown {
		guide += fmt.Sprintf("初めの10か所だけを出しました（ほか%dか所）。", count-shown)
	}
	guide += "省いていない元と後は下の「■ 全文差分」"
	if recorded {
		text := pyops.Str(pyops.Index(c.entries["diff"], "text"))
		guide += "と、記録と同じフォルダの " + text[strings.LastIndex(text, "/")+1:] + " "
	}
	lines = append(lines, "- "+guide+"にあります。")
	return []string{strings.Join(lines, "\n")}, summary
}

func nonBlank(lines pyjson.Value) int {
	n := 0
	for _, line := range pyops.Iter(lines) {
		if pystr.Strip(pyops.Str(line)) != "" {
			n++
		}
	}
	return n
}

// risksSection は report_parts(dict(base, meaning_risks=risks))["risks"]。
func risksSection(base *pyjson.Object, risks []pyjson.Value) string {
	d := base.Clone()
	d.Set("meaning_risks", plainList(risks))
	parts, err := diff.ReportParts(d)
	if err != nil {
		raiseFrom(err)
	}
	return pyops.Str(pyops.Index(parts, "risks"))
}

// emptyDiff は diff("", "")。
func emptyDiff() *pyjson.Object {
	d, err := diff.Diff("", "")
	if err != nil {
		raiseFrom(err)
	}
	return d
}

// sections は _sections (run の出力の節。候補が 0 件の節は出さない)。
func sections(c *runContext) []string {
	var out []string
	base := c.diffResult
	if base == nil {
		base = emptyDiff()
	}
	if len(c.risks) > 0 {
		out = append(out, risksSection(base, c.risks))
	}
	if c.lintResult != nil && !c.finalChanged && len(c.keptMarks) > 0 {
		replaced := len(c.keptReplacements) > 0
		note, unsourced := "", keptMarkUnsourced
		if replaced {
			note, unsourced = keptMarkReplacementNote, keptMarkUnsourcedWithReplacements
		}
		section := fmt.Sprintf(keptMarkSection, len(c.keptMarks), note, unsourced)
		if c.words != nil && pyops.Get(c.words, "flag_patterns_sha256") != nil {
			section = strings.Replace(section, "語の一覧（flag.txt）の語は、", "語の一覧（flag.txt）の語"+keptMarkPatternsNote+"は、", 1)
		}
		rows := make([]string, len(c.keptMarks))
		for i, mark := range c.keptMarks {
			rows[i] = "- " + keptMarksText([]pyjson.Value{mark}, c.keptReplacements)
		}
		out = append(out, section+"\n"+strings.Join(rows, "\n"))
	}
	parts := c.diffParts
	part := func(key string) string {
		if parts == nil {
			return ""
		}
		s, _ := pyops.Get(parts, key).(string)
		return s
	}
	rules := ""
	if parts != nil && pyops.Truthy(pyops.Index(parts, "rules")) {
		rules = strings.Replace(pyops.Str(pyops.Index(parts, "rules")), "■ 規則の見直し候補（", "■ 差分の見直し候補（diff、", 1)
	}
	var front []string
	if parts != nil && headingRequested(c) {
		if part("heading_shapes") != "" {
			front = append(front, part("heading_shapes"))
		}
		if rules != "" && strings.Contains(rules, "見出しの数が変わった") {
			front = append(front, rules)
			rules = ""
		}
	}
	out = append(front, out...)
	if rules != "" {
		out = append(out, rules)
	}
	for _, key := range []string{"added_actors", "heading_shapes", "comma_increase", "stiffness", "tone", "hedges", "immutable", "politeness", "register"} {
		if s := part(key); s != "" && !slices.Contains(front, s) {
			out = append(out, s)
		}
	}
	if c.lintResult != nil && pyops.Truthy(pyops.Get(c.lintResult, "candidate_count")) {
		out = append(out, "■ 表現・書式の見直し候補（lint、最終稿、"+pyops.Format(pyops.Index(c.lintResult, "candidate_count"))+"件）\n"+c.lintFindings)
	}
	if c.materialsResult != nil {
		out = append(out, strings.Join(materialsLines(c), "\n"))
	}
	if gaps := c.factGaps; gaps != nil && !pyops.Truthy(pyops.Index(gaps, "skipped")) {
		advice := "根拠に中身が書かれているときだけ補い、なければ残すか、書き手への問いにしてください"
		if c.docType != nil && (*c.docType == "formal" || *c.docType == "paper") {
			advice = "この種類では補わず、必要なら書き手への問いにしてください"
		}
		count := pyops.Index(gaps, "count")
		rows := []string{"■ 事実の穴の候補（" + pyops.Format(count) + "件。比喩や程度の語だけで、中身や値が書かれていないかもしれない所です。" + advice + "。候補は一部だけです）"}
		items := pyops.Iter(pyops.Index(gaps, "items"))
		for _, item := range items[:min(len(items), 10)] {
			rows = append(rows, "- 後"+pyops.Format(pyops.Index(item, "line"))+"行目（"+pyops.Format(pyops.Index(item, "need"))+"がない「"+
				pyops.Format(pyops.Index(item, "match"))+"」）: "+pyops.Format(pyops.Index(item, "sentence")))
		}
		if pyops.Less(int64(10), count) {
			rows = append(rows, "- ほか"+pyops.Format(pyops.Sub(count, int64(10)))+"件")
		}
		if problem := diff.ListProblems()[FactGapsList]; problem != "" {
			rows = append(rows, "- 一覧を読めなかったので、内蔵の一覧を使いました（"+problem+"）。")
		}
		out = append(out, strings.Join(rows, "\n"))
	}
	if s := part("full_diff"); s != "" {
		out = append(out, s)
	}
	return out
}

// materialsLines は _materials_lines (「■ 資料との照合」の節の行)。
func materialsLines(c *runContext) []string {
	result := c.materialsResult
	items := pyops.Iter(pyops.GetOr(result, "items", []pyjson.Value{}))
	rewrite := pyops.Equal(pyops.Get(result, "mode"), "rewrite")
	lines := []string{fmt.Sprintf("■ 資料との照合（資料%d件）", len(c.materials))}
	texts := func(keep func(pyjson.Value) bool, show func(pyjson.Value) string) []string {
		var out []string
		for _, item := range items {
			if keep(item) {
				out = append(out, show(item))
			}
		}
		return out
	}
	text := func(item pyjson.Value) string { return pyops.Str(pyops.Index(item, "text")) }
	classIs := classified
	if rewrite {
		if supplements := texts(classIs("from_materials"), text); len(supplements) > 0 {
			lines = append(lines, "- 資料に基づく補足の候補: "+strings.Join(supplements, "、"))
		}
	}
	shown := func(key string) func(pyjson.Value) string {
		return func(item pyjson.Value) string {
			var places []string
			for _, evidence := range pyops.Iter(pyops.Or(pyops.Get(item, key), []pyjson.Value{})) {
				row := pyops.Iter(evidence)
				if len(row) != 3 {
					pyops.Raise("ValueError", "not enough values to unpack (expected 3, got %d)", len(row))
				}
				places = append(places, "資料"+pyops.Format(row[0])+"の"+pyops.Format(row[1])+"行目「"+pyops.Str(pyops.Slice(row[2], 0, 40))+"」")
			}
			out := text(item)
			if len(places) > 0 {
				out += "（" + strings.Join(places, "、") + "）"
			}
			return out
		}
	}
	has := func(key string) func(pyjson.Value) bool {
		return func(item pyjson.Value) bool { return pyops.Truthy(pyops.Get(item, key)) }
	}
	if backed := texts(has("evidence"), shown("evidence")); len(backed) > 0 {
		lines = append(lines, "- 資料に基づく補足の候補（該当行）: "+strings.Join(backed[:min(len(backed), 5)], "、"))
	}
	if bare := texts(has("bare_in"), shown("bare_in")); len(bare) > 0 {
		lines = append(lines, "- 単位のない数とだけ一致（資料にない数値に数えています。根拠の行か読み比べてください）: "+strings.Join(bare[:min(len(bare), 5)], "、"))
	}
	words := texts(func(item pyjson.Value) bool {
		return pyops.Equal(pyops.Get(item, "kind"), "word") && classIs("not_in_sources")(item)
	}, text)
	if rewrite && c.diffResult != nil {
		newWords := pyops.Iter(pyops.Or(pyops.Get(c.diffResult, "new_words"), []pyjson.Value{}))
		var kept []string
		for _, w := range words {
			if !containsValue(newWords, w) {
				kept = append(kept, w)
			}
		}
		words = kept
	}
	if len(words) > 0 {
		lines = append(lines, "- 資料にない語: "+strings.Join(words, "、"))
	}
	dropped := texts(classIs("dropped_certainty"), func(item pyjson.Value) string {
		out := text(item)
		if values := pyops.Get(item, "values"); pyops.Truthy(values) {
			out += "（" + pyops.Join("、", values) + "の文）"
		}
		return out
	})
	if len(dropped) > 0 {
		lines = append(lines, "- 資料にあった確度の語が最終稿にない: "+strings.Join(dropped, "、"))
	}
	if !rewrite {
		if reference := texts(classIs("not_in_final"), text); len(reference) > 0 {
			lines = append(lines, "- 参考: 資料にあって最終稿にない数値: "+strings.Join(reference, "、"))
		}
	}
	return append(lines, "- "+materialsDisclaimer)
}

// materialsText は _materials_text (attempt-<n>.materials.txt)。
func materialsText(c *runContext) string {
	var blocks []string
	if risks := pyops.Iter(pyops.Or(pyops.Get(c.materialsResult, "meaning_risks"), []pyjson.Value{})); len(risks) > 0 {
		blocks = append(blocks, risksSection(emptyDiff(), risks))
	}
	blocks = append(blocks, strings.Join(materialsLines(c), "\n"))
	return strings.Join(blocks, "\n\n")
}

// resultContext は _result_context (run の --json の context)。
func resultContext(c *runContext) *pyjson.Object {
	o := pyjson.NewObject()
	o.Set("doc_type", pyjson.OptionalString(c.docType))
	o.Set("reader", pyjson.OptionalString(c.reader))
	if c.words != nil {
		o.Set("words", c.words.Clone())
	}
	return o
}
