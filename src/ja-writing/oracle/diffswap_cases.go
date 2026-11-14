package oracle

import (
	"math/rand/v2"
	"strconv"
)

// 群 O (主語と目的語の入れ替え) の記録: 段 _swap_risks(sides, groups) と _case_map・_passive_rewrite。

var diffSwapWords = []string{"管理者", "利用者", "担当者", "顧客", "システム", "サーバー", "田中さん", "佐藤様", "山田氏", "仕様", "同様", "彼氏", "データ", "ファイル", "田中くん", "花子ちゃん", "山田様", "ABC", "A1", "Ａ１", "設計・実装", "サーバーサイドの設定ファイル"}

var diffSwapVerbs = []string{"送る", "確認する", "見る", "渡す", "呼ぶ", "影響する", "影響される", "依頼された", "承認される", "選ばれました", "知られない"}

var diffSwapParticles = [][2]string{{"が", "を"}, {"が", "に"}, {"は", "を"}, {"から", "へ"}, {"より", "に"}, {"が", "は"}, {"を", "に"}, {"へ", "から"}, {"に", "より"}, {"は", "が"}, {"と", "が"}, {"で", "を"}}

// diffSwapSentence は「A p B q 動詞。」。
func diffSwapSentence(a, p, b, q, verb string) string { return a + p + b + q + verb + "。" }

func diffSwapHandPairs() []diffFullCase {
	doc := diffDoc
	return []diffFullCase{
		{name: "same", old: doc("管理者が利用者を呼ぶ。"), new: doc("管理者が利用者を呼ぶ。")},
		{name: "swap", old: doc("管理者が利用者を呼ぶ。"), new: doc("利用者が管理者を呼ぶ。")},
		{name: "swap-ni", old: doc("管理者が利用者に渡す。"), new: doc("利用者が管理者に渡す。")},
		{name: "no-swap-same-particle", old: doc("管理者が利用者が呼ぶ。"), new: doc("利用者が管理者が呼ぶ。")},
		{name: "passive", old: doc("この変更がユーザーに影響する。"), new: doc("ユーザーがこの変更に影響される。")},
		{name: "passive-wo", old: doc("管理者が利用者を呼ぶ。"), new: doc("利用者が管理者に呼ばれる。")},
		{name: "passive-other-pair", old: doc("管理者を利用者に渡す。"), new: doc("利用者を管理者に渡される。")},
		{name: "honorific-san", old: doc("田中さんが佐藤さんを呼ぶ。"), new: doc("佐藤さんが田中さんを呼ぶ。")},
		{name: "honorific-sama", old: doc("田中様が佐藤氏を呼ぶ。"), new: doc("佐藤氏が田中様を呼ぶ。")},
		{name: "short-sama", old: doc("仕様が同様を呼ぶ。"), new: doc("同様が仕様を呼ぶ。")},
		{name: "kare-shi", old: doc("彼氏が仕様を呼ぶ。"), new: doc("仕様が彼氏を呼ぶ。")},
		{name: "kara-he", old: doc("東京から大阪へ送る。"), new: doc("大阪から東京へ送る。")},
		{name: "kara-he-swap", old: doc("東京から大阪へ送る。"), new: doc("大阪へ東京から送る。")},
		{name: "yori", old: doc("AよりBに寄せる。"), new: doc("BよりAに寄せる。")},
		{name: "three-words", old: doc("管理者が利用者を担当者に渡す。"), new: doc("担当者が管理者を利用者に渡す。")},
		{name: "twelve-chars", old: doc("あいうえおかきくけこさしが利用者を呼ぶ。"), new: doc("利用者があいうえおかきくけこさしを呼ぶ。")},
		{name: "over-twelve", old: doc("あいうえおかきくけこさしすが利用者を呼ぶ。"), new: doc("利用者があいうえおかきくけこさしすを呼ぶ。")},
		{name: "duplicate-word", old: doc("管理者が管理者を呼ぶ。"), new: doc("管理者を管理者が呼ぶ。")},
		{name: "word-with-slash", old: doc("設計・実装が確認を呼ぶ。"), new: doc("確認が設計・実装を呼ぶ。")},
		{name: "table", old: doc("| 項目 | 値 |", "|---|---|", "| 管理者が | 利用者を |"), new: doc("| 項目 | 値 |", "|---|---|", "| 利用者が | 管理者を |")},
		{name: "multi-hunk", old: doc("管理者が利用者を呼ぶ。", "同じ行。", "顧客が担当者を見る。"), new: doc("利用者が管理者を呼ぶ。", "同じ行。", "担当者が顧客を見る。")},
		{name: "run-shifted", old: doc("管理者が利用者を呼ぶ。", "", "顧客が担当者を見る。", "", "田中さんが佐藤様を送る。"), new: doc("利用者が管理者を呼ぶ。顧客が担当者を見る。", "", "佐藤様が田中さんを送る。")},
		{name: "empty-old", old: "", new: doc("利用者が管理者を呼ぶ。")},
		{name: "crlf", old: "管理者が利用者を呼ぶ。\r\n", new: "利用者が管理者を呼ぶ。\r\n"},
	}
}

// diffSwapFuzz は固定の種 (PCG 1076/17) から作った、2 つの語の助詞を入れ替える組と、入れ替えない言い換え。
func diffSwapFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 17))
	var cases []diffFullCase
	for n := range 900 {
		a, b := diffSwapWords[r.IntN(len(diffSwapWords))], diffSwapWords[r.IntN(len(diffSwapWords))]
		pp := diffSwapParticles[r.IntN(len(diffSwapParticles))]
		verb := diffSwapVerbs[r.IntN(len(diffSwapVerbs))]
		old := diffSwapSentence(a, pp[0], b, pp[1], verb)
		var rewrite string
		switch r.IntN(6) {
		case 0, 1, 2:
			rewrite = diffSwapSentence(b, pp[0], a, pp[1], verb)
		case 3:
			rewrite = diffSwapSentence(b, pp[0], a, pp[1], diffSwapVerbs[r.IntN(len(diffSwapVerbs))])
		case 4:
			rewrite = diffSwapSentence(a, pp[1], b, pp[0], verb)
		default:
			// 3 つ目の語が入る・助詞が 1 つだけ入れ替わる
			c := diffSwapWords[r.IntN(len(diffSwapWords))]
			pq := diffSwapParticles[r.IntN(len(diffSwapParticles))]
			old = a + pp[0] + b + pp[1] + c + pq[0] + verb + "。"
			rewrite = c + pp[0] + b + pp[1] + a + pq[0] + verb + "。"
		}
		lines := []string{old}
		if r.IntN(3) == 0 {
			lines = append(lines, "同じ行です。")
		}
		cases = append(cases, diffFullCase{name: "fuzz-" + strconv.Itoa(n), old: diffDoc(lines...), new: diffDoc(append([]string{rewrite}, lines[1:]...)...)})
	}
	return cases
}

func diffSwapStringCalls() []Call {
	var calls []Call
	add := keepAdder(&calls)
	for _, a := range diffSwapWords {
		for _, p := range diffSwapParticles {
			add("_case_map", diffSwapSentence(a, p[0], diffSwapWords[0], p[1], diffSwapVerbs[0]))
		}
	}
	for _, s := range []string{"", "漢字", "管理者が", "管理者がが", "管理者さんが", "管理者さんくんが", "管理者様が", "管理者氏を", "管理者様氏が", "仕様が", "彼氏が", "Aから", "Aより", "Aへ", "Aと", "A　が", "A が", "Aがを", "Aがんば"} {
		add("_case_map", s)
	}
	for _, c := range []struct {
		old, new string
		before   map[string]any
		after    map[string]any
		words    []any
	}{
		{"管理者がユーザーに影響する。", "ユーザーが管理者に影響される。", map[string]any{"管理者": "が", "ユーザー": "に"}, map[string]any{"管理者": "に", "ユーザー": "が"}, []any{"管理者", "ユーザー"}},
		{"管理者がユーザーに影響される。", "ユーザーが管理者に影響する。", map[string]any{"管理者": "が", "ユーザー": "に"}, map[string]any{"管理者": "に", "ユーザー": "が"}, []any{"管理者", "ユーザー"}},
		{"管理者がユーザーに影響する。", "ユーザーが管理者に影響する。", map[string]any{"管理者": "が", "ユーザー": "に"}, map[string]any{"管理者": "に", "ユーザー": "が"}, []any{"管理者", "ユーザー"}},
		{"AがBを呼ぶ。", "BがAに呼ばれる。", map[string]any{"A": "が", "B": "を"}, map[string]any{"A": "に", "B": "が"}, []any{"A", "B"}},
		{"AがBに送る。", "BがAに送られる。", map[string]any{"A": "が", "B": "に"}, map[string]any{"A": "に", "B": "が"}, []any{"A", "B"}},
		{"AがBに送る。", "BがAに送られる。", map[string]any{"A": "が", "B": "に"}, map[string]any{"A": "に", "B": "が"}, []any{}},
		{"Aが呼ぶ。", "Bが呼ぶ。", map[string]any{"A": "が"}, map[string]any{"A": "が"}, []any{"A"}},
	} {
		add("_passive_rewrite", c.old, c.new, c.before, c.after, c.words)
	}
	return calls
}

func diffSwapCases(pairs [][3]string) []VisCase {
	cases := []VisCase{{Name: "swap/constants", Calls: diffConstantCalls(diffSwapConstants)}}
	for _, b := range diffBags(pairs) {
		cases = append(cases, VisCase{Name: "swap/" + b.name, Calls: diffRiskStageCalls(b.old, b.new, "_swap_risks", false)})
	}
	for _, g := range []struct {
		prefix string
		cases  []diffFullCase
		keep   bool
	}{{"swap/hand/", diffSwapHandPairs(), true}, {"swap/fuzz/", diffSwapFuzz(), false}} {
		for _, f := range g.cases {
			cases = append(cases, VisCase{Name: g.prefix + f.name, Calls: diffRiskStageCalls(f.old, f.new, "_swap_risks", g.keep)})
		}
	}
	return append(cases, VisCase{Name: "swap/strings", Calls: diffSwapStringCalls()})
}
