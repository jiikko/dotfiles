package lint

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"zundamon-kaisetsu/internal/app"
	"zundamon-kaisetsu/internal/paths"
	"zundamon-kaisetsu/internal/pyjson"
	"zundamon-kaisetsu/internal/script"
	"zundamon-kaisetsu/internal/textx"
)

// 台本と資料の突き合わせ (lint --source 資料)。候補を出すだけで、判断は人と確認役が行う (どれも目安で rc に効かない)。
//
// 出すもの: 資料に無い数字 (95.03%・2018年・8割)・資料に無い英字の語・導入の終わりに本題へ移る区切りの語が無い。
// 数字は、台本と資料の両方から同じ規則で「数字と単位の組」(6割・95.03%・2018年度) を取り出し、組どうしを比べる (部分一致にしない:
// 資料の「95.03%」の中の「3%」、「16割」の中の「6割」、「2018年度」の中の「2018年」で誤った数字が素通りした)。
// 資料と違う数字を素通りさせないための字句の近似の手直しは、2 周続けて破られたので (部分一致 → 単位・範囲・符号)、これ以上は足さない。
// 数字の照合は目安で、事実の確認の本体は手順 3-3 の確認役。
// 月日だけは書き方を揃えて比べる: 台本は「9月22日」と書く決まりで、資料は「9/22」「2024/9/22」「2024-09-22」と書くことが多く、
// 日付の多い資料 (障害の報告・年表) では目安の大半がこの書き方の違いになって、目安ごと読み飛ばされる。台本の「9月22日」は 1 組にして、
// 資料に同じ月日 (9月22日・9/22・2024-09-22) があるときだけ通す。資料が「9月に…22日に」と月と日を離して書く形は出る (受ける誤検出)。
// 「9月」と「22日」が別々にあれば通す形にしない: 資料の「9月10日」と「10月22日」から、資料に無い「9月22日」を通してしまう。
// 資料の側は「9月」「22日」も残す (台本が「22日」とだけ言う形を通す)。資料の「9/22」からは「22日」を作らない (部分一致になる)。
// 検出しない形: 漢数字 (六割)・一覧に無い単位 (GHz・km)・code / mermaid / image の図解の中の数字・表記ゆれ (「3か月」と「3ヶ月」・
// 「約3割」と「30%」は誤検出になる)・単位の付かない数字 (文脈が分からない)・数字・語の言い換え (「6 割」を「過半数」) と、資料に現れるが
// 文脈の違う数字 (資料の分数・比率・ページの「1/2」「3/10」も月日と数えるので、台本の 1月2日・3月10日 が資料に無くても通る)・
// 年の無い「9-22」と米式の「9/22/2024」(月日と見ない)・
// 導入の区切りの言葉の否定 (「本題じゃない」)。チャプターが 1 個以下なら導入の区切りは見ない。「1つ」「2つ」のような数える「〜つ」は
// 話の中で数えるだけのことが多いので見ない。重要語の図解の語がセリフに無いかは、言い回しの違い (「Windows 95.03%」と「Windowsが95.03%」)
// で鳴りすぎたので入れなかった (2026-10-08 に 3 本で試作して 6 件中ほとんどが言い回しの違い)。

var (
	// 数字と単位の組を取る (左端から最長で当たるので、数字の途中からは始まらない)。単位は長いものから先に当てる (年度を年より先に)
	// 範囲 (10〜20%) は左の数字にも右の単位を付けて取る。万・億・兆の後ろの単位 (100万円 / 100万ドル) も組に含める
	sourceNumberRe = regexp.MustCompile(`(?i)([-−]?\d+(?:[.,]\d+)*)(?:\s*[〜～~-]\s*([-−]?\d+(?:[.,]\d+)*))?\s*((?:万|億|兆)?(?:パーセント|年度|か月|ヶ月|カ月|ヵ月|時間|段階|種類|ドル|GB|MB|TB|%|割|年|月|日|人|名|歳|才|個|件|本|回|倍|分|秒|円|つ|位|社|台|行|字|代)|万|億|兆)`)
	thousandsRe    = regexp.MustCompile(`,(\d{3})`)
	countingRe     = regexp.MustCompile(`^\d+つ$`)
	introBoundary  = []string{"本題"}
	monthDayJaRe   = regexp.MustCompile(`(\d{1,2})月(\d{1,2})日`)
	// 資料の側の月日の書き方。年の無い「-」は範囲 (9-22%) と区別できないので取らない
	monthDaySlashRe = regexp.MustCompile(`(\d{4})/(\d{1,2})/(\d{1,2})|(\d{4})-(\d{1,2})-(\d{1,2})|(\d{1,2})/(\d{1,2})`)
)

// numberTokens は文の中の「数字と単位の組」と月日を、比べられる形 (全角を半角に・桁区切りのカンマを除く・パーセントを % に・
// 月日は「9月22日」の 1 組) で、文の中の順に返す。
func numberTokens(s string) []string {
	s = textx.FoldWidth(s)
	var out []string
	last := 0
	for _, m := range monthDayJaRe.FindAllStringSubmatchIndex(s, -1) {
		k := monthDay(s[m[2]:m[3]], s[m[4]:m[5]])
		if k == "" {
			continue
		}
		out = append(out, unitNumberTokens(s[last:m[0]])...)
		out = append(out, k)
		last = m[1]
	}
	return append(out, unitNumberTokens(s[last:])...)
}

// sourceNumbers は資料の側の組。台本の側 (numberTokens) より広く取る: 「9月22日」の「9月」「22日」も残し、
// 「9/22」「2024/9/22」「2024-09-22」の月日 (年があれば「2024年」も) を足す。
func sourceNumbers(source string) map[string]bool {
	s := textx.FoldWidth(source)
	set := map[string]bool{}
	for _, ks := range [][]string{numberTokens(s), unitNumberTokens(s), slashMonthDays(s)} {
		for _, k := range ks {
			set[k] = true
		}
	}
	return set
}

// slashMonthDays は「9/22」「2024/9/22」「2024-09-22」の月日 (と年)。前後に数字か「/」が続く形 (パスの 33259/12・9/22/2024) は月日と見ない
func slashMonthDays(s string) []string {
	var out []string
	for _, m := range monthDaySlashRe.FindAllStringSubmatchIndex(s, -1) {
		if (m[0] > 0 && isDigitOrSlash(s[m[0]-1])) || (m[1] < len(s) && isDigitOrSlash(s[m[1]])) {
			continue
		}
		g := func(i int) string {
			if m[2*i] < 0 {
				return ""
			}
			return s[m[2*i]:m[2*i+1]]
		}
		k := monthDay(g(2)+g(5)+g(7), g(3)+g(6)+g(8))
		if k == "" {
			continue
		}
		out = append(out, k)
		if y := g(1) + g(4); y != "" {
			out = append(out, y+"年")
		}
	}
	return out
}

// monthDay は月日の比べる形「9月22日」(先頭の 0 は除く)。月日として成り立たない数 (13月・32日) なら ""
func monthDay(month, day string) string {
	mo, err1 := strconv.Atoi(month)
	d, err2 := strconv.Atoi(day)
	if err1 != nil || err2 != nil || mo < 1 || mo > 12 || d < 1 || d > 31 {
		return ""
	}
	return fmt.Sprintf("%d月%d日", mo, d)
}

func isDigitOrSlash(b byte) bool { return (b >= '0' && b <= '9') || b == '/' }

// unitNumberTokens は文の中の「数字と単位の組」(月日を分けずに、9月22日 なら「9月」「22日」)。s は全角を半角にした文
func unitNumberTokens(s string) []string {
	var out []string
	num := func(n string) string { // 桁区切りのカンマ (後ろが 3 桁) だけを除き、全角のマイナスを揃える。小数のカンマ (10,5) は残す
		return strings.ReplaceAll(thousandsRe.ReplaceAllString(n, "$1"), "−", "-")
	}
	for _, m := range sourceNumberRe.FindAllStringSubmatch(s, -1) {
		unit := strings.ToUpper(strings.ReplaceAll(m[3], "パーセント", "%"))
		if m[2] != "" {
			out = append(out, num(m[1])+unit, num(m[2])+unit)
			continue
		}
		out = append(out, num(m[1])+unit)
	}
	return out
}

// SourceCandidates は台本の行と資料の文を突き合わせた候補 (すべて目安)。
func SourceCandidates(s *script.Script, source string) []lintIssue {
	var out []lintIssue
	hint := func(i int, rule, msg string) {
		out = append(out, lintIssue{Line: i, Rule: rule, Msg: msg, Hint: true})
	}
	srcNumbers := sourceNumbers(source)
	srcFold := textx.FoldWidth(source)
	for i, line := range s.Lines {
		// 字幕と、図解の中の文字 (重要語・比較の見出しと項目) を見る (図解の中の誤った数字も素通りさせない)
		parts := []string{pyjson.Str(line["text"])}
		if sh, ok := line["show"].(map[string]any); ok {
			parts = append(parts, showAllTexts(sh)...)
		}
		text := textx.FoldWidth(strings.Join(parts, " "))
		seenNum := map[string]bool{}
		for _, key := range numberTokens(text) {
			if seenNum[key] || countingRe.MatchString(key) || srcNumbers[key] {
				continue
			}
			seenNum[key] = true
			hint(i, "source-number", "数字「"+key+"」が資料に無い (資料の外から足した事実か、資料との食い違い)")
		}
		seen := map[string]bool{}
		for _, w := range textx.LineWords(text) {
			if seen[w] || dictWordInFold(srcFold, w) {
				continue
			}
			seen[w] = true
			hint(i, "source-word", "英字の語「"+w+"」が資料に無い (資料の外から足した語。手順 3-4 でユーザーに挙げる)")
		}
	}
	// 導入 (最初のチャプター) の終わり: 2 つ目のチャプターの行とその手前 3 行に、本題へ移る区切りの語が無い
	var chapters []int
	for i, line := range s.Lines {
		if pyjson.Truthy(line["chapter"]) {
			chapters = append(chapters, i)
		}
	}
	if len(chapters) >= 2 {
		c := chapters[1]
		var tail strings.Builder
		for j := max(chapters[0], c-3); j <= c; j++ {
			tail.WriteString(pyjson.Str(s.Lines[j]["text"]))
		}
		found := false
		for _, w := range introBoundary {
			if strings.Contains(tail.String(), w) {
				found = true
			}
		}
		if !found {
			hint(c, "intro-boundary", "導入の終わりに本題へ移る区切りの言葉が無い (めたんが「それじゃあ、本題に入るわね」と言い、ずんだもんが応じる)")
		}
	}
	return out
}

// dictWordInFold は英字の語 w が資料に語として (大文字小文字を区別せず、前後が英字でない位置に) 現れるか。
func dictWordInFold(source, w string) bool {
	return regexp.MustCompile(`(?i)(?:^|[^A-Za-z])` + regexp.QuoteMeta(w) + `(?:[^A-Za-z]|$)`).MatchString(source)
}

func ReadSource(path string) (string, error) {
	b, err := os.ReadFile(paths.ResolvePath(path))
	if err != nil {
		return "", app.Fail("%s: 資料を読めない (%v。--source にはテキストの資料を渡す)", path, err)
	}
	if strings.TrimSpace(string(b)) == "" {
		return "", app.Fail("%s: 資料が空 (台本の数字と語がすべて候補になってしまう)", path)
	}
	return string(b), nil
}

// showAllTexts は図解の中の文字 (重要語の語・補足、比較の見出し・項目、箇条書きの見出し・項目)。
func showAllTexts(sh map[string]any) []string {
	out := showTexts(sh)
	if v, ok := sh["title"].(string); ok && pyjson.Str(sh["type"]) == "list" {
		out = append(out, v)
	}
	if v, ok := sh["text"].(string); ok && pyjson.Str(sh["type"]) == "keyword" {
		out = append(out, v)
	}
	for _, side := range []string{"left", "right"} {
		if m, ok := sh[side].(map[string]any); ok {
			if v, ok := m["title"].(string); ok {
				out = append(out, v)
			}
		}
	}
	return out
}
