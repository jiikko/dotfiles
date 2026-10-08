package main

import (
	"os"
	"regexp"
	"strings"
)

// 台本と資料の突き合わせ (lint --source 資料。issue 685)。候補を出すだけで、判断は人と確認役が行う (どれも目安で rc に効かない)。
//
// 出すもの: 資料に無い数字 (95.03%・2018年・8割)・資料に無い英字の語・導入の終わりに本題へ移る区切りの語が無い。
// 数字は、台本と資料の両方から同じ規則で「数字と単位の組」(6割・95.03%・2018年度) を取り出し、組どうしを比べる (部分一致にしない:
// 資料の「95.03%」の中の「3%」、「16割」の中の「6割」、「2018年度」の中の「2018年」で誤った数字が素通りした。issue 685 の敵対的レビュー)。
// 字句の近似の手直しを 2 周続けて破られたので (部分一致 → 単位・範囲・符号)、これ以上は直さない。数字の照合は目安で、事実の確認の本体は
// 手順 3-3 の確認役。
// 検出しない形: 漢数字 (六割)・一覧に無い単位 (GHz・km)・code / mermaid / image の図解の中の数字・表記ゆれ (「3か月」と「3ヶ月」・
// 「約3割」と「30%」は誤検出になる)・単位の付かない数字 (文脈が分からない)・数字・語の言い換え (「6 割」を「過半数」、「10/1」と「10月1日」) と、資料に現れるが
// 文脈の違う数字・導入の区切りの言葉の否定 (「本題じゃない」)。チャプターが 1 個以下なら導入の区切りは見ない。「1つ」「2つ」のような数える「〜つ」は
// 話の中で数えるだけのことが多いので見ない。重要語の図解の語がセリフに無いかは、言い回しの違い (「Windows 95.03%」と「Windowsが95.03%」)
// で鳴りすぎたので入れなかった (2026-10-08 に 3 本で試作して 6 件中ほとんどが言い回しの違い)。

var (
	// 数字と単位の組を取る (左端から最長で当たるので、数字の途中からは始まらない)。単位は長いものから先に当てる (年度を年より先に)
	// 範囲 (10〜20%) は左の数字にも右の単位を付けて取る。万・億・兆の後ろの単位 (100万円 / 100万ドル) も組に含める
	sourceNumberRe = regexp.MustCompile(`(?i)([-−]?\d+(?:[.,]\d+)*)(?:\s*[〜～~-]\s*([-−]?\d+(?:[.,]\d+)*))?\s*((?:万|億|兆)?(?:パーセント|年度|か月|ヶ月|カ月|ヵ月|時間|段階|種類|ドル|GB|MB|TB|%|割|年|月|日|人|名|歳|才|個|件|本|回|倍|分|秒|円|つ|位|社|台|行|字|代)|万|億|兆)`)
	thousandsRe    = regexp.MustCompile(`,(\d{3})`)
	countingRe     = regexp.MustCompile(`^\d+つ$`)
	introBoundary  = []string{"本題"}
)

// numberTokens は文の中の「数字と単位の組」を、比べられる形 (全角を半角に・桁区切りのカンマを除く・パーセントを % に) で返す。
func numberTokens(s string) []string {
	var out []string
	num := func(n string) string { // 桁区切りのカンマ (後ろが 3 桁) だけを除き、全角のマイナスを揃える。小数のカンマ (10,5) は残す
		return strings.ReplaceAll(thousandsRe.ReplaceAllString(n, "$1"), "−", "-")
	}
	for _, m := range sourceNumberRe.FindAllStringSubmatch(foldWidth(s), -1) {
		unit := strings.ToUpper(strings.ReplaceAll(m[3], "パーセント", "%"))
		if m[2] != "" {
			out = append(out, num(m[1])+unit, num(m[2])+unit)
			continue
		}
		out = append(out, num(m[1])+unit)
	}
	return out
}

// sourceCandidates は台本の行と資料の文を突き合わせた候補 (すべて目安)。
func sourceCandidates(s *Script, source string) []lintIssue {
	var out []lintIssue
	hint := func(i int, rule, msg string) {
		out = append(out, lintIssue{Line: i, Rule: rule, Msg: msg, Hint: true})
	}
	srcNumbers := map[string]bool{}
	for _, k := range numberTokens(source) {
		srcNumbers[k] = true
	}
	srcFold := foldWidth(source)
	for i, line := range s.Lines {
		// 字幕と、図解の中の文字 (重要語・比較の見出しと項目) を見る (図解の中の誤った数字も素通りさせない)
		parts := []string{pyStr(line["text"])}
		if sh, ok := line["show"].(map[string]any); ok {
			parts = append(parts, showAllTexts(sh)...)
		}
		text := foldWidth(strings.Join(parts, " "))
		seenNum := map[string]bool{}
		for _, key := range numberTokens(text) {
			if seenNum[key] || countingRe.MatchString(key) || srcNumbers[key] {
				continue
			}
			seenNum[key] = true
			hint(i, "source-number", "数字「"+key+"」が資料に無い (資料の外から足した事実か、資料との食い違い)")
		}
		seen := map[string]bool{}
		for _, w := range lineWords(text) {
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
		if pyTruthy(line["chapter"]) {
			chapters = append(chapters, i)
		}
	}
	if len(chapters) >= 2 {
		c := chapters[1]
		var tail strings.Builder
		for j := max(chapters[0], c-3); j <= c; j++ {
			tail.WriteString(pyStr(s.Lines[j]["text"]))
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

func readSource(path string) (string, error) {
	b, err := os.ReadFile(resolvePath(path))
	if err != nil {
		return "", fail("%s: 資料を読めない (%v。--source にはテキストの資料を渡す)", path, err)
	}
	if strings.TrimSpace(string(b)) == "" {
		return "", fail("%s: 資料が空 (台本の数字と語がすべて候補になってしまう)", path)
	}
	return string(b), nil
}

// showAllTexts は図解の中の文字 (重要語の語・補足、比較の見出し・項目)。
func showAllTexts(sh map[string]any) []string {
	out := showTexts(sh)
	if v, ok := sh["text"].(string); ok && pyStr(sh["type"]) == "keyword" {
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
