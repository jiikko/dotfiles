package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const singleCallMark = "  印: 全員の本体が 1 つの呼び出しを 1 回するだけ"

// fakeWaiter は共通の待機の道具へ寄せた後に各 fake に残る 1 行の委譲 (文が 1 つのクロージャを渡す)。
func fakeWaiter(typeName string) string {
	return "final class " + typeName + " {\n    var updateCount = 0\n    let waiters = Waiters()\n" +
		"    func waitUntilUpdateCount(_ n: Int) async {\n        await waiters.wait(until: { self.updateCount >= n })\n    }\n}\n"
}

// singleCallGroups は report の json の組ごとの single_call。
func singleCallGroups(t *testing.T, root string) []bool {
	t.Helper()
	rc, out, errs := runReport(t, "-format", "json", "-min-nodes", "1", "-min-closure-nodes", "1000", root)
	var s struct {
		Groups []struct {
			SingleCall bool `json:"single_call"`
		}
	}
	if err := json.Unmarshal([]byte(out), &s); rc != 0 || err != nil {
		t.Fatalf("rc=%d err=%v stderr=%s", rc, err, errs)
	}
	var got []bool
	for _, g := range s.Groups {
		got = append(got, g.SingleCall)
	}
	return got
}

func TestThinDelegationGroupIsMarkedNotHidden(t *testing.T) {
	root := repoWith(t, map[string]string{"Tests/FakeA.swift": fakeWaiter("FakeA"), "Tests/FakeB.swift": fakeWaiter("FakeB")})
	if got := singleCallGroups(t, root); len(got) != 1 || !got[0] {
		t.Fatalf("文が 1 つのクロージャを渡す 1 行の委譲の組を 1 つ、印付きで出す: %v", got)
	}
	rc, out, _ := runReport(t, "-min-nodes", "1", "-min-closure-nodes", "1000", root)
	if rc != 0 || !strings.Contains(out, "waitUntilUpdateCount") || !strings.Contains(out, singleCallMark) {
		t.Fatalf("text では組を出したまま印の行を足す: %q", out)
	}
}

func TestThinDelegationWithOneStatementNestedClosureIsMarked(t *testing.T) {
	src := func(typ string) string {
		return "final class " + typ + " {\n    var updates: [Update] = []\n    let waiters = Waiters()\n" +
			"    func waitUntilUpdated(id: String) async {\n        await waiters.wait(until: { self.updates.contains { $0.id == id } })\n    }\n}\n"
	}
	root := repoWith(t, map[string]string{"Tests/FakeA.swift": src("FakeA"), "Tests/FakeB.swift": src("FakeB")})
	if got := singleCallGroups(t, root); len(got) != 1 || !got[0] {
		t.Fatalf("入れ子のクロージャも文が 1 つ (述語) なら、薄い委譲として印を付ける: %v", got)
	}
	goSrc := func(typ string) string {
		return "package a\n\ntype " + typ + " struct{ w *Waiters; xs []int }\n\nfunc (f *" + typ + ") WaitFor(n int) {\n\tf.w.Wait(func() bool { return contains(f.xs, func(x int) bool { return x == n }) })\n}\n"
	}
	root = repoWith(t, map[string]string{"a/a.go": goSrc("FakeA"), "a/b.go": goSrc("FakeB")})
	if got := singleCallGroups(t, root); len(got) != 1 || !got[0] {
		t.Fatalf("Go でも、入れ子の関数リテラルの文が 1 つなら印を付ける: %v", got)
	}
}

func TestSingleCallMarkNeedsThinArguments(t *testing.T) {
	cases := map[string]string{
		"引数が 3 つの呼び出し":            "    func make() -> Box {\n        Box(width: 1, height: 2, depth: 3)\n    }\n",
		"文が 2 つのクロージャ":            "    func wait(_ n: Int) async {\n        await waiters.wait(until: { let c = self.updateCount; return c >= n })\n    }\n",
		"呼び出しの後に文が続く":             "    func reset() {\n        store.clear()\n        count = 0\n    }\n",
		"呼ぶ先が計算 (呼び出しの結果)":        "    func run() {\n        makeRunner(kind).run(1)\n    }\n",
		"添字 (丸括弧でない)":             "    func read(_ key: String) -> Int {\n        values[key, default: 0]\n    }\n",
		"クロージャの 1 文が制御文":          "    func run(_ xs: [Int]) {\n        dispatch { for x in xs { validate(x); persist(x) } }\n    }\n",
		"クロージャの 1 文が defer":       "    func run() {\n        dispatch { defer { validate(); persist() } }\n    }\n",
		"クロージャの 1 文がさらにクロージャを持つ":  "    func run() {\n        dispatch { perform { validate(); persist() } }\n    }\n",
		"クロージャの 1 文が文 2 つのクロージャ式": "    func run() {\n        saveFactory { { validate(); persist() } }\n    }\n",
	}
	for name, member := range cases {
		t.Run(name, func(t *testing.T) {
			src := func(typ string) string { return "final class " + typ + " {\n" + member + "}\n" }
			root := repoWith(t, map[string]string{"Sources/A.swift": src("A"), "Sources/B.swift": src("B")})
			if got := singleCallGroups(t, root); len(got) != 1 || got[0] {
				t.Fatalf("組を 1 つ、印なしで出す: %v", got)
			}
		})
	}
}

func TestGoThinDelegationGroupIsMarked(t *testing.T) {
	src := func(typ string) string {
		return "package a\n\ntype " + typ + " struct{ w *Waiters; n int }\n\nfunc (f *" + typ + ") WaitUntil(n int) {\n\tf.w.Wait(func() bool { return f.n >= n })\n}\n"
	}
	root := repoWith(t, map[string]string{"a/a.go": src("FakeA"), "a/b.go": src("FakeB")})
	if got := singleCallGroups(t, root); len(got) != 1 || !got[0] {
		t.Fatalf("Go でも、文が 1 つの関数リテラルを渡す 1 行の委譲に印を付ける: %v", got)
	}
	wide := func(typ string) string {
		return "package a\n\ntype " + typ + " struct{ w *Waiters }\n\nfunc (f *" + typ + ") Make() *Box {\n\treturn NewBox(f.w, 1, 2)\n}\n"
	}
	root = repoWith(t, map[string]string{"a/a.go": wide("FakeA"), "a/b.go": wide("FakeB")})
	if got := singleCallGroups(t, root); len(got) != 1 || got[0] {
		t.Fatalf("Go の引数が 3 つの呼び出しには印を付けない: %v", got)
	}
	loop := func(typ string) string {
		return "package a\n\ntype " + typ + " struct{}\n\nfunc (f *" + typ + ") Process(xs []int) {\n\tdispatch(func() {\n\t\tfor _, x := range xs {\n\t\t\tvalidate(x)\n\t\t\tpersist(x)\n\t\t}\n\t})\n}\n"
	}
	root = repoWith(t, map[string]string{"a/a.go": loop("FakeA"), "a/b.go": loop("FakeB")})
	if got := singleCallGroups(t, root); len(got) != 1 || got[0] {
		t.Fatalf("Go の関数リテラルの 1 文が制御文なら印を付けない: %v", got)
	}
	nested := func(typ string) string {
		return "package a\n\ntype " + typ + " struct{}\n\nfunc (f *" + typ + ") Run() {\n\tdispatch(func() {\n\t\tdefer func() {\n\t\t\tvalidate()\n\t\t\tpersist()\n\t\t}()\n\t})\n}\n"
	}
	root = repoWith(t, map[string]string{"a/a.go": nested("FakeA"), "a/b.go": nested("FakeB")})
	if got := singleCallGroups(t, root); len(got) != 1 || got[0] {
		t.Fatalf("Go の関数リテラルの 1 文がさらに関数リテラルを持つなら印を付けない: %v", got)
	}
	assign := func(typ string) string {
		return "package a\n\ntype " + typ + " struct{ ready bool }\n\nfunc (f *" + typ + ") MarkReady() {\n\tdispatch(func() { f.ready = true })\n}\n"
	}
	root = repoWith(t, map[string]string{"a/a.go": assign("FakeA"), "a/b.go": assign("FakeB")})
	if got := singleCallGroups(t, root); len(got) != 1 || !got[0] {
		t.Fatalf("Go の関数リテラルの 1 文が代入でも印を付ける (Swift と同じ): %v", got)
	}
}

func TestGoClosureThinDelegationIsMarked(t *testing.T) {
	root := repoWith(t, map[string]string{"a/a.go": "package a\n\nvar sink func(int)\n\nvar first = func(n int) { sink(n) }\n\nvar second = func(n int) { sink(n) }\n"})
	rc, out, errs := runReport(t, "-format", "json", "-min-nodes", "1000", "-min-closure-nodes", "1", root)
	var s struct {
		Groups []struct {
			SingleCall bool `json:"single_call"`
		}
	}
	if err := json.Unmarshal([]byte(out), &s); rc != 0 || err != nil || len(s.Groups) != 1 || !s.Groups[0].SingleCall {
		t.Fatalf("Go のクロージャの薄い委譲にも印を付ける (Swift と同じ): rc=%d err=%v out=%s stderr=%s", rc, err, out, errs)
	}
}

func TestGateViolationCarriesSingleCallMark(t *testing.T) {
	root := repoWith(t, map[string]string{"Tests/FakeA.swift": fakeWaiter("FakeA")})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "Tests/FakeB.swift", fakeWaiter("FakeB"))
	rc, out, errs := runReport(t, "gate", "-format", "json", "-min-nodes", "1", "-min-closure-nodes", "1000", "-base", "HEAD", root)
	var s struct {
		Violations []struct {
			SingleCall bool `json:"single_call"`
		}
	}
	if err := json.Unmarshal([]byte(out), &s); rc != 1 || err != nil || len(s.Violations) != 1 || !s.Violations[0].SingleCall {
		t.Fatalf("gate の違反にも同じ印を付ける (止めるかは変えない): rc=%d err=%v out=%s stderr=%s", rc, err, out, errs)
	}
}
