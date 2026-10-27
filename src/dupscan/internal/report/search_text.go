package report

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// SearchCandidateText keeps evidence separate from whether the helper can be reused.
func SearchCandidateText(candidate SearchCandidate) (string, bool) {
	e := candidate.Entry
	var b strings.Builder
	fmt.Fprintf(&b, "%q:%d:%d %q score=%d reuse=not_proven\n", e.File, e.Line, e.Col, label(e.UnitRef), candidate.Score)
	fmt.Fprintf(&b, "  visibility=%q shared=%t test=%t returns=%q params=%q\n", e.Visibility, e.Shared, e.Test, e.ReturnType, e.ParamTypes)
	for _, evidence := range candidate.Matched {
		fmt.Fprintf(&b, "  matched %q fields=%s components=%q\n", evidence.Term, strings.Join(evidence.Fields, ","), evidence.Components)
	}
	if len(candidate.Missing) > 0 {
		fmt.Fprintf(&b, "  missing=%q\n", candidate.Missing)
	}
	truncated := utf8.RuneCountInString(e.Doc) > 160
	if e.Doc != "" {
		fmt.Fprintf(&b, "  doc=%q doc_truncated=%t\n", shorten(e.Doc, 160), truncated)
	}
	return strings.ToValidUTF8(b.String(), "�"), truncated
}
