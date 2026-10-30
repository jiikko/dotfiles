package main

import (
	"strings"
	"testing"
)

// computed property の `set(v)` の v は新しい局所の名前 (setter の引数)。名前だけが違う setter の写しは L1 の組になる。
func TestL1SwiftSetterParamIsLocal(t *testing.T) {
	prop := func(typ, p string) string {
		return "final class " + typ + " {\n    var store = 0\n    var w: Int {\n        get { store }\n" +
			"        set(" + p + ") { store = " + p + " + 1; print(" + p + ", 1, 2, 3) }\n    }\n}\n"
	}
	out := reportGroups(t, map[string]string{"A.swift": prop("A", "v"), "B.swift": prop("B", "u")})
	if !strings.Contains(out, "L1") || !strings.Contains(out, "var w") {
		t.Errorf("引数の名前だけが違う setter の写しは L1 の組になる:\n%s", out)
	}
}

// setter の引数は setter の中だけで効く。setter を先に書き、getter が同じ綴りのプロパティを読む写しは、読むプロパティが違えば組にならない。
func TestL1SwiftSetterParamDoesNotReachGetter(t *testing.T) {
	prop := func(typ, p string) string {
		return "final class " + typ + " {\n    var store = 0\n    var " + p + " = 1\n    var w: Int {\n" +
			"        set(" + p + ") { store = " + p + " + 1; print(" + p + ", 1, 2, 3) }\n        get { " + p + " + 7 }\n    }\n}\n"
	}
	out := reportGroups(t, map[string]string{"A.swift": prop("A", "v"), "B.swift": prop("B", "q")})
	if strings.Contains(out, "var w") {
		t.Errorf("getter が読むプロパティ (v / q) が違う 2 つを L1 の組にしない:\n%s", out)
	}
}
