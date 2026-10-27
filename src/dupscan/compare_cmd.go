package main

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"dupscan/internal/group"
	"dupscan/internal/norm"
	"dupscan/internal/report"
)

type comparisonSummary struct {
	Schema          string                  `json:"schema"`
	Budget          string                  `json:"budget"`
	Left            report.UnitRef          `json:"left"`
	Right           report.UnitRef          `json:"right"`
	Total           int                     `json:"total_differences"`
	Returned        int                     `json:"returned_differences"`
	Truncated       bool                    `json:"truncated"`
	ParseErrorFiles int                     `json:"parse_error_files"`
	Differences     []report.FormDifference `json:"differences"`
	Limitations     string                  `json:"limitations"`
}

func writeComparison(stdout, stderr io.Writer, left, right *group.Entry, got scanned, limit int, format string) int {
	fail := func(err error) int { fmt.Fprintln(stderr, "dupscan explain -compare:", err); return 2 }
	if limit < 1 || limit > 50 {
		return fail(errors.New("limit must be 1..50"))
	}
	if format != "text" && format != "json" {
		return fail(errors.New("compare supports text/json only"))
	}
	if filepath.Ext(left.Unit.File) != filepath.Ext(right.Unit.File) {
		return fail(errors.New("compare requires the same language"))
	}
	for _, e := range []*group.Entry{left, right} {
		if e.Unit.Unreadable {
			return fail(fmt.Errorf("%s contains unreadable syntax", e.Unit.File))
		}
		if len(got.contents[e.Unit.File]) > report.CompareFormBytes {
			return fail(fmt.Errorf("selected source file exceeds %d bytes", report.CompareFormBytes))
		}
	}
	a, err := norm.L1View(left.Unit, report.CompareFormBytes)
	if err != nil {
		return fail(err)
	}
	b, err := norm.L1View(right.Unit, report.CompareFormBytes)
	if err != nil {
		return fail(err)
	}
	differences, total, err := report.CompareForms(a, b, limit)
	if err != nil {
		return fail(err)
	}
	s := comparisonSummary{Schema: "dupscan.explain.compare.v1", Budget: fmt.Sprintf("differences<=%d; selected_source_file_bytes<=%d; L1_bytes<=%d; depth<=256; snippet_runes<=160; literal_tokens_per_side<=8", limit, report.CompareFormBytes, report.CompareFormBytes), Left: report.BuildExplain(left, group.Options{}).Target, Right: report.BuildExplain(right, group.Options{}).Target, Total: total, Returned: len(differences), Truncated: total > len(differences), ParseErrorFiles: len(got.errFiles), Differences: differences, Limitations: "L1 ordered AST paths, not source positions; property initializers unresolved; local names normalized per unit; no edit alignment; reuse=not_proven"}
	for _, d := range differences {
		s.Truncated = s.Truncated || d.SnippetTruncated
	}
	if format == "json" {
		err = report.WriteJSON(stdout, s)
	} else {
		_, err = fmt.Fprintf(stdout, "dupscan explain -compare: %q:%d vs %q:%d differences=%d returned=%d truncated=%t parse_error_files=%d\n%s\n%s\n", s.Left.File, s.Left.Line, s.Right.File, s.Right.Line, s.Total, s.Returned, s.Truncated, s.ParseErrorFiles, s.Budget, s.Limitations)
		for _, d := range differences {
			if err != nil {
				break
			}
			_, err = fmt.Fprintf(stdout, "%s %s\n  left: %s\n  right: %s\n  left literal tokens: %q\n  right literal tokens: %q\n", d.Path, d.Kind, d.Left, d.Right, d.LeftLiterals, d.RightLiterals)
		}
	}
	if err != nil {
		return fail(err)
	}
	return 0
}
