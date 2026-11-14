package diff

import (
	"slices"
	"strings"
	"testing"

	"ja-writing/internal/difflib"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pyunicode"
	"ja-writing/oracle"
	"ja-writing/oracle/replay"
)

// 群 X・W の段の記録の突き合わせ。

var reviewHits = map[string]int{}

// reviewBodies は覚えた _Body (名前 → 文)。_citation_number_covered の手書きの呼び出しが参照で渡す。
var reviewBodies = map[string]body{}

// reviewContext は組 1 つの _build_full_diff・_sentence_pairs と、段が読む中間の値 (本家の段の呼び出しが覚える値を Go で作り直したもの)。
type reviewContext struct {
	old, new   string
	full       *fullDiff
	s          *sides
	groups     []*sentenceGroup
	marks      []marksReview
	changes    []TypeChange
	markRisks  []*pyjson.Object
	negation   []*pyjson.Object
	cert       map[string][]*pyjson.Object
	nrisks     []*pyjson.Object
	tokens     map[string][]valueToken
	politeness map[string]*pyjson.Object
}

var reviewCache *reviewContext

func reviewContextOf(t *testing.T, build []any) *reviewContext {
	t.Helper()
	orig, rw := argString(t, build[0]), argString(t, build[1])
	if c := reviewCache; c != nil && c.old == orig && c.new == rw {
		return c
	}
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		t.Fatalf("buildFullDiff: %v", err)
	}
	groups := s.sentencePairs(full.hunks)
	o, r := Normalize(orig), Normalize(rw)
	var opcodes []difflib.OpCode
	if o != r {
		opcodes = difflib.NewExact(pyunicode.Runes(o), pyunicode.Runes(r)).GetOpcodes()
	}
	changes := TypeChanges(o, r, opcodes, &typeCache{})
	c := &reviewContext{old: orig, new: rw, full: full, s: s, groups: groups, changes: changes, marks: s.marksReviewsOf(changes),
		cert: map[string][]*pyjson.Object{}, tokens: map[string][]valueToken{}, politeness: map[string]*pyjson.Object{}}
	reviewCache = c
	return c
}

func (c *reviewContext) certainty(docType string) []*pyjson.Object {
	if _, ok := c.cert[docType]; !ok {
		c.cert[docType] = c.s.certaintyRisks(c.groups, c.marks, nil, docType)
	}
	return c.cert[docType]
}

// marksRiskObjects は文の種類の印の恐れ (本家の _marks_risks の戻り値)、negationRisks は否定の恐れ (本家の _negation_risks の risk)。
func (c *reviewContext) marksRiskObjects() []*pyjson.Object {
	if c.markRisks == nil {
		c.markRisks = append([]*pyjson.Object{}, c.s.marksRisks(c.changes)...)
	}
	return c.markRisks
}

func (c *reviewContext) negationRisks() []*pyjson.Object {
	if c.negation == nil {
		c.negation = []*pyjson.Object{}
		for _, item := range c.s.negationRisks(c.groups) {
			c.negation = append(c.negation, item.risk)
		}
	}
	return c.negation
}

func (c *reviewContext) numberRisks() []*pyjson.Object {
	if c.nrisks == nil {
		for _, item := range c.s.numberRisks(c.full, c.groups) {
			c.nrisks = append(c.nrisks, item.risk)
		}
		if c.nrisks == nil {
			c.nrisks = []*pyjson.Object{}
		}
	}
	return c.nrisks
}

// hedgeInputs は _hedges に渡す risks の引数 ($concat か 1 つの参照) を、Go の段の入力 (印の恐れと確度の恐れ) にする。
func hedgeInputs(t *testing.T, c *reviewContext, arg any) ([]marksReview, []*pyjson.Object) {
	t.Helper()
	refs := []any{arg}
	if m, ok := arg.(map[string]any); ok && m["$concat"] != nil {
		refs = m["$concat"].([]any)
	}
	var marks []marksReview
	var certainty []*pyjson.Object
	for _, ref := range refs {
		switch name := refName(t, ref); name {
		case "marks":
			marks = append(marks, c.marks...)
		case "cert":
			certainty = append(certainty, c.certainty("")...)
		case "certA":
			certainty = append(certainty, c.certainty("article")...)
		default:
			t.Fatalf("知らない参照 %s", name)
		}
	}
	return marks, certainty
}

func valueKindOf(t *testing.T, ref string) (string, int) {
	t.Helper()
	for i, k := range oracle.DiffReviewValueKinds {
		if ref == "re_"+k[0] {
			return k[0], i
		}
	}
	t.Fatalf("知らない正規表現の参照 %s", ref)
	return "", 0
}

func tokenValue(tokens []valueToken) pyjson.Value {
	return replay.MapList(tokens, func(v valueToken) pyjson.Value { return replay.List(v.key, v.body.value()) })
}

// objectOf は JSON の値 (map・[]any) を pyjson の値にする (手書きの risk を渡す用)。
func objectOf(v any) pyjson.Value {
	switch x := v.(type) {
	case map[string]any:
		o := pyjson.NewObject()
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			o.Set(k, objectOf(x[k]))
		}
		return o
	case []any:
		out := make([]pyjson.Value, len(x))
		for i, e := range x {
			out[i] = objectOf(e)
		}
		return out
	case int:
		return int64(x)
	}
	return v
}

func reviewConstantValue(t *testing.T, name string) pyjson.Value {
	t.Helper()
	switch name {
	case "_CITATION":
		return lazyValue(citationRe)
	case "_SCIENTIFIC_VALUE":
		return lazyValue(scientificRe)
	case "_SIGNED_VALUE":
		return lazyValue(signedRe)
	case "_STATISTIC_VALUE":
		return lazyValue(statisticRe)
	case "_EMAIL":
		return lazyValue(emailRe)
	case "_ADDRESSEE":
		// 先頭の (?m) は、本家の flags では MULTILINE (8) に数えられる
		return replay.Pattern(pyUnicodeFlag|pyre.MULTILINE, addresseeRe.Pattern())
	case "_ADDRESSEE_FAMILY":
		return replay.Strings(addresseeFamily)
	case "_VALUE_SKIP_KINDS":
		return sortedSet(valueSkipKinds)
	case "POLITENESS_THRESHOLD":
		return politenessThreshold
	case "_MAIL_HONORIFIC":
		return lazyValue(mailHonorific)
	case "_MAIL_HONORIFIC_NAMES":
		o := pyjson.NewObject()
		for _, n := range honorificNames {
			o.Set(n[0], n[1])
		}
		return replay.SortKeys(o)
	case "_POLITE_END":
		return lazyValue(politeEnd)
	case "_PLAIN_END":
		return lazyValue(plainEnd)
	case "_DIALOGUE":
		return lazyValue(dialogueRe)
	case "_QUOTE_CLOSE":
		o := pyjson.NewObject()
		for _, k := range []rune{'「', '『'} {
			o.Set(string(k), string(quoteOpen[k]))
		}
		return replay.SortKeys(o)
	case "_QUOTE_DROP":
		return lazyValue(quoteDrop)
	case "_CITATION_LATIN":
		return citationLatin
	case "_CITATION_JA":
		return citationJA
	}
	t.Fatalf("定数 %s を Go に持っていない", name)
	return nil
}

func countCandidates(prefix string, section *pyjson.Object) {
	list, _ := section.Get("candidates")
	items := list.([]pyjson.Value)
	for _, item := range items {
		sub, _ := item.(*pyjson.Object).Get("subkind")
		reviewHits[prefix+sub.(string)]++
	}
	if len(items) == 0 {
		reviewHits[prefix+"(none)"]++
	}
}

// invokeReview は群 X・W の呼び出し 1 つの結果。build は、直前の _build_full_diff の呼び出しの引数。
func invokeReview(t *testing.T, c oracle.Call, build []any) pyjson.Value {
	t.Helper()
	a := c.Args
	switch c.Function {
	case "_hedges":
		ctx := reviewContextOf(t, build)
		marks, certainty := hedgeInputs(t, ctx, a[2])
		section := ctx.s.hedges(ctx.groups, marks, certainty)
		countCandidates("hedges:", section)
		return replay.SortKeys(section)
	case "_immutable_values":
		ctx := reviewContextOf(t, build)
		section, citations := ctx.s.immutableValues(ctx.numberRisks(), ctx.groups)
		countCandidates("value:", section)
		for _, r := range citations {
			kind, _ := r.Get("kind")
			sub, _ := r.Get("subkind")
			reviewHits["citations:"+kind.(string)+":"+sub.(string)]++
		}
		return replay.SortKeys(replay.List(section, riskObjectsValue(citations)))
	case "_value_tokens":
		ctx := reviewContextOf(t, build)
		kind, index := valueKindOf(t, refName(t, a[2]))
		tokens := ctx.s.valueTokens(argInt(t, a[1]), valueKinds[index].re)
		ctx.tokens[c.Save] = tokens
		reviewHits["tokens:"+kind]++
		if len(tokens) == 0 {
			reviewHits["tokens:"+kind+":(none)"]++
		}
		return tokenValue(tokens)
	case "_uncancelled_tokens":
		ctx := reviewContextOf(t, build)
		kept := uncancelledTokens(ctx.tokens[refName(t, a[0])], ctx.tokens[refName(t, a[1])], valuePlaces(ctx.groups), ctx.groups)
		if len(kept[0])+len(kept[1]) > 0 {
			reviewHits["uncancelled:left"]++
		} else {
			reviewHits["uncancelled:(none)"]++
		}
		return replay.List(tokenValue(kept[0]), tokenValue(kept[1]))
	case "_citation_key":
		return citationKey(argString(t, a[0]))
	case "_citation_number_covered":
		pick := func(arg any) *valueToken {
			if arg == nil {
				return nil
			}
			pair := arg.([]any)
			return &valueToken{argString(t, pair[0]), reviewBodies[refName(t, pair[1])]}
		}
		var risks []*pyjson.Object
		for _, r := range a[2].([]any) {
			risks = append(risks, objectOf(r).(*pyjson.Object))
		}
		got := citationNumberCovered(pick(a[0]), pick(a[1]), risks)
		reviewHits["covered:"+map[bool]string{true: "true", false: "false"}[got]]++
		return got
	case "_politeness_metrics":
		ctx := reviewContextOf(t, build)
		side := map[string]int{"b0": 0, "b1": 1}[refName(t, a[0])]
		return replay.SortKeys(measurePoliteness(ctx.s.allBodies(side)).value())
	case "_politeness":
		ctx := reviewContextOf(t, build)
		docType := ""
		if a[1] != nil {
			docType = argString(t, a[1])
		}
		section := ctx.s.politeness(docType)
		ctx.politeness[c.Save] = section
		if applicable, _ := section.Get("applicable"); applicable == false {
			reviewHits["politeness:not_applicable"]++
		} else {
			countCandidates("politeness:", section)
		}
		return replay.SortKeys(section)
	case "_quote_risks":
		ctx := reviewContextOf(t, build)
		risks := ctx.s.quoteRisks(ctx.groups, nil)
		for _, r := range risks {
			sub, _ := r.Get("subkind")
			reviewHits["quote:"+sub.(string)]++
			detail, _ := r.Get("detail")
			if d, _ := detail.(*pyjson.Object).Get("dialogue"); d == true {
				reviewHits["quote:dialogue"]++
			}
		}
		if len(risks) == 0 {
			reviewHits["quote:(none)"]++
		}
		return replay.SortKeys(riskObjectsValue(risks))
	case "_line_quotes":
		found, tops := lineQuotes(argString(t, a[0]))
		return replay.List(quoteFoundValue(found), spansValue(tops))
	case "_quote_normalize":
		return quoteNormalize(argString(t, a[0]))
	case "_blank_masked":
		ctx := reviewContextOf(t, build)
		side := map[string]int{"mask0": 0, "mask1": 1}[refName(t, a[0])]
		got := ctx.s.newMasker(side).blankMasked(argString(t, a[1]), argInt(t, a[2]))
		if strings.ContainsRune(got, 0) {
			reviewHits["blank_masked:masked"]++
		} else {
			reviewHits["blank_masked:same"]++
		}
		return got
	case "_register_of":
		return registerOf(argString(t, a[0]))
	case "_contained":
		return contained(argString(t, a[0]), argString(t, a[1]))
	case "_neighbor_pairs":
		var order [][2]int
		for _, p := range a[0].([]any) {
			pair := p.([]any)
			order = append(order, [2]int{argInt(t, pair[0]), argInt(t, pair[1])})
		}
		var firsts []int
		for _, f := range a[1].([]any) {
			firsts = append(firsts, argInt(t, f))
		}
		low, high := neighborPairs(order, firsts, argInt(t, a[2]), argInt(t, a[3]))
		return replay.List(int64(low), int64(high))
	case "_register_changes":
		ctx := reviewContextOf(t, build)
		name := refName(t, a[2])
		politeness := ctx.politeness[name]
		if politeness == nil {
			docType := map[string]string{"polNone": "", "polMail": "mail"}[name]
			politeness = ctx.s.politeness(docType)
		}
		section := ctx.s.registerChanges(ctx.groups, politeness)
		if skipped, _ := section.Get("skipped_by"); skipped != nil {
			reviewHits["register:skipped_by_politeness"]++
		} else {
			countCandidates("register:", section)
			list, _ := section.Get("candidates")
			for _, item := range list.([]pyjson.Value) {
				detail, _ := item.(*pyjson.Object).Get("detail")
				pattern, _ := detail.(*pyjson.Object).Get("pattern")
				reviewHits["register:"+pattern.(string)]++
			}
		}
		return replay.SortKeys(section)
	}
	t.Fatalf("関数 %s を Go に持っていない", c.Function)
	return nil
}

// checkReviewHits は群 X・W の記録が、候補が出る組と何も出ない組のどちらにも十分届いていることを確かめる。
func checkReviewHits(t *testing.T) {
	t.Helper()
	for _, want := range []string{
		"hedges:decreased", "hedges:(none)",
		"value:(none)", "value:addressee", "value:email", "citations:citation:replaced", "citations:citation:lost", "citations:citation:added",
		"citations:number:replaced", "citations:number:lost", "citations:number:added",
		"tokens:citation", "tokens:addressee", "tokens:email", "tokens:scientific_unit", "tokens:statistic", "tokens:number_sign", "uncancelled:left", "uncancelled:(none)",
		"politeness:not_applicable", "politeness:honorific_rate", "politeness:polite_ratio", "politeness:(none)",
		"quote:changed", "quote:lost", "quote:(none)", "quote:dialogue",
		"register:to_polite", "register:to_plain", "register:unified", "register:switched", "register:skipped_by_politeness", "register:(none)",
		"covered:true", "covered:false", "blank_masked:masked", "blank_masked:same",
	} {
		min := 20
		if strings.HasPrefix(want, "covered:") {
			min = 5 // 手書きの呼び出しだけ
		}
		if reviewHits[want] < min {
			t.Errorf("群 X・W の記録が %s に届いた回数は %d (20 回以上が要る)", want, reviewHits[want])
		}
	}
	t.Logf("群 X・W の記録の届き方: %v", reviewHits)
}

// 入口の関数 (HedgesSection ほか) は、記録の突き合わせが使う段の呼び出しと同じ答えを返す。
func TestReviewEntryPointsMatchStages(t *testing.T) {
	old := diffDocForTest("遅れると考えられる。[1]を見る。「はい」と言った。", "設定を開く。", "保存を押した。", "結果は3件だ。", "田中様、")
	new := diffDocForTest("遅れます。[2]を見る。「いいえ」と言った。", "設定を開きます。", "保存を押しました。", "結果は3件です。", "山田様、")
	build := []any{old, new}
	ctx := reviewContextOf(t, build)
	for _, docType := range []string{"", "mail"} {
		got, err := PolitenessSection(old, new, docType)
		if err != nil {
			t.Fatal(err)
		}
		if want := ctx.s.politeness(docType); !equalValues(t, got, want) {
			t.Errorf("PolitenessSection(%q) が段と違う", docType)
		}
		reg, err := RegisterChanges(old, new, docType)
		if err != nil {
			t.Fatal(err)
		}
		if want := ctx.s.registerChanges(ctx.groups, ctx.s.politeness(docType)); !equalValues(t, reg, want) {
			t.Errorf("RegisterChanges(%q) が段と違う", docType)
		}
	}
	hedges, err := HedgesSection(old, new, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := ctx.s.hedges(ctx.groups, ctx.marks, ctx.certainty("")); !equalValues(t, hedges, want) {
		t.Error("HedgesSection が段と違う")
	}
	quotes, err := QuoteRisks(old, new)
	if err != nil {
		t.Fatal(err)
	}
	if want := riskObjectsValue(ctx.s.quoteRisks(ctx.groups, nil)); !equalValues(t, quotes, want) {
		t.Error("QuoteRisks が段と違う")
	}
	imm, err := ImmutableValues(old, new)
	if err != nil {
		t.Fatal(err)
	}
	section, citations := ctx.s.immutableValues(ctx.numberRisks(), ctx.groups)
	if want := replay.List(section, riskObjectsValue(citations)); !equalValues(t, imm, want) {
		t.Error("ImmutableValues が段と違う")
	}
}

func diffDocForTest(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

func equalValues(t *testing.T, a, b pyjson.Value) bool {
	t.Helper()
	x, _, err := replay.ResultDigest(replay.SortKeys(a))
	if err != nil {
		t.Fatal(err)
	}
	y, _, err := replay.ResultDigest(replay.SortKeys(b))
	if err != nil {
		t.Fatal(err)
	}
	return x == y
}

func quoteFoundValue(found []quoteFound) pyjson.Value {
	out := make([]pyjson.Value, len(found))
	for i, f := range found {
		out[i] = replay.List(int64(f.start), f.bracket, f.in)
	}
	return out
}
