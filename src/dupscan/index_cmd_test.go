package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

const indexed = `struct Helpers {
    /// Builds the bucket location used by the storage tests.
    func bucketLocation(path: String) -> StorageLocation {
        StorageLocation(connectionID: ConnectionID(rawValue: "conn"), path: ObjectPath(rawValue: path))
    }
    // dupscan:allow intentional 分けておく
    /// Location for listing.
    private func listLocation(_ p: String, depth: Int) -> StorageLocation {
        let f = { (x: Int) in x + depth }
        return StorageLocation(connectionID: ConnectionID(rawValue: "conn"), path: ObjectPath(rawValue: p + String(f(1))))
    }
    func location(path: String) -> StorageLocation { make(path) }
}
`

const indexedTest = `struct TestSupport {
    func bucketLocation(path: String) -> StorageLocation { make(path) }
}
`

func index(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	return runReport(t, append([]string{"index"}, args...)...)
}

func lines(out string) []string { return strings.Split(strings.TrimRight(out, "\n"), "\n") }

func TestIndexFindsByNameReturnParamAndDoc(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/Helpers.swift": indexed, "Sources/U.swift": unrelated})
	for _, c := range []struct {
		args []string
		want []string
	}{
		{[]string{"-name", "location"}, []string{"Helpers.location(path: String)", "Helpers.bucketLocation", "Helpers.listLocation"}},
		{[]string{"-returns", "StorageLocation"}, []string{"Helpers.bucketLocation", "Helpers.location", "Helpers.listLocation"}},
		{[]string{"-param-type", "Int"}, []string{"U.total(_ xs: [Int])", "Helpers.listLocation"}}, // 型は一部で引く ([Int] も当たる)。private は後
		{[]string{"-doc", "bucket"}, []string{"Helpers.bucketLocation"}},
		{[]string{"-name", "loc", "-param-type", "String", "-returns", "Storage"}, []string{"Helpers.location", "Helpers.bucketLocation", "Helpers.listLocation"}}, // 前方一致を一部の一致より前
	} {
		rc, out, errs := index(t, append(c.args, root)...)
		got := lines(out)
		if rc != 0 || len(got) != len(c.want) {
			t.Errorf("%v: %d 件を期待: rc=%d out=%q err=%q", c.args, len(c.want), rc, out, errs)
			continue
		}
		for i, w := range c.want {
			if !strings.Contains(got[i], w) {
				t.Errorf("%v: %d 番目に %s を期待: %q", c.args, i+1, w, out)
			}
		}
	}
	if _, out, _ := index(t, "-name", "nothing", root); !strings.Contains(out, "当たる単位なし (単位 4)") {
		t.Errorf("当たらないときは 1 行 (クロージャは数えない): %q", out)
	}
}

func TestIndexLineShowsDocAndMarks(t *testing.T) {
	// test のファイルを場所の順で前に置き、並べ方が場所でなく prod / test で決まることを見る。
	root := repoWith(t, map[string]string{"Sources/Helpers.swift": indexed, "AppTests/TestSupport.swift": indexedTest})
	_, out, _ := index(t, "-name", "listLocation", root)
	if strings.TrimSpace(out) != "Sources/Helpers.swift:8 Helpers.listLocation(_ p: String, depth: Int) -> StorageLocation [private] // Location for listing." {
		t.Errorf("1 行の形 (許可の印の行は doc にしない): %q", out)
	}
	got := lines(func() string { _, o, _ := index(t, "-name", "bucketLocation", root); return o }())
	if len(got) != 2 || !strings.HasPrefix(got[0], "Sources/Helpers.swift") || !strings.HasSuffix(got[1], "[test]") {
		t.Errorf("prod を test より前に並べる: %q", got)
	}
}

func TestIndexFileIsOutlineInLineOrder(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/Helpers.swift": indexed, "Sources/U.swift": unrelated})
	_, out, errs := index(t, "-file", filepath.Join(root, "Sources/Helpers.swift"), root)
	got := lines(out)
	if len(got) != 3 || !strings.Contains(got[0], ":3 ") || !strings.Contains(got[1], ":8 ") || !strings.Contains(got[2], ":12 ") {
		t.Errorf("ファイルの単位を行の順に (クロージャは出さない): %q err=%q", out, errs)
	}
	if _, out, _ := index(t, "-file", filepath.Join(root, "Sources/Helpers.swift"), "-limit", "1", root); !strings.Contains(out, "… 他 2 件") {
		t.Errorf("-limit を書けば -file でも切る: %q", out)
	}
	var many strings.Builder
	many.WriteString("struct Many {\n")
	for i := range 25 {
		fmt.Fprintf(&many, "    func f%d() {}\n", i)
	}
	many.WriteString("}\n")
	writeFile(t, root, "Sources/Many.swift", many.String())
	if _, out, _ := index(t, "-file", filepath.Join(root, "Sources/Many.swift"), root); len(lines(out)) != 25 {
		t.Errorf("-file の既定は件数で切らない: %q", out)
	}
}
