package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// staleDetails は report -v の「効いていない除外」の下に出る「詳しく:」の行。
func staleDetails(t *testing.T, root string) []string {
	t.Helper()
	rc, out, errs := runReport(t, "-v", root)
	if rc != 0 || errs != "" || !strings.Contains(out, "効いていない除外: ") {
		t.Fatalf("効いていない除外を 1 つ期待: rc=%d out=%q stderr=%q", rc, out, errs)
	}
	var got []string
	for _, line := range strings.Split(out, "\n") {
		if d, ok := strings.CutPrefix(line, "  詳しく: "); ok {
			got = append(got, d)
		}
	}
	return got
}

func pinHashOf(t *testing.T, rule map[string]any, file string) string {
	t.Helper()
	for _, p := range rule["group"].([]any) {
		if s := p.(string); strings.HasPrefix(s, file+"|") {
			return s[strings.LastIndex(s, "@")+1:]
		}
	}
	t.Fatalf("%s の釘が無い: %v", file, rule)
	return ""
}

// 残った単位が関数なので、記録がこの関数か消えたクロージャかは決められず「本体が変わった」とは言わない (TestStaleDetailDoesNotClaimChangedBodyWhenClosureMayHaveGone)。
func TestStaleDetailShowsRecordedAndCurrentHashOfChangedBody(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA, "Sources/B.swift": helperB})
	rule := groupRule(t, root)
	writeConfig(t, root, rule)
	writeFile(t, root, "Sources/A.swift", strings.ReplaceAll(helperA, `"conn"`, `"other"`))
	got := staleDetails(t, root)
	old := pinHashOf(t, rule, "Sources/A.swift")
	if len(got) != 1 || !strings.HasPrefix(got[0], "同じ鍵の単位の本体が記録と違う: Sources/A.swift|A|location(path: String) -> StorageLocation (記録 @"+old+" → 今 @") ||
		strings.Contains(got[0], "今 @"+old) {
		t.Fatalf("変わった釘 1 つだけを、記録と今の違う hash で出す: %q", got)
	}
}

func TestStaleDetailSuggestsSameBodyForRenamedUnit(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA, "Sources/B.swift": helperB})
	writeConfig(t, root, groupRule(t, root))
	writeFile(t, root, "Sources/B.swift", strings.Replace(helperB, "func makeLocation(", "func makeStorageLocation(", 1))
	got := staleDetails(t, root)
	if len(got) != 1 || !strings.Contains(got[0], "指す単位が無い: Sources/B.swift|B|makeLocation(_ p: String) -> StorageLocation") ||
		!strings.Contains(got[0], "同じ本体の単位 (改名・移動の候補): Sources/B.swift|B|makeStorageLocation(") {
		t.Fatalf("消えた釘に、同じ本体の改名先を候補として出す: %q", got)
	}
}

func TestStaleDetailListsMemberAddedOutsidePins(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA, "Sources/B.swift": helperB})
	writeConfig(t, root, groupRule(t, root))
	writeFile(t, root, "Sources/C.swift", helperC)
	got := staleDetails(t, root)
	if len(got) != 1 || !strings.Contains(got[0], "釘の外に 1 個増えた") || !strings.Contains(got[0], "Sources/C.swift|C|otherLocation(") {
		t.Fatalf("釘が全部今のまま在る組の除外には、増えたメンバーを出す: %q", got)
	}
}

func TestStaleDetailCountsEveryAddedMemberEvenWithSamePin(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/R.swift": siblingBody(2)})
	writeConfig(t, root, groupRule(t, root))
	writeFile(t, root, "Sources/S.swift", strings.Replace(siblingBody(2), "func register(", "func enroll(", 1))
	got := staleDetails(t, root)
	if len(got) != 1 || !strings.Contains(got[0], "釘の外に 2 個増えた") {
		t.Fatalf("同じ釘になる 2 つのクロージャも 2 個と数える: %q", got)
	}
}

func TestStaleDetailDoesNotClaimChangedBodyWhenClosureMayHaveGone(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/R.swift": siblingBody(2)})
	writeConfig(t, root, groupRule(t, root))
	writeFile(t, root, "Sources/R.swift", "func register(_ r: Registry) {}\n")
	got := staleDetails(t, root)
	if len(got) != 1 || !strings.Contains(got[0], "クロージャが消えた形と区別しない") {
		t.Fatalf("クロージャの釘と同じ鍵の外側の関数しか無いときは、本体が変わったと断定しない: %q", got)
	}
}

func TestStaleDetailIsVerboseTextOnly(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA, "Sources/B.swift": helperB})
	writeConfig(t, root, groupRule(t, root))
	writeFile(t, root, "Sources/C.swift", helperC)
	if _, out, _ := runReport(t, root); strings.Contains(out, "詳しく: ") {
		t.Errorf("-v の無い text には出さない: %q", out)
	}
	_, out, _ := runReport(t, "-format", "json", root)
	var s map[string]any
	if err := json.Unmarshal([]byte(out), &s); err != nil || strings.Contains(out, "釘の外に") {
		t.Errorf("json の形を変えない (詳しくは text の一覧だけ): err=%v", err)
	}
}

func TestStaleDetailReportsFewerUnitsThanRecordedForSamePin(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/R.swift": siblingBody(2)})
	writeConfig(t, root, groupRule(t, root))
	writeFile(t, root, "Sources/R.swift", siblingBody(1))
	got := staleDetails(t, root)
	if len(got) != 1 || !strings.Contains(got[0], "同じ釘の単位の数が記録と違う") || !strings.Contains(got[0], "記録 2 個 → 今 1 個") {
		t.Fatalf("同じ釘の単位が減った組の除外には、記録と今の数を出す: %q", got)
	}
}

func TestStaleDetailShowsCurrentHashesWhenKeyIsAmbiguous(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/R.swift": siblingBody(2)})
	writeConfig(t, root, groupRule(t, root))
	writeFile(t, root, "Sources/R.swift", strings.ReplaceAll(siblingBody(2), `audit("x"`, `audit("y"`))
	got := staleDetails(t, root)
	if len(got) != 1 || !strings.Contains(got[0], "どれが記録の単位か決められない") || !strings.Contains(got[0], "今 @") {
		t.Fatalf("同じ鍵の単位が複数でも、今の hash を比べる材料として出す: %q", got)
	}
}
