package oracle

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
)

// 群 R (語の一覧) の記録: parse_list(text, pattern)・_load_list(name)・_list_info(name)。
// parse_list は書き方の誤りで ValueError を投げる (その文面も記録と比べる)ので、例外になる呼び出しがある関数はこれだけ。

// DiffListNames は本家が読む 4 本の一覧 (_LIST_PATTERNS の鍵。_load_list と _list_info が受ける名前)。
var DiffListNames = []string{"nominalization", "intensifiers", "feelings", "hedges"}

// DiffListFunctions は群 R の関数 (Go の突き合わせが、この名前の呼び出しを群 R の関数に回す)。
var DiffListFunctions = []string{"parse_list", "_load_list", "_list_info"}

// diffListTexts は parse_list に通す手書きの文。BOM・CRLF・空行・コメント・タブの数・3 つ目の欄・誤り (正規表現の欄が空) の表示の長さ (40 字ちょうど・41 字) を狙う。
var diffListTexts = []string{
	"", "\n", "\r\n", "\r", "\r\r\n", "あ", "あ\n", "\nあ", "あ\nい", "あ\r\nい\r\n", "あ\rい", "  あ  \n",
	"\ufeffあ\nい", "\ufeff\ufeffあ", "あ\ufeffい", "\ufeff", "\ufeff# コメント\nあ", "\ufeff\tあ",
	"# コメント\nあ", "  # コメント\nあ", "\t# コメント\nあ", "\u3000# 全角空白のあとのコメント\nあ", "あ # 行の途中の # は語の一部", "#\n#あ\n# \nい", "＃全角の井桁\nあ",
	"\u3000\n\u00a0\n\u2028\n\u0085\n\x1c\n\x1f\nあ", "あ\u3000\n\u3000い\u3000", "\u3000あ\u3000\tい\u3000\t\u3000う\u3000",
	"a\tb", "a\tb\n", "a\tb\tc", "a\tb\tc\td", "a\tb\tc\td\te", "a\t\tc", "a\tb\t", "a\tb\t \t", "a\tb\t  c  ", "a\t b \t c ", "\tb", "\tb\tc", "a\t\t", "\t\t", "\t", "a\t", "a\t \n", "a\t\u3000\n",
	"あ\tい\n# c\n\nう\tえ\tお", "あ\tい\r\nう\tえ\r\n", "あ\tい\rう\tえ\r", "あ\tい\r\r\nう", "名前\t正規\t原文\t余り\t余り",
	"a\tb\nc", "a\tb\nc\n\nd\te", "a\tb\n\n\n", "a\tb\n   \n  # c\n\t\n",
	"嬉し\t嬉|うれし", "感謝\t感謝\tありがと|お礼|(?<![失無非])礼を|謝意", "落ち込む\t落ち込(?<!に落ち込)(?<![げ数率量額高要績気益価][がはの]落ち込)",
	// 誤りの文の行の表示は 40 字までで、超えたら 40 字で切って「…」を付ける
	strings.Repeat("あ", 39), strings.Repeat("あ", 40), strings.Repeat("あ", 41), strings.Repeat("あ", 80),
	strings.Repeat("あ", 39) + "\t", strings.Repeat("あ", 40) + "\t", strings.Repeat("あ", 41) + "\t", strings.Repeat("あ", 39) + "\t\t",
	strings.Repeat("a", 38) + "\tb\t", strings.Repeat("a", 40) + "\t \t", strings.Repeat("a", 41) + "\t \t", strings.Repeat("a", 200) + "\t\t\t",
	strings.Repeat("あ", 20) + "\t" + strings.Repeat("い", 20), strings.Repeat("あ", 20) + "\t" + strings.Repeat("い", 21),
	"あ\n" + strings.Repeat("い", 60) + "\t", "\ufeff" + strings.Repeat("い", 60) + "\t", "\u3000" + strings.Repeat("い", 60) + "\t",
	"あ\t😀\t😀", "😀" + strings.Repeat("あ", 39) + "\t", "😀" + strings.Repeat("あ", 38) + "\t", "゙\t゙\t゙", "é\té\té", "\U0001F468\u200d\U0001F469\t\U0001F468\u200d\U0001F469",
	"\v\f\x1d\x1e\x1c\n\u2029\u200b\ufeff\n\u180e",
}

// diffListFuzz は固定の種 (PCG 1076/11) から作った文。行の部品 (語・タブ・空白・BOM・井桁・改行の種類) をつないだもの。
func diffListFuzz() []string {
	r := rand.New(rand.NewPCG(1076, 11))
	parts := []string{"あ", "い", "語", "foo", "(?:x|y)", "\t", "\t", "\t", " ", "\u3000", "#", "\n", "\n", "\r\n", "\r", "\ufeff", "\u00a0", "\u2028", "\u0085", "\x1c", "# c", "ほっと(?=し|す)", strings.Repeat("あ", 20), strings.Repeat("い", 41)}
	out := make([]string, 0, 1200)
	for range 1200 {
		var b strings.Builder
		for range 1 + r.IntN(14) {
			b.WriteString(parts[r.IntN(len(parts))])
		}
		out = append(out, b.String())
	}
	return out
}

// diffListCases は群 R の case (定数の無い関数だけ)。Go が埋め込んだ 8 本の一覧 (lists/data。diffDir の 2 つ上の lists/data) そのものも、両方の読み方で通す。
// lists パッケージを import しないのは、lists が internal/pystr を使い、pystr の試験が oracle を import していて循環するため。
func diffListCases(diffDir string) ([]VisCase, error) {
	call := func(text string, pattern, keep bool) Call {
		c := dcall("parse_list", text, pattern)
		c.Keep = keep
		return c
	}
	var files []Call
	dataDir := filepath.Join(diffDir, "..", "..", "lists", "data")
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dataDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		for _, pattern := range []bool{false, true} {
			// 結果は大きいので記録には digest だけ (本家の一覧のファイルは固定で、Go も同じ中身を埋め込む)
			files = append(files, call(string(data), pattern, false))
		}
	}
	var hand, fuzz []Call
	for _, text := range diffListTexts {
		for _, pattern := range []bool{false, true} {
			hand = append(hand, call(text, pattern, true))
		}
	}
	for _, text := range diffListFuzz() {
		for _, pattern := range []bool{false, true} {
			fuzz = append(fuzz, call(text, pattern, false))
		}
	}
	var loaded []Call
	for _, name := range DiffListNames {
		c := dcall("_load_list", name)
		c.Keep = true
		info := dcall("_list_info", name)
		info.Keep = true
		loaded = append(loaded, c, info)
	}
	return []VisCase{
		{Name: "list/files", Calls: files},
		{Name: "list/hand", Calls: hand},
		{Name: "list/fuzz", Calls: fuzz},
		{Name: "list/load", Calls: loaded},
	}, nil
}
