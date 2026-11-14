package oracle

import (
	"math/rand/v2"
	"strconv"
	"strings"
)

// 群 U (確度) の記録: 段 _certainty_risks(sides, groups, risks, masks, doc_type) と、その下の関数と定数。
// 組は、群 M までと同じ入れ物 (diffBags) に、専用の手書きの組と固定の種 (PCG 1076/26・27) の組を足したもの。

// diffCertaintyConstants は群 U の Go の表と比べる本家の定数 (正規表現は文字列と flags)。
var diffCertaintyConstants = []string{
	"_CERTAINTY_VERB", "_PREDICATE_END", "_PREDICATE_STEM", "_PREDICATE_BEFORE", "_YOTEI", "_MIKOMI", "_APPEARANCE", "_CERTAINTY_TABLE", "_COUNTED_FROM",
	"_CERTAINTY_NEG_AFTER", "_CERTAINTY_NEG_FORMS", "_CERTAINTY_QUESTION", "_HEDGE_CONDITION", "_HEDGE_CLAUSE_STOP", "_HEDGE_CONTENT",
	"_COUNTED_BY_DOC_TYPE_FROM", "_CERTAINTY_FREQUENCY", "_CERTAINTY_DUTY", "COUNTED_TYPES",
}

// DiffCertaintyFunctions は群 U の関数。
var DiffCertaintyFunctions = []string{
	"_certainty_risks", "_certainty_words", "_certainty_word_spans", "_certainty_negations", "_cancel_same_level", "_cancel_pairs", "_hedge_conditions", "_hedge_clause", "_hedge_polarity", "_classify_hedge_marks", "_certainty_change", "_unpaired_neighbors",
}

// DiffCertaintyConstantNames は定数の名前 (Go の突き合わせが、この名前の Get を回す)。
func DiffCertaintyConstantNames() []string { return diffCertaintyConstants }

// diffCertaintyBank は確度の語ごとの文 (表の 1 行ごとに当たる文と、近くて当たらない文)。
var diffCertaintyBank = []string{
	// 強さ 1
	"遅れる可能性がある。", "遅れる可能性もあります。", "可能性はある。", "可能性があり、点検する。", "可能性が考えられる。", "可能性が残っている。", "可能性を否定できない。", "可能性を捨てきれない。", "可能性を拭えない。", "可能性が存在する。", "可能性はございます。",
	"遅れるかもしれない。", "遅れるかもしれません。", "遅れるかも知れない。", "遅れるかもしれず、確認する。", "遅れるかもしれなかった。", "遅れるかもしれなければ待つ。",
	"おそれがある。", "恐れがあります。", "虞がある。", "おそれも残る。", "恐れはない。", "恐れ入ります。",
	"リスクがある。", "リスクも伴う。", "リスクが高い。", "リスクが残る。", "リスクはない。",
	"懸念がある。", "懸念も残る。", "懸念され、確認する。", "懸念はない。",
	"起こり得る。", "起こり得ます。", "起こりうる。", "あり得る。", "ありうる。", "壊れ得ない。", "得る物がある。", "起こり得ません。",
	"遅れかねない。", "遅れかねません。", "遅れかねず、困る。", "かねてから準備する。",
	"必ずしも安全ではない。", "安全とは限らない。", "安全とは限らず、注意する。", "安全とは限りません。", "安全とは限らなかった。",
	"遅れることがある。", "遅れることもある。", "遅れることがあった。", "遅れたことがある。", "遅れったことがある。", "遅れだことがある。", "ケースがある。", "ときが出る。", "ときもある。", "ケースもあり、確認する。",
	"遅れる場合がある。", "遅れる場合もある。", "場合があった。", "場合があり、確認する。",
	"検討中です。", "検討している。", "検討しております。", "検討を進める。", "検討を続けます。", "検討を始める。", "検討する。",
	// 強さ 2
	"遅れる可能性が高い。", "可能性は高い。", "可能性も大きい。", "可能性が十分ある。", "可能性が低い。",
	"遅れると思われる。", "遅れるとおもわれます。", "遅れると考えられる。", "遅れると考えられます。", "遅れると見られる。", "遅れるとみられる。", "遅れると見ている。", "遅れるとみています。", "遅れると見ており、準備する。",
	"遅れる見込みです。", "遅れる見込みだ。", "見込みを立てる。", "見込み客が来る。", "遅れる見込まれる。", "遅れると見込まれます。", "見込まれせる。", "見込んで準備する。", "見込む。", "見込める。", "見込めます。", "見込みます。", "売上の見込みを立てる。", "見込みが高い。", "見込みが立たない。", "見込みは薄い。", "見込みもある。",
	"遅れると予想される。", "予想して準備する。", "予想します。", "予想する。", "予想です。", "予想だ。", "予想であり、確認する。", "予想では遅れる。", "予測される。", "予測して準備している。", "予測です。",
	"遅れると想定される。", "想定して準備している。", "想定します。", "想定です。", "想定では遅れる。", "想定外の事態になる。",
	"見通しです。", "見通しだ。", "見通しが立つ。", "見通しで、遅れる。", "見通しを立てる。",
	"推測される。", "推定される。", "推察して準備している。", "推測します。", "推測です。", "推定だ。",
	"公算が大きい。", "公算は高い。", "公算も強い。", "公算がある。",
	"遅れるでしょう。", "遅れるでしょうか。", "遅れるだろう。", "遅れるだろうか。", "遅れるであろう。",
	"遅れるはずだ。", "遅れるはずです。", "遅れるはずで、準備する。", "遅れるはずでした。", "遅れるはずだった。", "遅れるはずの便。", "はずがない。", "はずを整える。",
	"おそらく遅れる。", "恐らく遅れる。", "たぶん遅れる。", "多分遅れる。", "多分野にわたる。",
	"示唆する。", "示唆される。", "示唆して準備する。", "示唆し、確認する。", "示唆に富む。",
	"遅れそうです。", "遅れそうだ。", "遅れそうな便。", "遅れそうに見える。", "遅れそうでした。", "遅れそうだった。", "遅れそうでして、準備する。", "遅れそうです",
	"遅れるようだ。", "遅れるようです。", "遅れるようでした。", "遅れるようだった。", "遅れるようである。", "遅れるようだね。", "このようだ。", "遅れるらしい。", "遅れるらしかった。", "遅れるらしいね。", "女らしい。", "遅れるらしいです。",
	"遅れるみたいだ。", "遅れるみたいです。", "遅れるみたいでした。", "遅れるみたいだった。",
	"遅れる傾向がある。", "遅れる傾向にある。", "遅れる傾向が見られる。", "傾向がない。",
	"効果が期待できる。", "効果が期待される。", "効果を期待しています。", "期待します。", "期待する。", "期待できます。", "期待して待つ。",
	"遅れると思います。", "遅れると思う。", "遅れると思る。", "遅れると思ます。", "遅れると思っています。", "遅れると考える。", "遅れると考えています。", "遅れると考えており、準備する。", "遅れると思っていました。", "遅れると思っておりました。", "遅れると思って、準備する。", "思うのは遅れることだ。", "遅れるとおもっている。", "遅れると考えております。",
	// 強さ 3
	"来月に修正する予定です。", "予定です。", "予定だ。", "予定である。", "予定でした。", "予定だった。", "予定でござる。", "予定となる。", "予定として扱う。", "予定され、確認する。", "予定になる。", "予定にします。", "予定。", "予定",
	"予定して準備する。", "予定しております。", "予定します。", "予定する。", "予定される。", "予定をします。", "リリース予定の機能。", "修正する予定で、準備する。", "修正する予定で作業する。", "来週までの予定で、進める。", "会議の予定を調整する。", "予定表を見る。", "予定外の作業。", "予定より遅れる。", "予定通りに進む。", "予定どおりです。", "予定では遅れる。",
	"見直す計画です。", "見直す計画だ。", "見直す計画で、進める。", "見直す計画である。", "見直す計画がある。", "見直す計画もある。", "見直し計画中だ。", "計画して準備する。", "計画しております。", "計画中です。", "計画を立てる。",
	"見直す方針です。", "見直す方針だ。", "見直す方針で進める。", "見直す方針である。", "方針を決める。", "方針です。",
	"見直す方向です。", "見直す方向だ。", "見直す方向で進める。", "見直す方向である。", "方向を決める。",
	"見直すつもりです。", "見直すつもりだ。", "見直すつもりで進める。", "つもりを話す。",
	// 強さ 5
	"確実に遅れる。", "確実だ。", "確実です。", "確実で、安全だ。", "確実な方法を使う。", "ほぼ確実に遅れる。", "ほとんど確実だ。", "確実性を上げる。",
	"必ず遅れる。", "必ずしも遅れない。", "必ず、確認する。", "必ず実施する。", "必ずです。",
	"間違いなく遅れる。", "ほぼ間違いなく遅れる。", "間違いありません。", "間違いない。", "間違いを直す。",
	"確定です。", "確定だ。", "確定である。", "確定済みだ。", "確定している。", "確定しています。", "確定しており、進める。", "確定しました。", "確定した。", "確定しました。ら、進める。", "確定したら進める。", "確定する。",
	"絶対に遅れる。", "絶対に守る。", "絶対だ。",
	// 否定・問い・条件・頻度・決まり
	"遅れる可能性はない。", "遅れる可能性は低い。", "遅れる可能性は小さい。", "遅れる可能性は少ない。", "遅れる可能性は薄い。", "遅れる可能性はほぼない。", "遅れる可能性がありません。", "遅れる可能性もなくなる。",
	"可能性は低くない。", "遅れる見込みはない。", "遅れる見込みは薄い。", "遅れる見込みは低い。", "遅れる見込みがありません。", "予定はない。", "予定がありません。", "はずがない。", "はずはありません。", "おそれはない。", "リスクは低い。", "リスクは小さい。", "リスクは少ない。", "懸念はない。",
	"起こり得ない。", "なり得ません。", "あり得ません。", "し得ない。", "し得ません。", "なりえない。", "起こりえません。",
	"遅れる可能性はありますか。", "遅れるでしょうか。", "遅れるでしょうか？", "遅れるでしょうか?」", "遅れるでしょうか。」", "遅れる可能性がありますか？", "遅れるのか。", "遅れると思いますか。",
	"遅れる可能性があれば連絡する。", "遅れる可能性があるなら連絡する。", "遅れる可能性がある場合は連絡する。", "遅れる可能性があるときは連絡する。", "遅れる可能性があったら連絡する。", "遅れる可能性があると連絡する。",
	"遅れる可能性があれば、連絡する。", "遅れるおそれがあれば連絡する。", "遅れるかもしれず、遅れる場合は連絡する。",
	"まれに遅れることがある。", "稀に遅れる場合がある。", "ときどき遅れることがある。", "時々遅れる場合がある。", "時折遅れることがある。", "ときおり遅れる場合がある。", "たまに遅れることがある。", "一部の環境で遅れることがある。", "環境によっては遅れる場合がある。", "場合によって遅れる場合がある。", "遅れることもある。",
	"手順を実施すること。", "必須にする。", "必須です。", "実施するものとする。", "実施するものとします。", "実施してください。", "実施しなければならない。", "実施する必要がある。", "実施するべきだ。", "お願いします。",
	"設定を開きます。", "結果は3件でした。", "確認します。", "実施します。", "確認する。", "確認した。",
}

// diffCertaintyExtraBank は、確度の語のすぐあとに否定が続く文 (推量の語の範囲と、そのすぐあとの否定を数えない所を狙う)。
// bank と別に持つのは、bank の並びが変わると、乱数で選んだ組の case が全部変わるため。
var diffCertaintyExtraBank = []string{
	"遅れる可能性も否定できない。", "遅れる可能性は否定できません。", "可能性が捨てきれず、遅れない。", "可能性も拭えないので止まらない。", "止まらない可能性もある。", "止まらない可能性も否定できない。", "止まるかもしれないが、続かない。",
}

// diffCertaintyContexts は組の文の前後に置く、組にならなかった文 (確度の語を持つもの・持たないもの・元の文と同じ字を共有するもの)。
var diffCertaintyContexts = []string{
	"前の文です。", "次の文です。", "必ず確認する。", "必ず実施する。", "確実に遅れる。", "確実です。", "遅れる可能性がある。", "遅れる見込みです。", "来月に修正する予定です。", "予定です。", "遅れるでしょう。",
	"遅れるかもしれない。", "遅れることがある。", "間違いなく遅れる。", "設定を開きます。", "遅れる。", "確認する。",
}

func diffCertaintyHandPairs() []diffFullCase {
	doc := diffDoc
	return []diffFullCase{
		{name: "same", old: doc("遅れる可能性がある。"), new: doc("遅れる可能性がある。")},
		{name: "strengthen-possibility", old: doc("遅れる可能性があります。"), new: doc("遅れます。")},
		{name: "strengthen-mikomi", old: doc("遅れる見込みです。"), new: doc("遅れます。")},
		{name: "strengthen-schedule", old: doc("見直す予定です。"), new: doc("見直します。")},
		{name: "strengthen-must", old: doc("遅れます。"), new: doc("必ず遅れます。")},
		{name: "strengthen-level", old: doc("遅れるかもしれません。"), new: doc("遅れる見込みです。")},
		{name: "weaken-level", old: doc("必ず遅れます。"), new: doc("遅れるかもしれません。")},
		{name: "weaken-from-assertion", old: doc("遅れます。"), new: doc("遅れるかもしれません。")},
		{name: "mixed", old: doc("遅れるかもしれず、必ず直る。"), new: doc("遅れ、直るかもしれない。")},
		{name: "same-level-rename", old: doc("遅れる可能性がある。"), new: doc("遅れるかもしれない。")},
		{name: "question-skip", old: doc("遅れる可能性がありますか。"), new: doc("遅れますか。")},
		{name: "question-hou", old: doc("遅れるでしょうか。"), new: doc("遅れます。")},
		{name: "negation-same", old: doc("遅れる可能性はない。"), new: doc("遅れない。")},
		{name: "negation-differs", old: doc("遅れる可能性がある。"), new: doc("遅れる可能性はない。")},
		{name: "noncore-frequency", old: doc("遅れることがある。"), new: doc("遅れます。")},
		{name: "noncore-case", old: doc("遅れる場合がある。"), new: doc("遅れます。")},
		{name: "noncore-frequent-rewrite", old: doc("遅れる場合がある。"), new: doc("まれに遅れます。")},
		{name: "added-must-duty", old: doc("必須にする。"), new: doc("必ず実施する。")},
		{name: "added-must-plain", old: doc("実施する。"), new: doc("必ず実施する。")},
		{name: "added-must-sentence", old: doc("確認します。"), new: doc("確認します。必ず実施します。")},
		{name: "neighbor-cancel", old: doc("遅れる可能性がある。点検することがある。"), new: doc("遅れる可能性があり、点検する予定です。")},
		{name: "context-cancel-old", old: doc("遅れる可能性がある。確認する。"), new: doc("遅れる。確認する可能性がある。")},
		{name: "context-cancel-new", old: doc("遅れる。確認する可能性がある。"), new: doc("遅れる可能性がある。確認する。")},
		{name: "split-sentence-must", old: doc("確認して実施します。"), new: doc("確認します。必ず実施します。")},
		{name: "split-sentence-twice", old: doc("確認して実施して報告します。"), new: doc("確認します。必ず実施します。確実に報告します。")},
		{name: "multi-pair-context", old: doc("A を見る。B を見る。C を見る。"), new: doc("A を必ず見る。B を見る。C を確実に見る。")},
		{name: "duty-neighbor-after", old: doc("確認します。必須にする。"), new: doc("必ず確認します。")},
		{name: "duty-neighbor-before", old: doc("必須にする。確認します。"), new: doc("必ず確認します。")},
		{name: "split-shared-neighbor", old: doc("一つ目を確認する。二つ目を確認する。"), new: doc("一つ目を確認します。必ず確認する。二つ目を確認します。")},
		{name: "split-shared-neighbor-lines", old: doc("一つ目を確認する。", "二つ目を確認する。"), new: doc("一つ目を確認します。", "必ず確認する。", "二つ目を確認します。")},
		{name: "marks-hedge-suppressed", old: doc("遅れるようです。"), new: doc("遅れます。")},
		{name: "marks-opinion-suppressed", old: doc("遅れると思います。"), new: doc("遅れます。")},
		{name: "marks-weaken-suppressed", old: doc("遅れます。"), new: doc("遅れると思います。")},
		{name: "marks-other-line", old: doc("遅れるようです。", "遅れる可能性がある。"), new: doc("遅れるようです。", "遅れます。")},
		{name: "marks-different-sentence", old: doc("遅れるようです。それは遅れる可能性がある。"), new: doc("遅れるようです。それは遅れます。")},
		{name: "heading", old: doc("# 遅れる可能性がある"), new: doc("# 遅れる")},
		{name: "list", old: doc("- 遅れる可能性がある。"), new: doc("- 遅れる。")},
		{name: "quote", old: doc("> 遅れる可能性がある。"), new: doc("> 遅れる。")},
		{name: "table", old: doc("| 項目 | 値 |", "|---|---|", "| 遅れる可能性がある | 注意 |"), new: doc("| 項目 | 値 |", "|---|---|", "| 遅れる | 注意 |")},
		{name: "inline-code", old: doc("`可能性がある`と遅れる可能性がある。"), new: doc("`可能性がある`と遅れる。")},
		{name: "multi-hunk", old: doc("遅れる可能性がある。", "同じ行です。", "予定です。"), new: doc("遅れる。", "同じ行です。", "必ず。")},
		{name: "run-shifted", old: doc("遅れる可能性がある。", "", "見込みです。"), new: doc("遅れる。見込みです。", "", "確認する。")},
		{name: "crlf", old: "遅れる可能性がある。\r\n", new: "遅れる。\r\n"},
		{name: "empty-old", old: "", new: doc("必ず遅れる。")},
		{name: "empty-new", old: doc("遅れる可能性がある。"), new: ""},
	}
}

// diffCertaintyFuzz は固定の種 (PCG 1076/26) から作った、確度の語の文を入れ替える・足す・消す組 (組にならなかった文を前後に置く)。
func diffCertaintyFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 26))
	wrap := []string{"{s}", "{s}", "{s}", "- {s}", "## {s}", "> {s}", "| {s} | x |", "前の文です。{s}", "{s}後の文です。"}
	var cases []diffFullCase
	for n := range 2600 {
		var old, new []string
		for range 1 + r.IntN(4) {
			w := randPick(r, wrap)
			o, nw := randPick(r, diffCertaintyBank), randPick(r, diffCertaintyBank)
			if r.IntN(3) == 0 {
				// 同じ文を言い換える (文の頭を共有して、書き換えた先に残る)
				nw = strings.TrimSuffix(o, "。") + randPick(r, []string{"。必ず実施する。", "。確実です。", "。間違いなく遅れる。", "。", "か。", "。遅れる可能性がある。"})
			}
			old, new = append(old, diffTemplate(w, "s", o)), append(new, diffTemplate(w, "s", nw))
			if r.IntN(3) == 0 {
				c := randPick(r, diffCertaintyContexts)
				if r.IntN(2) == 0 {
					old = append(old, c)
				} else {
					new = append(new, c)
				}
			}
		}
		cases = append(cases, diffFullCase{name: "fuzz-" + strconv.Itoa(n), old: diffContrastJoinSame(r, old), new: diffContrastJoinSame(r, new)})
	}
	return cases
}

// diffContrastJoinSame は文を同じ行につなぐ・行を分ける (行頭の記号のある文は必ず行を分ける)。
func diffContrastJoinSame(r *rand.Rand, sentences []string) string {
	return diffContrastJoin(r, sentences)
}

// diffCertaintyMixedFuzz は固定の種 (PCG 1076/28) から作った、強さの違う語を 2 つずつ持つ文を入れ替えて、強まる段と弱まる段を同時に作る組。
func diffCertaintyMixedFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 28))
	levels := [][]string{
		{"遅れる可能性がある。", "遅れるかもしれない。", "おそれがある。", "リスクがある。"},
		{"遅れると思われる。", "遅れるでしょう。", "遅れる見込みです。", "遅れるはずだ。", "おそらく遅れる。"},
		{"来月に修正する予定です。", "見直す計画です。", "見直す方針です。"},
		{"必ず遅れる。", "確実に遅れる。", "間違いなく遅れる。", "絶対に遅れる。"},
		{"遅れる。", "確認します。", "直ります。"},
	}
	pick := func() string { l := levels[r.IntN(len(levels))]; return randPick(r, l) }
	var cases []diffFullCase
	for n := range 400 {
		// 1 つの文に 2 つの語を持たせる (確度は文の組ごとに比べるので、読点でつなぐ)
		old := strings.TrimSuffix(pick(), "。") + "、" + pick()
		new := strings.TrimSuffix(pick(), "。") + "、" + pick()
		if r.IntN(3) == 0 {
			cases = append(cases, diffFullCase{name: "mixed-lines-" + strconv.Itoa(n), old: diffDoc(old), new: diffDoc(new, pick())})
			continue
		}
		cases = append(cases, diffFullCase{name: "mixed-" + strconv.Itoa(n), old: diffDoc(old), new: diffDoc(new)})
	}
	return cases
}

func diffCertaintyBankCases() []diffFullCase {
	var cases []diffFullCase
	for i, s := range diffCertaintyExtraBank {
		cases = append(cases, diffFullCase{name: "extra-" + strconv.Itoa(i), old: diffDoc(s), new: diffDoc("遅れます。")},
			diffFullCase{name: "extra-rev-" + strconv.Itoa(i), old: diffDoc("遅れます。"), new: diffDoc(s)})
	}
	for i, s := range diffCertaintyBank {
		base := diffFullCase{name: "bank-" + strconv.Itoa(i), old: diffDoc(s), new: diffDoc("遅れます。")}
		cases = append(cases, base,
			diffFullCase{name: "bank-rev-" + strconv.Itoa(i), old: diffDoc("遅れます。"), new: diffDoc(s)},
			diffFullCase{name: "bank-two-" + strconv.Itoa(i), old: diffDoc(s, "確認します。"), new: diffDoc("遅れます。確認します。")})
	}
	return cases
}

// diffCertaintyDocTypes は _certainty_risks に渡す文書の種類 (nil は指定なし。countedTypes のうち 2 つと、それ以外)。
var diffCertaintyDocTypes = []any{nil, "article", "incident", "mail"}

// diffCertaintyStageCalls は組 1 つを、本家の diff() と同じ順で作らせて (覚える)、_certainty_risks に通す呼び出し (種類は types の分だけ)。
func diffCertaintyStageCalls(old, new string, types []any, keep bool) []Call {
	save := diffSave
	ops := dcall("_ExactSequenceMatcher", nil, diffRef("o"), diffRef("r"))
	ops.Kwargs = map[string]any{"autojunk": false}
	ops.Method = "get_opcodes"
	calls := []Call{
		save(dcall("_build_full_diff", old, new, nil), "full"),
		save(dcall("_sentence_pairs", diffFullRef(1), diffFullRef(0)), "groups"),
		save(dcall("normalize", old), "o"), save(dcall("normalize", new), "r"),
		save(ops, "ops"),
		save(dcall("_type_changes", diffRef("o"), diffRef("r"), diffRef("ops")), "tc"),
		save(dcall("_marks_risks", diffFullRef(1), diffRef("tc")), "marks"),
	}
	for _, t := range types {
		c := dcall("_certainty_risks", diffFullRef(1), diffRef("groups"), diffRef("marks"), nil, t)
		c.Keep = keep
		calls = append(calls, c)
	}
	return calls
}

// diffCertaintyTexts は下の関数に通す文 (表の文と、文の組み合わせ)。
func diffCertaintyTexts() []string {
	r := rand.New(rand.NewPCG(1076, 27))
	texts := append([]string{}, diffCertaintyBank...)
	for range 900 {
		a, b := randPick(r, diffCertaintyBank), randPick(r, diffCertaintyBank)
		texts = append(texts, a+b, a+"\n"+b)
	}
	return texts
}

// diffCertaintyStringCalls は確度の下の関数の呼び出し (文と、別の文の組から作る。結果そのものを記録に残すかは keep)。
func diffCertaintyStringCalls(texts []string, others []string, keep bool) []Call {
	var calls []Call
	add := func(c Call) {
		c.Keep = keep
		calls = append(calls, c)
	}
	kinds := [][2][]any{
		{{map[string]any{"kind": "推量"}}, {}}, {{}, {map[string]any{"kind": "推量"}}}, {{map[string]any{"kind": "推量"}}, {map[string]any{"kind": "意見"}}},
		{{map[string]any{"kind": "意見"}}, {}}, {{}, {}},
	}
	for i, text := range texts {
		other := others[i]
		sa := dcall("_certainty_word_spans", text)
		sa.Save, sa.Quiet = "spans", true
		sb := dcall("_certainty_word_spans", other)
		sb.Save, sb.Quiet = "spansB", true
		calls = append(calls, sa, sb)
		add(dcall("_certainty_words", text))
		add(dcall("_certainty_word_spans", text))
		add(dcall("_certainty_negations", text, diffRef("spans")))
		add(dcall("_hedge_conditions", text, diffRef("spans")))
		add(dcall("_hedge_clause", text, 0))
		add(dcall("_hedge_clause", text, len([]rune(text))))
		for _, start := range []int{3, 8} {
			if start < len([]rune(text)) {
				add(dcall("_hedge_clause", text, start))
			}
		}
		add(dcall("_cancel_same_level", diffRef("spans"), diffRef("spansB")))
		add(dcall("_hedge_polarity", text, map[string]any{"$ref": "spans", "path": []any{0}}, diffRef("spans")))
		add(dcall("_cancel_pairs", diffRef("spans"), diffRef("spansB")))
		for _, k := range kinds {
			add(dcall("_classify_hedge_marks", text, other, k[0], k[1]))
		}
		for _, dt := range diffCertaintyDocTypes {
			add(dcall("_certainty_change", text, other, []any{}, []any{}, []any{}, dt, false))
		}
		add(dcall("_certainty_change", text, other, []any{}, []any{}, []any{}, "article", true))
	}
	return calls
}

// diffCertaintySmallCalls は _unpaired_neighbors の手書きの呼び出し (結果そのものを記録に残す)。
func diffCertaintySmallCalls() []Call {
	var calls []Call
	long := "字" + strings.Repeat("あ", 150) + "語"
	for _, start := range []int{151, 100, 160} {
		call := dcall("_hedge_clause", long, start)
		call.Keep = true
		calls = append(calls, call)
	}
	for _, c := range []struct {
		count, index int
		paired       []int
	}{{1, 0, nil}, {3, 1, nil}, {3, 1, []int{0, 2}}, {5, 2, []int{0, 4}}, {5, 2, []int{1}}, {5, 2, []int{3}}, {5, 0, []int{1}}, {5, 4, []int{3}}, {4, 3, []int{0, 1, 2}}} {
		p := make([]any, len(c.paired))
		for i, v := range c.paired {
			p[i] = v
		}
		call := dcall("_unpaired_neighbors", c.count, c.index, p)
		call.Keep = true
		calls = append(calls, call)
	}
	return calls
}

// diffCertaintyCases は群 U の case の一覧 (pairs は入力の集まりと diff の入力の組)。
func diffCertaintyCases(pairs [][3]string) []VisCase {
	cases := []VisCase{{Name: "certainty/constants", Calls: diffConstantCalls(diffCertaintyConstants)}, {Name: "certainty/small", Calls: diffCertaintySmallCalls()}}
	for _, b := range diffBags(pairs) {
		cases = append(cases, VisCase{Name: "certainty/" + b.name, Calls: diffCertaintyStageCalls(b.old, b.new, diffCertaintyDocTypes[:1], false)})
	}
	for _, g := range []struct {
		prefix string
		cases  []diffFullCase
		keep   bool
	}{{"certainty/hand/", diffCertaintyHandPairs(), true}, {"certainty/bank/", diffCertaintyBankCases(), false}, {"certainty/mixed/", diffCertaintyMixedFuzz(), false}, {"certainty/fuzz/", diffCertaintyFuzz(), false}} {
		for _, f := range g.cases {
			cases = append(cases, VisCase{Name: g.prefix + f.name, Calls: diffCertaintyStageCalls(f.old, f.new, diffCertaintyDocTypes, g.keep)})
		}
	}
	texts := diffCertaintyTexts()
	others := make([]string, len(texts))
	for i := range texts {
		others[i] = texts[(i*7+3)%len(texts)]
	}
	for i := 0; i < len(texts); i += 80 {
		j := min(i+80, len(texts))
		cases = append(cases, VisCase{Name: "certainty/strings/" + strconv.Itoa(i/80), Calls: diffCertaintyStringCalls(texts[i:j], others[i:j], false)})
	}
	extra := diffCertaintyExtraBank
	rotated := append(append([]string{}, extra[1:]...), extra[0])
	return append(cases, VisCase{Name: "certainty/strings/extra", Calls: diffCertaintyStringCalls(extra, rotated, false)})
}
