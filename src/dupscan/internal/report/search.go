package report

import (
	"errors"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"dupscan/internal/tree"
)

type SearchQuery struct {
	Terms      []string
	Filters    Query
	MinMatched int
	Scope      string
}

type SearchEvidence struct {
	Term       string
	Fields     []string
	Components []string
}

type SearchCandidate struct {
	Entry   IndexEntry
	EndLine int
	Matched []SearchEvidence
	Missing []string
	Score   int
}

type SearchSummary struct {
	Units      int
	Matches    int
	Candidates []SearchCandidate
}

// SearchTerms bounds the input before matching; repeated spellings retain their first form.
func SearchTerms(query string) ([]string, error) {
	if !utf8.ValidString(query) {
		return nil, errors.New("query must be valid UTF-8")
	}
	if len(query) > 512 {
		return nil, errors.New("query must be at most 512 UTF-8 bytes")
	}
	terms := []string{}
	seen := map[string]bool{}
	for _, term := range strings.Fields(query) {
		if utf8.RuneCountInString(term) > 64 {
			return nil, errors.New("query term must be at most 64 runes")
		}
		key := strings.ToLower(term)
		if !seen[key] {
			terms = append(terms, term)
			seen[key] = true
		}
	}
	if len(terms) < 1 || len(terms) > 8 {
		return nil, errors.New("query must contain 1 to 8 distinct terms")
	}
	return terms, nil
}

func BuildSearch(units []*tree.Unit, q SearchQuery) SearchSummary {
	s := SearchSummary{Candidates: []SearchCandidate{}}
	min := q.MinMatched
	if min == 0 {
		min = 1
	}
	for _, u := range units {
		if u.Kind == tree.KindClosure {
			continue
		}
		s.Units++
		if _, ok := q.Filters.match(u); !ok {
			continue
		}
		if q.Scope == "prod" && u.Test || q.Scope == "test" && !u.Test {
			continue
		}
		doc, _, _ := strings.Cut(u.Doc, "\n")
		c := SearchCandidate{EndLine: u.EndLine, Entry: IndexEntry{UnitRef: ref(u), ReturnType: u.ReturnType, ParamTypes: u.ParamTypes, Doc: doc, Shared: q.Filters.shared(u.File)}, Matched: []SearchEvidence{}, Missing: []string{}}
		fields := []string{u.Name, u.Signature, doc}
		for _, term := range q.Terms {
			ev := SearchEvidence{Term: term, Fields: []string{}, Components: searchComponents(term)}
			for i, value := range fields {
				if searchFieldMatches(value, term, ev.Components) {
					if len(ev.Fields) == 0 {
						c.Score += 3 - i
					}
					ev.Fields = append(ev.Fields, []string{"name", "signature", "doc"}[i])
				}
			}
			if len(ev.Fields) == 0 {
				c.Missing = append(c.Missing, term)
			} else {
				c.Matched = append(c.Matched, ev)
			}
		}
		if len(c.Matched) >= min {
			s.Candidates = append(s.Candidates, c)
		}
	}
	exact := strings.ToLower(strings.Join(q.Terms, " "))
	sort.Slice(s.Candidates, func(i, j int) bool {
		a, b := s.Candidates[i], s.Candidates[j]
		if len(a.Matched) != len(b.Matched) {
			return len(a.Matched) > len(b.Matched)
		}
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		ae, be := strings.ToLower(a.Entry.Name) == exact, strings.ToLower(b.Entry.Name) == exact
		if ae != be {
			return ae
		}
		if a.Entry.Shared != b.Entry.Shared {
			return a.Entry.Shared
		}
		if ah, bh := hidden(a.Entry.Visibility), hidden(b.Entry.Visibility); ah != bh {
			return !ah
		}
		if a.Entry.Test != b.Entry.Test {
			return !a.Entry.Test
		}
		if a.Entry.File != b.Entry.File {
			return a.Entry.File < b.Entry.File
		}
		if a.Entry.Line != b.Entry.Line {
			return a.Entry.Line < b.Entry.Line
		}
		if a.Entry.Col != b.Entry.Col {
			return a.Entry.Col < b.Entry.Col
		}
		if a.Entry.Signature != b.Entry.Signature {
			return a.Entry.Signature < b.Entry.Signature
		}
		return a.Entry.Key < b.Entry.Key
	})
	s.Matches = len(s.Candidates)
	return s
}

func searchFieldMatches(field, term string, components []string) bool {
	if strings.Contains(strings.ToLower(field), strings.ToLower(term)) {
		return true
	}
	// Japanese terms use only the original substring: splitting is not morphological analysis.
	if strings.ContainsFunc(term, func(r rune) bool {
		return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r)
	}) {
		return false
	}
	if len(components) == 0 {
		return false
	}
	tokens := searchComponents(field)
	for _, component := range components {
		if !containsAny(tokens, component) {
			return false
		}
	}
	return true
}

func searchComponents(value string) []string {
	runes := []rune(value)
	out := []string{}
	start := -1
	for i, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r) {
			if start >= 0 {
				out = append(out, strings.ToLower(string(runes[start:i])))
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
			continue
		}
		prev := runes[i-1]
		if unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev) || unicode.IsUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1])) {
			out = append(out, strings.ToLower(string(runes[start:i])))
			start = i
		}
	}
	if start >= 0 {
		out = append(out, strings.ToLower(string(runes[start:])))
	}
	return out
}
