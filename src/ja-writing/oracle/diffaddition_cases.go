package oracle

import (
	"math/rand/v2"
	"strconv"
	"strings"
)

// 群 V (書き足した内容の恐れ) の記録: 段 _addition_risks(sides, groups, risks, masks) と、その下の関数と定数。
// 組は、群 M までと同じ入れ物 (diffBags) に、専用の手書きの組と固定の種 (PCG 1076/40〜43) の組を足したもの。
// risks は本家の _meaning_risks と同じに、文の種類の印・確度・否定の候補をつないで渡す (V が読むのはこの 3 種類)。

// DiffAdditionFunctions は群 V の関数。_addition_old_future_count・_addition_old_negative は本家のどこからも呼ばれない (定義だけ) ので写していない。
// _addition_find_all は _addition_causal_words からだけ呼ばれ、Go は表を直接たどる。
var DiffAdditionFunctions = []string{
	"_addition_risks", "_addition_state_words", "_addition_topic", "_addition_on_topic", "_addition_stem_neg", "_addition_promise_clauses", "_addition_key_head",
	"_addition_pred_stem", "_addition_old_futures", "_addition_old_negatives", "_addition_same_act", "_addition_causal_words", "_addition_bigrams",
	"_addition_novelty", "_addition_time_to_cause", "_addition_cause_span",
}

// diffAdditionConstants は群 V の Go の表と比べる本家の定数 (_ADD_STATE_RX などの組んだ正規表現は、表から作るので記録しない)。
var diffAdditionConstants = []string{
	"_ADD_K", "_ADD_NEG", "_ADD_PERF_NEG", "_ADD_CHU", "_ADD_CHU_COMMA", "_ADD_STATE", "_ADD_STATE_ANY", "_ADD_KEEP_EXTRA", "_ADD_KEEP_CROSS", "_ADD_COND", "_ADD_STATE_DENIED",
	"_ADD_SEND_OFF_OBJECT", "_ADD_DONE", "_ADD_PROGRESSIVE", "_ADD_TOPIC_STOP", "_ADD_APOLOGY", "_ADD_ANY_NEG", "_ADD_PERFORMATIVE", "_ADD_REPORT", "_ADD_REPORT_MAYBE",
	"_ADD_PERF_MARK", "_ADD_FUTURE_MARK", "_ADD_LATER_MARK", "_ADD_LATER_STRONG", "_ADD_OTHER_SUBJECT", "_ADD_WRITER_MARK", "_ADD_STATIVE", "_ADD_OLD_FUTURE", "_ADD_PROMISE",
	"_ADD_REQUEST", "_ADD_INTRANSITIVE_SAHEN", "_ADD_AGENTIVE_SAHEN", "_ADD_E_ROW", "_ADD_INTRANSITIVE_TAIL", "_ADD_PAST_END", "_ADD_OLD_INTRANSITIVE", "_ADD_OLD_STATIVE",
	"_ADD_PREVENT_ANY", "_ADD_PREVENT_UNSETTLED", "_ADD_PREVENT_ACT", "_ADD_PREVENT_NOT_ACT", "_ADD_GUARANTEE_FAM", "_ADD_CAUSAL", "_ADD_INQUIRY_RESULT", "_ADD_TIME_LINK",
	"_ADD_CONTENT", "_ADD_PRED_SKIP", "_ADD_PRED_SYNONYMS",
}

// DiffAdditionConstantNames は定数の名前 (Go の突き合わせが、この名前の Get を回す)。
func DiffAdditionConstantNames() []string { return diffAdditionConstants }

// diffAdditionStateBank は原文の状態の語を持つ文 (表の行ごとに当たる文と、条件・否定・見送りの例外)。
var diffAdditionStateBank = []string{
	"原因は未確認です。", "原因は不明です。", "原因はご不明です。", "原因が不明瞭だ。", "不明点が残る。", "不明な点がある。", "未判明の点が残る。", "原因は未解明です。", "原因は分からない。", "原因はわかりません。", "原因は判りかねます。",
	"分かる点がある。", "原因が分かっていない。", "原因が判明していません。", "原因を特定できていない。", "状況を把握していない。", "確認が取れていません。", "はっきりしていない。", "明らかになっておりません。",
	"原因を特定できない。", "原因を特定できません。", "原因が確認できなかった。", "確認が取れない。",
	"日程は未定です。", "日程は未決定です。", "日程は未確定です。", "未定義の値です。", "未決済の件です。", "仕様は決まっていない。", "仕様は決まっておりません。", "仕様は決まらない。", "仕様は白紙です。", "仕様は決まらず、保留です。",
	"目処が立っていない。", "目途はついていません。", "めどが立たない。", "見通しはまだ立っていない。", "見通しがつかない。", "メドもつきません。",
	"判断できない。", "判断がつきません。", "決定はできず、様子を見る。", "決断がつかない。",
	"調査中です。", "確認中だ。", "照会中でした。", "問い合わせ中のため、待つ。", "調査中、会場は本社。", "調査中、原因を探る。", "確認中の案件です。", "精査中につき回答できません。",
	"原因を調べている。", "原因を調べています。", "調査しております。", "問い合わせている。", "調べていた。",
	"検討中です。", "調整中だ。", "協議中の件です。", "検討中、日程は来週。", "検討している。", "検討しております。", "調整しています。", "次回話し合う。", "次回までに改めて検討する。", "次回に決める。",
	"採用は保留です。", "結論は持ち越しになった。", "導入を見送りました。", "実施を見送る。", "見送りとなった。", "見送りです。", "人を見送る。", "お見送りします。",
	"回答を待っている。", "返事を待ちます。", "結果を待って判断する。", "入荷を待っております。", "待っています。", "待っておりました。",
	"未着手です。", "未対応の項目がある。", "未実施です。", "未完了のまま残る。", "未払いの件です。", "未納の分がある。",
	"まだ終わっていない。", "まだ回答がありません。", "まだ手を付けていない。", "まだ決まっていません。", "まだ対応できない。", "まだ確認が取れず、待つ。",
	"作業は済んでいない。", "対応は終わっていません。", "着手していない。", "完了していません。",
	"確認には至っていない。", "決定にはまだ至らない。", "合意に至りません。",
	"予定はない。", "計画はありません。", "予定が立っていない。", "予定しておりません。", "予定もなく、見送る。",
	"対応中です。", "作業中だ。", "準備中の項目です。", "修正中、原因を探る。", "移行中でした。", "途中です。", "途中だ。",
	"未確認の項目はありません。", "原因不明ではありません。", "不明な点はありません。", "未定の件はない。", "調査中なら待つ。", "調査中の場合は連絡する。", "確認中のとき教える。", "検討中であれば相談する。",
}

// diffAdditionKeptBank は原文の状態が推敲後も残ったとみなす言い方・済んだと言い切る言い方・言い換え (家族ごと)。
var diffAdditionKeptBank = []string{
	"原因は引き続き確認を進めています。", "原因は明確ではありません。", "原因は不透明です。", "原因は未知数です。", "原因はまだはっきりしない。", "原因は見えていない。", "存じません。", "原因は存じ上げません。",
	"決まり次第連絡します。", "決まったら連絡する。", "日程はまだ未定です。", "調査を進めています。", "確認を続けている。", "調査しているところです。", "現在、原因を調べています。",
	"検討を進める。", "調整を続けています。", "次回改めて協議する。", "判断を待つ。", "いったん保留にする。", "いったん見送りとする。", "届いていない。", "届き次第連絡する。", "届きましたら連絡します。", "お待ちしている。", "待っております。",
	"そのままになっている。", "終わらないまま残る。", "されていない。", "しておりません。", "進めている。", "最中です。", "検討している最中だ。", "確認しているところです。",
	"調査を進めて、検討を続ける。", "検討を進めている。", "作業を進めておる。",
	"原因は判明しました。", "調査は完了しました。", "日程は決定しました。", "確認済みです。", "解決しました。", "決まりました。", "終わりました。", "特定できました。", "未確認ではなく確認済みです。",
	"困難です。", "不可能だ。", "難しい。", "まだです。", "いまだ分からない。", "確認できない。", "申し訳ありません。分かりません。", "申し訳ありませんが、確認できました。", "すみません、確認できていません。",
	"検討中です。", "調査中です。", "作業中です。", "対応しています。", "検討しています、日程は未定。", "調査して、原因は不明。",
}

// diffAdditionNeutralBank は状態も約束も因果も持たない文 (隣の文・書き換えの先)。
var diffAdditionNeutralBank = []string{
	"遅れます。", "設定を開きます。", "結果は3件でした。", "確認します。", "前の文です。", "次の文です。", "図を参照する。", "田中さんが来た。", "画面が表示される。", "手順は以下のとおり。",
	"こちらです。", "以上です。", "本日は晴れ。", "データを送る。",
}

// diffAdditionPromiseBank は推敲後の文に足す約束 (再発防止・保証・その他の約束と、見ない形)。
var diffAdditionPromiseBank = []string{
	"再発防止に努めます。", "再発を防ぎます。", "再発防止策は実施済みです。", "再発防止を求められました。", "再発しないよう注意します。", "再発防止の徹底を図ります。",
	"今後このようなことがないよう注意いたします。", "二度とこのようなことがないよう対策します。", "今後同じことがないようにします。", "同じミスが起きないよう徹底します。", "同様の問題が発生しないようにする。",
	"徹底いたします。", "徹底します。", "徹底する。", "徹底を図ります。", "徹底は済みました。", "徹底していない。", "チェックを徹底する。",
	"気をつけます。", "気を引き締めてまいります。", "注意を払います。", "心がけます。", "心掛けています。", "肝に銘じます。", "努めます。", "取り組んでいきます。", "取り組んでまいります。", "注意する。", "気をつける。",
	"二度と起きることはありません。", "漏れることはありません。", "失敗することはない。", "起こることは一切ありません。", "壊れることはない。", "あることは一切ない。", "ありえることはございません。",
	"追加費用はかかりません。", "費用は一切かかりません。", "別途料金は発生しません。", "手数料は発生いたしません。", "負担はいただきません。", "費用はかからない。", "無料でご利用いただけます。", "無償にて提供します。", "無料です。",
	"このまま使えます。", "問題なくご利用いただけます。", "引き続きご利用いただけます。", "これまでどおり使っていただけます。", "安心してご利用になれます。", "支障なくご利用できます。",
	"価格は変えません。", "仕様は変更しません。", "料金は据え置きます。", "値上げしません。", "変えない。", "上げません。",
	"品質を保証します。", "お約束します。", "約束いたします。", "動作を保証する。", "品質は保証される。", "担保します。", "担保される。", "保証している。", "保証されている。", "保証しています。", "保証できる。", "保証書を同封する。", "配信保証がある。", "約束する。",
	"確認させていただきます。", "ご連絡させていただきます。", "対応させていただく。", "来週対応させていただきます。",
	"確認いたします。", "ご連絡いたします。", "お送りいたします。", "修正致します。", "確認いたしましょう。", "お願いいたします。", "検討いたします。", "感謝いたします。", "お詫びいたします。", "確認いたしました。",
	"確認してまいります。", "修正してまいります。", "順次進めていきます。", "確認していきます。", "確認してまいりました。",
	"お送りします。", "ご案内します。", "お知らせします。", "ご報告します。", "ご連絡します。", "ご報告いたします。", "お知らせいたします。", "取り急ぎご報告します。", "以下のとおりご報告します。", "後日あらためてご報告します。", "追ってご連絡します。", "ご案内いたします。",
	"修正します。", "対応します。", "報告します。", "連絡します。", "確認する。", "調整します。", "実施します。", "再開します。", "改善します。", "削除します。", "共有します。", "提出します。",
	"完了します。", "開始します。", "増えます。", "発生します。", "起きます。", "あります。", "できます。", "分かります。", "決まります。", "届きます。", "始まります。", "変わります。", "見られます。",
	"今後は弊社で対応します。", "担当が対応します。", "私が確認します。", "来週中に確認します。", "明日修正します。", "必ず確認します。", "事前に連絡します。", "引き続き対応します。",
	"先方が確認します。", "お客様が対応します。", "皆様にお知らせします。", "メーカーから連絡します。",
	"確認してください。", "ご確認をお願いします。", "確認していただけますか。", "ご確認いただければ幸いです。", "確認してほしい。",
	"確認しますか。", "確認いたしますか？", "ご確認いたしますか", "対応しますか。", "直しますでしょうか。",
	"「確認します」と言った。", "田中「確認します。」", "彼は「必ず直します。」と答えた。", "「確認いたします」", "彼は「確認し",
	"明日からは確認する。", "来週からは直す。", "次回は修正する。", "今後は確認する。", "来月に実施する。", "確認する予定です。", "直すつもりです。", "確認する方針です。", "確認したいと思います。",
	"以下に報告します。", "次のとおり連絡します。", "この場でお知らせします。",
}

// diffAdditionFutureBank は原文の未来の行為・決まり (推敲後の約束を言い換えとして打ち消す) と否定の言い方。
var diffAdditionFutureBank = []string{
	"確認します。", "修正する予定です。", "直します。", "報告します。", "連絡します。", "来週対応する。", "費用はかかりません。", "起きません。", "確認いたします。", "お送りします。", "調べます。", "書き換える。",
	"確認する。", "直すつもりです。", "確認する方針です。", "送る。", "出します。", "返します。", "決めます。", "変えます。", "止めます。", "始めます。", "続けます。", "払います。", "届けます。", "渡します。", "聞きます。", "片付けます。",
	"遅れる。", "見つかる。", "分かります。", "消えます。", "届きます。", "あります。", "いません。", "ありません。", "ございません。", "ない。", "ないが、確認する。", "確認はない。", "次回は20日に開催。", "来月に実施。", "確認しました。", "確認した。",
	"確認しましょう。", "確認したい。", "確認するが、遅れる。", "確認するので待つ。", "確認するよ。", "直したい。", "確認しますか。", "確認する予定はない。", "確認するつもりはありません。", "確認する方針はなし。",
	"変更しません。", "上げません。", "値上げしません。", "無料にならない。", "料金は上がらない。", "追加はない。", "起きない。", "漏れません。",
}

// diffAdditionCausalBank は推敲後の文に足す理由・原因・結果のつなぎ (とそれを持たない文)。
var diffAdditionCausalBank = []string{
	"システム障害により遅延しました。", "設定の誤りによって停止した。", "障害が起きたため、遅れます。", "遅れるために、連絡する。", "遅れたため、謝る。", "障害が起きたので、遅れます。", "遅れたので連絡する。", "遅れるからです。", "遅れたから、謝る。", "遅れたことから、対策した。",
	"遅れるのため、連絡する。", "雨のため、遅れる。", "のでしょう。", "遅れるのでしょうか。", "遅れるのです。",
	"集中したことで速くなった。", "集中することにより速くなった。", "おかげで速くなった。", "雨のせいで遅れた。", "設定が原因で停止した。", "障害を受けて対応した。", "障害の影響で遅れた。", "影響により遅れた。", "新機能の効果で改善した。", "効果により改善した。", "これがきっかけで始まった。", "による被害が出た。", "によるものだ。",
	"その結果、遅れました。", "この結果、止まった。", "結果として、遅れた。", "そのため、遅れる。", "このため、止まる。", "これにより、止まった。", "これによって止まった。", "それにより遅れた。", "したがって止める。", "よって、止める。",
	"改善につながる。", "改善につながった。", "成功をもたらした。", "品質に寄与する。", "成長に貢献した。", "対策が功を奏した。", "障害につながります。",
	"調べた結果、原因が分かった。", "確認した結果、問題はなかった。", "検証した結果、遅れた。", "集計した結果、増えた。", "見直した結果、変えた。",
	"結果は3件でした。", "結果を見る。", "成果です。", "効果があった。", "原因は不明です。",
	"集中時に速くなった。", "集中する時に速くなった。", "雨の際に遅れた。", "停電の直後に止まった。", "作業の最中に遅れた。", "実際に動いた。", "国際化した。", "当時は遅かった。", "同時に起きた。",
}

// diffAdditionTimeOldBank は原文の「〜時に」「〜際に」「直後に」の言い方 (推敲で原因の語にした書き換えの原文)。
var diffAdditionTimeOldBank = []string{
	"集中した時に速くなった。", "集中したとき速くなった。", "集中した際に速くなった。", "停電の直後に止まった。", "雨が降ると遅れた。", "雨の時は遅れた。", "作業の最中に遅れた。", "集中時、速くなった。",
	"集中するときに速かった。", "停電したとき止まった。",
}

// diffAdditionStageCalls は組 1 つを、本家の diff() と同じ順で作らせて (覚える)、_addition_risks に通す呼び出し。risks は
// 文の種類の印・確度・否定の恐れをつないだもの (marksOnly なら印だけ)。
func diffAdditionStageCalls(old, new string, marksOnly, keep bool) []Call {
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
		save(dcall("_marks_risks", sides, diffRef("tc")), "marks"),
		save(dcall("_certainty_risks", sides, groups, diffRef("marks"), nil, nil), "cert"),
		save(dcall("_negation_risks", sides, groups), "neg"),
		save(dcall("_Masker", sides, 0), "m0"), save(dcall("_Masker", sides, 1), "m1"),
	}
	risks := map[string]any{"$concat": []any{diffRef("marks"), diffRef("cert"), map[string]any{"$column": diffRef("neg"), "n": 2}}}
	if marksOnly {
		risks = map[string]any{"$concat": []any{diffRef("marks")}}
	}
	return append(calls, withKeep(keep, dcall("_addition_risks", sides, groups, risks, []any{diffRef("m0"), diffRef("m1")})))
}

func diffAdditionHandPairs() []diffFullCase {
	doc := diffDoc
	return []diffFullCase{
		{name: "state-same", old: doc("原因は未確認です。"), new: doc("原因は未確認です。")},
		{name: "state-lost", old: doc("原因は未確認です。"), new: doc("原因は対策しました。")},
		{name: "state-lost-done", old: doc("原因は未確認です。"), new: doc("原因は判明しました。")},
		{name: "state-kept-other-wording", old: doc("原因は未確認です。"), new: doc("原因は明確ではありません。")},
		{name: "state-kept-progressive", old: doc("調査中です。"), new: doc("調査を進めています。")},
		{name: "state-kept-progressive-form", old: doc("検討中です。"), new: doc("検討している。")},
		{name: "state-kept-cross-family", old: doc("日程は未定です。"), new: doc("日程は検討中です。")},
		{name: "state-lost-cross-family-unknown", old: doc("原因は不明です。"), new: doc("原因は確認を進めています。")},
		{name: "state-lost-two-words", old: doc("原因は未確認で、日程は未定です。"), new: doc("遅れます。")},
		{name: "state-lost-families", old: doc("原因は未確認です。日程は未定です。"), new: doc("遅れます。")},
		{name: "state-neighbor-kept", old: doc("原因は未確認です。"), new: doc("遅れます。原因は不明です。")},
		{name: "state-neighbor-other-topic", old: doc("原因は未確認です。"), new: doc("遅れます。日程は不明です。")},
		{name: "state-count-guard", old: doc("原因は未確認です。", "設定を開く。"), new: doc("原因は対策しました。", "設定は未確認です。")},
		{name: "state-count-guard-same-topic", old: doc("原因は未確認です。", "設定を開く。"), new: doc("原因は対策しました。", "原因は未確認です。")},
		{name: "state-negation-covered", old: doc("原因は不明です。"), new: doc("原因は不明ではありません。")},
		{name: "state-denied", old: doc("未確認の項目はありません。"), new: doc("遅れます。")},
		{name: "state-cond", old: doc("調査中なら待つ。"), new: doc("遅れます。")},
		{name: "state-comma-topic", old: doc("検討中、日程は来週。"), new: doc("遅れます。")},
		{name: "state-comma-no-topic", old: doc("検討中、確認する。"), new: doc("遅れます。")},
		{name: "state-send-off-object", old: doc("導入を見送りました。"), new: doc("導入しました。")},
		{name: "state-send-off-person", old: doc("人を見送る。"), new: doc("遅れます。")},
		{name: "state-send-off-noun", old: doc("見送りです。"), new: doc("遅れます。")},
		{name: "state-awaiting", old: doc("回答を待っている。"), new: doc("回答は届いていない。")},
		{name: "state-not-done-stem", old: doc("未実施です。"), new: doc("実施していません。")},
		{name: "state-not-done-stem-far", old: doc("未実施です。"), new: doc("確認は実施してあれもこれも実施しません。")},
		{name: "state-still-not", old: doc("まだ終わっていない。"), new: doc("終わりません。")},
		{name: "state-not-done-any-neg", old: doc("未着手です。"), new: doc("困難です。")},
		{name: "state-not-done-any-neg-done", old: doc("未着手です。"), new: doc("困難でしたが完了しました。")},
		{name: "state-not-done-apology", old: doc("未着手です。"), new: doc("申し訳ありません。")},
		{name: "state-certainty-steps", old: doc("原因は未確認で、遅れる可能性がある。"), new: doc("遅れます。")},
		{name: "state-marks-hedge", old: doc("原因は未確認です。"), new: doc("原因は未確認だと思います。")},
		{name: "state-heading", old: doc("# 調査中"), new: doc("# 完了")},
		{name: "state-list", old: doc("- 調査中", "- 設定を開く。"), new: doc("- 完了", "- 設定を開く。")},
		{name: "state-table", old: doc("| a | b |", "|---|---|", "| 調査中 | x |"), new: doc("| a | b |", "|---|---|", "| 完了 | x |")},
		{name: "state-inline-code", old: doc("`調査中` と原因は未確認です。"), new: doc("`調査中` と遅れます。")},
		{name: "state-other-line", old: doc("原因は未確認です。", "遅れます。"), new: doc("遅れます。", "原因は未確認です。")},
		{name: "state-unpaired-neighbors", old: doc("原因は未確認です。確認します。設定を開く。"), new: doc("遅れます。")},
		{name: "state-in-progress-comma", old: doc("作業中、会場は本社。"), new: doc("遅れます。")},
		{name: "state-no-plan", old: doc("予定はない。"), new: doc("予定を組んだ。")},
		{name: "state-pending", old: doc("採用は保留です。"), new: doc("採用しました。")},
		{name: "state-considering-next", old: doc("次回話し合う。"), new: doc("決めた。")},
		{name: "state-empty-new", old: doc("原因は未確認です。"), new: ""},
		{name: "promise-added", old: doc("遅れます。"), new: doc("遅れます。確認します。")},
		{name: "promise-cancelled-by-old-future", old: doc("確認します。"), new: doc("チェックします。")},
		{name: "promise-cancelled-synonym", old: doc("直します。"), new: doc("修正します。")},
		{name: "promise-not-cancelled", old: doc("読みます。"), new: doc("破棄します。")},
		{name: "promise-head-in-old", old: doc("確認の件です。"), new: doc("確認いたします。")},
		{name: "promise-head-nonpast", old: doc("設定を確認した。", "遅れます。"), new: doc("遅れます。確認いたします。")},
		{name: "promise-prevention", old: doc("遅れました。"), new: doc("遅れました。再発防止に努めます。")},
		{name: "promise-prevention-in-old", old: doc("再発防止に努めます。"), new: doc("再発防止に努めます。徹底します。")},
		{name: "promise-prevention-unsettled", old: doc("再発防止策は未定です。"), new: doc("再発防止に努めます。")},
		{name: "promise-prevention-done", old: doc("遅れました。"), new: doc("再発防止策は実施済みです。")},
		{name: "promise-prevention-requested", old: doc("遅れました。"), new: doc("再発防止を求められました。")},
		{name: "promise-prevention-decided", old: doc("遅れました。"), new: doc("再発防止に取り組むこととしました。")},
		{name: "promise-guarantee", old: doc("遅れました。"), new: doc("遅れました。追加費用はかかりません。")},
		{name: "promise-guarantee-in-old", old: doc("追加費用はかかりません。"), new: doc("費用は一切かかりません。")},
		{name: "promise-guarantee-negative-pred", old: doc("起きません。"), new: doc("起きることはありません。")},
		{name: "promise-guarantee-negation-count", old: doc("複製しません。"), new: doc("複製しないため、漏れることはありません。")},
		{name: "promise-guarantee-negation-same", old: doc("起きない。"), new: doc("起きることはない。")},
		{name: "promise-guarantee-never-change", old: doc("価格は上がらない。"), new: doc("価格は変えません。")},
		{name: "promise-guarantee-keep", old: doc("遅れました。"), new: doc("品質を保証します。")},
		{name: "promise-weak", old: doc("遅れました。"), new: doc("修正します。")},
		{name: "promise-weak-future-mark", old: doc("遅れました。"), new: doc("明日修正します。")},
		{name: "promise-sahen-intransitive", old: doc("遅れました。"), new: doc("遅延が発生します。")},
		{name: "promise-sahen-agentive", old: doc("遅れました。"), new: doc("対応します。")},
		{name: "promise-sahen-agentive-future", old: doc("遅れました。"), new: doc("今後は弊社で対応します。")},
		{name: "promise-stative", old: doc("遅れました。"), new: doc("あります。")},
		{name: "promise-plain-without-future", old: doc("遅れました。"), new: doc("確認する。")},
		{name: "promise-plain-with-future", old: doc("遅れました。"), new: doc("明日確認する。")},
		{name: "promise-report", old: doc("遅れました。"), new: doc("ご報告します。")},
		{name: "promise-report-later", old: doc("遅れました。"), new: doc("後日あらためてご報告します。")},
		{name: "promise-report-perf", old: doc("遅れました。"), new: doc("以下のとおりご報告します。")},
		{name: "promise-report-maybe", old: doc("遅れました。"), new: doc("以下のとおりご案内します。")},
		{name: "promise-other-subject", old: doc("遅れました。"), new: doc("先方が確認します。")},
		{name: "promise-request", old: doc("遅れました。"), new: doc("確認してください。")},
		{name: "promise-question", old: doc("遅れました。"), new: doc("確認しますか。")},
		{name: "promise-open-quote", old: doc("遅れました。"), new: doc("彼は「確認し")},
		{name: "promise-dialogue", old: doc("遅れました。"), new: doc("田中「確認します。」")},
		{name: "promise-performative", old: doc("遅れました。"), new: doc("感謝いたします。")},
		{name: "promise-certainty-covered", old: doc("遅れる可能性があります。"), new: doc("必ず確認します。")},
		{name: "promise-request-lost", old: doc("確認してください。"), new: doc("確認します。")},
		{name: "promise-request-lost-kept-request", old: doc("確認してください。"), new: doc("確認をお願いします。確認します。")},
		{name: "promise-neighbor-future", old: doc("確認します。遅れます。"), new: doc("遅れます。チェックします。")},
		{name: "promise-two-clauses", old: doc("遅れました。"), new: doc("再発防止に努め、追加費用はかかりません。確認いたします。")},
		{name: "promise-heading", old: doc("# 経緯"), new: doc("# 経緯", "確認いたします。")},
		{name: "promise-code", old: doc("遅れました。"), new: doc("`確認いたします`と遅れました。")},
		{name: "claim-added", old: doc("遅れました。"), new: doc("障害により遅れました。")},
		{name: "claim-old-has-causal", old: doc("障害により遅れました。"), new: doc("障害によって遅れました。")},
		{name: "claim-old-neighbor-causal", old: doc("障害により遅れました。確認します。"), new: doc("確認します。障害によって遅れました。")},
		{name: "claim-known-words", old: doc("障害があった。遅れました。"), new: doc("障害により遅れました。")},
		{name: "claim-result", old: doc("遅れました。"), new: doc("その結果、止まりました。")},
		{name: "claim-result-tsunagaru", old: doc("遅れました。"), new: doc("改善につながります。")},
		{name: "claim-inquiry-result", old: doc("遅れました。"), new: doc("調べた結果、原因が分かりました。")},
		{name: "claim-joined", old: doc("障害が起きた。", "遅れました。"), new: doc("障害が起きたため、遅れました。")},
		{name: "claim-joined-lines", old: doc("障害が起きた。遅れました。"), new: doc("障害が起きたため、遅れました。")},
		{name: "claim-time-to-cause", old: doc("集中した時に速くなった。"), new: doc("集中したことで速くなった。")},
		{name: "claim-time-to-cause-other", old: doc("雨の時は遅れた。"), new: doc("集中したことで速くなった。")},
		{name: "claim-multi-causal", old: doc("遅れました。"), new: doc("障害により遅れたため、停止しました。")},
		{name: "claim-two-causal-one-old", old: doc("障害により遅れました。"), new: doc("障害により遅れたため、停止しました。")},
		{name: "claim-new-only-line", old: doc("遅れました。"), new: doc("遅れました。", "障害により止まりました。")},
		{name: "claim-quote", old: doc("遅れました。"), new: doc("> 障害により遅れました。")},
		{name: "mixed-all", old: doc("原因は未確認です。確認します。"), new: doc("障害により遅れました。再発防止に努めます。")},
		{name: "same-doc", old: doc("障害により遅れました。確認します。"), new: doc("障害により遅れました。確認します。")},
		{name: "empty-old", old: "", new: doc("障害により遅れました。再発防止に努めます。")},
		{name: "crlf", old: "原因は未確認です。\r\n", new: "原因は対策しました。\r\n"},
	}
}

var diffAdditionWrap = []string{"{s}", "{s}", "{s}", "- {s}", "## {s}", "> {s}", "| {s} | x |", "前の文です。{s}", "{s}後の文です。", "`{s}`"}

// diffAdditionFuzz は固定の種 (PCG 1076/seed) から、old と new の文の組 (kind ごとの銀行から) を作った組。
func diffAdditionFuzz(seed uint64, count int, oldBank, newBank []string, change func(r *rand.Rand, old string) string) []diffFullCase {
	r := rand.New(rand.NewPCG(1076, seed))
	var cases []diffFullCase
	for i := range count {
		var old, new []string
		for range 1 + r.IntN(3) {
			w := randPick(r, diffAdditionWrap)
			o := randPick(r, oldBank)
			nw := change(r, o)
			if nw == "" {
				nw = randPick(r, newBank)
			}
			old, new = append(old, diffTemplate(w, "s", o)), append(new, diffTemplate(w, "s", nw))
			if r.IntN(3) == 0 {
				c := randPick(r, diffAdditionNeutralBank)
				if r.IntN(2) == 0 {
					old = append(old, c)
				} else {
					new = append(new, c)
				}
			}
		}
		cases = append(cases, diffFullCase{name: "fuzz-" + strconv.Itoa(i), old: diffContrastJoin(r, old), new: diffContrastJoin(r, new)})
	}
	return cases
}

// diffAdditionStateFuzz は固定の種 (PCG 1076/40) の、状態の語を持つ文を、同じ文・残る言い方・済んだ言い方・別の文に書き換える組。
func diffAdditionStateFuzz() []diffFullCase {
	return diffAdditionFuzz(40, 2200, diffAdditionStateBank, diffAdditionKeptBank, func(r *rand.Rand, old string) string {
		switch r.IntN(5) {
		case 0:
			return old
		case 1:
			return randPick(r, diffAdditionKeptBank)
		case 2:
			return randPick(r, diffAdditionStateBank)
		case 3:
			return strings.TrimSuffix(old, "。") + randPick(r, []string{"が、確認する。", "。原因は不明です。", "。調査を進めています。", "。決まりました。"})
		}
		return ""
	})
}

// diffAdditionPromiseFuzz は固定の種 (PCG 1076/41) の、原文の未来の行為を、約束の文に書き換える組。
func diffAdditionPromiseFuzz() []diffFullCase {
	return diffAdditionFuzz(41, 2600, diffAdditionFutureBank, diffAdditionPromiseBank, func(r *rand.Rand, old string) string {
		switch r.IntN(4) {
		case 0:
			return old
		case 1:
			return strings.TrimSuffix(old, "。") + "。" + randPick(r, diffAdditionPromiseBank)
		}
		return ""
	})
}

// diffAdditionCausalFuzz は固定の種 (PCG 1076/42) の、原因のつなぎのない文・時の言い方の文を、つなぎのある文に書き換える組。
func diffAdditionCausalFuzz() []diffFullCase {
	old := append(append([]string{}, diffAdditionNeutralBank...), diffAdditionTimeOldBank...)
	old = append(old, diffAdditionCausalBank[:15]...)
	return diffAdditionFuzz(42, 2000, old, diffAdditionCausalBank, func(r *rand.Rand, o string) string {
		if r.IntN(5) == 0 {
			return o
		}
		return ""
	})
}

// diffAdditionMixedFuzz は固定の種 (PCG 1076/43) の、3 種類の銀行を混ぜた組 (打ち消し合う候補を作る)。
func diffAdditionMixedFuzz() []diffFullCase {
	all := append(append(append([]string{}, diffAdditionStateBank...), diffAdditionFutureBank...), diffAdditionTimeOldBank...)
	all = append(all, diffAdditionNeutralBank...)
	news := append(append(append([]string{}, diffAdditionKeptBank...), diffAdditionPromiseBank...), diffAdditionCausalBank...)
	return diffAdditionFuzz(43, 2400, all, news, func(r *rand.Rand, o string) string {
		if r.IntN(4) == 0 {
			return strings.TrimSuffix(o, "。") + "。" + randPick(r, news)
		}
		return randPick(r, news)
	})
}

// diffAdditionTimeFuzz は固定の種 (PCG 1076/45) の、原文の「〜時に」「〜際に」を、同じ中身のまま原因の語 (ことで・により・おかげ) にした組。
func diffAdditionTimeFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 45))
	nouns := []string{"集中", "作業", "設定", "起動", "停電", "点検", "更新", "接続", "導入", "確認"}
	olds := []string{"{n}した時に", "{n}したとき", "{n}した際に", "{n}の直後に", "{n}の最中に", "{n}するときに", "{n}の時は"}
	news := []string{"{n}したことで", "{n}したことにより", "{n}のおかげで", "{n}のせいで", "{n}が原因で", "{n}の影響で", "{n}によって", "{n}により", "{n}したため、", "{n}したので"}
	results := []string{"速くなった。", "遅れた。", "止まった。", "安定した。", "壊れた。"}
	var cases []diffFullCase
	for i := range 700 {
		n, res := randPick(r, nouns), randPick(r, results)
		oldText := diffTemplate(randPick(r, olds), "n", n) + res
		newText := diffTemplate(randPick(r, news), "n", n) + res
		if r.IntN(4) == 0 {
			newText = diffTemplate(randPick(r, news), "n", randPick(r, nouns)) + res
		}
		old, new := []string{oldText}, []string{newText}
		if r.IntN(3) == 0 {
			c := randPick(r, diffAdditionNeutralBank)
			old, new = append(old, c), append(new, c)
		}
		cases = append(cases, diffFullCase{name: "time-" + strconv.Itoa(i), old: diffContrastJoin(r, old), new: diffContrastJoin(r, new)})
	}
	return cases
}

// diffAdditionTexts は下の関数に通す文 (銀行の文と、文の組み合わせ)。
func diffAdditionTexts() []string {
	r := rand.New(rand.NewPCG(1076, 44))
	var texts []string
	for _, bank := range [][]string{diffAdditionStateBank, diffAdditionKeptBank, diffAdditionNeutralBank, diffAdditionPromiseBank, diffAdditionFutureBank, diffAdditionCausalBank, diffAdditionTimeOldBank} {
		texts = append(texts, bank...)
	}
	n := len(texts)
	for range 700 {
		a, b := texts[r.IntN(n)], texts[r.IntN(n)]
		texts = append(texts, a+b, strings.TrimSuffix(a, "。")+"、"+b)
	}
	return texts
}

var diffAdditionKeys = []string{"確認", "お送り", "ご連絡", "徹底", "ご報告", "検討", "x", "", "お", "ご", "修", "ア", "カタカナ", "お願", "ご案内", "一", "確認いたし", "A確認"}

var diffAdditionStems = [][]any{{}, {"確"}, {"確", "認"}, {"直"}, {"出"}, {"送", "付"}, {"作"}, {"調", "べ"}, {"見"}, {"伝"}, {"知"}, {"言"}, {"返"}, {"決"}, {"変"}, {"止"}, {"始"}, {"続"}, {"払"}, {"届"}, {"渡"}, {"聞"}, {"片"}, {"用"}, {"準"}, {"手"}, {"来"}, {"x"}}

// diffAdditionStringCalls は下の関数の呼び出し (texts[i] に others[i] を相手に通す。結果そのものを記録に残すかは keep)。
func diffAdditionStringCalls(texts, others []string, keep bool) []Call {
	var calls []Call
	add := func(c Call) { calls = append(calls, withKeep(keep, c)) }
	for i, text := range texts {
		other := others[i]
		length := len([]rune(text))
		add(dcall("_addition_state_words", text))
		add(dcall("_addition_promise_clauses", text))
		add(dcall("_addition_old_futures", text))
		add(dcall("_addition_old_negatives", text))
		add(dcall("_addition_causal_words", text))
		add(dcall("_addition_bigrams", text))
		for _, pos := range []int{0, 3, length / 2, length} {
			add(dcall("_addition_topic", text, pos))
			add(dcall("_addition_pred_stem", text, pos))
		}
		add(dcall("_addition_on_topic", []any{}, text))
		add(dcall("_addition_on_topic", []any{"障害", "確認"}, text))
		add(dcall("_addition_on_topic", []any{"あい", "確認"}, other))
		add(dcall("_addition_stem_neg", text))
		add(dcall("_addition_novelty", text, other))
		add(dcall("_addition_novelty", other, text))
		add(dcall("_addition_time_to_cause", text, other))
		add(dcall("_addition_time_to_cause", other, text))
		for _, family := range []string{"reason", "means", "result"} {
			for _, start := range []int{0, 2, length / 2} {
				add(dcall("_addition_cause_span", text, family, start, min(length, start+3)))
			}
		}
		for _, stem := range diffAdditionStems[:1+i%len(diffAdditionStems)] {
			add(dcall("_addition_same_act", stem, other))
		}
	}
	return calls
}

// diffAdditionSmallCalls は文字列だけの小さい関数の手書きの呼び出し (結果そのものを記録に残す)。
func diffAdditionSmallCalls() []Call {
	var calls []Call
	for _, key := range diffAdditionKeys {
		calls = append(calls, withKeep(true, dcall("_addition_key_head", key)))
	}
	for _, w := range []string{"未確認", "未実施", "未", "未漢字語", "まだ終わっていない", "まだ決まっていません", "まだ確認が取れず", "分かっていない", "未対応の件", "検討中", "待っている"} {
		calls = append(calls, withKeep(true, dcall("_addition_stem_neg", w)))
	}
	return calls
}

// diffAdditionCases は群 V の case の一覧 (pairs は入力の集まりと diff の入力の組)。
func diffAdditionCases(pairs [][3]string) []VisCase {
	cases := []VisCase{{Name: "addition/constants", Calls: diffConstantCalls(diffAdditionConstants)}, {Name: "addition/small", Calls: diffAdditionSmallCalls()}}
	for _, b := range diffBags(pairs) {
		cases = append(cases, VisCase{Name: "addition/" + b.name, Calls: diffAdditionStageCalls(b.old, b.new, false, false)})
	}
	for _, g := range []struct {
		prefix string
		cases  []diffFullCase
		keep   bool
		marks  bool
	}{
		{"addition/hand/", diffAdditionHandPairs(), true, false},
		{"addition/hand-marks/", diffAdditionHandPairs(), false, true},
		{"addition/state-fuzz/", diffAdditionStateFuzz(), false, false},
		{"addition/promise-fuzz/", diffAdditionPromiseFuzz(), false, false},
		{"addition/causal-fuzz/", diffAdditionCausalFuzz(), false, false},
		{"addition/mixed-fuzz/", diffAdditionMixedFuzz(), false, false},
		{"addition/time-fuzz/", diffAdditionTimeFuzz(), false, false},
	} {
		for _, f := range g.cases {
			cases = append(cases, VisCase{Name: g.prefix + f.name, Calls: diffAdditionStageCalls(f.old, f.new, g.marks, g.keep)})
		}
	}
	texts := diffAdditionTexts()
	others := make([]string, len(texts))
	for i := range texts {
		others[i] = texts[(i*7+3)%len(texts)]
	}
	for i := 0; i < len(texts); i += 60 {
		j := min(i+60, len(texts))
		cases = append(cases, VisCase{Name: "addition/strings/" + strconv.Itoa(i/60), Calls: diffAdditionStringCalls(texts[i:j], others[i:j], false)})
	}
	return cases
}
