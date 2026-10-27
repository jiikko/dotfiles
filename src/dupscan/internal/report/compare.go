package report

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"dupscan/internal/norm"
)

const CompareFormBytes = 1 << 20

// FormDifference identifies an ordered normalized-AST path, not a source span.
type FormDifference struct {
	Path             string   `json:"path"`
	Kind             string   `json:"kind"`
	Left             string   `json:"left"`
	Right            string   `json:"right"`
	LeftLiterals     []string `json:"left_literal_tokens"`
	RightLiterals    []string `json:"right_literal_tokens"`
	SnippetTruncated bool     `json:"snippet_truncated"`
}

// CompareForms compares generated L1 views by child position, without resolving values.
// A changed node header reports its subtree once; it does not infer an edit alignment.
func CompareForms(left, right []*norm.FormNode, limit int) ([]FormDifference, int, error) {
	if limit < 1 || limit > 50 {
		return nil, 0, errors.New("compare limit must be 1..50")
	}
	differences := []FormDifference{}
	total := 0
	var walk func([]*norm.FormNode, []*norm.FormNode, string)
	walk = func(xs, ys []*norm.FormNode, path string) {
		for i := range max(len(xs), len(ys)) {
			var x, y *norm.FormNode
			if i < len(xs) {
				x = xs[i]
			}
			if i < len(ys) {
				y = ys[i]
			}
			p := fmt.Sprintf("%s/%d", path, i)
			if x != nil && y != nil && x.Header == y.Header {
				walk(x.Children, y.Children, p)
				continue
			}
			total++
			if len(differences) == limit {
				continue
			}
			d := FormDifference{Path: p, Kind: "changed", LeftLiterals: []string{}, RightLiterals: []string{}}
			var leftCut, rightCut bool
			if x == nil {
				d.Kind = "added"
			} else {
				d.Left, d.LeftLiterals, leftCut = formEvidence(x)
			}
			if y == nil {
				d.Kind = "removed"
			} else {
				d.Right, d.RightLiterals, rightCut = formEvidence(y)
			}
			d.SnippetTruncated = leftCut || rightCut
			if utf8.RuneCountInString(d.Left) > 160 {
				d.Left = shorten(d.Left, 159)
				d.SnippetTruncated = true
			}
			if utf8.RuneCountInString(d.Right) > 160 {
				d.Right = shorten(d.Right, 159)
				d.SnippetTruncated = true
			}
			differences = append(differences, d)
		}
	}
	walk(left, right, "$")
	return differences, total, nil
}

func formEvidence(node *norm.FormNode) (string, []string, bool) {
	literals := []string{}
	truncated := false
	var text strings.Builder
	var write func(*norm.FormNode)
	write = func(n *norm.FormNode) {
		text.WriteString(n.Header)
		if n.Literal {
			if len(literals) == 8 {
				truncated = true
			} else {
				token := n.Header + ")"
				if utf8.RuneCountInString(token) > 160 {
					token = shorten(token, 159)
					truncated = true
				}
				literals = append(literals, token)
			}
		}
		for _, child := range n.Children {
			write(child)
		}
		text.WriteByte(')')
	}
	write(node)
	return text.String(), literals, truncated
}
