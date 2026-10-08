package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// 読みの検査 (kana --script --check。issue 673)。
//
// 見るのは「英字の語を、エンジンが 1 文字ずつ読んだ行」だけ (SOLIDWORKS を エスオーエル… と読む形)。
// 止めたいのは、台本を書く側が気づかずに残した誤読 (台本の書き手による意図的な迂回は相手にしない)。
//
// 検出しない形 (人が英字の語の一覧と kana.tsv で見る。SKILL.md の手順 5):
//   - 崩れた読み (SIer を エスイアー)、漢字の読み分け (内製を うちせい)、数字・助数詞の読み
//   - 同じ並びの語が 1 行に 2 つあり、両方とも 1 文字ずつ読まれた形の数え分け (語と読みの位置の対応は取れない。spelledWords)
//   - 略語の一覧にある語が、本当は誤読だった場合 (一覧に足した時点で、その語は 1 文字ずつ読むのが正しいと決めたことになる)
// 一覧の「行の中では別の読み」の印は、目で見る行を挙げるためのもので、付いても誤読とは限らない (Wi-Fi は Wi と Fi に分かれて付く)。
// 検査が通っても、誤読が無いことにはならない。

// acronymsFile は 1 文字ずつ読むのが正しい略語 (CPU・OS など) の一覧。skill の dir に読み辞書と並べて置く。
func (e *Env) acronymsFile() string { return filepath.Join(e.SkillDir, "acronyms.json") }

func loadAcronyms(env *Env) (map[string]bool, error) {
	b, err := os.ReadFile(env.acronymsFile())
	if err != nil {
		return nil, fail("%s: 読めない (%v)", env.acronymsFile(), err)
	}
	var list []string
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fail("%s: 文字列の配列で書く (%v)", env.acronymsFile(), err)
	}
	m := map[string]bool{}
	for _, w := range list {
		m[w] = true
	}
	return m, nil
}

var (
	englishWordRe = regexp.MustCompile(`[A-Za-z]+`)
	// 読みの記号: ' はアクセント、/ と 、 は区切り、_ は無声化。? や ！ は文末の印
	kanaMarksRe = regexp.MustCompile(`['/_、？！?!。]`)
	eRowI       = regexp.MustCompile(`([エケセテネヘメレゲゼデベペェ])イ`)
	oRowU       = regexp.MustCompile(`([オコソトノホモヨロゴゾドボポョォ])ウ`)
)

// foldWidth は全角の英数字・記号を半角にする (全角英字もエンジンが 1 文字ずつ読むことがあるので、検査も同じ語として見る。
// 読みそのものは全角と半角で違うことがある: Ｗｉ－Ｆｉ は ウィエフアイ、Wi-Fi は ワイファイ)。
func foldWidth(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '！' && r <= '～' {
			return r - '！' + '!'
		}
		return r
	}, s)
}

func stripKanaMarks(k string) string { return kanaMarksRe.ReplaceAllString(k, "") }

// longVowelForm は長音の書き方を揃えた形。エンジンは同じ文字名を、単独では「エイ」、文の中では「エエ」と書くことがある (A・K 等)。
//
// 🚨 行の読み全体には当てない。文字名の境目をまたいで置き換わり (A の「エ」+ E の「イイ」の「エイ」)、1 文字ずつ読んだ
// AED・KEY を取りこぼした (issue 673 の敵対的レビュー)。文字名の側に両方の書き方を持たせて照合する。
func longVowelForm(k string) string {
	return oRowU.ReplaceAllString(eRowI.ReplaceAllString(k, "${1}エ"), "${1}オ")
}

// flexibleKana は読み (記号を除いた形) に、長音の 2 通りの書き方 (エイ / エエ、オウ / オオ) のどちらでも当たる正規表現。
// 相手の読みは揃えずにそのまま照合する (揃えると境目をまたいで置き換わる。longVowelForm の注記)。
func flexibleKana(k string) *regexp.Regexp {
	q := regexp.QuoteMeta(stripKanaMarks(k))
	q = eRowI.ReplaceAllString(q, "${1}(?:イ|エ)")
	q = regexp.MustCompile(`([オコソトノホモヨロゴゾドボポョォ])[ウオ]`).ReplaceAllString(q, "${1}(?:ウ|オ)")
	q = regexp.MustCompile(`([エケセテネヘメレゲゼデベペェ])エ`).ReplaceAllString(q, "${1}(?:イ|エ)")
	return regexp.MustCompile(q)
}

// letterAliases はエンジンが文の中で使うことがある、単独の読みとは別の文字名。
var letterAliases = map[byte][]string{'Z': {"ゼット"}, 'H': {"エッチ"}, 'V': {"ヴイ"}}

// letterForms は文字名を、照合に使う書き方の候補 (記号を除いた形と、長音を揃えた形。重複なし) にする。
func letterForms(standalone string, aliases []string) []string {
	var out []string
	for _, n := range append([]string{standalone}, aliases...) {
		n = stripKanaMarks(n)
		for _, f := range []string{n, longVowelForm(n)} {
			if f != "" && !slices.Contains(out, f) {
				out = append(out, f)
			}
		}
	}
	return out
}

// letterNames は A〜Z をエンジンに読ませた文字名の書き方の候補。文字名は話者で変わらない前提で、最初の行の声で 1 回だけ問い合わせる。
func letterNames(env *Env, styleID int64) (map[byte][]string, error) {
	names := map[byte][]string{}
	for c := byte('A'); c <= 'Z'; c++ {
		q, err := audioQuery(env.Engine, string(c), styleID)
		if err != nil {
			return nil, err
		}
		k := queryKana(q)
		if stripKanaMarks(k) == "" {
			return nil, fail("文字 %c の読みがエンジンから取れない", c)
		}
		names[c] = letterForms(k, letterAliases[c])
	}
	return names, nil
}

// spelledPattern は語を 1 文字ずつ読んだときの読み (記号を除いた形) に当たる正規表現。
func spelledPattern(word string, names map[byte][]string) *regexp.Regexp {
	var b strings.Builder
	for i := 0; i < len(word); i++ {
		alts := names[strings.ToUpper(word[i : i+1])[0]]
		quoted := make([]string, len(alts))
		for j, a := range alts {
			quoted[j] = regexp.QuoteMeta(a)
		}
		b.WriteString("(?:" + strings.Join(quoted, "|") + ")")
	}
	return regexp.MustCompile(b.String())
}

// lineWords は音声にする文の英字の語 (2 文字以上。全角は半角にして) を、文の中の順に返す。
func lineWords(spoken string) []string {
	var out []string
	for _, w := range englishWordRe.FindAllString(foldWidth(spoken), -1) {
		if len(w) >= 2 {
			out = append(out, w)
		}
	}
	return out
}

// spelledWords は、音声にする文の英字の語のうち、読みの中で 1 文字ずつ読まれているもの。
//
// 語と読みの位置は対応が取れない (kana は文単位) ので、語ごとに「1 文字ずつ読んだ並び」が読みに現れるかで決める。
// 短い語の並びは長い語の 1 文字読みの途中にも現れる (No の エヌオオ が NOC の エヌオオシイ の中に。IT と it も同じ並び) ので、
// 長い語から先に照合して当たった範囲をその語の取り分にし、短い語は取り分の外で当たったときだけ数える。同じ長さなら略語の一覧の
// 語が先に取る (略語は照合して範囲を取るが、警告はしない)。
//
// 🚨 文の順に、読みの前から照合位置を進める形にしない。短い語が後ろの語の読みの途中に当たると位置が進みすぎ、
// 後ろの本物の 1 文字読み (NOC・SNOWDEN) を黙って取りこぼした (issue 673 の敵対的レビュー 2 周目)。
func spelledWords(spoken, kana string, names map[byte][]string, acronyms map[string]bool) []string {
	k := stripKanaMarks(kana)
	words := lineWords(spoken)
	sort.SliceStable(words, func(a, b int) bool {
		if len(words[a]) != len(words[b]) {
			return len(words[a]) > len(words[b])
		}
		return acronyms[words[a]] && !acronyms[words[b]]
	})
	var taken [][2]int
	overlaps := func(m []int) bool {
		for _, t := range taken {
			if m[0] < t[1] && t[0] < m[1] {
				return true
			}
		}
		return false
	}
	var out []string
	for _, w := range words {
		re := spelledPattern(w, names)
		// 取り分と重なった一致は、その 1 文字後ろから探し直す (FindAll は一致の終わりから次を探すので、「PC、CC」の
		// C + C の一致に続く CC 本体を拾えなかった。敵対的レビュー 3 周目)
		for pos := 0; pos < len(k); {
			loc := re.FindStringIndex(k[pos:])
			if loc == nil {
				break
			}
			m := []int{pos + loc[0], pos + loc[1]}
			if overlaps(m) {
				_, size := utf8.DecodeRuneInString(k[m[0]:])
				pos = m[0] + size
				continue
			}
			taken = append(taken, [2]int{m[0], m[1]})
			if !acronyms[w] && !slices.Contains(out, w) {
				out = append(out, w)
			}
			break
		}
	}
	return out
}

// checkReadings は 1 文字ずつ読まれた英字の語の警告と、英字の語の一覧を出す。警告が 1 つでもあれば rc=1。
func checkReadings(env *Env, lines []lineKana) error {
	type wordInfo struct {
		lines   []int
		styleID int64
	}
	words := map[string]*wordInfo{}
	var order []string
	for i, l := range lines {
		for _, w := range lineWords(l.Spoken) {
			wi, ok := words[w]
			if !ok {
				wi = &wordInfo{styleID: l.StyleID}
				words[w] = wi
				order = append(order, w)
			}
			if !slices.Contains(wi.lines, i) {
				wi.lines = append(wi.lines, i)
			}
		}
	}
	if len(order) == 0 {
		fmt.Fprintln(env.Stdout, "# 英字の語は無い (読みの検査の対象なし)")
		return nil
	}
	acronyms, err := loadAcronyms(env)
	if err != nil {
		return err
	}
	names, err := letterNames(env, lines[0].StyleID)
	if err != nil {
		return err
	}
	fmt.Fprintln(env.Stdout, "# 英字の語を 1 文字ずつ読んだ行 (直す: 台本の readings か行の read)")
	warned := 0
	for i, l := range lines {
		for _, w := range spelledWords(l.Spoken, l.Kana, names, acronyms) {
			fmt.Fprintf(env.Stdout, "%d\t%s\t%s\n", i, w, l.Kana)
			warned++
		}
	}
	if warned == 0 {
		fmt.Fprintln(env.Stdout, "(なし)")
	}
	fmt.Fprintln(env.Stdout, "# 英字の語の一覧 (語だけを読ませた読み。崩れた読みが無いか目で見る。「行の中では別の読み」の行は kana.tsv でその行を見る)")
	sort.SliceStable(order, func(a, b int) bool { return len(words[order[a]].lines) > len(words[order[b]].lines) })
	for _, w := range order {
		q, err := audioQuery(env.Engine, w, words[w].styleID)
		if err != nil {
			return err
		}
		alone := queryKana(q)
		// 語だけの読みが行の読みに現れない行は、行の中では別の読みになっている (Next.js の js、1 文字ずつ読んだ IDOR)
		re := flexibleKana(alone)
		var differ []string
		for _, i := range words[w].lines {
			if !re.MatchString(stripKanaMarks(lines[i].Kana)) {
				differ = append(differ, fmt.Sprint(i))
			}
		}
		note := ""
		if acronyms[w] {
			note += "\t(略語)"
		}
		if len(differ) > 0 {
			note += "\t(行の中では別の読み: " + strings.Join(differ, ", ") + " 行目)"
		}
		fmt.Fprintf(env.Stdout, "%s\t%d 行\t%s%s\n", w, len(words[w].lines), alone, note)
	}
	if warned > 0 {
		return fail("英字の語を 1 文字ずつ読んだ行が %d 件ある (上の一覧)。誤読でなければ %s に足す", warned, env.acronymsFile())
	}
	return nil
}
