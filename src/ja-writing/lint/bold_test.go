package lint

import (
	"testing"

	"ja-writing/visibility"
)

// TestBoldFindingsMatchUpstream は候補の文面 (message・snippet・重さ) が本家の lint_text と同じかを見る。
// 期待値は本家 (db78b9bc) の yomiyasu_lint.lint_text に同じ文を通して取った値 (2026-11-13)。太字の位置と直し方は internal/bold の記録の試験が見る。
func TestBoldFindingsMatchUpstream(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []BoldFinding
	}{
		{"これは**「重要」**です。\n", []BoldFinding{{
			Rule: "bold_not_rendered", Line: 1, Severity: "error",
			Message: "太字の記号（**）について、太字として表示されない可能性のある書き方が見つかりました。GitHub などで太字にならず ** がそのまま表示されることがあります。直し方: かっこの内側だけを太字にする。",
			Snippet: "これは**「重要」**です。 → これは「**重要**」です。",
		}}},
		{"a**b*c**d\n", []BoldFinding{}},
	} {
		a, err := visibility.AnalyzeMarkdown(tc.text, true)
		if err != nil {
			t.Fatal(err)
		}
		got, err := BoldFindings(tc.text, a)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(tc.want) {
			t.Fatalf("%q: 候補 %d 件 (本家 %d 件): %+v", tc.text, len(got), len(tc.want), got)
		}
		for k := range got {
			if got[k] != tc.want[k] {
				t.Errorf("%q: Go %+v / 本家 %+v", tc.text, got[k], tc.want[k])
			}
		}
	}
}
