package oracle

import (
	"math/rand/v2"
	"strconv"
)

// 群 T (気持ちの創作) の記録: 段 _feeling_risks(sides, groups)。語の一覧は群 R の記録が持つ。
// 組は、群 M までと同じ入れ物 (diffBags) に、専用の手書きの組と固定の種 (PCG 1076/23) の組を足したもの。

// DiffFeelingFunctions は群 T の関数。
var DiffFeelingFunctions = []string{"_feeling_risks"}

// diffFeelingBank は気持ちの語の家族ごとに、当たる文と、当たらない文 (正規表現の先読み・後読みで外す形) を並べたもの。
var diffFeelingBank = []string{
	"ほっとしました。", "ほっと一息つく。", "ほっとひと息つく。", "ほっとする。", "ほっとで終わる。", "ほっとけない。", "ほっとくと壊れる。", "安堵しました。", "安心しました。", "安心です。", "肩の荷が下りました。", "胸をなで下ろしました。",
	"嬉しいです。", "うれしかったです。", "喜んでいます。", "喜びを感じます。", "喜ばれる設計です。", "よろこんだ。", "楽しいです。", "たのしかった。", "面白いです。", "おもしろいと思う。", "悔しいです。", "くやしかった。",
	"残念です。", "悲しいです。", "かなしかった。", "寂しいです。", "淋しい。", "さびしい。", "さみしかった。", "がっかりしました。", "落胆しました。", "驚きました。", "驚いた。", "驚く。", "おどろいた。", "びっくりしました。", "驚異的な速さです。",
	"不安です。", "不安を感じる。", "不安定です。", "不安定な動作です。", "心配です。", "心配しました。", "心配りをする。", "心配はありません。", "心配ありません。", "心配もありません。", "心配いりません。", "心配ない。", "心配要りません。", "ご心配は要りません。", "心配には及びません。", "心配は及ばない。", "心配無用です。", "心配なく使えます。",
	"怖いです。", "怖かった。", "怖くなる。", "恐ろしい。", "こわい。", "こわかった。", "こわくなる。", "こわくしない。", "こわくずれる。", "こわくれる。", "焦っています。", "焦りがある。", "焦る。", "あせっている。", "あせりを感じる。",
	"恥ずかしいです。", "はずかしかった。", "誇らしいです。", "ほこらしかった。", "感動しました。", "感激しました。", "もやもやします。", "モヤモヤする。", "いらいらする。", "イライラする。", "苛立ちを覚える。", "うんざりです。",
	"わくわくします。", "ワクワクする。", "どきどきする。", "ドキドキします。", "満足しました。", "満足です。", "満足度が高い。", "要件を満足する場合。", "条件を満足するとき。", "条件を満足すること。", "満足するものを選ぶ。", "条件を満足する。",
	"感謝します。", "ありがとうございます。", "お礼を申し上げます。", "礼を言う。", "失礼を承知で書く。", "無礼を許す。", "非礼を詫びる。", "謝意を表します。", "ありがたいです。", "有難い。", "有り難い。",
	"申し訳ありません。", "すみません。", "すいません。", "お詫び申し上げます。", "おわびします。", "お詫びと訂正。", "ごめんなさい。", "陳謝します。", "謝罪します。", "幸せです。", "しあわせです。", "腹が立ちました。", "怒っています。", "怒りを覚える。", "腹立たしい。",
	"後悔しています。", "戸惑いました。", "とまどった。", "懐かしいです。", "なつかしい。", "憂鬱です。", "ゆううつだ。", "落ち込みました。", "売上が落ち込んだ。", "利益が落ち込んだ。", "気が落ち込む。", "谷に落ち込む。", "気持ちが落ち込んでいる。",
	"つらいです。", "つらかった。", "つらくなる。", "切ないです。", "せつなかった。", "涙が出ました。", "緊張しました。", "緊張感のある場面。", "緊張状態が続く。", "緊張関係にある。", "苦しいです。", "苦しかった。", "苦しくなる。", "苦しさがある。", "苦しみを感じる。", "苦しそうだ。",
	"見苦しい。", "堅苦しい。", "重苦しい。", "暑苦しい。", "恐怖を感じる。", "嫌な気がする。", "嫌な予感がする。", "嫌だった。", "嫌気がさす。", "嫌になった。", "愛おしいです。", "いとおしかった。", "心細いです。", "情けないです。", "むなしいです。", "虚しい。",
	"胸が痛みます。", "胸が熱くなる。", "胸が締め付けられる。", "胸が躍る。", "胸が高鳴る。", "胸が詰まる。", "胸がいっぱいです。", "胸が大きい。",
	"設定を開きます。", "結果は3件でした。", "確認します。",
}

// diffFeelingFamilies は 3 つ目の欄 (原文の言い方) を持つ家族ごとの、原文の言い方と、同じ気持ちの推敲後の言い方。
var diffFeelingFamilies = [][2]string{
	{"ありがとうございます。", "感謝します。"}, {"お礼を申し上げます。", "感謝しております。"}, {"謝意を表します。", "感謝いたします。"},
	{"お詫び申し上げます。", "申し訳ありません。"}, {"すみません。", "申し訳ございません。"}, {"ごめんなさい。", "申し訳ない。"}, {"謝罪します。", "申し訳なく思います。"},
	{"嬉しいです。", "うれしいです。"}, {"安心しました。", "安心です。"}, {"心配です。", "心配しました。"},
}

func diffFeelingHandPairs() []diffFullCase {
	doc := diffDoc
	return []diffFullCase{
		{name: "same", old: doc("ほっとしました。"), new: doc("ほっとしました。")},
		{name: "added-line", old: doc("設定を開きます。"), new: doc("設定を開きます。", "ほっとしました。")},
		{name: "replaced", old: doc("設定を開きます。"), new: doc("ほっとしました。")},
		{name: "family-in-original", old: doc("ほっとして、設定を開きます。"), new: doc("ほっとしました。設定を開きます。")},
		{name: "family-in-original-other-line", old: doc("ほっとしました。", "設定を開きます。"), new: doc("設定を開きます。", "ほっとして休みます。")},
		{name: "family-in-table-original", old: doc("| a | b |", "|---|---|", "| ほっとした | x |"), new: doc("ほっとしました。")},
		{name: "family-in-table-new", old: doc("設定を開きます。"), new: doc("| a | b |", "|---|---|", "| ほっとした | x |")},
		{name: "family-in-code-original", old: doc("```", "ほっとした", "```", "設定を開きます。"), new: doc("設定を開きます。", "ほっとしました。")},
		{name: "family-in-code-new", old: doc("設定を開きます。"), new: doc("設定を開きます。", "```", "ほっとした", "```")},
		{name: "family-in-frontmatter-original", old: doc("---", "title: ほっと", "---", "設定を開きます。"), new: doc("---", "title: ほっと", "---", "ほっとしました。")},
		{name: "family-in-html-original", old: doc("<div>", "ほっとした", "</div>", "設定を開きます。"), new: doc("設定を開きます。", "ほっとしました。")},
		{name: "family-in-inline-code-original", old: doc("`ほっとした` を使う。"), new: doc("ほっとしました。")},
		{name: "family-in-inline-code-new", old: doc("設定を開きます。"), new: doc("設定を開きます。`ほっとした` を使う。")},
		{name: "family-in-link-new", old: doc("設定を開きます。"), new: doc("設定を開きます。[ほっとした](https://a.example/)")},
		{name: "same-sentence-masked-in-original", old: doc("a `a", "嬉しかった。`"), new: doc("a `a", "嬉しかった。")},
		{name: "two-families", old: doc("設定を開きます。"), new: doc("ほっとして、嬉しいです。")},
		{name: "same-family-twice", old: doc("設定を開きます。"), new: doc("ほっとして、ほっとしました。")},
		{name: "two-words-same-family", old: doc("設定を開きます。"), new: doc("嬉しいです。うれしいです。")},
		{name: "heading", old: doc("# 設定"), new: doc("# ほっとした設定")},
		{name: "list", old: doc("- 設定を開きます。"), new: doc("- 設定を開きます。", "- 嬉しいです。")},
		{name: "quote", old: doc("> 設定を開きます。"), new: doc("> 嬉しいです。")},
		{name: "run-shifted", old: doc("設定を開きます。", "", "確認します。"), new: doc("設定を開きます。嬉しいです。", "", "確認します。")},
		{name: "run-second-hunk-merged", old: doc("設定を開きます。", "", "確認します。", "", "保存します。"), new: doc("設定を開きます。確認します。", "", "嬉しいです。保存します。")},
		{name: "run-second-hunk-tail", old: doc("設定を開きます。", "", "確認します。", "", "保存します。"), new: doc("設定を開きます。確認します。", "", "保存します。嬉しいです。")},
		{name: "join-lookbehind", old: doc("売上が", "落ち込んだ。"), new: doc("売上が", "落ち込みました。")},
		{name: "multi-hunk", old: doc("設定を開きます。", "同じ行です。", "確認します。"), new: doc("嬉しいです。", "同じ行です。", "悲しいです。")},
		{name: "old-sentence-same-text", old: doc("嬉しいです。"), new: doc("設定を開きます。", "嬉しいです。")},
		{name: "empty-old", old: "", new: doc("嬉しいです。")},
		{name: "empty-new", old: doc("嬉しいです。"), new: ""},
		{name: "crlf", old: "設定を開きます。\r\n", new: "嬉しいです。\r\n"},
	}
}

// diffFeelingFuzz は固定の種 (PCG 1076/23) から作った、気持ちの語の家族・原文の言い方・行の種類を組み合わせた組。
func diffFeelingFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 23))
	wrap := []string{"{s}", "- {s}", "## {s}", "> {s}", "| {s} | x |", "`{s}`", "[{s}](https://a.example/)", "前の文です。{s}"}
	var cases []diffFullCase
	for n := range 900 {
		var old, new []string
		count := 1 + r.IntN(3)
		for range count {
			w := randPick(r, wrap)
			o, nw := randPick(r, diffFeelingBank), randPick(r, diffFeelingBank)
			if r.IntN(4) == 0 {
				f := diffFeelingFamilies[r.IntN(len(diffFeelingFamilies))]
				o, nw = f[0], f[1]
			}
			old, new = append(old, diffTemplate(w, "s", o)), append(new, diffTemplate(w, "s", nw))
		}
		if r.IntN(3) == 0 {
			// 原文のほかの行にだけ気持ちの語がある
			old = append(old, diffTemplate(randPick(r, wrap), "s", randPick(r, diffFeelingBank)))
		}
		cases = append(cases, diffFullCase{name: "fuzz-" + strconv.Itoa(n), old: diffDoc(old...), new: diffDoc(new...)})
	}
	return cases
}

func diffFeelingBankCases() []diffFullCase {
	var cases []diffFullCase
	for i, s := range diffFeelingBank {
		cases = append(cases, diffFullCase{name: "bank-added-" + strconv.Itoa(i), old: diffDoc("設定を開きます。", "確認します。"), new: diffDoc("設定を開きます。", s)},
			diffFullCase{name: "bank-line-" + strconv.Itoa(i), old: diffDoc("設定を開きます。"), new: diffDoc("設定を開きます。", s)})
	}
	for i, f := range diffFeelingFamilies {
		cases = append(cases, diffFullCase{name: "family-" + strconv.Itoa(i), old: diffDoc(f[0], "設定を開きます。"), new: diffDoc(f[1], "設定を開きます。")},
			diffFullCase{name: "family-other-line-" + strconv.Itoa(i), old: diffDoc(f[0], "設定を開きます。"), new: diffDoc("設定を開きます。", f[1])})
	}
	return cases
}

// diffFeelingCases は群 T の case の一覧 (pairs は入力の集まりと diff の入力の組)。
func diffFeelingCases(pairs [][3]string) []VisCase {
	var cases []VisCase
	for _, b := range diffBags(pairs) {
		cases = append(cases, VisCase{Name: "feeling/" + b.name, Calls: diffRiskStageCalls(b.old, b.new, "_feeling_risks", false)})
	}
	for _, g := range []struct {
		prefix string
		cases  []diffFullCase
		keep   bool
	}{{"feeling/hand/", diffFeelingHandPairs(), true}, {"feeling/bank/", diffFeelingBankCases(), false}, {"feeling/fuzz/", diffFeelingFuzz(), false}} {
		for _, f := range g.cases {
			cases = append(cases, VisCase{Name: g.prefix + f.name, Calls: diffRiskStageCalls(f.old, f.new, "_feeling_risks", g.keep)})
		}
	}
	return cases
}
