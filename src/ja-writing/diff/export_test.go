package diff

import (
	"ja-writing/internal/difflib"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
	"slices"
)

// AddedActors は元の文書と書き直した文書の、原文と資料にない人や担当を足した文 (JSON の added_actors の値)。materials は --material の資料。
func AddedActors(orig, rw string, materials []string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return s.addedActors(s.sentencePairs(full.hunks), materials), nil
}

// typeChangesOf は正規化した 2 つの文の、文の種類の印の変化 (本家の diff() が _type_changes に渡す opcodes を作ってから比べる)。
// cache は許可・能力と選ぶ範囲・因果の比べが、作った範囲を使い回すための入れ物。
func typeChangesOf(o, r string, cache *typeCache) []TypeChange {
	var opcodes []difflib.OpCode
	if o != r {
		opcodes = difflib.NewExact(pyunicode.Runes(o), pyunicode.Runes(r)).GetOpcodes()
	}
	return TypeChanges(o, r, opcodes, cache)
}

// AdditionRisks は元の文と書き直した文の、状態の消え・約束・因果の足しの候補 (risk の並び。JSON の値の形)。docType は文書の種類 (空なら指定なし)。
// 既存の候補として、本家の _meaning_risks が渡す種類のうちこの検出が読む 3 つ (文の種類の印・確度・否定) を渡す。
func AdditionRisks(orig, rw, docType string) (pyjson.Value, error) {
	docType, err := entryDocType(docType)
	if err != nil {
		return nil, err
	}
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	groups := s.sentencePairs(full.hunks)
	changes := typeChangesOf(Normalize(orig), Normalize(rw), &typeCache{})
	marks := s.marksReviewsOf(changes)
	risks := slices.Concat(s.marksRisks(changes), s.certaintyRisks(groups, marks, nil, docType))
	for _, item := range s.negationRisks(groups) {
		risks = append(risks, item.risk)
	}
	return riskObjectsValue(s.additionRisks(groups, risks, &[2]*masker{s.newMasker(0), s.newMasker(1)})), nil
}

// CertaintyRisks は元の文と書き直した文の、確度の候補 (risk の並び。JSON の値の形)。docType は文書の種類 (空なら指定なし)。
func CertaintyRisks(orig, rw, docType string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	o, r := Normalize(orig), Normalize(rw)
	var opcodes []difflib.OpCode
	if o != r {
		opcodes = difflib.NewExact(pyunicode.Runes(o), pyunicode.Runes(r)).GetOpcodes()
	}
	marks := s.marksReviewsOf(TypeChanges(o, r, opcodes, &typeCache{}))
	return riskObjectsValue(s.certaintyRisks(s.sentencePairs(full.hunks), marks, nil, docType)), nil
}

// ContrastRisks は元の文と書き直した文の、否定対比の主張の逆転の恐れ (hunk の番号, 塊の中の順, risk の並び。JSON の値の形)。
func ContrastRisks(orig, rw string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return riskItemsValue(s.contrastRisks(s.sentencePairs(full.hunks))), nil
}

// EndingChanges は本家の ending_changes: 書き直しで文末の種類が変わった文。似ている元の文と組にして比べる。
func EndingChanges(o, r string) []EndingChange {
	return endingChangesFromUnits(EndingUnits(o, false), EndingUnits(r, false))
}

// FeelingRisks は元の文と書き直した文の、気持ちの語を足した恐れ (hunk の番号, 塊の中の順, risk の並び。JSON の値の形)。
func FeelingRisks(orig, rw string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return riskItemsValue(s.feelingRisks(s.sentencePairs(full.hunks))), nil
}

// FullDiff は元の文と書き直した文の全文差分 (JSON の full_diff)。渡された文字列をそのまま比べる (BOM を外したり、改行をそろえたりしない)。
func FullDiff(orig, rw string, notes []EncodingNote) (pyjson.Value, error) {
	full, _, err := buildFullDiff(orig, rw, notes)
	if err != nil {
		return nil, err
	}
	return full.value(), nil
}

// HeadingStructure は元の文と書き直した文の、見出しの変化の見直し候補の文の並び (本家の _heading_structure)。withShapes なら、見出しの形の変化 (JSON の heading_shapes) も返す。
func HeadingStructure(orig, rw string, withShapes bool) ([]string, []HeadingShape, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, nil, err
	}
	if !withShapes {
		return headingStructure(s, full.hunks, nil), nil, nil
	}
	shapes := []HeadingShape{}
	out := headingStructure(s, full.hunks, &shapes)
	return out, shapes, nil
}

// HedgesSection は元の文と書き直した文の、慎重な言い方の節 (JSON の値の形)。docType は文書の種類 (空なら指定なし。確度の恐れを数える種類が変わる)。
// 既存の候補は、本家の diff() が渡す形 (意味が変わる恐れの出力と、そこから外した文の種類の印の恐れ) で読む。
func HedgesSection(orig, rw, docType string) (pyjson.Value, error) {
	result, s, groups, err := meaningRisksOf(orig, rw, true, docType)
	if err != nil {
		return nil, err
	}
	return s.hedges(groups, slices.Concat(result.risks, result.quiet)), nil
}

// ImmutableValues は元の文と書き直した文の、動かしてはいけない値の節と、意味が変わる恐れに足す引用などの候補 (JSON の値の形。[節, 候補の並び])。
// 数値の恐れは、既存の数値の候補と重ねないために読む。
func ImmutableValues(orig, rw string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	groups := s.sentencePairs(full.hunks)
	var risks []*pyjson.Object
	for _, item := range s.numberRisks(full, groups) {
		risks = append(risks, item.risk)
	}
	section, citations := s.immutableValues(risks, groups)
	return []pyjson.Value{section, riskObjectsValue(citations)}, nil
}

// meaningInputs は meaning_risks と diff() が _meaning_risks に渡す値を、正規化した 2 つの文から作る (許可・能力と選ぶ範囲・因果は、文の種類の印が作った範囲を使い回す)。
// withChanges が偽なら type_changes は None (文の種類の印も許可・能力も選ぶ範囲・因果も足さない)。
func meaningInputs(orig, rw string, withChanges bool) (changes []TypeChange, modality []ModalityChange, logic []LogicChange) {
	if !withChanges {
		return nil, nil, nil
	}
	o, r := Normalize(orig), Normalize(rw)
	cache := &typeCache{}
	changes = typeChangesOf(o, r, cache)
	return changes, ModalityChanges(o, r, cache), LogicChanges(o, r, cache)
}

// entryDocType は公開の入口に渡された文書の種類を本家の normalize_doc_type に通す (大文字・前後の空白は寄せ、使えない値は ValueError)。
// 空は指定なし。入口の型が string なので、本家で ValueError になる空文字列は区別できない (diff() の配線は *string の NormalizeDocType を通す)。
func entryDocType(docType string) (string, error) {
	if docType == "" {
		return "", nil
	}
	dt, err := visibility.NormalizeDocType(&docType)
	if err != nil {
		return "", err
	}
	return *dt, nil
}

// meaningRisksOf は本家の meaning_risks (JSON の meaning_risks)。withChanges が偽なら type_changes は None。
func meaningRisksOf(orig, rw string, withChanges bool, docType string) (meaningResult, *sides, []*sentenceGroup, error) {
	docType, err := entryDocType(docType)
	if err != nil {
		return meaningResult{}, nil, nil, err
	}
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return meaningResult{}, nil, nil, err
	}
	groups := s.sentencePairs(full.hunks)
	changes, modality, logic := meaningInputs(orig, rw, withChanges)
	return s.meaningRisks(full, groups, changes, docType, modality, logic), s, groups, nil
}

// MeaningRisks は元の文と書き直した文の、意味が変わる恐れ (risk の並び。JSON の値の形。本家の meaning_risks)。
// withTypeChanges が偽なら文の種類の印と、許可・能力・選ぶ範囲・因果の言い方の候補を含めない。docType は文書の種類 (空なら指定なし)。
func MeaningRisks(orig, rw string, withTypeChanges bool, docType string) (pyjson.Value, error) {
	result, _, _, err := meaningRisksOf(orig, rw, withTypeChanges, docType)
	if err != nil {
		return nil, err
	}
	return riskObjectsValue(result.risks), nil
}

// NegationRisks は元の文と書き直した文の、否定の恐れ (hunk の番号, 塊の中の順, risk の並び。JSON の値の形)。
func NegationRisks(orig, rw string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return riskItemsValue(s.negationRisks(s.sentencePairs(full.hunks))), nil
}

func (r riskItem) value() pyjson.Value {
	return []pyjson.Value{int64(r.index), int64(r.order), r.risk}
}

func riskItemsValue(items []riskItem) pyjson.Value { return pyjson.MapList(items, riskItem.value) }

// NumberRisks は元の文と書き直した文の、数値の恐れ (hunk の番号, 塊の中の順, risk の並び。JSON の値の形)。
func NumberRisks(orig, rw string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return riskItemsValue(s.numberRisks(full, s.sentencePairs(full.hunks))), nil
}

// PolitenessSection は元の文と書き直した文の、敬語の度合いの節 (JSON の値の形)。docType は文書の種類 (空なら指定なし。mail だけ比べる)。
func PolitenessSection(orig, rw, docType string) (pyjson.Value, error) {
	s, err := newSides(orig, rw)
	if err != nil {
		return nil, err
	}
	return s.politeness(docType), nil
}

// ProtectedRisks は元の文と書き直した文の、変えない箇所の変化の恐れ (hunk の番号, 塊の中の順, risk の並び。JSON の値の形)。
func ProtectedRisks(orig, rw string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return riskItemsValue(s.protectedRisks(full)), nil
}

// QualifierRisks は元の文と書き直した文の、概数と範囲を表す語の変化の恐れ (risk の並び。JSON の値の形)。
func QualifierRisks(orig, rw string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return s.qualifierRisks(s.sentencePairs(full.hunks)), nil
}

// QuestionRisks は元の文と書き直した文の、問いの変化の恐れ (hunk の番号, 塊の中の順, risk の並び。JSON の値の形)。
func QuestionRisks(orig, rw string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return riskItemsValue(s.questionRisks(s.sentencePairs(full.hunks))), nil
}

// ChangeRisks は文の種類の印・許可・能力・選ぶ範囲と因果の変化 (本家の diff() が _meaning_risks に渡す type_changes と、meaning_risks が作る modality・logic) を
// 意味が変わる恐れの形にした risk の並び (JSON の値の形)。範囲づくりは 1 回で、3 つが使い回す。
type ChangeRisks struct{ Marks, Modality, Logic pyjson.Value }

func ChangeRisksOf(orig, rw string) (*ChangeRisks, error) {
	s, err := newSides(orig, rw)
	if err != nil {
		return nil, err
	}
	o, r := Normalize(orig), Normalize(rw)
	var opcodes []difflib.OpCode
	if o != r {
		opcodes = difflib.NewExact(pyunicode.Runes(o), pyunicode.Runes(r)).GetOpcodes()
	}
	cache := &typeCache{}
	marks := s.marksRisks(TypeChanges(o, r, opcodes, cache))
	modality := s.modalityRisks(ModalityChanges(o, r, cache))
	logic := s.logicRisks(LogicChanges(o, r, cache))
	return &ChangeRisks{riskObjectsValue(marks), riskObjectsValue(modality), riskObjectsValue(logic)}, nil
}

// QuoteRisks は元の文と書き直した文の、かぎかっこの中の言葉を書き換えた恐れ (risk の並び。JSON の値の形)。
func QuoteRisks(orig, rw string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return riskObjectsValue(s.quoteRisks(s.sentencePairs(full.hunks), nil)), nil
}

// RegisterChanges は元の文と書き直した文の、文体の切り替えの節 (JSON の値の形)。docType は文書の種類 (空なら指定なし。敬体の割合の候補が出ると切り替えは数えない)。
func RegisterChanges(orig, rw, docType string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return s.registerChanges(s.sentencePairs(full.hunks), s.politeness(docType)), nil
}

func (s sentence) value() pyjson.Value {
	return []pyjson.Value{s.text, int64(s.line), int64(s.start)}
}

func sentencesValue(list []sentence) pyjson.Value { return pyjson.MapList(list, sentence.value) }

func (g *sentenceGroup) value() pyjson.Value {
	pairs := make([]pyjson.Value, len(g.pairs))
	for i, p := range g.pairs {
		pairs[i] = []pyjson.Value{int64(p[0]), int64(p[1])}
	}
	o := pyjson.NewObject()
	o.Set("hunk", g.hunk.value())
	o.Set("old", sentencesValue(g.old))
	o.Set("new", sentencesValue(g.new))
	o.Set("pairs", pairs)
	if g.run != nil {
		run := make([]pyjson.Value, len(g.run))
		for i, h := range g.run {
			run[i] = h.value()
		}
		o.Set("hunks", run)
	}
	return o
}

// SentencePairs は元の文と書き直した文の、意味が変わる恐れの文の組 (hunk ごと。JSON の値の形)。
func SentencePairs(orig, rw string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	groups := s.sentencePairs(full.hunks)
	out := make([]pyjson.Value, len(groups))
	for i, g := range groups {
		out[i] = g.value()
	}
	return out, nil
}

func (b body) value() pyjson.Value {
	return []pyjson.Value{b.text, b.raw, int64(b.line), int64(b.start)}
}

// AllBodies は 1 つの側 (0 が元、1 が後) の数える文 (JSON の値の形)。
func AllBodies(orig, rw string, side int) (pyjson.Value, error) {
	s, err := newSides(orig, rw)
	if err != nil {
		return nil, err
	}
	bodies := s.allBodies(side)
	out := make([]pyjson.Value, len(bodies))
	for i, b := range bodies {
		out[i] = b.value()
	}
	return out, nil
}

// StiffnessMetrics は本家の stiffness_metrics (1 つの文書の値。コード、URL、表、見出しは数えない)。
func StiffnessMetrics(text string) (pyjson.Value, error) {
	s, err := newSides(text, text)
	if err != nil {
		return nil, err
	}
	return metricsOf(s.allBodies(0), map[string]stiffValues{}).value(), nil
}

// Stiffness は本家の stiffness (文体の硬化の検査。docType は文書の種類。incident・formal・paper なら参考)。
func Stiffness(orig, rw string, docType *string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	groups := s.sentencePairs(full.hunks)
	dt, err := visibility.NormalizeDocType(docType)
	if err != nil {
		return nil, err
	}
	return s.stiffness(full, groups, dt), nil
}

// SwapRisks は元の文と書き直した文の、主語と目的語の入れ替えの恐れ (hunk の番号, 塊の中の順, risk の並び。JSON の値の形)。
func SwapRisks(orig, rw string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return riskItemsValue(s.swapRisks(s.sentencePairs(full.hunks))), nil
}

// Tone は本家の tone (勢いの検査。JSON の tone と同じ形)。docType は文書の種類 (pr・article・incident・manual・formal・paper なら参考。
// mail と promo は絵文字と見出しも見直し候補、minutes は見出しを見直し候補にする)。
func Tone(orig, rw string, docType *string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	dt, err := visibility.NormalizeDocType(docType)
	if err != nil {
		return nil, err
	}
	return s.tone(full, dt, nil)
}

// TypeMarks は本家の type_marks: 文や範囲に含まれる文の種類の印 (同じ印は 1 回だけ)。
func TypeMarks(sentence string) []KindHits {
	return marksIn(markPositions(sentence), [][2]int{{0, pystr.Len(sentence)}}, typeKindNames, nil)
}
