package oracle

import (
	"math/rand/v2"
	"strconv"
	"strings"
)

// 群 Y (意味が変わる恐れの組み立て) の記録: 公開の関数 meaning_risks と、本家の diff() と同じ形の段 _meaning_risks (sides.immutable・sides.quiet_marks と、
// そのあとに同じ sides で呼ぶ _hedges も)。組は、群 M までと同じ入れ物 (diffBags) に、専用の手書きの組と固定の種 (PCG 1076/50〜51) の組を足したもの。

// DiffMeaningFunctions は群 Y の関数。getattr は組み込みで、_meaning_risks が _Sides に置く immutable と quiet_marks を読むだけ。
var DiffMeaningFunctions = []string{"meaning_risks", "_meaning_risks", "getattr"}

// diffMeaningDocTypes は meaning_risks と _meaning_risks に渡す文書の種類 (nil は指定なし。article は確度の 2 つの強まりを数える側に入れる種類)。
var diffMeaningDocTypes = []any{nil, "article"}

// diffMeaningAttr は _Sides の属性を読む呼び出し (組み込みの getattr)。
func diffMeaningAttr(name string, keep bool) Call {
	return Call{Module: "builtins", Function: "getattr", Args: []any{diffFullRef(1), name}, Digest: true, Keep: keep}
}

// diffMeaningStageCalls は組 1 つを、本家の diff() と同じ順で作らせて (覚える)、_meaning_risks とそのあとの _hedges に通す呼び出し (種類は types の分だけ。
// 文書の種類ごとに同じ sides で呼び直すので、sides.quiet_marks は直前の呼び出しのもの)。public が真なら、公開の meaning_risks も type_changes なし・ありで呼ぶ。
func diffMeaningStageCalls(old, new string, types []any, public, keep bool) []Call {
	save := diffSave
	sides, groups := diffFullRef(1), diffRef("groups")
	ops := dcall("_ExactSequenceMatcher", nil, diffRef("o"), diffRef("r"))
	ops.Kwargs = map[string]any{"autojunk": false}
	ops.Method = "get_opcodes"
	calls := []Call{
		save(dcall("_build_full_diff", old, new, nil), "full"),
		save(dcall("_sentence_pairs", diffFullRef(1), diffFullRef(0)), "groups"),
		save(dcall("normalize", old), "o"), save(dcall("normalize", new), "r"), save(ops, "ops"),
		save(dcall("_type_changes", diffRef("o"), diffRef("r"), diffRef("ops")), "tc"),
		save(dcall("_modality_changes", diffRef("o"), diffRef("r")), "mod"),
		save(dcall("_logic_changes", diffRef("o"), diffRef("r")), "lg"),
	}
	for _, dt := range types {
		mr := dcall("_meaning_risks", sides, diffFullRef(0), diffRef("tc"), groups, dt, diffRef("mod"), diffRef("lg"))
		mr.Save, mr.Keep = "mr", keep
		calls = append(calls, mr, diffMeaningAttr("immutable", keep), diffMeaningAttr("quiet_marks", keep), withKeep(keep, dcall("_hedges", sides, groups, diffRef("mr"))))
	}
	// type_changes が None の形 (文の種類の印・許可・能力・選ぶ範囲・因果を含めない)
	calls = append(calls, withKeep(keep, dcall("_meaning_risks", sides, diffFullRef(0), nil, groups, nil, nil, nil)))
	if public {
		calls = append(calls, withKeep(keep, dcall("meaning_risks", old, new)))
		for _, dt := range types {
			c := dcall("meaning_risks", old, new, nil, diffRef("tc"))
			c.Kwargs = map[string]any{"doc_type": dt}
			calls = append(calls, withKeep(keep, c))
		}
	}
	return calls
}

func diffMeaningHandPairs() []diffFullCase {
	doc := diffDoc
	return []diffFullCase{
		{name: "same", old: doc("遅れます。"), new: doc("遅れます。")},
		{name: "hedge-strengthened", old: doc("遅れると思われる。"), new: doc("遅れます。")},
		{name: "hedge-strengthened-possible", old: doc("遅れるかもしれない。"), new: doc("遅れます。")},
		{name: "hedge-same-level", old: doc("遅れる可能性がある。"), new: doc("遅れるかもしれない。")},
		{name: "hedge-same-level-article", old: doc("遅れる可能性がある。"), new: doc("遅れるかもしれません。")},
		{name: "hedge-weakened", old: doc("遅れます。"), new: doc("遅れると思われる。")},
		{name: "hedge-lost-not-certain", old: doc("遅れるようです。"), new: doc("遅れます。")},
		{name: "hedge-opinion", old: doc("遅れると思います。"), new: doc("遅れます。")},
		{name: "hedge-negation", old: doc("安全とは限らない。"), new: doc("安全です。")},
		{name: "hedge-question", old: doc("遅れるでしょうか。"), new: doc("遅れます。")},
		{name: "hedge-condition", old: doc("遅れるなら、可能性がある。"), new: doc("遅れるなら、遅れます。")},
		{name: "hedge-other-line", old: doc("遅れるようです。", "遅れる可能性がある。"), new: doc("遅れるようです。", "遅れます。")},
		{name: "hedge-quiet-covers-hedges", old: doc("遅れると考えられる。"), new: doc("遅れる見込みです。")},
		{name: "hedge-quiet-covers-state", old: doc("原因は未確認と思われる。"), new: doc("原因は対策済みと考えられる。")},
		{name: "hedge-quiet-state-two", old: doc("原因は未確認と思われる。日程は未定と思われる。"), new: doc("原因は対策済みと考えられる。日程は決まったと考えられる。")},
		{name: "hedge-two-sentences", old: doc("遅れると思われる。直る見込みです。"), new: doc("遅れます。直ります。")},
		{name: "hedge-state-lost", old: doc("原因は未確認で、遅れる可能性がある。"), new: doc("遅れます。")},
		{name: "hedge-state-kept", old: doc("原因は未確認で、遅れる可能性がある。"), new: doc("原因は未確認です。遅れます。")},
		{name: "state-lost-certainty-review", old: doc("原因は未確認で、必ず遅れる。"), new: doc("遅れます。")},
		{name: "state-lost-weaken", old: doc("原因は不明です。"), new: doc("原因は不明かもしれない。")},
		{name: "citation-number", old: doc("詳しくは[1]を見る。"), new: doc("詳しくは[2]を見る。")},
		{name: "citation-author", old: doc("詳しくは(田中, 2019)を見る。"), new: doc("詳しくは(山田, 2019)を見る。")},
		{name: "citation-lost", old: doc("詳しくは[1]を見る。"), new: doc("詳しくは見る。")},
		{name: "number-and-qualifier", old: doc("約120人が来る。"), new: doc("120人が来る。")},
		{name: "number-changed", old: doc("120人が来る。"), new: doc("130人が来る。")},
		{name: "negation", old: doc("保存します。"), new: doc("保存しません。")},
		{name: "swap", old: doc("管理者が利用者を呼ぶ。"), new: doc("利用者が管理者を呼ぶ。")},
		{name: "question", old: doc("確認しますか。"), new: doc("確認します。")},
		{name: "protected", old: doc("`code` を使う。", "https://a.example/ を見る。"), new: doc("`code2` を使う。", "https://b.example/ を見る。")},
		{name: "contrast", old: doc("速度ではなく品質を重視する。"), new: doc("品質ではなく速度を最優先する。")},
		{name: "feeling", old: doc("結果を見た。"), new: doc("結果を見て、うれしかった。")},
		{name: "quote", old: doc("彼は「はい」と答えた。"), new: doc("彼は「いいえ」と答えた。")},
		{name: "modality-permission", old: doc("保存してもよい。"), new: doc("保存する。")},
		{name: "modality-ability", old: doc("保存できる。"), new: doc("保存する。")},
		{name: "choice", old: doc("どれか一つを選ぶ。"), new: doc("すべてを選ぶ。")},
		{name: "causality", old: doc("遅れるのは関連がある。"), new: doc("遅れるのは原因だ。")},
		{name: "promise-claim", old: doc("遅れました。"), new: doc("障害により遅れました。再発防止に努めます。")},
		{name: "everything", old: doc("原因は未確認です。遅れる可能性がある。120人が来る。保存してもよい。"), new: doc("障害により遅れました。必ず遅れる。130人が来る。保存する。再発防止に努めます。")},
		{name: "heading-and-table", old: doc("# 調査中", "| a | b |", "|---|---|", "| 遅れる可能性がある | 3 |"), new: doc("# 完了", "| a | b |", "|---|---|", "| 遅れる | 4 |")},
		{name: "crlf", old: "原因は未確認です。\r\n", new: "原因は対策しました。\r\n"},
		{name: "empty-old", old: "", new: doc("障害により遅れました。")},
		{name: "empty-new", old: doc("原因は未確認です。"), new: ""},
	}
}

// diffMeaningBank は、いろいろな群の文を 1 つにまとめた銀行 (組み立てが集める候補が重なる文書を作る)。
func diffMeaningBank() []string {
	var bank []string
	for _, b := range [][]string{diffNumberBank, diffNegationBank, diffCertaintyBank, diffAdditionStateBank, diffAdditionKeptBank, diffAdditionPromiseBank, diffAdditionCausalBank,
		diffAdditionFutureBank, diffAdditionNeutralBank, diffFeelingBank, diffQuestionBank, diffQualifierBank, diffQuoteBank, diffReviewHedgeBank, diffModalityBank,
		diffContrastHandSentences, diffTypeBank, diffChoiceOne, diffChoiceAll, diffRelations, diffCauses} {
		bank = append(bank, b...)
	}
	for _, v := range diffReviewValues[:40] {
		bank = append(bank, diffTemplate("{v}を参照する。", "v", v))
	}
	return bank
}

// diffMeaningFuzz は固定の種 (PCG 1076/50) から、銀行の文を 2〜5 文並べた文書を、文ごとに同じ・近い言い換え・別の文・足し・消しにして書き換える組。
func diffMeaningFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 50))
	bank := diffMeaningBank()
	suffixes := []string{"。必ず実施する。", "。遅れる可能性がある。", "。確認します。", "。障害により止まった。", "。", "か。"}
	var cases []diffFullCase
	for i := range 3600 {
		var old, new []string
		for range 2 + r.IntN(4) {
			w := randPick(r, diffReviewWrap)
			o := randPick(r, bank)
			nw := o
			switch r.IntN(6) {
			case 1:
				nw = strings.TrimSuffix(o, "。") + randPick(r, suffixes)
			case 2, 3:
				nw = randPick(r, bank)
			case 4:
				nw = strings.NewReplacer("1", "2", "3", "4", "は", "が", "ない", "ある", "できる", "できない", "可能性", "予定").Replace(o)
			}
			old = append(old, diffTemplate(w, "s", o))
			if r.IntN(10) != 0 {
				new = append(new, diffTemplate(w, "s", nw))
			}
			if r.IntN(5) == 0 {
				new = append(new, randPick(r, bank))
			}
		}
		cases = append(cases, diffFullCase{name: "fuzz-" + strconv.Itoa(i), old: diffContrastJoin(r, old), new: diffContrastJoin(r, new)})
	}
	return cases
}

// diffMeaningHedgeFuzz は固定の種 (PCG 1076/51) から、推量・確度の語を持つ文を、より強い言い方・同じ強さの別の言い方・弱い言い方に書き換える組 (出力から外す印の恐れを作る)。
func diffMeaningHedgeFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 51))
	hedged := append(append([]string{}, diffReviewHedgeBank[:15]...), diffCertaintyBank[:60]...)
	strong := []string{"遅れます。", "必ず遅れる。", "確実に遅れます。", "遅れました。", "遅れる予定です。", "設定を開きます。"}
	var cases []diffFullCase
	for i := range 1500 {
		var old, new []string
		for range 1 + r.IntN(3) {
			o := randPick(r, hedged)
			nw := randPick(r, strong)
			switch r.IntN(4) {
			case 0:
				nw = randPick(r, hedged)
			case 1:
				nw = strings.TrimSuffix(randPick(r, hedged), "。") + "。" + randPick(r, strong)
			}
			old, new = append(old, o), append(new, nw)
		}
		cases = append(cases, diffFullCase{name: "hedge-" + strconv.Itoa(i), old: diffContrastJoin(r, old), new: diffContrastJoin(r, new)})
	}
	return cases
}

// diffMeaningCases は群 Y の case の一覧 (pairs は入力の集まりと diff の入力の組)。公開の meaning_risks は、手書きの組と組 8 つに 1 つに通す。
func diffMeaningCases(pairs [][3]string) []VisCase {
	var cases []VisCase
	for n, b := range diffBags(pairs) {
		cases = append(cases, VisCase{Name: "meaning/" + b.name, Calls: diffMeaningStageCalls(b.old, b.new, diffMeaningDocTypes[:1], n%8 == 0, false)})
	}
	for _, g := range []struct {
		prefix string
		cases  []diffFullCase
		types  []any
		public bool
		keep   bool
	}{
		{"meaning/hand/", diffMeaningHandPairs(), diffMeaningDocTypes, true, true},
		{"meaning/hand-added/", diffAdditionHandPairs(), diffMeaningDocTypes[:1], true, false},
		{"meaning/hedge-fuzz/", diffMeaningHedgeFuzz(), diffMeaningDocTypes, false, false},
		{"meaning/fuzz/", diffMeaningFuzz(), diffMeaningDocTypes[:1], false, false},
		{"meaning/swap-fuzz/", diffEvery(diffSwapFuzz(), 3), diffMeaningDocTypes[:1], false, false},
		{"meaning/contrast-fuzz/", diffEvery(diffContrastFuzz(), 6), diffMeaningDocTypes[:1], false, false},
		{"meaning/qualifier-fuzz/", diffEvery(diffQualifierFuzz(), 6), diffMeaningDocTypes[:1], false, false},
		{"meaning/value-fuzz/", diffEvery(diffReviewValueFuzz(), 6), diffMeaningDocTypes[:1], false, false},
	} {
		for _, f := range g.cases {
			cases = append(cases, VisCase{Name: g.prefix + f.name, Calls: diffMeaningStageCalls(f.old, f.new, g.types, g.public, g.keep)})
		}
	}
	return cases
}

// diffEvery は組の並びから n 個に 1 つを取る (ほかの群の固定の種の組を、少しだけ借りる)。
func diffEvery(cases []diffFullCase, n int) []diffFullCase {
	var out []diffFullCase
	for i := 0; i < len(cases); i += n {
		out = append(out, cases[i])
	}
	return out
}
