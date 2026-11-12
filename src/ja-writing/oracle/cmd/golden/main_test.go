package main

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"ja-writing/oracle"
)

// 2 回の生成は PYTHONHASHSEED を変える。同じ seed の 2 回では hash の順に依存する出力が同じ順に出て、気づけない。
func TestTwiceVariesHashSeed(t *testing.T) {
	var seeds []string
	_, err := twice(oracle.Runner{}, func(r oracle.Runner) ([]byte, error) {
		seeds = append(seeds, r.HashSeed)
		return []byte("x"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seeds) != 2 || seeds[0] == seeds[1] || !slices.Contains(seeds, "0") {
		t.Fatalf("seed の並びが %q (0 と別の値が 1 つずつ要る)", seeds)
	}
}

func TestTwiceStopsWhenOutputDependsOnHashSeed(t *testing.T) {
	_, err := twice(oracle.Runner{}, func(r oracle.Runner) ([]byte, error) { return []byte("seed" + r.HashSeed), nil })
	if err == nil || !strings.Contains(err.Error(), "決定的でない") {
		t.Fatalf("seed で出力が変わるのに止まらない: %v", err)
	}
	if _, err := twice(oracle.Runner{}, func(oracle.Runner) ([]byte, error) { return nil, errors.New("boom") }); err == nil {
		t.Fatal("生成の失敗を返さない")
	}
}

func TestSamePatternsIgnoringTextOnlyForgivesPatternText(t *testing.T) {
	mk := func(pattern, digest string) []byte {
		b, err := oracle.MarshalPatterns(oracle.PatternsGolden{Patterns: []oracle.PatternRecord{{Pattern: pattern, Digest: digest, Matches: 1}}})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	two := func(p1, p2 string) []byte {
		b, err := oracle.MarshalPatterns(oracle.PatternsGolden{Patterns: []oracle.PatternRecord{{Pattern: p1, Digest: "1"}, {Pattern: p2, Digest: "2"}}})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	b2, _ := oracle.MarshalPatterns(oracle.PatternsGolden{Patterns: []oracle.PatternRecord{{Pattern: "y", Digest: "2"}, {Pattern: "x", Digest: "1"}}})
	if !samePatternsIgnoringText(two("p", "q"), b2) {
		t.Error("並びの違いを許さない")
	}
	if !samePatternsIgnoringText(mk("a|b", "d"), mk("b|a", "d")) {
		t.Error("パターンの文字列だけの違いを許さない")
	}
	if samePatternsIgnoringText(mk("a|b", "d"), mk("a|b", "e")) {
		t.Error("digest の違いを許した")
	}
	if samePatternsIgnoringText(mk("a", "d"), []byte("{")) {
		t.Error("壊れた記録を同じとした")
	}
}
