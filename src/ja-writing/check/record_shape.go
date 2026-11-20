package check

import (
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
	"ja-writing/internal/pyre"
	"ja-writing/visibility"
)

// 記録の形の検査 (本家の _record_shape・_fact_gaps_shape・_words_shape)。検査の項目は recordRules の表に本家と同じ順で並べ、
// RecordShape はその表を 1 本の検査器で上から回す。項目の名前は食い違ったキーの名前で、本家と同じ順・同じ数だけ返す (verify がそのまま文にする)。

var (
	hex64Pattern = pyre.NewLazy(`^[0-9a-f]{64}$`, 0) // re.match の $ は末尾の改行の手前にも当たる (本家と同じ)
	eols         = []string{"lf", "crlf", "cr", "mixed", "none"}
	checkNames   = []string{"lint", "diff", "materials"}
	treatedBy    = []string{"similarity", "previous_attempt"}
	recordKeys   = []string{"format", "format_version", "run_id", "attempt", "fix_count", "previous", "created_at", "mode", "cwd",
		"host", "skill", "tools", "python", "inputs", "options", "required_checks", "checks", "materials_summary", "result", "record_digest"}
	inputKeys = []string{"role", "index", "path", "via", "bytes", "sha256", "text_sha256", "bom", "eol", "snapshot"}
	checkKeys = []string{"name", "status", "exit_code", "duration_ms", "target", "report", "report_sha256", "text", "text_sha256",
		"summary", "stderr", "error"}
	wordsKeys        = []string{"path", "allow_lines", "flag_lines", "allow_sha256", "flag_sha256"}
	wordsPatternKeys = []string{"flag_patterns_lines", "flag_patterns_sha256"}
	factGapKinds     = []string{"degree", "change", "effect", "figure"}
)

// shape は 1 つの記録の検査の途中 (食い違ったキーの名前を順に足す)。
type shape struct {
	record *pyjson.Object
	wrong  []string
}

// need は本家の need。条件が偽なら名前を足し、条件をそのまま返す。
func (s *shape) need(ok bool, name string) bool {
	if !ok {
		s.wrong = append(s.wrong, name)
	}
	return ok
}

func (s *shape) get(key string) pyjson.Value { return pyops.Get(s.record, key) }

// shapeRule は表の 1 行。check が nil でなければ need(check(記録[name]), name)、group は自分で need を呼ぶ項目のまとまり。
type shapeRule struct {
	name  string
	check func(v pyjson.Value) bool
	group func(s *shape)
}

func intAtLeast(low int64) func(pyjson.Value) bool {
	return func(v pyjson.Value) bool { return isIntAtLeast(v, low) }
}

func oneOf(values ...string) func(pyjson.Value) bool {
	return func(v pyjson.Value) bool { return inStrings(v, values...) }
}

// recordRules は本家の _record_shape の検査を、必須のキーの検査の後ろに並ぶ順のまま並べた表。
var recordRules = []shapeRule{
	{name: "run_id", check: func(v pyjson.Value) bool { s, ok := v.(string); return ok && runIDPattern.MustMatchAt0(s) }},
	{name: "attempt", check: intAtLeast(1)},
	{name: "fix_count", check: intAtLeast(0)},
	{name: "previous", check: func(v pyjson.Value) bool {
		o, ok := v.(*pyjson.Object)
		return v == nil || (ok && isIntAtLeast(pyops.Get(o, "attempt"), 1) && textOrNull(pyops.Get(o, "final_sha256")))
	}},
	{name: "created_at", check: func(v pyjson.Value) bool {
		s, ok := v.(string)
		if !ok {
			return false
		}
		_, parsed := ParseISO(s)
		return parsed
	}},
	{name: "mode", check: oneOf("rewrite", "compose")},
	{name: "cwd", check: isText},
	{name: "host", check: func(v pyjson.Value) bool { return hasTextOrNull(v, "session", "codex_thread_id", "armed_by") }},
	{name: "skill", check: func(v pyjson.Value) bool {
		skill, ok := v.(*pyjson.Object)
		if !ok {
			return false
		}
		_, found := pyops.Get(skill, "found").(bool)
		return found && hasTextOrNull(skill, "dir", "skill_md_sha256", "version")
	}},
	{name: "tools", check: toolsShape},
	{name: "python", check: func(v pyjson.Value) bool {
		p, ok := v.(*pyjson.Object)
		if !ok || !isText(pyops.Get(p, "version")) || !isText(pyops.Get(p, "implementation")) {
			return false
		}
		u, has := p.Get("unicode_version")
		return !has || isText(u)
	}},
	{group: inputsShape},
	{group: optionsShape},
	{name: "required_checks", check: func(v pyjson.Value) bool {
		required, ok := v.([]pyjson.Value)
		return ok && allIn(required, checkNames) && distinctCount(required) == len(required)
	}},
	{group: checksShape},
	{name: "materials_summary", check: isNullOrDict},
	{name: "result", check: func(v pyjson.Value) bool {
		r, ok := v.(*pyjson.Object)
		if !ok {
			return false
		}
		_, hit := pyops.Get(r, "fail_on_hit").(bool)
		_, unchanged := pyops.Get(r, "final_unchanged_during_run").(bool)
		_, notJudged := pyops.Get(r, "not_judged").([]pyjson.Value)
		return oneOf("executed", "partial")(pyops.Get(r, "status")) && isText(pyops.Get(r, "token")) && isText(pyops.Get(r, "next")) &&
			hit && isIntAtLeast(pyops.Get(r, "meaning_risk_count"), 0) && unchanged && notJudged
	}},
	{name: "record_digest", check: isText},
}

// RecordShape は _record_shape。記録の必須のキーと型を確かめ、食い違ったキーの名前の並びを返す (空なら形は合っている)。
// 本家が TypeError で落ちる記録 (鍵や集合の要素になれない list・dict を、検査の名前や fact_gaps の kind に持つもの) は error を返す。
func RecordShape(record *pyjson.Object) (wrong []string, err error) {
	defer pyops.Catch(&err)
	s := &shape{record: record}
	for _, key := range recordKeys {
		_, ok := record.Get(key)
		s.need(ok, key)
	}
	if len(s.wrong) > 0 {
		return s.wrong, nil
	}
	for _, rule := range recordRules {
		if rule.group != nil {
			rule.group(s)
		} else {
			s.need(rule.check(s.get(rule.name)), rule.name)
		}
	}
	return s.wrong, nil
}

func toolsShape(v pyjson.Value) bool {
	tools, ok := v.(*pyjson.Object)
	if !ok {
		return false
	}
	for _, name := range ToolNames {
		tool, ok := pyops.Get(tools, name).(*pyjson.Object)
		if !ok || !textOrNull(pyops.Get(tool, "version")) || !textOrNull(pyops.Get(tool, "sha256")) {
			return false
		}
	}
	for _, key := range append(listKeys(), FactGapsKey) {
		v, has := tools.Get(key)
		if !has {
			continue
		}
		list, ok := v.(*pyjson.Object)
		if !ok || pyops.Get(list, "version") != nil || !textOrNull(pyops.Get(list, "sha256")) {
			return false
		}
	}
	return true
}

func listKeys() []string {
	keys := make([]string, len(ListNames))
	for i, name := range ListNames {
		keys[i] = "references/lists/" + name + ".txt"
	}
	return keys
}

// inputShape は check_input。
func (s *shape) inputShape(item pyjson.Value, name, role string, index int64) {
	o, ok := item.(*pyjson.Object)
	if !ok {
		s.need(false, name)
		return
	}
	if !s.need(hasKeys(o, inputKeys), name) {
		return
	}
	s.need(pyops.Get(o, "role") == role, name+".role")
	if role == "material" {
		label := pyops.Get(o, "label")
		text, isStr := label.(string)
		s.need(label == nil || (isStr && pyops.Len(text) <= 200 && !strings.Contains(text, ">>>") &&
			!strings.ContainsAny(text, "\n\r")), name+".label")
	}
	s.need(isExactInt(pyops.Get(o, "index"), index), name+".index")
	via := pyops.Get(o, "via")
	s.need(inStrings(via, "file", "stdin"), name+".via")
	path := pyops.Get(o, "path")
	s.need((path == nil) == (via == "stdin") && textOrNull(path), name+".path")
	s.need(isIntAtLeast(pyops.Get(o, "bytes"), 0), name+".bytes")
	s.need(isHex(pyops.Get(o, "sha256")), name+".sha256")
	s.need(isHex(pyops.Get(o, "text_sha256")), name+".text_sha256")
	_, bom := pyops.Get(o, "bom").(bool)
	s.need(bom, name+".bom")
	s.need(inStrings(pyops.Get(o, "eol"), eols...), name+".eol")
	s.need(pyops.Get(o, "snapshot") == "inputs/"+pyops.Format(pyops.Get(o, "sha256"))+".bin", name+".snapshot")
}

func inputsShape(s *shape) {
	mode := s.get("mode")
	inputs, ok := s.get("inputs").(*pyjson.Object)
	if !ok {
		s.need(false, "inputs")
		return
	}
	if !s.need(hasKeys(inputs, []string{"source", "materials", "final"}), "inputs") {
		return
	}
	source := pyops.Get(inputs, "source")
	if mode == "rewrite" || source != nil {
		s.inputShape(source, "inputs.source", "source", 0)
	}
	materials, isList := pyops.Get(inputs, "materials").([]pyjson.Value)
	if s.need(isList && (mode != "compose" || len(materials) > 0), "inputs.materials") {
		treatedCount := 0
		for i, item := range materials {
			name := "inputs.materials[" + strconv.Itoa(i+1) + "]"
			s.inputShape(item, name, "material", int64(i+1))
			o, itemDict := item.(*pyjson.Object)
			if !itemDict {
				continue
			}
			treated := pyops.GetOr(o, "treated_as_source", false)
			treatedBool, isBool := treated.(bool)
			s.need(isBool && (mode == "compose" || !treatedBool), name+".treated_as_source")
			s.need(similarityShape(pyops.Get(o, "similarity")), name+".similarity")
			if by, has := o.Get("treated_by"); has {
				if treated == true {
					s.need(inStrings(by, treatedBy...), name+".treated_by")
				} else {
					s.need(by == nil, name+".treated_by")
				}
			}
			if treated == true {
				treatedCount++
			}
		}
		s.need(treatedCount <= 1, "inputs.materials.treated_as_source")
	}
	s.inputShape(pyops.Get(inputs, "final"), "inputs.final", "final", 0)
	if mode == "compose" {
		s.need(pyops.Get(inputs, "source") == nil, "inputs.source")
	}
}

// similarityShape は似かたが null か 0 以上 1 以下の数 (int か float。bool は除く)。
func similarityShape(v pyjson.Value) bool {
	switch x := v.(type) {
	case nil:
		return true
	case float64:
		return x >= 0 && x <= 1
	}
	n, ok := bigInt(v)
	return ok && n.Sign() >= 0 && n.Cmp(big.NewInt(1)) <= 0
}

func optionsShape(s *shape) {
	options, optionsDict := s.get("options").(*pyjson.Object)
	s.need(optionsDict && optionsValues(options), "options")
	if !optionsDict {
		return
	}
	_, isBool := pyops.GetOr(options, "fact_gaps", false).(bool)
	s.need(isBool, "options.fact_gaps")
	if words, has := options.Get("words"); has {
		s.need(WordsShape(words), "options.words")
	}
	gaps := s.get("fact_gaps")
	if pyops.Truthy(pyops.Get(options, "fact_gaps")) {
		s.need(FactGapsShape(gaps), "fact_gaps")
	} else {
		s.need(gaps == nil, "fact_gaps")
	}
}

func optionsValues(options *pyjson.Object) bool {
	if !textOrNull(pyops.Get(options, "stance")) {
		return false
	}
	failOn, ok := pyops.Get(options, "fail_on").([]pyjson.Value)
	if !ok || slices.ContainsFunc(failOn, func(v pyjson.Value) bool { return !isText(v) }) {
		return false
	}
	timeout := pyops.Get(options, "timeout")
	if _, isFloat := timeout.(float64); !isFloat {
		if _, isInt := bigInt(timeout); !isInt {
			return false
		}
	}
	docTypes := make([]string, len(visibility.DocTypes))
	for i, row := range visibility.DocTypes {
		docTypes[i] = row.Value
	}
	readers := make([]string, len(visibility.ReaderLevels))
	for i, row := range visibility.ReaderLevels {
		readers[i] = row.Value
	}
	return knownValue(pyops.Get(options, "doc_type"), docTypes) && knownValue(pyops.Get(options, "reader"), readers)
}

// knownValue は _known_value (null か、使える値の文字列)。
func knownValue(v pyjson.Value, values []string) bool { return v == nil || inStrings(v, values...) }

func checksShape(s *shape) {
	checks, ok := s.get("checks").([]pyjson.Value)
	if !ok {
		s.need(false, "checks")
		return
	}
	var names []pyjson.Value
	for i, entry := range checks {
		name := "checks[" + strconv.Itoa(i) + "]"
		o, entryDict := entry.(*pyjson.Object)
		if !s.need(entryDict && hasKeys(o, checkKeys), name) {
			continue
		}
		names = append(names, pyops.Get(o, "name"))
		s.need(inStrings(pyops.Get(o, "name"), checkNames...), name+".name")
		s.need(inStrings(pyops.Get(o, "status"), "ran", "error", "timeout"), name+".status")
		exit := pyops.Get(o, "exit_code")
		_, exitInt := bigInt(exit)
		s.need(exit == nil || exitInt, name+".exit_code")
		s.need(isIntAtLeast(pyops.Get(o, "duration_ms"), 0), name+".duration_ms")
		s.need(isDict(pyops.Get(o, "target")), name+".target")
		for _, key := range []string{"report", "text", "error"} {
			s.need(textOrNull(pyops.Get(o, key)), name+"."+key)
		}
		for _, key := range []string{"report_sha256", "text_sha256"} {
			v := pyops.Get(o, key)
			s.need(v == nil || isHex(v), name+"."+key)
		}
		s.need(isNullOrDict(pyops.Get(o, "summary")), name+".summary")
		s.need(isText(pyops.Get(o, "stderr")), name+".stderr")
	}
	s.need(distinctCount(names) == len(names), "checks.name")
	mode := s.get("mode")
	inputs, inputsDict := s.get("inputs").(*pyjson.Object)
	if !inStrings(mode, "rewrite", "compose") || !inputsDict {
		return
	}
	if materials, isList := pyops.Get(inputs, "materials").([]pyjson.Value); isList {
		allowed := modeChecks(mode.(string), len(materials) > 0, recordTreated(s.record) != nil)
		s.need(allIn(names, allowed), "checks.name")
		required, isRequiredList := s.get("required_checks").([]pyjson.Value)
		s.need(!isRequiredList || allIn(required, allowed), "required_checks")
	}
}

// modeChecks は _mode_checks (モードと資料の有無で決まる必要な検査。新規作成で資料の 1 つを原文とみなしたときは diff も要る)。
func modeChecks(mode string, hasMaterials, treated bool) []string {
	if mode == "rewrite" {
		if hasMaterials {
			return []string{"lint", "diff", "materials"}
		}
		return []string{"lint", "diff"}
	}
	if treated {
		return []string{"lint", "diff", "materials"}
	}
	return []string{"lint", "materials"}
}

// recordTreated は _record_treated (inputs.materials[] で treated_as_source が真の最初のもの)。
func recordTreated(record *pyjson.Object) *pyjson.Object {
	inputs, ok := pyops.Get(record, "inputs").(*pyjson.Object)
	if !ok {
		return nil
	}
	materials, ok := pyops.Get(inputs, "materials").([]pyjson.Value)
	if !ok {
		return nil
	}
	for _, item := range materials {
		if o, ok := item.(*pyjson.Object); ok && pyops.Get(o, "treated_as_source") == true {
			return o
		}
	}
	return nil
}

// FactGapsShape は _fact_gaps_shape (記録の fact_gaps が run の書く形か)。kind が鍵になれない値 (list・dict) なら本家と同じく TypeError で落ちる。
func FactGapsShape(gaps pyjson.Value) bool {
	o, ok := gaps.(*pyjson.Object)
	if !ok || !sameKeySet(o, []string{"count", "skipped", "by_kind", "items"}) {
		return false
	}
	count := pyops.Get(o, "count")
	skipped := pyops.Get(o, "skipped")
	kinds, kindsOK := pyops.Get(o, "by_kind").(*pyjson.Object)
	items, itemsOK := pyops.Get(o, "items").([]pyjson.Value)
	n, countOK := bigInt(count)
	if !countOK || n.Sign() < 0 || (skipped != nil && !inStrings(skipped, "essay", "promo")) || !kindsOK || !itemsOK ||
		int64(len(items)) != min(clampInt64(n), 20) {
		return false
	}
	var values []pyjson.Value
	for _, kind := range kinds.Keys() {
		v, _ := kinds.Get(kind)
		k, ok := bigInt(v)
		if !slices.Contains(factGapKinds, kind) || !ok || k.Sign() <= 0 {
			return false
		}
		values = append(values, v)
	}
	if !pyops.Equal(pyops.Sum(values), count) {
		return false
	}
	if pyops.Truthy(skipped) && (pyops.Truthy(count) || kinds.Len() > 0 || len(items) > 0) {
		return false
	}
	for _, item := range items {
		if !factGapItemShape(item, kinds) {
			return false
		}
	}
	return true
}

func factGapItemShape(item pyjson.Value, kinds *pyjson.Object) bool {
	o, ok := item.(*pyjson.Object)
	if !ok {
		return false
	}
	kind := pyops.Get(o, "kind")
	hashable(kind, "a dict key")
	k, isStr := kind.(string)
	if !isStr {
		return false
	}
	if _, has := kinds.Get(k); !has {
		return false
	}
	line, lineOK := bigInt(pyops.Get(o, "line"))
	if !lineOK || line.Sign() <= 0 {
		return false
	}
	for _, key := range []string{"sentence", "match", "need"} {
		if !isText(pyops.Get(o, key)) {
			return false
		}
	}
	return pyops.Len(pyops.Get(o, "sentence")) <= 80
}

// WordsShape は _words_shape (記録の options.words が、run の書く形 (context.words の 5 つのキーか、flag-patterns.txt の 2 つを足した 7 つのキー) か)。
func WordsShape(words pyjson.Value) bool {
	o, ok := words.(*pyjson.Object)
	if !ok {
		return false
	}
	keys := o.Keys()
	if slices.Equal(keys, append(slices.Clone(wordsKeys), wordsPatternKeys...)) {
		lines := pyops.Get(o, "flag_patterns_lines")
		sum, isStr := pyops.Get(o, "flag_patterns_sha256").(string)
		return isIntAtLeast(lines, 0) && isStr && wordsDigest(sum) && wordsBody(o)
	}
	return slices.Equal(keys, wordsKeys) && wordsBody(o) &&
		(pyops.Get(o, "allow_sha256") != nil || pyops.Get(o, "flag_sha256") != nil)
}

func wordsBody(o *pyjson.Object) bool {
	path, ok := pyops.Get(o, "path").(string)
	return ok && strings.HasPrefix(path, "/") &&
		isIntAtLeast(pyops.Get(o, "allow_lines"), 0) && isIntAtLeast(pyops.Get(o, "flag_lines"), 0) &&
		wordsDigest(pyops.Get(o, "allow_sha256")) && wordsDigest(pyops.Get(o, "flag_sha256"))
}

// wordsDigest は null か、64 字の小文字の 16 進 (末尾の改行は許さない。hex64Pattern と違う)。
func wordsDigest(v pyjson.Value) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && len(s) == 64 && strings.Trim(s, "0123456789abcdef") == ""
}

// ここから下は本家の検査の中の小さな判定 (type(v) is int は bool を含まない・isinstance(v, str) など)。

func bigInt(v pyjson.Value) (*big.Int, bool) {
	switch x := v.(type) {
	case int:
		return big.NewInt(int64(x)), true
	case int64:
		return big.NewInt(x), true
	case *big.Int:
		return x, true
	}
	return nil, false
}

func clampInt64(n *big.Int) int64 {
	if !n.IsInt64() {
		return math.MaxInt64
	}
	return n.Int64()
}

func isIntAtLeast(v pyjson.Value, low int64) bool {
	n, ok := bigInt(v)
	return ok && n.Cmp(big.NewInt(low)) >= 0
}

func isExactInt(v pyjson.Value, want int64) bool {
	n, ok := bigInt(v)
	return ok && n.Cmp(big.NewInt(want)) == 0
}

func isText(v pyjson.Value) bool { _, ok := v.(string); return ok }

func textOrNull(v pyjson.Value) bool { return v == nil || isText(v) }

func isDict(v pyjson.Value) bool { _, ok := v.(*pyjson.Object); return ok }

func isNullOrDict(v pyjson.Value) bool { return v == nil || isDict(v) }

func isHex(v pyjson.Value) bool { s, ok := v.(string); return ok && hex64Pattern.MustMatchAt0(s) }

// inStrings は v in (値, ...) (値はどれも str なので、str どうしの == だけが当たる)。
func inStrings(v pyjson.Value, values ...string) bool {
	s, ok := v.(string)
	return ok && slices.Contains(values, s)
}

func allIn(items []pyjson.Value, values []string) bool {
	for _, item := range items {
		if !inStrings(item, values...) {
			return false
		}
	}
	return true
}

func hasKeys(o *pyjson.Object, keys []string) bool {
	for _, key := range keys {
		if _, ok := o.Get(key); !ok {
			return false
		}
	}
	return true
}

func hasTextOrNull(v pyjson.Value, keys ...string) bool {
	o, ok := v.(*pyjson.Object)
	if !ok {
		return false
	}
	for _, key := range keys {
		item, has := o.Get(key)
		if !has || !textOrNull(item) {
			return false
		}
	}
	return true
}

func sameKeySet(o *pyjson.Object, keys []string) bool {
	return o.Len() == len(keys) && hasKeys(o, keys)
}

// distinctCount は len(set(items))。list と dict は TypeError (本家と同じ)。数は 1 == 1.0 == True で 1 つに数え、nan はどれも別に数える
// (json.loads が作る nan はそれぞれ別の object で、set は identity の次に == を見るため)。
func distinctCount(items []pyjson.Value) int {
	var seen []pyjson.Value
	for _, item := range items {
		hashable(item, "a set element")
		if !slices.ContainsFunc(seen, func(v pyjson.Value) bool { return pyops.Equal(v, item) }) {
			seen = append(seen, item)
		}
	}
	return len(seen)
}

// hashable は v が dict の鍵や set の要素になれるか確かめる。list と dict は本家と同じ文の TypeError (Python 3.15 の文。use は "a dict key" か "a set element")。
func hashable(v pyjson.Value, use string) {
	switch v.(type) {
	case []pyjson.Value, *pyjson.Object:
		name := pyops.TypeName(v)
		pyops.Raise("TypeError", "cannot use '%s' as %s (unhashable type: '%s')", name, use, name)
	}
}
