package oracle

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// 群 L (文の組と本文) の段 (_sentence_pairs・_all_bodies) に通す組。文の区切り・行頭の印・文に分けない行・伏せる範囲・hunk の並びのまとめ方 (段落をずらした書き換え)・
// 1 行対 1 行の置き換えの組み方を狙う。

// diffSentenceCases は文の組と本文の手書きの組と固定の種の組 (名前は sentence/ を付けて case にする)。
func diffSentenceCases() []diffFullCase {
	doc := diffDoc
	cases := []diffFullCase{
		{name: "one-to-one-low-ratio", old: doc("動くでしょうか。"), new: doc("動きます。")},
		{name: "one-to-one-number", old: doc("3日かかります。"), new: doc("3週間かかります。")},
		{name: "two-sentences-low", old: doc("まず設定を開きます。次に保存します。"), new: doc("全く違う内容です。別の文もあります。")},
		{name: "one-to-two", old: doc("設定を開きます。"), new: doc("設定を開きます。保存します。")},
		{name: "insert-only", old: doc("前の文です。"), new: doc("前の文です。", "足した文です。")},
		{name: "delete-only", old: doc("前の文です。", "消す文です。"), new: doc("前の文です。")},
		{name: "bullets", old: doc("- 設定を開きます。", "* 保存します。", "+ 閉じます。", "・終わります。"), new: doc("- 設定を開きました。", "* 保存しました。", "+ 閉じました。", "・終わりました。")},
		{name: "numbered", old: doc("1. 開きます。", "2) 保存します。", "(1) 閉じます。", "（２）終わります。", "3）戻ります。", "（４） 終わります。"), new: doc("1. 開きました。", "2) 保存しました。", "(1) 閉じました。", "（２）終わりました。", "3）戻りました。", "（４） 終わりました。")},
		{name: "quote-nested", old: doc("> 引用の文です。", "> > 二重の引用です。", ">引用です。"), new: doc("> 引用の文でした。", "> > 二重の引用でした。", ">引用でした。")},
		{name: "heading-sentence", old: doc("## 見出しの文です。", "本文です。"), new: doc("## 見出しの文でした。", "本文でした。")},
		{name: "code-fence", old: doc("説明です。", "```", "コードの中の文です。", "```", "終わりです。"), new: doc("説明でした。", "```", "コードの中の文でした。", "```", "終わりでした。")},
		{name: "frontmatter", old: doc("---", "title: 題です。", "---", "本文です。"), new: doc("---", "title: 題でした。", "---", "本文でした。")},
		{name: "html-block", old: doc("<div>", "中の文です。", "</div>", "本文です。"), new: doc("<div>", "中の文でした。", "</div>", "本文でした。")},
		{name: "table", old: doc("| a | b |", "|---|---|", "| 文です。 | 2 |"), new: doc("| a | b |", "|---|---|", "| 文でした。 | 3 |")},
		{name: "english-dots", old: doc("Hello world. Next sentence. v1.2.3 is out. Done."), new: doc("Hello world! Next sentences. v1.2.4 is out. Done.")},
		{name: "fullwidth-dot", old: doc("設定を開く．保存する．閉じる．"), new: doc("設定を開いた．保存した．閉じた．")},
		{name: "closing-brackets", old: doc("「開きます。」と言う。（保存します。）終わり。"), new: doc("「開きました。」と言う。（保存しました。）終わり。")},
		{name: "leading-spaces", old: doc("  　 設定を開きます。　 保存します。"), new: doc("  　 設定を開きました。　 保存しました。")},
		{name: "blank-only-lines", old: doc("   ", "　", "設定です。"), new: doc("設定でした。", "", "　")},
		{name: "run-shifted", old: doc("設定を開きます。", "", "保存します。", "", "閉じます。"), new: doc("設定を", "開きます。", "", "保存します。", "", "閉じます。")},
		{name: "run-shifted-merge", old: doc("設定を", "開きます。", "", "保存します。", "", "閉じます。"), new: doc("設定を開きます。", "", "保存します。", "", "閉じます。")},
		{name: "run-same-paragraphs", old: doc("設定を開きます。", "", "保存します。", "", "閉じます。"), new: doc("設定を開きました。", "", "保存しました。", "", "閉じました。")},
		{name: "run-lines-only", old: doc("設定を開きます。", "", "保存します。", "閉じます。", "", "戻ります。"), new: doc("設定を開きました。", "", "- 保存しました。", "- 閉じました。", "", "戻りました。")},
		{name: "run-not-blank-between", old: doc("設定です。", "", "間の行です。", "", "保存です。"), new: doc("設定でした。", "", "間の行です。", "", "保存でした。")},
		{name: "run-one-replace", old: doc("設定を開きます。", "", "保存します。"), new: doc("設定を", "開きます。", "", "保存します。", "", "追加します。")},
		{name: "mask-inline-code", old: doc("`rm -rf` を実行します。", "実行は `ls` です。"), new: doc("`rm -rf` を実行しました。", "実行は `ls` でした。")},
		{name: "mask-url", old: doc("https://example.com/a を開きます。", "<https://example.com> を見ます。"), new: doc("https://example.com/b を開きました。", "<https://example.com> を見ました。")},
		{name: "merged-first-hunk-insert", old: "\n。\n\n。", new: "*\n\n面\n\n）"},
		// URL のあとに続く本文は、URL の範囲に入れずに数える (本家の _analyze の bare_url の縮め)
		{name: "bare-url-trailing-text", old: "詳しくは https://example.com/guideにあり、保存期間は3日です。\n", new: "詳しくは https://example.com/guideにあり、保存期間は5日です。\n"},
		{name: "bare-url-trailing-period", old: "手順は https://example.com/guide。保存期間は3日です。\n", new: "手順は https://example.com/guide。保存期間は5日です。\n"},
		{name: "bare-url-trailing-paren", old: "（https://example.com/guide）保存期間は3日です。\n", new: "（https://example.com/guide）保存期間は5日です。\n"},
		{name: "bare-url-only-ascii", old: "https://example.com/a?b=3&c=4 を開きます。\n", new: "https://example.com/a?b=5&c=4 を開きます。\n"},
		{name: "bare-url-url-changed-text-same", old: "見る https://example.com/a から保存期間は3日です。\n", new: "見る https://example.com/b から保存期間は3日です。\n"},
		{name: "bare-url-all-url-chars", old: "https://e.example/azAZ09-._~:/?#[]@!$&'()*+,;=%zにあり保存期間は3日です。\n", new: "https://e.example/azAZ09-._~:/?#[]@!$&'()*+,;=%zにあり保存期間は5日です。\n"},
		{name: "bare-url-tilde-percent-nine", old: "https://e.example/~a%20b9ZAzにあり保存期間は3日です。\n", new: "https://e.example/~a%20b9ZAzにあり保存期間は5日です。\n"},
		{name: "mask-link-image", old: doc("[文](http://x.example/a) を読みます。", "![図](img/a.png) を見ます。", "[ref]: http://x.example/ref"), new: doc("[文](http://x.example/b) を読みました。", "![図](img/b.png) を見ました。", "[ref]: http://x.example/ref2")},
		{name: "mask-html-token", old: doc("<b>太い</b>文です。", "<br/>改行です。"), new: doc("<b>太い</b>文でした。", "<br/>改行でした。")},
		{name: "mask-nested", old: doc("![a `b`](c.png) を見ます。"), new: doc("![a `b`](c.png) を見ました。")},
		// 文の始まりが、画像の中のインラインのコードの始まりと同じ位置 (外側の範囲は、その文では伏せない)
		{name: "mask-nested-start", old: doc("![説明。`b`](c.png) です。"), new: doc("![説明。`b`](c.png) でした。")},
		{name: "run-shifted-blank-side", old: doc("設定を開きます。", "", "　"), new: doc("設定を開きました。", "", "追加の文です。")},
		{name: "run-shifted-blank-side-three", old: doc("設定を開きます。", "", "　", "", "閉じます。"), new: doc("設定を開きました。", "", "追加の文です。", "", "閉じました。")},
		{name: "mask-all-sentence", old: doc("`code`"), new: doc("`コード`")},
		{name: "mask-adjacent", old: doc("`a``b` と <b>c</b> です。"), new: doc("`a``b` と <b>c</b> でした。")},
		{name: "skip-kinds", old: doc("# 見出しです。", "題", "===", "[a]: http://a.example", "---", "| x |", "本文です。"), new: doc("# 見出しでした。", "題", "===", "[a]: http://b.example", "---", "| y |", "本文でした。")},
		{name: "empty", old: "", new: ""},
		{name: "no-final-newline", old: "設定です。", new: "設定でした。"},
	}
	return append(cases, diffSentenceFuzz()...)
}

// diffSentenceFuzz は固定の種から作った、段落の数が変わる書き換え・行頭の印・伏せる範囲を混ぜた組 (種を変えない限り同じ組になる)。
func diffSentenceFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 6))
	prefixes := []string{"", "", "", "- ", "* ", "1. ", "> ", "## ", "(1) "}
	pieces := []string{"設定を開きます。", "保存してください。", "`code` を実行します。", "https://example.com を開きます。", "結果は3件です。", "確認しました．", "Done. Next.", "「引用です。」", "（補足です。）", "![図](a.png) です。"}
	sentence := func() string {
		var sb strings.Builder
		for range 1 + r.IntN(3) {
			sb.WriteString(pieces[r.IntN(len(pieces))])
		}
		return sb.String()
	}
	var cases []diffFullCase
	for n := range 140 {
		var paragraphs [][]string
		for range 2 + r.IntN(5) {
			var lines []string
			for range 1 + r.IntN(3) {
				lines = append(lines, prefixes[r.IntN(len(prefixes))]+sentence())
			}
			paragraphs = append(paragraphs, lines)
		}
		join := func(ps [][]string) string {
			parts := make([]string, len(ps))
			for i, p := range ps {
				parts[i] = strings.Join(p, "\n")
			}
			return strings.Join(parts, "\n\n") + "\n"
		}
		cur := make([][]string, len(paragraphs))
		for i, p := range paragraphs {
			cur[i] = append([]string{}, p...)
		}
		for ops := 1 + r.IntN(4); ops > 0; ops-- {
			i := r.IntN(len(cur))
			switch r.IntN(6) {
			case 0: // 段落を分ける
				if len(cur[i]) > 1 {
					k := 1 + r.IntN(len(cur[i])-1)
					head, tail := cur[i][:k:k], cur[i][k:]
					cur = append(cur[:i], append([][]string{head, tail}, cur[i+1:]...)...)
				}
			case 1: // 次の段落と合わせる
				if i+1 < len(cur) {
					cur[i] = append(cur[i], cur[i+1]...)
					cur = append(cur[:i+1], cur[i+2:]...)
				}
			case 2: // 文を書き換える
				cur[i][r.IntN(len(cur[i]))] = prefixes[r.IntN(len(prefixes))] + sentence()
			case 3: // 文を足す
				cur[i] = append(cur[i], sentence())
			case 4: // 文を消す
				if len(cur[i]) > 1 {
					cur[i] = cur[i][:len(cur[i])-1]
				}
			default: // 1 行の語を変える
				line := r.IntN(len(cur[i]))
				cur[i][line] = strings.Replace(cur[i][line], "します", "しました", 1)
			}
		}
		cases = append(cases, diffFullCase{name: fmt.Sprintf("fuzz-%03d", n), old: join(paragraphs), new: join(cur)})
	}
	return cases
}
