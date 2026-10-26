package langs

import "testing"

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
	if _, ok := For("README.md"); ok {
		t.Error("README.md を読む adapter があることになっている")
	}
	if !IsTestPath("Tests/ATests.swift") || IsTestPath("Sources/A.swift") || IsTestPath("Tests/README.md") {
		t.Error("IsTestPath が adapter の規則と食い違う")
	}
}
