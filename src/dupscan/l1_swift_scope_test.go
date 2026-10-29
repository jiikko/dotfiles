package main

import (
	"strings"
	"testing"
)

// Swift の束縛は、それを囲む scope (block・closure・ループ・switch の case・catch・if の then・入れ子の関数・局所の型) の中だけで効く。
// guard の束縛は else の本体では効かず、guard の後ろでは効く。束縛の届かない所の同じ綴りはプロパティなので番号にしない。

func TestL1SwiftBindingDoesNotReachOutsideItsScope(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"closure_param", "        let f = { (X: String) in show(X) }\n        f(X ?? \"\"); print(1, 2, 3)"},
		{"for_after", "        for X in items { show(X) }\n        save(X); print(1, 2, 3)"},
		{"guard_else", "        guard let X = w else { show(X); return }\n        show(X); print(1, 2, 3)"},
		{"if_let_else", "        if let X = w { show(X) } else { show(X); print(1, 2, 3) }"},
		{"if_after", "        if let X = w { show(X) }\n        show(X); print(1, 2, 3)"},
		{"block_after", "        do { let X = 1; show(X) }\n        show(X); print(1, 2, 3)"},
		{"while_after", "        while let X = w { show(X); break }\n        show(X); print(1, 2, 3)"},
		{"switch_after", "        switch w { case let X?: show(X) default: break }\n        show(X); print(1, 2, 3)"},
		{"catch_after", "        do { try g() } catch let X { show(X) }\n        show(X); print(1, 2, 3)"},
		{"else_if_tail", "        if let X = w { show(X) } else if let v = z { show(v) } else { show(X); print(1, 2, 3) }"},
		{"nested_func_param", "        func h(X: Int) { show(X) }\n        show(X); print(1, 2, 3)"},
		{"local_type_member", "        struct S { let X = 1 }\n        show(X); print(1, 2, 3)"},
		{"local_type_init", "        struct S { init(X: Int) {} }\n        show(X); print(1, 2, 3)"},
		{"local_type_subscript", "        struct S { subscript(X: Int) -> Int { X } }\n        show(X); print(1, 2, 3)"},
		{"local_enum_member", "        enum E { static let X = 1 }\n        show(X); print(1, 2, 3)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := reportGroups(t, map[string]string{
				"A.swift": swiftRender("user", strings.ReplaceAll(tc.body, "X", "user")),
				"B.swift": swiftRender("account", strings.ReplaceAll(tc.body, "X", "account")),
			})
			// 入れ子の関数・局所の型の init / subscript はそれ自身が単位で、引数の名前だけが違う正しい写しとして組になりうる。見るのは render
			if strings.Contains(out, ".render()") {
				t.Errorf("束縛の届かない所で別のプロパティを読む 2 つの render を L1 の組にしない:\n%s", out)
			}
		})
	}
}

func TestL1SwiftGuardBindingStaysLocalAfterGuard(t *testing.T) {
	// 対照: guard の束縛は guard の後ろでは局所の名前 (番号)。局所の名前だけが違う写しは組のまま
	body := func(local string) string {
		return "        guard let " + local + " = user else { return }\n        show(" + local + "); print(" + local + ".count, 1, 2, 3)"
	}
	out := reportGroups(t, map[string]string{
		"A.swift": swiftRender("user", body("a")),
		"B.swift": strings.Replace(swiftRender("user", body("b")), "class Cuser", "class Duser", 1),
	})
	if !strings.Contains(out, "L1") {
		t.Errorf("guard の後ろで局所の名前だけが違う写しは L1 の組のまま:\n%s", out)
	}
}
