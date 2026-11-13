package lint

import "ja-writing/oracle"

// ruleSet は本家の規則 40 の ID の集合 (oracle.LintPostRules から作る)。TestRawFindingsMatchPythonRecording・TestRawRecordingRuleIDsMatchRuleSet が、記録に出る規則 ID の集合と同じかを見る。
var ruleSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(oracle.LintPostRules))
	for _, r := range oracle.LintPostRules {
		m[r] = struct{}{}
	}
	return m
}()
