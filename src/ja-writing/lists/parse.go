package lists

import (
	"strings"

	"ja-writing/internal/pystr"
)

// bom は先頭の U+FEFF (Windows のメモ帳の「UTF-8 (BOM 付き)」)。
const bom = "\xef\xbb\xbf"

// Lines は一覧のファイルの中身から、読む行を取り出す。本家の yomiyasu_diff.parse_list と yomiyasu_lint._v220_parse_list が同じ読み方をする所で、
// 先頭の BOM を外し、"\n" で分け、行末の "\r" を全部落とし、空白だけの行と「#」で始まる行を読み飛ばす。
// 行の中身 (タブで分ける・strip する) は一覧ごとに違うので、呼ぶ側が読む。
func Lines(text string) []string {
	text = strings.TrimPrefix(text, bom)
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if pystr.Strip(line) == "" || strings.HasPrefix(pystr.LStrip(line), "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// Words は語だけの一覧 (1 行に 1 つ) の語。本家の parse_list の pattern が偽の読み。
func Words(text string) []string {
	lines := Lines(text)
	words := make([]string, len(lines))
	for i, line := range lines {
		words[i] = pystr.Strip(line)
	}
	return words
}
