package check

import (
	"cmp"
	"math/big"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyops"
	"ja-writing/internal/pyos"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// verify の記録を探す所 (本家の _Found・_index_found・_find とそれが呼ぶもの)。記録は形を確かめる前のものも読むので、
// 欄の読みは pyops で本家と同じ例外を起こす (例外は panic で運び、runVerify の入口で error に戻す)。

var (
	indexEntryPattern  = pyre.NewLazy(`^(.+)\.([0-9]+)$`, 0)
	attemptFilePattern = pyre.NewLazy(`^attempt-([0-9]+)\.json$`, 0)
	byPathEntryPattern = pyre.NewLazy(`^([0-9]{8}T[0-9]{6}Z)\.(.+)\.([0-9]+)$`, 0)
)

// lineageLimit は _LINEAGE_LIMIT (原文をさかのぼって調べる記録の数の上限)。
const lineageLimit = 20

// failure は runVerify の中で起きた Go の誤り (作業フォルダが消えた・深すぎる JSON)。panic で運び、入口で error に戻す。
type failure struct{ err error }

func must[T any](v T, err error) T {
	if err != nil {
		panic(failure{err})
	}
	return v
}

// catchFailure は failure の panic を *err にする (pyops の例外とほかの panic はそのまま投げ直す)。
func catchFailure(err *error) {
	if r := recover(); r != nil {
		f, ok := r.(failure)
		if !ok {
			panic(r)
		}
		*err = f.err
	}
}

// groups は pyre の search の当たりのグループ (当たらなければ nil)。
func groups(p *pyre.Lazy, text string) []string {
	m, err := p.Get().Search(pyre.NewText(text), 0, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	if m == nil {
		return nil
	}
	out := make([]string, p.Get().Groups()+1)
	for i := range out {
		out[i], _ = m.Group(i)
	}
	return out
}

// listNames は path の下の名前 (Path.iterdir。読めなければ nil と false)。
func listNames(path string) ([]string, bool) {
	entries, err := pyos.ReadDir(path)
	if err != nil {
		return nil, false
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names, true
}

// attemptNo は回の番号 (先頭の 0 を落とした十進の字。0 は "0"。ゼロ値は回が無いこと)。本家は名前の数字を int で読むので、int64 に収まらない番号も
// 大きさで並べ、記録の attempt と値で比べる (int にすると、その名前を読み飛ばして本家と別の記録を選ぶ)。
// 字を土台にした型にしない: <・>・==・slices.Sort・sort.Strings が字の順 ("9" > "10") のままコンパイルを通す。比べは cmp だけを使い、
// 中身の字を直に比べない (_ の欄は == を止める)。
type attemptNo struct {
	digits string
	_      [0]func()
}

func parseAttemptNo(digits string) attemptNo {
	if trimmed := strings.TrimLeft(digits, "0"); trimmed != "" {
		return attemptNo{digits: trimmed}
	}
	return attemptNo{digits: "0"}
}

// cmp は大きさの比べ (-1・0・1)。
func (a attemptNo) cmp(b attemptNo) int {
	if len(a.digits) != len(b.digits) {
		return cmp.Compare(len(a.digits), len(b.digits))
	}
	return strings.Compare(a.digits, b.digits)
}

// String はファイルの名前と文に入れる字。
func (a attemptNo) String() string { return a.digits }

// none は回が無い (ゼロ値) か 0 のとき真 (本家の `attempt or "-"`)。
func (a attemptNo) none() bool { return a.digits == "" || a.digits == "0" }

// value は JSON の値 (int64 に収まれば int64、収まらなければ *big.Int。pyjson が読む整数と同じ形)。
func (a attemptNo) value() pyjson.Value {
	if n, err := strconv.ParseInt(a.digits, 10, 64); err == nil {
		return n
	}
	n, _ := new(big.Int).SetString(a.digits, 10)
	return n
}

// attemptNumbers は _attempt_numbers (実行のフォルダの attempt-<n>.json の n。昇順)。
func attemptNumbers(runDir string) []attemptNo {
	names, _ := listNames(runDir)
	var numbers []attemptNo
	for _, name := range names {
		if g := groups(attemptFilePattern, name); g != nil {
			numbers = append(numbers, parseAttemptNo(g[1]))
		}
	}
	slices.SortFunc(numbers, attemptNo.cmp)
	return numbers
}

func attemptFile(state, runID string, n attemptNo) string {
	return state + "/runs/" + runID + "/attempt-" + n.String() + ".json"
}

// found は _Found (見つけた記録 1 つ。実行 ID と回はファイルの場所から取る)。
type found struct {
	runID   string
	attempt attemptNo
	path    string
	status  RecordStatus
	record  *pyjson.Object
	made    int64
}

func newFound(state, runID string, attempt attemptNo) *found {
	f := &found{runID: runID, attempt: attempt, path: attemptFile(state, runID, attempt)}
	f.status, f.record = must2(ReadRecordStatus(f.path))
	if info, err := pyos.Stat(f.path); err == nil {
		f.made = info.ModTime().UnixNano()
	}
	return f
}

func must2[A, B any](a A, b B, err error) (A, B) {
	if err != nil {
		panic(failure{err})
	}
	return a, b
}

// recordOrEmpty は (self.record or {})。
func (f *found) recordOrEmpty() pyjson.Value {
	if f.record == nil {
		return pyjson.NewObject()
	}
	return f.record
}

// orderKey は order() の 1 つ目 (読める created_at か "")。
func (f *found) createdKey() string {
	created, ok := pyops.Get(f.recordOrEmpty(), "created_at").(string)
	if ok {
		if _, valid := ParseISO(created); valid {
			return created
		}
	}
	return ""
}

// less は order() どうしの <。
func (f *found) less(o *found) bool {
	if a, b := f.createdKey(), o.createdKey(); a != b {
		return a < b
	}
	if f.made != o.made {
		return f.made < o.made
	}
	return f.attempt.cmp(o.attempt) < 0
}

// final は記録の inputs.final (dict でなければ nil)。
func (f *found) final() *pyjson.Object {
	inputs, ok := pyops.Get(f.recordOrEmpty(), "inputs").(*pyjson.Object)
	if !ok {
		return nil
	}
	final, _ := pyops.Get(inputs, "final").(*pyjson.Object)
	return final
}

// latest は max(items, key=order) (最大のうち最初のもの)。
func latest(items []*found) *found {
	best := items[0]
	for _, item := range items[1:] {
		if best.less(item) {
			best = item
		}
	}
	return best
}

// indexFound は _index_found (index/by-final/<sha256>/ か index/by-text/<text_sha256>/ が指す記録。ファイルが無いものは飛ばす)。
func indexFound(state, kind, key string) []*found {
	names, ok := listNames(state + "/index/" + kind + "/" + key)
	if !ok {
		return nil
	}
	sort.Strings(names)
	var out []*found
	for _, name := range names {
		g := groups(indexEntryPattern, name)
		if g == nil || !runIDPattern.MustMatchAt0(g[1]) {
			continue
		}
		if item := newFound(state, g[1], parseAttemptNo(g[2])); item.status != RecordMissing {
			out = append(out, item)
		}
	}
	return out
}

// byPathEntries は _by_path_entries (index/by-path/<キー>/ の索引を新しい順に。[(実行 ID, 回)])。
func byPathEntries(state, key string) []found {
	folder := state + "/index/by-path/" + key
	names, ok := listNames(folder)
	if !ok {
		return nil
	}
	type entry struct {
		stamp string
		made  int64
		n     attemptNo
		runID string
	}
	var entries []entry
	for _, name := range names {
		g := groups(byPathEntryPattern, name)
		if g == nil || !runIDPattern.MustMatchAt0(g[2]) {
			continue
		}
		info, err := pyos.Stat(folder + "/" + name)
		if err != nil {
			continue
		}
		entries = append(entries, entry{g[1], info.ModTime().UnixNano(), parseAttemptNo(g[3]), g[2]})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		switch {
		case a.stamp != b.stamp:
			return a.stamp > b.stamp
		case a.made != b.made:
			return a.made > b.made
		case a.n.cmp(b.n) != 0:
			return a.n.cmp(b.n) > 0
		case a.runID != b.runID:
			return a.runID > b.runID
		}
		return false
	})
	out := make([]found, len(entries))
	for i, e := range entries {
		out[i] = found{runID: e.runID, attempt: e.n}
	}
	return out
}

// origin は _origin の値 (モード, 原文の本文のハッシュ, 資料の本文のハッシュの並び)。nil は形が分からない記録。
type origin struct {
	mode, source string
	hasSource    bool
	materials    string
}

// originOf は _origin (曖昧さを決めるのに使う、記録の原文と資料の集まり)。
func originOf(record *pyjson.Object) *origin {
	if record == nil {
		return nil
	}
	inputs, ok := pyops.Get(record, "inputs").(*pyjson.Object)
	if !ok {
		return nil
	}
	mode := pyops.Get(record, "mode")
	materials, ok := pyops.Get(inputs, "materials").([]pyjson.Value)
	if !ok {
		return nil
	}
	key, ok := materialsKey(materials)
	if !ok {
		return nil
	}
	if mode == "rewrite" {
		if source, ok := pyops.Get(inputs, "source").(*pyjson.Object); ok {
			if text, ok := pyops.Get(source, "text_sha256").(string); ok {
				return &origin{mode: "rewrite", source: text, hasSource: true, materials: key}
			}
		}
		return nil
	}
	if mode == "compose" && len(materials) > 0 {
		return &origin{mode: "compose", materials: key}
	}
	return nil
}

// materialsKey は資料の本文のハッシュを並べた tuple (要素がすべて text_sha256 の str を持つ dict のときだけ)。
func materialsKey(materials []pyjson.Value) (string, bool) {
	hashes := make([]string, len(materials))
	for i, item := range materials {
		text, ok := pyops.Get(dictOrNil(item), "text_sha256").(string)
		if _, isDict := item.(*pyjson.Object); !isDict || !ok {
			return "", false
		}
		hashes[i] = strconv.Quote(text)
	}
	sort.Strings(hashes)
	return strings.Join(hashes, ","), true
}

// dictOrNil は v が dict ならそれ、でなければ (nil の *Object も) 空の dict (本家の「d if isinstance(d, dict) else {}」。get を当てても例外にしないため)。
func dictOrNil(v pyjson.Value) *pyjson.Object {
	if o, ok := v.(*pyjson.Object); ok && o != nil {
		return o
	}
	return pyjson.NewObject()
}

func (o *origin) key() string {
	if o == nil {
		return "-"
	}
	return o.mode + "\x00" + strconv.FormatBool(o.hasSource) + strconv.Quote(o.source) + "\x00" + o.materials
}

// finalMatches は _final_matches (記録の最終稿と今の最終稿のハッシュが同じか)。
func finalMatches(record *pyjson.Object, final *Target) bool {
	if record == nil {
		return false
	}
	if final.Via == "file" {
		return pyops.Equal(pyops.Get(record, "sha256"), final.SHA256)
	}
	return final.TextSHA256 != nil && pyops.Equal(pyops.Get(record, "text_sha256"), *final.TextSHA256)
}

// sourceMatches は _source_matches (記録の原文と --source が同じか。新規作成で資料を原文とみなした記録は、その資料と比べる)。
func sourceMatches(record *pyjson.Object, source *Target) bool {
	var recorded pyjson.Value
	if record != nil {
		if inputs, ok := pyops.Get(record, "inputs").(*pyjson.Object); ok {
			recorded = pyops.Get(inputs, "source")
		}
		if recorded == nil && pyops.Get(record, "mode") == "compose" {
			if t := recordTreated(record); t != nil {
				recorded = t
			}
		}
	}
	o, ok := recorded.(*pyjson.Object)
	if !ok {
		return false
	}
	if pyops.Equal(pyops.Get(o, "via"), "file") && source.Via == "file" {
		return pyops.Equal(pyops.Get(o, "sha256"), source.SHA256)
	}
	return source.TextSHA256 != nil && pyops.Equal(pyops.Get(o, "text_sha256"), *source.TextSHA256)
}

// expectedToken は _expected_token (final_changed の expected=。今の最終稿と同じ種類のハッシュ)。
func expectedToken(record *pyjson.Object, final *Target) *string {
	prefix, key := "sha256:", "sha256"
	if final.Via == "stdin" {
		prefix, key = "text-sha256:", "text_sha256"
	}
	value, ok := pyops.Get(record, key).(string)
	if !ok || !hex64Pattern.MustMatchAt0(value) {
		return nil
	}
	token := prefix + value[:12]
	return &token
}

func normalizeEOL(data []byte) string {
	return strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
}

// onlyLineEndings は _only_line_endings (記録の最終稿の写しと、今の最終稿が改行コードだけ違うか)。
func onlyLineEndings(state string, record *pyjson.Object, final *Target) bool {
	if final.Via != "file" {
		return false
	}
	value, ok := pyops.Get(record, "sha256").(string)
	if !ok || !hex64Pattern.MustMatchAt0(value) {
		return false
	}
	recorded, err := pyos.ReadFile(state + "/inputs/" + value + ".bin")
	if err != nil {
		return false
	}
	return string(recorded) != string(final.Data) && normalizeEOL(recorded) == normalizeEOL(final.Data)
}

// sameFinalPlace は _same_final_place (記録の最終稿と今の最終稿が同じ場所か。パスが null どうしは同じ)。
func sameFinalPlace(record *pyjson.Object, final *Target) bool {
	recorded := pyops.Get(record, "path")
	if final.Path == nil || recorded == nil {
		return final.Path == nil && recorded == nil
	}
	s, ok := recorded.(string)
	return ok && must(SamePath(s, *final.Path))
}

// ambiguity は _ambiguity (実行 ID の並び (新しい順、重なりなし) と曖昧さの種類)。
func ambiguity(items []*found, kind string) ([]string, string) {
	ordered := slices.Clone(items)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[j].less(ordered[i]) })
	var runIDs []string
	for _, item := range ordered {
		if !slices.Contains(runIDs, item.runID) {
			runIDs = append(runIDs, item.runID)
		}
	}
	if kind == "" {
		kind = "source"
		known, sawNone := map[string]*origin{}, false
		for _, item := range items {
			o := originOf(item.record)
			if o == nil {
				sawNone = true
				continue
			}
			known[o.key()] = o
		}
		heads, allRewrite := map[string]bool{}, true
		for _, o := range known {
			heads[strconv.Quote(o.mode)+strconv.FormatBool(o.hasSource)+strconv.Quote(o.source)] = true
			allRewrite = allRewrite && o.mode == "rewrite"
		}
		if !sawNone && len(heads) == 1 && allRewrite {
			kind = "materials"
		}
	}
	return runIDs, kind
}

// placeOrigin は _place_origin (最終稿のパスの記録どうしで比べる原文)。nil は形が分からない記録。
func placeOrigin(record *pyjson.Object) *string {
	if record == nil {
		return nil
	}
	inputs, ok := pyops.Get(record, "inputs").(*pyjson.Object)
	if !ok {
		return nil
	}
	var item pyjson.Value
	switch pyops.Get(record, "mode") {
	case "rewrite":
		item = pyops.Get(inputs, "source")
	case "compose":
		if t := recordTreated(record); t != nil {
			item = t
		} else {
			materials, ok := pyops.Get(inputs, "materials").([]pyjson.Value)
			if !ok || len(materials) == 0 {
				return nil
			}
			key, ok := materialsKey(materials)
			if !ok {
				return nil
			}
			value := "materials\x00" + key
			return &value
		}
	default:
		return nil
	}
	if o, ok := item.(*pyjson.Object); ok {
		if text, ok := pyops.Get(o, "text_sha256").(string); ok {
			value := "text\x00" + text
			return &value
		}
	}
	return nil
}

// continuesTo は _continues_to (記録の原文から、その本文を最終稿として検査した記録へさかのぼり、本文のハッシュ target に行き着くか)。
func continuesTo(state string, record *pyjson.Object, target string) bool {
	pending := []*pyjson.Object{record}
	seen := map[string]bool{}
	looked := 0
	for len(pending) > 0 && looked < lineageLimit {
		next := pending[0]
		pending = pending[1:]
		o := placeOrigin(next)
		looked++
		if o == nil || !strings.HasPrefix(*o, "text\x00") {
			continue
		}
		text := strings.TrimPrefix(*o, "text\x00")
		if seen[text] {
			continue
		}
		if text == target {
			return true
		}
		seen[text] = true
		for _, item := range indexFound(state, "by-text", text) {
			if item.status == RecordOK {
				pending = append(pending, item.record)
			}
		}
	}
	return false
}

// placeKey は by-path のキー (_place の綴りの SHA-256)。
func placeKey(path string) string {
	return textKey(must(Place(path)))
}

// otherOriginAtPlace は _other_origin_at_place (今の最終稿のパスの最新の記録が、見つけた記録と別の原文なら、その記録)。
func otherOriginAtPlace(state string, final *Target, best *found) *found {
	if final.Via != "file" {
		return nil
	}
	mine := placeOrigin(best.record)
	if mine == nil {
		return nil
	}
	for _, entry := range byPathEntries(state, placeKey(*final.Path)) {
		item := newFound(state, entry.runID, entry.attempt)
		var theirs *string
		if item.status == RecordOK {
			theirs = placeOrigin(item.record)
		}
		if theirs == nil {
			continue
		}
		if *theirs == *mine {
			return nil
		}
		if target, ok := pyops.Get(dictOrNil(item.final()), "text_sha256").(string); ok && continuesTo(state, best.record, target) {
			return nil
		}
		return item
	}
	return nil
}

// findResult は _find の結果。kind は found・ambiguous・final_changed・wrong_file・invalid・no_record。
type findResult struct {
	kind   string
	item   *found
	runIDs []string
	reason string // ambiguous の種類 (materials・source・place)
}

// find は _find (記録を探す)。
func find(state string, final *Target, runID *string, source *Target) findResult {
	if runID != nil {
		var items []*found
		for _, n := range attemptNumbers(state + "/runs/" + *runID) {
			if item := newFound(state, *runID, n); item.status != RecordMissing {
				items = append(items, item)
			}
		}
		if len(items) == 0 {
			return findResult{kind: "no_record"}
		}
		var matching, readable []*found
		for _, item := range items {
			if item.status == RecordOK {
				readable = append(readable, item)
				if finalMatches(item.final(), final) {
					matching = append(matching, item)
				}
			}
		}
		if len(matching) > 0 {
			return findResult{kind: "found", item: maxAttempt(matching)}
		}
		pool := readable
		if len(pool) == 0 {
			pool = items
		}
		last := maxAttempt(pool)
		record := last.final()
		if record == nil {
			return findResult{kind: "invalid", item: last}
		}
		if sameFinalPlace(record, final) {
			return findResult{kind: "final_changed", item: last}
		}
		return findResult{kind: "wrong_file", item: last}
	}
	kind, key := "by-final", final.SHA256
	if final.Via == "stdin" {
		if final.TextSHA256 == nil {
			pyops.Raise("AssertionError", "") // 本家の assert key is not None (verify_quick に UTF-8 でないバイト列を渡したとき)
		}
		kind, key = "by-text", *final.TextSHA256
	}
	if candidates := indexFound(state, kind, key); len(candidates) > 0 {
		var pool []*found
		for _, item := range candidates {
			if item.status == RecordOK && originOf(item.record) != nil {
				pool = append(pool, item)
			}
		}
		if len(pool) == 0 {
			pool = candidates
		}
		if source != nil {
			var narrowed []*found
			for _, item := range pool {
				if sourceMatches(item.record, source) {
					narrowed = append(narrowed, item)
				}
			}
			if len(narrowed) > 0 {
				pool = narrowed
			}
		}
		origins := map[string]bool{}
		for _, item := range pool {
			origins[originOf(item.record).key()] = true
		}
		if len(origins) > 1 {
			ids, reason := ambiguity(pool, "")
			return findResult{kind: "ambiguous", runIDs: ids, reason: reason}
		}
		best := latest(pool)
		if source == nil {
			if other := otherOriginAtPlace(state, final, best); other != nil {
				ids, reason := ambiguity([]*found{best, other}, "place")
				return findResult{kind: "ambiguous", runIDs: ids, reason: reason}
			}
		}
		return findResult{kind: "found", item: best}
	}
	var key2 string
	switch {
	case final.Via == "file":
		key2 = placeKey(*final.Path)
	case source != nil && source.TextSHA256 != nil:
		key2 = textKey("stdin:" + *source.TextSHA256)
	default:
		return findResult{kind: "no_record"}
	}
	for _, entry := range byPathEntries(state, key2) {
		item := newFound(state, entry.runID, entry.attempt)
		if item.status == RecordOK && item.final() != nil {
			if finalMatches(item.final(), final) {
				return findResult{kind: "found", item: item}
			}
			return findResult{kind: "final_changed", item: item}
		}
	}
	return findResult{kind: "no_record"}
}

// maxAttempt は max(items, key=attempt) (最大のうち最初のもの)。
func maxAttempt(items []*found) *found {
	best := items[0]
	for _, item := range items[1:] {
		if item.attempt.cmp(best.attempt) > 0 {
			best = item
		}
	}
	return best
}

// newerAttempt は _newer_attempt (見つけた記録より後の回で、別の本文を検査した回の番号。なければ偽)。
func newerAttempt(state string, f *found, record *pyjson.Object) (attemptNo, bool) {
	mine := pyops.Get(pyops.Index(pyops.Index(record, "inputs"), "final"), "text_sha256")
	numbers := attemptNumbers(state + "/runs/" + f.runID)
	for i := len(numbers) - 1; i >= 0; i-- {
		number := numbers[i]
		if number.cmp(f.attempt) <= 0 {
			break
		}
		later := must(ReadRecord(attemptFile(state, f.runID, number)))
		var recorded *pyjson.Object
		if later != nil {
			if inputs, ok := pyops.Get(later, "inputs").(*pyjson.Object); ok {
				recorded, _ = pyops.Get(inputs, "final").(*pyjson.Object)
			}
		}
		if recorded != nil && !pyops.Equal(pyops.Get(recorded, "text_sha256"), mine) {
			return number, true
		}
	}
	return attemptNo{}, false
}

// recordedAsInput は _recorded_as_input (--final に渡した本文が、記録では原文か資料として使われているか)。
func recordedAsInput(state string, final *Target) bool {
	if info := fileInfo(state+"/inputs/"+final.SHA256+".bin", true); info == nil || !info.Mode().IsRegular() {
		return false
	}
	names, ok := listNames(state + "/runs")
	if !ok {
		return false
	}
	var runIDs []string
	for _, name := range names {
		if runIDPattern.MustMatchAt0(name) {
			runIDs = append(runIDs, name)
		}
	}
	sort.Strings(runIDs)
	for _, runID := range runIDs {
		for _, n := range attemptNumbers(state + "/runs/" + runID) {
			record := must(ReadRecord(attemptFile(state, runID, n)))
			if record == nil {
				continue
			}
			inputs, ok := pyops.Get(record, "inputs").(*pyjson.Object)
			if !ok {
				continue
			}
			items := []pyjson.Value{pyops.Get(inputs, "source")}
			if materials, ok := pyops.Get(inputs, "materials").([]pyjson.Value); ok {
				items = append(items, materials...)
			}
			for _, item := range items {
				if o, ok := item.(*pyjson.Object); ok && pyops.Equal(pyops.Get(o, "sha256"), final.SHA256) {
					return true
				}
			}
		}
	}
	return false
}

// previousTreated は _previous_treated (同じ実行の attempt より前の読める最新の記録が、原文とみなした資料)。verify は回を必ず渡す (本家の attempt が None の枝は run が使う)。
func previousTreated(state, runID string, attempt attemptNo) *pyjson.Object {
	numbers := attemptNumbers(state + "/runs/" + runID)
	for i := len(numbers) - 1; i >= 0; i-- {
		number := numbers[i]
		if number.cmp(attempt) >= 0 {
			continue
		}
		record := must(ReadRecord(attemptFile(state, runID, number)))
		if record == nil {
			continue
		}
		if pyops.Get(record, "mode") != "compose" {
			return nil
		}
		return recordTreated(record)
	}
	return nil
}

// fileSHA256 は _file_sha256。
func fileSHA256(path string) (string, error) {
	data, err := pyos.ReadFile(path)
	if err != nil {
		return "", err
	}
	return SHA256(data), nil
}

// pathParts は Path(state, *rel.split('/')) の文字列 (空の部分と「.」を落とす)。
func pathParts(state, rel string) string {
	parts := []string{state}
	for _, p := range strings.Split(rel, "/") {
		if p != "" && p != "." {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "/")
}

// snapshotText は _snapshot_text (記録の Input の写しの decode_source の本文。読めない・ハッシュが合わない・UTF-8 でなければ nil)。
func snapshotText(state string, item pyjson.Value) *string {
	o, ok := item.(*pyjson.Object)
	if !ok {
		return nil
	}
	sha, ok := pyops.Get(o, "sha256").(string)
	if !ok {
		return nil
	}
	data, err := pyos.ReadFile(state + "/inputs/" + sha + ".bin")
	if err != nil || SHA256(data) != sha {
		return nil
	}
	text, _, err := visibility.DecodeSourceBytes(data)
	if err != nil {
		return nil
	}
	return &text
}

// treatedAgain は _treated_again (新規作成の記録の写しから、原文とみなす資料の番号と決め方を求め直す)。index は 1 から (0 は None)。
func treatedAgain(state string, record *pyjson.Object, runID string, attempt attemptNo) (known bool, index int, by string) {
	if pyops.Index(record, "mode") != "compose" {
		return false, 0, ""
	}
	inputs := pyops.Index(record, "inputs")
	final := snapshotText(state, pyops.Index(inputs, "final"))
	materials := pyops.Iter(pyops.Index(inputs, "materials"))
	texts := make([]string, 0, len(materials))
	for _, item := range materials {
		text := snapshotText(state, item)
		if text == nil {
			return false, 0, ""
		}
		texts = append(texts, *text)
	}
	if final == nil {
		return false, 0, ""
	}
	if best, _ := SourceLike(texts, *final, false); best != 0 {
		return true, best, "similarity"
	}
	if marked := previousTreated(state, runID, attempt); marked != nil {
		for i, item := range materials {
			if pyops.Equal(pyops.Get(item, "text_sha256"), pyops.Get(marked, "text_sha256")) {
				return true, i + 1, "previous_attempt"
			}
		}
	}
	return true, 0, ""
}

// treatedLabel は _treated_label (「資料1」「資料1（前の回から引き継ぎ）」「なし」)。
func treatedLabel(index pyjson.Value, treatedBy pyjson.Value) string {
	if index == nil {
		return "なし"
	}
	suffix := ""
	if pyops.Equal(treatedBy, "previous_attempt") {
		suffix = "（前の回から引き継ぎ）"
	}
	return "資料" + pyops.Format(index) + suffix
}

// recheckProblem は _recheck_problem (写しから原文とみなす資料を求め直してよいか。よければ ""、だめなら "unicode" か "tool")。
func recheckProblem(record *pyjson.Object, current *pyjson.Object) string {
	if version := pyops.Get(pyops.Index(record, "python"), "unicode_version"); version != nil && !pyops.Equal(version, pyunicode.UnicodeVersion) {
		return "unicode"
	}
	recorded := pyops.Get(pyops.Index(pyops.Index(record, "tools"), "yomiyasu_check.py"), "sha256")
	if !pyops.Equal(recorded, pyops.Index(pyops.Index(current, "yomiyasu_check.py"), "sha256")) {
		return "tool"
	}
	return ""
}

// requiredFor は _required_for (必要な検査と、記録の required_checks を合わせたもの)。treated が nil なら記録の treated_as_source で決める。
func requiredFor(record *pyjson.Object, treated *bool) []string {
	isTreated := recordTreated(record) != nil
	if treated != nil {
		isTreated = *treated
	}
	expected := modeChecks(pyops.Str(pyops.Index(record, "mode")), pyops.Truthy(pyops.Index(pyops.Index(record, "inputs"), "materials")), isTreated)
	out := slices.Clone(expected)
	for _, name := range pyops.Iter(pyops.Index(record, "required_checks")) {
		s := pyops.Str(name)
		if !slices.Contains(expected, s) {
			out = append(out, s)
		}
	}
	return out
}

// diffSourceItem は _diff_source_item (記録の diff の原文の Input)。
func diffSourceItem(record *pyjson.Object) *pyjson.Object {
	if source, ok := pyops.Index(pyops.Index(record, "inputs"), "source").(*pyjson.Object); ok {
		return source
	}
	return recordTreated(record)
}

// expectedTarget は _expected_target (各検査の target に入るはずのハッシュ)。
func expectedTarget(record *pyjson.Object, name string) *pyjson.Object {
	inputs := pyops.Index(record, "inputs")
	final := pyops.Index(pyops.Index(inputs, "final"), "sha256")
	var source pyjson.Value
	if item := diffSourceItem(record); item != nil {
		source = pyops.Index(item, "sha256")
	}
	target := pyjson.NewObject()
	switch name {
	case "lint":
		target.Set("final", final)
	case "diff":
		target.Set("source", source)
		target.Set("final", final)
	default:
		var hashes []pyjson.Value
		for _, entry := range pyops.Iter(pyops.Index(inputs, "materials")) {
			hashes = append(hashes, pyops.Index(entry, "sha256"))
		}
		if hashes == nil {
			hashes = []pyjson.Value{}
		}
		target.Set("materials", hashes)
		target.Set("final", final)
		if source != nil {
			target.Set("source", source)
		}
	}
	return target
}

// verifyState は _verify_record の状態 (状態の名前と、incomplete・stopped・replay_timeout の中身)。
type verifyState struct {
	name    string
	names   []string    // incomplete と replay_timeout の検査の名前
	stopped [][2]string // stopped の (検査の名前, 状態)
}

type problem struct{ code, detail string }

// verifyRecord は _verify_record (見つけた記録を確かめる。最初に当てはまった状態と、見つかった食い違いのすべて)。
// replay は --replay の作り直し (付けないときは nil)。ここまでで食い違いが無いときだけ動かす。
func verifyRecord(state string, f *found, final *Target, source *Target, since *time.Time, replay *replayer, files ToolFiles) (verifyState, []problem, string, bool) {
	var problems []problem
	var states []verifyState
	fail := func(stateName, code, detail string) {
		problems = append(problems, problem{code, detail})
		if stateName != "" {
			states = append(states, verifyState{name: stateName})
		}
	}
	record := f.record
	if f.status != RecordOK || record == nil {
		fail("invalid_record", "unreadable_record", "JSON のオブジェクトとして読めません")
		return verifyState{name: "invalid_record"}, problems, "skipped", false
	}
	if pyops.Get(record, "format") != "yomiyasu-check-record" || !isExactInt(pyops.Get(record, "format_version"), 1) {
		fail("invalid_record", "record_format", "format と format_version が yomiyasu-check-record の形式1ではありません")
		return verifyState{name: "invalid_record"}, problems, "skipped", false
	}
	if wrong := must(RecordShape(record)); len(wrong) > 0 {
		fail("invalid_record", "record_shape", "キーか型が違います: "+strings.Join(wrong, ", "))
		return verifyState{name: "invalid_record"}, problems, "skipped", false
	}
	if !pyops.Equal(pyops.Index(record, "run_id"), f.runID) || !pyops.Equal(pyops.Index(record, "attempt"), f.attempt.value()) {
		fail("invalid_record", "record_location", "run_id と attempt が記録のファイルの場所（"+f.runID+"、"+f.attempt.String()+"回目）と合いません")
	}
	if !pyops.Equal(pyops.Index(record, "record_digest"), must(RecordDigest(record))) {
		fail("invalid_record", "record_digest", "record_digest が記録の中身と合いません")
	}
	if !finalMatches(asObject(pyops.Index(pyops.Index(record, "inputs"), "final")), final) {
		fail("invalid_record", "final_hash", "記録の inputs.final のハッシュが今の最終稿と合いません")
	}
	options, _ := pyops.Get(record, "options").(*pyjson.Object)
	factGaps := options != nil && pyops.Get(options, "fact_gaps") == true
	current := ToolVersions(files, factGaps)
	recordedTreated := recordTreated(record)
	treated := recordedTreated != nil
	reason := ""
	if pyops.Index(record, "mode") == "compose" {
		reason = recheckProblem(record, current)
	}
	var known bool
	var again int
	var againBy string
	switch reason {
	case "":
		known, again, againBy = treatedAgain(state, record, f.runID, f.attempt)
	case "unicode":
		fail("", "treated_not_rechecked", "Unicode のバージョンが記録（"+pyops.Format(pyops.Get(pyops.Index(record, "python"), "unicode_version"))+
			"）と今（"+pyunicode.UnicodeVersion+"）で違うので、原文とみなす資料を写しから求め直していません")
	}
	if known {
		var againValue, againByValue pyjson.Value
		if again != 0 {
			againValue = int64(again)
		}
		if againBy != "" {
			againByValue = againBy
		}
		var recordedBy pyjson.Value
		if recordedTreated != nil {
			recordedBy = pyops.GetOr(recordedTreated, "treated_by", againByValue)
		}
		if (again == 0) != (recordedTreated == nil) || (again != 0 && recordedTreated != nil &&
			(!pyops.Equal(pyops.Get(recordedTreated, "index"), againValue) || !pyops.Equal(recordedBy, againByValue))) {
			was := "なし"
			if recordedTreated != nil {
				was = treatedLabel(pyops.Get(recordedTreated, "index"), recordedBy)
			}
			fail("invalid_record", "treated_as_source", "写しから求め直すと、原文とみなす資料は"+treatedLabel(againValue, againByValue)+"です（記録は"+was+"）")
		}
		treated = again != 0
	}
	if mismatched := summaryProblems(state, record, requiredFor(record, &treated)); len(mismatched) > 0 {
		fail("invalid_record", "summary_mismatch", "記録の件数がレポートと合いません: "+strings.Join(mismatched, ", "))
	}
	trusted := len(states) == 0
	if source != nil && !sourceMatches(record, source) {
		fail("other_source", "other_source", "記録の原文と --source のハッシュが違います")
	}
	createdText := pyops.Str(pyops.Index(record, "created_at"))
	created, _ := ParseISO(createdText)
	if since != nil && created.Before(*since) {
		fail("stale_record", "stale_record", "記録の created_at "+createdText+" が --since より前です")
	}
	entries, order := checkEntries(record)
	var missing []string
	for _, name := range requiredFor(record, &treated) {
		entry, ok := entries[name]
		switch {
		case !ok || !pyops.Equal(pyops.Index(entry, "status"), "ran"):
			detail := name
			if ok {
				detail = name + "（" + pyops.Format(pyops.Index(entry, "status")) + "）"
			}
			fail("", "missing_check", detail)
			missing = append(missing, name)
		case !pyops.Equal(pyops.Index(entry, "target"), expectedTarget(record, name)):
			fail("", "check_target", name+" の target が inputs と違います")
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		var stopped [][2]string
		for _, name := range missing {
			if entry, ok := entries[name]; ok {
				if status, ok := pyops.Index(entry, "status").(string); ok && failedNames[status] != "" {
					stopped = append(stopped, [2]string{name, status})
				}
			}
		}
		if len(stopped) == len(missing) {
			states = append(states, verifyState{name: "stopped", stopped: stopped})
		} else {
			states = append(states, verifyState{name: "incomplete", names: missing})
		}
	}
	runPart := "runs/" + f.runID + "/attempt-" + f.attempt.String()
	inputs := pyops.Index(record, "inputs")
	type labeled struct {
		label string
		item  pyjson.Value
	}
	var items []labeled
	if src := pyops.Index(inputs, "source"); src != nil {
		items = append(items, labeled{"source", src})
	}
	for _, item := range pyops.Iter(pyops.Index(inputs, "materials")) {
		items = append(items, labeled{"material[" + pyops.Format(pyops.Index(item, "index")) + "]", item})
	}
	items = append(items, labeled{"final", pyops.Index(inputs, "final")})
	for _, it := range items {
		sha := pyops.Str(pyops.Index(it.item, "sha256"))
		got, err := fileSHA256(state + "/inputs/" + sha + ".bin")
		if err != nil || got != sha {
			fail("invalid_record", "snapshot", it.label+" の写し "+pyops.Format(pyops.Index(it.item, "snapshot"))+" がないか、ハッシュが合いません")
		}
	}
	for _, name := range order {
		entry := entries[name]
		if !pyops.Equal(pyops.Index(entry, "status"), "ran") {
			continue
		}
		for _, part := range [][3]string{{"report", ".json", "report_sha256"}, {"text", ".txt", "text_sha256"}} {
			expected := runPart + "." + name + part[1]
			if !pyops.Equal(pyops.Index(entry, part[0]), expected) {
				fail("invalid_record", "report", name+" の "+part[0]+" が "+expected+" ではありません")
				continue
			}
			got, err := fileSHA256(pathParts(state, expected))
			if err != nil || !pyops.Equal(got, pyops.Index(entry, part[2])) {
				fail("invalid_record", "report", expected+" がないか、ハッシュが記録と合いません")
			}
		}
	}
	tools := pyops.Index(record, "tools").(*pyjson.Object)
	for _, name := range ToolNames {
		if !pyops.Equal(pyops.Index(pyops.Index(current, name), "sha256"), pyops.Index(pyops.Index(tools, name), "sha256")) {
			fail("tool_changed", "tool_changed", name)
		}
	}
	keys := listKeys()
	if factGaps {
		keys = append(keys, "references/lists/"+FactGapsList+".txt")
	}
	for _, key := range keys {
		if !pyops.Equal(ListSHA256(current, key), ListSHA256(tools, key)) {
			fail("tool_changed", "tool_changed", key)
		}
	}
	if !pyops.Equal(pyops.Index(SkillInfo(), "skill_md_sha256"), pyops.Index(pyops.Index(record, "skill"), "skill_md_sha256")) {
		fail("", "skill_changed", "SKILL.md のハッシュが記録と違います")
	}
	if trusted {
		if newer, ok := newerAttempt(state, f, record); ok {
			fail("", "newer_attempt_differs", newer.String()+"回目で別の本文を検査しています")
		}
	}
	// 作り直し (ここまでで食い違いがないときだけ)
	replayed := "skipped"
	if replay != nil && len(states) == 0 {
		var found []problem
		found, replayed = replay.run(state, record, files)
		problems = append(problems, found...)
		switch replayed {
		case "mismatch":
			states = append(states, verifyState{name: "not_reproducible"})
		case "timeout":
			var names []string
			for _, p := range found {
				if p.code == "replay_timeout" {
					names = append(names, p.detail)
				}
			}
			states = append(states, verifyState{name: "replay_timeout", names: names})
		}
	}
	if len(states) == 0 {
		return verifyState{name: "executed"}, problems, replayed, trusted
	}
	return states[0], problems, replayed, trusted
}

// asObject は v が dict ならそれ、でなければ nil。
func asObject(v pyjson.Value) *pyjson.Object {
	o, _ := v.(*pyjson.Object)
	return o
}

// checkEntries は dict((entry["name"], entry) for entry in record["checks"]) (同じ名前は後のものが勝ち、並びは最初に出た位置)。
func checkEntries(record *pyjson.Object) (map[string]pyjson.Value, []string) {
	entries := map[string]pyjson.Value{}
	var order []string
	for _, entry := range pyops.Iter(pyops.Index(record, "checks")) {
		name := pyops.Index(entry, "name")
		pyops.Hashable(name)
		key := pyops.Str(name)
		if _, ok := entries[key]; !ok {
			order = append(order, key)
		}
		entries[key] = entry
	}
	return entries, order
}

// blank は str.strip() が空か (本文が空白と改行だけか)。
func blank(text string) bool { return pystr.Strip(text) == "" }
