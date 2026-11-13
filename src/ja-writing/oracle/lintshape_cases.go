package oracle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// G18 (文の形) と G19 (文書の型) の文の関数の記録。表 (語の集合・正規表現) は定数の記録で比べ、ここは表を通した判定の結果を比べる
// (frozenset の語を 1 つずつ通すので、表から語を落としたり足したりすると結果が変わる)。

// lintShapeSentenceCalls は 1 つの文に、G18 の 3 つの文の判定を通す呼び出し。
func lintShapeSentenceCalls(s string) []Call {
	rec := recorder{true}
	return []Call{rec.call("_say_hedge_sentence", s), rec.call("_copy_comma_noun_sentence", s), rec.call("_bare_verb_chain_sentence", s)}
}

// lintShapeSentences は G18 の判定に通す手書きの文。語の表の項目ごとに 1 つずつ、境目 (字数・読点の数・句点の有無) を足す。
func lintShapeSentences() []string {
	out := []string{
		"", "。", "、。", "と言えるでしょう。", "といえるでしょう。", "と言えるでしょう", "と言えるでしょう！", "と言えるでしょう!", "と言えるでしょうか。", "と言えるでしょう。。",
		"これは契約と言えるでしょう。", "これは有効と言えるでしょう。", "契約に当たると言えるでしょう。", "契約にあたると言えるでしょう。", "区分へ当たると言えるでしょう。", "区分に該当すると言えるでしょう。",
		"読めると言えるでしょう。", "使えるといえるでしょう。", "漢字と言えるでしょう。", "カタカナと言えるでしょう。", "ABCと言えるでしょう。", "123と言えるでしょう。", "ＡＢと言えるでしょう。", "ｱｲと言えるでしょう。",
		"ＡＢＣＤＥと言えるでしょう。", "あいうえおと言えるでしょう。", "あいうえと言えるでしょう。", "ひらがなと言えるでしょう。", "ーと言えるでしょう。", "々と言えるでしょう。",
		// 形容動詞の語幹 (出す) と、語幹を含むだけで終わりが違うもの (出さない)
		"有効的と言えるでしょう。", "有効性と言えるでしょう。", "これは安全だと言えるでしょう。",
		// 手掛かり (読点の形) と文の長さの境目
		"暑い、暑い。", "疲れた、眠い。", "迷った、決めた。", "ある、です。", "はい、了解。", "また、今度。", "美しい、夜。", "美しい、夜を。", "美しい、夜をを。", "美しい、1234567。", "美しい、12345678。",
		"美しい、123456。", "美しい、1234567を。", "美しい、1234567を", "美しい、夜", "美の、夜。", "あ、夜。", "あな、夜。", "条件を残した、やや長い出力。", "雨が降った、静かな夜。", "これで良い、答え。",
		"ここでの、答え。", "春の、花。", "春は、花。", "春ははる、花。", "春はな、花。", "大きな、夢の、光。", "大きな、" + strings.Repeat("あ", 20) + "。", "大きな、夢" + strings.Repeat("あ", 14) + "。",
		"大きな、" + strings.Repeat("あ", 6) + "。", "大きな、" + strings.Repeat("あ", 7) + "。", "大きな、ひらがな。", "大きな、ひらがなか。", "大きな、ABC。", "大きな、ＡＢＣ。", "大きな、ー。",
		"走る、跳ぶ、泳ぐ。", "走る、跳ぶ、泳ぐ", "走る、跳ぶ。", "走る、跳ぶ、泳ぐか。", "走るか、跳ぶか、泳ぐか。", "走る、跳ぶ、泳ぎます。", "走ります、跳ぶ、泳ぐ。", "走る、跳ぶ、泳ぎません。", "走る、跳ぶ、泳ぐです。",
		"一つ、二つ、三つ。", "いくつ、ひとつ、ふたつ。", "楽しく、走る、泳ぐ。", "走る、楽しく、泳ぐ。", "走る、跳ぶ、楽しく。", "本を、走る、泳ぐ。", "彼は、走る、泳ぐ。", "彼はは、走る、泳ぐ。", "走る、彼が、泳ぐ。", "走る、彼に、泳ぐ。", "彼にと、走る、泳ぐ。",
		"走る、、泳ぐ。", "走る、 跳ぶ、 泳ぐ。", "漢字を、走る、泳ぐ。", "歩く、走る、飛ぶ、泳ぐ、潜る、読む。", strings.Repeat("あう、", 9) + "あう。", strings.Repeat("あう、", 10) + "あう。", "走る、跳ぶ、泳ぐ。。",
		"走る、跳ぶ、泳ぐ。", "言う、書く、継ぐ、話す、待つ、死ぬ、遊ぶ、読む、走る。", "言い、書き、走る。", "あ、い、う。", "る、る、る。", "カタカナを、走る、泳ぐ。", "ランニング、ジョギング、ウォーク。", "ブートする、動く、泳ぐ。",
	}
	for _, m := range []string{"はい", "いいえ", "いい", "また", "まあ", "あの", "その", "この", "どの", "あのね", "ええ", "いや", "おい", "なお", "ただ", "しかし", "ちなみ", "つまり", "さて", "では", "はいた", "また終わった"} {
		out = append(out, m+"、出力。", m+"、了解。", "新しい"+m+"、出力。")
	}
	for _, n := range []string{"思い", "想い", "願い", "違い", "匂い", "出会い", "味わい", "祝い", "誓い", "迷い", "笑い", "住まい", "佇まい", "問い", "扱い", "装い", "幸い", "暑い", "眠い", "嬉い", "しい", "たい", "ない", "ません"} {
		out = append(out, "美しい、"+n+"。", "美しい、"+n+"を。")
	}
	for _, c := range []string{"る", "た", "す", "だ", "て", "で", "ね", "よ", "わ", "か", "ぞ", "ぜ", "さ", "う", "く", "ぐ", "つ", "ぬ", "ぶ", "む", "ず", "じ", "は", "が", "に", "の", "ま", "ん", "い"} {
		out = append(out, "美しい、夜"+c+"。", "美しい、"+c+"。", "美しい、"+c+"夜。")
	}
	for _, c := range []string{"な", "い", "た", "の", "る", "ん"} {
		out = append(out, "あい"+c+"、夜。", c+"、夜。", "あい"+c+"あい"+c+"、夜。")
	}
	for _, c := range []string{"を", "は", "が", "で", "に", "と", "も", "は犬", "がん", "でん", "はは"} {
		out = append(out, "春"+c+"な、夜。", "春"+c+"い、夜。", "春"+c+"あた、夜。")
	}
	return out
}

func lintShapeStringCases() []VisCase {
	var calls []Call
	for _, s := range lintShapeSentences() {
		calls = append(calls, lintShapeSentenceCalls(s)...)
	}
	rec := recorder{true}
	var quotes []Call
	for _, s := range lintTopQuoteStrings {
		quotes = append(quotes, rec.call("_top_quote_contents", s))
	}
	return []VisCase{{Name: "shape/sentences", Calls: calls}, {Name: "shape/top-quotes", Calls: quotes}}
}

// lintTopQuoteStrings は _top_quote_contents に通す手書きの文 (入れ子・閉じない括弧・種類の違う閉じ・空の中身)。
var lintTopQuoteStrings = []string{
	"", "あ", "「a」", "「a」「b」", "「a「b」c」", "『a』", "『a「b」c』d「e』f」", "「a』", "『a」", "「「a」", "「a」」", "」「", "「", "」", "「」", "『』「」", "「a『b』c」d『e』",
	"x「a」y『b』z「c", "「a\nb」", "「" + strings.Repeat("あ", 300) + "」",
}

// lintG19FormsDoc は G19 の台詞の見つけ方と地の文の数え方の枝を 1 行ずつ当てる文書。
const lintG19FormsDoc = "「こんにちは。」と言った。\n『二重』と「一重」と言う。\n「外「内」外。」\n「閉じない\n名前「台詞。」\nハル：屁理屈（笑う）\n- ハル：リストの台詞。\n1. ナレ：番号の台詞｡\n" +
	"SE：ドアの音\nse：小文字\n場所：駅前\nＢＧＭ：静かな曲\n10:00：時間\n3コマ目：コマ\n（ハル：括弧）\nハル：（笑う）台詞（泣く）\nハル：\nハル：\u3000\n# ハル：見出し\n" +
	"ナレーション：地の文です。\nはるはるはるはるはるはるはるはる：長い名前。\n*ハル*：太字の名前。\n\n> ハル：引用\n\n| ハル：表 |\n| - |\n\n" +
	"私は走る。\n公私の区別。\n私立の学校。\n私鉄に乗る。\nわたしは見る。\nわたしてする。\nわたした。\nわたしたちは。\nわたします。\nぼくは行く。\nぼくじ。\nおれは言う。\nおれいを言う。\nおれる。\n" +
	"僕と俺と自分と筆者。\n手わたしする。\nあおれは。\n1,000円と1.5倍と１２３と1,2,3。\nv2.0.1 と a_b と x/y/1 と file_2.go と camelCase2。\n「私は1000」と書く。\n**太字の私**と__下線の僕__。\n" +
	"- 箇条書き1\n* 箇条書き2\n1. 番号付き\n[![badge](x)](y)\n  [![badge](x)](y)\n![img](a.png)\n`code 私 1`と書く。\n\n```\n私 1\n```\n\n<div>私 1</div>\n"

// lintShapeDocCases は文書ごとに _dialogue_lines と _narrative_counts を通す (入力の集まりの全ファイルと、枝を当てる文書。結果は digest だけを記録する)。
func lintShapeDocCases(c Corpus) ([]VisCase, error) {
	docCase := func(name, text string, keep bool) VisCase {
		rec := recorder{keep}
		return VisCase{Name: name, Calls: []Call{lanalyze(text, "a"), rec.call("_dialogue_lines", Ref("a")), rec.call("_narrative_counts", Ref("a"))}}
	}
	cases := []VisCase{docCase("shape/forms", lintG19FormsDoc, true)}
	for _, f := range c.Files {
		raw, err := os.ReadFile(filepath.Join(c.Root, filepath.FromSlash(f.Path)))
		if err != nil {
			return nil, fmt.Errorf("入力 %s を読めません: %w", f.Path, err)
		}
		cases = append(cases, docCase("shape/corpus/"+f.Path, string(raw), false))
	}
	return cases, nil
}
