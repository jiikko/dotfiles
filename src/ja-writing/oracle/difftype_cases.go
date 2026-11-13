package oracle

import (
	"math/rand/v2"
	"strconv"
	"strings"
)

// 群 F (文の種類の印) の記録: 印の関数 type_marks と、段 _type_changes (元の文と書き直した文を正規化し、文字単位の対応を取ってから渡す)。
// 段の呼び出しは normalize → normalize → _ExactSequenceMatcher の get_opcodes を覚えてから _type_changes に渡す (本家の _rule_candidates と同じ順)。

// diffTypeConstants は本家の定数のうち、群 F の Go の表と比べるもの (Get で値そのものを記録する)。
var diffTypeConstants = []string{
	"_END", "_TYPE_PATTERNS", "TYPE_MARKERS", "_TYPE_LITERALS", "_SIMILE_HEADS", "_SIMILE_LIKE", "_STOPS", "_STOP", "_NOT_BODY", "_SENTENCE_PIECE",
}

func diffTypeConstantCalls() []Call {
	calls := make([]Call, len(diffTypeConstants))
	for i, name := range diffTypeConstants {
		calls[i] = Call{Module: DiffModule, Function: name, Get: true, Digest: true, Keep: true}
	}
	return calls
}

// diffTypeStageCalls は書き直す前と後の文書 1 組に、群 F の段を通す呼び出し (結果が大きい組は結果そのものを記録に残さない)。
func diffTypeStageCalls(old, new string) []Call {
	o := dcall("normalize", old)
	o.Save, o.Quiet = "o", true
	r := dcall("normalize", new)
	r.Save, r.Quiet = "r", true
	ops := dcall("_ExactSequenceMatcher", nil, diffRef("o"), diffRef("r"))
	ops.Kwargs = map[string]any{"autojunk": false}
	ops.Method, ops.Save, ops.Quiet = "get_opcodes", "ops", true
	changes := dcall("_type_changes", diffRef("o"), diffRef("r"), diffRef("ops"))
	changes.Keep = len(old)+len(new) < 700
	return []Call{o, r, ops, changes}
}

// diffTypeBank は文の種類の印の正規表現の選択肢ごとに、当たる文と当たらない文 (除外の後読みが効く文) を 1 つずつ並べたもの。
var diffTypeBank = []string{
	// 伝聞
	"雨が降るそうです。", "雨が降るそうだ。", "新機能が出たそうだ。", "彼は行ったそうです。", "本を読んだそうだ。", "今日は来ないそうです。", "雨が降りそうです。", "雨だそうです。",
	"雨が降るそうなので、傘を持つ。", "雨が降るそうで、助かる。", "雨が降るそうでした。", "雨が降るそうだった。", "雨が降るそうでね。",
	"安全とされる。", "安全とされています。", "安全とされており、問題ない。", "安全とされてきた。", "安全とされました。", "安全とされた。", "必要とされる。", "手を落とされた。",
	"そう言われている。", "運用は不要という。", "不要というが、確認する。", "という名前です。", "そうといいます。", "要点といいます。", "あとのことです。", "後とのことです。", "休みとのことです。",
	"記事によると、値上げだ。", "記事によれば、値上げだ。", "場合によると、変わる。", "場合によれば、変わる。", "遅れると言われている。", "遅れると言われた。", "遅れるといわれています。", "遅れたと聞いた。", "遅れたと聞いている。", "遅れたと聞きました。", "かと聞いた。",
	// 推量
	"雨が降るらしい。", "雨が降るらしかった。", "雨が降るらしく、傘を持つ。", "春らしい陽気です。", "犯人は学生らしい。", "学生らしいが、確かではない。", "彼らしい。", "人間らしいけど、確かではない。", "人間らしい。", "素晴らしい。",
	"わざとらしい。", "可愛らしかった。", "男らしかった。", "雨のようです。", "雨のようだ。", "雨のようである。", "雨のようであった。", "雨のようで、助かる。", "雨のようなので、傘を持つ。", "雨のようなのです。",
	"次のようです。", "このようだ。", "雨みたいだ。", "雨みたいです。", "雨みたいなので、傘を持つ。", "雨かもしれない。", "雨かも知れません。", "雨かもしれず、傘を持つ。", "雨の可能性がある。", "雨の可能性があった。", "雨の可能性がありました。",
	"雨だろう。", "原因でだろう。", "雨だろうか。", "雨であろう。", "雨でしょう。", "雨でしょうか。", "雨ではないでしょうか。", "雨ではないだろうか。", "雨のはずだ。", "雨のはずです。", "雨のはずで、傘を持つ。", "雨のはずでした。",
	"雨と見られる。", "雨とみられます。", "雨と思われる。", "雨と考えられる。", "雨と考えられます。",
	// 意見
	"良いと思う。", "良いと思います。", "良いと思いました。", "良いと思い、進める。", "良いと思った。", "良いと思っている。", "良いと思っています。", "良いと思っていた。", "良いとおもう。",
	"良いと私は思う。", "良いと筆者は考える。", "良いと考えると、進められる。", "良いと考えます。", "良いと考えました。", "良いと考えた。", "良いと考えている。", "良いと感じる。", "良いと感じて、進める。",
	"1日100件と考え、見積もる。", "3人と感じ、見積もる。", "良いと考え、進める。", "良いように思う。", "良いように感じます。", "良いように感じられる。", "良いように思いました。",
	// 例え
	"まるで夢のようだ。", "あたかも夢のようです。", "さながら夢のように見える。", "まるで子供だ。", "前述のごとく進める。", "彼のごとく進める。", "彼のごとき人だ。", "彼の如く進める。", "彼の如き人だ。", "次の如く進める。",
	"まるで鳥のように見え、鳥のようだ。", "まるであたかも鳥のようだ。", "まるで。鳥のようだ。", "まるで鳥みたいだ。", "まるで鳥のように思う。", "あたかも鳥のように、まるで鳥のようだ。",
	// 依頼・決まり
	"確認してください。", "確認して下さい。", "資料をください。", "資料を下さい。", "ご確認ください。", "お読み下さい。", "ご案内いただきます。", "お願いします。", "よろしくお願いします。", "宜しくお願いします。",
	"お願いいたします。", "お願い致します。", "お願い申し上げます。", "お願いできますか。", "お願いできますでしょうか。", "見ていただけますか。", "見てもらえませんか。", "見てほしい。", "見てもらいたい。", "見ていただきたい。",
	"確認すべきだ。", "しかるべき手順を取る。", "驚くべき結果だ。", "べき乗を求める。", "読まなければならない。", "読まなければならず、困る。", "読まなければいけない。", "読まなければいけません。", "読まなくてはならない。", "読まなくてはいけません。",
	"読まないといけない。", "読まないといけません。", "読んではいけない。", "読んではならない。", "読んではいけません。", "読まねばならない。", "読まねばならず、困る。",
	"確認が必要である。", "確認の必要がある。", "確認の必要があります。", "確認の必要があり、進める。", "確認が必要です。", "確認が必要だ。", "不必要です。", "確認が必要になる。", "確認が必要となります。", "確認が必要になり、進める。", "確認が必要になった。", "確認が必要になったら、進める。", "確認が必要になったらしい。", "確認が必要になりました。", "確認が必要になるまで待つ。",
	// どれにも当たらない文
	"確認します。", "設定を開きます。", "結果は3件でした。", "",
}

// diffTypeSimilePairs は、例えの対にする語の組み合わせ (まるで〜よう・みたい)。bank にも入れたものの、頭の語と止める記号の位置を変えた形。
var diffTypeSimilePairs = []string{
	"まるで鳥のようだ", "まるで鳥のようです。", "まるで鳥みたいだ。", "まるで鳥のように。", "まるで鳥のようなので、", "まるで鳥のような。", "まるで\n鳥のようだ。", "まるで鳥だ。そう、みたいだ。", "まるで鳥だ。鳥のようだ。",
	"まるで、あたかも、さながら鳥のようだ。", "まるで鳥のようだ、あたかも鳥みたいだ。", "さながら鳥のようであった。", "あたかも鳥のようである。", "あたかも鳥のようだった。", "あたかも鳥のようでした。",
	"まるで鳥のようで、鳥のようで、", "まるで鳥のようで。", "まるでようだ。", "まるでみたい。", "まるで鳥のように思う。", "まるで鳥のように思う。まるで鳥のようだ。",
}

// diffTypeCases は群 F の段の手書きの組と、固定の種の組。範囲づくりの枝 (丸ごと移す・消えた文を字面で組にする・端を広げる・境目へずらす・足した語句を含める・
// 残った文とみなす下限・6 割の下限) を狙う。
func diffTypeCases() []diffFullCase {
	doc := diffDoc
	var cases []diffFullCase
	for i, s := range diffTypeBank {
		if s == "" {
			continue
		}
		cases = append(cases, diffFullCase{name: "bank-lost-" + strconv.Itoa(i), old: doc("設定を開きます。", s, "結果は3件でした。"), new: doc("設定を開きます。", "確認します。", "結果は3件でした。")},
			diffFullCase{name: "bank-added-" + strconv.Itoa(i), old: doc("設定を開きます。", "確認します。", "結果は3件でした。"), new: doc("設定を開きます。", s, "結果は3件でした。")})
	}
	for i, s := range diffTypeSimilePairs {
		cases = append(cases, diffFullCase{name: "simile-" + strconv.Itoa(i), old: doc(s), new: doc("確認します。")}, diffFullCase{name: "simile-added-" + strconv.Itoa(i), old: doc("確認します。"), new: doc(s)})
	}
	cases = append(cases, diffTypeRangeCases(doc)...)
	cases = append(cases, diffTypeFuzz()...)
	return append(cases, diffTypeFuzzSimilar()...)
}

// diffTypeRangeCases は範囲づくりの枝を狙う手書きの組。
func diffTypeRangeCases(doc func(...string) string) []diffFullCase {
	return []diffFullCase{
		{name: "same-text", old: doc("確認してください。"), new: doc("確認してください。")},
		{name: "same-after-normalize", old: doc("**確認**してください。"), new: doc("確認してください。")},
		{name: "empty-old", old: "", new: doc("確認してください。")},
		{name: "empty-new", old: doc("確認してください。"), new: ""},
		{name: "kept-mark", old: doc("確認してください。"), new: doc("読んでください。")},
		{name: "mark-to-other-kind", old: doc("確認してください。"), new: doc("確認すべきです。")},
		{name: "mark-swapped-kinds", old: doc("確認してください。雨だろう。"), new: doc("確認すべきだ。雨のようだ。")},
		// 句点 1 文字ぶん前にずれた足した文 (文の境目へずらす)
		{name: "slide-insert-forward", old: doc("設定を開きます。結果を見ます。"), new: doc("設定を開きます。確認してください。結果を見ます。")},
		{name: "slide-insert-backward", old: doc("設定を開く。結果を見る。"), new: doc("設定を開く。確認すべきだ。結果を見る。")},
		{name: "slide-delete", old: doc("設定を開く。確認してください。結果を見る。"), new: doc("設定を開く。結果を見る。")},
		{name: "slide-delete-tail", old: doc("設定を開く。結果を見る。確認してください。"), new: doc("設定を開く。結果を見る。")},
		{name: "slide-repeat-start", old: doc("設定を開く。設定を確認してください。設定を見る。"), new: doc("設定を開く。設定を見る。")},
		{name: "slide-both-ways", old: doc("ああ。ああ確認してください。ああ。"), new: doc("ああ。ああ。")},
		{name: "slide-tie", old: doc("あい。あい。あい。"), new: doc("あい。あい。あい。あい。")},
		// 句点も改行も含まない挿入 (文頭・文の末尾に足した語句は、その文に含める)
		{name: "bare-insert-head", old: doc("設定を開きます。"), new: doc("必ず設定を開きます。")},
		{name: "bare-insert-head-mark", old: doc("設定を開きます。"), new: doc("まるで設定を開くようです。")},
		{name: "bare-insert-tail", old: doc("設定を開きます"), new: doc("設定を開きますべきです")},
		{name: "bare-insert-end-of-sentence", old: doc("設定を開きます。結果を見ます。"), new: doc("設定を開きますので、必要があります。結果を見ます。")},
		{name: "insert-with-stop", old: doc("設定を開く。結果を見る。"), new: doc("設定を開く。\n確認してください。\n結果を見る。")},
		{name: "insert-newline", old: doc("設定を開く", "結果を見る"), new: doc("設定を開く", "確認してください", "結果を見る")},
		// 丸ごと移した文
		{name: "moved-sentence", old: doc("確認してください。設定を開く。結果を見る。"), new: doc("設定を開く。結果を見る。確認してください。")},
		{name: "moved-line", old: doc("確認してください。", "設定を開く。", "結果を見る。"), new: doc("設定を開く。", "結果を見る。", "確認してください。")},
		{name: "moved-with-mark-change", old: doc("確認してください。", "設定を開く。", "結果を見る。"), new: doc("設定を開く。", "結果を見る。", "確認すべきです。")},
		{name: "moved-twice-same", old: doc("確認してください。", "設定を開く。", "確認してください。", "結果を見る。"), new: doc("設定を開く。", "確認してください。", "結果を見る。", "確認してください。")},
		{name: "moved-into-sentence", old: doc("確認してください。", "設定を開きます。"), new: doc("設定を開きます。確認してください。")},
		{name: "moved-over-group", old: doc("結果を見る。設定を開く。確認してください。画面を閉じる。"), new: doc("画面を閉じる。確認してください。結果を見る。設定を開く。")},
		{name: "moved-short-heading", old: doc("# 確認", "確認してください。"), new: doc("確認してください。", "# 確認")},
		// 消えた文を、字面の近い書き直した文と組にする
		{name: "gone-paired", old: doc("設定画面を開いてから、確認してください。", "結果を見る。"), new: doc("設定画面を開いて確認すること。", "結果を見る。")},
		{name: "gone-paired-mark-kept", old: doc("設定画面を開いてから、読んでください。", "結果を見る。"), new: doc("設定画面を開いて、読んでください。", "結果を見る。")},
		{name: "gone-not-paired", old: doc("確認してください。", "結果を見る。"), new: doc("全く別の話です。", "結果を見る。")},
		{name: "gone-two-lines", old: doc("設定画面を開いてから", "確認してください。", "結果を見る。"), new: doc("設定画面を開いて確認すること。", "結果を見る。")},
		{name: "gone-two-lines-reverse", old: doc("設定画面を開いて確認すること。", "結果を見る。"), new: doc("設定画面を開いてから", "確認してください。", "結果を見る。")},
		{name: "gone-fold-after", old: doc("設定画面を開いてから", "確認してください", "結果を見る。"), new: doc("設定画面を開いてから確認してください、結果を見る。")},
		{name: "gone-fold-before", old: doc("確認してください", "設定画面を開いてから", "結果を見る。"), new: doc("設定画面を開いてから確認してください。", "結果を見る。")},
		{name: "gone-owned", old: doc("設定を開く。確認してください。結果を見る。", "画面を閉じる。"), new: doc("設定を開く。結果を見る。", "画面を閉じる。")},
		{name: "gone-drop-owner", old: doc("設定画面を開きます。確認してください。", "別の話です。"), new: doc("設定画面を開く。", "別の話です。")},
		{name: "gone-owner-shrink-end", old: doc("結果を見る。設定画面を開いてから確認する。", "終わり。"), new: doc("結果を見る設定画面を開いて確認すること。", "終わり。")},
		{name: "gone-owner-shrink-start", old: doc("設定画面を開いてから確認する。結果を見る。", "終わり。"), new: doc("設定画面を開いて確認すること。結果を見る。", "終わり。")},
		{name: "gone-three-pairs", old: doc("あ設定画面を開いてから確認する。", "い資料を読んでから返信する。", "う結果を見てから報告する。"), new: doc("設定画面を開いて確認すること。", "資料を読んで返信すること。", "結果を見て報告すること。")},
		{name: "gone-same-text-twice", old: doc("設定画面を開いてから確認する。", "資料。", "設定画面を開いてから確認する。"), new: doc("設定画面を開いて確認すること。", "資料。")},
		{name: "gone-dice-boundary", old: doc("設定画面を開いてから確認する手順です。"), new: doc("設定画面を開いて確認すること。確認する手順です。")},
		// 残った文とみなす下限・6 割の下限
		{name: "aligned-low", old: doc("確認してください。結果を見る。"), new: doc("確認。結果を見る。")},
		{name: "aligned-short-keep", old: doc("お手数ですが確認してください。"), new: doc("確認を。")},
		{name: "aligned-owns", old: doc("確認してください、そして設定を確認する。"), new: doc("設定を確認。")},
		{name: "aligned-replace-ratio", old: doc("確認してください。結果をよく見て設定を開く。"), new: doc("確認。結果を見て設定を開く。")},
		// 範囲を文の端まで広げる
		{name: "widen-left", old: doc("設定を開く。結果を見る、確認してください。"), new: doc("設定を開く。結果を見る、確認してはいけません。")},
		{name: "widen-claimed", old: doc("設定を開く。結果を見る。確認してください。"), new: doc("設定を開く。結果を見る確認すべきです。")},
		{name: "widen-adjacent-groups", old: doc("あ。確認してください。い。確認すべきだ。う。"), new: doc("あ。確認する。い。確認する。う。")},
		// 表・コード・空行・見出し
		{name: "table-skipped", old: doc("| 手順 | 確認してください。 |", "|---|---|"), new: doc("| 手順 | 確認する。 |", "|---|---|")},
		{name: "code-skipped", old: doc("```", "確認してください。", "```", "結果を見る。"), new: doc("```", "確認する。", "```", "結果を見る。")},
		{name: "code-open", old: doc("```", "確認してください。"), new: doc("```", "確認する。")},
		{name: "blank-lines", old: doc("設定を開く。", "", "確認してください。", "", "結果を見る。"), new: doc("設定を開く。", "", "確認する。", "", "結果を見る。")},
		{name: "list-items", old: doc("- 確認してください。", "- 設定を開く。"), new: doc("- 確認する。", "- 設定を開く。")},
		{name: "heading-line", old: doc("# 確認してください", "本文です。"), new: doc("# 確認する", "本文です。")},
		{name: "quote-marks", old: doc("「確認してください。」と言う。"), new: doc("「確認する。」と言う。")},
		{name: "ideographic-space", old: doc("設定を開く。　確認してください。"), new: doc("設定を開く。　確認する。")},
		{name: "whitespace-only-line", old: doc("設定を開く。", "   ", "確認してください。"), new: doc("設定を開く。", "   ", "確認する。")},
		{name: "crlf", old: "設定を開く。\r\n確認してください。\r\n", new: "設定を開く。\r\n確認する。\r\n"},
		{name: "no-final-stop", old: "確認してください", new: "確認する"},
		{name: "fullwidth-stops", old: doc("確認してください！設定を開く？"), new: doc("確認する！設定を開く？")},
		{name: "ascii-stops", old: doc("確認してください! 設定を開く?"), new: doc("確認する! 設定を開く?")},
		{name: "merge-sentences", old: doc("設定を開く。", "確認してください。"), new: doc("設定を開く、確認してください。")},
		{name: "split-sentence", old: doc("設定を開いて、確認してください。"), new: doc("設定を開く。", "確認してください。")},
		{name: "owns-punct-only", old: doc("確認してください。"), new: doc("、、")},
		{name: "owns-punct-only-tail", old: doc("設定を開く。確認してください。"), new: doc("設定を開く。、、")},
		{name: "split-lose-mark", old: doc("設定を開いて、確認してください。"), new: doc("設定を開く。", "確認する。")},
	}
}

// diffTypeFuzz は固定の種 (PCG 1076/7) から作った、文の並べ替え・足す・消す・書き換えを混ぜた組 (種を変えない限り同じ組になる)。
func diffTypeFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 7))
	neutral := []string{"設定を開きます。", "結果は3件でした。", "画面を閉じる。", "確認します。", "資料を読んでから返信する。", "手順は次のとおりです。", "あ。", "いい。"}
	pick := func() string {
		if r.IntN(3) == 0 {
			return neutral[r.IntN(len(neutral))]
		}
		for {
			if s := diffTypeBank[r.IntN(len(diffTypeBank))]; s != "" {
				return s
			}
		}
	}
	type doc [][]string // 行 (文の並び)
	var cases []diffFullCase
	for n := range 400 {
		var d doc
		for range 2 + r.IntN(6) {
			var line []string
			for range 1 + r.IntN(3) {
				line = append(line, pick())
			}
			d = append(d, line)
		}
		old := diffJoinGroups(d, "", "\n")
		cur := make(doc, len(d))
		for i, l := range d {
			cur[i] = append([]string{}, l...)
		}
		for ops := 1 + r.IntN(4); ops > 0 && len(cur) > 0; ops-- {
			i := r.IntN(len(cur))
			j := r.IntN(len(cur[i]))
			switch r.IntN(10) {
			case 0: // 消す
				cur[i] = append(cur[i][:j], cur[i][j+1:]...)
				if len(cur[i]) == 0 {
					cur = append(cur[:i], cur[i+1:]...)
				}
			case 1: // 別の文に置き換える
				cur[i][j] = pick()
			case 2: // 足す
				cur[i] = append(cur[i][:j], append([]string{pick()}, cur[i][j:]...)...)
			case 3: // 行を入れ替える
				k := r.IntN(len(cur))
				cur[i], cur[k] = cur[k], cur[i]
			case 4: // 次の行とつなぐ
				if i+1 < len(cur) {
					cur[i] = append(cur[i], cur[i+1]...)
					cur = append(cur[:i+1], cur[i+2:]...)
				}
			case 5: // 行を分ける
				if j+1 < len(cur[i]) {
					head, tail := append([]string{}, cur[i][:j+1]...), append([]string{}, cur[i][j+1:]...)
					cur = append(cur[:i], append(doc{head, tail}, cur[i+1:]...)...)
				}
			case 6: // 句点を落とす・足す
				s := cur[i][j]
				if strings.HasSuffix(s, "。") {
					cur[i][j] = strings.TrimSuffix(s, "。")
				} else {
					cur[i][j] = s + "。"
				}
			case 7: // 文を少し書き換える (印を落とす・言い換える)
				s := cur[i][j]
				for _, from := range []string{"ください", "べき", "そう", "らしい", "だろう", "と思う", "ようだ", "必要"} {
					if strings.Contains(s, from) {
						s = strings.Replace(s, from, "ね", 1)
						break
					}
				}
				cur[i][j] = s
			case 8: // 文の前に語を足す
				cur[i][j] = "まるで" + cur[i][j]
			default: // 同じ文を移す
				k := r.IntN(len(cur))
				s := cur[i][j]
				cur[k] = append(cur[k], s)
			}
		}
		cases = append(cases, diffFullCase{name: "fuzz-" + strconv.Itoa(n), old: old, new: diffJoinGroups(cur, "", "\n")})
	}
	return cases
}

// diffTypeMarkSentences は type_marks に通す文 (bank と例えの対の形)。
func diffTypeMarkSentences() []string {
	out := append([]string{}, diffTypeBank...)
	return append(out, diffTypeSimilePairs...)
}

// diffTypeLineCalls は文書の行ごとに type_marks を通す呼び出し (空の行は飛ばす)。
func diffTypeLineCalls(text string) []Call {
	lines, _ := physicalLinesOf(text)
	var calls []Call
	for _, line := range lines {
		if line != "" {
			calls = append(calls, dcall("type_marks", line))
		}
	}
	return calls
}

// diffTypeFuzzSimilar は固定の種 (PCG 1076/8) から作った、字面の近い文を言い換える組。語の並びを共有する文 (同じ語の組み合わせ) を作り、
// 文の一部の書き換え・文末の印の付け外し・読点でのつなぎ・足す・消す・移す・同じ書き出しの文の足し引きを混ぜる。
// 範囲を作る処理の、消えた文を字面の近い文と組にする所・文の境目へずらす所・範囲を広げる所に届くように、文が長く、書き出しを共有させる。
func diffTypeFuzzSimilar() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 8))
	heads := []string{"設定画面を開いて", "設定画面を確認して", "資料を読んで", "結果を見て", "手順を確かめて", "画面を閉じて"}
	middles := []string{"から", "ので", "ことで", "うえで", "後に", "前に"}
	tails := []string{"確認してください。", "確認する。", "確認すべきです。", "確認します。", "確認するようです。", "確認するはずです。", "確認が必要です。", "確認できます。", "確認すること。", "確認したそうです。", "確認と思います。", "確認を。", "返信する。", "報告してください。"}
	pick := func(xs []string) string { return randPick(r, xs) }
	mk := func() string {
		s := pick(heads)
		if r.IntN(2) == 0 {
			s += pick(middles) + pick(heads)
		}
		return s + pick(middles) + "、" + pick(tails)
	}
	var cases []diffFullCase
	for n := range 1500 {
		var lines [][]string
		for range 2 + r.IntN(5) {
			var line []string
			for range 1 + r.IntN(3) {
				line = append(line, mk())
			}
			lines = append(lines, line)
		}
		old := diffJoinGroups(lines, "", "\n")
		cur := make([][]string, len(lines))
		for i, l := range lines {
			cur[i] = append([]string{}, l...)
		}
		for ops := 1 + r.IntN(4); ops > 0 && len(cur) > 0; ops-- {
			i := r.IntN(len(cur))
			if len(cur[i]) == 0 {
				continue
			}
			j := r.IntN(len(cur[i]))
			switch r.IntN(9) {
			case 0: // 文末を替える
				s := cur[i][j]
				if k := strings.LastIndex(s, "、"); k >= 0 {
					cur[i][j] = s[:k+len("、")] + pick(tails)
				}
			case 1: // 前半だけ替える
				s := cur[i][j]
				if k := strings.Index(s, "、"); k >= 0 {
					cur[i][j] = pick(heads) + pick(middles) + s[k:]
				}
			case 2: // 読点で次の文とつなぐ
				if j+1 < len(cur[i]) {
					cur[i][j] = strings.TrimSuffix(cur[i][j], "。") + "、"
				}
			case 3: // 消す
				cur[i] = append(cur[i][:j], cur[i][j+1:]...)
			case 4: // 足す
				cur[i] = append(cur[i][:j], append([]string{mk()}, cur[i][j:]...)...)
			case 5: // 同じ文を別の行へ移す
				k := r.IntN(len(cur))
				cur[k] = append(cur[k], cur[i][j])
				cur[i] = append(cur[i][:j], cur[i][j+1:]...)
			case 6: // 句点を落として次の文へつなぐ (境目のずれ)
				cur[i][j] = strings.TrimSuffix(cur[i][j], "。")
			case 7: // 同じ書き出しの文を足す
				s := cur[i][j]
				if k := strings.Index(s, "、"); k >= 0 {
					cur[i] = append(cur[i][:j+1], append([]string{s[:k+len("、")] + pick(tails)}, cur[i][j+1:]...)...)
				}
			default: // 文に語を足す
				cur[i][j] = "必ず" + cur[i][j]
			}
		}
		cases = append(cases, diffFullCase{name: "similar-" + strconv.Itoa(n), old: old, new: diffJoinGroups(cur, "", "\n")})
	}
	return cases
}
