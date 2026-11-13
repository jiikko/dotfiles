package lint

import (
	"ja-writing/internal/bold"
	"ja-writing/visibility"
)

// BoldFinding は規則 bold_not_rendered の候補 1 つ (本家 lint_text の 5577-5584 行の dict と同じ欄)。
// 候補の型は lint の骨組み (1075 の T4) が決めるので、骨組みはこの欄を自分の型へ写す。
type BoldFinding struct {
	Rule     string
	Line     int
	Severity string
	Message  string
	Snippet  string
}

const (
	boldRule    = "bold_not_rendered"
	boldMessage = "太字の記号（**）について、太字として表示されない可能性のある書き方が見つかりました。GitHub などで太字にならず ** がそのまま表示されることがあります。直し方: "
)

// BoldFindings は lint_text の「2.5 太字が表示されるか」。analysis は lint_text が作ったもの (skip_frontmatter=True)。
func BoldFindings(text string, a *visibility.Analysis) ([]BoldFinding, error) {
	problems, err := bold.ProblemsWithAnalysis(text, a)
	if err != nil {
		return nil, err
	}
	out := make([]BoldFinding, 0, len(problems))
	for _, p := range problems {
		snippet := p.Found
		if p.Suggest != "" {
			snippet = p.Found + " → " + p.Suggest
		}
		out = append(out, BoldFinding{Rule: boldRule, Line: p.Line, Severity: "error", Message: boldMessage + p.How + "。", Snippet: snippet})
	}
	return out, nil
}
