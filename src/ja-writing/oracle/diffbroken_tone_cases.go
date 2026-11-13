package oracle

// 5 本目の敵対的レビューで、壊した形にしても記録の検査が緑のまま通った所を分ける組。本家 (Python) の答えが、壊した形の Go の答えと違う入力で、乱数で探した最小に近いもの。
// tone と stiffness は文書の種類 (docType) ごとに呼ぶ。壊した形は case の名前の先頭の語が示す関数の、下の所:
//   tone-pseudo-line   番号つき項目・見出しの見かけを、伏せた後の文ではなく行そのもので判定する (インラインのコードのある番号つき項目)
//   tone-question-mask 間接の問いで減らす「？」を数える masker が、見出しの行を外す (見出しの行の「？」)
//   tone-cut-whole     抜き出しが空になったときの代わりの長さ (幅の 2 倍)
//   tone-strip-blank   空白だけの行 (全角空白) を空の行と同じに扱う判定
//   stiff-best-tie     文書全体の例に、同点の最大が 2 つあるとき並べ替えた先頭ではなく本家の並びの最初を選ぶ

type diffToneBroken struct {
	name, old, new string
	docType        any
}

func diffToneBrokenCases() []diffToneBroken {
	return []diffToneBroken{
		{name: "tone-pseudo-line-promo", old: "\u2764\ufe0fで手順の工場!\r\n# 確かとように #\r\n\u2764をがすることが可能はを図るに\u2122\uff1f\r\n1. 項目の絶対値の期限の手順実施を行う今期イチオシ\u2049につながるを\u2026【バージョン注意】window-style/window-active-style は古くからある。`cursor-style` / `cursor-colour` は 3.4+。nvim 等が自前で背景を塗るアプリ内では反映されず、素のプロンプトのペインで最も反映が目立つ。障害を図るできる営業部の手順の対応の対応\uff1f\r\n**直せるかと聞かれた#\ufe0f\u20e3を行う保存の絶対パス結果手順の保存の実施イチオシ。**[リンク\uff01](http://a.b?q)、一日\u200d\uff1f",
			new: "\u2764\ufe0fで手順の工場!\r\n# 確かと画面の条件 #の実施\uff0e\r\n\u2764をがすることが可能はを図るに\u2122\uff1f\r\n1. 項目の絶対値の期限の手順実施を行う今期イチオシ\u2049につながるを\u2026【バージョン注意】window-style/window-active-style は古くからある。`cursor-style` / `cursor-colour` は 3.4+。nvim 等が自前で背景を塗るアプリ内では反映されず、素のプロンプトのペインで最も反映が目立つ。障害を図るできる営業部の手順の対応の対応\uff1f\r\n直せるかと聞かれた対応!\n", docType: "promo"},
		{name: "tone-question-mask-heading", old: "は\uff01\uff01\n## \u203cに絶対値の絶対値の検討はとても\uff01\uff01で期限イチオシ結果絶対パス\u2028ように\uff0e\nずらり何かとに関してぜひとても\uff0eしますため!?\n進めてよいかどうかぜひわかる確認の実施で手順の条件の営業部の手順\uff1f\n実施は\u2028確認とてもを担保\uff1f- [Slack: 2025-05 terms / rate limit update and FAQ](https://api.slack.com/changelog/2025-05-terms-rate-limit-update-and-faq)\n絶対最後の三つ1\ufe0f\u20e3何かとすごく条件\u203c?**できるぜひ1\ufe0f\u20e3保存三つの保存の実施の実施\uff01\uff01**\n---",
			new: "は\uff01\uff01\n## \u203cに絶対値の絶対値の検討はとても\uff01\uff01絶対で期限イチオシ結果絶対パス\u2028ように\uff0e\nずらり何かとに関してぜひとても\uff0eしますため!?\n進めてよいかどうかぜひわかる\n実施は\u2028確認とてもを担保\uff1f- [Slack: 2025-05 terms / rate limit update and FAQ](https://api.slack.com/changelog/2025-05-terms-rate-limit-update-and-faq)\n絶対最後の三つ何かとすごく条件?**検討の条件遅延の設定1\u20e3保存三つの保存の実施の実施\uff01\uff01**\n---を実施し\u2026", docType: nil},
		{name: "tone-cut-whole-promo", old: "設\u3000あすごく",
			new: "設\u3000あすかなりけう認定きあ\uff01 b確けえき確えう定いここ\uff01うけえ\uff1f、きおうけおcこbえ設きい cお認設aa", docType: "promo"},
		{name: "tone-strip-blank-mail", old: "?\u3000",
			new: "\u3000", docType: "mail"},
	}
}

// diffStiffBrokenCases は群 Z の段の組 (stiffness を文書の種類なしで呼ぶ)。
func diffStiffBrokenCases() []diffToneBroken {
	return []diffToneBroken{
		{name: "stiff-best-tie", old: "うこぜひいb\uff1faおき\uff01cあ\uff1fく\uff1fう \uff1faえいかb",
			new: "うこ、c認いc\uff1f。定設けb定\uff1fくういこういけ、。かa定お"},
	}
}
