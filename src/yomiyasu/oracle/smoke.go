package oracle

import (
	"context"
	"fmt"
)

// smokeCalls・smokeCLIs は、関数と CLI の driver が動くことの確認に使う最小の入力 (1074 以降が自分の入力を足すときの見本)。
// 本家が投げる例外 (使えない値) も記録の対象にする。
func smokeCalls() []Call {
	return []Call{
		{Module: "markdown_visibility", Function: "normalize_doc_type", Args: []any{" SLIDES "}},
		{Module: "markdown_visibility", Function: "normalize_doc_type", Args: []any{"unknown"}},
		{Module: "markdown_visibility", Function: "unicode_whitespace", Args: []any{"\u3000"}},
		{Module: "markdown_visibility", Function: "context_label", Args: []any{"pr", "expert"}},
	}
}

func smokeCLIs() []CLISpec {
	return []CLISpec{
		{Script: "yomiyasu_lint.py", Args: []string{"--version"}},
		{Script: "yomiyasu_diff.py", Args: []string{"--version"}},
		{Script: "yomiyasu_check.py", Args: []string{"--version"}},
		{Script: "yomiyasu_hook.py", Args: []string{"--version"}},
		{Script: "yomiyasu_lint.py", Args: []string{"--json", "-"}, Stdin: "これは**太字**です。\n"},
		{Script: "yomiyasu_lint.py", Args: []string{"--doc-type", "nonexistent"}, Stdin: "あ\n"},
	}
}

// RecordSmoke は smokeCalls と smokeCLIs を本家で動かして記録にする。
func (r Runner) RecordSmoke(ctx context.Context, header Header) (SmokeGolden, error) {
	calls := smokeCalls()
	results, err := r.CallFunctions(ctx, calls)
	if err != nil {
		return SmokeGolden{}, err
	}
	g := SmokeGolden{Header: header}
	for i, c := range calls {
		g.Functions = append(g.Functions, SmokeCall{Call: c, Result: results[i]})
	}
	for _, spec := range smokeCLIs() {
		res, err := r.RunCLI(ctx, spec)
		if err != nil {
			return SmokeGolden{}, fmt.Errorf("CLI %s %v: %w", spec.Script, spec.Args, err)
		}
		g.CLIs = append(g.CLIs, SmokeCLI{Spec: spec, Result: res})
	}
	return g, nil
}
