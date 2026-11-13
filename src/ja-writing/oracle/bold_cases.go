package oracle

import (
	"fmt"
	"strings"
)

// BoldText は太字の記録に足す入力 1 つ。
type BoldText struct {
	Name string
	Text string
}

// boldUnits は崩れた太字の単位。かっこ・句読点・空白の 3 通りの案と、案を確かめる各ガード (escape・コード・URL・リンク・画像・行頭) が当たる形。
var boldUnits = []BoldText{
	{"bracket", "これは **「重要」**です。"},
	{"bracket-ascii", "これは**(重要)**です。"},
	{"punct", "これは**重要。**です"},
	{"punct-bang", "これは**重要！**です"},
	{"space", "これは** 重要**です"},
	{"space-right", "これは**重要 **です"},
	{"escaped", "これは\\**「重要」**です"},
	{"escaped-close", "これは**「重要\\」**です"},
	{"code", "これは`**「重要」**`です"},
	{"url", "見る https://example.com/**「x」**です"},
	{"www", "見る www.example.com/**「x」**です"},
	{"link", "[**「重要」**](https://example.com)です"},
	{"link-dest", "[a]**(x)**です"},
	{"image", "!**[x]**です"},
	{"image-punct", "これは**重要!**[x]"},
	{"before-link", "**[x]**(y)です"},
	{"html-entity", "これは**&amp;「x」**です"},
	// 行き先を読み損ねる「](」(閉じない・空白を含む・入れ子の括弧)。近道はこれより後ろの案を確かめない (本家 2495・2594 行)
	{"link-unclosed", "x [a](これは**「重要」**です"},
	{"link-space", "x [a](b これは**「重要」**です)"},
	{"link-nested", "x [a](b(c これは**「重要」**です"},
	{"link-unclosed-punct", "x [a](これは**重要。**です"},
}

// boldSymbols は GFM と CommonMark で答えが分かれる入力。ASCII の記号は Unicode の P と S にちょうど重なるので、分かれるのは ASCII の外の S の字だけ。
var boldSymbols = []BoldText{
	{"sym-star", "**★重要**です"},
	{"sym-arrow", "**→次**へ"},
	{"sym-yen", "**¥100**円"},
	{"sym-copy", "**©表示**の"},
	{"sym-bracket", "これは**「★」**だ"},
	{"sym-close", "これは**重要★**だ"},
	{"sym-both", "★**★重要★**★"},
	// ASCII の S の字 ($ + < = > ^ ` | ~) は、GFM では ASCII の記号として、CommonMark では S として、どちらでも句読点になる。
	// 左右の字が句読点のときだけ開ける・閉じられる形で、ASCII の記号の扱いを外した GFM と答えが分かれる
	{"ascii-s-dollar", "$**「x」**$"},
	{"ascii-s-plus", "a+**「x」**+b"},
	{"ascii-s-eq", "a=**「x」**=b"},
	{"ascii-s-pipe", "|**「x」**|"},
	{"ascii-s-tilde", "~**「x」**~"},
}

// boldMixed は長さの違う * の印を混ぜた入力 (長さ 2 の印だけでは 3 の倍数の規則が答えに出ないため)。
var boldMixed = []BoldText{
	{"mixed-1", " *a *b **「。x」**→ y*z w** "},
	{"mixed-2", "a***「x」***b"},
	{"mixed-3", "a****「x」****b"},
	{"mixed-4", "*a **「b」** c*"},
	{"mixed-5", "**a*「b」*c**d"},
	{"mixed-6", "***a** b*「c」*"},
	{"mixed-7", "x*「a」**b**「c」*y"},
	{"mixed-8", "*a**「b」**c*d**「e」**"},
	{"mixed-9", "***「a」*b**「c」**"},
	{"mixed-10", "a**b*「c」***d"},
}

// boldEdges は端の入力 (空・印だけ・文の端の印)。
var boldEdges = []BoldText{
	{"empty", ""},
	{"stars-2", "**"},
	{"stars-4", "****"},
	{"stars-space", "** **"},
	{"tail", "a**"},
	{"head", "**a"},
	{"head-bracket", "**「a」**"},
	{"only-bracket", "**「」**"},
	{"long", "これは**「" + strings.Repeat("長い", 20) + "」**です"},
	{"newline-post", "これは**「重要」**で\nつづき"},
	{"frontmatter", "---\ntitle: **「x」**です\n---\n本文の**「y」**です\n"},
	// 近道 (decide) の枝を通す入力 (枝は本家の行番号): 中身の頭の空白を飛ばす 2521 行・
	// 中身の頭が ** で行頭が保たれる 2523 行・句読点の案で「!」の後に「[」が来る 2554 行・開けない / 閉じられないで False を返す 2619 行
	{"decide-lead-space", " ** a  」!\n\t#  1. !**あ  "},
	{"decide-lead-stars", "1. **#。**a>  **  "},
	{"decide-bang-bracket", "**\t」 > 。!」\\あ!**[。\t"},
	{"decide-cannot-open", "**  [#)。**["},
}

// BoldExtraTexts は入力の集まりに足す入力。近道 (_BoldRepairIndex.decide) は、印が 3 つ以上の段落で、解析し直した案が 2 つを超えたときにだけ通るので、
// 本家のテスト (tests/test_v202_diff_bold_performance.py の 74・92 行) と同じ組み方で単位を並べた段落を作る。
func BoldExtraTexts() []BoldText {
	var out []BoldText
	add := func(name, text string) { out = append(out, BoldText{name, text}) }
	singles := append(append(append(append([]BoldText{}, boldUnits...), boldSymbols...), boldMixed...), boldEdges...)
	for _, u := range singles {
		add(u.Name, u.Text)
	}
	for _, u := range append(append([]BoldText{}, boldUnits...), boldSymbols...) {
		add(u.Name+"*4", strings.Repeat(u.Text, 4))
		add(u.Name+"*8", strings.Repeat(u.Text, 8))
		for _, count := range []int{3, 5} {
			add(fmt.Sprintf("%s*%d-paras", u.Name, count), strings.Join([]string{strings.Repeat(u.Text, count), strings.Repeat(u.Text, count), strings.Repeat(u.Text, count)}, "\n\n"))
		}
		// 近道を使わない枝 (本家 2464-2470 行): 「<」を含む段落・先頭行が ` {0,3}[` で始まる段落・リスト・引用・見出し
		add(u.Name+"*4-lt", "a < b "+strings.Repeat(u.Text, 4))
		add(u.Name+"*4-ref", "[x] "+strings.Repeat(u.Text, 4))
		add(u.Name+"*4-list", "- "+strings.Repeat(u.Text, 4))
		add(u.Name+"*4-quote", "> "+strings.Repeat(u.Text, 4))
		add(u.Name+"*4-heading", "# "+strings.Repeat(u.Text, 4))
		add(u.Name+"*4-listref", "- [x] "+strings.Repeat(u.Text, 4))
	}
	var all []string
	for _, u := range boldUnits {
		all = append(all, u.Text)
	}
	add("units-joined", strings.Join(all, ""))
	add("units-lines", strings.Join(all, "\n"))
	add("units-joined*2", strings.Repeat(strings.Join(all, ""), 2))
	var syms []string
	for _, u := range boldSymbols {
		syms = append(syms, u.Text)
	}
	add("symbols-joined", strings.Join(syms, ""))
	var mixed []string
	for _, u := range boldMixed {
		mixed = append(mixed, u.Text)
	}
	add("mixed-joined", strings.Join(mixed, " "))
	add("mixed-lines", strings.Join(mixed, "\n"))
	add("mixed-with-units", strings.Join(mixed, " ")+strings.Join(all, ""))
	// 行頭が ** の行を並べた段落とリスト (行頭の印を読む処理が ** で止まるかの枝)
	add("line-head", strings.Repeat("**「x」**です\n", 4))
	add("line-head-list", strings.Repeat("- **「x」**です\n", 4))
	add("line-head-quote", strings.Repeat("> **「x」**です\n", 4))
	add("line-head-ordered", strings.Repeat("1. **重要。**です\n", 4))
	add("line-head-space", strings.Repeat("** 重要**です\n", 4))
	return out
}
