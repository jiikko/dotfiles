package lint

import (
	"slices"
	"strings"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/lint/pausecomma"
)

// G16 AI の言い回し (ai_phrase。本家 yomiyasu_lint.py の 4921-5005)。表は g16_tables.go。
//
// 文書全体に手掛かり (aiPhraseHead) を元の文字で当て、当たりの始まりの行だけを見える文字で調べる (速さのための絞り込みだが、元の文字に手掛かりが無い行は調べない形をそのまま写す)。

const aiPhraseMessageHead = "AIの文章によく出る言い回し（当たった言い方: "

// v220Sentence は本家の _V220_SENTENCE (新しい語の規則が文に分ける)。
var v220Sentence = newLazy(`[^。．！？!?|｜]+[。．！？!?]*`, 0)

// g16AiPhraseFindings は後から足す検出の 3 番目 (本家の _v250_ai_phrase_findings。種類で型が変わる: script・essay・slides・minutes・promo)。
func (s *lintState) g16AiPhraseFindings() {
	if len(s.an.Lines) == 0 {
		return
	}
	docType := s.docTypeName()
	for _, lineNo := range s.candidateLines(aiPhraseHead.Get()) {
		row := s.an.Lines[lineNo-1]
		if (row.Kind != "paragraph" && row.Kind != "list" && row.Kind != "heading" && row.Kind != "table") || row.QuoteDepth != 0 {
			continue
		}
		var words []string
		for _, part := range mustFindall0(v220Sentence, s.doc.v220ScanText(lineNo)) {
			if sentence := pystr.Strip(part); sentence != "" {
				words = s.aiPhraseWords(words, sentence, docType, row.Kind)
			}
		}
		if len(words) == 0 {
			continue
		}
		f := newFinding("ai_phrase", lineNo, "info", aiPhraseMessage(words), pystr.Strip(row.Raw))
		f.Words = words
		s.add(f)
	}
}

// aiPhraseWords は 1 文に当てた型のうち、除く条件に当たらないものを words の後ろに足して返す。
func (s *lintState) aiPhraseWords(words []string, sentence, docType, kind string) []string {
	text := pyre.NewText(sentence)
	for _, form := range aiPhraseForms {
		match := searchText(form.re, sentence)
		if match == nil || slices.Contains(words, form.name) {
			continue
		}
		start := match.Start(0)
		switch form.name {
		case "見立てです":
			// 「見立て」より前に判断した人や組織の語がある文は除く。
			if m, err := aiJudge.Get().Search(text, 0, runeIndexFrom(sentence, "見立て", start)); err != nil {
				panic(err)
			} else if m != nil {
				continue
			}
		case "が招く":
			if m, err := aiInviter.Get().Search(text, 0, start); err != nil {
				panic(err)
			} else if m != nil {
				continue
			}
		case "なんよ":
			if docType == "script" || docType == "essay" || (kind != "paragraph" && kind != "list") {
				continue
			}
		case "て形の体言止め":
			if slices.Contains([]string{"slides", "minutes", "promo", "script"}, docType) || kind != "paragraph" ||
				pystr.EndsWithAny(text.Slice(0, min(start+2, text.Len())), pausecomma.TENotVerb) {
				continue
			}
		}
		words = append(words, form.name)
	}
	return words
}

// runeIndexFrom は sentence.index(sub, start) (文字の位置)。
func runeIndexFrom(sentence, sub string, start int) int {
	rs := pyunicode.Runes(sentence)
	tail := pyunicode.FromRunes(rs[start:])
	i := strings.Index(tail, sub)
	if i < 0 {
		panic("runeIndexFrom: 手掛かりの語が無い")
	}
	return start + pyunicode.RuneCount(tail[:i])
}

// aiPhraseMessage は ai_phrase の message (本家の _AI_PHRASE_MESSAGE.format("、".join(rest)))。post.go の withWords が呼ぶ。
func aiPhraseMessage(words []string) string {
	return aiPhraseMessageHead + strings.Join(words, "、") + "）" + v220MessageTail
}
