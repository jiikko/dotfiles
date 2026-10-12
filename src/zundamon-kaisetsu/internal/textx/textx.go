// Package textx は文字列の小さな道具 (表示用の切り詰め・全角英数字の半角化・英字の語の切り出し)。
package textx

import (
	"regexp"
	"strings"
)

func FirstRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func LastRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[len(r)-n:])
	}
	return s
}

var englishWordRe = regexp.MustCompile(`[A-Za-z]+`)

// FoldWidth は全角の英数字・記号を半角にする (全角英字もエンジンが 1 文字ずつ読むことがあるので、検査も同じ語として見る。
// 読みそのものは全角と半角で違うことがある: Ｗｉ－Ｆｉ は ウィエフアイ、Wi-Fi は ワイファイ)。
func FoldWidth(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '！' && r <= '～' {
			return r - '！' + '!'
		}
		return r
	}, s)
}

// LineWords は音声にする文の英字の語 (2 文字以上。全角は半角にして) を、文の中の順に返す。
func LineWords(spoken string) []string {
	var out []string
	for _, w := range englishWordRe.FindAllString(FoldWidth(spoken), -1) {
		if len(w) >= 2 {
			out = append(out, w)
		}
	}
	return out
}
