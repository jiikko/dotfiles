package oracle

import (
	"slices"
	"strings"
)

// lintRawGapDir は検出の生の並びの記録にだけ足す入力 (testdata/corpus/ には足さない) の置き場。
// corpus に足すと lint-base・visibility・lint-cli・lint-raw の記録が全部変わる。
const lintRawGapDir = "rawgap/"

// lintRawGapInputs は入力の集まり (testdata/corpus/) では 1 回も当たらない語・型と、当たりが少ない規則の形を 1 行ずつ当てる入力。
// 群を入れる人が表から語・型を落としたり書き間違えたりしても、規則単位の「当たる入力がある」では気づけないので、
// 語と型ごとに 1 件は本家の候補が出る入力にしてある (lint-raw の記録で、候補の行ごとに比べる)。本家を走らせて、1 行が 1 件になることを確かめて作った。
var lintRawGapInputs = map[string]string{
	// SLOP_WORDS のうち corpus に出ない 5 語。
	"slop-words": "泥臭さを残したまま設計します。\n\n営みの記録を残します。\n\n羅針盤として使います。\n\n起爆剤になります。\n\n触媒の役割を持ちます。\n",
	// METAPHOR_VERB_PATTERNS のうち corpus に出ない 5 型 (踏み込む・収斂する・文化が醸成される・プロセスが定着する・事例が残した)。
	"metaphor-verbs": "実装に踏み込んで調べます。\n\n方向性が議論の末に収斂した。\n\n文化が醸成される。\n\nプロセスが定着する。\n\n事例が残した教訓です。\n",
	// heading_colon の「とは：」と redundant_bracket の 5 つの補足。
	"heading-forms": "# 仕組みとは：何か\n\n## 使い方（素の出力）\n\n## 図（いわゆる）\n\n## 表（概要）\n\n## 経緯（詳細）\n\n## 反省（感謝と設計への反映）\n\n本文です。\n",
	// count_declaration の 2 つの型 (「Xは N つです・あります」と「以下の N つの観点・ポイント」) の、数字の 3 つの書き方。
	"count-declaration": "理由は三つです。\n\nコツは３つあります。\n\n方法は3つです。\n\n観点は三つあります。\n\n以下の３つのポイントを見ます。\n\n以下の三つの観点を見ます。\n\nポイントは3つあります。\n",
	// dialogue_period: 台詞が 10 個以上で、「。」で終わる台詞が半分以上 (script だけ)。
	"dialogue-period": strings.Repeat("「こんにちは。」\n", 12),
	// 行頭が空白のバッジの行 (imageBreakRow・imageOnlyRow が先頭の空白を飛ばして見る)。
	"badge-indented": "  [![b](x)](y)\n\n前の文です。\n  [![c](x)](y)\n次の文です。\n",
}

// lintRawGapNames は lintRawGapInputs の名前の昇順。
func lintRawGapNames() []string {
	names := make([]string, 0, len(lintRawGapInputs))
	for name := range lintRawGapInputs {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
