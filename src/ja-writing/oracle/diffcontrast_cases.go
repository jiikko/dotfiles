package oracle

import (
	"math/rand/v2"
	"strconv"
	"strings"
	"unicode/utf8"
)

// 群 S (否定対比の逆転) の記録: 段 _contrast_risks(sides, groups) と、その下の関数と定数。
// 組は、群 M までと同じ入れ物 (diffBags) に、専用の手書きの組と固定の種 (PCG 1076/24・25) の組を足したもの。
// 文は「元の否定対比の文」と「推敲後の文」の書式の表から作る。書式は本家のコメントと docstring の例文と、正規表現の選択肢 1 つずつを狙う。

// diffContrastConstants は群 S の Go の表と比べる本家の定数 (正規表現は文字列と flags)。
var diffContrastConstants = []string{
	"_CONTRAST", "_ZU_MARK", "_NOT_ONLY", "_RHETORICAL", "_SUBORDINATE", "_CLAUSE_BREAK", "_TOPIC", "_TOKEN_OKURI_MID", "_TOKEN", "CONTRAST_WEIGHTS", "_STOP_TOKENS",
	"_NEG_AFTER", "_DEMOTE_WORD", "_OTHER_NOUN", "_DEMOTE_WORD_OTHER", "_LEXICAL_NEG", "_COORD_HEAD", "_TOPIC_MARKS", "_PROMOTE_TO", "_USE_A", "_DEMOTE_REDUCE", "_DEMOTE",
	"_REDUCE_ITSELF", "_DEMOTE_ANCHORED", "_A_PHRASE", "_OTHER_OBJECT", "_SWITCH_TO", "_SWITCH_FROM", "_CONTRAST_SKIP_KINDS", "_ALSO_AFTER", "_COORD_AFTER", "_COMPARE_CLAUSE",
	"_HYPOTHESIS", "_ADVERSATIVE_END", "_WEIGHT_PATTERN", "_WEIGHT_ONLY_CLAUSE", "_ADJ_NOUN", "_SIDE_REPLACED", "_SIDE_REJECTED", "_KATAKANA_HEAD", "_LATIN_HEAD", "_SHOWN_TAIL",
	"_SHOWN_TAIL_NOT_HEAD", "_SHOWN_TAIL_PARTICLES", "_SHOWN_TAIL_MO_GA",
}

// DiffContrastFunctions は群 S の関数。
var DiffContrastFunctions = []string{
	"_contrast_risks", "_neg_after", "_clause_bounds", "_is_token", "_negated_tokens", "_shown_word", "_demoted", "_other_object",
	"_affirmed_with_weight", "_side_affirmed", "_affirmed_plain", "_original_affirms", "_continues_word", "_b_side_phrases", "_head_clue",
}

// DiffContrastConstantNames は定数の名前 (Go の突き合わせが、この名前の Get を回す)。
func DiffContrastConstantNames() []string { return diffContrastConstants }

// diffContrastNouns は否定された語 A と、肯定した側の語 B の候補 (漢字 2 字以上・片仮名・英字・送り仮名つき・1 字の漢字と さ/み・「の」を挟む語)。
var diffContrastNouns = []string{
	"品質", "速度", "コスト", "見た目", "使い方", "締め切り", "性能", "手作業", "自動化", "ツール", "習慣", "外部の認証", "キャッシュ", "サーバー", "API", "データの質", "道具",
	"新機能", "見積もり", "出来上がり", "問い合わせ", "言い方", "仕組み", "お知らせ", "会議", "共通化", "速さ", "強み", "高い品質", "支払い期限", "対象", "国内", "社内", "移行", "文書",
	"golang", "C++", "テスト", "事業", "施設", "設定", "グローバル変数", "古いAPI", "手順", "自前の認証", "社内のツール", "リリース", "障害",
}

// diffContrastOld は原文の否定対比の文の書式 ({A} が否定された語、{B} が肯定した語)。
var diffContrastOld = []string{
	"{A}ではなく{B}を使う。", "{A}ではなく、{B}が重要だ。", "{A}ではない。", "{A}じゃなくて{B}です。", "{A}でもなく{B}でもない。", "{A}でなく{B}を選ぶ。", "{A}でなくて{B}を選ぶ。",
	"{A}は使わず{B}を使う。", "{A}は使わずに{B}を使う。", "{A}はせず{B}をする。", "{A}はせずに{B}をする。", "{A}だけでなく{B}も使う。", "{A}ばかりではなく{B}も使う。", "{A}のみでなく{B}も使う。",
	"{A}は目的ではなく手段だ。", "{A}ではないか。", "{A}ではないでしょうか。", "{A}ではありませんか。", "{A}ではないので、まず{B}を試す。", "{A}ではないから、{B}にする。", "{A}ではない場合は{B}にする。",
	"{A}ではなく、{A}の使い方だ。", "{A}ではなく、{A}の{B}だ。", "{A}ではなく、{B}の{A}だ。", "{A}ではなく{A}を使う。", "{A}ではなく、{B}と{A}を使う。", "{A}は{B}ではなく{A}のことだ。",
	"{A}ではありません。{B}を使います。", "{A}ではなかった。{B}だった。", "{A}じゃない、{B}だ。", "{A}ではなく、{B}の使い方が大事だ。", "新たに{A}を始める者ではなく、既に{A}を営んでいる者が対象だ。",
	"## {A}ではなく{B}から変える", "## {A}ではなく{B}で回す", "## {A}は使わず{B}で回す", "| {A}ではなく{B} | 値 |", "- {A}ではなく{B}を使う。", "> {A}ではなく{B}を使う。",
	"{A}は{B}ではなく手段だ。", "{A}でもない。{B}でもない。", "性能改善と{A}ではなく{B}を直す。", "{A}と{B}ではなく{B}を使う。", "{A}ではなく、{B}などを使う。", "{A}なく{B}を使う。",
}

// diffContrastNew は推敲後の文の書式。重みの語・比べて下げる形・並べて肯定する形・仮説・逆接・否定・置き換えを 1 つずつ狙う。
var diffContrastNew = []string{
	"{A}を最優先にする。", "{A}が目的だ。", "目的は{A}だ。", "まず、{A}を使う。", "まず{A}を使う。", "第一に、{A}を使う。", "{A}を重視する。", "何よりも{A}が大切だ。", "{A}が大事だ。", "一番の狙いは{A}だ。",
	"最優先は、{A}を使うことだ。", "最優先は{A}で、{B}はその次だ。", "{A}を最優先にして、{B}はその次にする。", "{B}を最優先にして{A}はその次にする。", "{A}は大事だが{B}はおまけだ。",
	"{B}を重視してその次に{A}を見る。", "{A}を重視し、{B}も重視する。", "{A}を中心にする。", "{A}を目指す。", "{A}を筆頭にする。", "{A}が主眼だ。", "{A}が主な理由だ。", "{A}が最も重要だ。",
	"{A}より{B}を重視する。", "{A}の代わりに{B}を最優先にする。", "{A}に代わり{B}を最優先にする。", "{A}に替えて{B}を最優先にする。", "{B}を最優先にして{A}は後にする。", "{A}は見送る。{B}が目的だ。",
	"目的は{B}で、{A}は後日にする。", "{A}を書く前に{B}を重視する。", "{A}を減らすことを最優先にする。", "{A}は減らすことが目的だ。", "{A}は減らす。目的は{B}だ。", "{A}の数は減らす。目的は{B}だ。",
	"{A}を{B}に置き換えるのが最優先だ。", "まず、{A}を{B}に置き換える。", "まず、{B}を{A}に置き換える。", "最優先は、{A}を使う構成に移行することだ。", "まず、{A}を本番に移す。", "まず、{A}を先頭に移す。",
	"{B}を{A}に替えるのが目的だ。", "{B}が{A}に代わるのが目的だ。", "{A}から{B}に切り替えるのが目的だ。", "{A}から{B}へ移行するのが目的だ。", "{A}は{B}に変更するのが最優先だ。",
	"まず{A}を疑ったが、{B}だった。", "{A}が目的だと考えていたが、{B}だった。", "{A}もできますが、目的は{B}です。", "{A}が最優先と思っていたが、{B}だった。", "{A}が目的だと考えられます。",
	"{A}だけでなく{B}も使う。", "{A}ばかりでなく{B}も使う。", "{A}のみならず{B}も使う。", "{A}に加え{B}も使う。", "{A}に限らず{B}も使う。", "{A}のほか{B}も使う。", "{A}のほか{B}は使わない。",
	"{A}と{B}を使う。", "{A}や{B}を使う。", "{A}・{B}を使う。", "{A}及び{B}を使う。", "{A}および{B}を使う。", "{A}並びに{B}を使う。", "{A}と{B}の両方を調べた。", "{A}と{B}を比べた。",
	"{A}と{B}のうち{A}を選ぶ。", "{A}と{B}を確認した。", "{A}と{B}だと確認した。", "{A}と{B}であることを確認した。", "{A}と{B}について確認した。", "{A}と{B}を再確認した。",
	"{A}という{B}を使う。", "{A}として{B}を使う。", "{A}とは{B}のことだ。", "{A}との{B}を使う。", "{A}と同じ{B}を使う。", "{A}と違う{B}を使う。", "{A}となる{B}を使う。", "{A}とする{B}を使う。",
	"{A}と{B}を試したが、{B}にした。", "{A}と{B}を並べて試した。", "{A}と{B}を検討した。", "{A}と{B}を使ったが、{B}にした。", "{A}と{B}の候補を使う。", "{A}と{B}を使う。{A}を使う。",
	"{A}を使わない。", "{A}以外を重視する。", "{A}は不要で{B}が目的だ。", "{A}は二の次だ。", "{A}は後回しだ。", "{A}は対象外で、{B}が目的だ。", "{A}と{B}の廃止が目的だ。", "{A}とXの廃止が目的だ。",
	"{A}を使わず{B}を最優先にする。", "{A}を除いて最優先にする。", "{A}抜きで最優先にする。", "{A}は最優先ではない。", "{A}が最優先ではなかった。", "{A}は据え置きで{B}が目的だ。",
	"{A}は{B}を重視する。", "{A}は、その次に{B}を重視する。", "{A}は、目的だ。", "目的は、{A}は使うことだ。", "{A}を後で使う。", "{A}はあとで使う。", "{A}を控える。", "{A}を避ける。", "{A}をやめる。",
	"{A}を止める。", "{A}は見送る。", "{A}は保留する。", "{A}は次回にする。", "{A}は次回からにする。", "{A}は次回も使う。", "{A}は次回から使う。", "{A}を突き止める。", "{A}の前に{B}を最優先にする。",
	"{B}を前に進めて{A}を最優先にする。", "{A}ははやめに使う。", "{A}は後にする。", "{A}を出した後に使う。", "{A}は来週に控え、目的は{B}だ。",
	"{B}を最優先にして{A}はその次にする。", "{A}をその次にする。", "{B}で{A}を避ける。", "移行で障害を避ける。{A}を最優先にする。", "{A}は障害を避ける。", "{A}について特に障害を避ける。",
	"{B}で{A}の話し合いはその次にする。", "{B}は{A}を最優先にしてその次を見る。", "{A}の{B}を最優先にする。", "{A}の{B}が目的だ。", "{B}の{A}を最優先にする。", "{B}をどう使うかが重要だ。",
	"{A}は{B}を使う。{A}を最優先にする。", "{A}を使う。{B}を最優先にする。", "{A}は、{B}を最優先にする。", "## まず、{A}を使う", "## {A}を最優先にする", "## まず、{A}の手順を{B}にする",
	"## まず習慣、{A}はその次", "## まず、{A}で回す", "## {A}を使う", "## 最優先は、{A}を使うこと", "- {A}を最優先にする。", "> {A}を最優先にする。", "| {A}を最優先 | 値 |", "`{A}`を最優先にする。",
	"{A}が{B}に代わる。目的は{A}だ。", "{A}の廃止と{B}を最優先にする。", "{A}と{A}を最優先にする。", "{A}を重視して、{B}を重視する。", "{B}を重視して、{A}を重視する。",
}

// diffContrastNegations は正規表現の選択肢と後読みの端 (否定の語の前の字・後ろの字・主題の「は」・節の切れ目) を狙う手書きの文。
var diffContrastHandSentences = []string{
	"共通化は目的ではなく手段だ。", "共通化が目的ではなく、手段だ。", "新たに事業を始める者ではなく、既に事業を営んでいる者が対象だ。", "道具ではなく、道具の使い方が大事だ。", "道具ではなく、道具の淹れ方が大事だ。",
	"データの質ではなく量が大事だ。", "データの質ではなく、データの質が大事だ。", "見た目ではなく中身を見る。", "高い品質ではなく速度を重視する。", "支払い期限ではなく金額を確認する。",
	"会議などではなく文書で伝える。", "問い合わせではなくお知らせを見る。", "見積もりではなく請求を見る。", "出来上がりではなく途中を見る。", "国内外ではなく国内が対象外だ。",
	"対象外ではなく対象だ。", "国内外の対応が最優先だ。", "社内外の対応が最優先だ。", "対象外の項目が目的だ。", "性能改善と古いAPIの廃止が最優先だ。", "性能改善とAPIの廃止が目的だ。",
	"品質ではなく速度が最優先です。", "品質じゃなくて速度が最優先です。", "品質でもなく速度でもなく最優先は安全です。", "まだ本番環境ではないので、まず検証環境で試します。", "キャッシュではないか、まず調べる。",
	"キャッシュではないでしょうか。", "キャッシュではなくて、目的は速度だ。", "キャッシュだけでなく速度も重視する。", "キャッシュばかりではなく速度も重視する。", "キャッシュのみでなく速度も重視する。",
	"手作業を使わず自動化する。", "手作業はせず自動化する。", "手作業は使わずに自動化する。", "必ず確認する。", "まず確認する。", "まず、確認する。", "とりあえず確認する。", "少しずつ確認する。",
	"まずい。", "まず手作業は使わず、自動化を最優先にする。", "まず、手作業は使わず自動化する。", "目的は、手作業は使わず自動化することだ。", "最優先は、手作業はせず自動化することだ。",
}

// diffContrastHandPairs は否定対比の手書きの組 (名前, 元, 後)。本家のコメントと docstring の例を 1 つずつ。
func diffContrastHandPairs() []diffFullCase {
	doc := diffDoc
	return []diffFullCase{
		{name: "same", old: doc("品質ではなく速度を使う。"), new: doc("品質ではなく速度を使う。")},
		{name: "plain-reversal", old: doc("品質ではなく速度が目的だ。"), new: doc("品質が目的だ。")},
		{name: "comment-mitame", old: doc("見た目ではなく中身が大事だ。"), new: doc("見た目と中身が大事だ。")},
		{name: "comment-kyotsuka", old: doc("共通化は目的ではなく手段だ。"), new: doc("共通化が目的だ。")},
		{name: "comment-jigyo", old: doc("新たに事業を始める者ではなく、既に事業を営んでいる者が対象だ。"), new: doc("事業を始める者が最優先だ。")},
		{name: "comment-dougu-no", old: doc("道具ではなく、道具の使い方が大事だ。"), new: doc("道具が大事だ。")},
		{name: "comment-dougu-rephrase", old: doc("道具ではなく、道具の使い方が大事だ。"), new: doc("道具をどう使うかが大事だ。")},
		{name: "comment-data-quality", old: doc("データではなく、データの質が大事だ。"), new: doc("質の高いデータが大事だ。")},
		{name: "comment-heading-habit", old: doc("## ツールではなく習慣から変える"), new: doc("## まず習慣、ツールはその次")},
		{name: "comment-heading-mazu-a", old: doc("## 手作業ではなく自動化で回す"), new: doc("## まず、手作業の手順を自動化する")},
		{name: "comment-heading-mazu-b", old: doc("## 手作業ではなく自動化で回す"), new: doc("## まず、手作業で回す")},
		{name: "comment-zu-heading", old: doc("## 手作業は使わず自動化で回す"), new: doc("## まず、手作業で回す")},
		{name: "comment-zu-mazu", old: doc("手作業は使わず自動化で回す。"), new: doc("まず、手作業で回す。")},
		{name: "comment-quality-top", old: doc("品質ではなく速度が最優先だ。"), new: doc("品質が最優先で、締め切りはその次です。")},
		{name: "comment-demote-sonotsugi", old: doc("締め切りではなく品質を重視する。"), new: doc("品質を最優先にして速度はその次にする。")},
		{name: "comment-demote-sonotsugi-a", old: doc("品質ではなく速度を重視する。"), new: doc("品質を最優先にして速度はその次にする。")},
		{name: "comment-demote-sonotsugi-b", old: doc("速度ではなく品質を重視する。"), new: doc("品質を最優先にして速度はその次にする。")},
		{name: "comment-demote-sonotsugi-c", old: doc("見た目ではなく中身を重視する。"), new: doc("見た目は大事だが中身はおまけだ。")},
		{name: "comment-demote-other", old: doc("品質ではなく速度を重視する。"), new: doc("品質を最優先にしてほかはその次にする。")},
		{name: "comment-demote-coastal", old: doc("コストではなく原因を重視する。"), new: doc("コストが原因の不具合はその次にする。")},
		{name: "comment-replace-ext", old: doc("手作業ではなく自動化を使う。"), new: doc("まず、外部の認証を自前の認証に置き換える。")},
		{name: "comment-replace-heading", old: doc("手作業ではなく自動化を使う。"), new: doc("## 最優先は、手作業を自動化に置き換えること")},
		{name: "comment-replace-topic", old: doc("手作業ではなく自動化を使う。"), new: doc("まず、手作業は自動化に置き換える。")},
		{name: "comment-replace-promote", old: doc("新機能ではなく既存機能を使う。"), new: doc("まず、新機能を本番に移す。")},
		{name: "comment-replace-use-a", old: doc("クラウドではなく手元を使う。"), new: doc("最優先は、クラウドを使う構成に移行することだ。")},
		{name: "comment-reduce-itself", old: doc("会議ではなく文書を使う。"), new: doc("会議は減らすことを最優先にする。")},
		{name: "comment-reduce-other", old: doc("会議ではなく文書を使う。"), new: doc("文書を最優先にして会議は減らす。")},
		{name: "comment-demote-mae", old: doc("コードではなく設計を見る。"), new: doc("コードを書く前に設計を最優先にする。")},
		{name: "comment-demote-mae-susumeru", old: doc("コードではなく設計を見る。"), new: doc("コードを前に進めることを最優先にする。")},
		{name: "comment-demote-ato", old: doc("速度ではなく品質を見る。"), new: doc("速度を出した後に品質を最優先にする。")},
		{name: "comment-demote-ato-ni", old: doc("速度ではなく品質を見る。"), new: doc("速度は後に回し、品質を最優先にする。")},
		{name: "comment-demote-jikai", old: doc("速度ではなく品質を見る。"), new: doc("速度は次回から始め、品質を最優先にする。")},
		{name: "comment-demote-jikai-ni", old: doc("速度ではなく品質を見る。"), new: doc("速度は次回からにする。品質を最優先にする。")},
		{name: "comment-demote-tome", old: doc("速度ではなく品質を見る。"), new: doc("速度を食い止めることが最優先だ。")},
		{name: "comment-demote-yame", old: doc("速度ではなく品質を見る。"), new: doc("速度をやめて品質を最優先にする。")},
		{name: "comment-demote-hayame", old: doc("速度ではなく品質を見る。"), new: doc("速度ははやめに出すことが最優先だ。")},
		{name: "comment-demote-hikae", old: doc("速度ではなく品質を見る。"), new: doc("来週に控え、速度が最優先だ。")},
		{name: "comment-demote-other-object", old: doc("移行ではなく品質を見る。"), new: doc("移行で障害を避けることが最優先だ。")},
		{name: "comment-demote-other-object-a", old: doc("移行ではなく品質を見る。"), new: doc("移行は障害を避けることが最優先だ。")},
		{name: "comment-demote-phrase-a", old: doc("新機能ではなく品質を見る。"), new: doc("新機能は今回のリリースでは見送りが最優先だ。")},
		{name: "comment-demote-phrase-b", old: doc("契約社員ではなく品質を見る。"), new: doc("契約社員への適用は見送るのが最優先だ。")},
		{name: "comment-demote-phrase-c", old: doc("キャッシュではなく品質を見る。"), new: doc("キャッシュを本番で使うのは避けるのが最優先だ。")},
		{name: "comment-switch-to", old: doc("手作業ではなく自動化を使う。"), new: doc("最優先は、手作業を自動化に替えることだ。")},
		{name: "comment-switch-to-ga", old: doc("手作業ではなく自動化を使う。"), new: doc("最優先は、自動化が手作業に代わることだ。")},
		{name: "comment-side-replaced", old: doc("手作業ではなく自動化で回す。"), new: doc("まず、手作業で自動化のツールを置き換える。")},
		{name: "comment-side-rejected", old: doc("手作業ではなく自動化で回す。"), new: doc("まず、手作業で回して自動化を中止する。")},
		{name: "comment-side-rejected-a", old: doc("手作業ではなく文書で伝える。"), new: doc("まず、手作業で回して文書は省く。")},
		{name: "comment-side-also", old: doc("手作業ではなく自動化で回す。"), new: doc("まず、手作業で回して自動化も試す。")},
		{name: "comment-also-after", old: doc("手作業ではなく自動化を使う。"), new: doc("手作業だけでなく自動化も使う。")},
		{name: "comment-also-after-neg", old: doc("手作業ではなく自動化を使う。"), new: doc("手作業だけでなく自動化も使わない。")},
		{name: "comment-coord", old: doc("手作業ではなく自動化を使う。"), new: doc("手作業と自動化を使う。")},
		{name: "comment-coord-compare", old: doc("手作業ではなく自動化を使う。"), new: doc("手作業と自動化の両方を調べた。")},
		{name: "comment-coord-confirm", old: doc("手作業ではなく自動化を使う。"), new: doc("手作業と自動化を順に確認し、使った。")},
		{name: "comment-coord-confirm-result", old: doc("手作業ではなく自動化を使う。"), new: doc("手作業と自動化だと確認した。")},
		{name: "comment-coord-tameshi", old: doc("手作業ではなく自動化を使う。"), new: doc("手作業と自動化を並べて試した。")},
		{name: "comment-coord-adversative", old: doc("手作業ではなく自動化を使う。"), new: doc("手作業と自動化を使ったが、自動化にした。")},
		{name: "comment-coord-quote", old: doc("手作業ではなく自動化を使う。"), new: doc("手作業という自動化を使う。")},
		{name: "comment-hypothesis", old: doc("キャッシュではなく書き込みを調べる。"), new: doc("まずキャッシュを疑ったが、書き込みだった。")},
		{name: "comment-hypothesis-kangae", old: doc("キャッシュではなく書き込みを調べる。"), new: doc("キャッシュが目的だと考えていたが、書き込みだった。")},
		{name: "comment-hypothesis-kangaerareru", old: doc("キャッシュではなく書き込みを調べる。"), new: doc("キャッシュが目的だと考えられます。")},
		{name: "comment-adversative", old: doc("キャッシュではなく書き込みを調べる。"), new: doc("キャッシュもできますが、目的は書き込みです。")},
		{name: "comment-rhetoric", old: doc("キャッシュではないか。まず調べる。"), new: doc("キャッシュが最優先だ。")},
		{name: "comment-subordinate", old: doc("まだ本番環境ではないので、まず検証環境で試します。"), new: doc("本番環境が最優先だ。")},
		{name: "comment-lexical-coord", old: doc("性能改善ではなく古いAPIの廃止を使う。"), new: doc("性能改善と古いAPIの廃止が最優先だ。")},
		{name: "comment-gai-nai", old: doc("国内外ではなく国内を使う。"), new: doc("国内外が最優先だ。")},
		{name: "comment-gai", old: doc("対象ではなく範囲を使う。"), new: doc("対象外が最優先だ。")},
		{name: "comment-shown-nado", old: doc("会議などではなく文書を使う。"), new: doc("会議を最優先にする。")},
		{name: "comment-shown-tsumori", old: doc("見積もりではなく請求を使う。"), new: doc("見積もりを最優先にする。")},
		{name: "comment-shown-agari", old: doc("出来上がりではなく途中を使う。"), new: doc("出来上がりを最優先にする。")},
		{name: "comment-shown-toiawase", old: doc("問い合わせではなくお知らせを使う。"), new: doc("問い合わせを最優先にする。")},
		{name: "comment-adjective-noun", old: doc("支払い期限ではなく金額を使う。"), new: doc("期限を最優先にする。")},
		{name: "comment-adjective-noun-a", old: doc("高い品質ではなく速度を使う。"), new: doc("品質を最優先にする。")},
		{name: "comment-sa-mi", old: doc("速さではなく強みを使う。"), new: doc("速さを最優先にする。")},
		{name: "comment-katakana", old: doc("サーバーではなくクライアントを使う。"), new: doc("サーバーを最優先にする。")},
		{name: "comment-latin", old: doc("golangではなくRustを使う。"), new: doc("golangを最優先にする。")},
		{name: "comment-original-affirms", old: doc("目的は品質で、品質ではなく速度が大事だ。"), new: doc("品質が最優先だ。")},
		{name: "comment-original-affirms-b-side", old: doc("品質ではなく、品質と速度が大事だ。"), new: doc("品質が最優先だ。")},
		{name: "comment-continues-word", old: doc("施設ではなく、既存施設の長寿命化が大事だ。"), new: doc("施設が最優先だ。")},
		{name: "comment-continues-word-katakana", old: doc("ツールではなく、新ツールが大事だ。"), new: doc("ツールが最優先だ。")},
		{name: "comment-continues-word-latin", old: doc("APIではなく、新APIが大事だ。"), new: doc("APIが最優先だ。")},
		{name: "comment-paired-other", old: doc("品質ではなく速度を使う。", "品質が最優先だ。"), new: doc("品質を最優先にする。", "速度を使う。")},
		{name: "split-sentence", old: doc("品質ではなく速度を使う。"), new: doc("速度を使う。品質を最優先にする。")},
		{name: "merge-sentences", old: doc("品質ではなく速度を使う。確認する。"), new: doc("品質を最優先にして確認する。")},
		{name: "two-sentences-pair", old: doc("品質ではなく速度を使う。設定ではなく手順を使う。"), new: doc("品質を最優先にする。設定を最優先にする。")},
		{name: "same-text-in-old", old: doc("品質ではなく速度を使う。品質が最優先だ。"), new: doc("品質が最優先だ。")},
		{name: "insert-only", old: doc("設定を使う。"), new: doc("設定を使う。", "品質を最優先にする。")},
		{name: "multi-token", old: doc("品質や速度ではなくコストを使う。"), new: doc("品質を最優先にする。速度を最優先にする。")},
		{name: "table-row", old: doc("| 項目 | 値 |", "|---|---|", "| 品質ではなく速度 | 使う |"), new: doc("| 項目 | 値 |", "|---|---|", "| 品質 | 最優先 |")},
		{name: "code-fence", old: doc("```", "品質ではなく速度", "```", "設定を使う。"), new: doc("```", "品質ではなく速度", "```", "品質を最優先にする。")},
		{name: "inline-code", old: doc("`品質`ではなく速度を使う。"), new: doc("`品質`を最優先にする。")},
		{name: "frontmatter", old: doc("---", "title: 品質ではなく速度", "---", "設定を使う。"), new: doc("---", "title: 品質ではなく速度", "---", "品質を最優先にする。")},
		{name: "empty-old", old: "", new: doc("品質を最優先にする。")},
		{name: "empty-new", old: doc("品質ではなく速度を使う。"), new: ""},
		{name: "crlf", old: "品質ではなく速度を使う。\r\n", new: "品質を最優先にする。\r\n"},
	}
}

// diffContrastFuzz は固定の種 (PCG 1076/24) から作った、否定対比の書式の表の組み合わせ。
func diffContrastFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 24))
	noun := func() string { return randPick(r, diffContrastNouns) }
	fill := func(format, a, b string) string { return diffTemplate(diffTemplate(format, "A", a), "B", b) }
	var cases []diffFullCase
	for n := range 2400 {
		a, b := noun(), noun()
		var old, new []string
		for range 1 + r.IntN(3) {
			o := fill(randPick(r, diffContrastOld), a, b)
			if r.IntN(5) == 0 {
				o = fill(randPick(r, diffContrastHandSentences), a, b)
			}
			old = append(old, o)
		}
		for range 1 + r.IntN(3) {
			nw := fill(randPick(r, diffContrastNew), a, b)
			if r.IntN(4) == 0 {
				nw = fill(randPick(r, diffContrastNew), noun(), b)
			}
			new = append(new, nw)
		}
		if r.IntN(4) == 0 {
			// 原文の別の行に肯定の A が出る (組になった文が原文でもともと肯定)
			old = append(old, fill(randPick(r, diffContrastNew), a, b))
		}
		cases = append(cases, diffFullCase{name: "fuzz-" + strconv.Itoa(n), old: diffContrastJoin(r, old), new: diffContrastJoin(r, new)})
	}
	return cases
}

// diffContrastJoin は文を 1 行につなぐ・行を分ける・空行を挟む (0: 同じ行、1: 改行、2: 空行)。見出し・箇条書き・表・引用の文は行頭から始まるので必ず行を分ける。
func diffContrastJoin(r *rand.Rand, sentences []string) string {
	var lines []string
	cur := ""
	for _, s := range sentences {
		block := strings.HasPrefix(s, "#") || strings.HasPrefix(s, "-") || strings.HasPrefix(s, "|") || strings.HasPrefix(s, ">")
		if cur != "" && (block || r.IntN(2) == 0) {
			lines = append(lines, cur)
			cur = ""
		}
		if block {
			lines = append(lines, s)
			continue
		}
		cur += s
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return diffJoinParts(lines, r.IntN(3))
}

// diffContrastTexts は関数の単位の記録に通す文 (新旧の書式を全部、名詞をいくつか入れ替えて作る)。
func diffContrastTexts(r *rand.Rand, n int) []string {
	var formats []string
	formats = append(formats, diffContrastOld...)
	formats = append(formats, diffContrastNew...)
	formats = append(formats, diffContrastHandSentences...)
	var texts []string
	for i := range n {
		f := formats[i%len(formats)]
		a, b := randPick(r, diffContrastNouns), randPick(r, diffContrastNouns)
		texts = append(texts, diffTemplate(diffTemplate(f, "A", a), "B", b))
		if i%7 == 0 {
			// 2 つの書式をつなぐ (節の前の主題の句・前の節の重みの語・複数の A)
			g := formats[r.IntN(len(formats))]
			texts[len(texts)-1] += diffTemplate(diffTemplate(g, "A", a), "B", b)
		}
	}
	return texts
}

func runePos(text string, sub string) int {
	i := strings.Index(text, sub)
	if i < 0 {
		return -1
	}
	return utf8.RuneCountInString(text[:i])
}

// diffContrastMarks は否定対比の印 (原文の否定の文から、印の位置を探す)。
var diffContrastMarks = []string{"ではなくて", "ではなく", "じゃなくて", "じゃなく", "でもなく", "でなくて", "でなく", "ではない", "ではありません", "は使わずに", "は使わず", "はせずに", "はせず"}

// diffContrastStringCalls は否定対比の下の関数の呼び出し (結果そのものを記録に残す)。
func diffContrastStringCalls(texts []string, keep bool) []Call {
	var calls []Call
	add := func(fn string, args ...any) {
		c := dcall(fn, args...)
		c.Keep = keep
		calls = append(calls, c)
	}
	for _, text := range texts {
		add("_negated_tokens", text)
		add("_clause_bounds", text, 0)
		ts := []rune(text)
		var present []string
		for _, nn := range diffContrastNouns {
			if strings.Contains(text, nn) {
				present = append(present, nn)
			}
		}
		for i, nn := range present {
			if i >= 3 {
				break
			}
			p := runePos(text, nn)
			end := p + utf8.RuneCountInString(nn)
			// 否定の印の始まりと終わり (印がなければ、語の終わり)
			markStart, markEnd := end, end
			for _, mk := range diffContrastMarks {
				if q := strings.Index(string(ts[end:]), mk); q >= 0 {
					if at := end + utf8.RuneCountInString(string(ts[end:])[:q]); markStart == end || at < markStart {
						markStart, markEnd = at, at+utf8.RuneCountInString(mk)
					}
				}
			}
			add("_clause_bounds", text, p)
			add("_clause_bounds", text, end)
			add("_shown_word", text, nn, p)
			add("_shown_word", text, nn, markStart)
			after := string(ts[end:])
			add("_neg_after", after, nn)
			add("_demoted", text, 0, p, after)
			add("_demoted", text, p, p, after)
			add("_other_object", string(ts[p:]))
			add("_other_object", after)
			add("_continues_word", text, p, nn)
			add("_b_side_phrases", text, nn, markEnd)
			add("_original_affirms", text, nn, markEnd, markStart)
			add("_original_affirms", text, nn, end, p)
			side := []string{}
			for _, o := range present {
				if o != nn {
					side = append(side, o)
				}
			}
			add("_affirmed_with_weight", text, nn, false, []string{})
			add("_affirmed_with_weight", text, nn, true, side)
			add("_affirmed_with_weight", text, nn, true, []string{})
			add("_affirmed_plain", text, nn, side)
			add("_affirmed_plain", text, nn, []string{"品質"})
			add("_side_affirmed", text, p, nn, len(ts), side)
			add("_side_affirmed", text, markEnd, nn, len(ts), side)
			add("_side_affirmed", text, 0, nn, len(ts), side)
		}
	}
	return calls
}

// diffContrastPairCalls は名詞を 2 つつなげた語の、後ろの名詞が前の字と続くか (字の種類が同じか) の呼び出し。
func diffContrastPairCalls() []Call {
	var calls []Call
	for _, a := range diffContrastNouns {
		for _, b := range diffContrastNouns {
			calls = append(calls, dcall("_continues_word", a+b, utf8.RuneCountInString(a), b))
		}
	}
	return calls
}

// diffContrastSmallCalls は関数の単位の手書きの呼び出し (結果そのものを記録に残す)。
func diffContrastSmallCalls() []Call {
	var calls []Call
	add := keepAdder(&calls)
	for _, tk := range []string{"品質", "見た目", "使い方", "高い品質", "支払い期限", "速さ", "強み", "サーバー", "API", "golang", "C+", "場合", "目的", "こと", "", "あ", "ア", "ツール", "得た知識", "褒め言葉", "見習い期間", "立ち会い検査", "高い", "重要", "最優先", "ほか"} {
		add("_is_token", tk)
	}
	for _, tk := range []string{"使い方", "淹れ方", "質", "方", "あ方", "", "あ"} {
		add("_head_clue", tk)
	}
	for _, p := range []int{0, 1, 5, 100} {
		add("_clause_bounds", "あ、い。う\nえ|お", p)
	}
	for _, c := range []struct {
		text, token string
		position    int
	}{{"既存施設の長寿命化", "施設", 2}, {"新ツール", "ツール", 1}, {"新API", "API", 1}, {"あAPI", "API", 1}, {"施設", "施設", 0}, {"a施設", "施設", 1}, {"ああツール", "ツール", 2}, {"1API", "API", 1}, {"道具", "道具", 0}} {
		add("_continues_word", c.text, c.position, c.token)
	}
	for _, c := range []struct {
		text, token string
		mark        int
	}{{"見積もりではなく", "見積", 4}, {"問い合わせではなく", "問い合", 5}, {"会議などではなく", "会議", 4}, {"出来上がりではなく", "出来上", 5}, {"見た目ではなく", "見た目", 3}, {"使い方ではなく", "使い", 3}, {"会議ではなく", "会議", 2},
		{"会議あではなく", "会議", 3}, {"会議まで\nではなく", "会議", 5}, {"会議などでは", "会議", 4}, {"会議が", "会議", 3}, {"会議もが", "会議", 4}, {"会議ほど", "会議", 4}, {"会議こそ", "会議", 4}, {"", "あ", 0}} {
		add("_shown_word", c.text, c.token, c.mark)
	}
	for _, c := range []struct {
		text, token string
		markEnd     int
	}{{"道具ではなく、道具の使い方だ。", "道具", 4}, {"道具ではなく、道具の淹れ方だ。", "道具", 4}, {"道具ではなく、道具の使い方と道具のコスト。", "道具", 4}, {"道具ではなく道具の", "道具", 4}, {"道具ではなく道具のあ", "道具", 4}, {"道具のX", "道具", 0}} {
		add("_b_side_phrases", c.text, c.token, c.markEnd)
	}
	for _, c := range [][2]string{{"とXは古いAPIの廃止", "性能改善"}, {"とX廃止", "性能改善"}, {"とXの廃止", "性能改善"}, {"外れ", "国内"}, {"外れ", "社内"}} {
		add("_neg_after", c[0], c[1])
	}
	for _, c := range []struct {
		text     string
		left, at int
		after    string
	}{{"XをAに替えBより目的", 0, 2, "に替えBより目的"}, {"会議は減らすことだ", 0, 0, "は減らすことだ"}, {"会議は減らすことを最優先にする", 0, 0, "は減らすことを最優先にする"}, {"XがAに替える", 0, 2, "に替える"}} {
		add("_demoted", c.text, c.left, c.at, c.after)
	}
	for _, c := range []struct {
		text, token string
		clauseOnly  bool
	}{{"品質が目的だが、速度も使う。", "品質", false}, {"まず 、品質を使う", "品質", true}, {"外外外が目的だ", "外外", false}, {"最優先は、品質を使うことだ", "品質", true}, {"まず、品質を使う", "品質", true}} {
		add("_affirmed_with_weight", c.text, c.token, c.clauseOnly, []string{})
	}
	for _, c := range []struct {
		text, token, word string
		position          int
	}{{"まず、手作業で回して自動化にも試す", "手作業", "自動化", 3}, {"をまず手作業で自動化に替える", "手作業", "自動化", 3}, {"まず、手作業で回して自動化を中止する", "手作業", "自動化", 3}, {"まず、手作業で回して自動化を試す", "手作業", "自動化", 3}} {
		add("_side_affirmed", c.text, c.position, c.token, len([]rune(c.text)), []string{c.word})
	}
	add("_original_affirms", "ののの", "のの", 0, 0)
	add("_original_affirms", "大事な道具、ではなく道具の使い方", "道具", 10, 6)
	add("_head_clue", "品質")
	add("_head_clue", "データ")
	for _, s := range []string{"を", "の使い方を", "は障害を避け", "について", "への適用は", "を本番で使うのは", "", "の数を", "をxをy"} {
		add("_other_object", s)
	}
	return calls
}

// diffContrastCases は群 S の case の一覧 (pairs は入力の集まりと diff の入力の組)。
func diffContrastCases(pairs [][3]string) []VisCase {
	cases := []VisCase{{Name: "contrast/constants", Calls: diffConstantCalls(diffContrastConstants)}}
	for _, b := range diffBags(pairs) {
		cases = append(cases, VisCase{Name: "contrast/" + b.name, Calls: diffRiskStageCalls(b.old, b.new, "_contrast_risks", false)})
	}
	for _, g := range []struct {
		prefix string
		cases  []diffFullCase
		keep   bool
	}{{"contrast/hand/", diffContrastHandPairs(), true}, {"contrast/fuzz/", diffContrastFuzz(), false}} {
		for _, f := range g.cases {
			cases = append(cases, VisCase{Name: g.prefix + f.name, Calls: diffRiskStageCalls(f.old, f.new, "_contrast_risks", g.keep)})
		}
	}
	cases = append(cases, VisCase{Name: "contrast/strings/small", Calls: diffContrastSmallCalls()})
	cases = append(cases, VisCase{Name: "contrast/strings/pairs", Calls: diffContrastPairCalls()})
	texts := diffContrastTexts(rand.New(rand.NewPCG(1076, 25)), 1500)
	for i := 0; i < len(texts); i += 60 {
		cases = append(cases, VisCase{Name: "contrast/strings/" + strconv.Itoa(i/60), Calls: diffContrastStringCalls(texts[i:min(i+60, len(texts))], false)})
	}
	return cases
}
