package lint

import (
	"cmp"
	"slices"

	"ja-writing/internal/pyjson"
)

// 候補の内部の欄を、本家の記録 (lint-raw.json) と比べる値にする。本番は内部の欄を出力に出さないので、比べるテストだけが使う。

// internalValue は内部の欄も含めた候補の値 (本家の候補の dict そのもの。記録との突き合わせ用)。鍵は昇順 (記録の正規の JSON の sort_keys に合わせる)。
// 持たない内部の欄は出さない (本家は該当する規則の候補にだけ、その欄を足す)。
func (f finding) internalValue() pyjson.Value {
	o := pyjson.NewObject()
	if f.FlagHits != nil {
		o.Set("_flag_hits", stringValues(f.FlagHits))
	}
	if f.Heads || f.Objects != nil {
		o.Set("_heads", f.Heads)
	}
	if f.Objects != nil {
		o.Set("_objects", stringValues(f.Objects))
	}
	if f.PatternMatches != nil {
		m := pyjson.NewObject()
		for _, p := range sortedPatternMatches(f.PatternMatches) {
			m.Set(p.Name, stringValues(p.Strings))
		}
		o.Set("_pattern_matches", m)
	}
	if f.Replacements != nil {
		m := pyjson.NewObject()
		for _, p := range sortedReplacements(f.Replacements) {
			m.Set(p.Word, p.Replacement)
		}
		o.Set("_replacements", m)
	}
	if f.WordTypes != nil {
		list := make([]pyjson.Value, len(f.WordTypes))
		for i, w := range f.WordTypes {
			list[i] = []pyjson.Value{w.Word, int64(w.Number)}
		}
		o.Set("_word_types", list)
	}
	if f.Words != nil {
		o.Set("_words", stringValues(f.Words))
	}
	o.Set("line", int64(f.Line))
	o.Set("message", f.Message)
	o.Set("rule", f.Rule)
	o.Set("severity", f.Severity)
	o.Set("snippet", f.Snippet)
	return o
}

func stringValues(ss []string) pyjson.Value {
	return valuesOf(ss, func(s string) pyjson.Value { return s })
}

func valuesOf[T any](items []T, f func(T) pyjson.Value) pyjson.Value {
	out := make([]pyjson.Value, len(items))
	for i, item := range items {
		out[i] = f(item)
	}
	return out
}

func sortedPatternMatches(items []patternMatch) []patternMatch {
	return slices.SortedStableFunc(slices.Values(items), func(a, b patternMatch) int { return cmp.Compare(a.Name, b.Name) })
}

func sortedReplacements(items []wordPair) []wordPair {
	return slices.SortedStableFunc(slices.Values(items), func(a, b wordPair) int { return cmp.Compare(a.Word, b.Word) })
}
