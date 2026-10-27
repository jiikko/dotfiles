package report

import (
	"strings"
	"testing"
	"unicode/utf8"

	"dupscan/internal/norm"
)

func TestCompareFormsPositionsAndMissingChildren(t *testing.T) {
	a := []*norm.FormNode{{Header: "(body", Children: []*norm.FormNode{{Header: "(literal \"direct\""}, {Header: "(member \"namespace\""}}}}
	b := []*norm.FormNode{{Header: "(body", Children: []*norm.FormNode{{Header: "(name \"connectionID\""}}}}
	differences, total, err := CompareForms(a, b, 20)
	if err != nil || total != 2 || len(differences) != 2 || differences[0].Path != "$/0/0" || differences[0].Left != `(literal "direct")` || differences[0].Right != `(name "connectionID")` || differences[1].Kind != "removed" || differences[1].Right != "" {
		t.Fatalf("unresolved/missing node: %+v %d %v", differences, total, err)
	}
	differences, total, err = CompareForms(a, b, 1)
	if err != nil || total != 2 || len(differences) != 1 {
		t.Fatalf("limit lost total: %+v %d %v", differences, total, err)
	}
	differences, total, err = CompareForms(a, a, 20)
	if err != nil || total != 0 || len(differences) != 0 {
		t.Fatalf("same: %+v %d %v", differences, total, err)
	}
	differences, total, err = CompareForms(b, a, 20)
	if err != nil || total != 2 || differences[1].Kind != "added" || differences[1].Left != "" {
		t.Fatalf("added: %+v %d %v", differences, total, err)
	}
}

func TestCompareFormsTruncatesUTF8AndRejectsInvalidLimit(t *testing.T) {
	large := []*norm.FormNode{{Header: `(literal "` + strings.Repeat("猫", 200) + `"`, Literal: true}}
	small := []*norm.FormNode{{Header: `(name "unresolved"`}}
	differences, _, err := CompareForms(large, small, 20)
	if err != nil || !differences[0].SnippetTruncated || !utf8.ValidString(differences[0].Left) || utf8.RuneCountInString(differences[0].Left) != 160 {
		t.Fatalf("snippet: %+v %v", differences, err)
	}
	tokens := []*norm.FormNode{{Header: "(body"}}
	for range 9 {
		tokens[0].Children = append(tokens[0].Children, &norm.FormNode{Header: `(literal "x"`, Literal: true})
	}
	differences, _, err = CompareForms(tokens, small, 20)
	if err != nil || len(differences[0].LeftLiterals) != 8 || !differences[0].SnippetTruncated {
		t.Fatalf("token budget: %+v %v", differences, err)
	}
	for _, limit := range []int{0, 51} {
		if _, _, err := CompareForms(large, small, limit); err == nil {
			t.Fatalf("invalid limit %d", limit)
		}
	}
}
