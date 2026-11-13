package lint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyunicode"
	"ja-writing/oracle"
	"ja-writing/visibility"
)

const (
	goldenDir = "../testdata/golden"
	corpusDir = "../testdata/corpus"
)

// このファイルは、本家 (Python) の記録 (testdata/golden/lint-base.json) と、lint の土台 (絵文字・文末の飾り・文字の下ごしらえ) の Go の実装を突き合わせる。
// 記録は oracle.LintBaseCases の呼び出しを本家に通した結果の digest (正規の JSON の SHA-256 の先頭 8 字) で、Go も同じ呼び出しを通して同じ正規の JSON を作る。
// 正規の JSON は、本家の sort_keys=True を再現する代わりに、組む側が鍵を昇順 (整数の鍵は数の順) に並べて入れ、並べ替えは使わない
// (整数の鍵の dict は、本家は整数の大小で並べてから文字にするので、文字の順に並べ替えると食い違う)。

var canonical = pyjson.Options{Separators: &pyjson.Separators{Item: ",", Key: ":"}}

type replayer struct {
	reg map[string]any
}

func list(vs ...pyjson.Value) pyjson.Value { return append([]pyjson.Value{}, vs...) }

func stringList(ss []string) pyjson.Value {
	return mapList(ss, func(s string) pyjson.Value { return s })
}

func mapList[T any](items []T, f func(T) pyjson.Value) pyjson.Value {
	out := make([]pyjson.Value, 0, len(items))
	for _, item := range items {
		out = append(out, f(item))
	}
	return out
}

func rangesValue(rs []runeRange) pyjson.Value {
	out := make([]pyjson.Value, len(rs))
	for i, r := range rs {
		out[i] = list(int64(r.lo), int64(r.hi))
	}
	return out
}

func runePairsValue(ps [][2]rune, swap bool) pyjson.Value {
	out := make([]pyjson.Value, len(ps))
	for i, p := range ps {
		a, b := p[0], p[1]
		if swap {
			a, b = b, a
		}
		out[i] = list(string(a), string(b))
	}
	return out
}

// sortedStringObject は鍵 (1 字) を昇順に並べた dict (本家の sort_keys=True)。
func sortedStringObject(pairs [][2]rune, keyIndex int) pyjson.Value {
	o := pyjson.NewObject()
	sorted := slices.Clone(pairs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i][keyIndex] < sorted[j][keyIndex] })
	for _, p := range sorted {
		o.Set(string(p[keyIndex]), string(p[1-keyIndex]))
	}
	return o
}

func constantValue(name string) (pyjson.Value, bool) {
	switch name {
	case "_EMOJI_DATA_VERSION":
		return emojiDataVersion, true
	case "_EMOJI_REGISTERED_TEXT_BASES":
		return rangesValue(emojiRegisteredTextBases[:]), true
	case "_EMOJI_EXPLICIT_VARIATION_BASES":
		return rangesValue(emojiExplicitVariationBases[:]), true
	case "_EMOJI_OTHER_BASES":
		return rangesValue(emojiOtherBases[:]), true
	case "_EMOJI_FLAG_REGION_CODES":
		return stringList(emojiFlagRegionCodes[:]), true
	case "_END_SYMBOL_RANGES":
		return rangesValue(endSymbolRanges), true
	case "_END_JOINER_RANGES":
		return rangesValue(endJoinerRanges), true
	case "_END_VISIBLE_RANGES":
		return rangesValue(endVisibleRanges), true
	case "_END_VISIBLE_CHARS":
		return endVisibleChars(), true
	case "_END_JOINER_CHARS":
		class := emojiCharacterClass(endJoinerRanges)
		return class[1 : len(class)-1], true
	case "_END_DECORATION_CHARS":
		return endDecorationChars(), true
	case "_END_KEYCAP":
		return endKeycapPattern, true
	case "_END_DECORATION_RUN":
		return endDecorationRun(), true
	case "_END_BRACKETS":
		return sortedStringObject(endBracketPairs[:], 0), true
	case "_END_CLOSERS":
		return endClosers, true
	case "_END_QUOTES":
		return sortedStringObject(endQuotePairs[:], 0), true
	case "_END_MARKS":
		return endMarks, true
	case "_END_BREAK_HEAD_CHARS":
		return endBreakHeadChars, true
	case "_END_BREAK_PAIRS":
		return runePairsValue(endBreakPairs[:], false), true
	case "_QUESTION_CLOSERS":
		return questionClosers, true
	case "_QUESTION_SUFFIXES":
		return stringList(questionSuffixes), true
	}
	return tableConstantValue(name)
}

func argString(t *testing.T, a any) string {
	t.Helper()
	switch v := a.(type) {
	case string:
		return v
	case oracle.Cps:
		return pyunicode.FromRunes(v)
	}
	t.Fatalf("文字列でない引数 %T", a)
	return ""
}

func argInt(t *testing.T, a any) int {
	t.Helper()
	return argAs[int](t, a)
}

func argAs[T any](t *testing.T, a any) T {
	t.Helper()
	v, ok := a.(T)
	if !ok {
		t.Fatalf("引数の型が違う: %T", a)
	}
	return v
}

func (r *replayer) document(t *testing.T, a any) *document {
	t.Helper()
	ref, ok := a.(oracle.Ref)
	if !ok {
		t.Fatalf("Ref でない引数 %T", a)
	}
	d, ok := r.reg[string(ref)].(*document)
	if !ok {
		t.Fatalf("Ref %s が analysis でない", ref)
	}
	return d
}

func argRanges(t *testing.T, a any) []runeRange {
	t.Helper()
	items, ok := a.([]any)
	if !ok {
		t.Fatalf("区間の並びでない引数 %T", a)
	}
	out := make([]runeRange, len(items))
	for i, item := range items {
		pair := item.([]any)
		out[i] = runeRange{rune(argInt(t, pair[0])), rune(argInt(t, pair[1]))}
	}
	return out
}

func stringsFromArg(t *testing.T, a any) []string {
	t.Helper()
	items := a.([]any)
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = argString(t, item)
	}
	return out
}

func (r *replayer) invoke(t *testing.T, c oracle.Call) pyjson.Value {
	t.Helper()
	if c.Get {
		v, ok := constantValue(c.Function)
		if !ok {
			t.Fatalf("定数 %s を Go に持っていない", c.Function)
		}
		return v
	}
	a := c.Args
	var result pyjson.Value
	switch c.Function {
	case "analyze_markdown":
		an, err := visibility.AnalyzeMarkdown(argString(t, a[0]), true)
		if err != nil {
			t.Fatalf("analysis を作れない: %v", err)
		}
		r.reg[c.Save] = newDocument(an)
		return "quiet"
	case "_v220_load_list":
		result, _ = tableFunctionValue(c.Function)
	case "_emoji_matches":
		result = stringList(EmojiMatches(argString(t, a[0])))
	case "_emoji_character_class":
		result = emojiCharacterClass(argRanges(t, a[0]))
	case "_emoji_flag_pattern":
		result = emojiFlagPattern()
	case "_merged_ranges":
		result = rangesValue(mergedRanges(argRanges(t, a[0])))
	case "_bracket_opens_sentence":
		closer := pyunicode.Runes(argString(t, a[2]))
		result = bracketOpensSentence(pyunicode.Runes(argString(t, a[0])), argInt(t, a[1]), closer[0])
	case "_strip_end_decoration":
		closers := ""
		if len(a) > 1 {
			closers = argString(t, a[1])
		}
		core, _ := stripEndDecoration(argString(t, a[0]), closers)
		result = core
	case "_decoration_breaks":
		if pieces := decorationBreaks(argString(t, a[0])); pieces != nil {
			result = stringList(pieces)
		}
	case "_with_undecorated":
		closers := endClosers
		if len(a) > 1 {
			closers = argString(t, a[1])
		}
		result = stringList(withUndecorated(stringsFromArg(t, a[0]), closers))
	case "_end_quote_closers":
		result = intKeyed(r.document(t, a[0]).endQuoteClosers(), func(s string) pyjson.Value { return s })
	case "_end_closers_for":
		result = r.document(t, a[0]).endClosersFor(argInt(t, a[1]))
	case "_mask_quoted_segments", "_mask_quoted_segments_sparse":
		result = maskQuotedSegments(argString(t, a[0]))
	case "_question_content_end":
		result = int64(questionContentEnd(pyunicode.Runes(argString(t, a[0]))))
	case "_last_context_sentence":
		result = lastContextSentence(argString(t, a[0]))
	case "_ends_with_question":
		result = endsWithQuestion(argString(t, a[0]))
	case "_prose_visible_text":
		result = r.document(t, a[0]).proseVisibleText(argInt(t, a[1]))
	case "_image_only_row":
		row := a[1].(map[string]any)
		result = r.document(t, a[0]).imageOnlyRow(&visibility.Line{Start: row["start"].(int), Raw: row["raw"].(string)}, argString(t, r.reg[string(a[2].(oracle.Ref))]))
	case "_plain_sentence_records":
		records := r.document(t, a[0]).plainSentenceRecords()
		out := make([]pyjson.Value, len(records))
		for i, rec := range records {
			out[i] = list(int64(rec.Line), rec.Visible, rec.Source, list(int64(rec.Group[0]), int64(rec.Group[1])))
		}
		result = out
	case "_rule9_context":
		result = intKeyed(r.document(t, a[0]).rule9Context(), func(ss []contextSentence) pyjson.Value {
			out := make([]pyjson.Value, len(ss))
			for i, s := range ss {
				out[i] = list(s.Sentence, s.Following)
			}
			return out
		})
	case "_say_hedge_sentence":
		result = sayHedgeSentence(argString(t, a[0]))
	case "_copy_comma_noun_sentence":
		result = copyCommaNounSentence(argString(t, a[0]))
	case "_bare_verb_chain_sentence":
		result = bareVerbChainSentence(argString(t, a[0]))
	case "_v220_scan_text":
		result = r.document(t, a[0]).v220ScanText(argInt(t, a[1]))
	default:
		t.Fatalf("Go に対応を持たない関数 %s", c.Function)
	}
	if c.Save != "" {
		r.reg[c.Save] = result
	}
	return result
}

// intKeyed は整数の鍵の dict を、鍵を整数の大小で並べた Object にする (本家の sort_keys=True が整数の鍵に対してする並べ方)。
func intKeyed[V any](m map[int]V, value func(V) pyjson.Value) pyjson.Value {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	o := pyjson.NewObject()
	for _, k := range keys {
		o.Set(strconv.Itoa(k), value(m[k]))
	}
	return o
}

func digestOf(t *testing.T, v pyjson.Value) (canon, digest string) {
	t.Helper()
	rec := pyjson.NewObject()
	rec.Set("result", v)
	s, err := pyjson.Dumps(rec, canonical)
	if err != nil {
		t.Fatalf("正規の JSON にできません: %v", err)
	}
	sum := sha256.Sum256([]byte(s))
	return s, hex.EncodeToString(sum[:])[:oracle.DigestWidth]
}

func loadGolden(t *testing.T) (oracle.LintBaseGolden, []oracle.VisCase) {
	t.Helper()
	var g oracle.LintBaseGolden
	if err := oracle.ReadGolden(goldenDir, oracle.LintBaseFile, &g); err != nil {
		t.Fatal(err)
	}
	corpus, err := oracle.LoadCorpus(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	cases, err := oracle.LintBaseCases(corpus)
	if err != nil {
		t.Fatal(err)
	}
	return g, cases
}

func TestLintBaseMatchesPythonRecording(t *testing.T) {
	g, cases := loadGolden(t)
	if len(cases) != len(g.Cases) {
		t.Fatalf("case %d 件に対して記録は %d 件。make golden で作り直す", len(cases), len(g.Cases))
	}
	calls := map[string]int{}
	failed := 0
	for i, c := range cases {
		rec := g.Cases[i]
		if rec.Name != c.Name || rec.Calls != len(c.Calls) || len(rec.Digests) != rec.Calls*oracle.DigestWidth {
			t.Fatalf("case %d が記録と違う (%s %d 件 / 記録 %s %d 件)。make golden で作り直す", i, c.Name, len(c.Calls), rec.Name, rec.Calls)
		}
		kept := map[int]oracle.VisKept{}
		for _, k := range rec.Kept {
			kept[k.Index] = k
		}
		r := &replayer{reg: map[string]any{}}
		for j, call := range c.Calls {
			v := r.invoke(t, call)
			var canon, digest string
			calls[call.Function]++
			if call.Quiet {
				digest = "00000000"
			} else {
				canon, digest = digestOf(t, v)
			}
			if want := rec.Digests[j*oracle.DigestWidth : (j+1)*oracle.DigestWidth]; digest != want {
				failed++
				if failed <= 15 {
					msg := fmt.Sprintf("case %s の %d 番目 (%s): digest %s、記録 %s\n  Go: %s", c.Name, j, call.Function, digest, want, clip(canon, 400))
					if k, ok := kept[j]; ok {
						msg += "\n  記録: " + clip(string(k.Result), 400)
					}
					t.Error(msg)
				}
			}
		}
	}
	if failed > 15 {
		t.Errorf("ほかにも %d 件の食い違いがあります", failed-15)
	}
	for _, f := range g.Functions {
		if calls[f.Name] != f.Calls {
			t.Errorf("%s の呼び出し数が記録と違う: Go %d、記録 %d", f.Name, calls[f.Name], f.Calls)
		}
	}
}

// clip は食い違いの表示用に、長い文字列を n 字までにする。
func clip(s string, n int) string {
	return fmt.Sprintf("%.*s", n, s) + strings.Repeat("…", min(1, max(0, len(s)-n)))
}

// 空振りを止める: 土台の関数と定数ごとに記録の件数が 0 でなく、記録に一覧にない名前が無い。
func TestLintBaseRecordingCoversEveryFunction(t *testing.T) {
	g, _ := loadGolden(t)
	byName := map[string]oracle.VisFunctionRecord{}
	for _, f := range g.Functions {
		byName[f.Name] = f
	}
	want := append(slices.Clone(oracle.LintBaseFunctions), oracle.LintBaseConstants...)
	for _, name := range want {
		if rec, ok := byName[name]; !ok || rec.Calls == 0 {
			t.Errorf("%s の記録が 0 件 (空振り)", name)
		}
	}
	for _, f := range g.Functions {
		if f.Name != "analyze_markdown" && !slices.Contains(want, f.Name) {
			t.Errorf("記録に、一覧 (oracle.LintBaseFunctions と LintBaseConstants) にない名前 %s がある", f.Name)
		}
		if f.Raised != 0 {
			t.Errorf("%s が例外になる呼び出しを %d 件持つ (土台の記録は例外を使わない。analysis を作れない入力は oracle の lintAnalysisFails で外す)", f.Name, f.Raised)
		}
	}
	for _, name := range oracle.LintBaseConstants {
		if _, ok := constantValue(name); !ok {
			t.Errorf("定数 %s を Go に持っていない", name)
		}
	}
}
