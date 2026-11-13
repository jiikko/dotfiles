package lint

import (
	"slices"
	"strings"

	"ja-writing/internal/pyre"
	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// G14 の metaphor_label (見出しか文書の 1 行目が、記号を除いて 8 字以下で、「〜の横」「〜のそば」「〜係」「〜番」で終わるか一語だけで、
// そのラベルが本文のほかの行に出てこないとき。本家 yomiyasu_lint.py の 3720-3844 と 4363-4442)。

// labelIsPhrase は本家の _label_is_phrase。ラベルが一語でなく句か (助詞、名詞を修飾する形、から・まで・より、動詞で終わる形)。
func labelIsPhrase(label string) bool {
	rs := pyunicode.Runes(label)
	if strings.ContainsAny(label, labelAnywhere) || strings.Contains(label, "から") || strings.Contains(label, "まで") || strings.Contains(label, "より") {
		return true
	}
	if strings.HasSuffix(label, "か") {
		return true
	}
	length := len(rs)
	for index := 1; index < length; index++ {
		char, before := rs[index], rs[index-1]
		if strings.ContainsRune(labelAfterNoun, char) && (isKanjiRune(before) || isKatakanaRune(before)) {
			return true
		}
		if strings.ContainsRune(labelBetween, char) && index+1 < length && isWordRune(rs[index+1]) {
			return true
		}
		if isHiraganaRune(char) && !isHiraganaRune(before) && (isKanjiRune(before) || isKatakanaRune(before)) {
			end := index
			for end < length && isHiraganaRune(rs[end]) {
				end++
			}
			if end < length && isWordRune(rs[end]) && strings.ContainsRune(labelAttributive, rs[end-1]) {
				return true
			}
		}
	}
	if strings.ContainsRune(labelVerbEnd, rs[length-1]) {
		index := length - 2
		for index >= 0 && isHiraganaRune(rs[index]) {
			index--
		}
		if index >= 0 && isKanjiRune(rs[index]) {
			return true
		}
	}
	// 動詞の終止形＋「こと」(守ること、いつも守ること) も句。
	return length > 2 && strings.HasSuffix(label, "こと") && strings.ContainsRune(labelVerbEnd, rs[length-3])
}

// labelKanjiRuns は本家の _label_kanji_runs。漢字の続き (2 字以上) とカタカナの続き (2 字以上)。kind は漢字が "k"、カタカナが "t"。
func labelKanjiRuns(text string) []labelRun {
	var runs []labelRun
	var current []rune
	kind := ""
	for _, char := range append(pyunicode.Runes(text), ' ') {
		now := ""
		if isKanjiRune(char) {
			now = "k"
		} else if isKatakanaRune(char) {
			now = "t"
		}
		if now == kind && now != "" {
			current = append(current, char)
			continue
		}
		if kind != "" && len(current) >= 2 {
			runs = append(runs, labelRun{kind, pyunicode.FromRunes(current)})
		}
		if now != "" {
			current, kind = []rune{char}, now
		} else {
			current, kind = nil, ""
		}
	}
	return runs
}

type labelRun struct{ kind, text string }

// labelText は本家の _label_text。見出しか 1 行目のラベル (記号と番号を除いた文字。英数字は残す)。
func labelText(row *visibility.Line, scan string) string {
	text := pystr.Strip(scan)
	if row.Kind == "heading" {
		text = pystr.Strip(pystr.StripChars(text, "#"))
	}
	text = pystr.Strip(replaceAll(labelNumbering, text, ""))
	var kept []rune
	for _, char := range pyunicode.Runes(text) {
		if isKanjiRune(char) || isKatakanaRune(char) || isHiraganaRune(char) || isAlnumWide(char) {
			kept = append(kept, char)
		}
	}
	return pyunicode.FromRunes(kept)
}

// replaceAll は p.sub(repl, text) (repl は置換の文字列で、中に逆スラッシュを持たない)。
func replaceAll(p *lazyPattern, text, repl string) string {
	out, err := p.Get().Sub(repl, pyre.NewText(text), 0)
	if err != nil {
		panic(err)
	}
	return out
}

// labelShape は本家の _label_shape。ラベルの形の条件 (8 字以下で、よく使う節の名前でなく、「〜の横」「〜のそば」「〜係」「〜番」で終わるか一語)。
func labelShape(label string) bool {
	rs := pyunicode.Runes(label)
	if len(rs) < 2 || len(rs) > labelMax || slices.Contains(labelCommon, label) {
		return false
	}
	if slices.ContainsFunc(rs, isAlnumWide) || pystr.EndsWithAny(label, labelNotEndings) {
		return false
	}
	run, longest := 0, 0
	for _, char := range rs {
		if isHiraganaRune(char) {
			run++
		} else {
			run = 0
		}
		longest = max(longest, run)
	}
	if longest >= 4 || !slices.ContainsFunc(rs, isWordRune) {
		return false
	}
	if pystr.EndsWithAny(label, labelEndings) {
		return true
	}
	if pystr.EndsWithAny(label, labelAbstractEndings) || (strings.HasSuffix(label, "方") && len(rs) > 1 && isHiraganaRune(rs[len(rs)-2])) {
		return false
	}
	return !labelIsPhrase(label)
}

// labelMayFit は本家の _label_may_fit。見出しか 1 行目が、伏せる前の文字でラベルになりうるか (長い見出しと、ふつうの見出しを先に飛ばす)。
// 伏せる所 (識別子、コード) は英数字か記号なので、漢字と仮名だけを残した文字で見る。
func labelMayFit(row *visibility.Line) bool {
	raw := row.Raw
	quick := replaceAll(labelNotJapanese, raw, "")
	if n := pystr.Len(quick); n < 2 || n > labelMax || strings.ContainsAny(raw, labelMarks) {
		return false // 台詞や引用、作品の名前、括弧の補足を含む行は、一語のラベルではない
	}
	return labelShape(quick)
}

// metaphorLabel は本家の _metaphor_label。metaphor_label のラベルを返す (当たらなければ空)。
func (s *lintState) metaphorLabel(row *visibility.Line, scan string) string {
	if row.Kind != "heading" && row.Kind != "paragraph" {
		return "" // 1 行目が表や箇条書きのときは見ない
	}
	if row.Kind != "heading" && pystr.EndsWithAny(pystr.RStrip(scan), []string{"。", "．", "！", "？", "!", "?"}) {
		return "" // 文として終わる 1 行目はラベルとみなさない
	}
	if strings.ContainsAny(row.Raw, labelMarks) {
		return ""
	}
	content := pystr.Strip(replaceAll(labelNumbering, pystr.Strip(pystr.StripChars(pystr.Strip(scan), "#")), ""))
	if mustSearch(labelSeparated, content) {
		return ""
	}
	// 番号のほかに英数字を含む見出し (v1.1.2 チェックポイント) は、版や名前を書いた見出しで、一語の比喩ではない。
	if mustSearch(labelAscii, replaceAll(labelNumbering, pystr.Strip(pystr.StripChars(pystr.Strip(row.Raw), "#")), "")) {
		return ""
	}
	text, raw := s.an.Text, row.Raw
	if nonSpaceCount(text)-nonSpaceCount(raw) < labelMinBody {
		return ""
	}
	label := labelText(row, scan)
	if !labelShape(label) {
		return ""
	}
	pieces := []string{label}
	stem := label
	for _, ending := range labelEndings {
		if strings.HasSuffix(stem, ending) {
			stem = strings.TrimSuffix(stem, ending)
			break
		}
	}
	// ラベルの漢字の続きの 2 字 (とカタカナの続き)。どれかが本文のほかの行に出れば、本文の話題の語とみなして出さない。
	for _, run := range labelKanjiRuns(stem) {
		if run.kind == "t" {
			pieces = append(pieces, run.text)
			continue
		}
		rs := pyunicode.Runes(run.text)
		for index := 0; index+1 < len(rs); index++ {
			pieces = append(pieces, pyunicode.FromRunes(rs[index:index+2]))
		}
	}
	if slices.ContainsFunc(uniqueInOrder(pieces), func(piece string) bool { return strings.Count(text, piece) > strings.Count(raw, piece) }) {
		return ""
	}
	if !pystr.EndsWithAny(label, labelEndings) {
		// 一語のラベルの漢字が、どれも本文のほかの所に出る (文体の節に「文」と「体」が出る) なら、本文の話題の語とみなす。
		var kanji []string
		for _, char := range pyunicode.Runes(label) {
			if isKanjiRune(char) {
				kanji = append(kanji, string(char))
			}
		}
		if len(kanji) >= 2 && !slices.ContainsFunc(kanji, func(char string) bool { return strings.Count(text, char) <= strings.Count(raw, char) }) {
			return ""
		}
	}
	return label
}
