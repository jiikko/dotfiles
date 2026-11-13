package lint

// G3 太字 (bold_not_rendered。本家 yomiyasu_lint.py の 2076-2836 と 5577-5584)。照合の中身は internal/bold、候補の形は bold.go の BoldFindings。

// g03Bold は行のループの前に 1 回 (本家の「2.5 太字が表示されるか」)。
func (s *lintState) g03Bold() error {
	found, err := BoldFindings(s.text, s.an)
	if err != nil {
		return err
	}
	for _, f := range found {
		s.add(newFinding(f.Rule, f.Line, f.Severity, f.Message, f.Snippet))
	}
	return nil
}
