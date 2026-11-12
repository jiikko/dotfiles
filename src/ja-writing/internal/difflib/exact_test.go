package difflib

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"ja-writing/oracle"
)

// repeatedDoc は同じ段落を n 回繰り返した文書 (本家が遅かった形。同じ字が何度も出る)。段落ごとに番号を入れるかを選べる。
func repeatedDoc(paragraph string, n int, numbered bool) []rune {
	var b strings.Builder
	for k := range n {
		if numbered {
			fmt.Fprintf(&b, "%d. ", k)
		}
		b.WriteString(paragraph)
		b.WriteString("\n\n")
	}
	return []rune(b.String())
}

const paragraph = "この道具は、直す前と後の文を比べて、意味が変わった疑いのある所を出します。数値・否定・伝聞・動作主を見ます。"

type pair struct {
	name string
	a, b []rune
}

// pairs は試験の入力の組 (字の種類の少ない文字列・同じ字の繰り返し・同じ段落の繰り返しを直したもの・共通の無いもの・空)。
func pairs() []pair {
	r := oracle.LCG(1)
	var out []pair
	add := func(name string, a, b []rune) {
		out = append(out, pair{name, a, b}, pair{name + "/逆", b, a})
	}
	add("空", nil, nil)
	add("片方が空", nil, []rune("あいう"))
	add("同じ", []rune(paragraph), []rune(paragraph))
	add("共通なし", []rune("abcdefgh"), []rune("ijklmnop"))
	for _, alpha := range []string{"a", "ab", "abc", "abcde", "あいうえおかきくけこ", "abcdefghijklmnopqrstuvwxyz"} {
		alphabet := []rune(alpha)
		for _, n := range []int{1, 2, 7, 30, 64, 65, 120, 300, 900} {
			a := r.Runes(n, alphabet)
			add(fmt.Sprintf("乱/%s/%d/直す", alpha, n), a, r.Edit(a, 1+n/40, alphabet))
			add(fmt.Sprintf("乱/%s/%d/別", alpha, n), a, r.Runes(n+r.Next(20), alphabet))
		}
	}
	for _, n := range []int{50, 200, 3000} {
		run := []rune(strings.Repeat("あ", n))
		add(fmt.Sprintf("1 字の繰り返し/%d", n), run, r.Edit(run, 3, []rune("あい")))
		add(fmt.Sprintf("周期の繰り返し/%d", n), []rune(strings.Repeat("ab", n)), []rune(strings.Repeat("ab", n-1)+"b"+strings.Repeat("ab", n/2)))
	}
	alphabet := []rune("あいうえお。、数値否定")
	for _, n := range []int{3, 10, 40} {
		doc := repeatedDoc(paragraph, n, false)
		add(fmt.Sprintf("段落の繰り返し/%d/直す", n), doc, r.Edit(doc, 4, alphabet))
		add(fmt.Sprintf("段落の繰り返し/%d/番号", n), doc, repeatedDoc(paragraph, n, true))
		add(fmt.Sprintf("段落の繰り返し/%d/減らす", n), doc, repeatedDoc(paragraph, n-1, false))
	}
	return out
}

// tunings は試験で使う閾値。本家と同じ値と、短い入力でも全部の求め方を通す小さい値 (どの求め方も同じ値を求めるので、閾値は答えを変えない)。
func tunings() map[string]tuning {
	small := tuning{smallArea: 4, probeMax: 8, seedShort: 3, seedLong: 5, sampleMin: 6, sampleBudget: 4, samFactor: 1,
		wholeIndexMin: 16, probeCache: 2, localDiv: 8, automatonMax: 40, hashMask: ^uint64(0)}
	noLateDP := small
	noLateDP.samFactor = 0
	bigBudget := small
	bigBudget.sampleBudget, bigBudget.automatonMax = 1, 1<<16
	collide := small
	collide.hashMask = 0x3
	return map[string]tuning{"本家": defaultTuning, "小": small, "DP なし": noLateDP, "候補を多く": bigBudget, "hash の衝突": collide}
}

func newExactTuned(a, b []rune, t tuning) *SequenceMatcher[rune] {
	m := &SequenceMatcher[rune]{}
	m.fast = &exactFinder{m: m, tune: t}
	m.SetSeqs(a, b)
	return m
}

// pathNames は path の名前 (試験の表示に使う。path の並びと同じ順)。
var pathNames = []string{"smallDP", "findA", "findB", "findMiss", "probeSmall", "probeWhole", "probeLocalB", "probeLocalA", "probeEvict", "probeHit",
	"seededFound", "seededEarly", "seededNone", "seededBudget", "lateDP", "automaton", "automatonCap", "bisect", "hashCollision"}

// TestExactMatchesPlain は、本家の速い照合 (NewExact) が difflib の求め方 (New) と同じ答えを返すことを、閾値を替えて確かめる。
// 求め方ごとに、試験の入力で 1 回以上通ったことも数える (どこかの求め方が試験から外れたまま緑にならないように)。
func TestExactMatchesPlain(t *testing.T) {
	var stats [numPaths]int
	cases := pairs()
	for name, tune := range tunings() {
		for _, p := range cases {
			plain := New(p.a, p.b)
			exact := newExactTuned(p.a, p.b, tune)
			want, got := plain.GetMatchingBlocks(), exact.GetMatchingBlocks()
			if !slices.Equal(got, want) {
				t.Fatalf("%s / %s: NewExact の一致 %v、New は %v", name, p.name, got, want)
			}
			if !slices.Equal(exact.GetOpcodes(), plain.GetOpcodes()) || exact.Ratio() != plain.Ratio() {
				t.Fatalf("%s / %s: opcodes か ratio が違う", name, p.name)
			}
			// 部分の範囲の find_longest_match (get_matching_blocks の外から呼ぶ形)。
			r := oracle.LCG(len(p.a)*31 + len(p.b))
			for range 6 {
				alo, blo := r.Next(len(p.a)+1), r.Next(len(p.b)+1)
				ahi, bhi := alo+r.Next(len(p.a)-alo+1), blo+r.Next(len(p.b)-blo+1)
				if got, want := exact.FindLongestMatch(alo, ahi, blo, bhi), plain.FindLongestMatch(alo, ahi, blo, bhi); got != want {
					t.Fatalf("%s / %s: FindLongestMatch(%d, %d, %d, %d) = %v、New は %v", name, p.name, alo, ahi, blo, bhi, got, want)
				}
			}
			for k, n := range exact.fast.stats {
				stats[k] += n
			}
		}
	}
	if len(pathNames) != int(numPaths) {
		t.Fatalf("求め方の名前 %d 個と numPaths %d が合わない", len(pathNames), numPaths)
	}
	for k, n := range stats {
		if n == 0 {
			t.Errorf("求め方 %s が試験の入力で 1 回も通らなかった", pathNames[k])
		}
	}
	t.Logf("求め方ごとの回数: %v", stats)
}

// TestExactReuseWithSetSeq は、照合を使い回して比べる相手を替えたとき (本家の set_seq1 / set_seq2 の使い方)、作り直したものと同じ答えを返すことを確かめる。
// b で決まる表 (種の表・長さちょうどの表) を替えたときに捨て忘れると、前の b の位置を返す。
func TestExactReuseWithSetSeq(t *testing.T) {
	tune := tunings()["小"]
	cases := pairs()
	exact := newExactTuned(nil, nil, tune)
	for k, p := range cases {
		q := cases[(k*7+3)%len(cases)]
		exact.SetSeq2(p.b)
		exact.SetSeq1(p.a)
		if got, want := exact.GetMatchingBlocks(), New(p.a, p.b).GetMatchingBlocks(); !slices.Equal(got, want) {
			t.Fatalf("%s: 使い回した照合の一致 %v、作り直すと %v", p.name, got, want)
		}
		exact.SetSeq1(q.a)
		if got, want := exact.Ratio(), New(q.a, p.b).Ratio(); got != want {
			t.Fatalf("%s と %s: set_seq1 の後の ratio %v、作り直すと %v", q.name, p.name, got, want)
		}
		exact.SetSeqs(q.b, q.a)
		if got, want := exact.GetOpcodes(), New(q.b, q.a).GetOpcodes(); !slices.Equal(got, want) {
			t.Fatalf("%s: set_seqs の後の opcodes が違う", q.name)
		}
	}
}

func TestIndexRunesMatchesStringsIndex(t *testing.T) {
	r := oracle.LCG(7)
	for range 2000 {
		hay := r.Runes(r.Next(40), []rune("ab"))
		needle := r.Runes(r.Next(5), []rune("ab"))
		lo := r.Next(len(hay) + 1)
		hi := lo + r.Next(len(hay)-lo+1)
		want := -1
		if k := strings.Index(string(hay[lo:hi]), string(needle)); k != -1 {
			want = lo + k // 字はどれも 1 バイト
		}
		if got := indexRunes(hay, lo, hi, needle); got != want {
			t.Fatalf("indexRunes(%q, %d, %d, %q) = %d、strings.Index は %d", string(hay), lo, hi, string(needle), got, want)
		}
	}
}

func TestRollingHashesMatchPolyHash(t *testing.T) {
	r := oracle.LCG(9)
	rs := r.Runes(500, []rune{'a', 'あ', 0x10FFFF, 0xD800, 0, -1, -0x80000000, 0x7fffffff})
	for _, size := range []int{1, 2, 13, 256, 500} {
		hs := rollingHashes(rs, size)
		if len(hs) != len(rs)-size+1 {
			t.Fatalf("size %d: %d 個", size, len(hs))
		}
		for k, h := range hs {
			if want := polyHash(rs[k : k+size]); h != want {
				t.Fatalf("size %d の位置 %d: ずらして求めた hash %d、直接求めると %d", size, k, h, want)
			}
		}
	}
	if rollingHashes(rs, 501) != nil || rollingHashes(rs, 0) != nil {
		t.Fatal("部分が無いときは空")
	}
}
