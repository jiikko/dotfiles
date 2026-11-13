package lint

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"ja-writing/internal/pystr"
	"ja-writing/internal/pyunicode"
	"ja-writing/visibility"
)

// G19 文書の型 (dialogue_period・narrative_density。本家 yomiyasu_lint.py の 5162-5300)。表は g19_tables.go。
// 種類を限って当てる規則で、dialogue_period は script、narrative_density は essay のときだけ数える。文書に 1 件。

// g19State は G19 が文書の中で持つ状態。文書ごとに 1 回数えるだけなので、持つものは無い。
type g19State struct{}

// dialogueLine は台詞 1 つ (行の番号と中身)。
type dialogueLine struct {
	Line    int
	Content string
}

// topQuoteContents は本家の _top_quote_contents。1 行の中のいちばん外の「」と『』の中身 (閉じていない括弧は数えない)。
func topQuoteContents(text string) []string {
	type open struct {
		char  rune
		index int
	}
	var contents []string
	var stack []open
	runes := pyunicode.Runes(text)
	for index, char := range runes {
		if _, ok := quotePairs[char]; ok {
			stack = append(stack, open{char, index})
		} else if (char == '」' || char == '』') && len(stack) > 0 && quotePairs[stack[len(stack)-1].char] == char {
			start := stack[len(stack)-1].index
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				contents = append(contents, runeSlice(runes, start+1, index))
			}
		}
	}
	return contents
}

// skipsDocumentRow は台詞と地の文を探さない行 (見出し・コード・表・引用)。
func skipsDocumentRow(row *visibility.Line) bool {
	return slices.Contains(documentSkipKinds, row.Kind) || row.QuoteDepth != 0 || row.Kind == "quote"
}

// dialogueLines は本家の _dialogue_lines。台詞の (行の番号, 中身) の並び。「」と『』の中身、「名前：…」の行の中身を数える。
// 「」が 1 つもない行だけ「名前：…」の形を見る。
func (s *lintState) dialogueLines() []dialogueLine {
	var dialogue []dialogueLine
	for _, row := range s.an.Lines {
		if skipsDocumentRow(row) {
			continue
		}
		visible := strings.ReplaceAll(strings.ReplaceAll(s.doc.proseVisibleText(row.Line), "__", ""), "*", "")
		if pystr.Strip(visible) == "" {
			continue
		}
		var quoted []string
		if strings.Contains(visible, "「") || strings.Contains(visible, "『") {
			quoted = topQuoteContents(visible)
		}
		if len(quoted) == 0 {
			speaker := matchAt0(speakerLine, visible)
			if speaker == nil {
				continue
			}
			name, _ := speaker.Group(1)
			if mustMatchAt0(notSpeaker, name) {
				continue
			}
			body, _ := speaker.Group(2)
			quoted = []string{subText(edgeStageNote, "", pystr.Strip(body))}
		}
		for _, content := range quoted {
			if content = pystr.StripChars(content, " \t\u3000"); content != "" {
				dialogue = append(dialogue, dialogueLine{row.Line, content})
			}
		}
	}
	return dialogue
}

// narrativeCounts は本家の _narrative_counts の戻り (地の文の字数・一人称の数・数字の数・箇条書きの項目の数・最初の行の番号)。
type narrativeCounts struct {
	chars, firstPerson, numbers, bullets int
	firstLine                            int // 地の文が無ければ 0 (本家は None)
}

// narrativeCounts は本家の _narrative_counts。見出し・コード・表・引用・画像だけの行は数えない。字数は空白を除いた見える文字。
// 一人称と数字は、カギ括弧の中と英数字の識別子を伏せてから数える。
func (s *lintState) narrativeCounts() narrativeCounts {
	var n narrativeCounts
	for _, row := range s.an.Lines {
		if skipsDocumentRow(row) {
			continue
		}
		visible := s.doc.proseVisibleText(row.Line)
		if pystr.Strip(visible) == "" {
			continue
		}
		// 画像だけの行 (バッジ) は数えない (指標と同じ判定の門)。
		if strings.HasPrefix(pystr.LStrip(row.Raw), "[![") && s.doc.imageOnlyRow(row, visible) {
			continue
		}
		plain := strings.ReplaceAll(strings.ReplaceAll(visible, "__", ""), "*", "")
		length := nonSpaceCount(plain)
		if length == 0 {
			continue
		}
		if n.firstLine == 0 {
			n.firstLine = row.Line
		}
		n.chars += length
		if row.Kind == "list" && mustMatchAt0(listMarkerLine, row.Raw) {
			n.bullets++
		}
		masked := plain
		if strings.Contains(plain, "「") || strings.Contains(plain, "『") {
			masked = maskQuotedSegments(plain)
		}
		if strings.Contains(masked, "_") || strings.Contains(masked, ".") || strings.Contains(masked, "/") {
			masked = subText(asciiIdentifier, " ", masked)
		}
		n.firstPerson += len(mustFindall(firstPerson, masked))
		n.numbers += len(mustFindall(narrativeNumber, masked))
	}
	return n
}

// perThousand は本家の _per_thousand。
func perThousand(count, chars int) float64 {
	if chars == 0 {
		return 0
	}
	return float64(count*1000) / float64(chars)
}

// thousands は `"{:,}".format(n)`。
func thousands(n int) string {
	digits := strconv.Itoa(n)
	var b strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	return b.String()
}

// g19DocumentFindings は後から足す検出の 6 番目 (本家の _v220_document_findings。script と essay だけ)。
func (s *lintState) g19DocumentFindings() {
	switch s.docTypeName() {
	case "script":
		dialogue := s.dialogueLines()
		ended := 0
		for _, d := range dialogue {
			if strings.HasSuffix(d.Content, "。") || strings.HasSuffix(d.Content, "｡") {
				ended++
			}
		}
		if len(dialogue) >= dialogueMin && float64(ended) >= dialogueRatio*float64(len(dialogue)) {
			lineNo := dialogue[0].Line
			message := "台詞の最後の句点（台詞" + strconv.Itoa(len(dialogue)) + "個のうち" + strconv.Itoa(ended) + "個、" +
				strconv.Itoa(int(math.RoundToEven(float64(ended*100)/float64(len(dialogue))))) + "%が「。」で終わる）の候補です。文脈を見て、誰が何をどうするかを書き直すか確かめてください。"
			s.add(newFinding("dialogue_period", lineNo, "info", message, pystr.Strip(s.an.Lines[lineNo-1].Raw)))
		}
	case "essay":
		n := s.narrativeCounts()
		personRate := perThousand(n.firstPerson, n.chars)
		density := perThousand(n.numbers+n.bullets, n.chars)
		if n.chars >= narrativeMinChars && personRate < firstPersonMax && density >= densityMin {
			message := "読み物の一人称と数字（地の文" + thousands(n.chars) + "字、一人称は千字あたり" + strconv.FormatFloat(personRate, 'f', 1, 64) +
				"、数字と箇条書きは千字あたり" + strconv.FormatFloat(density, 'f', 1, 64) +
				"）の候補です。文脈を見て、誰が何をどうするかを書き直すか確かめてください。一人称と体験は足さず、数字も消しません。"
			s.add(newFinding("narrative_density", n.firstLine, "info", message, pystr.Strip(s.an.Lines[n.firstLine-1].Raw)))
		}
	}
}
