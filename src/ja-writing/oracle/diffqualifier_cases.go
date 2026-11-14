package oracle

import (
	"math/rand/v2"
	"strconv"
	"strings"
)

// 群 Q (数に付いた、概数と範囲を表す語) の記録: 段 _qualifier_risks(sides, groups) と _qualifier_yaku_ok と定数。
// 組は、群 M までと同じ入れ物 (diffBags) に、専用の手書きの組と固定の種 (PCG 1076/19) の組を足したもの。

var diffQualifierConstants = []string{
	"_QUALIFIER_JOIN", "_QUALIFIER_SKIP", "_QUALIFIER_SHOWN", "_QUALIFIER_KATAKANA_UNIT", "_QUALIFIER_PART", "_QUALIFIER_AFTER", "_QUALIFIER_UNIT_ONLY",
	"_QUALIFIER_PLACE_UNITS", "_QUALIFIER_VERSION_BEFORE", "_QUALIFIER_BEFORE", "_QUALIFIER_BEFORE_REACH", "_QUALIFIER_YAKU_WORDS", "_QUALIFIER_YAKU_NOUNS",
	"_QUALIFIER_NEAR_AFTER", "_QUALIFIER_NEAR_BEFORE", "_QUALIFIER_SENTENCE", "_QUALIFIER_FAMILIES", "_QUALIFIER_BOUNDS", "_QUALIFIER_STRICT",
	"_QUALIFIER_SENTENCE_STOP", "_QUALIFIER_SPLIT_DECIMAL",
}

// DiffQualifierFunctions は群 Q の関数。
var DiffQualifierFunctions = []string{"_qualifier_risks", "_qualifier_yaku_ok"}

// DiffQualifierConstantNames は群 Q の定数の名前 (Go の突き合わせが、この名前の Get を回す)。
func DiffQualifierConstantNames() []string { return diffQualifierConstants }

// diffQualifierBank は概数と範囲を表す語ごとの文。
var diffQualifierBank = []string{
	"約120人が参加した。", "120人が参加した。", "およそ120人が参加した。", "おおよそ120人が参加した。", "凡そ120人が参加した。", "概ね120人が参加した。", "おおむね120人が参加した。", "ほぼ120人が参加した。", "推定120人が参加した。", "大体120人が参加した。", "だいたい120人が参加した。",
	"120人ほどが参加した。", "120人程度が参加した。", "120人程が参加した。", "120人前後が参加した。", "120人ぐらいが参加した。", "120人くらいが参加した。", "120人位が参加した。", "120人ごろが参加した。", "120人頃が参加した。", "120人余りが参加した。", "120人余が参加した。", "120人あまりが参加した。",
	"120人近くが参加した。", "120人足らずが参加した。", "120人内外が参加した。", "120人かそこらが参加した。", "120人やそこらが参加した。", "120人弱が参加した。", "120人強が参加した。",
	"100人以上が参加した。", "100人以下が参加した。", "100人以内が参加した。", "100人以前に決めた。", "100人以降に決めた。", "100人以後に決めた。", "100人超が参加した。", "100人を超える。", "100人を超えた。", "100人未満が参加した。", "100人を上回る。", "100人を下回る。", "100人を超えず参加した。", "100人超過した。",
	"3日以上かかる。", "3日以内に終える。", "3日以下で済む。", "3日超かかる。", "3日未満で終わる。", "3日を超える。", "3日を超えて使えません。", "3日を下回らない。",
	"3〜5日程度かかる。", "約3〜5日かかる。", "3日から5日ほどかかる。", "3〜5日かかる。", "3日から5日かかる。", "3-5日かかる。", "3〜5日ほどかかる。", "3日程度から5日程度かかる。", "3万円程度から5万円程度かかる。",
	"1万5000人が参加した。", "約1万5000人が参加した。", "1万5000人ほどが参加した。", "2億3000万円かかる。", "約2億3000万円かかる。", "3万〜5万円かかる。", "3万円〜5万円かかる。", "10万円ほど。", "約10万円。", "10万円。", "3億円。", "5パーセントほど。", "約5パーセント。",
	"契約3件を結んだ。", "約3件を結んだ。", "予約2件を取った。", "要約3行を書く。", "所要約30分かかる。", "主要約10社が参加した。", "作成約3時間かかる。", "体制約50人で動く。", "募集約50名。", "利用者約120人。", "人口約120万人。", "人口120万人。",
	"ベスト4強に入った。", "16強に残った。", "震度5弱を観測した。", "震度は6強だった。", "3位に入った。", "5分弱かかる。", "1時間弱要した。", "3年余働いた。", "5分強かかる。", "3年位かかる。", "3位置に置く。", "3人強くなった。",
	"第3章以下を読む。", "第5条以下に従う。", "3人以下で行く。", "v2.0前後を使う。", "バージョン2.0前後を使う。", "2.0前後を使う。", "10月中旬ごろ。", "15時半ごろ。", "3月末ごろ。", "2年半ほど。", "10月上旬。", "10月初旬ごろ。", "10月下旬ごろ。", "10月半ば。",
	"国道1号近くにある。", "3人近くにある。", "3日ほど前。", "3年前ぐらい。", "1時間前ほど。", "3年ぐらい前。", "1時間ほど前。", "3日後ほど。", "3日ほど後。",
	"タイムアウトは30秒で、待ちがこれを超えた。", "待ちが30秒を超えた。", "最大3日かかる。", "最長3日かかる。", "最低3日かかる。", "少なくとも3日かかる。", "多くて3日かかる。", "上限3日で終える。", "下限3日で終える。", "3日まで待つ。", "3日より前に終える。", "3日より短く終える。", "3日から始める。", "3日でも足りる。",
	"3日規模の作業。", "3日見当で終える。", "3日ばかり待つ。", "3日そこそこで終える。", "3日に迫る。", "3日に近い。", "3日に満たない。", "3日を切る。", "3日を割る。", "3日のうちに終える。", "3日を限度とする。", "3日が上限だ。", "3日は必要です。", "3日はかかる。", "3日より古い。", "3.9より新しい。",
	"推定で3日かかる。", "概算で3日かかる。", "見込みは3日だ。", "目安は3日以上だ。", "想定は3日以内だ。", "3日と見られる。", "ざっと3日かかる。", "3日程度、約5日、およそ7日。",
	"**3**日ほど。", "約**3**日。", "3 日ほど。", "3 GB 程度。", "約16 GB。", "95 %前後。", "30sほど。", "約30s。", "３．５人ほど。", "約３．５人。", "3.5人以上。", "３．５人以上。", "１．２万人ほど。",
	"一つ程度。", "約一つ。", "三日ほど。", "約三日。", "三日以上。", "三〜五日程度。", "約三から五日。", "ひとりほど。", "約ひとり。", "二人以上。", "十人ほど。", "約十人。",
}

// diffQualifierSwaps は概数・範囲の語を足す・消す・入れ替える置き換え (元の文字列, 変えた文字列)。
var diffQualifierSwaps = [][2]string{
	{"約", ""}, {"", "約"}, {"およそ", "約"}, {"約", "およそ"}, {"ほど", ""}, {"程度", ""}, {"前後", ""}, {"ぐらい", ""}, {"以上", ""}, {"以下", ""}, {"以内", ""}, {"未満", ""}, {"を超える", ""}, {"を上回る", ""},
	{"ほど", "程度"}, {"程度", "ほど"}, {"ぐらい", "くらい"}, {"以上", "以下"}, {"以下", "以上"}, {"以上", "超"}, {"超", "以上"}, {"以下", "未満"}, {"未満", "以下"}, {"以内", "以下"}, {"以下", "以内"},
	{"3日", "約3日"}, {"3日", "3日ほど"}, {"3日", "3日以上"}, {"3日", "3日以内"}, {"3日", "3日を超える"}, {"3日", "3日未満"}, {"120人", "約120人"}, {"120人", "120人ほど"}, {"100人", "100人以上"}, {"100人", "100人以下"},
	{"3〜5日", "3日から5日"}, {"3日から5日", "3〜5日"}, {"3〜5日程度", "約3〜5日"}, {"約3〜5日", "3〜5日程度"}, {"3〜5日", "5日"}, {"1万5000人", "1万人"}, {"1万5000人", "15000人"},
	{"最大", ""}, {"最大", "約"}, {"少なくとも", ""}, {"推定", ""}, {"概算", ""}, {"見込み", ""}, {"まで", ""}, {"まで", "以内"}, {"から", ""}, {"規模", ""}, {"規模", "ほど"},
	{"契約3件", "約3件"}, {"予約2件", "約2件"}, {"所要約30分", "30分"}, {"5分弱", "5分"}, {"5分強", "5分以上"}, {"3年余", "3年"}, {"3年余", "3年以上"}, {"第3章以下", "第3章"}, {"v2.0前後", "v2.0"},
	{"3日", "3 日"}, {"3日", "三日"}, {"3日", "3日間"}, {"。", "ほど。"}, {"。", "以上。"}, {"。", "以内。"},
}

func diffQualifierHandPairs() []diffFullCase {
	doc := diffDoc
	return []diffFullCase{
		{name: "same", old: doc("約120人が参加した。"), new: doc("約120人が参加した。")},
		{name: "approx-lost", old: doc("約120人が参加した。"), new: doc("120人が参加した。")},
		{name: "approx-added", old: doc("120人が参加した。"), new: doc("約120人が参加した。")},
		{name: "approx-same-family", old: doc("約120人が参加した。"), new: doc("120人ほどが参加した。")},
		{name: "lower-lost", old: doc("100人以上が参加した。"), new: doc("100人が参加した。")},
		{name: "lower-to-upper", old: doc("100人以上が参加した。"), new: doc("100人以下が参加した。")},
		{name: "lower-in-to-ex", old: doc("100人以上が参加した。"), new: doc("100人超が参加した。")},
		{name: "upper-in-to-ex", old: doc("3日以内に終える。"), new: doc("3日未満に終える。")},
		{name: "upper-same", old: doc("3日以内に終える。"), new: doc("3日以下で終える。")},
		{name: "bound-verb", old: doc("2秒を超えて使えません。"), new: doc("2秒以上かかると使えません。")},
		{name: "bound-verb-lost", old: doc("2秒を超えて使えません。"), new: doc("2秒で使えません。")},
		{name: "range-to-single", old: doc("移行は3〜5日で終える。"), new: doc("移行は5日で終える。")},
		{name: "range-notation", old: doc("移行は約3〜5日で終える。"), new: doc("移行は3〜5日程度で終える。")},
		{name: "compound", old: doc("約1万5000人が参加した。"), new: doc("1万5000人ほどが参加した。")},
		{name: "yaku-word", old: doc("契約3件を結んだ。"), new: doc("契約3件を結んだ。")},
		{name: "yaku-noun", old: doc("所要約30分かかる。"), new: doc("所要30分かかる。")},
		{name: "unit-only-kyo", old: doc("ベスト4強に入った。"), new: doc("ベスト4に入った。")},
		{name: "unit-only-jaku", old: doc("5分弱かかる。"), new: doc("5分かかる。")},
		{name: "place-unit", old: doc("第3章以下を読む。"), new: doc("第3章を読む。")},
		{name: "version", old: doc("v2.0前後を使う。"), new: doc("v2.0を使う。")},
		{name: "go-chikaku", old: doc("国道1号近くにある。"), new: doc("国道1号にある。")},
		{name: "near-after", old: doc("3日まで待つ。"), new: doc("3日以内で待つ。")},
		{name: "near-before", old: doc("最大3日かかる。"), new: doc("3日以内でかかる。")},
		{name: "sentence-word", old: doc("推定3日かかる。"), new: doc("3日かかる。")},
		{name: "duplicate-values", old: doc("3日以上と3日以内と3日を使う。"), new: doc("3日と3日以内と3日以上を使う。")},
		{name: "duplicate-values-shift", old: doc("約3日、3日、3日以上。"), new: doc("3日、約3日、3日。")},
		{name: "weak-skip", old: doc("一つ程度ある。"), new: doc("一つある。")},
		{name: "split-decimal-same", old: doc("３．５人ほど。"), new: doc("３．５人ほど。")},
		{name: "split-decimal-changed", old: doc("３．５人ほど。"), new: doc("３．５人。")},
		{name: "split-decimal-reach", old: doc("約３．５人以上。"), new: doc("約３．５人。")},
		{name: "kanji", old: doc("三日ほどかかる。"), new: doc("三日かかる。")},
		{name: "table", old: doc("| 項目 | 値 |", "|---|---|", "| 人数 | 約3人 |"), new: doc("| 項目 | 値 |", "|---|---|", "| 人数 | 3人 |")},
		{name: "no-number", old: doc("ほど。"), new: doc("以上。")},
		{name: "empty", old: "", new: doc("約3日。")},
		{name: "join-slash", old: doc("2026/10/13ごろ。"), new: doc("2026/10/13。")},
		{name: "join-time", old: doc("10:30前後。"), new: doc("10:30。")},
		{name: "between-newline", old: doc("3日", "約5日"), new: doc("3日", "5日")},
	}
}

func diffQualifierFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 19))
	pick := func() string { return diffQualifierBank[r.IntN(len(diffQualifierBank))] }
	variant := func(s string) string {
		for range 8 {
			sw := diffQualifierSwaps[r.IntN(len(diffQualifierSwaps))]
			if sw[0] == "" {
				// 数の前に印を足す
				if i := strings.IndexAny(s, "0123456789"); i >= 0 {
					return s[:i] + sw[1] + s[i:]
				}
				continue
			}
			if strings.Contains(s, sw[0]) {
				return strings.Replace(s, sw[0], sw[1], 1)
			}
		}
		return pick()
	}
	var cases []diffFullCase
	for n := range 1800 {
		var lines [][]string
		for range 1 + r.IntN(3) {
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

func diffQualifierStringCalls() []Call {
	var calls []Call
	for _, s := range []string{"契約3件", "予約2件", "所要約30分", "主要約10社", "要約3行", "約", "約3", "あ約3", "所要約", "作成約3時間", "体制約50人", "利用者約120人", "x契約", "契約", "集約3", "新約聖書約3", "人口約120万人", "必要約3"} {
		for at := range len([]rune(s)) {
			for _, start := range []int{0, 1, 2} {
				c := dcall("_qualifier_yaku_ok", s, start, at)
				c.Keep = true
				calls = append(calls, c)
			}
		}
	}
	return calls
}

func diffQualifierCases(pairs [][3]string) []VisCase {
	cases := []VisCase{{Name: "qualifier/constants", Calls: diffConstantCalls(diffQualifierConstants)}}
	for _, b := range diffBags(pairs) {
		cases = append(cases, VisCase{Name: "qualifier/" + b.name, Calls: diffRiskStageCalls(b.old, b.new, "_qualifier_risks", false)})
	}
	for _, g := range []struct {
		prefix string
		cases  []diffFullCase
		keep   bool
	}{{"qualifier/hand/", diffQualifierHandPairs(), true}, {"qualifier/fuzz/", diffQualifierFuzz(), false}} {
		for _, f := range g.cases {
			cases = append(cases, VisCase{Name: g.prefix + f.name, Calls: diffRiskStageCalls(f.old, f.new, "_qualifier_risks", g.keep)})
		}
	}
	return append(cases, VisCase{Name: "qualifier/strings", Calls: diffQualifierStringCalls()})
}
