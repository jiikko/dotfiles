package report

import (
	"fmt"
	"io"
	"strings"
)

type groupedSymbolRow struct {
	file, owner, role, text string
	line, col               int
	test                    bool
}

// ReferencesTextGrouped keeps consecutive references under one quoted file heading.
func ReferencesTextGrouped(w io.Writer, s ReferencesSummary, limit int, more string) error {
	if s.Matches == 0 {
		return ReferencesTextMore(w, s, limit, more)
	}
	rows := make([]groupedSymbolRow, 0, len(s.Entries))
	for _, e := range s.Entries {
		role := "式の位置"
		if e.Kind == "type_position" {
			role = "型の位置"
		}
		rows = append(rows, groupedSymbolRow{file: e.File, line: e.Line, col: e.Col, owner: e.Enclosing, test: e.Test, role: role, text: e.Text})
	}
	writeGroupedSymbols(w, rows, s.Matches, limit, more)
	return nil
}

// CallersTextGrouped keeps consecutive calls under one quoted file heading.
func CallersTextGrouped(w io.Writer, s CallersSummary, limit int, more string) error {
	if s.Matches == 0 {
		return CallersTextMore(w, s, limit, more)
	}
	rows := make([]groupedSymbolRow, 0, len(s.Entries))
	for _, e := range s.Entries {
		rows = append(rows, groupedSymbolRow{file: e.File, line: e.Line, col: e.Col, owner: e.Caller, test: e.Test, text: e.Text})
	}
	writeGroupedSymbols(w, rows, s.Matches, limit, more)
	return nil
}

func writeGroupedSymbols(w io.Writer, rows []groupedSymbolRow, matches, limit int, more string) {
	var group fileHeading
	for i, row := range rows {
		if limit > 0 && i == limit {
			fmt.Fprintf(w, "… 他 %d 件 (%s)\n", matches-limit, more)
			break
		}
		group.write(w, row.file, row.test)
		owner := row.owner
		if owner == "" {
			owner = "(単位の外)"
		}
		fmt.Fprintf(w, "  %d:%d %s", row.line, row.col, owner)
		if row.test {
			fmt.Fprint(w, " [test]")
		}
		if row.role != "" {
			fmt.Fprintf(w, " [%s]", row.role)
		}
		fmt.Fprintf(w, " | %s\n", row.text)
	}
}

type fileHeading struct {
	file string
	test bool
	set  bool
}

func (h *fileHeading) write(w io.Writer, file string, test bool) {
	if !h.set || h.file != file || h.test != test {
		fmt.Fprintf(w, "%q\n", file)
		h.file, h.test, h.set = file, test, true
	}
}

// EvidenceTextGrouped keeps evidence rows under file headings within each section.
func EvidenceTextGrouped(w io.Writer, s EvidenceSummary, limit int) error {
	fmt.Fprintf(w, "\n## 語を含む行で呼ばれている名前のうち、repo の定義と同じ名前 (%d 個。型を解決しないので同名の外の関数 (t.Fatalf・append など) も混ざる)\n", len(s.Called))
	for i, c := range s.Called {
		if limit > 0 && i == limit {
			fmt.Fprintf(w, "… 他 %d 個 (-limit 0 で全部)\n", len(s.Called)-limit)
			break
		}
		fmt.Fprintf(w, "%s (%d 行)\n", c.Name, c.Lines)
	}
	sections := []struct {
		title   string
		entries []EvidenceEntry
	}{
		{fmt.Sprintf("doc に語がある単位 (%d 個 / doc の %d 行)", len(s.Doc), s.DocLines), s.Doc},
		{fmt.Sprintf("テストの行 (%d 件)", len(s.Test)), s.Test},
		{fmt.Sprintf("その他のソースの行 (%d 件。コメント・文字列を含む)", len(s.Source)), s.Source},
	}
	for _, sec := range sections {
		fmt.Fprintf(w, "\n## %s\n", sec.title)
		var group fileHeading
		for i, e := range sec.entries {
			if limit > 0 && i == limit {
				fmt.Fprintf(w, "… 他 %d 件 (-limit 0 で全部)\n", len(sec.entries)-limit)
				break
			}
			group.write(w, e.File, false)
			unit := shorten(e.Unit, evidenceUnitRunes)
			if unit == "" {
				unit = "(単位の外)"
			}
			fmt.Fprintf(w, "  %d %s | %s", e.Line, unit, e.Text)
			if e.Hits > 1 {
				fmt.Fprintf(w, "  (doc の %d 行に語がある)", e.Hits)
			}
			if len(e.Calls) > 0 {
				fmt.Fprintf(w, "  → 呼び出し: %s", strings.Join(e.Calls, ", "))
			}
			fmt.Fprintln(w)
		}
	}
	return nil
}
