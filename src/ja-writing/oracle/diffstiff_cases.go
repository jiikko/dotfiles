package oracle

import (
	"math/rand/v2"
	"strconv"
	"strings"
)

// 群 Z (文体の硬化) の記録: stiffness(orig, rw, doc_type)・stiffness_metrics(text)・_measure・_ratio・_nominal_hits・_no_chains・_changed_excerpt と定数。
// 組は、群 F〜I までと同じ入れ物 (入力の集まりと diff の入力の組・全文差分・見出し・文・種類・許可・規則の手書きの組) に、群 Z 専用の組
// (閾値の分かれ目にちょうど当たる組・本家のコメントの例文・固定の種の組) を足したもの。

// diffStiffConstants は群 Z の Go の表と比べる本家の定数 (正規表現は文字列と flags)。
var diffStiffConstants = []string{
	"STIFFNESS_THRESHOLDS", "STIFFNESS_INFO_DOC_TYPES", "_K", "_OKURI_MID", "_OKURI_TAIL", "_NOUN_CHARS", "_NOUN",
	"_STIFF_KANJI", "_HIRAGANA_CHARS", "_NO_CHAIN", "_KANGO", "_NUMERAL_HEAD", "_LETTER_OR_DIGIT", "_LENGTH_PUNCT_PATTERN",
}

// DiffStiffFunctions は群 Z の関数。
var DiffStiffFunctions = []string{"stiffness", "stiffness_metrics", "_measure", "_ratio", "_nominal_hits", "_no_chains", "_changed_excerpt"}

// DiffStiffConstantNames は群 Z の定数の名前 (Go の突き合わせが、この名前の Get を群 Z の定数に回す)。
func DiffStiffConstantNames() []string { return diffStiffConstants }

func diffConstantCalls(names []string) []Call {
	calls := make([]Call, len(names))
	for i, name := range names {
		calls[i] = Call{Module: DiffModule, Function: name, Get: true, Digest: true, Keep: true}
	}
	return calls
}

// diffStiffDocTypes は stiffness に渡す文書の種類 (nil は指定なし。incident・formal・paper が参考で、ほかは見直し候補)。
var diffStiffDocTypes = []any{nil, "incident", "formal", "paper", "promo", "mail"}

// diffStiffStageCalls は組 1 つに stiffness と、両方の文書の stiffness_metrics を通す呼び出し (種類は types の分だけ)。
func diffStiffStageCalls(old, new string, types []any, keep bool) []Call {
	var calls []Call
	for _, t := range types {
		c := dcall("stiffness", old, new, t)
		c.Keep = keep
		calls = append(calls, c)
	}
	for _, text := range []string{old, new} {
		c := dcall("stiffness_metrics", text)
		c.Keep = keep
		calls = append(calls, c)
	}
	return calls
}

// diffStiffSentenceCalls は 1 つの文 (行) に文の単位の関数を通す呼び出し。
func diffStiffSentenceCalls(text string, keep bool) []Call {
	patterns := map[string]any{"$ref": "nominal", "path": []any{0}}
	calls := []Call{dcall("_measure", text), dcall("_nominal_hits", text, patterns), dcall("_no_chains", text)}
	for i := range calls {
		calls[i].Keep = keep
	}
	return calls
}

// diffStiffLoadNominal は名詞化の言い回しの一覧を覚える呼び出し (_nominal_hits の引数)。
func diffStiffLoadNominal() Call {
	c := dcall("_load_list", "nominalization")
	c.Save, c.Quiet = "nominal", true
	return c
}

// diffBag は群 Z・AA が通す組の入れ物 (群 F〜I までと同じ組の集まり)。
type diffBag struct{ name, old, new string }

// diffBags は入力の集まりと diff の入力の組 (pairs) と、全文差分・見出し・文・種類・許可・規則の手書きの組と固定の種の組。
func diffBags(pairs [][3]string) []diffBag {
	var bags []diffBag
	for _, p := range pairs {
		bags = append(bags, diffBag{"pair/" + p[0], p[1], p[2]})
	}
	for _, g := range []struct {
		prefix string
		cases  []diffFullCase
	}{{"full/", diffFullCases()}, {"heading/", diffHeadingCases()}, {"sentence/", diffSentenceCases()}, {"type/", diffTypeCases()}, {"modal/", diffModalCases()}, {"rule/", diffRuleCases()}} {
		for _, f := range g.cases {
			bags = append(bags, diffBag{g.prefix + f.name, f.old, f.new})
		}
	}
	return bags
}

// diffStiffSoft は軟らかい言い方と、同じことを硬く言い直した言い方の組 (漢字の割合が増え、平仮名が減る)。
var diffStiffSoft = [][2]string{
	{"決めました。", "決定しました。"},
	{"使えるようにします。", "利用可能な状態にします。"},
	{"ちゃんと確かめます。", "確実に確認します。"},
	{"とても大事なことです。", "極めて重要な事項です。"},
	{"どうやって直すかを話し合います。", "修正方法を協議します。"},
	{"知らせを出します。", "通知を実施します。"},
	{"まとめて送ります。", "一括送信します。"},
	{"先に見ておきます。", "事前に確認しておきます。"},
	{"お客さまに聞いてみます。", "顧客に照会します。"},
	{"ほかの人にも伝えます。", "関係者全員に周知します。"},
	{"なおすのに時間がかかります。", "復旧に時間を要します。"},
	{"二つの案を比べます。", "二案を比較検討します。"},
	{"今日中に終わらせます。", "本日中に完了させます。"},
	{"できるだけ早くやります。", "可及的速やかに対応します。"},
	{"元に戻せます。", "復元が可能です。"},
}

// diffStiffNominal は名詞化の言い回し (が可能・することが可能・の実施・を実施・を担保・を図る・に資する・につながる・の観点から・に関して・を行う・不可能) を含む文と、含まない文。
var diffStiffNominal = []string{
	"保存が可能です。", "保存が可能でした。", "保存が可能だ。", "保存が可能である。", "保存が可能となり、安心です。", "保存が可能になります。", "保存が可能な形にする。", "保存が可能に見える。", "保存が可能で、速い。", "保存が可能。", "保存が可能、とされる。",
	"保存することが可能です。", "確認の実施を決める。", "確認を実施します。", "確認を実施した。", "確認を実施する。", "確認を実施中です。", "品質を担保します。", "品質の確保を図る。", "品質を図り直す。", "品質を図った。", "品質を図れない。", "改善に資する取り組み。", "改善に資した。", "改善に資さない。",
	"成果につながる。", "成果につながった。", "成果につながって、嬉しい。", "成果につながっている。", "安全の観点から見直す。", "安全の観点で見直す。", "安全の観点では問題ない。", "安全の観点がある。",
	"設定に関して説明する。", "設定に関し、説明する。", "設定に関する説明をする。", "設定に関わる説明。", "点検を行う。", "点検を行い、報告する。", "点検を行って、報告する。", "点検を行った。", "点検を行わない。", "点検を行えば、分かる。", "点検を行える。",
	"保存は不可能です。", "保存が不可能だ。", "保存を不可能にする。", "不可能を可能にする。", "それは不可能に近い。", "保存を試みる。", "確認する。",
}

// diffStiffChains は「の」の連続 (名詞 3 つ以上で、3 つとも漢語) の文と、そうでない文 (漢数字で始まる語・漢語が 2 つだけ・平仮名の名詞が入る)。
var diffStiffChains = []string{
	"運用管理の設定変更の確認手順を整理する。", "営業部の今期の売上を報告する。", "東京本社の営業部の今期の売上を報告する。", "営業部（東京本社）の今期の売上を報告する。", "全社の売上の予算の実績を見る。",
	"最後の一日の作業の確認。", "一日の作業の確認の手順。", "三つの項目の設定の変更。", "工場の最後の一日の作業。", "十月の売上の実績の集計。", "百万円の予算の執行の状況。",
	"設定の変更の確認。", "システムの管理の設定の変更の履歴。", "ABC の設定の変更の履歴を残す。", "私の考えの方針の転換。", "今日の天気の話。", "管理者の権限の設定の画面。",
	"経営企画部の中期の経営の計画の見直し。", "業務の改善の提案の検討の結果の共有。", "お客さまの声の反映の仕組み。", "資料の作成の担当の決定の連絡。",
}

// diffStiffNeutral は硬化に関係しない文。
var diffStiffNeutral = []string{
	"今日はよい天気です。", "ここは静かな町です。", "朝ごはんを食べました。", "猫が窓のそばで寝ています。", "友だちと映画を見に行きました。", "雨が降ってきたので、傘を持って出かけました。",
	"これはテストです。", "さて、次に進みましょう。", "よろしくお願いします。", "ありがとうございました。",
}

func diffStiffPick(r *rand.Rand, pool []string) string { return pool[r.IntN(len(pool))] }

// diffStiffFuzz は固定の種 (PCG 1076/12) から作った、軟らかい言い方を硬い言い方に変える・名詞化の言い回しを足す・「の」の連続を足す・文を足す・消す・入れ替える組。
func diffStiffFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 12))
	var cases []diffFullCase
	for n := range 900 {
		var old, rewrite []string
		for range 1 + r.IntN(5) {
			switch r.IntN(6) {
			case 0, 1:
				p := diffStiffSoft[r.IntN(len(diffStiffSoft))]
				old, rewrite = append(old, p[0]), append(rewrite, p[1])
			case 2:
				s := diffStiffPick(r, diffStiffNominal)
				if r.IntN(2) == 0 {
					old, rewrite = append(old, diffStiffPick(r, diffStiffNeutral)), append(rewrite, s)
				} else {
					old, rewrite = append(old, s), append(rewrite, s)
				}
			case 3:
				s := diffStiffPick(r, diffStiffChains)
				if r.IntN(3) == 0 {
					old, rewrite = append(old, s), append(rewrite, diffStiffPick(r, diffStiffChains))
				} else {
					old, rewrite = append(old, diffStiffPick(r, diffStiffNeutral)), append(rewrite, s)
				}
			case 4:
				s := diffStiffPick(r, diffStiffNeutral)
				old, rewrite = append(old, s), append(rewrite, s)
			default:
				p := diffStiffSoft[r.IntN(len(diffStiffSoft))]
				old, rewrite = append(old, p[1]), append(rewrite, p[0])
			}
		}
		// 行の並べ方 (同じ行・段落・見出し・箇条書き・引用・コード) を変える
		join := func(list []string) string {
			var b strings.Builder
			for _, s := range list {
				switch r.IntN(8) {
				case 0:
					b.WriteString("\n\n")
				case 1:
					b.WriteString("\n- ")
				case 2:
					b.WriteString("\n## ")
				case 3:
					b.WriteString("\n> ")
				}
				b.WriteString(s)
			}
			return b.String() + "\n"
		}
		o, w := join(old), join(rewrite)
		if r.IntN(4) == 0 {
			w = strings.Replace(w, "\n", "\n\n", 1)
		}
		if r.IntN(10) == 0 {
			o = "```\n" + diffStiffPick(r, diffStiffChains) + "\n```\n" + o
		}
		cases = append(cases, diffFullCase{name: "stiff-fuzz-" + strconv.Itoa(n), old: o, new: w})
	}
	return cases
}

// diffStiffGrid は閾値の分かれ目にちょうど当たる組。字数の合計 total の文 (漢字 kanji 字・平仮名 hira 字・残りは英字) を作る。
func diffStiffSentence(kanji, hira, total int) string {
	return strings.Repeat("漢", kanji) + strings.Repeat("あ", hira) + strings.Repeat("A", total-kanji-hira) + "。"
}

// diffStiffBoundaryCases は 1 文の条件 (漢字の割合 +8.0 ポイント・漢字 +2 字・平仮名 -2 字)・名詞化の条件 (漢字 +2 字)・文書全体の条件 (+3.0 ポイント・漢字 +2 字・組が 2 つ以上)・
// 名詞化の数の増え (+2) の、それぞれ分かれ目の両側を通す組。
func diffStiffBoundaryCases() []diffFullCase {
	var cases []diffFullCase
	name := func(parts ...int) string {
		s := "stiff-boundary"
		for _, p := range parts {
			s += "-" + strconv.Itoa(p)
		}
		return s
	}
	// 1 文: 元の漢字 ok・平仮名 ho → 後の漢字 nk・平仮名 hn (字数 total)
	for _, total := range []int{20, 25, 50} {
		for _, ok := range []int{0, 1, 2} {
			for _, up := range []int{1, 2, 3} {
				for _, down := range []int{1, 2, 3} {
					ho := 6
					cases = append(cases, diffFullCase{name: name(total, ok, up, down), old: diffStiffSentence(ok, ho, total) + "\n", new: diffStiffSentence(ok+up, ho-down, total) + "\n"})
				}
			}
		}
	}
	// 割合の差がちょうど 8.0 になる組 (1/25 → 3/25、2/25 → 4/25)
	for _, ok := range []int{1, 2, 3} {
		cases = append(cases, diffFullCase{name: name(900, ok), old: diffStiffSentence(ok, 5, 25) + "\n", new: diffStiffSentence(ok+2, 3, 25) + "\n"})
		cases = append(cases, diffFullCase{name: name(901, ok), old: diffStiffSentence(ok, 5, 25) + "\n", new: diffStiffSentence(ok+2, 3, 24) + "\n"})
		cases = append(cases, diffFullCase{name: name(902, ok), old: diffStiffSentence(ok, 5, 25) + "\n", new: diffStiffSentence(ok+2, 3, 26) + "\n"})
	}
	// 文書全体: 2 つの文の漢字が合計で kanjiUp 字増える (字数は 1 文 total ずつ)。組が 1 つだけの文書も通す
	for _, total := range []int{34, 50, 100, 200} {
		for _, up := range []int{1, 2, 3, 4, 6} {
			old := diffStiffSentence(0, 20, total) + "\n" + diffStiffSentence(0, 20, total) + "\n"
			first := up / 2
			second := up - first
			cases = append(cases, diffFullCase{name: name(1000, total, up), old: old, new: diffStiffSentence(first, 20, total) + "\n" + diffStiffSentence(second, 20, total) + "\n"})
			cases = append(cases, diffFullCase{name: name(1001, total, up), old: old, new: diffStiffSentence(up, 20, total) + "\n" + diffStiffSentence(0, 20, total) + "\n"})
		}
	}
	// 名詞化の数: 元 base 個 → 後 base+up 個 (別々の文に 1 つずつ)
	for _, base := range []int{0, 1, 2} {
		for _, up := range []int{0, 1, 2, 3} {
			var old, rewrite []string
			for range base {
				old, rewrite = append(old, "保存を行う。"), append(rewrite, "保存を行う。")
			}
			for range up {
				old = append(old, "保存する。")
				rewrite = append(rewrite, "点検を実施する。")
			}
			cases = append(cases, diffFullCase{name: name(2000, base, up), old: diffStiffLines(old), new: diffStiffLines(rewrite)})
		}
	}
	return cases
}

func diffStiffLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// diffStiffHandCases は本家のコメントの例文と、各候補の枝・level・コード/表/見出しを数えない・伏せる範囲・同じ文の組・組にならない文の塊を通す手書きの組。
func diffStiffHandCases() []diffFullCase {
	doc := diffDoc
	return []diffFullCase{
		{name: "stiff-same", old: doc("決めました。"), new: doc("決めました。")},
		{name: "stiff-empty", old: "", new: ""},
		{name: "stiff-empty-old", old: "", new: doc("決定しました。")},
		{name: "stiff-empty-new", old: doc("決定しました。"), new: ""},
		{name: "stiff-sentence", old: doc("ちゃんと確かめます。"), new: doc("確実に確認します。")},
		{name: "stiff-sentence-nominal", old: doc("点検します。"), new: doc("点検を実施します。")},
		{name: "stiff-sentence-both", old: doc("ちゃんと決めます。"), new: doc("確実に決定を実施します。")},
		{name: "stiff-document", old: doc("決めました。使えるようにします。", "ちゃんと確かめます。"), new: doc("決定しました。利用可能な状態にします。", "確実に確認します。")},
		{name: "stiff-document-two-pairs", old: doc("決めました。", "ちゃんと確かめます。"), new: doc("決定しました。", "確実に確認します。")},
		{name: "stiff-document-one-pair", old: doc("決めました。", "今日はよい天気です。"), new: doc("決定しました。", "今日はよい天気です。")},
		{name: "stiff-nominal-doc", old: doc("保存する。", "確認する。", "送る。"), new: doc("保存を実施する。", "確認を実施する。", "送信が可能です。")},
		{name: "stiff-nominal-doc-two", old: doc("保存する。", "確認する。"), new: doc("保存を実施する。", "確認を実施する。")},
		{name: "stiff-nominal-dup-line", old: doc("保存する。確認する。"), new: doc("保存を実施する。確認を実施する。")},
		{name: "stiff-nominal-removed", old: doc("保存を実施する。確認を実施する。"), new: doc("保存する。確認する。")},
		{name: "stiff-nominal-overlap", old: doc("保存する。"), new: doc("保存することが可能です。保存が可能です。")},
		{name: "stiff-chain-added", old: doc("売上を報告する。"), new: doc("営業部の今期の売上を報告する。")},
		{name: "stiff-chain-paired", old: doc("営業部の売上を報告する。"), new: doc("営業部の今期の売上を報告する。")},
		{name: "stiff-chain-bracket", old: doc("営業部（東京本社）の今期の売上を報告する。"), new: doc("東京本社の営業部の今期の売上を報告する。")},
		{name: "stiff-chain-numeral", old: doc("最後の日の作業。"), new: doc("最後の一日の作業の確認。")},
		{name: "stiff-chain-numeral-3", old: doc("確認する。"), new: doc("一日の作業の確認を行う。")},
		{name: "stiff-chain-numeral-3-month", old: doc("集計する。"), new: doc("十月の売上の実績を集計する。")},
		{name: "stiff-chain-numeral-3-amount", old: doc("執行する。"), new: doc("百万円の予算の執行を行う。")},
		{name: "stiff-chain-numeral-3-middle", old: doc("作業する。"), new: doc("最後の一日の作業を行う。")},
		{name: "stiff-chain-unpaired", old: doc("設定を見る。"), new: doc("設定を見る。", "", "運用管理の設定変更の確認手順を整理する。")},
		{name: "stiff-chain-old-has", old: doc("運用管理の設定変更の確認手順を整理する。"), new: doc("運用管理の設定変更の確認手順を整理する。", "", "運用管理の設定変更の確認手順を整理し直す。")},
		{name: "stiff-chain-count", old: doc("運用管理の設定変更の確認手順と、営業部の今期の売上を報告する。"), new: doc("運用管理の設定変更の確認手順を報告する。")},
		{name: "stiff-chain-fewer", old: doc("運用管理の設定変更の確認手順を整理する。"), new: doc("運用管理の設定変更の確認手順と、営業部の今期の売上を報告する。")},
		{name: "stiff-chain-same-raw", old: doc("営業部の今期の売上。"), new: doc("営業部の今期の売上。")},
		{name: "stiff-code", old: doc("```", "決めました。", "```"), new: doc("```", "決定しました。", "```")},
		{name: "stiff-table", old: doc("| 項 | 値 |", "|---|---|", "| ちゃんと | 決めます |"), new: doc("| 項 | 値 |", "|---|---|", "| 確実に | 決定します |")},
		{name: "stiff-heading", old: doc("# 決めること", "本文です。"), new: doc("# 決定事項", "本文です。")},
		{name: "stiff-frontmatter", old: doc("---", "title: ちゃんと", "---", "本文です。"), new: doc("---", "title: 確実に", "---", "本文です。")},
		{name: "stiff-inline-code", old: doc("`決めました` と書きます。"), new: doc("`決定しました` と書きます。")},
		{name: "stiff-url", old: doc("https://example.com/決めました を見ます。"), new: doc("https://example.com/決定しました を見ます。")},
		{name: "stiff-image-link", old: doc("![決めました](a.png) と [見ます](https://x/決めた) 。"), new: doc("![決定しました](a.png) と [見ます](https://x/決定した) 。")},
		{name: "stiff-html", old: doc("<span class=\"決めた\">決めました</span>。"), new: doc("<span class=\"決定した\">決定しました</span>。")},
		{name: "stiff-replace-rest", old: doc("ちゃんと確かめます。まとめて送ります。"), new: doc("確実に確認し、一括送信します。")},
		{name: "stiff-replace-rest-many", old: doc("ちゃんと確かめます。", "別の話です。", "まとめて送ります。"), new: doc("確実に確認します。", "一括送信を実施します。")},
		{name: "stiff-insert-only", old: doc("今日はよい天気です。"), new: doc("今日はよい天気です。", "点検を実施します。")},
		{name: "stiff-delete-only", old: doc("今日はよい天気です。", "点検を実施します。"), new: doc("今日はよい天気です。")},
		{name: "stiff-moved", old: doc("決めました。", "", "今日はよい天気です。"), new: doc("今日はよい天気です。", "", "決定しました。")},
		{name: "stiff-crlf", old: "決めました。\r\n", new: "決定しました。\r\n"},
		{name: "stiff-list", old: doc("- 決めました", "- 見ました"), new: doc("- 決定しました", "- 確認しました")},
		{name: "stiff-quote", old: doc("> 決めました。"), new: doc("> 決定しました。")},
		{name: "stiff-numbers", old: doc("今日は３月１日です。"), new: doc("本日は三月一日です。")},
		{name: "stiff-katakana-alpha", old: doc("サーバーを AB で動かす。"), new: doc("サーバー運用管理を AB で動かす。")},
		{name: "stiff-kanji-ranges", old: doc("あ"), new: doc("㐀䶿一鿿豈\ufaff々〆")},
		{name: "stiff-kanji-outside", old: doc("あ"), new: doc("㏿䷀䷿ꀀ\uf8ffﬀ〇")},
		{name: "stiff-hira-ranges", old: doc("ぁゖ漢漢"), new: doc("\u3040\u3097漢漢漢漢")},
		{name: "stiff-hira-outside", old: doc("\u3040\u3097ゝ゠AAAA漢"), new: doc("漢漢漢AAAA漢")},
		{name: "stiff-punct", old: doc("ぁ、。，．！？!?「」『』（）()…・x"), new: doc("漢、。，．！？!?「」『』（）()…・x")},
		{name: "stiff-punct-not", old: doc("あ【】〈〉《》〔〕[]{}<>;:,.-_ x"), new: doc("漢【】〈〉《》〔〕[]{}<>;:,.-_ x")},
		{name: "stiff-digits", old: doc("あ１２３٣ⅷ½²x_y"), new: doc("漢１２３٣ⅷ½²x_y")},
		{name: "stiff-empty-body", old: doc("   "), new: doc("決定しました。")},
		{name: "stiff-excerpt-long", old: doc("あ、い、う、え、お、か、き、く、け、こ、さ、し、す、せ、そ、た、ち、つ、て、と、な、に、ぬ、ね、の、は、ひ、ふ、へ、ほ、ま、み、む、め、も。"), new: doc("あ、い、う、え、お、か、き、く、け、こ、さ、し、す、せ、そ、た、ち、つ、て、と、な、に、ぬ、ね、の、は、ひ、ふ、へ、ほ、ま、み、む、め、も、や、ゆ、よ。")},
	}
}

// diffStiffExcerptPairs は _changed_excerpt に通す組 (2 つの文の変わった所を、読点か句点で区切った節の単位で出す)。
func diffStiffExcerptPairs() [][2]string {
	long := strings.Repeat("あ", 50)
	return [][2]string{
		{"", ""}, {"あ", ""}, {"", "あ"}, {"あ", "あ"}, {"あい", "あう"}, {"あい、う", "あえ、う"}, {"あ、い。う", "あ、え。う"}, {"あいう。えお", "あいう。かき"},
		{"前置き、本体です。後ろ。", "前置き、変更です。後ろ。"}, {"前置き、本体です。後ろ。", "前置き、本体を変更します。後ろ。"}, {"、あ、", "、い、"}, {"，あ，", "，い，"}, {"あ！い", "あ？い"}, {"あ!い", "あ?い"},
		{long + "い" + long, long + "う" + long}, {long + "、い、" + long, long + "、う、" + long}, {long + "、い、う" + long, long + "、え、お" + long}, {long, long + "あ"}, {long + "あ", long},
		{"abc、def、ghi", "abc、dXf、ghi"}, {"あ。", "い。"}, {"。あ", "。い"}, {"、。、", "、、。"}, {"あ、", "あ、い、"}, {"あ、い、", "あ、"},
		{"あいうえお", "あいうえお。かきくけこ"}, {"あいうえお。かきくけこ", "あいうえお"}, {"、、あ、、", "、、い、、"}, {"，、あ、，", "，、い、，"},
		{"一、二、三", "一、2、三"}, {"営業部の今期の売上", "営業部（東京本社）の今期の売上"},
	}
}

// diffStiffMeasureStrings は _measure・_ratio・_nominal_hits・_no_chains に通す手書きの文 (漢字・平仮名・句読点・記号・数字の範囲の端を含む)。
func diffStiffMeasureStrings() []string {
	out := []string{"", " ", "あ", "漢", "A", "1", "_", "。", "、。，．！？!?「」『』（）()…・", "【】〈〉《》[]{}<>;:,.-_", "a_b", "٣", "ⅷ", "½", "²", "③", "㈠", "𠀋", "𪚥",
		"㏿㐀䶿䷀䷿一鿿ꀀ\uf8ff豈\ufaffﬀ〄々〆〇", "\u3040ぁゖ\u3097\u3099ゝゞ゠",
		"漢字と ひらがな と カタカナ と ABC と 123", "営業部の今期の売上", "一日の作業の確認の手順", "運用管理の設定変更の確認手順", "設定の変更の確認", "運用管理の設定変更の確認手順の整理の方法",
		"保存が可能です。確認を実施します。", "保存することが可能です。", "不可能を可能にする。", "を行うを行う", "に関してに関して", "の観点からの観点で", "を図りを図っを図れ", "に資するに資し", "につながるにつながり",
		"\n", "漢\n字", "営業部の今期の\n売上", "営業部\nの今期の売上",
	}
	for _, p := range diffStiffSoft {
		out = append(out, p[0], p[1])
	}
	out = append(out, diffStiffNominal...)
	out = append(out, diffStiffChains...)
	out = append(out, diffStiffNeutral...)
	return out
}

// diffStiffCases は群 Z の case。
func diffStiffCases(pairs [][3]string) []VisCase {
	cases := []VisCase{{Name: "stiff/constants", Calls: diffConstantCalls(diffStiffConstants)}}
	for _, b := range diffBags(pairs) {
		cases = append(cases, VisCase{Name: "stiff/" + b.name, Calls: diffStiffStageCalls(b.old, b.new, diffStiffDocTypes[:1], false)})
	}
	for _, g := range []struct {
		prefix string
		cases  []diffFullCase
	}{{"stiff/hand/", diffStiffHandCases()}, {"stiff/boundary/", diffStiffBoundaryCases()}, {"stiff/fuzz/", diffStiffFuzz()}} {
		for _, f := range g.cases {
			types, keep := diffStiffDocTypes[:1], false
			if g.prefix == "stiff/hand/" {
				// 結果そのものを記録に残すのは手書きの組だけ (食い違ったときに読む。ほかは digest だけで、記録が数 MB 膨らむため)
				types, keep = diffStiffDocTypes, true
			}
			cases = append(cases, VisCase{Name: g.prefix + f.name, Calls: diffStiffStageCalls(f.old, f.new, types, keep)})
		}
	}
	calls := []Call{diffStiffLoadNominal()}
	for _, s := range diffStiffMeasureStrings() {
		calls = append(calls, diffStiffSentenceCalls(s, true)...)
	}
	for _, k := range []int{0, 1, 2, 3, 5, 7, 10, 25, 33, 50, 100} {
		for _, c := range []int{0, 1, 3, 7, 10, 25, 33, 100, 300} {
			r := dcall("_ratio", k, c)
			r.Keep = true
			calls = append(calls, r)
		}
	}
	for _, p := range diffStiffExcerptPairs() {
		for _, width := range []int{40, 1, 5, 12} {
			c := dcall("_changed_excerpt", p[0], p[1], width)
			c.Keep = true
			calls = append(calls, c)
		}
		calls = append(calls, dcall("_changed_excerpt", p[0], p[1]))
	}
	// 節の端の読点と「，」の組み合わせ (strip の字が「、」だけだと、端の「，」が残る)。既存の呼び出しの後ろに足す
	for _, p := range [][2]string{{"，，あ", "，、い"}, {"あ，", "い，，"}, {"，，あ，，", "，，い，，"}, {"、、あ", "、，い"}} {
		for _, width := range []int{40, 1, 5, 12} {
			c := dcall("_changed_excerpt", p[0], p[1], width)
			c.Keep = true
			calls = append(calls, c)
		}
		calls = append(calls, dcall("_changed_excerpt", p[0], p[1]))
	}
	cases = append(cases, VisCase{Name: "stiff/sentences", Calls: calls})
	for _, f := range diffStiffBrokenCases() {
		cases = append(cases, VisCase{Name: "stiff/broken/" + f.name, Calls: diffStiffStageCalls(f.old, f.new, []any{f.docType}, true)})
	}
	return cases
}
