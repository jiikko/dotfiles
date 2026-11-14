package diff

import (
	"strings"

	"ja-writing/internal/difflib"
	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// 群 P の問い (本家の _is_question・_only_final_period_removed・_question_risks) と、文の種類の印・許可・能力・選ぶ範囲と因果の変化を意味が変わる恐れの形にする
// 変換 (_find_line・_marks_risks・_modality_risks・_logic_risks)。判定そのものは群 F・H・I が持つ。

var (
	questionEnd = pyre.NewLazy(`(?:[？?]|か[。？?])[」』）)]*$`, 0)
	// 「か。」で終わっても問いでない語 (形容動詞の語幹と「ほか」。原因は明らか。→ 原因は明らかだ)
	notQuestionKa = pyre.NewLazy(`(?:明ら|確|わず|僅|静|豊|ほ|速や|穏や|鮮や|細や|爽や|健や|和や|緩や|賑や|にぎや|華や|`+
		`密|愚|疎|おろ|遥|はる|ほの|柔ら|滑ら|なめら|清ら|朗ら|安ら|大ま)か。[」』）)]*$`, 0)
	// 台詞の最後の「。」(閉じ括弧の前)。台詞・脚本の推敲は、この「。」だけを外す
	finalPeriodInQuote = pyre.NewLazy(`。(?=[」』]+$)`, 0)
	sentenceEnd        = pyre.NewLazy(`[。！？!?]+[」』）)]*`, 0)
)

func isQuestion(text string) bool {
	text = pystr.RStrip(text)
	return questionEnd.MustSearch(text) && !notQuestionKa.MustSearch(text)
}

// onlyFinalPeriodRemoved は片方の文から台詞の最後の「。」を外すと、もう片方と同じになるか (「見たのか。」→「見たのか」)。
func onlyFinalPeriodRemoved(old, new string) bool {
	old, new = pystr.RStrip(old), pystr.RStrip(new)
	return finalPeriodInQuote.MustSub("", old) == new || finalPeriodInQuote.MustSub("", new) == old
}

// questionRisks は問い (kind sentence_type、subkind question)。組になった文の片方だけが問いの文 (文末が ？、?、か。、か？) なら出す。句点のない「…か」で終わる文は問いに数えない。
func (s *sides) questionRisks(groups []*sentenceGroup) []riskItem {
	var out []riskItem
	order := 0
	for _, group := range groups {
		for _, p := range group.pairs {
			old, new := group.old[p[0]], group.new[p[1]]
			oldQuestion, newQuestion := isQuestion(old.text), isQuestion(new.text)
			// 台詞の最後の「。」だけを外した組は、問いのまま読めるので出さない
			if oldQuestion != newQuestion && !onlyFinalPeriodRemoved(old.text, new.text) {
				d := pyjson.NewObject()
				d.Set("old_is_question", oldQuestion)
				d.Set("new_is_question", newQuestion)
				out = append(out, riskItem{groupIndex(group, &new, 1), order, newRisk("sentence_type", "question", old.line, new.line, old.text, new.text, true, true, d)})
				order++
			}
		}
	}
	return out
}

// QuestionRisks は元の文と書き直した文の、問いの変化の恐れ (hunk の番号, 塊の中の順, risk の並び。JSON の値の形)。
func QuestionRisks(orig, rw string) (pyjson.Value, error) {
	full, s, err := buildFullDiff(orig, rw, nil)
	if err != nil {
		return nil, err
	}
	return riskItemsValue(s.questionRisks(s.sentencePairs(full.hunks))), nil
}

func (s *sides) normalizedLines(side int) []string {
	if s.normalized[side] == nil {
		lines := make([]string, len(s.lines[side]))
		for i, line := range s.lines[side] {
			lines[i] = Normalize(line)
		}
		s.normalized[side] = lines
	}
	return s.normalized[side]
}

// findLine は 9 キーの分析 (正規化した文) の文が、生の文の何行目にあるか (見つからなければ 0)。最初の文の先頭 (20 字まで) を、行ごとに正規化した文から探す。
func (s *sides) findLine(side int, text string) int {
	head, _, _ := strings.Cut(text, "\n")
	if m := mustSearchAt(sentenceEnd, pyre.NewText(head), 0, pyre.NoEnd); m != nil {
		head = runeSlice(pyunicode.Runes(head), 0, m.End(0))
	}
	head = runeSlice(pyunicode.Runes(pystr.Strip(head)), 0, 20)
	if head == "" {
		return 0
	}
	for i, line := range s.normalizedLines(side) {
		if strings.Contains(line, head) {
			return i + 1
		}
	}
	return 0
}

func kindHitsList(list []KindHits) pyjson.Value {
	return pyjson.MapList(list, func(e KindHits) pyjson.Value {
		o := pyjson.NewObject()
		o.Set("kind", e.Kind)
		o.Set("hits", pyjson.Strings(e.Hits))
		return o
	})
}

// marksRisks は文の種類の印 (kind sentence_type、subkind marks)。type_changes の各項目をそのまま数える (判定は変えない)。
func (s *sides) marksRisks(changes []TypeChange) []*pyjson.Object {
	out := make([]*pyjson.Object, len(changes))
	for i, c := range changes {
		d := pyjson.NewObject()
		d.Set("lost", kindHitsList(c.Lost))
		d.Set("added", kindHitsList(c.Added))
		out[i] = newRisk("sentence_type", "marks", s.findLine(0, c.Orig), s.findLine(1, c.Rewrite), c.Orig, c.Rewrite, true, true, d)
	}
	return out
}

// modalityRisks は許可・能力の言い方の変化。許可が消えた・増えたものは kind modality (数える)。能力が消えたものは、どの文書の種類でも modality_review (数えない)。
func (s *sides) modalityRisks(changes []ModalityChange) []*pyjson.Object {
	out := make([]*pyjson.Object, len(changes))
	for i, c := range changes {
		kind := "modality"
		if c.Subkind == "ability_lost" {
			kind = "modality_review"
		}
		d := pyjson.NewObject()
		d.Set("lost", kindHitsList(c.Lost))
		d.Set("added", kindHitsList(c.Added))
		d.Set("from", c.From)
		d.Set("to", c.To)
		out[i] = newRisk(kind, c.Subkind, s.findLine(0, c.Orig), s.findLine(1, c.Rewrite), c.Orig, c.Rewrite, true, true, d)
	}
	return out
}

// logicRisks は選ぶ範囲と因果の言い方の変化 (kind は choice か causality。どちらも数える)。
func (s *sides) logicRisks(changes []LogicChange) []*pyjson.Object {
	out := make([]*pyjson.Object, len(changes))
	for i, c := range changes {
		d := pyjson.NewObject()
		d.Set("from", c.From)
		d.Set("to", c.To)
		d.Set("old_hits", pyjson.Strings(c.OldHits))
		d.Set("new_hits", pyjson.Strings(c.NewHits))
		out[i] = newRisk(c.Kind, c.Subkind, s.findLine(0, c.Orig), s.findLine(1, c.Rewrite), c.Orig, c.Rewrite, true, true, d)
	}
	return out
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

func riskObjectsValue(list []*pyjson.Object) pyjson.Value {
	return pyjson.MapList(list, func(o *pyjson.Object) pyjson.Value { return o })
}
