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
	// G1 の境目 (excess_bold: 地の文 300 字は出ず 301 字で出る・1,000 字あたり 3.0 個ちょうどは出ず 4 個で出る。
	// excess_list: 比率 0.25 ちょうどは出ない・<details> の中の箇条書きを除いた字数 300 は出ず 301 で出る)。corpus の指標は、この境目のどちらにも当たらない。
	"g1-bold-300":     strings.Repeat("**あ**\n\n", 10) + strings.Repeat("い", 250) + "\n",
	"g1-bold-301":     strings.Repeat("**あ**\n\n", 10) + strings.Repeat("い", 251) + "\n",
	"g1-bold-3-exact": strings.Repeat("**あ**\n\n", 3) + strings.Repeat("い", 985) + "\n",
	"g1-bold-4":       strings.Repeat("**あ**\n\n", 4) + strings.Repeat("い", 980) + "\n",
	"g1-list-quarter": "- " + strings.Repeat("い", 100) + "\n\n" + strings.Repeat(strings.Repeat("い", 100)+"\n\n", 3),
	"g1-gate-300":     g1DetailsDoc(74),
	"g1-gate-301":     g1DetailsDoc(75),
	// sentence_end_repetition の語尾のうち corpus に出ない 4 つ (だ・である・でした・だろう)。
	"g2-endings": "これは仕様だ。これは設計だ。これは方針だ。\n\nこれは仕様である。これは設計である。これは方針である。\n\nこれは仕様でした。これは設計でした。これは方針でした。\n\nこれは仕様だろう。これは設計だろう。これは方針だろう。\n",
	// 絵文字で文を続けた所の分け方 (句点なしで 3 つ・句点と絵文字・先頭のコードが伏せられて見える文字と元の行の幅が違うとき) ・分けた後ろが 3 字のとき (分けない)・閉じかっこで終わる文 (語尾に数えない)。
	"g2-decoration": "これはAです🚀これはBです🚀これはCです🚀\n\n一つ目の説明です。🚀\n二つ目の説明です。🚀\n三つ目の説明です。🚀\n\n`コード` これはAです🚀これはBです🚀これはCです🚀\n\n一つ目の説明です）\n二つ目の説明です）\n三つ目の説明です）\n\n一つ目の説明だ。二つ目の説明だ。三つ目の説明です🚀OKだ\n",
	// emoji_prohibited の、引用の中 (引用の中でも出す)・引用の中の箇条書き・4 個以上 (message は最初の 3 つ)・ZWJ でつないだ絵文字・旗・キーキャップ・コードの中 (出さない)。
	"g4-emoji": "> 引用の中の絵文字🚀です。\n>\n> - 引用の中の箇条書き✅です。\n\n五つ🚀✅❌🎉🔥並べます。\n\n家族👨\u200d👩\u200d👧です。\n\n旗🇯🇵とキー1️⃣です。\n\n`🚀` はコードの中です。\n",
	// 区間を表すダッシュ (「東京―大阪間」) を普通名詞の語尾で取り違えない 13 語 (corpus は 人間・時間・目線 だけ)・語尾が接尾辞の 1 字・2 字だけのとき・連続したダッシュ・端・名詞でない隣・記号だけの行・
	// dash_list_ending の末尾 (絵文字・句点・ピリオド・字数の上限・文頭の位置)。
	"g5-dash": "東京―大阪期間です。\n\n東京―大阪空間です。\n\n東京―大阪世間です。\n\n東京―大阪仲間です。\n\n東京―大阪手間です。\n\n東京―大阪視線です。\n\n東京―大阪動線です。\n\n東京―大阪光線です。\n\n東京―大阪伏線です。\n\n東京―大阪生命線です。\n\n東京―大阪方便です。\n\n東京―大阪利便です。\n\n東京―大阪不便です。\n\n東京―線です。\n\n東京―区間です。\n\n東京―間です。\n\n東京―人間線です。\n\n東京――大阪間です。\n\n―大阪間です。\n\n東京間―\n\nTokyo―大阪間です。\n\n東京―Osaka間です。\n\n東京―大阪区間です。\n\n――◆――\n\n―あ―\n\n速さ、軽さ、静けさ──🚀\n\n速さ、軽さ、静けさ──。\n\n速さ、軽さ、静けさ──.\n\n速さ、軽さ──。\n\nあいうえおかきくけこさしす、軽さ、静けさ──。\n\n文です。速さ、軽さ、静けさ──。\n\n速さ、軽さ、静けさ──です。\n\n\n\nコーヒー―大阪間です。\n\n人々―大阪間です。\n\n―\u2003―",
	// G6 の境目と形 (short_summary_heading: 地の文 1,999 字は出て 2,000 字は出ない・「おわりに」・見出しの水準・末尾の絵文字と空白・「まとめと感想」は出ない。heading_colon: 半角のコロン)。
	"g6-summary-1999":  "# まとめ\n\n" + strings.Repeat("い", 1995) + "\n",
	"g6-summary-2000":  "# まとめ\n\n" + strings.Repeat("い", 1996) + "\n",
	"g6-summary-forms": "# おわりに\n\n## まとめ🚀\n\n### おわりに 🎉\n\n#### まとめ  \n\n##### まとめ\n\n###### おわりに\n\n## まとめと感想\n\n## 仕組みとは:何か\n\n本文です。\n",
	// G7 (trailing_colon): 全角と半角・コロンだけの行 (2 つは出る・3 つ以上は出ない)・http で始まる行・コードを伏せて http で始まる行・箇条書き・行末の空白・URL の後ろのコロン・コードの中のコロン・最後のコロンが保護された範囲に入る行 (閉じない `<https://…`。行末の空白の有無)。
	"g7-colon": "次の通りです：\n\n次の通りです:\n\n::\n\n:::\n\n::::\n\nhttp://example.com/a:\n\n`x` http://example.com/a:\n\nhttpの話:\n\n- 項目:\n\n  次です：  \n\n見る https://example.com/a:\n\n`コード:` です\n\n`コード:`\n\n見出しの下:\n\n見る <https://a.b/c:\n\n見る <https://a.b/c:  \n",
	// G8 の語・型のうち corpus に出ないもの (SLOP_WORDS の 21 語・比喩動詞「引き返す」「添える」)・語の形で除く例外・「壊れる」と「静かに壊れる」の二重の反応・比喩の「効く」の枝 (文字どおり・慣用句・打ち消し・名詞の主語・効き目・だんだん効く・時間と前後の行)・「版」と「回帰」の例外。
	"g8-slop-words":    "肌感を見ます。\n\n肌感覚を見ます。\n\n体温が上がる。\n\n温度感を測る。\n\n熱量を見る。\n\n血の通った文章です。\n\n泥臭い作業です。\n\n腹落ちしました。\n\nメンタルモデルを作る。\n\n本質的な話です。\n\n地に足のついた設計です。\n\n等身大の表現です。\n\n虚飾を排す。\n\n境地に達する。\n\n美学がある。\n\n深淵を覗く。\n\n冷徹な判断です。\n\n禁欲的な設計です。\n\n優美な書き方です。\n\n極致に至る。\n\n宿命を負う。\n",
	"g8-slop-patterns": "ナビゲートします。\n\n搭乗ゲートへ行く。\n\nレビューゲートを置く。\n\nゲートウェイを置く。\n\n安全装置を付ける。\n\n思考装置として使う。\n\nこの装置は毎朝自動で起動します。\n\n真理値を返す。\n\n人生の真理を語る。\n\n推移閉包を取る。\n\n会計台帳を見る。\n\n経営OSを入れ替える。\n\nOSXを使う。\n\n本質を突く話です。\n",
	"g8-metaphor":      "動かしながら引き返す。\n\n代わりに添える。\n\nシステムが壊れた。別の所では静かに壊れる。\n\nデータが静かに壊れる。\n\n静かに落ちる。\n\n黙って無視される。\n\n時間を溶かした。\n\n前提が崩れる。\n\n収斂した議論です。\n\n最初の文です。 彼が到着した瞬間でした。\n",
	"g8-kiku":          "薬が効く。\n\n融通が効く。\n\n気が効く。\n\n地味に効く。\n\nよく効かない。\n\n見直しは効かない。\n\n見直しが効いた。\n\n工夫したことが効く。\n\n見直しの効き目が出る。\n\n効き目が出る。\n\nここで効いてくるのが設計だ。\n\n30分ほどで効いてきます。\n\nコーヒーを飲むと効いてくる。\n\n後から効く。\n\n徐々に効く。\n\nキャッシュを入れた。ここで効いてくるのが設計だ。\n\nキャッシュが効く。\n\n服用した。後から効いてくる。\n",
	"g8-translated":    "この版では速くなりました。\n\n辞書のこの版では速くなりました。\n\nこの版では速い。版を更新した。\n\n日本語版を更新した。\n\n回帰が発生した。\n\n回帰テストを回す。\n\n原点回帰が起こる。\n",
	"g8-kiku-window":   "服用します。\n\nここで効いてくるのが設計です。\n\n本文です。\n\n続きです。\n\nさらに続きです。\n\nここで効いてくるのが設計です。\n",
	"g8-kiku-adverb":   "後から効く。\n\n徐々に効く。\n\nあとで効かない。\n\nじわじわと効いてくる。\n\n次第に効いてきた。\n\nキャッシュを入れた。ここで効いてくるのが設計だ。\n",
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

// g1DetailsDoc は <details> の中の長い箇条書き 1 行と、外の短い箇条書き 2 行・地の文 2 行の文書。外の字数 (箇条書きの門に使う字数) は 152 + firstProse + 74 (firstProse が 74 で 300)。
func g1DetailsDoc(firstProse int) string {
	return "<details>\n\n- " + strings.Repeat("い", 100) + "\n\n</details>\n\n- " + strings.Repeat("い", 75) + "\n\n- " + strings.Repeat("い", 75) + "\n\n" +
		strings.Repeat("い", firstProse) + "\n\n" + strings.Repeat("い", 74) + "\n"
}
