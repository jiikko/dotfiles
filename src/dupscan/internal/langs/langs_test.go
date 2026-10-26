package langs

import (
	"strings"
	"testing"

	"dupscan/internal/lang/swift"
)

// 2 つの adapter が同じ拡張子を持つと、For は先に並んだ方だけを返し、後の方は黙って読まれない。
func TestExtensionsAreUnique(t *testing.T) {
	owner := map[string]string{}
	for _, a := range All {
		if len(a.Extensions) == 0 {
			t.Errorf("%s が拡張子を持たない", a.Name)
		}
		for _, e := range a.Extensions {
			if o, ok := owner[e]; ok {
				t.Errorf("拡張子 %s を %s と %s が持つ", e, o, a.Name)
			}
			owner[e] = a.Name
		}
	}
}

func TestForPicksAdapterByExtension(t *testing.T) {
	if a, ok := For("Sources/A.swift"); !ok || a.Name != "swift" {
		t.Errorf("A.swift: %q %v", a.Name, ok)
	}
	if a, ok := For("cmd/main.go"); !ok || a.Name != "go" {
		t.Errorf("main.go: %q %v", a.Name, ok)
	}
	if _, ok := For("README.md"); ok {
		t.Error("README.md を読む adapter があることになっている")
	}
	if !IsTestPath("Tests/ATests.swift") || IsTestPath("Sources/A.swift") || IsTestPath("Tests/README.md") ||
		!IsTestPath("pkg/a_test.go") || IsTestPath("pkg/a.go") || !IsTestPath("pkg/testdata/fixture.go") {
		t.Error("IsTestPath が adapter の規則と食い違う")
	}
}

// ハッシュには言語が入らない。Go の adapter は節の種類名に go. の頭を付けて Swift の単位と同じ形にならないようにしているので、
// Swift の文法 (tree-sitter) の種類名に go. で始まるものが無いことを文法から確かめる (文法の版を上げたときに崩れないように)。
func TestGoKindsDoNotCollideWithSwiftGrammar(t *testing.T) {
	l := swift.Language()
	n := 0
	for id := range uint16(l.NodeKindCount()) {
		n++
		if k := l.NodeKindForId(id); strings.HasPrefix(k, "go.") {
			t.Errorf("Swift の文法に Go の頭の種類名 %q がある", k)
		}
	}
	if n < 150 {
		t.Fatalf("文法の種類が %d しかない (列挙が空振りしていないか)", n)
	}
}
