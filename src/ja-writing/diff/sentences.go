package diff

import (
	"sort"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// 群 L: 文の組と本文。本家の _line_sentences・_sentence_pairs・_hunk_group・_blank_between・_run_shifted・_paragraph_count と、
// 本文として数える文 (_Masker・_Body・_all_bodies)。意味が変わる恐れの検出 (群 M 以降) が共通に使う下ごしらえ。

var (
	// 行頭の記号 (引用の >、箇条書きの記号、見出しの #、「(1)」「（１）」「1）」の番号)。位置から当てる (^ は付けない)
	linePrefix = pyre.NewLazy(`(?:[ \t]{0,3}>[ \t]?|[ \t]*(?:[-*+・]|\d+[.)])[ \t]+|[ \t]{0,3}#{1,6}(?:[ \t]+|$)`+
		`|[ \t]*(?:[(（]\d{1,9}[)）]|\d{1,9}）)[ \t　]*)`, 0)
	// 意味が変わる恐れの文の区切り。句点 (。！？!?) のほか「．」と、空白か行末が続く「.」でも切る
	riskSentencePiece = pyre.NewLazy(`(?:[^。！？!?．.\n]|\.(?![ \t]|$))+(?:[。！？!?．]+|\.+(?=[ \t]|$))?[」』）)]*`, 0)
)

// 文に分けない行の種類 (中の変化は「変えない箇所」で見る)
var noSentenceKinds = map[string]bool{"code": true, "frontmatter": true, "html_block": true}

// sentence は文 1 つ。text は行頭の記号 (箇条書き、見出し、引用) を外した文、line は行番号 (1 から)、start は文書の中の位置。
type sentence struct {
	text        string
	line, start int
}

func (s sentence) value() pyjson.Value {
	return []pyjson.Value{s.text, int64(s.line), int64(s.start)}
}

func sentencesValue(list []sentence) pyjson.Value {
	out := make([]pyjson.Value, len(list))
	for i, s := range list {
		out[i] = s.value()
	}
	return out
}

// lineSentences は行 first から last の手前まで (0 から数える) を文に分ける。行頭の箇条書きの記号、見出しの #、引用の > を外す。
// コード、先頭の設定、HTML の行は分けない。
func (s *sides) lineSentences(side, first, last int) []sentence {
	var out []sentence
	rows := s.analyses[side].Lines
	for index := first; index < last; index++ {
		if index < len(rows) && noSentenceKinds[rows[index].Kind] {
			continue
		}
		text := pyre.NewText(s.lines[side][index])
		offset := 0
		for {
			m, err := linePrefix.Get().Match(text, offset, pyre.NoEnd)
			if err != nil {
				panic(err)
			}
			if m == nil {
				break
			}
			offset = m.End(0)
		}
		pieces, err := riskSentencePiece.Get().Finditer(text, offset, pyre.NoEnd)
		if err != nil {
			panic(err)
		}
		for _, m := range pieces {
			whole, _ := m.Group(0)
			if stripped := pystr.Strip(whole); stripped != "" {
				start := m.Start(0) + (pystr.Len(whole) - pystr.Len(pystr.LStrip(whole)))
				out = append(out, sentence{stripped, index + 1, s.starts[side][index] + start})
			}
		}
	}
	return out
}

// sentenceGroup は hunk 1 つ (か、まとめた hunk の並び) の文の組。pairs は (元の文の番号, 後の文の番号)。run は並びをまとめたときの元の hunk (そうでなければ nil)。
type sentenceGroup struct {
	hunk     *hunk
	run      []*hunk
	old, new []sentence
	pairs    [][2]int
}

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

func sentenceTexts(list []sentence) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.text
	}
	return out
}

// alignedPairs は文の並びを、中身が同じ文を先に組にしてから、その間の文を同じ窓 (直前の組の次から 4 文、似かた 0.45 以上で最大) で組にする。
func alignedPairs(old, new []sentence) [][2]int {
	var pairs [][2]int
	for _, p := range align(sentenceTexts(old), sentenceTexts(new), true) {
		pairs = append(pairs, [2]int{p.i, p.j})
	}
	return pairs
}

// sentencePairs は本家の _sentence_pairs: 意味が変わる恐れの文の組 (共通の下ごしらえ)。replace の塊だけ文を組にする。追加と削除の塊の文は、すべて組にならない文。
// 空白だけの行しか挟まない hunk の並びで、元と後の空白でない行の数 (段落の数) が違うときは、並び全体の文をまとめて組にする
// (行の対応は空行を順に組にするので、段落を 1 つ分けると後ろの段落がすべて 1 つずれて別の段落と組になる)。
func (s *sides) sentencePairs(hunks []*hunk) []*sentenceGroup {
	var groups []*sentenceGroup
	for index := 0; index < len(hunks); {
		run := []*hunk{hunks[index]}
		for index+len(run) < len(hunks) && s.blankBetween(run[len(run)-1], hunks[index+len(run)]) {
			run = append(run, hunks[index+len(run)])
		}
		if len(run) > 1 && s.runShifted(run) {
			var old, new []sentence
			for _, h := range run {
				old = append(old, s.lineSentences(0, h.oldStart-1, h.oldEnd)...)
				new = append(new, s.lineSentences(1, h.newStart-1, h.newEnd)...)
			}
			var pairs [][2]int
			if len(old) > 0 && len(new) > 0 {
				pairs = alignedPairs(old, new)
			}
			merged := *run[0]
			merged.tag = "replace"
			groups = append(groups, &sentenceGroup{hunk: &merged, run: run, old: old, new: new, pairs: pairs})
		} else {
			for _, h := range run {
				groups = append(groups, s.hunkGroup(h))
			}
		}
		index += len(run)
	}
	return groups
}

func (s *sides) hunkGroup(h *hunk) *sentenceGroup {
	g := &sentenceGroup{hunk: h, old: s.lineSentences(0, h.oldStart-1, h.oldEnd), new: s.lineSentences(1, h.newStart-1, h.newEnd)}
	if h.tag == "replace" && len(g.old) > 0 && len(g.new) > 0 {
		// 1 行対 1 行の置き換えで、両側が 1 文ずつなら、似かたによらず組にする (行の対応で同じ場所と分かっているため)
		if len(g.old) == 1 && len(g.new) == 1 && h.oldEnd == h.oldStart && h.newEnd == h.newStart {
			g.pairs = [][2]int{{0, 0}}
		} else {
			g.pairs = alignedPairs(g.old, g.new)
		}
	}
	return g
}

// blankBetween は 2 つの hunk の間の、変わっていない行が両側とも空白だけか (間に行がなくても真)。
func (s *sides) blankBetween(first, second *hunk) bool {
	for side, span := range [2][2]int{{first.oldEnd, second.oldStart - 1}, {first.newEnd, second.newStart - 1}} {
		for at := span[0]; at < span[1]; at++ {
			if !isBlank(s.lines[side][at]) {
				return false
			}
		}
	}
	return true
}

// runShifted は、空白だけの行しか挟まない hunk の並びが、段落を分けたか合わせたためにずれて組になった形か。
// 段落の数が元と後で違う hunk があり、置き換えの hunk がほかにもあるときに真。1 つの hunk の中で行の数だけが違う (段落を空行のない箇条書きにした) なら偽。
func (s *sides) runShifted(run []*hunk) bool {
	differs, replaced := false, 0
	for _, h := range run {
		old, new := paragraphCount(h.oldLines), paragraphCount(h.newLines)
		if old != new {
			differs = true
		}
		if h.tag == "replace" && old > 0 && new > 0 {
			replaced++
		}
	}
	return differs && replaced >= 2
}

func paragraphCount(lines []string) int {
	count, inside := 0, false
	for _, line := range lines {
		blank := isBlank(line)
		if !blank && !inside {
			count++
		}
		inside = !blank
	}
	return count
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

// 本文として数えない行の種類 (コード、URL、表、見出しなど)
var bodySkipKinds = map[string]bool{
	"code": true, "frontmatter": true, "html_block": true, "table": true, "heading": true, "heading_underline": true,
	"reference_definition": true, "thematic_break": true, "blank": true,
}

// 文の中で伏せる範囲の種類 (インラインのコード、URL、リンク先、画像、HTML のタグ)
var bodyMaskKinds = map[string]bool{"inline_code": true, "autolink": true, "bare_url": true, "link_destination": true, "image": true, "html_token": true}

// body は数える文。text は伏せる範囲を除いた文、raw は元の文 (行頭の印を外したもの)。
type body struct {
	text, raw   string
	line, start int
}

func (b body) value() pyjson.Value {
	return []pyjson.Value{b.text, b.raw, int64(b.line), int64(b.start)}
}

// masker は 1 つの側の、数えない行と伏せる範囲。
type masker struct {
	rows  []*visibility.Line
	spans [][2]int
	lefts []int
}

// newMasker は本家の _Masker。
func (s *sides) newMasker(side int) *masker {
	a := s.analyses[side]
	m := &masker{rows: a.Lines}
	for _, span := range a.ProtectedSpans {
		if bodyMaskKinds[span.Kind] {
			m.spans = append(m.spans, [2]int{span.Start, span.End})
		}
	}
	sort.Slice(m.spans, func(i, j int) bool {
		return m.spans[i][0] < m.spans[j][0] || (m.spans[i][0] == m.spans[j][0] && m.spans[i][1] < m.spans[j][1])
	})
	for _, span := range m.spans {
		m.lefts = append(m.lefts, span[0])
	}
	return m
}

// mask は位置 start から始まる text から、伏せる範囲を除いた文字列。
func (m *masker) mask(text string, start int) string {
	runes := pyunicode.Runes(text)
	end := start + len(runes)
	first := max(0, sort.SearchInts(m.lefts, start+1)-1)
	var pieces []rune
	cursor := start
	for index := first; index < len(m.spans); index++ {
		left, right := m.spans[index][0], m.spans[index][1]
		if left >= end {
			break
		}
		if right <= start {
			continue
		}
		if left > cursor {
			pieces = append(pieces, runes[cursor-start:left-start]...)
		}
		cursor = max(cursor, right)
	}
	if cursor == start {
		return text
	}
	if cursor < end {
		pieces = append(pieces, runes[cursor-start:]...)
	}
	return pyunicode.FromRunes(pieces)
}

func (m *masker) body(sn sentence) (body, bool) {
	index := sn.line - 1
	if index < len(m.rows) && bodySkipKinds[m.rows[index].Kind] {
		return body{}, false
	}
	return body{m.mask(sn.text, sn.start), sn.text, sn.line, sn.start}, true
}

// allBodies は本家の _all_bodies: 文書全体の数える文 (コード、表、見出しなどを除き、伏せる範囲を除いたもの)。
func (s *sides) allBodies(side int) []body {
	m := s.newMasker(side)
	var out []body
	for _, sn := range s.lineSentences(side, 0, len(s.lines[side])) {
		if b, ok := m.body(sn); ok && pystr.Strip(b.text) != "" {
			out = append(out, b)
		}
	}
	return out
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
