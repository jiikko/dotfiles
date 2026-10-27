package main

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"dupscan/internal/group"
	"dupscan/internal/norm"
	"dupscan/internal/report"
	"dupscan/internal/tree"
)

type similarUnit struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
}

type similarMatch struct {
	Unit         similarUnit `json:"unit"`
	Score        float64     `json:"score"`
	SharedWeight int         `json:"shared_weight"`
}

type similarSummary struct {
	Target          similarUnit    `json:"target"`
	Mode            string         `json:"mode"`
	Threshold       float64        `json:"threshold"`
	MinSubtreeNodes int            `json:"min_subtree_nodes"`
	Compared        int            `json:"compared"`
	Skipped         int            `json:"skipped"`
	ParseErrorFiles int            `json:"parse_error_files"`
	Total           int            `json:"total"`
	Returned        int            `json:"returned"`
	Matches         []similarMatch `json:"matches"`
	Limitations     string         `json:"limitations"`
}

func writeSimilar(stdout, stderr io.Writer, target *group.Entry, entries []*group.Entry, got scanned, threshold float64, limit int, format string) int {
	if format != "text" && format != "json" {
		fmt.Fprintln(stderr, "dupscan near: similarはtext/jsonのみ")
		return 2
	}
	at := func(u *tree.Unit) similarUnit { return similarUnit{File: u.File, Line: u.Line, Col: u.Col} }
	s := similarSummary{Target: at(target.Unit), Mode: "experimental_l1_subtrees", Threshold: threshold, MinSubtreeNodes: 3, ParseErrorFiles: len(got.errFiles), Matches: []similarMatch{}, Limitations: "reuse=not_proven; whole-unit overlap, not partial-clone coverage; return type is not compared; unit-wide local numbering; nested subtree weights; clone exclusions are not applied"}
	ancestors := map[string]string{}
	for _, e := range entries {
		ancestors[e.Unit.Key] = e.Unit.Owner
	}
	contains := func(child, parent string) bool {
		seen := map[string]bool{}
		for current := child; current != "" && !seen[current]; {
			if current == parent {
				return true
			}
			seen[current] = true
			owner := ancestors[current]
			if owner == "" {
				// Swift named units encode their containing unit in Key, including closures.
				for cut := strings.LastIndex(current, "/"); cut >= 0; cut = strings.LastIndex(current[:cut], "/") {
					if _, exists := ancestors[current[:cut]]; exists {
						owner = current[:cut]
						break
					}
				}
			}
			current = owner
		}
		return false
	}
	bag := norm.Subtrees(target.Unit, 3)
	if len(bag) == 0 {
		s.Limitations += "; target has no subtree with 3 nodes: no useful comparison"
	}
	for _, e := range entries {
		if e == target {
			continue
		}
		if e.Unit.Unreadable || e.Unit.Body == nil || filepath.Ext(e.Unit.File) != filepath.Ext(target.Unit.File) || contains(e.Unit.Key, target.Unit.Key) || contains(target.Unit.Key, e.Unit.Key) {
			s.Skipped++
			continue
		}
		s.Compared++
		score, shared := bag.Dice(norm.Subtrees(e.Unit, 3))
		if shared > 0 && score >= threshold {
			s.Matches = append(s.Matches, similarMatch{Unit: at(e.Unit), Score: score, SharedWeight: shared})
		}
	}
	sort.Slice(s.Matches, func(i, j int) bool {
		a, b := s.Matches[i], s.Matches[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Unit.File != b.Unit.File {
			return a.Unit.File < b.Unit.File
		}
		if a.Unit.Line != b.Unit.Line {
			return a.Unit.Line < b.Unit.Line
		}
		return a.Unit.Col < b.Unit.Col
	})
	s.Total = len(s.Matches)
	if len(s.Matches) > limit {
		s.Matches = s.Matches[:limit]
	}
	s.Returned = len(s.Matches)
	var err error
	if format == "json" {
		err = report.WriteJSON(stdout, s)
	} else {
		_, err = fmt.Fprintf(stdout, "dupscan near -similar: %q:%d compared=%d skipped=%d parse_error_files=%d candidates=%d returned=%d threshold=%.3f min_subtree_nodes=3\n%s\n", s.Target.File, s.Target.Line, s.Compared, s.Skipped, s.ParseErrorFiles, s.Total, s.Returned, s.Threshold, s.Limitations)
		for _, m := range s.Matches {
			if err != nil {
				break
			}
			_, err = fmt.Fprintf(stdout, "%.4f shared_weight=%d %q:%d:%d\n", m.Score, m.SharedWeight, m.Unit.File, m.Unit.Line, m.Unit.Col)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "dupscan near: %v\n", err)
		return 2
	}
	return 0
}
