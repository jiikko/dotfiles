package lint

import (
	"strings"

	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
)

// G2 文末の反復 (sentence_end_repetition。本家 yomiyasu_lint.py の 1802-1900 と 5574)。

// g02SentenceEnd は行のループの前に 1 回。段落の文を、絵文字で続けた所で分けてから、3 文以上続く同じ語尾を候補にする。
func (s *lintState) g02SentenceEnd() {
	s.findings = append(s.findings, sentenceEndFindings(splitDecorationBreaks(s.sentences))...)
}

// isEndMarkOrSpace は `ch in "。！？" or ch.isspace()`。
func isEndMarkOrSpace(r rune) bool {
	return strings.ContainsRune("。！？", r) || pyunicode.IsSpace(r)
}

// sentenceEndType は本家の _sentence_end_type。数えない語尾は「その他」。
// 本家は末尾の 64 字までを手で削り、それより長い句点・空白の並びは正規表現で全部削る。どちらも、文末の「。！？」と空白の並びを全部削る (並びの長さで答えは変わらない)。
func sentenceEndType(s string) string {
	rs := pyunicode.Runes(s)
	end := len(rs)
	for end > 0 && isEndMarkOrSpace(rs[end-1]) {
		end--
	}
	clean := pyunicode.FromRunes(rs[:end])
	// 文末の後ろの絵文字や空白は除いて語尾を見る。閉じかっこと閉じかぎは除かない (会話や補足の終わり)。
	if core, removed := stripEndDecoration(clean, ""); removed {
		cr := pyunicode.Runes(core)
		e := len(cr)
		for e > 0 && isEndMarkOrSpace(cr[e-1]) {
			e--
		}
		clean = pyunicode.FromRunes(cr[:e])
	}
	for _, suffix := range [...]string{"です", "ます", "でした", "ました", "である", "だ", "だろう", "でしょう", "ですね"} {
		if strings.HasSuffix(clean, suffix) {
			return suffix
		}
	}
	return "その他"
}

// splitDecorationBreaks は本家の _split_decoration_breaks。句点の代わりに絵文字を置いて続けた文を、句点で分けたときと同じように分ける。
// 分けるのは前後とも 4 字以上あるときだけ。語尾が数えない種類の文も分ける (分けないと次の文の語尾を引き継いで続きを誤って数える)。
func splitDecorationBreaks(sentences []plainSentence) []plainSentence {
	found := false
	for _, p := range sentences {
		if hasEndDecorationChar(pyunicode.Runes(p.Visible)) {
			found = true
			break
		}
	}
	if !found {
		return sentences
	}
	var out []plainSentence
	for _, p := range sentences {
		sentence := pyunicode.Runes(p.Visible)
		pieces := decorationBreaks(p.Visible)
		var cuts []int
		if pieces != nil {
			textEnd := len(pyunicode.Runes(pystr.RStrip(p.Visible)))
			position, start := 0, 0
			for _, piece := range pieces[:len(pieces)-1] {
				position += pystr.Len(piece)
				if pystr.Len(pystr.Strip(runeSlice(sentence, max(start, position-8), position))) > 3 && textEnd-position > 3 {
					cuts = append(cuts, position)
					start = position
				}
			}
		}
		if len(cuts) == 0 {
			out = append(out, p)
			continue
		}
		source := pyunicode.Runes(p.Source)
		sameWidth := len(source) == len(sentence)
		bounds := append(append([]int{0}, cuts...), len(sentence))
		for i := 0; i+1 < len(bounds); i++ {
			start, end := bounds[i], bounds[i+1]
			piece := plainSentence{Line: p.Line, Visible: pystr.Strip(runeSlice(sentence, start, end)), Source: p.Source, Group: p.Group}
			if sameWidth {
				piece.Source = pystr.Strip(runeSlice(source, start, end))
			}
			out = append(out, piece)
		}
	}
	return out
}

// sentenceEndFindings は本家の _sentence_end_findings。同じ段落 (Group が同じ) の続きで、同じ語尾が 3 文目になった文に候補を出す。
func sentenceEndFindings(sentences []plainSentence) []finding {
	var out []finding
	count := 1
	var prevType string
	for i, p := range sentences {
		endType := sentenceEndType(p.Visible)
		if i > 0 && p.Group == sentences[i-1].Group && endType != "その他" && endType == prevType {
			count++
			if count == 3 {
				out = append(out, newFinding("sentence_end_repetition", p.Line, "warn",
					"同一文末「"+endType+"」が3回以上連続しています。読みにくくなっていないか確認し、自然な説明や文体は保ってください。", p.Source))
			}
		} else {
			count = 1
		}
		prevType = endType
	}
	return out
}
