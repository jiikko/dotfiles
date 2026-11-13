package lint

import (
	"slices"
	"strconv"
	"strings"

	"ja-writing/lint/pausecomma"
)

// G13 読点 (pause_comma。本家 yomiyasu_lint.py の 2967-3380 と 5878-5904)。当て方の中身は lint/pausecomma。
// G17 (slide_comma) が候補の Objects と Heads を読む。

// g13State は G13 が文書の中で持つ状態。lines は段落と箇条書きの項目ごとに行をつないで当てた結果 (本家の pause_lines)。
// 文書で初めて当てる行で 1 回だけ作る (作ったかは built で持つ。当たりの無い文書でも表は作り直さない)。
type g13State struct {
	built bool
	lines map[int][]pausecomma.Hit
}

const pauseCommaMessageTail = "のすぐあとに読点があります（%s）。間を作るだけの読点は、AIの文章らしく見えます。読点のあとに別の主語が来る、条件や場面の枠を示す、並べている、外すと語がつながったり掛かり先が変わったりする、のどれにも当たらなければ、読点だけを外してください（語順と述語は変えません）。小説や随筆で書き手が間として置いた読点は残してかまいません。"

// g13PauseComma は地の文の行ごと (本家の 5879-5904)。種類が pause_comma を当てる種類のときだけ当てる。
func (s *lintState) g13PauseComma(r *rowCtx) {
	if only, restricted := docTypeOnlyRules["pause_comma"]; restricted && !slices.Contains(only, s.docTypeName()) {
		return
	}
	objects := s.docTypeName() != "essay"
	if !s.g13.built {
		s.g13.lines = pausecomma.LineHits(s.an, s.doc.proseVisibleText, objects)
		s.g13.built = true
	}
	hits, ok := s.g13.lines[r.lineNo]
	if !ok {
		hits = pausecomma.Fallback(r.plainText, objects)
	}
	if len(hits) == 0 {
		return
	}
	var phrases strings.Builder
	for _, h := range hits[:min(3, len(hits))] {
		phrases.WriteString("「" + h.Phrase + h.Chunk + "、」")
	}
	if len(hits) > 3 {
		phrases.WriteString("ほか" + strconv.Itoa(len(hits)-3) + "か所")
	}
	objectWords := []string{}
	for _, h := range hits {
		if h.Chunk == "を" {
			objectWords = append(objectWords, h.Phrase+"を")
		}
	}
	where := "短い主語や主題"
	if len(objectWords) > 0 {
		where = "短い主語や主題、目的語"
	}
	f := newFinding("pause_comma", r.lineNo, "info", where+strings.Replace(pauseCommaMessageTail, "%s", phrases.String(), 1), r.stripped)
	f.Objects = objectWords
	f.Heads = len(objectWords) < len(hits)
	s.add(f)
}
