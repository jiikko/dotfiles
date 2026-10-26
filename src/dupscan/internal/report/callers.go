package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"dupscan/internal/tree"
)

// CallQuery は callers の引き方: 名前と、書いてあれば引数ラベル (`make(x:_:)` の x と _)。
type CallQuery struct {
	Name   string
	Labels []string // nil ならラベルで絞らない
}

// ParseCallQuery は `名前` か `名前(ラベル:ラベル:)` を読む。ラベルの無い引数は `_:`。
func ParseCallQuery(s string) (CallQuery, error) {
	name, rest, ok := strings.Cut(s, "(")
	if name == "" {
		return CallQuery{}, fmt.Errorf("%q に名前が無い", s)
	}
	if !ok {
		return CallQuery{Name: name}, nil
	}
	inner, ok := strings.CutSuffix(rest, ")")
	if !ok {
		return CallQuery{}, fmt.Errorf("%q は 名前(ラベル:ラベル:) の形ではない", s)
	}
	q := CallQuery{Name: name, Labels: []string{}}
	for _, l := range strings.Split(inner, ":") {
		if l != "" {
			q.Labels = append(q.Labels, l)
		}
	}
	return q, nil
}

// matches は呼び出し c が q に当たるか。後置クロージャの引数はラベルが括弧の外にあるので、後置クロージャのある呼び出しは
// 括弧の中のラベルが q のラベルの先頭に一致すれば当てる (型を解決しないので近似)。
func (q CallQuery) matches(c tree.Call) bool {
	if c.Name != q.Name {
		return false
	}
	if q.Labels == nil {
		return true
	}
	if len(c.Labels) > len(q.Labels) || !c.Trailing && len(c.Labels) != len(q.Labels) {
		return false
	}
	for i, l := range c.Labels {
		if l != q.Labels[i] {
			return false
		}
	}
	return !c.Trailing || len(c.Labels) < len(q.Labels)
}

// CallerEntry は callers の 1 行。
type CallerEntry struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Col    int    `json:"col"`
	Caller string `json:"caller,omitempty"` // 呼び出しを囲む単位 (型.署名)。単位の外 (top-level 等) なら空
	Test   bool   `json:"test"`
	Text   string `json:"text"`
}

// CallersSummary は callers の出力。
type CallersSummary struct {
	Query   string        `json:"query"`
	Matches int           `json:"matches"`
	Entries []CallerEntry `json:"entries"`
}

// BuildCallers は q に当たる呼び出しを、prod を test より前・場所の順に並べる。isTest はファイルが test のファイルか。
func BuildCallers(query string, q CallQuery, calls []tree.Call, units []*tree.Unit, isTest func(string) bool) CallersSummary {
	s := CallersSummary{Query: query, Entries: []CallerEntry{}}
	byFile := map[string][]*tree.Unit{}
	for _, u := range units {
		byFile[u.File] = append(byFile[u.File], u)
	}
	for _, c := range calls {
		if !q.matches(c) {
			continue
		}
		e := CallerEntry{File: c.File, Line: c.Line, Col: c.Col, Test: isTest(c.File), Text: c.Text}
		if c.Owner != nil {
			if !c.Owner.Unreadable {
				e.Caller = label(UnitRef{Signature: c.Owner.Signature, Enclosing: c.Owner.Enclosing})
			}
			e.Test = e.Test || c.Owner.Test
		} else if u := enclosing(byFile[c.File], c.Line); u != nil {
			e.Caller = label(ref(u))
			e.Test = e.Test || u.Test
		}
		s.Entries = append(s.Entries, e)
	}
	sort.SliceStable(s.Entries, func(i, j int) bool {
		a, b := s.Entries[i], s.Entries[j]
		if a.Test != b.Test {
			return !a.Test
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	s.Matches = len(s.Entries)
	return s
}

// enclosing は line を含む名前の付いた単位のうち最も内側 (最も後の行で始まるもの)。クロージャは呼び出し元として名指ししない。
func enclosing(units []*tree.Unit, line int) *tree.Unit {
	var best *tree.Unit
	for _, u := range units {
		if u.Kind != tree.KindClosure && u.Line <= line && line <= u.EndLine && (best == nil || u.Line > best.Line) {
			best = u
		}
	}
	return best
}

// CallersText は callers を 1 行 1 件で出す (`file:line 呼び出し元 [test] | その行`)。
func CallersText(w io.Writer, s CallersSummary, limit int) error {
	return CallersTextMore(w, s, limit, "-limit 0 か -format json で全部。ラベルを書いて絞る")
}

// CallersTextMore は CallersText の、省略したときの案内 (more) を呼び出し側が決める版。
func CallersTextMore(w io.Writer, s CallersSummary, limit int, more string) error {
	if s.Matches == 0 {
		fmt.Fprintf(w, "dupscan callers: %s の呼び出しなし\n", s.Query)
		return nil
	}
	for i, e := range s.Entries {
		if limit > 0 && i == limit {
			fmt.Fprintf(w, "… 他 %d 件 (%s)\n", s.Matches-limit, more)
			break
		}
		caller := e.Caller
		if caller == "" {
			caller = "(単位の外)"
		}
		mark := ""
		if e.Test {
			mark = " [test]"
		}
		fmt.Fprintf(w, "%s:%d %s%s | %s\n", e.File, e.Line, caller, mark, e.Text)
	}
	return nil
}
