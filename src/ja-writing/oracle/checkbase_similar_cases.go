package oracle

import "strings"

// 似かた (_compact・_similarity・_source_like) の case。しきい値 0.53 の前後は、字をすべて違う漢字にした文で 2 字組の数を決めて作る。

// distinctRun は U+4E00 から offset 字目以降の n 個の違う漢字。2 字組は全部違う (重なる数を字数で決められる)。
func distinctRun(offset, n int) string {
	rs := make([]rune, n)
	for i := range rs {
		rs[i] = rune(0x4E00 + offset + i)
	}
	return string(rs)
}

func checkSimilarCases() []VisCase {
	var compact []Call
	for _, s := range []any{
		"", " ", "abc", "a b\tc\nd", "a\u3000b", "ＡＢＣ\u3000１２３", "ｱｲｳ", "㌔グラム", "ﬁ", "①②③", "㈱", "ǆ", "x\u200by", "x\u00ady", "\ufeffx", "x\u200dy\u2060z", "x\U000e0001y", "日本語 の 文", "e\u0301", "é",
		"a\x00b", "a\x1cb", "a\x85b", "a\u2028b\u2029c", "a\u00a0b", "ab\u200b \u3000\n\t", "\u200b", "\u200b\u200c\u200d", "한국어", "ｶﾞｷﾞ", "ガ", "ガ", "ヴ", "ｳﾞ", cps("a", rune(0xD800), "b"), "（株）", "…", "‥", "㍻", "㎏",
		"x\u0600y", "x\u061cy", "x\u180ey", "x\u2066y", "x\ue000y", "x\U000f0000y", strings.Repeat("あ ", 40), "Ⅷ", "ⅷ", "ﬃ ﬃ",
	} {
		compact = append(compact, ckeep(ccall("_compact", s)))
	}
	compact = append(compact, ckeep(ccall("_compact", cps("a", rune(0xD800), " b"))), ckeep(ccall("_compact", cps(rune(0xDFFF)))))

	var similarity []Call
	pairs := [][2]string{
		{"", ""}, {"a", "a"}, {"ab", ""}, {"", "ab"}, {"ab", "ab"}, {"ab", "ba"}, {"abc", "abd"}, {"aaaa", "aa"}, {"aa", "aaaa"}, {"abab", "ab"}, {"ab ab", "abab"},
		{"ＡＢＣ", "ABC"}, {"今日は 晴れ です", "今日は晴れです"}, {"今日は晴れです。", "今日は雨です。"}, {"x\u200by\u200bz", "xyz"}, {"abcabc", "abc"}, {"abc", "abcabc"}, {"あいうえお", "いうえおあ"},
		{distinctRun(0, 10), distinctRun(0, 10)}, {distinctRun(0, 10), distinctRun(5, 10)}, {distinctRun(0, 10), distinctRun(100, 10)},
		{"㌔グラム", "キログラム"}, {strings.Repeat("あ", 50), strings.Repeat("あ", 5)}, {strings.Repeat("あい", 20), strings.Repeat("いあ", 20)},
	}
	for _, p := range pairs {
		similarity = append(similarity, ckeep(ccall("_similarity", p[0], p[1])))
	}

	// しきい値 0.53: 最終稿は 101 字 (2 字組 100)、資料は先頭の k+1 字 (2 字組 k。全部が重なる)。最小の側は 重なり/最終稿の 2 字組 = k/100。
	var sourceLike []Call
	final := distinctRun(0, 101)
	for _, k := range []int{1, 52, 53, 54, 99, 100} {
		sourceLike = append(sourceLike, ckeep(ccall("_source_like", []any{distinctRun(0, k+1)}, final, false)))
	}
	// 資料が長く、重なりが小さい側が資料の 2 字組 (重なり/資料の 2 字組)。200 字の資料の中に最終稿の先頭 54 字がある: 重なり 53 / 資料 199 / 最終稿 100
	long := distinctRun(0, 54) + distinctRun(300, 146)
	sourceLike = append(sourceLike, ckeep(ccall("_source_like", []any{long}, final, false)))
	// 2 つの資料: 若い番号が同点で勝つ・しきい値以上でいちばん似ているものが勝つ・どれも届かない
	sourceLike = append(sourceLike,
		ckeep(ccall("_source_like", []any{distinctRun(0, 60), distinctRun(0, 60)}, final, false)),
		ckeep(ccall("_source_like", []any{distinctRun(0, 60), distinctRun(0, 80)}, final, false)),
		ckeep(ccall("_source_like", []any{distinctRun(0, 80), distinctRun(0, 60)}, final, false)),
		ckeep(ccall("_source_like", []any{distinctRun(0, 30), distinctRun(500, 90)}, final, false)),
		ckeep(ccall("_source_like", []any{distinctRun(0, 54), distinctRun(0, 53)}, final, false)),
		ckeep(ccall("_source_like", []any{}, final, false)),
		ckeep(ccall("_source_like", []any{"", "a"}, "", false)),
		ckeep(ccall("_source_like", []any{"今日は 晴れ です。明日は 雨 でしょう。"}, "今日は晴れです。明日は雨でしょう。", false)),
		ckeep(ccall("_source_like", []any{"今日は 晴れ です。"}, "全く 別の 文章 を 書く。", false)),
	)
	// compacted=True は _compact を通さない: 空白や書式の文字が残ったままの 2 字組で数える
	spaced := "今日は 晴れ です"
	sourceLike = append(sourceLike,
		ckeep(ccall("_source_like", []any{spaced}, spaced, true)),
		ckeep(ccall("_source_like", []any{spaced}, "今日は晴れです", true)),
		ckeep(ccall("_source_like", []any{"今日は晴れです"}, "今日は晴れです", true)),
		ckeep(ccall("_source_like", []any{"ＡＢＣＤ"}, "ABCD", true)),
		ckeep(ccall("_source_like", []any{"ＡＢＣＤ"}, "ABCD", false)),
	)
	return []VisCase{{Name: "check/compact", Calls: compact}, {Name: "check/similarity", Calls: similarity}, {Name: "check/source-like", Calls: sourceLike}}
}
