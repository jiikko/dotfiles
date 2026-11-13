package oracle

import (
	"math/rand/v2"
	"strconv"
	"strings"
)

// 群 H (許可・能力) と群 I (選ぶ範囲・因果) の記録: 段 _modality_changes(o, r) と _logic_changes(o, r)。正規化した元と書き直した文章を渡す
// (本家の diff() が渡す形。群 F と同じく normalize の結果を覚えて渡す)。

// diffModalConstants は群 H・I の Go の表と比べる本家の定数。
var diffModalConstants = []string{
	"_MODALITY_PATTERNS", "MODALITY_MARKERS", "_MODALITY_LITERALS", "_ABILITY_NEGATIVE", "_MODALITY_NAMES",
	"_CHOICE_NOUN", "_LOGIC_PATTERNS", "LOGIC_MARKERS", "_LOGIC_LITERALS", "_LOGIC_CHANGES", "_QUESTION_KA_LEFT", "_QUESTION_KA_RIGHT", "_CHOICE_NOUN_CHAR", "_CHOICE_HEAD",
}

// DiffModalConstantNames は群 H・I の定数の名前 (Go のテストが、どの表の値で比べるかを選ぶ)。
func DiffModalConstantNames() []string { return diffModalConstants }

func diffModalConstantCalls() []Call {
	calls := make([]Call, len(diffModalConstants))
	for i, name := range diffModalConstants {
		calls[i] = Call{Module: DiffModule, Function: name, Get: true, Digest: true, Keep: true}
	}
	return calls
}

// diffModalStageCalls は 1 組に _modality_changes と _logic_changes を通す呼び出し (normalize した 2 つの文を覚えてから渡す)。
func diffModalStageCalls(old, new string) []Call {
	o := dcall("normalize", old)
	o.Save, o.Quiet = "o", true
	r := dcall("normalize", new)
	r.Save, r.Quiet = "r", true
	modality := dcall("_modality_changes", diffRef("o"), diffRef("r"))
	modality.Keep = len(old)+len(new) < 700
	logic := dcall("_logic_changes", diffRef("o"), diffRef("r"))
	logic.Keep = len(old)+len(new) < 700
	return []Call{o, r, modality, logic}
}

// diffModalityBank は許可・能力・不要・義務・禁止・保証の印の正規表現の選択肢ごとに、当たる文と当たらない文 (除外の後読みが効く文) を並べたもの。
var diffModalityBank = []string{
	"保存してもよい。", "保存しても良い。", "保存してもよろしい。", "保存してもよく、問題ない。", "保存しても良く、問題ない。", "保存してもいい。", "保存してもいいです。", "保存してもいいでしょう。", "保存してもいいが、確認する。", "保存してもいいか。",
	"読んでもよい。", "急いでもよい。", "何でもよい。", "PDF でも構わない。", "PDF でもかまいません。", "PDF でも差し支えない。", "PDF でも差支えありません。", "どうでもよい。", "ないでもよい。",
	"とてもよい。", "にとってよい。", "全てもよい。", "初めてもよい。", "くてもよい。", "安くてもよい。", "いただいてもよろしいですか。", "頂いてもいいです。", "もらってもいい。", "これでよい。", "すればよい。",
	"すれば結構です。", "いけば結構だ。", "それで結構です。", "で結構。", "この形で差し支えない。", "許可する。", "許可されます。", "許可します。", "許可した。", "保存しても問題ない。", "保存しても問題はありません。", "保存しても大丈夫です。", "保存しても結構です。", "結構大変だ。",
	"保存しなくてもよい。", "保存しないでもよい。", "保存しないでいい。", "保存は不要です。", "保存は不必要だ。", "保存する必要はない。", "保存する必要がない。", "保存する必要もない。", "保存する必要はありません。", "保存する必要はございません。",
	"保存できる。", "保存できます。", "保存できない。", "保存できません。", "保存できず、困る。", "保存できなく、困る。", "保存できていない。", "保存できておりません。", "保存できていません。", "保存できていらず、困る。", "保存できるだけ早く行う。", "保存できる限り行う。", "可能な限り保存する。", "保存は可能です。", "保存は可能だ。", "保存は不可能です。", "保存は不可能である。", "保存は可能な場合がある。", "保存は不可能に思う。", "保存は可能と考える。", "保存は可能で、安全だ。",
	"準備ができた。", "準備ができました。", "保存ができた。", "保存することができた。", "保存することができました。", "保存できた。", "保存できました。", "保存できなかった。", "保存できていた。", "保存できていました。", "保存が出来る。", "保存が出来た。",
	"保存して準備ができている。", "確認でき、進める。", "確認できて、進める。", "確認できて安心した。", "確認できている。", "確認できておりました。",
	"含められる。", "進められます。", "見られる。", "得られない。", "食べられる。", "休ませられる。", "させられた。", "いただける。", "頂けます。", "いただけない。", "あり得る。", "ありうる。", "なりえます。", "使える。", "使えます。", "書ける。", "書けません。", "防げる。", "出せる。", "話せます。", "減らせる。", "起こせます。", "待てる。", "選べる。", "読める。", "楽しめる。", "守れる。", "続ける。", "伝える。",
	"確認しなければならない。", "確認しなければなりません。", "確認しなければいけない。", "確認しなくてはならない。", "確認しないといけない。", "確認せねばならない。", "確認ねばならず、急ぐ。", "確認する必要がある。", "確認の必要があります。", "確認が必要です。", "確認が必要だ。", "確認が必要になる。", "確認が必要となります。", "不必要です。", "確認すべきだ。", "しかるべき手順だ。", "驚くべき結果だ。", "べき乗を使う。", "確認するものとする。", "確認するものとします。", "確認が必須です。",
	"保存してはいけない。", "保存してはなりません。", "保存では駄目だ。", "保存は禁止です。", "禁止する。",
	"保存を保証する。", "保存を保証します。", "保存が保証される。", "保存が保証されます。", "保存を保証いたします。", "保存を保証致します。", "保存が保証している。", "保存を保証しています。", "保存を担保する。", "保存が担保されます。", "必ず保存する。", "必ずしも保存しない。", "確実に保存する。", "保存しないことはない。", "保存しないことはありません。", "保存しないことはございません。",
	"確認します。", "設定を開きます。", "結果は3件でした。",
}

// diffChoiceOne はどれか一つの言い方の文 (どれか一つを選ぶ・どれでもよい・問いの「か」・連体)。
var diffChoiceOne = []string{
	"AまたはBを選ぶ。", "AもしくはBを選ぶ。", "AあるいはBを選ぶ。", "どちらかを選ぶ。", "どちらかといえばAだ。", "どちらからでも選べる。", "いずれかを選ぶ。", "いずれからでもよい。", "どれかを選ぶ。", "片方だけを選ぶ。", "片方のみ選ぶ。", "択一で選ぶ。",
	"木曜か金曜に確認する。", "上長か人事部に確認する。", "設定が有効か管理者に確認する。", "各端末が最新\u7248か担当者に聞く。", "何かを選ぶ。", "数回か試す。", "2か月かかる。", "3か所ある。", "ABC か否かを見る。", "原因か調べる。", "どちらでも選べる。", "いずれにでもよい。", "AでもBでも選べる。", "AからでもBからでも選べる。",
	"いずれかの条件を満たす。", "どれかの項目を選ぶ。", "どちらか一つの方式を使う。", "いずれか一方の設定を使う。", "どちらかの1つの手段を使う。",
}

// diffChoiceAll は全部の言い方の文。
var diffChoiceAll = []string{
	"AとBの両方を選ぶ。", "両方向に動く。", "AとBの双方が使う。", "双方向に動く。", "どちらも選ぶ。", "いずれも選ぶ。", "どれも選ぶ。", "両者とも選ぶ。", "二つとも選ぶ。", "三つとも選ぶ。", "どちらとも言えない。", "どちらとも選ぶ。", "どちらにも使う。", "いずれへも使う。",
	"AとBのすべてを保存する。", "AとBの全てが使う。", "全部を保存する。", "すべてに適用する。", "すべての項目に適用する。", "全項目に適用する。", "全部の設定を使う。", "AとBはともに使う。", "AとBが共に使う。", "どちらの鍵も使う。", "いずれの設定も使う。", "AもBも選ぶ。", "AにもBにも使う。", "住民票の写しも戸籍も出す。", "AとBの両方を保存する。",
}

// diffRelations は関連の言い方の文、diffCauses は因果の言い方の文。
var diffRelations = []string{
	"相関がある。", "相関関係がある。", "相関性がある。", "相関している。", "関連がある。", "関連性が高い。", "関連している。", "関係がある。", "関係している。", "関係性がある。", "関係があるか調べる。", "関係はありません。", "関係のある部署を探す。", "AとBは関連する。", "AとBは関連すると考えられる。", "結びついている。", "成果に結びついている。", "関わりがある。", "かかわりが深い。", "関連が見られた。", "相関は見られなかった。", "相関係数を求める。", "関連記事を探す。", "関連する設定を探す。",
}

var diffCauses = []string{
	"因果関係がある。", "原因である。", "起因する。", "要因だ。", "引き起こす。", "ひき起こした。", "もたらす。", "誘発する。", "施策によって増加した。", "施策により売上が伸びる。", "施策のせいで減った。", "天候のせいだ。", "おかげで進んだ。", "影響を与える。", "影響を及ぼす。", "成果につながる。", "サーバーにつながる。", "ネットワークにつながる。", "混乱を招く。", "担当者を招く。", "検査の結果、分かった。", "分析の結果、分かった。", "結果は3件。",
	"変更ことで改善した。", "変更により低下する。",
}

// diffModalCases は群 H・I の手書きの組と固定の種の組。
func diffModalCases() []diffFullCase {
	doc := diffDoc
	var cases []diffFullCase
	for i, s := range diffModalityBank {
		cases = append(cases, diffFullCase{name: "modality-lost-" + strconv.Itoa(i), old: doc("設定を開きます。", s, "結果は3件でした。"), new: doc("設定を開きます。", "確認します。", "結果は3件でした。")},
			diffFullCase{name: "modality-added-" + strconv.Itoa(i), old: doc("設定を開きます。", "確認します。", "結果は3件でした。"), new: doc("設定を開きます。", s, "結果は3件でした。")})
	}
	for i, a := range diffChoiceOne {
		for j, b := range diffChoiceAll {
			cases = append(cases, diffFullCase{name: "choice-one-all-" + strconv.Itoa(i) + "-" + strconv.Itoa(j), old: doc(a), new: doc(b)},
				diffFullCase{name: "choice-all-one-" + strconv.Itoa(i) + "-" + strconv.Itoa(j), old: doc(b), new: doc(a)})
		}
	}
	for i, a := range diffRelations {
		for j, b := range diffCauses {
			cases = append(cases, diffFullCase{name: "relation-cause-" + strconv.Itoa(i) + "-" + strconv.Itoa(j), old: doc(a), new: doc(b)},
				diffFullCase{name: "relation-cause-tail-" + strconv.Itoa(i) + "-" + strconv.Itoa(j), old: doc("設定を開く。" + a), new: doc("設定を開く。結果を見る。" + b)})
		}
	}
	cases = append(cases, diffModalHand(doc)...)
	cases = append(cases, diffRedTeamModalCases()...)
	return append(cases, diffModalFuzz()...)
}

func diffModalHand(doc func(...string) string) []diffFullCase {
	return []diffFullCase{
		{name: "same", old: doc("保存してもよい。"), new: doc("保存してもよい。")},
		{name: "permission-to-obligation", old: doc("保存してもよい。"), new: doc("保存しなければならない。")},
		{name: "permission-to-ability", old: doc("保存してもよい。"), new: doc("保存できる。")},
		{name: "permission-to-guarantee", old: doc("保存してもよい。"), new: doc("必ず保存する。")},
		{name: "permission-to-prohibit", old: doc("保存してもよい。"), new: doc("保存してはいけない。")},
		{name: "ability-to-permission", old: doc("保存できる。"), new: doc("保存してもよい。")},
		{name: "ability-to-weak", old: doc("保存できる。"), new: doc("保存できた。")},
		{name: "ability-negative-lost", old: doc("保存できません。"), new: doc("保存は失敗します。")},
		{name: "ability-negative-and-positive", old: doc("保存できる。変更できません。"), new: doc("保存する。変更は失敗する。")},
		{name: "ability-to-unneeded", old: doc("保存できる。"), new: doc("保存しなくてよい。")},
		{name: "ability-two-kinds", old: doc("保存できる。保存してもよい。"), new: doc("保存する。")},
		{name: "rule-kind-skip", old: doc("保存してください。"), new: doc("保存してもよい。")},
		{name: "rule-kind-skip-added", old: doc("保存してもよい。"), new: doc("保存してください。")},
		{name: "rule-kind-both", old: doc("保存してもよい。確認してください。"), new: doc("保存する。確認してください。")},
		{name: "permission-moved", old: doc("保存してもよい。", "確認する。"), new: doc("確認する。", "保存してもよい。")},
		{name: "permission-merged", old: doc("保存してもよい。", "確認できる。"), new: doc("保存して確認する。")},
		{name: "no-marks", old: doc("確認します。"), new: doc("確認しました。")},
		{name: "empty", old: "", new: ""},
		{name: "choice-adnominal-promote", old: doc("いずれかの条件を満たす。"), new: doc("すべての条件を満たす。")},
		{name: "choice-adnominal-all-nouns", old: doc("いずれかの条件を満たす。"), new: doc("全条件を満たす。")},
		{name: "choice-adnominal-other-noun", old: doc("いずれかの条件を満たす。"), new: doc("すべての項目を満たす。")},
		{name: "choice-adnominal-blocked", old: doc("いずれかの条件を満たす。"), new: doc("すべての条件と全条件を満たす。")},
		{name: "choice-adnominal-blocked-other", old: doc("いずれかの条件とすべての条件を満たす。"), new: doc("すべての条件を満たす。")},
		{name: "choice-adnominal-blocked-other-all", old: doc("いずれかの条件と全条件を満たす。"), new: doc("すべての条件を満たす。")},
		{name: "choice-head-long", old: doc("AまたはBを選ぶ。"), new: doc(strings.Repeat("ア", 35) + "もBも選ぶ。")},
		{name: "choice-head-long-of", old: doc("AまたはBを選ぶ。"), new: doc(strings.Repeat("アの", 20) + "ア" + "もBも選ぶ。")},
		{name: "cause-tail-newline-only", old: doc("関連がある。"), new: doc("関連を見る。", "因果関係がある。")},
		{name: "cause-tail-third", old: doc("関連がある。"), new: doc("関連を見る。結果は3件。確認する。因果関係がある。")},
		{name: "cause-tail-second", old: doc("関連がある。"), new: doc("関連を見る。結果は3件。因果関係がある。")},
		{name: "choice-adnominal-all-to-one", old: doc("すべての条件を満たす。"), new: doc("いずれかの条件を満たす。")},
		{name: "choice-all-stays", old: doc("すべてを保存し、AまたはBを選ぶ。"), new: doc("すべてを保存し、AとBの両方を選ぶ。")},
		{name: "choice-any-remains", old: doc("AまたはBを選ぶ。"), new: doc("AとBの両方を選ぶ。どちらでもよい。")},
		{name: "choice-one-remains", old: doc("AまたはBを選ぶ。"), new: doc("AとBの両方か、どちらかを選ぶ。")},
		{name: "choice-all-to-one-before", old: doc("AとBの両方を選ぶ。または別を選ぶ。"), new: doc("どちらかを選ぶ。")},
		{name: "choice-question-ka", old: doc("設定が有効か管理者に確認する。"), new: doc("設定が有効かどうかを両方確認する。")},
		{name: "choice-two-sentences", old: doc("AまたはBを選ぶ。CもしくはDを選ぶ。"), new: doc("AとBの両方を選ぶ。CとDのどちらも選ぶ。")},
		{name: "choice-and-head", old: doc("住民票の写しまたは戸籍を出す。"), new: doc("住民票の写しも戸籍も出す。")},
		{name: "choice-head-start-zero", old: doc("AまたはB。"), new: doc("も出す。もB。")},
		{name: "choice-tomo", old: doc("AまたはB。"), new: doc("ともに出す。AとBはともに出す。AもBも出す。")},
		{name: "cause-tail-two", old: doc("関連がある。"), new: doc("関連がない。因果関係がある。結果を見る。")},
		{name: "cause-tail-three", old: doc("関連がある。"), new: doc("関連を見る。結果は3件。因果関係がある。原因である。")},
		{name: "cause-tail-newline", old: doc("関連がある。", "別の話。"), new: doc("関連を見る。", "因果関係がある。")},
		{name: "cause-tail-owned", old: doc("関連がある。結果を見る。"), new: doc("関連を見る。結果を見る。因果関係がある。")},
		{name: "cause-wide-before", old: doc("関連があるため、見る。"), new: doc("因果関係がある。")},
		{name: "cause-already", old: doc("因果関係がある。関連がある。"), new: doc("因果関係がある。")},
		{name: "cause-to-cause", old: doc("関連がある。"), new: doc("因果関係があり、原因である。")},
		{name: "cause-in-range", old: doc("関連がある。", "別の文。"), new: doc("因果関係がある。", "別の文。")},
		{name: "logic-both-kinds", old: doc("AまたはBを選ぶ。関連がある。"), new: doc("AとBの両方を選ぶ。因果関係がある。")},
		{name: "logic-moved", old: doc("関連がある。", "AまたはB。"), new: doc("AとBの両方。", "因果関係がある。")},
	}
}

// diffModalFuzz は固定の種 (PCG 1076/10) から作った、印の付いた文の並べ替え・置き換え・足す・消すの組。
func diffModalFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 10))
	var pool []string
	pool = append(pool, diffModalityBank...)
	pool = append(pool, diffChoiceOne...)
	pool = append(pool, diffChoiceAll...)
	pool = append(pool, diffRelations...)
	pool = append(pool, diffCauses...)
	pool = append(pool, "設定を開く。", "結果を見る。", "確認します。")
	pick := func() string { return pool[r.IntN(len(pool))] }
	var cases []diffFullCase
	for n := range 700 {
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
			switch r.IntN(6) {
			case 0:
				cur[i][j] = pick()
			case 1:
				cur[i] = append(cur[i][:j], cur[i][j+1:]...)
			case 2:
				cur[i] = append(cur[i][:j], append([]string{pick()}, cur[i][j:]...)...)
			case 3:
				k := r.IntN(len(cur))
				cur[i], cur[k] = cur[k], cur[i]
			case 4:
				cur[i][j] = "確認する。"
			default:
				cur[i] = append(cur[i], pick())
			}
		}
		cases = append(cases, diffFullCase{name: "modal-fuzz-" + strconv.Itoa(n), old: diffJoinGroups(lines, "", "\n"), new: diffJoinGroups(cur, "", "\n")})
	}
	return cases
}
