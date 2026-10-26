package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"dupscan/internal/tree"
)

// DefQuery is an exact definition lookup. Labels nil means that labels are not a filter.
type DefQuery struct {
	Name   string
	Type   string
	Labels []string
}

// ParseDefQuery reads `name`, `Type.member`, and `name(label:label:)`.
func ParseDefQuery(value string) (DefQuery, error) {
	value = strings.TrimSpace(value)
	name, rest, hasLabels := strings.Cut(value, "(")
	var labels []string
	if hasLabels {
		inner, ok := strings.CutSuffix(rest, ")")
		if !ok || strings.ContainsAny(inner, "()") {
			return DefQuery{}, fmt.Errorf("%q は 名前(ラベル:ラベル:) の形ではない", value)
		}
		labels = []string{}
		for _, label := range strings.Split(inner, ":") {
			if label != "" {
				labels = append(labels, label)
			}
		}
	}
	if name == "" {
		return DefQuery{}, fmt.Errorf("%q に名前が無い", value)
	}
	q := DefQuery{Name: name, Labels: labels}
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		if dot == 0 || dot == len(name)-1 {
			return DefQuery{}, fmt.Errorf("%q は Type.member の形ではない", value)
		}
		q.Type = name[:dot]
		q.Name = name[dot+1:]
	}
	return q, nil
}

// DefinitionRecord is a def result assembled from adapter metadata and source lines.
type DefinitionRecord struct {
	Name       string
	Kind       string
	File       string
	Line       int
	EndLine    int
	Col        int
	Enclosing  string
	Signature  string
	Labels     []string
	Test       bool
	Nested     bool
	Visibility string
	Doc        string
}

// TypeRecord is a declaration of a type body, kept separate from comparison units.
type TypeRecord struct {
	Name       string
	Kind       string
	File       string
	Line       int
	EndLine    int
	Col        int
	Enclosing  string
	Scope      string
	Signature  string
	Test       bool
	Nested     bool
	Visibility string
	Doc        string
}

// DefinitionEntry is one def result. Source is omitted for type declarations, which use Members.
type DefinitionEntry struct {
	Name           string       `json:"name"`
	Kind           string       `json:"kind"`
	File           string       `json:"file"`
	Line           int          `json:"line"`
	EndLine        int          `json:"end_line"`
	Enclosing      string       `json:"enclosing,omitempty"`
	Signature      string       `json:"signature"`
	Test           bool         `json:"test"`
	Source         []string     `json:"source,omitempty"`
	Truncated      bool         `json:"truncated,omitempty"`
	RemainingLines int          `json:"remaining_lines,omitempty"`
	Members        []IndexEntry `json:"members,omitempty"`
}

// DefinitionsSummary is the JSON shape returned by def.
type DefinitionsSummary struct {
	Query   string            `json:"query"`
	Matches int               `json:"matches"`
	Entries []DefinitionEntry `json:"entries"`
}

// BuildDefinitions assembles exact matches. Type declarations are separate records, not comparison units.
func BuildDefinitions(query string, q DefQuery, defs []DefinitionRecord, types []TypeRecord, units []*tree.Unit, calls []tree.Call, sources map[string][]byte, maxLines int, shared func(string) bool) DefinitionsSummary {
	s := DefinitionsSummary{Query: query, Entries: []DefinitionEntry{}}
	for _, d := range defs {
		if d.Name != q.Name || (q.Type != "" && (d.Nested || !sameTypeScope(d.Enclosing, q.Type))) ||
			(q.Labels != nil && !supportsArgumentLabels(d)) || !labelsMatch(q.Labels, d.Labels) {
			continue
		}
		e := DefinitionEntry{
			Name: d.Name, Kind: d.Kind, File: d.File, Line: d.Line, EndLine: d.EndLine,
			Enclosing: d.Enclosing, Signature: d.Signature, Test: d.Test,
		}
		e.Source, e.Truncated, e.RemainingLines = sourceRange(sources[d.File], d.Line, d.EndLine, maxLines)
		s.Entries = append(s.Entries, e)
	}
	if q.Labels == nil {
		// 型は Outer.Inner の形でも引ける (extension Outer.Inner と同じ書き方)
		qualified := q.Name
		if q.Type != "" {
			qualified = q.Type + "." + q.Name
		}
		for _, d := range types {
			if !sameTypeScope(d.Scope, qualified) {
				continue
			}
			members := typeMembers(d, defs, types, units, calls, shared)
			e := DefinitionEntry{
				Name: d.Name, Kind: d.Kind, File: d.File, Line: d.Line, EndLine: d.EndLine,
				Enclosing: d.Enclosing, Signature: d.Signature, Test: d.Test, Members: members,
			}
			s.Entries = append(s.Entries, e)
		}
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

// supportsArgumentLabels は括弧つきの問い合わせ (`count()`) を当ててよい定義か。subscript と deinit は property の種類で記録しているので名前で見る。
func supportsArgumentLabels(d DefinitionRecord) bool {
	switch d.Kind {
	case string(tree.KindFunc), string(tree.KindInit):
		return true
	case string(tree.KindProperty):
		return d.Name == "subscript" || d.Name == "deinit"
	default:
		return false
	}
}

func labelsMatch(query, got []string) bool {
	if query == nil {
		return true
	}
	if len(query) != len(got) {
		return false
	}
	for i := range query {
		if query[i] != got[i] {
			return false
		}
	}
	return true
}

func sameTypeScope(got, want string) bool {
	got, want = normalizeTypeScope(got), normalizeTypeScope(want)
	return got == want || strings.HasSuffix(got, "."+want)
}

func normalizeTypeScope(value string) string {
	value = strings.TrimSpace(value)
	value, _, _ = strings.Cut(value, " where ")
	var out strings.Builder
	depth := 0
	for _, r := range value {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				out.WriteRune(r)
			}
		}
	}
	return strings.Join(strings.Fields(out.String()), " ")
}

func typeMembers(d TypeRecord, defs []DefinitionRecord, types []TypeRecord, units []*tree.Unit, calls []tree.Call, shared func(string) bool) []IndexEntry {
	var members []*tree.Unit
	for _, u := range units {
		if u.Kind == tree.KindClosure || u.Line < d.Line || u.EndLine > d.EndLine || !sameTypeScope(u.Enclosing, d.Scope) {
			continue
		}
		members = append(members, u)
	}
	entries := BuildIndex(members, calls, Query{File: d.File, Shared: shared}).Entries
	for _, member := range defs {
		if member.Nested || member.File != d.File || member.Line < d.Line || member.EndLine > d.EndLine || !sameTypeScope(member.Enclosing, d.Scope) {
			continue
		}
		if hasIndexUnit(members, member) {
			continue
		}
		entries = append(entries, syntheticMember(member, shared))
	}
	for _, nested := range types {
		if nested.Nested || nested.File != d.File || nested.Line < d.Line || nested.EndLine > d.EndLine || !sameTypeScope(nested.Enclosing, d.Scope) {
			continue
		}
		entries = append(entries, IndexEntry{
			UnitRef: UnitRef{File: nested.File, Line: nested.Line, Col: nested.Col, Kind: nested.Kind,
				Name: nested.Name, Signature: nested.Kind + " " + nested.Signature, Enclosing: d.Scope,
				Test: nested.Test, Visibility: nested.Visibility},
			Doc: nested.Doc, Shared: shared != nil && shared(nested.File),
		})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].File != entries[j].File {
			return entries[i].File < entries[j].File
		}
		if entries[i].Line != entries[j].Line {
			return entries[i].Line < entries[j].Line
		}
		return entries[i].Col < entries[j].Col
	})
	return entries
}

func hasIndexUnit(units []*tree.Unit, definition DefinitionRecord) bool {
	for _, unit := range units {
		if unit.File == definition.File && unit.Line == definition.Line && unit.Name == definition.Name {
			return true
		}
	}
	return false
}

func syntheticMember(d DefinitionRecord, shared func(string) bool) IndexEntry {
	return IndexEntry{
		UnitRef: UnitRef{File: d.File, Line: d.Line, Col: d.Col, Kind: d.Kind, Name: d.Name,
			Signature: d.Signature, Enclosing: d.Enclosing, Test: d.Test, Visibility: d.Visibility},
		Doc: d.Doc, Shared: shared != nil && shared(d.File),
	}
}

func sourceRange(source []byte, first, last, maxLines int) ([]string, bool, int) {
	lines := strings.Split(string(source), "\n")
	if first < 1 || last < first || first > len(lines) {
		return nil, false, 0
	}
	if last > len(lines) {
		last = len(lines)
	}
	body := lines[first-1 : last]
	if maxLines > 0 && len(body) > maxLines {
		remaining := len(body) - maxLines
		return append([]string(nil), body[:maxLines]...), true, remaining
	}
	return append([]string(nil), body...), false, 0
}

// DefinitionsText writes source declarations and type outlines.
func DefinitionsText(w io.Writer, s DefinitionsSummary, limit int) error {
	return DefinitionsTextMore(w, s, limit, "-limit 0 か -format json で全部")
}

// DefinitionsTextMore は DefinitionsText の、省略したときの案内 (more) を呼び出し側が決める版 (-format を持たないコマンド用)。
func DefinitionsTextMore(w io.Writer, s DefinitionsSummary, limit int, more string) error {
	if s.Matches == 0 {
		fmt.Fprintf(w, "dupscan def: %s の定義なし\n", s.Query)
		return nil
	}
	for i, e := range s.Entries {
		if limit > 0 && i == limit {
			fmt.Fprintf(w, "… 他 %d 件 (%s)\n", s.Matches-limit, more)
			break
		}
		name := e.Signature
		if e.Kind == "struct" || e.Kind == "class" || e.Kind == "enum" || e.Kind == "protocol" || e.Kind == "actor" || e.Kind == "extension" {
			name = e.Kind + " " + e.Signature
		} else if e.Enclosing != "" {
			name = e.Enclosing + "." + name
		}
		fmt.Fprintf(w, "%s:%d-%d %s\n", e.File, e.Line, e.EndLine, name)
		for _, line := range e.Source {
			fmt.Fprintln(w, line)
		}
		if e.Truncated {
			fmt.Fprintf(w, "… 残り %d 行\n", e.RemainingLines)
		}
		for _, member := range e.Members {
			fmt.Fprintf(w, "  %s\n", IndexEntryText(member))
		}
	}
	return nil
}

// ReferenceRecord is one syntax-tree identifier occurrence collected by an adapter.
type ReferenceRecord struct {
	Name      string
	Kind      string
	File      string
	Line      int
	Col       int
	Text      string
	Test      bool
	Enclosing string
	TestOwner string
}

// ReferenceEntry is one refs result.
type ReferenceEntry struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	Col       int    `json:"col"`
	Enclosing string `json:"enclosing,omitempty"`
	Test      bool   `json:"test"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
}

// ReferencesSummary is the JSON shape returned by refs.
type ReferencesSummary struct {
	Query   string           `json:"query"`
	Matches int              `json:"matches"`
	Entries []ReferenceEntry `json:"entries"`
}

// BuildReferences filters exact identifier spellings and sorts prod before tests, then by location.
func BuildReferences(query string, references []ReferenceRecord, units []*tree.Unit, isTest func(string) bool) ReferencesSummary {
	s := ReferencesSummary{Query: query, Entries: []ReferenceEntry{}}
	byFile := map[string][]*tree.Unit{}
	for _, u := range units {
		byFile[u.File] = append(byFile[u.File], u)
	}
	for _, r := range references {
		if r.Name != query {
			continue
		}
		e := ReferenceEntry{File: r.File, Line: r.Line, Col: r.Col, Test: r.Test || isTest(r.File), Kind: r.Kind, Text: r.Text}
		if u := enclosing(byFile[r.File], r.Line); u != nil {
			e.Enclosing = label(ref(u))
			e.Test = e.Test || u.Test
		} else {
			e.Enclosing = r.TestOwner
			if e.Enclosing == "" {
				e.Enclosing = r.Enclosing
			}
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
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Col < b.Col
	})
	s.Matches = len(s.Entries)
	return s
}

// ReferencesText writes one occurrence per line in callers-style form.
func ReferencesText(w io.Writer, s ReferencesSummary, limit int) error {
	return ReferencesTextMore(w, s, limit, "-limit 0 か -format json で全部")
}

// ReferencesTextMore は ReferencesText の、省略したときの案内 (more) を呼び出し側が決める版。
func ReferencesTextMore(w io.Writer, s ReferencesSummary, limit int, more string) error {
	if s.Matches == 0 {
		fmt.Fprintf(w, "dupscan refs: %s の参照なし\n", s.Query)
		return nil
	}
	for i, e := range s.Entries {
		if limit > 0 && i == limit {
			fmt.Fprintf(w, "… 他 %d 件 (%s)\n", s.Matches-limit, more)
			break
		}
		caller := e.Enclosing
		if caller == "" {
			caller = "(単位の外)"
		}
		test := ""
		if e.Test {
			test = " [test]"
		}
		kind := "式の位置"
		if e.Kind == "type_position" {
			kind = "型の位置"
		}
		fmt.Fprintf(w, "%s:%d %s%s [%s] | %s\n", e.File, e.Line, caller, test, kind, e.Text)
	}
	return nil
}
