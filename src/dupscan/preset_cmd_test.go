package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// 除外のプリセット l2-single-call。定数だけが違う 1 行の委譲の L2 の組は外し、それ以外の写しは止める。

const delegateA = `struct ATests {
    private func makeTemporaryFile(contents: Data) throws -> URL {
        try TestScratchDirectory.makeFile(prefix: "a-tests", contents: contents)
    }
}
`

const delegateB = `struct BTests {
    private func makeTemporaryFile(contents: Data) throws -> URL {
        try TestScratchDirectory.makeFile(prefix: "b-tests", contents: contents)
    }
}
`

var presetRule = map[string]any{"preset": "l2-single-call", "kind": "false-positive", "reason": "定数だけが違う 1 行の委譲"}

// presetRepo は files と、プリセットを書いた設定を commit した repo を作る。
func presetRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := repoWith(t, files)
	writeConfig(t, root, presetRule)
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	return root
}

func TestPresetPassesDelegationWithOtherConstantButReportsIt(t *testing.T) {
	root := presetRepo(t, map[string]string{"Tests/A.swift": delegateA})
	writeFile(t, root, "Tests/B.swift", delegateB)
	rc, out, errs := runGateCmd(t, "-base", "HEAD", "-fail-new-exclusions", root)
	if rc != 0 || !strings.Contains(out, "プリセットで通した変わった単位 1") {
		t.Errorf("定数だけが違う委譲は通し、通した数を出す: rc=%d out=%q err=%q", rc, out, errs)
	}
	// 同じ木でプリセットを外すと止まる (プリセットが通している)。
	if rc, out, _ := runGateCmd(t, "-base", "HEAD", "-no-exclude", root); rc != 1 {
		t.Errorf("プリセットが無ければ違反のはず: rc=%d out=%q", rc, out)
	}
	if _, out, _ := runReport(t, root); !strings.Contains(out, "重複なし") || strings.Contains(out, "効いていない除外") {
		t.Errorf("report でも組を出さず、効いているプリセットを効いていないと言わない: %q", out)
	}
	if rc, out, _ := near(t, root, filepath.Join(root, "Tests/B.swift")+":makeTemporaryFile"); rc != 0 || !strings.Contains(out, "近いものはない") {
		t.Errorf("near でも外す: rc=%d %q", rc, out)
	}
}

func TestPresetKeepsCopiesThatAreNotOneCallDelegations(t *testing.T) {
	cases := map[string]string{
		"2 文": `struct BTests {
    private func makeTemporaryFile(contents: Data) throws -> URL {
        let prefix = "b-tests"
        return try TestScratchDirectory.makeFile(prefix: prefix, contents: contents)
    }
}
`,
		"引数に計算": `struct BTests {
    private func makeTemporaryFile(contents: Data) throws -> URL {
        try TestScratchDirectory.makeFile(prefix: "b-" + "tests", contents: contents)
    }
}
`,
		"引数に引数のある呼び出し": `struct BTests {
    private func makeTemporaryFile(contents: Data) throws -> URL {
        try TestScratchDirectory.makeFile(prefix: name("b"), contents: contents)
    }
}
`,
	}
	base := `struct ATests {
    private func makeTemporaryFile(contents: Data) throws -> URL {
        let prefix = "a-tests"
        return try TestScratchDirectory.makeFile(prefix: prefix, contents: contents)
    }
}
`
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			a := base
			if name != "2 文" {
				a = strings.Replace(b, "BTests", "ATests", 1)
				a = strings.Replace(a, `"b`, `"a`, 1)
			}
			root := presetRepo(t, map[string]string{"Tests/A.swift": a})
			writeFile(t, root, "Tests/B.swift", b)
			if rc, out, errs := runGateCmd(t, "-base", "HEAD", root); rc != 1 {
				t.Errorf("1 つの呼び出しの委譲でない写しは止める: rc=%d out=%q err=%q", rc, out, errs)
			}
		})
	}
}

// L1 では同じ出どころから来たので違反にならない組が、L2 で初めて違反になる写しは、プリセットが消さない (メンバーの L1 が同じなら効かせない)。
// base: A と B は定数まで同じ写し (名前を渡す)・C は文字列を渡す (L2 でも A・B と違う)。変更: A と B に同じ修正を入れ、文字列を渡す形にする。
// L1 の組 {A,B} は出どころが同じなので違反にならず、L2 の組 {A,B,C} で初めて違反になる。
func TestPresetDoesNotHideCopyFirstSeenAtL2(t *testing.T) {
	body := func(typ, arg string) string {
		return "struct " + typ + " {\n    private func makeTemporaryFile(contents: Data) throws -> URL {\n        try TestScratchDirectory.makeFile(prefix: " + arg + ", contents: contents)\n    }\n}\n"
	}
	root := presetRepo(t, map[string]string{
		"Tests/A.swift": body("ATests", "prefixName"),
		"Tests/B.swift": body("BTests", "prefixName"),
		"Tests/C.swift": body("CTests", `"c-tests"`),
	})
	writeFile(t, root, "Tests/A.swift", body("ATests", `"z-tests"`))
	writeFile(t, root, "Tests/B.swift", body("BTests", `"z-tests"`))
	if rc, out, _ := runGateCmd(t, "-base", "HEAD", "-no-exclude", root); rc != 1 {
		t.Fatalf("前提: プリセットが無ければ L2 で違反になるはず: rc=%d out=%q", rc, out)
	}
	if rc, out, errs := runGateCmd(t, "-base", "HEAD", root); rc != 1 {
		t.Errorf("L1 が同じメンバーを含む組にはプリセットを効かせない: rc=%d out=%q err=%q", rc, out, errs)
	}
}

// near もプリセットを 2 人ずつでなく L2 の組全体に掛ける: A と B が定数まで同じ写しなら、C から見た A・B は隠さない (gate も止める組)。
func TestNearKeepsPresetGroupWithIdenticalMembers(t *testing.T) {
	c := strings.Replace(strings.Replace(delegateA, "ATests", "CTests", 1), `"a-tests"`, `"c-tests"`, 1)
	root := presetRepo(t, map[string]string{
		"Tests/A.swift": delegateA,
		"Tests/B.swift": strings.Replace(delegateA, "ATests", "BTests", 1),
	})
	writeFile(t, root, "Tests/C.swift", c)
	if rc, out, _ := runGateCmd(t, "-base", "HEAD", root); rc != 1 {
		t.Fatalf("前提: gate は C を止める: rc=%d out=%q", rc, out)
	}
	rc, out, _ := near(t, root, filepath.Join(root, "Tests/C.swift")+":makeTemporaryFile")
	if rc != 0 || !strings.Contains(out, "Tests/A.swift") || !strings.Contains(out, "Tests/B.swift") {
		t.Errorf("gate が止める組の相手を near が隠した: rc=%d %q", rc, out)
	}
}

func TestPresetConfigErrorsAndStale(t *testing.T) {
	root := repoWith(t, map[string]string{"Tests/A.swift": delegateA})
	writeConfig(t, root, map[string]any{"preset": "no-such", "kind": "false-positive", "reason": "x"})
	if rc, _, errs := runReport(t, root); rc != 2 || !strings.Contains(errs, "知らない preset") {
		t.Errorf("知らないプリセットは rc=2: rc=%d err=%q", rc, errs)
	}
	writeConfig(t, root, map[string]any{"preset": "l2-single-call", "paths": []string{"x/**"}, "kind": "false-positive", "reason": "x"})
	if rc, _, _ := runReport(t, root); rc != 2 {
		t.Errorf("preset と paths を一緒に書いたら rc=2: rc=%d", rc)
	}
	writeConfig(t, root, presetRule)
	if _, out, _ := runReport(t, root); !strings.Contains(out, "効いていない除外 1") {
		t.Errorf("何も消さないプリセットは効いていない除外に出る: %q", out)
	}
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	// base に無いプリセットを足した変更は、新しく足された除外として出る。
	root2 := repoWith(t, map[string]string{"Tests/A.swift": delegateA})
	gitRun(t, root2, "add", ".")
	gitRun(t, root2, "commit", "-q", "-m", "base")
	writeConfig(t, root2, presetRule)
	if rc, out, _ := runGateCmd(t, "-base", "HEAD", "-fail-new-exclusions", "-v", root2); rc != 1 || !strings.Contains(out, "preset:l2-single-call") {
		t.Errorf("新しく足したプリセットは新しく足された除外に出て止まる: rc=%d out=%q", rc, out)
	}
}
