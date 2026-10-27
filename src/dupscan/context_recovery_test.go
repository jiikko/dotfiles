package main

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestContextQualifiedTypeFallbackRetainsPrimaryAndScope(t *testing.T) {
	root := repoWith(t, map[string]string{
		"Scoped/Sources/FIFO.swift": `struct BoundedFIFOArray<T> {
 mutating func append(_ value: T) {
  consume(value)
 }
}
struct Noise { mutating func append(_ value: Int) {} }
func noise(_ value: inout Noise) {
` + strings.Repeat(" value.append(0)\n", 25) + `}
struct Unique { func compute() {} }
func single(_ value: Unique) { value.compute() }
`,
		"Scoped/Tests/FIFOTests.swift": `func scenarios() {
 var values = BoundedFIFOArray<Int>()
 values.append(1)
 values.append(2)
 values.append(3)
 var empty = BoundedFIFOArray<Int>()
 empty.append(4)
 empty.append(5)
}
`,
		"Outside.swift": "// OUTSIDE_SCOPE BoundedFIFOArray\n",
	})
	scope, err := absPath(filepath.Join(root, "Scoped"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, query, primary string
		flags                []string
		wantFallback         bool
	}{
		{"qualified_truncated", "BoundedFIFOArray.append", "refs", nil, true},
		{"qualified_generic", "BoundedFIFOArray<Int>.append", "refs", nil, true},
		{"refs_only", "BoundedFIFOArray.append", "refs", []string{"-sections", "refs", "-limit", "1"}, true},
		{"summary", "BoundedFIFOArray.append", "context", []string{"-sections", "summary"}, true},
		{"callers_only", "BoundedFIFOArray.append", "callers", []string{"-sections", "callers", "-limit", "1"}, true},
		{"complete", "BoundedFIFOArray.append", "callers", []string{"-limit", "0"}, false},
		{"unique_complete", "Unique.compute", "callers", nil, false},
		{"bare", "append", "refs", nil, false},
		{"body_only_omitted", "BoundedFIFOArray.append", "context", []string{"-sections", "definition", "-max-lines", "1"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			args := append(append([]string{}, c.flags...), c.query, scope)
			rc, out, errs := contextCmd(t, args...)
			if rc != 0 || errs != "" {
				t.Fatalf("context: rc=%d stderr=%q out=%q", rc, errs, out)
			}
			actions := []nextAction{}
			for _, line := range strings.Split(out, "\n") {
				if raw, ok := strings.CutPrefix(line, "next_action: "); ok {
					var action nextAction
					if err := json.Unmarshal([]byte(raw), &action); err != nil {
						t.Fatal(err)
					}
					actions = append(actions, action)
				}
			}
			want := 1
			if c.wantFallback {
				want = 2
			}
			if len(actions) != want || actions[0].Kind != c.primary {
				t.Fatalf("primary order/fallback applicability: %+v out=%q", actions, out)
			}
			if c.name == "summary" {
				rc, expanded, errs := runReport(t, actions[0].Argv[1:]...)
				if rc != 0 || errs != "" || !strings.Contains(expanded, "consume(value)") {
					t.Fatalf("summary body expansion lost: rc=%d out=%q stderr=%q", rc, expanded, errs)
				}
			}
			if !c.wantFallback {
				return
			}
			action := actions[1]
			if action.Kind != "evidence" || !strings.Contains(action.Reason, "型未解決") || !strings.Contains(action.Reason, "同名別型") || !reflect.DeepEqual(action.Argv, []string{"dupscan", "evidence", "--", "BoundedFIFOArray", scope}) {
				t.Fatalf("unproven scoped recovery: %+v", action)
			}
			rc, recovered, errs := runReport(t, action.Argv[1:]...)
			if rc != 0 || errs != "" || !strings.Contains(recovered, "Tests/FIFOTests.swift") || !strings.Contains(recovered, "var values = BoundedFIFOArray<Int>()") || !strings.Contains(recovered, "var empty = BoundedFIFOArray<Int>()") || strings.Contains(recovered, "OUTSIDE_SCOPE") {
				t.Fatalf("type construction recovery: rc=%d out=%q stderr=%q", rc, recovered, errs)
			}
		})
	}
}
