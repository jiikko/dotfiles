package oracle

import (
	"math/rand/v2"
	"strconv"
	"strings"
)

// 群 AB (足した人や担当) の記録: 段 _added_actors(sides, groups, materials) と、その下の関数と定数。
// 組は、群 M までと同じ入れ物 (diffBags) に、専用の手書きの組と固定の種 (PCG 1076/60) の組を足したもの。
// 語の探し方 (_ActorSource の contains・has・present・_find) は、_actor_candidates に原文を変えて通して確かめる。

// DiffActorFunctions は群 AB の関数。_ActorSource は覚えるだけの呼び出し。_actor_tier は mention の組を引数に取るので、_actor_candidates 越しに確かめる。
var DiffActorFunctions = []string{
	"_actor_norm", "_actor_core", "_actor_prefix_start", "_actor_mentions", "_actor_frame", "_actor_candidates", "_added_actors", "_ActorSource",
}

// diffActorConstants は群 AB の Go の表と比べる本家の定数。_ACTOR_SPECIFIC_SET は _ACTOR_SPECIFIC の frozenset、_ACTOR_INDEX_MIN と _ACTOR_FRAME_NAMES は
// 表示と 2 字の索引のためで、Go は持たない (索引は部分文字列の検索と同じ答え、表示は群 AD)。
var diffActorConstants = []string{
	"_ACTOR_SPECIFIC", "_ACTOR_GENERIC", "_ACTOR_PERSON", "_ACTOR_READER", "_ACTOR_CHAR", "_ACTOR_PREFIX_CHAR", "_ACTOR_NAME_CHAR", "_ACTOR_PREFIX_MAX",
	"_ACTOR_HEAD", "_ACTOR_HONORIFIC", "_ACTOR_PERSON_RE", "_ACTOR_TITLE", "_ACTOR_TITLE_BAD", "_ACTOR_NOT_TITLE", "_ACTOR_ORG", "_ACTOR_ORG_END",
	"_ACTOR_ORG_STEM_BAD", "_ACTOR_ORG_NUMBERED", "_ACTOR_ORG_PART", "_ACTOR_NO_HITO", "_ACTOR_OF_HEADS", "_ACTOR_SWAP", "_ACTOR_SAME", "_ACTOR_VERB_NOUN",
	"_ACTOR_SELF_REF", "_ACTOR_SUBJECT", "_ACTOR_RECIPIENT", "_ACTOR_NOTIFY", "_ACTOR_NOTIFY_WINDOW", "_ACTOR_FROM", "_ACTOR_CORE", "_ACTOR_SKIP_KINDS",
}

// DiffActorConstantNames は定数の名前 (Go の突き合わせが、この名前の Get を回す)。
func DiffActorConstantNames() []string { return diffActorConstants }

// diffActorBank は人や担当の語を持つ文と、持たない近い文。語の表の行ごと、前に続く語・敬称・助詞・別の字との続きの境目ごとに 1 つ以上。
var diffActorBank = []string{
	// 役割の語 (主語)
	"担当者が確認します。", "管理者は承認する。", "責任者も見る。", "上長が決める。", "運用者では判断しない。", "オペレーターが操作する。", "当番が対応する。", "オンコールが見る。",
	"SREが対応する。", "PdMが決める。", "PMも確認する。", "RPMが増える。", "10PMに開始する。", "SREsが見る。", "PM以外は見ない。", "マネージャーが承認する。", "リーダーが決める。",
	"事務局が配る。", "窓口が受ける。", "情シスが調べる。", "承認者が見る。", "申請者が出す。", "レビュアーが読む。", "レビュワーが読む。", "テスターが試す。", "顧客が使う。", "お客様が来る。",
	"取引先が決める。", "ベンダーが納める。", "委託先が作る。", "協力会社が動く。", "関係者が集まる。", "関係部署が確認する。", "各部署が見る。", "各チームが対応する。",
	"部長が承認する。", "課長様が来る。", "社長殿が来る。", "役員が決める。", "経営層が見る。", "管理職が集まる。", "人事が手続きする。", "総務が配る。", "経理が払う。", "法務が見る。",
	"保守チームが直す。", "サポートチームが答える。", "サポート担当が答える。", "サポート窓口が受ける。", "オーナーが決める。", "ステークホルダーが見る。", "アドミンが設定する。",
	"運用担当者が見る。", "バックエンドチームが対応する。", "システム管理者が設定する。", "プロジェクトマネージャーが決める。", "第一担当者が見る。", "あらゆる管理者が見る。",
	"開発者が書く。", "利用者が開く。", "ユーザーが押す。", "ユーザが押す。", "読者が読む。", "読み手が読む。", "参加者が集まる。", "メンバーが集まる。", "全員が参加する。", "各自が行う。",
	"エンジニアが書く。", "チームが作業する。", "運用側が決める。", "開発側が決める。", "運営が決める。", "スタッフが配る。", "開発者たちが集まる。", "利用者らが使う。", "利用者らしい。", "利用者られる。",
	"ユーザーさんが開く。", "メンバーの方々が集まる。", "利用者の方が使う。", "担当者様が来る。",
	// 知らせる・頼む相手
	"担当者に連絡する。", "管理者へ報告する。", "上長宛てにメールする。", "上長宛に送る。", "窓口まで届ける。", "窓口までに届ける。", "関係部署にも共有する。", "担当者に確認をお願いします。", "管理者にのみ見える。",
	"利用者にとって便利です。", "窓口において受け付ける。", "担当者に対して通知する。", "管理者に関する話です。", "担当者に向けて知らせる。", "管理者になる。", "担当者に任せる。", "窓口へも相談する。",
	"事務局に承認を求める。", "運用者にアラートを送る。", "窓口に問い合わせてください。", "顧客に案内する。",
	// 動作主の「より」
	"総務部より連絡します。", "事務局より、お知らせです。", "前のチームより速い。", "担当者よりも先に確認する。", "事務局よりご説明します。", "窓口より回答します。", "窓口より後で届く。", "窓口より、確認する。",
	// 主語の助詞
	"担当者から連絡する。", "担当者によって決まる。", "担当者により決まる。", "担当者による確認です。", "担当者の手で直す。", "運用側で直す。", "担当者として動く。", "担当者もしくは管理者が見る。", "担当者もし来れば。",
	// 人称
	"私たちは確認します。", "私達が行います。", "我々が決める。", "弊社が対応します。", "当社が決める。", "当チームが対応する。", "私が書く。", "僕が行く。", "自分たちで直す。",
	"皆さんにお知らせします。", "皆様がご覧ください。", "みなさんが見る。", "皆さまにお送りする。", "各位に連絡する。", "あなたが決める。", "公私は分ける。", "関係者各位に連絡する。", "私どもが行う。",
	"こちらでは私が見る。", "私たちの方針です。",
	// 敬称の人名
	"田中さんが確認する。", "山田様に連絡する。", "鈴木氏が来る。", "佐藤殿へ送る。", "お嬢さんが来る。", "お医者さんに聞く。", "皆様にお知らせする。", "お客様が来る。", "お客さんが来る。",
	"仕様は変わる。", "同様に扱う。", "逆さまに置く。", "宮殿が建つ。", "田中さんたちが来る。", "田中様ご夫妻が来る。", "さんが来る。", "様が来る。", "お子さんが来る。", "お母さんが来る。", "山田太郎さんが来る。",
	"山田ジョンさんが来る。", "ABCさんが来る。", "田中さんと山田さんが来る。", "田中様の件です。",
	// 部署
	"営業部が対応する。", "情報システム部門が管理する。", "開発グループが対応する。", "第1部が担当する。", "第三課が管理する。", "全部が揃う。", "処理部が動く。", "都市部は混む。", "データ部が崩れる。",
	"一部が変わる。", "経理課に報告する。", "営業班が回る。", "事業部が決める。", "本部が決める。", "部門が決める。", "表示部が点灯する。", "上部が欠ける。", "内部は広い。", "本部長が決める。",
	"新規事業部が決める。", "グループが決める。", "ABグループが決める。", "営業部分が増える。", "営業部品が増える。", "営業部外が増える。", "営業部署が決める。", "営業所が決める。", "営業課題が残る。",
	"二課が決める。", "冒頭部が決める。", "外部が決める。", "開発部が対応する。",
	// 担当の人 / の者
	"担当の人に連絡する。", "当番の方が見る。", "運用の者が対応する。", "管理の人が見る。", "窓口の方に聞く。",
	// 「〇〇の担当者」
	"当番の担当者が対応する。", "経理のメンバーが集計する。", "総務の窓口に連絡する。", "当番の担当者に通知する。", "営業のチームが決める。", "営業のスタッフが配る。", "営業の人が決める。", "システムの管理者が見る。",
	// 人や担当を持たない文
	"設定を開きます。", "結果は3件でした。", "確認します。", "遅れる可能性がある。", "", "。", "AB", "サポートする。", "サポートに連絡する。", "サポートが対応する。", "サポート窓口が対応する。",
	"サポートへ連絡してください。", "サポートから連絡する。",
}

func init() {
	for _, p := range []string{"私たち", "私達", "我々", "弊社", "当社", "当チーム", "私", "僕", "自分たち", "皆さん", "皆様", "みなさん", "皆さま", "各位", "あなた"} {
		for _, tail := range []string{"が確認する。", "に連絡する。"} {
			diffActorBank = append(diffActorBank, p+tail)
		}
	}
	for _, name := range []string{"田中", "山田", "鈴木", "佐藤", "高橋", "伊藤"} {
		for _, title := range []string{"さん", "様", "氏", "殿"} {
			diffActorBank = append(diffActorBank, name+title+"が来る。")
		}
	}
	for _, stem := range []string{"営業", "総務", "開発", "経理", "人事", "品質", "技術", "広報"} {
		for _, org := range []string{"部", "課", "グループ", "班"} {
			diffActorBank = append(diffActorBank, stem+org+"が決める。")
		}
	}
	for _, stem := range []string{"担当", "当番", "運用", "管理", "責任", "窓口", "保守", "経理"} {
		for _, tail := range []string{"の人", "の方", "の者"} {
			diffActorBank = append(diffActorBank, stem+tail+"に連絡する。")
		}
	}
	for _, stem := range []string{"当番", "経理", "総務", "営業", "開発", "保守"} {
		for _, head := range []string{"担当者", "メンバー", "チーム", "窓口"} {
			diffActorBank = append(diffActorBank, stem+"の"+head+"が対応する。")
		}
	}
	for _, prefix := range []string{"システム", "運用", "新規", "各種", "第二", "主要", "社内", "外部"} {
		for _, head := range []string{"管理者", "担当者"} {
			diffActorBank = append(diffActorBank, prefix+head+"が見る。")
		}
	}
}

// diffActorSources は _ActorSource に渡す原文 (と資料)。語の探し方の分岐 (そのもの・芯・「者」の外し・「の」を詰めた形・部署の芯・入れ替え・同じ人を指す別の語・動詞にもなる語) ごとに 1 つ以上。
var diffActorSources = [][]string{
	{""},
	{"私が書いた。担当者と管理者が見る。"},
	{"運用担当が確認する。受付の担当者が来る。"},
	{"総務が配る。開発側が決める。"},
	{"お客様が来る。プロジェクトマネージャーが決める。上司が見る。オンコールが見る。"},
	{"サポートする。サポート窓口に連絡する。"},
	{"サポートに連絡する。サポートが答える。"},
	{"こちらでは確認する。関係者が集まる。"},
	{"ステークホルダーが見る。エンジニアが書く。レビュー担当者が読む。所有者が決める。"},
	{"情報システム部が見る。管理者が見る。admin が見る。"},
	{"担当の人が来る。当番の方が見る。"},
	{"開発チームが作業する。経理部が払う。営業の担当者が行く。"},
}

// diffActorMaterials は --material の資料 (原文に足して語を探す)。
var diffActorMaterials = [][]string{
	nil,
	{"担当者と管理者が窓口を使う。"},
	{"事務局と総務部と私たちが決める。", "田中さんが見る。"},
}

// diffActorStringCalls は文 1 つに、文の単位の関数を全部通す呼び出し (sources は覚える _ActorSource の名前と原文の並び)。
func diffActorStringCalls(texts []string, keep bool) []Call {
	var calls []Call
	add := func(c Call) { calls = append(calls, withKeep(keep, c)) }
	for i, src := range diffActorSources {
		c := dcall("_ActorSource", anySlice(src))
		calls = append(calls, diffSave(c, "src"+strconv.Itoa(i)))
	}
	for _, text := range texts {
		length := len([]rune(text))
		add(dcall("_actor_norm", text))
		add(dcall("_actor_mentions", text))
		for i := range diffActorSources {
			add(dcall("_actor_candidates", text, diffRef("src"+strconv.Itoa(i))))
		}
		for end := 0; end <= length; end++ {
			add(dcall("_actor_frame", text, end))
		}
		for _, start := range []int{0, 2, length / 2, length} {
			for _, floor := range []int{0, 1} {
				for _, limit := range []int{0, 3, 8} {
					add(dcall("_actor_prefix_start", text, start, floor, limit))
				}
			}
		}
	}
	return calls
}

func anySlice(xs []string) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

// diffActorCoreWords は _actor_core に通す語 (敬称・複数の語尾・語尾が語の一部の例)。
var diffActorCoreWords = []string{
	"田中さん", "山田様", "お客さま", "担当者", "担当者の方", "メンバーの方々", "開発者たち", "開発者達", "利用者ら", "利用者ども", "ユーザー殿", "ら", "さん", "", "ステークホルダー", "総務部", "運用担当者様",
}

// diffActorStageCalls は組 1 つを、本家の diff() と同じ順で作らせて (覚える)、_added_actors に通す呼び出し (materials は資料の並びの並び)。
func diffActorStageCalls(old, new string, materials [][]string, keep bool) []Call {
	calls := []Call{
		diffSave(dcall("_build_full_diff", old, new, nil), "full"),
		diffSave(dcall("_sentence_pairs", diffFullRef(1), diffFullRef(0)), "groups"),
	}
	for _, m := range materials {
		var arg any = []any{}
		if m != nil {
			arg = anySlice(m)
		}
		calls = append(calls, withKeep(keep, dcall("_added_actors", diffFullRef(1), diffRef("groups"), arg)))
	}
	return calls
}

func diffActorHandPairs() []diffFullCase {
	doc := diffDoc
	return []diffFullCase{
		{name: "same", old: doc("担当者が確認します。"), new: doc("担当者が確認します。")},
		{name: "added-subject", old: doc("確認します。"), new: doc("担当者が確認します。")},
		{name: "added-recipient", old: doc("確認します。"), new: doc("担当者に連絡します。")},
		{name: "added-person", old: doc("確認します。"), new: doc("私たちが確認します。")},
		{name: "added-person-self", old: doc("私が確認します。"), new: doc("私たちが確認します。")},
		{name: "added-person-self-other", old: doc("こちらで確認します。"), new: doc("弊社が確認します。")},
		{name: "added-reader-self", old: doc("私が確認します。"), new: doc("皆さんが確認します。")},
		{name: "narrowed", old: doc("管理者が確認します。"), new: doc("システム管理者が確認します。")},
		{name: "narrowed-modifier", old: doc("担当者が確認します。"), new: doc("当番の担当者が確認します。")},
		{name: "joined-by-no", old: doc("受付担当者が確認します。"), new: doc("受付の担当者が確認します。")},
		{name: "core-sha", old: doc("運用担当が確認します。"), new: doc("運用担当者が確認します。")},
		{name: "org-stem", old: doc("総務が確認します。"), new: doc("総務部が確認します。")},
		{name: "swap-side-team", old: doc("開発側が確認します。"), new: doc("開発チームが確認します。")},
		{name: "same-group", old: doc("お客様が確認します。"), new: doc("ユーザーが確認します。")},
		{name: "same-group-prefix", old: doc("サポートが確認します。"), new: doc("サポート担当者が確認します。")},
		{name: "same-pm", old: doc("プロジェクトマネージャーが確認します。"), new: doc("PMが確認します。")},
		{name: "verb-support-old", old: doc("設定をサポートします。"), new: doc("サポートが確認します。")},
		{name: "verb-support-person", old: doc("サポートに連絡します。"), new: doc("サポートが確認します。")},
		{name: "no-hito", old: doc("確認します。"), new: doc("担当の人が確認します。")},
		{name: "no-hito-old-tanto", old: doc("担当が確認します。"), new: doc("担当の人が確認します。")},
		{name: "title", old: doc("確認します。"), new: doc("田中さんが確認します。")},
		{name: "title-old", old: doc("田中さんが確認します。"), new: doc("田中さんに連絡します。")},
		{name: "org", old: doc("確認します。"), new: doc("営業部が確認します。")},
		{name: "two-actors", old: doc("確認します。"), new: doc("担当者が管理者に連絡します。")},
		{name: "unpaired-near", old: doc("設定を確認します。", "保存します。", "終了します。"), new: doc("設定を確認します。", "管理者が保存します。", "終了します。")},
		{name: "unpaired-replaced", old: doc("まず設定を開く。次に保存を押す。"), new: doc("管理者が設定を開く。担当者が保存を押す。")},
		{name: "unpaired-no-old", old: "", new: doc("管理者が確認します。")},
		{name: "unpaired-many", old: doc("一つ目の文です。二つ目の文です。三つ目の文です。四つ目の文です。"), new: doc("管理者が一つ目を見る。担当者が二つ目を見る。窓口が三つ目を見る。上長が四つ目を見る。五つ目を足す。")},
		{name: "unpaired-first", old: doc("設定を開く。保存する。"), new: doc("担当者が全く別の話をする。設定を開く。保存する。")},
		{name: "unpaired-last", old: doc("設定を開く。保存する。"), new: doc("設定を開く。保存する。窓口が全く別の話をする。")},
		{name: "seen-twice", old: doc("確認します。確認します。"), new: doc("担当者が確認します。担当者が確認します。")},
		{name: "code-skipped", old: doc("確認します。"), new: doc("確認します。", "```", "担当者が確認します。", "```")},
		{name: "table-skipped", old: doc("| a |", "|---|", "| 確認 |"), new: doc("| a |", "|---|", "| 担当者が確認 |")},
		{name: "heading-counted", old: doc("# 確認"), new: doc("# 担当者が確認")},
		{name: "list", old: doc("- 確認します"), new: doc("- 担当者が確認します")},
		{name: "quote", old: doc("> 確認します"), new: doc("> 担当者が確認します")},
		{name: "frontmatter-skipped", old: doc("---", "title: a", "---", "確認します。"), new: doc("---", "title: 担当者が見る", "---", "確認します。")},
		{name: "backtick-masked", old: doc("確認します。"), new: doc("`担当者が` 確認します。")},
		{name: "blank-only", old: doc("確認します。"), new: doc("確認します。", "", "   ")},
		{name: "nfkc", old: doc("確認します。"), new: doc("ＳＲＥが確認します。")},
		{name: "empty-both", old: "", new: ""},
		{name: "empty-new", old: doc("担当者が確認します。"), new: ""},
		{name: "material", old: doc("確認します。"), new: doc("事務局が確認します。管理者に連絡します。")},
		{name: "material-title", old: doc("確認します。"), new: doc("田中さんが確認します。私たちが連絡します。")},
	}
}

// diffActorFuzz は固定の種 (PCG 1076/60) から、人や担当を持つ文と持たない文を並べた文書を、文ごとに同じ・別の文・足し・消し・言い換えにして書き換える組。
func diffActorFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 60))
	bank := diffActorBank
	plain := []string{"設定を開きます。", "結果を保存します。", "確認します。", "遅れる可能性がある。", "一覧を表示する。"}
	swaps := strings.NewReplacer("が", "に", "担当者", "管理者", "部長", "課長", "私たち", "弊社", "さん", "様", "チーム", "部")
	var cases []diffFullCase
	for i := range 1400 {
		var old, new []string
		for range 2 + r.IntN(4) {
			o := randPick(r, bank)
			nw := o
			switch r.IntN(7) {
			case 1:
				nw = randPick(r, bank)
			case 2:
				nw = randPick(r, plain)
			case 3:
				nw = swaps.Replace(o)
			case 4:
				nw = randPick(r, plain) + o
			case 5:
				nw = "システム" + o
			}
			if r.IntN(3) == 0 {
				o = randPick(r, plain)
			}
			old = append(old, o)
			if r.IntN(10) != 0 {
				new = append(new, nw)
			}
			if r.IntN(5) == 0 {
				new = append(new, randPick(r, bank))
			}
		}
		cases = append(cases, diffFullCase{name: "fuzz-" + strconv.Itoa(i), old: diffContrastJoin(r, old), new: diffContrastJoin(r, new)})
	}
	return cases
}

// diffActorCases は群 AB の case の一覧 (pairs は入力の集まりと diff の入力の組)。
func diffActorCases(pairs [][3]string) []VisCase {
	cases := []VisCase{{Name: "actor/constants", Calls: diffConstantCalls(diffActorConstants)}}
	var coreCalls []Call
	for _, w := range diffActorCoreWords {
		c := dcall("_actor_core", w)
		c.Keep = true
		coreCalls = append(coreCalls, c)
	}
	cases = append(cases, VisCase{Name: "actor/core", Calls: coreCalls})
	for i := 0; i < len(diffActorBank); i += 40 {
		cases = append(cases, VisCase{Name: "actor/strings/" + strconv.Itoa(i/40), Calls: diffActorStringCalls(diffActorBank[i:min(i+40, len(diffActorBank))], i < 40)})
	}
	for n, b := range diffBags(pairs) {
		var m [][]string
		if n%4 == 0 {
			m = diffActorMaterials
		} else {
			m = diffActorMaterials[:1]
		}
		cases = append(cases, VisCase{Name: "actor/" + b.name, Calls: diffActorStageCalls(b.old, b.new, m, false)})
	}
	for _, f := range diffActorHandPairs() {
		cases = append(cases, VisCase{Name: "actor/hand/" + f.name, Calls: diffActorStageCalls(f.old, f.new, diffActorMaterials, true)})
	}
	for n, f := range diffActorFuzz() {
		m := diffActorMaterials[:1]
		if n%5 == 0 {
			m = diffActorMaterials
		}
		cases = append(cases, VisCase{Name: "actor/fuzz/" + f.name, Calls: diffActorStageCalls(f.old, f.new, m, false)})
	}
	return cases
}
