package oracle

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// difflibParagraph は同じ段落を繰り返す入力に使う段落 (本家が遅かった形。同じ字が何度も出る)。
const difflibParagraph = "この道具は、直す前と後の文を比べて、意味が変わった疑いのある所を出します。数値・否定・伝聞・動作主を見ます。\n\n"

// LCG は照合の入力を作る決まった数列 (乱数は使わない。同じ記録は同じ入力から作る)。internal/difflib の試験も同じものを使う。
type LCG uint64

// Next は [0, n) の次の値。
func (r *LCG) Next(n int) int {
	*r = *r*6364136223846793005 + 1442695040888963407
	return int(uint64(*r>>33) % uint64(n))
}

// Runes は alphabet の字を n 個並べた列。
func (r *LCG) Runes(n int, alphabet []rune) []rune {
	out := make([]rune, n)
	for i := range out {
		out[i] = alphabet[r.Next(len(alphabet))]
	}
	return out
}

// Edit は rs の決まった位置に、削除・置き換え・差し込みを count 回当てたものを返す (rs は変えない)。
func (r *LCG) Edit(rs []rune, count int, alphabet []rune) []rune {
	out := append([]rune(nil), rs...)
	for range count {
		pos := 0
		if len(out) > 0 {
			pos = r.Next(len(out))
		}
		size := 1 + r.Next(8)
		end := min(len(out), pos+size)
		switch r.Next(3) {
		case 0:
			out = append(out[:pos:pos], out[end:]...)
		case 1:
			out = append(append(append([]rune(nil), out[:pos]...), r.Runes(size, alphabet)...), out[pos:]...)
		default:
			out = append(append(append([]rune(nil), out[:pos]...), r.Runes(size, alphabet)...), out[end:]...)
		}
	}
	return out
}

// finds は長さ la と lb の並びの中の範囲を n 個選ぶ (find_longest_match を部分の範囲で呼ぶ形)。
func (r *LCG) finds(la, lb, n int) [][4]int {
	out := [][4]int{{0, la, 0, lb}}
	for range n {
		alo, blo := r.Next(la+1), r.Next(lb+1)
		out = append(out, [4]int{alo, alo + r.Next(la-alo+1), blo, blo + r.Next(lb-blo+1)})
	}
	return out
}

// wlen は WTF-8 の文字列の字の数 (Python の len。単独のサロゲートも 1 字。[]rune(s) はサロゲートの 3 バイトを 3 つの U+FFFD にする)。
func wlen(s string) int {
	if rs, ok := surrogateRunes(s); ok {
		return len(rs)
	}
	return utf8.RuneCountInString(s)
}

func strSeq(s string) DifflibSeq { return DifflibSeq{Parts: []SeqPart{{Text: WStr(s)}}} }

func runeSeq(rs []rune) DifflibSeq { return strSeq(fromRunes(rs)) }

func repeatSeq(parts ...SeqPart) DifflibSeq { return DifflibSeq{Parts: parts} }

func listSeq(items ...string) DifflibSeq {
	out := DifflibSeq{List: []WStr{}}
	for _, it := range items {
		out.List = append(out.List, WStr(it))
	}
	return out
}

// allStrings は alphabet の字で作る長さ maxLen 以下の全部の文字列 (短い順、同じ長さは辞書の順)。
func allStrings(alphabet string, maxLen int) []string {
	out := []string{""}
	level := []string{""}
	for range maxLen {
		var next []string
		for _, s := range level {
			for _, c := range alphabet {
				next = append(next, s+string(c))
			}
		}
		out = append(out, next...)
		level = next
	}
	return out
}

// corpusPart は入力の集まりのファイルを指す部分 (中身は記録に書かず、SHA-256 で変わっていないことを見る)。
func corpusPart(corpusDir, rel string) (SeqPart, error) {
	body, err := os.ReadFile(filepath.Join(corpusDir, filepath.FromSlash(rel)))
	if err != nil {
		return SeqPart{}, fmt.Errorf("照合の入力 %s を読めません: %w", rel, err)
	}
	return SeqPart{File: rel, SHA256: SHA256Hex(body)}, nil
}

// DifflibCases は difflib.json の入力 (結果はまだ持たない)。手で選んだ端・小さい字の種類の全組み合わせ・字の種類の少ない長い文字列・
// 同じ段落の繰り返し・同じ字の長い繰り返し・入力の集まりの直す前と後の組・文字列のリスト・照合の使い回し。
func DifflibCases(corpusDir string) ([]DifflibCase, error) {
	var cases []DifflibCase
	add := func(c DifflibCase) { cases = append(cases, c) }
	str := func(name string, a, b DifflibSeq, finds [][4]int) {
		add(DifflibCase{Name: name, Kind: "str", A: a, B: b, Finds: finds})
	}
	r := LCG(20261113)

	// 端。サロゲートの組 (上位と下位を別の 2 コードポイントで持つ並び) と本物の U+1F600 は、Python では長さ 2 と 1 で等しくない。
	pair := fromRunes([]rune{0xD83D, 0xDE00})
	for _, e := range [][3]string{
		{"空", "", ""}, {"a が空", "", "あいう"}, {"b が空", "あいう", ""}, {"同じ", "abc", "abc"}, {"共通なし", "abcd", "efgh"},
		{"サロゲートの組と本物", pair + "a" + pair, "\U0001F600a" + pair},
		{"単独のサロゲート", fromRunes([]rune{'a', 0xD800, 'b', 0xDFFF}), fromRunes([]rune{0xDFFF, 'a', 0xD800})},
		{"BMP の外の字", "😀a😀🇯🇵", "a😀🇯🇵😀"},
		{"制御文字と改行", "a\x00b\r\nc\u2028", "\x00b\nc\u2028a"},
		{"結合文字", "が", "か\u3099"},
	} {
		la, lb := wlen(e[1]), wlen(e[2])
		str("端/"+e[0], strSeq(e[1]), strSeq(e[2]), r.finds(la, lb, 3))
	}
	// 小さい字の種類の全組み合わせ (DP の同点の選び方: いちばん長く、a で最も前、同じ a なら b で最も前)。
	for _, alpha := range []struct {
		chars string
		max   int
	}{{"ab", 3}, {"abc", 2}} {
		all := allStrings(alpha.chars, alpha.max)
		for _, a := range all {
			for _, b := range all {
				str(fmt.Sprintf("全組/%s/%q/%q", alpha.chars, a, b), strSeq(a), strSeq(b), [][4]int{{0, len(a), 0, len(b)}})
			}
		}
	}
	// 字の種類の少ない文字列 (乱は決まった数列)。直したものと、別に作ったもの。
	for _, alpha := range []struct {
		chars string
		sizes []int
	}{
		{"ab", []int{10, 60, 300, 1500}},
		{"abc", []int{10, 60, 300, 1500}},
		{"abcde", []int{60, 300, 1500, 4000}},
		{"あいうえおかきくけこさしすせそ", []int{60, 300, 1500, 6000}},
	} {
		alphabet := []rune(alpha.chars)
		for _, n := range alpha.sizes {
			a := r.Runes(n, alphabet)
			b := r.Edit(a, 1+n/50, alphabet)
			str(fmt.Sprintf("乱/%s/%d/直す", alpha.chars, n), runeSeq(a), runeSeq(b), r.finds(len(a), len(b), 4))
			c := r.Runes(n+r.Next(n/5+1), alphabet)
			str(fmt.Sprintf("乱/%s/%d/別", alpha.chars, n), runeSeq(a), runeSeq(c), r.finds(len(a), len(c), 4))
		}
	}
	// 字の種類の多い長い文字列が短い部分だけを共有する形。分けた範囲の答えの上限が短く (40 字)、b の範囲が長い (4,096 字超え) ので、
	// 本家と同じ閾値でも b 全体の部分文字列の表で長さちょうどの一致を探す枝に入る。
	kanji := make([]rune, 200)
	for i := range kanji {
		kanji[i] = rune(0x4E00 + i)
	}
	shared := r.Runes(40, kanji)
	part := func() []rune { return r.Runes(5000, kanji) }
	la, lb := []rune{}, []rune{}
	la = append(append(append(la, part()...), shared...), part()...)
	lb = append(append(append(lb, part()...), shared...), part()...)
	str("長い乱/共通の 40 字", runeSeq(la), runeSeq(lb), r.finds(len(la), len(lb), 2))
	// 同じ段落の繰り返し。段落を 1 つ直す・1 つ減らす・番号を振る。大きいものは difflib では遅すぎるので本家の速い照合だけを通す。
	edited := fromRunes(r.Edit([]rune(difflibParagraph), 3, []rune("あいう。、数値")))
	para := func(n int) SeqPart { return SeqPart{Text: difflibParagraph, Repeat: n} }
	for _, n := range []int{5, 30, 100, 400} {
		exactOnly := n >= 400
		k := n / 3
		la := n * wlen(difflibParagraph)
		for _, v := range []struct {
			name string
			b    DifflibSeq
		}{
			{"1 つ直す", repeatSeq(para(k), SeqPart{Text: WStr(edited)}, para(n-k-1))},
			{"1 つ減らす", repeatSeq(para(n - 1))},
			{"末尾に足す", repeatSeq(para(n), SeqPart{Text: "おわり。"})},
		} {
			b, err := v.b.Text("")
			if err != nil {
				return nil, err
			}
			add(DifflibCase{Name: fmt.Sprintf("段落の繰り返し/%d/%s", n, v.name), Kind: "str", A: repeatSeq(para(n)), B: v.b,
				Finds: r.finds(la, wlen(string(b)), 3), ExactOnly: exactOnly})
		}
	}
	var numbered strings.Builder
	for k := range 30 {
		fmt.Fprintf(&numbered, "%d. %s", k, difflibParagraph)
	}
	str("段落の繰り返し/30/番号", repeatSeq(para(30)), strSeq(numbered.String()), nil)
	// 同じ字と短い周期の長い繰り返し (種の段が候補の多さで止まる)。b が 65,536 字を超えるものは長さの二分探索に入る。
	for _, n := range []int{300, 2000} {
		str(fmt.Sprintf("1 字の繰り返し/%d", n), repeatSeq(SeqPart{Text: "あ", Repeat: n}),
			repeatSeq(SeqPart{Text: "あ", Repeat: n / 3}, SeqPart{Text: "い"}, SeqPart{Text: "あ", Repeat: n - n/3}), r.finds(n, n+1, 3))
		str(fmt.Sprintf("周期の繰り返し/%d", n), repeatSeq(SeqPart{Text: "ab", Repeat: n}),
			repeatSeq(SeqPart{Text: "ab", Repeat: n / 2}, SeqPart{Text: "b"}, SeqPart{Text: "ab", Repeat: n / 2}), r.finds(2*n, 2*n+1, 3))
	}
	for _, n := range []int{40000, 70000} {
		add(DifflibCase{Name: fmt.Sprintf("1 字の繰り返し/%d", n), Kind: "str", ExactOnly: true,
			A: repeatSeq(SeqPart{Text: "x", Repeat: n}, SeqPart{Text: "y"}, SeqPart{Text: "x", Repeat: n}),
			B: repeatSeq(SeqPart{Text: "x", Repeat: n}, SeqPart{Text: "z"}, SeqPart{Text: "x", Repeat: n})})
	}
	add(DifflibCase{Name: "周期の繰り返し/40000", Kind: "str", ExactOnly: true,
		A: repeatSeq(SeqPart{Text: "ab", Repeat: 40000}), B: repeatSeq(SeqPart{Text: "ab", Repeat: 20000}, SeqPart{Text: "a"}, SeqPart{Text: "ab", Repeat: 20000})})
	// 入力の集まりの直す前と後の組 (文字列と行のリスト)。
	names, err := corpusPairs(corpusDir)
	if err != nil {
		return nil, err
	}
	for _, p := range names {
		a, err := corpusPart(corpusDir, p[0])
		if err != nil {
			return nil, err
		}
		b, err := corpusPart(corpusDir, p[1])
		if err != nil {
			return nil, err
		}
		add(DifflibCase{Name: "組/" + p[1], Kind: "str", A: repeatSeq(a), B: repeatSeq(b)})
		aa, bb := a, b
		add(DifflibCase{Name: "組の行/" + p[1], Kind: "list", A: DifflibSeq{Lines: &aa}, B: DifflibSeq{Lines: &bb}})
	}
	// 文字列のリスト。小さい全組み合わせ・空の要素と囲みの繰り返し・長い要素の重複。
	all := allStrings("xy", 2)
	for _, a := range all {
		for _, b := range all {
			add(DifflibCase{Name: fmt.Sprintf("リスト/全組/%q/%q", a, b), Kind: "list", A: listSeq(strings.Split(a, "")...), B: listSeq(strings.Split(b, "")...)})
		}
	}
	long := strings.Repeat(difflibParagraph, 3)
	add(DifflibCase{Name: "リスト/空行と囲み", Kind: "list",
		A: listSeq("# 見出し", "", "```", "a", "```", "", "", "本文", "", "```", "b", "```", ""),
		B: listSeq("# 見出し", "", "", "```", "a", "```", "本文", "", "```", "c", "```", "", "")})
	add(DifflibCase{Name: "リスト/長い要素の重複", Kind: "list",
		A: listSeq(long, "短い", long, "", long, "末尾"), B: listSeq("短い", long, long, "", "末尾", long)})
	add(DifflibCase{Name: "リスト/空", Kind: "list", A: listSeq(), B: listSeq("a")})
	// 照合の使い回し。3005 行あたりの形 (直す前の文を b に置いて、直した文を a で回す) と、1902 行あたりの形 (長い b に長さの違う a を当てる)。
	sentences := func(rel string) ([]string, error) {
		body, err := os.ReadFile(filepath.Join(corpusDir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		var out []string
		for _, s := range strings.SplitAfter(string(body), "。") {
			if strings.TrimSpace(s) != "" && len(out) < 7 {
				out = append(out, s)
			}
		}
		return out, nil
	}
	if len(names) > 0 {
		olds, err := sentences(names[0][0])
		if err != nil {
			return nil, err
		}
		news, err := sentences(names[0][1])
		if err != nil {
			return nil, err
		}
		add(DifflibCase{Name: "使い回し/文", Kind: "reuse", A: listSeq(olds...), B: listSeq(news...)})
	}
	longB := strings.Repeat(difflibParagraph, 80)
	add(DifflibCase{Name: "使い回し/長い b", Kind: "reuse", A: listSeq(longB, strings.Repeat(difflibParagraph, 79)+edited),
		B: listSeq(strings.Repeat(difflibParagraph, 3), strings.Repeat(difflibParagraph, 40)+edited+strings.Repeat(difflibParagraph, 39), "", longB, edited, strings.Repeat(difflibParagraph, 81))})
	return cases, nil
}

// corpusPairs は入力の集まりの直す前と後の組 ([前, 後])。本家の raw_ai と yomiyasu_rewritten の同じ名前のファイル (後が無い名前は除く)。
func corpusPairs(corpusDir string) ([][2]string, error) {
	before, err := filepath.Glob(filepath.Join(corpusDir, "upstream", "raw_ai", "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(before)
	var out [][2]string
	for _, path := range before {
		name := filepath.Base(path)
		after := filepath.Join(corpusDir, "upstream", "yomiyasu_rewritten", name)
		if _, err := os.Stat(after); err != nil {
			continue
		}
		out = append(out, [2]string{"upstream/raw_ai/" + name, "upstream/yomiyasu_rewritten/" + name})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("入力の集まり %s に直す前と後の組が 1 つもありません", corpusDir)
	}
	return out, nil
}
