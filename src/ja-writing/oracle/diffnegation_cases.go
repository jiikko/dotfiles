package oracle

import (
	"math/rand/v2"
	"strconv"
	"strings"
)

// 群 N (否定) と群 O (主語と目的語の入れ替え) の記録: 段 _negation_risks(sides, groups)・_swap_risks(sides, groups) と、その下の関数と定数。
// 組は、群 M までと同じ入れ物 (diffBags) に、専用の手書きの組と固定の種 (PCG 1076/16・17) の組を足したもの。

// diffNegationConstants は群 N の Go の表と比べる本家の定数 (正規表現は文字列と flags)。
var diffNegationConstants = []string{
	"_NEGATION", "_NEGATION_MASKED", "_ADVERB_MAZU", "_NEGATION_LIMIT", "_NEGATION_END_RULE", "_END_WINDOW", "_PLACE_GAP", "_SENTENCE_TAIL", "_HIRAGANA", "_WORD_CHAR",
	"_WIDEN_RIGHT", "_ZU_TO_TE",
}

// diffSwapConstants は群 O の定数。
var diffSwapConstants = []string{"_CASE_WORD", "_HONORIFIC", "_CASE_PARTICLE", "_PASSIVE"}

// DiffNegationFunctions は群 N の関数。DiffSwapFunctions は群 O の関数。
var (
	DiffNegationFunctions = []string{"_negation_risks", "_negation_marks", "_negation_flip", "_changed_places", "_widen", "_covered", "_unmarked_letters", "_verb_stem_change"}
	DiffSwapFunctions     = []string{"_swap_risks", "_case_map", "_passive_rewrite"}
)

// DiffNegationConstantNames・DiffSwapConstantNames は定数の名前 (Go の突き合わせが、この名前の Get を回す)。
func DiffNegationConstantNames() []string { return diffNegationConstants }
func DiffSwapConstantNames() []string     { return diffSwapConstants }

// diffRiskStageCalls は組 1 つを、本家の _build_full_diff (覚える)・_sentence_pairs (覚える) と、fn(sides, groups) に通す呼び出し。
func diffRiskStageCalls(old, new, fn string, keep bool) []Call {
	build := dcall("_build_full_diff", old, new, nil)
	build.Save, build.Quiet = "full", true
	pairs := dcall("_sentence_pairs", diffFullRef(1), diffFullRef(0))
	pairs.Save, pairs.Quiet = "groups", true
	risks := dcall(fn, diffFullRef(1), diffRef("groups"))
	risks.Keep = keep
	return []Call{build, pairs, risks}
}

// diffNegationBank は否定の言い方ごとの文 (組の元と後に使う)。
var diffNegationBank = []string{
	"設定を変更する。", "設定を変更しない。", "設定を変更します。", "設定を変更しません。", "設定を変更しないでください。", "設定を変更してください。", "設定を変更しなかった。", "設定を変更した。", "設定を変更しなければならない。", "設定を変更しなくてはならない。",
	"設定を変更しないといけない。", "設定を変更せざるを得ない。", "設定を変更せねばならない。", "設定を変更するかもしれない。", "設定を変更するに違いない。", "設定を変更するとは限らない。", "設定を変更するわけではない。", "設定を変更するにすぎない。",
	"設定を変更するしかない。", "設定を変更するほかない。", "設定の有無を確認する。", "設定が無効です。", "設定が有効です。", "設定は不要です。", "設定は必要です。", "設定は不可能です。", "設定は可能です。", "設定は未対応です。", "設定は対応済みです。",
	"設定は非公開です。", "設定は公開です。", "設定は無料です。", "設定は有料です。", "設定は無視される。", "設定を使う。", "設定を使わない。", "設定を使わずに進める。", "設定を使って進める。", "設定を使わず、進める。", "設定を使わず進める。",
	"設定を禁止する。", "設定を許可する。", "設定を禁止します。", "問題はない。", "問題がある。", "問題もありません。", "問題はありません。", "問題なく動く。", "問題が起きる。", "特に問題ありません。",
	"必ず確認する。", "まず確認する。", "まず、確認する。", "読まず嫌いをやめる。", "読まず、書く。", "読まずに書く。", "読んで書く。", "はずがない。", "あえず確認する。", "とりあえず確認する。",
	"Aは対応している。", "Aは対応していない。", "Aは対応しているが、Bは対応していない。", "Aは対応していないが、Bは対応している。", "AではなくBを使う。", "ではなくBを使う。", "AでなくBを使う。", "じゃなくBを使う。", "Aでもない。", "Aじゃない。",
	"Aではありません。", "Aじゃありません。", "Aです。", "少ない。", "多い。", "危ない。", "安全です。", "切ない。", "仕方ない。", "間違いない。", "つまらない。", "くだらない。", "もったいない。", "たまらない。", "情けない。", "頼りない。", "あっけない。", "ぎこちない。", "さりげなく話す。", "味気ない。", "素っ気ない。",
	"この変更で直る。", "この変更では直らない。", "この変更で直らない。", "設定する。", "設定しない。", "確認せずに進める。", "確認して進める。", "確認せず進める。", "確認し、進める。", "確認しないで進める。", "確認しないで、進める。", "確認しなくて進める。", "確認せずに。", "確認して。",
	"未来を見る。", "未明に起きる。", "未完成です。", "不明です。", "不正です。", "不足している。", "不在です。", "不備がある。", "不適切です。", "不十分です。", "不一致です。", "不安定です。", "不許可です。", "不採用です。", "不使用です。", "不参加です。", "不具合がある。", "不思議だ。", "不意に起きる。",
	"非公開にする。", "公開する。", "非対応です。", "非表示にする。", "非推奨です。", "非同期で動く。", "非互換です。", "非同意です。", "非常に重要です。",
	"無効にする。", "有効にする。", "無料で使う。", "無視する。", "無関係です。", "無制限です。", "無断で使う。", "無理に使う。", "無事に終わる。",
	"禁止する。", "ぬ。", "知らぬ。", "知らぬ顔をする。", "あえて行わぬ。", "言わぬが花。",
	"変わらない。", "同じだ。", "欠かせない。", "重要だ。", "設定は変わらない。", "設定は同じだ。", "それは欠かせない設定です。", "それは重要な設定です。",
	"動かない。", "動く。", "動かなかった。", "動いた。", "動かず、止まった。", "動かなくなった。", "動かないこともある。", "動かないでください。",
	"使えない。", "使える。", "使えません。", "使えます。", "できない。", "できる。", "できません。", "できます。", "不能な状態です。", "できない状態です。",
	"なくなった。", "ある。", "なかった。", "あった。", "ないと思う。", "あると思う。", "なければ止まる。", "あれば動く。", "なけれど動く。", "なくても動く。",
}

// diffNegationSwaps は否定の言い方を変える置き換え (元の文字列, 変えた文字列)。
var diffNegationSwaps = [][2]string{
	{"しない", "する"}, {"する", "しない"}, {"しません", "します"}, {"します", "しません"}, {"しなかった", "した"}, {"した", "しなかった"}, {"している", "していない"}, {"していない", "している"},
	{"有効", "無効"}, {"無効", "有効"}, {"必要", "不要"}, {"不要", "必要"}, {"可能", "不可能"}, {"できる", "できない"}, {"できない", "できる"}, {"できます", "できません"}, {"できません", "できます"},
	{"使える", "使えない"}, {"使えない", "使える"}, {"動く", "動かない"}, {"動かない", "動く"}, {"ある", "ない"}, {"ない", "ある"}, {"あった", "なかった"}, {"なかった", "あった"},
	{"確認して", "確認しないで"}, {"確認しないで", "確認して"}, {"確認せずに", "確認して"}, {"確認して", "確認せずに"}, {"読まず", "読んで"}, {"読んで", "読まず"}, {"使わず", "使って"}, {"使って", "使わず"},
	{"公開", "非公開"}, {"非公開", "公開"}, {"対応", "非対応"}, {"対応", "未対応"}, {"未対応", "対応済み"}, {"許可", "禁止"}, {"禁止", "許可"}, {"問題はない", "問題がある"}, {"問題がある", "問題はない"},
	{"直る", "直らない"}, {"直らない", "直る"}, {"変わらない", "同じだ"}, {"欠かせない", "重要だ"}, {"同じだ", "変わらない"}, {"重要だ", "欠かせない"}, {"少ない", "多い"}, {"危ない", "安全です"}, {"多い", "少ない"},
	{"変更する", "変更しない"}, {"変更しない", "変更する"}, {"変更しなければならない", "変更する"}, {"変更する", "変更しなければならない"}, {"かもしれない", "に違いない"}, {"ではなくB", "でB"},
	{"。", "ではない。"}, {"です。", "ではありません。"}, {"ではありません。", "です。"}, {"。", "だ。"}, {"だ。", "ではない。"},
}

// diffNegationHandPairs は否定の手書きの組 (名前, 元, 後)。1 つの文の中の 2 つの否定の入れ替え・印の数が同じ言い換え・句点の前の規則・語幹の仮名の違い・長い文を狙う。
func diffNegationHandPairs() []diffFullCase {
	doc := diffDoc
	long := func(n int, tail string) string { return strings.Repeat("あ", n) + tail }
	return []diffFullCase{
		{name: "same", old: doc("設定を変更する。"), new: doc("設定を変更する。")},
		{name: "flip", old: doc("設定を変更する。"), new: doc("設定を変更しない。")},
		{name: "flip-back", old: doc("設定を変更しない。"), new: doc("設定を変更する。")},
		{name: "stem-su-shi", old: doc("設定する。"), new: doc("設定しない。")},
		{name: "stem-zu-te", old: doc("確認せずに進める。"), new: doc("確認して進める。")},
		{name: "stem-sazu", old: doc("確認させずに進める。"), new: doc("確認して進める。")},
		{name: "stem-zu-shi", old: doc("確認せず進める。"), new: doc("確認し進める。")},
		{name: "stem-reverse", old: doc("確認して進める。"), new: doc("確認せずに進める。")},
		{name: "swap-two", old: doc("Aは対応しているが、Bは対応していない。"), new: doc("Aは対応していないが、Bは対応している。")},
		{name: "swap-two-same-direction", old: doc("Aは対応していないが、Bは対応していない。"), new: doc("Aは対応しているが、Bは対応している。")},
		{name: "same-total-rephrase", old: doc("動かないので止める。"), new: doc("動作しないので止める。")},
		{name: "masked-double", old: doc("確認しなければならない。"), new: doc("確認する。")},
		{name: "masked-contrast", old: doc("AではなくBを使う。"), new: doc("Bを使う。")},
		{name: "end-rule", old: doc("この変更で直る。"), new: doc("この変更では直らない。")},
		{name: "end-rule-kango", old: doc("変わらない。"), new: doc("同じだ。")},
		{name: "end-rule-hiragana", old: doc("欠かせない。"), new: doc("重要だ。")},
		{name: "widen-left", old: doc("これは有効です。"), new: doc("これは無効です。")},
		{name: "widen-right", old: doc("設定が有効でございますので確認してください。"), new: doc("設定が無効でございますので確認してください。")},
		{name: "widen-katakana", old: doc("サーバーを使います。"), new: doc("サーバーを使いません。")},
		{name: "mazu", old: doc("まず確認する。"), new: doc("確認する。")},
		{name: "mazu-kanji", old: doc("読まず嫌いをやめる。"), new: doc("読み嫌いをやめる。")},
		{name: "kanazu", old: doc("必ず確認する。"), new: doc("確認する。")},
		{name: "pairs-gap-1", old: doc("あいうえお。"), new: doc("あいうえおない。")},
		{name: "sentence-tail", old: doc("確認する」。"), new: doc("確認しない」。")},
		{name: "table", old: doc("| 項目 | 値 |", "|---|---|", "| 設定 | あり |"), new: doc("| 項目 | 値 |", "|---|---|", "| 設定 | なし |")},
		{name: "long-2000", old: doc(long(1990, "設定する。")), new: doc(long(1990, "設定しない。"))},
		{name: "long-over-limit", old: doc(long(2005, "設定する。")), new: doc(long(2005, "設定しない。"))},
		{name: "long-rest-over", old: doc(long(10, strings.Repeat("い", 2100)+"する。")), new: doc(long(10, strings.Repeat("う", 2100)+"しない。"))},
		{name: "long-diff-in-range", old: doc(long(2100, "設定する。")), new: doc(long(2100, "設定しない。") + "あ")},
		{name: "empty-old", old: "", new: doc("設定しない。")},
		{name: "added-sentence", old: doc("設定する。"), new: doc("設定する。", "確認しない。")},
		{name: "run-shifted", old: doc("一段落目は使う。", "", "二段落目は使う。", "", "三段落目は使う。"), new: doc("一段落目は使わない。二段落目は使う。", "", "三段落目は使わない。")},
		{name: "multi-hunk", old: doc("A は使う。", "同じ行。", "B は使う。"), new: doc("A は使わない。", "同じ行。", "B は使わない。")},
		{name: "crlf", old: "設定を変更する。\r\n確認する。\r\n", new: "設定を変更しない。\r\n確認する。\r\n"},
		{name: "ne-ne", old: doc("確認しないことはない。"), new: doc("確認することはある。")},
		{name: "ka-mo", old: doc("確認するかもしれない。"), new: doc("確認する。")},
		{name: "hazu", old: doc("確認するはずがない。"), new: doc("確認するはずだ。")},
		{name: "zu-ni", old: doc("読まずに書く。"), new: doc("読んで書く。")},
		{name: "nu", old: doc("言わぬが花。"), new: doc("言うが花。")},
		{name: "mi-fu-hi-mu", old: doc("未対応です。不明です。非公開です。無効です。"), new: doc("対応済みです。判明しています。公開です。有効です。")},
	}
}

// diffNegationFuzz は固定の種 (PCG 1076/16) から作った、否定の言い方を変える組。文の並びに文を足す・消す・入れ替える・つなぐ。
func diffNegationFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 16))
	pick := func() string { return diffNegationBank[r.IntN(len(diffNegationBank))] }
	variant := func(s string) string {
		for range 6 {
			sw := diffNegationSwaps[r.IntN(len(diffNegationSwaps))]
			if strings.Contains(s, sw[0]) {
				return strings.Replace(s, sw[0], sw[1], 1)
			}
		}
		return pick()
	}
	var cases []diffFullCase
	for n := range 1800 {
		var lines [][]string
		for range 1 + r.IntN(4) {
			var line []string
			for range 1 + r.IntN(3) {
				line = append(line, pick())
			}
			lines = append(lines, line)
		}
		cur := make([][]string, len(lines))
		for i, l := range lines {
			cur[i] = append([]string{}, l...)
		}
		for ops := 1 + r.IntN(3); ops > 0 && len(cur) > 0; ops-- {
			i := r.IntN(len(cur))
			if len(cur[i]) == 0 {
				continue
			}
			j := r.IntN(len(cur[i]))
			switch r.IntN(7) {
			case 0, 1, 2:
				cur[i][j] = variant(cur[i][j])
			case 3:
				cur[i] = append(cur[i][:j], cur[i][j+1:]...)
			case 4:
				cur[i] = append(cur[i][:j], append([]string{pick()}, cur[i][j:]...)...)
			case 5:
				k := r.IntN(len(cur))
				cur[i], cur[k] = cur[k], cur[i]
			default:
				cur[i][j] = variant(variant(cur[i][j]))
			}
		}
		cases = append(cases, diffFullCase{name: "fuzz-" + strconv.Itoa(n), old: diffFuzzJoin(r, lines), new: diffFuzzJoin(r, cur)})
	}
	return cases
}

// diffNegationSentences は _negation_marks・_negation_flip の関数に通す文 (bank の全文)。
func diffNegationStringCalls() []Call {
	var calls []Call
	add := keepAdder(&calls)
	for _, s := range diffNegationBank {
		add("_negation_marks", s)
	}
	r := rand.New(rand.NewPCG(1076, 18))
	for n := range 600 {
		s := diffNegationBank[r.IntN(len(diffNegationBank))]
		t := s
		for range 3 {
			sw := diffNegationSwaps[r.IntN(len(diffNegationSwaps))]
			if strings.Contains(t, sw[0]) {
				t = strings.Replace(t, sw[0], sw[1], 1)
				break
			}
		}
		_ = n
		add("_negation_flip", s, t)
		add("_changed_places", s, t)
	}
	for _, s := range []string{"あいうえお", "確認しない", "これは有効です。", "サーバーを使います。", "有効", "", "ア", "設定が有効でございますので確認してください。"} {
		n := len([]rune(s))
		for _, lr := range [][2]int{{0, 0}, {0, 1}, {1, 2}, {n - 1, n}, {n / 2, n}, {0, n}, {n, n}, {2, 3}, {3, 5}, {5, 8}} {
			if lr[0] >= 0 && lr[1] <= n && lr[0] <= lr[1] {
				add("_widen", s, lr[0], lr[1])
			}
		}
	}
	marks := [][][]int{{}, {{1, 3}}, {{0, 2}, {4, 6}}, {{2, 5}, {3, 8}}}
	for _, m := range marks {
		for _, lr := range [][2]int{{0, 1}, {1, 4}, {0, 10}, {3, 6}, {5, 5}} {
			add("_covered", lr[0], lr[1], m)
			add("_unmarked_letters", "あ漢字カタカナい漢字ア", lr[0], min(lr[1]+3, 11), m)
		}
	}
	for _, p := range [][2]string{{"す", "し"}, {"し", "す"}, {"する", "しない"}, {"しない", "する"}, {"さずに", "して"}, {"せずに", "し"}, {"さず", "して"}, {"せず", "し"}, {"して", "せずに"}, {"し", "せず"}, {"す", "ない"},
		{"", "し"}, {"す", ""}, {"漢", "し"}, {"す", "漢"}, {"さずに", "さず"}, {"せずに", "せずに"}, {"あ", "い"}, {"すぎる", "しない"}, {"しない", "すぎる"}, {"せず", "して"}} {
		add("_verb_stem_change", p[0], p[1])
	}
	return calls
}

// diffNegationCases は群 N の case の一覧 (pairs は入力の集まりと diff の入力の組)。
func diffNegationCases(pairs [][3]string) []VisCase {
	cases := []VisCase{{Name: "negation/constants", Calls: diffConstantCalls(diffNegationConstants)}}
	for _, b := range diffBags(pairs) {
		cases = append(cases, VisCase{Name: "negation/" + b.name, Calls: diffRiskStageCalls(b.old, b.new, "_negation_risks", false)})
	}
	for _, g := range []struct {
		prefix string
		cases  []diffFullCase
		keep   bool
	}{{"negation/hand/", diffNegationHandPairs(), true}, {"negation/fuzz/", diffNegationFuzz(), false}} {
		for _, f := range g.cases {
			cases = append(cases, VisCase{Name: g.prefix + f.name, Calls: diffRiskStageCalls(f.old, f.new, "_negation_risks", g.keep)})
		}
	}
	return append(cases, VisCase{Name: "negation/strings", Calls: diffNegationStringCalls()})
}
