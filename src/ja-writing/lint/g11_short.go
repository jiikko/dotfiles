package lint

import (
	"strings"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// G11 短い文の型 (short_mochiron・fragment_run・cleft_list_ending。本家 yomiyasu_lint.py の 1561-1703 と 5817-5853)。正規表現は g11_tables.go。
// cleft_list_ending は G9 のフィラーの表 (fillerPatterns) を読む。

// g11State は G11 が文書の中で持つ状態。cleft_list_ending が前の文を探すために、行から (ブロックの番号, ブロックの中の位置) を引く表を、初めて使うときに作る。
type g11State struct {
	places map[int][2]int
}

const (
	shortMochironMessage = "文頭の「もちろん、」のあとに数語だけが続く短い文が検出されました。読み手が思いそうなことを先回りして認めているだけなら「もちろん、」を外してください。その含みを「当然、」などの別の語で補ったり、元にない理由や因果を足したりはしないでください。問いや依頼への答え、条件や前提の確認なら残してかまいません。"
	fragmentRunMessage   = "評価の語だけの短い文が3つ以上続いています（「効く。重い。安い。」など）。何がどうなのかが前後から分かる場合は、対象と評価を述語までつないでください。分からない場合は主語や理由を作らず、原文を残して書き手に確かめてください。小説の語りや詩など、書き手が意図した表現は残してかまいません。"
	cleftListMessage     = "「〜のは、」のあとに名詞を並べ、述語を書かずに終える文（「〜で変わったのは、A、B、C。」）が検出されました。何かが変わった、何かを目指したといった前提を前の文や見出しで書いていなければ、「のは」の前の動詞を述語に戻し、「〜では、A、B、Cが変わった。」のように述語まで書いてください。前提が直前に書いてある場合や、見出し、結び、スライドの箇条で書き手が強調として選んだ形なら残してかまいません。"
	cleftListStops       = "。！？!?"
)

// g11ShortSentences はこの順に出す: short_mochiron・fragment_run・cleft_list_ending。
func (s *lintState) g11ShortSentences(r *rowCtx) {
	if s.shortMochiron(r) {
		s.add(newFinding("short_mochiron", r.lineNo, "info", shortMochironMessage, r.stripped))
	}
	// 引用は中身を伏せた「」に置き換え、短い文の数に入れない (「1位は「田中」。」を断片にしない)。
	if strings.Count(r.plainText, "。")+strings.Count(r.plainText, "！") >= 3 {
		masked, err := quotedSegment.Get().Sub("「」", pyre.NewText(r.plainText), 0)
		if err != nil {
			panic(err)
		}
		if mustSearch(fragmentRun, masked) {
			s.add(newFinding("fragment_run", r.lineNo, "info", fragmentRunMessage, r.stripped))
		}
	}
	// 「〜のは、A、B、C。」(引用の中は伏せた文で見る)。直前の文が問いか同じ動詞の漢字を含むときは、前提があるとみて出さない。
	if strings.Contains(r.plainText, "のは、") && s.hasCleftListEnding(r) {
		s.add(newFinding("cleft_list_ending", r.lineNo, "info", cleftListMessage, r.stripped))
	}
}

// shortMochiron は文頭の「もちろん、」のあとに数語だけが続いて終わる短い文があるか (問いや依頼への答えの定型は除く)。
func (s *lintState) shortMochiron(r *rowCtx) bool {
	if !strings.Contains(r.plainText, "もちろん、") && !strings.Contains(r.plainText, "勿論、") {
		return false
	}
	closers := s.doc.endClosersFor(r.lineNo)
	for index, sentence := range r.lineSentences {
		core, _ := stripEndDecoration(sentence, closers)
		if (!mustMatchAt0(shortMochiron, sentence) && !mustMatchAt0(shortMochiron, core)) || mustMatchAt0(mochironAnswer, sentence) {
			continue
		}
		previous := lastContextSentence(r.sentenceBefore)
		if index > 0 {
			previous = r.lineSentences[index-1]
		}
		if endsWithQuestion(previous) {
			continue
		}
		return true
	}
	return false
}

// matchAt0 は p.match(text)。
func matchAt0(p *lazyPattern, text string) *pyre.Match {
	m, err := p.Get().Match(pyre.NewText(text), 0, pyre.NoEnd)
	if err != nil {
		panic(err)
	}
	return m
}

func subText(p *lazyPattern, repl, text string) string {
	out, err := p.Get().Sub(repl, pyre.NewText(text), 0)
	if err != nil {
		panic(err)
	}
	return out
}

// cleftListHead は本家の _cleft_list_head。「〜のは、A、B、C。」の形なら「のは」の前までを返す。
func cleftListHead(sentence string) (string, bool) {
	if !strings.Contains(sentence, "のは、") {
		return "", false
	}
	sentence = subText(cleftListSpace, "", subText(cleftListMaskedRun, "Ｘ", sentence))
	if m := matchAt0(cleftListComma, sentence); m != nil {
		return namedGroup(m, "head"), true
	}
	m := matchAt0(cleftListMixed, sentence)
	if m == nil {
		return "", false
	}
	items := namedGroup(m, "items")
	joiners := len(mustFindall(cleftListJoiner, items))
	// 読点だけの 2 項目 (「目指したのは、速さ、軽さ。」) は当てない。
	if joiners == 0 {
		return "", false
	}
	// 読点がなく「と・や」で 1 回つなぎ、最後の項目に「の」がある形 (「父と母の笑顔」) は、1 つの名詞句として当てない。
	if joiner := searchText(cleftListJoiner, items); joiners == 1 && !mustSearch(cleftListCommaMark, items) && joiner != nil &&
		strings.Contains(pyre.NewText(items).Slice(joiner.End(0), pyre.NewText(items).Len()), "の") {
		return "", false
	}
	return namedGroup(m, "head"), true
}

func namedGroup(m *pyre.Match, name string) string {
	text, _ := m.Group(m.Pattern().GroupIndex()[name])
	return text
}

// cleftListPremise は本家の _cleft_list_premise。直前の文が問いか、「のは」の前の動詞の語幹の漢字を含めば、前提を書いてあるとみる。
func cleftListPremise(head, previous string) bool {
	rs := pyunicode.Runes(previous)
	if end := questionContentEnd(rs); end > 0 {
		limited := pyunicode.FromRunes(rs[:end])
		if rs[end-1] == '？' || rs[end-1] == '?' || strings.HasSuffix(limited, "か。") || strings.HasSuffix(limited, "か") {
			return true
		}
	}
	stem := searchText(cleftListStem, head)
	if stem == nil {
		return false
	}
	text, _ := stem.Group(0)
	return strings.ContainsAny(previous, text)
}

func countStops(text string) int {
	n := 0
	for _, r := range pyunicode.Runes(text) {
		if strings.ContainsRune(cleftListStops, r) {
			n++
		}
	}
	return n
}

// cleftListJoin は本家の _cleft_list_join。rows[:position] の行を後ろから text の前につなぐ (改行で折り返した文を 1 つの文として読む)。最後の文が分かるだけ (句点などが 2 つになるまで) つなぐ。
func (s *lintState) cleftListJoin(rows []*visibility.Line, position int, text string) string {
	marks := countStops(text)
	for position > 0 && marks < 2 {
		position--
		piece := pystr.Strip(stripEmphasisMarks(s.doc.proseVisibleText(rows[position].Line)))
		marks += countStops(piece)
		text = piece + text
	}
	return text
}

// cleftListPrevious は本家の _cleft_list_previous。「〜のは、」の文の直前の文。同じ段落の前の文、なければ前の段落の最後の文、なければ直前の見出し。
// before はその行で「〜のは、」の文より前にある字。表・コード・引用などを挟んだときは前の文はないとみる。
func (s *lintState) cleftListPrevious(lineNo int, before string) string {
	blocks := s.an.Blocks
	if s.g11.places == nil {
		s.g11.places = map[int][2]int{}
		for number, block := range blocks {
			if block.Kind != "paragraph" && block.Kind != "list" {
				continue
			}
			for position, row := range block.Lines {
				s.g11.places[row.Line] = [2]int{number, position}
			}
		}
	}
	place := s.g11.places[lineNo]
	number, position := place[0], place[1]
	text := s.cleftListJoin(blocks[number].Lines, position, before)
	end := -1
	for i, r := range pyunicode.Runes(text) {
		if strings.ContainsRune(cleftListStops, r) {
			end = i
		}
	}
	if end >= 0 {
		return lastContextSentence(runeSlice(pyunicode.Runes(text), 0, end+1))
	}
	if number == 0 || (blocks[number-1].Kind != "paragraph" && blocks[number-1].Kind != "list" && blocks[number-1].Kind != "heading") {
		return ""
	}
	rows := blocks[number-1].Lines
	return lastContextSentence(s.cleftListJoin(rows, len(rows), ""))
}

// hasCleftListEnding は本家の _has_cleft_list_ending。行の文のどれかが、前提を書かずに「〜のは、A、B、C。」で終えていれば真。
// contexts は引用を伏せた文と同じブロックの次の文。引用を伏せた文と伏せていない文の数が同じ行では、前の文を伏せていない文で見る (「」の中の語も前提に数える)。
func (s *lintState) hasCleftListEnding(r *rowCtx) bool {
	contexts := s.rule9[r.lineNo]
	closers := s.doc.endClosersFor(r.lineNo)
	for index, c := range contexts {
		// 句点で終わらない文は、改行で折り返した文の途中でありうるので、ブロックの最後の文のときだけ当てる。
		if c.Following != "" && !strings.HasSuffix(c.Sentence, "。") && !strings.HasSuffix(c.Sentence, "！") && !strings.HasSuffix(c.Sentence, "!") {
			continue
		}
		head, ok := cleftListHead(c.Sentence)
		// 文末の後ろの飾り (「画面、帳票、通知 🚀」) を除いて読む。
		core, changed := stripEndDecoration(c.Sentence, closers)
		if !ok && changed {
			head, ok = cleftListHead(core)
		}
		// 頭が meta_filler の型の文 (「重要なのは、」) は、そちらだけを出す。
		if !ok || fillerHits(c.Sentence, core) {
			continue
		}
		var before []string
		if len(r.lineSentences) == len(contexts) {
			before = r.lineSentences
		} else {
			for _, item := range contexts {
				before = append(before, item.Sentence)
			}
		}
		if !cleftListPremise(head, s.cleftListPrevious(r.lineNo, strings.Join(before[:index], ""))) {
			return true
		}
	}
	return false
}

// fillerHits は meta_filler の型 (G9) のどれかが文か、文末の飾りを除いた文に当たるか。
func fillerHits(sentence, core string) bool {
	for _, p := range fillerPatterns {
		if mayMatch(p.required, sentence) && (mustSearch(p.re, sentence) || mustSearch(p.re, core)) {
			return true
		}
	}
	return false
}
