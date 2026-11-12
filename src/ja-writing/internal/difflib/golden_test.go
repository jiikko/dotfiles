package difflib

import (
	"slices"
	"testing"

	"ja-writing/internal/pyunicode"
	"ja-writing/oracle"
)

const (
	goldenDir = "../../testdata/golden"
	corpusDir = "../../testdata/corpus"
	// plainWorkMax は、本家の速い照合だけを通した記録の入力 (exact_only) でも New を比べる DP の手間の上限 (a の字と同じ b の字の組の数)。
	// これを超える入力 (同じ字の 8 万字の繰り返し) は、difflib の DP が長さの 2 乗で遅いので New を比べない (Python もそのために difflib を通していない)。
	plainWorkMax = 300_000_000
)

func toResult(blocks []Match, opcodes []OpCode, ratio, quick, realQuick float64, finds []Match) oracle.MatcherResult {
	r := oracle.MatcherResult{Ratio: ratio, Quick: quick, RealQuick: realQuick}
	for _, b := range blocks {
		r.Blocks = append(r.Blocks, [3]int{b.A, b.B, b.Size})
	}
	for _, o := range opcodes {
		r.Opcodes = append(r.Opcodes, oracle.OpcodeRec{Tag: o.Tag, I1: o.I1, I2: o.I2, J1: o.J1, J2: o.J2})
	}
	for _, f := range finds {
		r.Finds = append(r.Finds, [3]int{f.A, f.B, f.Size})
	}
	return r
}

func run[T comparable](m *SequenceMatcher[T], finds [][4]int) oracle.MatcherResult {
	var found []Match
	for _, f := range finds {
		found = append(found, m.FindLongestMatch(f[0], f[1], f[2], f[3]))
	}
	return toResult(m.GetMatchingBlocks(), m.GetOpcodes(), m.Ratio(), m.QuickRatio(), m.RealQuickRatio(), found)
}

func reuse[T comparable](m *SequenceMatcher[T], olds, news [][]T) oracle.MatcherResult {
	var r oracle.MatcherResult
	for _, old := range olds {
		m.SetSeq2(old)
		for _, n := range news {
			m.SetSeq1(n)
			r.Reuse = append(r.Reuse, [3]float64{m.RealQuickRatio(), m.QuickRatio(), m.Ratio()})
		}
	}
	return r
}

func sameResult(got, want oracle.MatcherResult) bool {
	return slices.Equal(got.Blocks, want.Blocks) && slices.Equal(got.Opcodes, want.Opcodes) && got.Ratio == want.Ratio &&
		got.Quick == want.Quick && got.RealQuick == want.RealQuick && slices.Equal(got.Finds, want.Finds) && slices.Equal(got.Reuse, want.Reuse)
}

func text(t *testing.T, s oracle.DifflibSeq) []rune {
	t.Helper()
	w, err := s.Text(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	return pyunicode.Runes(string(w))
}

func items(t *testing.T, s oracle.DifflibSeq) []string {
	t.Helper()
	ws, err := s.Items(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = string(w)
	}
	return out
}

func plainWork(a, b []rune) int {
	count := make(map[rune]int)
	for _, r := range b {
		count[r]++
	}
	work := 0
	for _, r := range a {
		work += count[r]
	}
	return work
}

// TestMatchesPythonRecording は、記録の全入力で New が Python の difflib.SequenceMatcher と、NewExact が本家の _ExactSequenceMatcher と同じ値を返すことを確かめる
// (記録の driver は、2 つの Python の答えが同じことを確かめてから 1 つの結果を書いている)。
func TestMatchesPythonRecording(t *testing.T) {
	g, err := oracle.ReadDifflibGolden(goldenDir)
	if err != nil {
		t.Fatalf("make golden で記録を作る: %v", err)
	}
	if len(g.Cases) < 500 || len(g.CallSites) == 0 {
		t.Fatalf("記録の入力が %d 件・本家の照合の呼び出しが %d 件しかない", len(g.Cases), len(g.CallSites))
	}
	for _, name := range g.Methods {
		if !slices.Contains(oracle.DifflibMethods, name) {
			t.Errorf("本家が使うメソッド %s を Go が持たない", name)
		}
	}
	var stats [numPaths]int
	counts := map[string]int{}
	for _, c := range g.Cases {
		if c.Result == nil {
			t.Fatalf("%s: 結果が無い", c.Name)
		}
		want := *c.Result
		switch c.Kind {
		case "str":
			a, b := text(t, c.A), text(t, c.B)
			exact := NewExact(a, b)
			if got := run(exact, c.Finds); !sameResult(got, want) {
				t.Fatalf("%s: NewExact の結果\n%+v\nPython は\n%+v", c.Name, got, want)
			}
			for k, n := range exact.fast.stats {
				stats[k] += n
			}
			if c.ExactOnly && plainWork(a, b) > plainWorkMax {
				counts["New を比べない"]++
				continue
			}
			if got := run(New(a, b), c.Finds); !sameResult(got, want) {
				t.Fatalf("%s: New の結果\n%+v\nPython は\n%+v", c.Name, got, want)
			}
		case "list":
			if got := run(New(items(t, c.A), items(t, c.B)), nil); !sameResult(got, want) {
				t.Fatalf("%s: New の結果\n%+v\nPython は\n%+v", c.Name, got, want)
			}
		case "reuse":
			var olds, news [][]rune
			for _, s := range items(t, c.A) {
				olds = append(olds, pyunicode.Runes(s))
			}
			for _, s := range items(t, c.B) {
				news = append(news, pyunicode.Runes(s))
			}
			if got := reuse(New[rune](nil, nil), olds, news); !sameResult(got, want) {
				t.Fatalf("%s: New を使い回した結果 %v、Python は %v", c.Name, got.Reuse, want.Reuse)
			}
			exact := NewExact(nil, nil)
			if got := reuse(exact, olds, news); !sameResult(got, want) {
				t.Fatalf("%s: NewExact を使い回した結果 %v、Python は %v", c.Name, got.Reuse, want.Reuse)
			}
			for k, n := range exact.fast.stats {
				stats[k] += n
			}
		default:
			t.Fatalf("%s: 知らない種類 %q", c.Name, c.Kind)
		}
		counts[c.Kind]++
	}
	// 本家と同じ閾値の NewExact が、記録の大きい入力で長さの二分探索とオートマトンまで通ったこと (通らなければ大きい入力の記録が要らない形に変わっている)。
	for _, p := range []path{pathSmallDP, pathSeededFound, pathSeededBudget, pathAutomaton, pathBisect, pathProbeWhole} {
		if stats[p] == 0 {
			t.Errorf("本家と同じ閾値で、求め方 %s が記録の入力で 1 回も通らなかった", pathNames[p])
		}
	}
	t.Logf("記録の %d 件と一致 (%v)。本家と同じ閾値の求め方ごとの回数: %v", len(g.Cases), counts, stats)
}
