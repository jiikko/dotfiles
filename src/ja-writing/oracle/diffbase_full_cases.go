package oracle

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// diffFullCase は full_diff に通す手書きの組 (1 つが全文差分の枝を狙う)。notes は encoding_notes (nil は渡さない)。
type diffFullCase struct {
	name, old, new string
	notes          []any
}

// diffFullCases は全文差分 (群 J) の手書きの組。行の対応 (前後の同じ行・1 回ずつだけ出る行・小さい間と大きい間)・文の断片の組み方・文字の対応・
// 動かした行・見出しの段・空白だけの違い・行の種類・注記・文字数の上限の手前と向こうを狙う。
func diffFullCases() []diffFullCase {
	numbered := func(prefix string, n int) []string {
		lines := make([]string, n)
		for i := range lines {
			lines[i] = fmt.Sprintf("%s%d", prefix, i)
		}
		return lines
	}
	// 段落ごとに空行がある文書。段落は全部違い、空行だけが何度も出る (1 回ずつだけ出る行が無く、間が大きい)。
	paragraphs := func(prefix string, n int) string {
		return strings.Join(numbered(prefix, n), "\n\n") + "\n"
	}
	sentences := func(topic string, n int) string {
		var sb strings.Builder
		for i := range n {
			fmt.Fprintf(&sb, "%sの手順%dを確認します。結果は%d件でした。", topic, i, i*3)
		}
		return sb.String() + "\n"
	}
	reordered := numbered("行", 12)
	reordered[2], reordered[9] = reordered[9], reordered[2]
	long := strings.Repeat("あいうえお", 450)
	cases := []diffFullCase{
		{name: "same", old: "確認します。\n試します。\n", new: "確認します。\n試します。\n"},
		{name: "empty-both", old: "", new: ""},
		{name: "empty-old", old: "", new: "確認します。\n"},
		{name: "empty-new", old: "確認します。\n", new: ""},
		{name: "insert", old: "a\nc\n", new: "a\nb\nc\n"},
		{name: "delete", old: "a\nb\nc\n", new: "a\nc\n"},
		{name: "replace", old: "a\nb\nc\n", new: "a\nB\nc\n"},
		{name: "final-newline", old: "a\nb", new: "a\nb\n"},
		{name: "only-newline", old: "\n", new: ""},
		{name: "heading-level", old: "## 見出し\n本文\n", new: "### 見出し\n本文\n"},
		{name: "heading-level-indent", old: " # 見出し\n", new: " ## 見出し\n"},
		{name: "heading-level-rest-differs", old: "## 見出し\n", new: "### 別の見出し\n"},
		{name: "heading-level-indent-differs", old: " ## 見出し\n", new: "### 見出し\n"},
		{name: "heading-to-paragraph", old: "## 見出し\n", new: "見出し\n"},
		{name: "heading-setext", old: "見出し\n====\n", new: "見出し\n----\n"},
		{name: "whitespace-only", old: "a b\n", new: "a  b\n"},
		{name: "whitespace-ideographic", old: "確認 します。\n", new: "確認\u3000します。\n"},
		{name: "whitespace-bom", old: "確認します。\n", new: "\ufeff確認します。\n"},
		{name: "moved", old: "A行\n\nB行\n\nC行\n", new: "B行\n\nC行\n\nA行\n"},
		{name: "moved-twice", old: "A行\nB行\nC行\nD行\n", new: "C行\nD行\nA行\nB行\n"},
		{name: "moved-blank-not-paired", old: "A\n\nB\n", new: "B\n\nA\n\n\n"},
		{name: "anchors-reordered", old: strings.Join(numbered("行", 12), "\n") + "\n", new: strings.Join(reordered, "\n") + "\n"},
		{name: "anchors-deep", old: strings.Join(numbered("行", 200), "\n") + "\n", new: strings.Join(append([]string{"先頭"}, numbered("行", 200)...), "\n") + "\n"},
		{name: "gap-small", old: "x\nx\ny\nx\n", new: "x\ny\ny\nx\nx\n"},
		{name: "gap-large-blank-only", old: paragraphs("旧", 30), new: paragraphs("新", 30)},
		{name: "gap-large-shifted", old: paragraphs("旧", 30), new: paragraphs("旧", 12) + "\n" + paragraphs("追加", 14) + "\n" + strings.Join(numbered("旧", 30)[12:], "\n\n") + "\n"},
		{name: "gap-large-far", old: strings.Repeat("同じ\n\n", 20), new: strings.Repeat("別\n\n", 24) + "同じ\n\n"},
		{name: "gap-large-many-shared", old: strings.Repeat("同じ\n", 24), new: strings.Repeat("別\n", 10) + strings.Repeat("同じ\n", 24)},
		{name: "crlf", old: "確認します。\r\n試します。\r\n", new: "確認します。\n試します。\n"},
		{name: "cr-only", old: "確認します。\r試します。\r", new: "確認します。\n試します。\n"},
		{name: "kinds", old: "> 引用です。\n> - 引用の中の箇条書き\n- 箇条書き\n\n```\nコード\n```\n| 表 |\n|---|\n| 値 |\n<div>HTML</div>\n[ref]: https://example.com\n---\n", new: "> 引用でした。\n> - 引用の中の箇条書き2\n- 箇条書きです\n\n```\nコードです\n```\n| 表 |\n|---|\n| 値2 |\n<div>HTML2</div>\n[ref]: https://example.org\n---\n"},
		{name: "frontmatter", old: "---\ntitle: a\n---\n本文です。\n", new: "---\ntitle: b\n---\n本文でした。\n"},
		{name: "chars-basic", old: "設定を確認します。\n", new: "設定を確認しました。\n"},
		{name: "chars-astral", old: "😀確認します。👨\u200d👩\u200d👧\n", new: "😀確認しました。👨\u200d👧\n"},
		{name: "chars-combining", old: "が確認します。\n", new: "か\u3099確認しました。\n"},
		{name: "chars-all-different", old: "あいう\n", new: "かきく\n"},
		{name: "chars-multi-line", old: "一行目です。\n二行目です。\n三行目です。\n", new: "一行目でした。\n三行目です。\n四行目です。\n"},
		{name: "fragments-sentences", old: sentences("設定", 14), new: strings.Replace(sentences("設定", 14), "手順5を確認します。", "手順5を確認しました。", 1)},
		{name: "fragments-rewritten", old: sentences("設定", 14), new: sentences("構成", 14)},
		{name: "fragments-inserted", old: sentences("設定", 14), new: sentences("設定", 6) + "新しい文を足します。" + strings.TrimPrefix(sentences("設定", 14), sentences("設定", 6))},
		{name: "fragments-english", old: strings.Repeat("Check the setting. It is 3.14 now. ", 20) + "\n", new: strings.Repeat("Check the setting. It is 3.15 now. ", 20) + "\n"},
		{name: "fragments-fullwidth", old: strings.Repeat("設定を確認します．結果を見ます！", 25) + "\n", new: strings.Repeat("設定を確認しました．結果を見ます？", 25) + "\n"},
		{name: "fragments-brackets", old: strings.Repeat("「確認します。」と言った。（補足です。）", 25) + "\n", new: strings.Repeat("「確認しました。」と言った。（補足でした。）", 25) + "\n"},
		{name: "fragments-long-line", old: long + "。末尾です。\n", new: long + "。末尾でした。\n"},
		{name: "fragments-long-differs", old: long + "。\n", new: strings.Repeat("かきくけこ", 450) + "。\n"},
		{name: "fragments-long-pair-affix", old: "前の文です。" + long + "旧。後の文です。\n", new: "前の文でした。" + long + "新。後の文でした。\n"},
		{name: "notes-eol", old: "a\n", new: "b\n", notes: []any{map[string]any{"kind": "eol", "old": "lf", "new": "crlf"}}},
		{name: "notes-bom", old: "a\n", new: "a\n", notes: []any{map[string]any{"kind": "bom", "old": false, "new": true}}},
		{name: "notes-both", old: "a\n", new: "b\n", notes: []any{map[string]any{"kind": "eol", "old": "cr", "new": "mixed"}, map[string]any{"kind": "bom", "old": true, "new": false}}},
		{name: "notes-empty", old: "a\n", new: "b\n", notes: []any{}},
	}
	cases = append(cases, diffFullTargeted()...)
	cases = append(cases, diffFullFuzz()...)
	return cases
}

// kanjiRun は n 字の、字が重ならない文字列 (seed ごとに違う)。長い文を作るのに使う。
func kanjiRun(seed, n int) string {
	rs := make([]rune, n)
	for i := range rs {
		rs[i] = rune(0x4e00 + (seed*131+i*7)%4000)
	}
	return string(rs)
}

// diffFullTargeted は 1 つの規則だけを狙う組 (同じ似かたの 2 候補・2 つの hunk の文字数の合計が上限を超える・動かした行の相手が 2 つの hunk から取り合いになる・
// 片側だけが 2,000 字を超える断片の組・引用の中の箇条書きの変更)。
func diffFullTargeted() []diffFullCase {
	f := []rune(kanjiRun(1, 520))
	g1, g2 := append([]rune{}, f...), append([]rune{}, f...)
	g1[5], g2[300] = 'あ', 'あ'
	// 文字数の合計が上限 (200,000) を超える: 1 つの hunk は 190,000 で収まり、次の hunk は残りに入らない。
	// 断片は 1,900 字 1 つで、1 字だけ違う (組にして文字で対応させる費用は 3,800)。
	bulk := func(first, n int, changed bool) string {
		var sb strings.Builder
		for i := range n {
			r := []rune(kanjiRun(first+i, 1900))
			if changed {
				r[10] = 'あ'
			}
			sb.WriteString(string(r) + "。\n")
		}
		return sb.String()
	}
	budgetOld := bulk(100, 50, false) + "区切り\n" + bulk(200, 10, false)
	budgetNew := bulk(100, 50, true) + "区切り\n" + bulk(200, 10, true)
	// 片側だけが 2,000 字を超える断片: difflib の ratio では 0.49 で組になるが、前後の同じ字の数だけで見ると 0 で組にならない。
	shared := kanjiRun(7, 1000)
	longOld, longNew := kanjiRun(8, 1000)+shared+kanjiRun(9, 100)+"。\n", kanjiRun(10, 950)+shared+"。\n"
	cases := []diffFullCase{
		{name: "tie-two-candidates", old: string(f) + "。", new: string(g1) + "。" + string(g2) + "。"},
		{name: "budget-second-hunk", old: budgetOld, new: budgetNew},
		{name: "moved-contested", old: "A行\n区切り1\nB行\n区切り2\n終わり\n", new: "区切り1\n区切り2\n終わり\nA行\nB行\n"},
		{name: "long-one-side-pairs", old: longOld, new: longNew},
		{name: "quote-list-changed", old: "> - 項目です\n> 続きです\n", new: "> - 項目でした\n> 続きでした\n"},
	}
	return append(cases, diffFullBoundaries()...)
}

// diffFullBoundaries は全文差分の分かれ目 (定数の比べ・空白の判定・前後の同じ字の数え方) にちょうど当たる組。
// 分かれ目の 1 つ手前や 1 つ先の入力では同じ答えになる規則を、定数や比べの向きを変えたときに落とすためにある。
func diffFullBoundaries() []diffFullCase {
	// 1 行 1 語の行の並び (空白で区切って書く)
	words := func(s string) string { return strings.ReplaceAll(s, " ", "\n") + "\n" }
	// 句点を含めて 2,000 字ちょうどになる断片 (1,999 字 + 句点) と、1 字入れ替えたものと 1 字足したもの
	long := []rune(kanjiRun(5, 1999))
	replaced := string(long[:700]) + string(long[701:]) + "あ"
	added := string(long[:700]) + string(long[701:900]) + "い" + string(long[900:])
	sentence := func(i int) string { return kanjiRun(i, 99) + "。" }
	blocks := []string{sentence(1), sentence(2), sentence(3), sentence(4), sentence(5)}
	swapped := []string{blocks[0], blocks[2], blocks[1], blocks[3], blocks[4]}
	head := kanjiRun(1, 50)
	return []diffFullCase{
		// Python の str.strip だけが空白とみなす字 (U+001C) の行は、1 回ずつだけ出る行に数えない
		{name: "anchor-python-space-only", old: "確認します。\n\x1c", new: "\x1c\n確認します。"},
		// 見出しの段の注記は、両側とも 1 行の hunk のときだけ付く
		{name: "heading-level-two-lines", old: "# 見出し\n# 別の見出し\n", new: "## 見出し\n## 別の見出し\n"},
		// 前の同じ字と後ろの同じ字は重ならない (一方が他方の頭の部分のとき)
		{name: "affix-shrink", old: "あああ\n", new: "ああ\n"},
		{name: "affix-grow", old: "ああ\n", new: "あああ\n"},
		// 組にする似かたの下限 0.45 のすぐ上 (50 字が同じで両側 110 字なので 100/220 = 0.4545)
		{name: "pair-ratio-0.4545", old: kanjiRun(9, 600) + "。" + head + kanjiRun(2, 60) + "。\n", new: kanjiRun(9, 600) + "。" + head + kanjiRun(3, 60) + "。\n"},
		// 近さの窓 (8 つ先) にちょうど当たる行 (空行を挟んだ組)
		{name: "gap-window-eight", old: "\n\n\n\n\n\n\n\ny\nx\n\n\n\n\nz", new: "w\n\n\n\n\n\n\n\n\n\n\n\n\n\nx\n\n\ny\n\n\n\ny\n\n\n\n\nx"},
		// 間の大きさ (両側の要素の数の積) の上限 400 ちょうどと 320。400 以下なら difflib で対応させる
		{name: "gap-product-400", old: words("x x w y w y z x z z x z z x w x y w w z"), new: words("w x w x z x z y z y y y x x x y z z w q1")},
		{name: "gap-product-320", old: words("z w w z y w w z y z z x w z z x y z y w"), new: words("w w w w w z w x w y w y x z w x")},
		// 塊の上限 500 字ちょうど (2 つの文を入れ替える)
		{name: "block-500", old: strings.Join(blocks, "") + "\n", new: strings.Join(swapped, "") + "\n"},
		// 断片の上限 2,000 字ちょうど (前の文と後の文に挟まれた 1 文)
		{name: "fragment-2000-replaced", old: "前の文です。" + string(long) + "。後の文です。\n", new: "前の文でした。" + replaced + "。後の文でした。\n"},
		{name: "fragment-2000-added", old: "前の文です。" + string(long) + "。後の文です。\n", new: "前の文でした。" + added + "。後の文でした。\n"},
	}
}

// diffFullFuzz は固定の種から作った、行の並びの組と文の並びの組 (手書きでは当たりにくい、間の大きさ・近さの窓・似かたの順の分かれ目を通す)。
// 種を変えない限り同じ組になる (math/rand/v2 の PCG の並びは仕様で決まっている)。
func diffFullFuzz() []diffFullCase {
	r := rand.New(rand.NewPCG(1076, 4))
	pool := []string{"", "同じ行", "別の行", "確認します。", "- 項目", "## 見出し", "> 引用", "    code", "---", "| a | b |"}
	pick := func(unique *int) string {
		if r.IntN(10) < 4 {
			return pool[r.IntN(len(pool))]
		}
		*unique++
		return fmt.Sprintf("固有の行%d", *unique)
	}
	var cases []diffFullCase
	for n := range 140 {
		unique := 0
		size := 3 + r.IntN(50)
		old := make([]string, size)
		for i := range old {
			old[i] = pick(&unique)
		}
		cur := append([]string{}, old...)
		for ops := 1 + r.IntN(7); ops > 0 && len(cur) > 1; ops-- {
			i := r.IntN(len(cur))
			switch r.IntN(5) {
			case 0:
				cur = append(cur[:i], cur[i+1:]...)
			case 1:
				cur = append(cur[:i], append([]string{pick(&unique)}, cur[i:]...)...)
			case 2:
				cur[i] = pick(&unique)
			case 3:
				j := r.IntN(len(cur))
				cur[i], cur[j] = cur[j], cur[i]
			default:
				// 長さの違う塊を 1 つ書き直す
				for k := i; k < min(i+1+r.IntN(12), len(cur)); k++ {
					cur[k] = pick(&unique)
				}
			}
		}
		cases = append(cases, diffFullCase{name: fmt.Sprintf("fuzz-lines-%03d", n), old: strings.Join(old, "\n") + "\n", new: strings.Join(cur, "\n") + "\n"})
	}
	// 同じ行が何度も出る (1 回ずつだけ出る行が無く、間が大きい) 組。近さの窓 (8 つ先) の分かれ目を通す。
	for n := range 60 {
		line := func(alphabet []string, size int) string {
			lines := make([]string, size)
			for i := range lines {
				lines[i] = alphabet[r.IntN(len(alphabet))]
			}
			return strings.Join(lines, "\n") + "\n"
		}
		cases = append(cases, diffFullCase{name: fmt.Sprintf("fuzz-repeat-%03d", n), old: line([]string{"x", "y", "z", "w"}, 22+r.IntN(19)), new: line([]string{"x", "y", "z", "w", "q1", "q2", "q3"}, 22+r.IntN(19))})
	}
	sentence := func(k int) string {
		return fmt.Sprintf("手順%dの設定を確認します。%sの結果は%d件でした。", k, kanjiRun(k, 12+r.IntN(30)), r.IntN(100))
	}
	for n := range 40 {
		var oldDoc, newDoc []string
		for range 1 + r.IntN(3) {
			var oldS []string
			for range 8 + r.IntN(10) {
				oldS = append(oldS, sentence(r.IntN(1000)))
			}
			newS := append([]string{}, oldS...)
			for ops := 1 + r.IntN(6); ops > 0 && len(newS) > 1; ops-- {
				i := r.IntN(len(newS))
				switch r.IntN(4) {
				case 0:
					newS = append(newS[:i], newS[i+1:]...)
				case 1:
					newS = append(newS[:i], append([]string{sentence(r.IntN(1000))}, newS[i:]...)...)
				case 2:
					newS[i] = strings.Replace(newS[i], "確認します", "確認しました", 1)
				default:
					j := r.IntN(len(newS))
					newS[i], newS[j] = newS[j], newS[i]
				}
			}
			oldDoc = append(oldDoc, strings.Join(oldS, ""))
			newDoc = append(newDoc, strings.Join(newS, ""))
		}
		cases = append(cases, diffFullCase{name: fmt.Sprintf("fuzz-sentences-%03d", n), old: strings.Join(oldDoc, "\n\n") + "\n", new: strings.Join(newDoc, "\n\n") + "\n"})
	}
	return cases
}
