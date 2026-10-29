package main

import (
	"strings"
	"testing"
)

// L1 は局所の名前を番号にするが、局所の名前と同じ綴りの外側の名前 (package・既に在る名前) は番号にしない。
// Go の `bytes := bytes.TrimSpace(b)` の右辺の bytes は、文の後で効く宣言より前なので package を指す。

func goTrimFunc(pkg, fn, local, param, typ string) string {
	return "package a\n\nimport \"" + pkg + "\"\n\nfunc " + fn + "(" + param + " " + typ + ") int {\n" +
		"\t" + local + " := " + pkg + ".TrimSpace(" + param + ")\n" +
		"\tif len(" + local + ") == 0 {\n\t\treturn 0\n\t}\n" +
		"\treturn len(" + local + ") + 1\n}\n"
}

func reportGroups(t *testing.T, files map[string]string) string {
	t.Helper()
	_, out, _ := runReport(t, "-v", "-min-nodes", "1", repoWith(t, files))
	return out
}

func TestL1KeepsOuterNameShadowedByShortVarDecl(t *testing.T) {
	out := reportGroups(t, map[string]string{
		"a.go": goTrimFunc("bytes", "trimA", "bytes", "b", "[]byte"),
		"b.go": goTrimFunc("strings", "trimB", "strings", "s", "string"),
	})
	if !strings.Contains(out, "重複なし") {
		t.Errorf("右辺の bytes / strings は package なので、違う package を使う 2 つを L1 の組にしない:\n%s", out)
	}
}

func TestL1StillNumbersRenamedLocals(t *testing.T) {
	// 対照: 同じ package を使い、局所の名前だけが違う写しは今までどおり L1 の組になる
	out := reportGroups(t, map[string]string{
		"a.go": goTrimFunc("bytes", "trimA", "bytes", "b", "[]byte"),
		"b.go": goTrimFunc("bytes", "trimB", "bytes", "s", "[]byte"),
	})
	if !strings.Contains(out, "L1") {
		t.Errorf("同じ package で引数の名前だけ違う写しは L1 の組のまま:\n%s", out)
	}
	out = reportGroups(t, map[string]string{
		"a.go": goTrimFunc("bytes", "trimA", "t", "b", "[]byte"),
		"b.go": goTrimFunc("bytes", "trimB", "u", "s", "[]byte"),
	})
	if !strings.Contains(out, "L1") {
		t.Errorf("局所の名前だけ違う写しは L1 の組のまま:\n%s", out)
	}
}

func TestL1NumbersRHSThatRefersToEarlierLocal(t *testing.T) {
	// 右辺の同じ綴りが既に在る局所の名前 (引数) を指すときは番号のまま (block の中の `x := x + 1` は引数 x を読む)
	body := func(fn, p string) string {
		return "package a\n\nfunc " + fn + "(" + p + " int) int {\n\tif " + p + " > 0 {\n\t\t" + p + " := " + p + " + 1\n\t\treturn " + p + " * 2\n\t}\n\treturn " + p + " - 3\n}\n"
	}
	out := reportGroups(t, map[string]string{"a.go": body("bumpA", "x"), "b.go": body("bumpB", "y")})
	if !strings.Contains(out, "L1") {
		t.Errorf("右辺が引数を指す `x := x + 1` の写しは L1 の組のまま:\n%s", out)
	}
}

// Swift: プロパティと同じ綴りの局所の名前 (shorthand の `if let user`・`if let user = user`・`let title = title…`) は、
// 右辺 (外側のプロパティ) を番号にしない。別のプロパティを読む写しを L1 の組にしない。
func swiftRender(prop, body string) string {
	return "final class C" + prop + " {\n    var " + prop + ": String?\n    func render() {\n" + body + "\n    }\n}\n"
}

func TestL1KeepsSwiftPropertyShadowedByBinding(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"shorthand", "        if let X { show(X); show(X + \"!\"); print(X.count, 1, 2, 3) }"},
		{"explicit", "        if let X = X { show(X); show(X + \"!\"); print(X.count, 1, 2, 3) }"},
		{"let_shadow", "        guard let base = X else { return }\n        let X = base.uppercased()\n        show(X); print(X.count, 1, 2, 3)"},
		{"for_in", "        for X in X ?? \"\" { show(X); print(X, 1, 2, 3) }"},
		{"annotated", "        let X: String = (X ?? \"\").uppercased()\n        show(X); print(X.count, 1, 2, 3)"},
		{"if_let_annotated", "        if let X: String = X { show(X); show(X + \"!\"); print(X.count, 1, 2, 3) }"},
		{"if_case_optional", "        if case let X? = X { show(X); show(X + \"!\"); print(X.count, 1, 2, 3) }"},
		{"if_case_some", "        if case .some(let X) = X { show(X); show(X + \"!\"); print(X.count, 1, 2, 3) }"},
		{"if_case_two_bindings", "        if case let (X?, w) = (X, 1) { show(X); show(X + \"!\"); print(X.count, w, 2, 3) }"},
		{"for_annotated", "        for X: Character in X ?? \"\" { show(X); print(X, 1, 2, 3) }"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := reportGroups(t, map[string]string{
				"A.swift": swiftRender("user", strings.ReplaceAll(tc.body, "X", "user")),
				"B.swift": swiftRender("account", strings.ReplaceAll(tc.body, "X", "account")),
			})
			if !strings.Contains(out, "重複なし") {
				t.Errorf("別のプロパティを読む 2 つを L1 の組にしない:\n%s", out)
			}
		})
	}
}

func TestL1StillGroupsSwiftShorthandOfParameter(t *testing.T) {
	// shorthand の `if let since` が引数 since を読むときは局所の名前 (番号)。引数の名前だけが違う写しは組のまま
	initOf := func(typ, p string) string {
		return "struct " + typ + " {\n    let parameters: [String: Any]\n    init(accountID: Int, " + p + ": String?) {\n" +
			"        var parameters: [String: Any] = [\"path\": \"accounts/\\(accountID)/runs\"]\n" +
			"        if let " + p + " {\n            parameters[\"key\"] = " + p + "\n        }\n        self.parameters = parameters\n    }\n}\n"
	}
	out := reportGroups(t, map[string]string{"A.swift": initOf("RunsRequest", "since"), "B.swift": initOf("WorkflowsRequest", "guid")})
	if !strings.Contains(out, "L1") {
		t.Errorf("引数を読む shorthand の写しは L1 の組のまま:\n%s", out)
	}
}

func TestL1StillNumbersSwiftRenamedLocals(t *testing.T) {
	// 対照: 同じプロパティを読み、局所の名前だけが違う写しは L1 の組のまま
	body := func(local string) string {
		return "        if let " + local + " = user { show(" + local + "); show(" + local + " + \"!\"); print(" + local + ".count, 1, 2, 3) }"
	}
	out := reportGroups(t, map[string]string{
		"A.swift": swiftRender("user", body("a")),
		"B.swift": strings.Replace(swiftRender("user", body("b")), "class Cuser", "class Duser", 1),
	})
	if !strings.Contains(out, "L1") {
		t.Errorf("局所の名前だけ違う写しは L1 の組のまま:\n%s", out)
	}
}
