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
	if strings.TrimSpace(out) != "Sources/Helpers.swift:8 Helpers.listLocation(_ p: String, depth: Int) -> StorageLocation [private] (呼び出し ~0) // Location for listing." {
		t.Errorf("1 行の形 (許可の印の行は doc にしない): %q", out)
	}
	got := lines(func() string { _, o, _ := index(t, "-name", "bucketLocation", root); return o }())
	if len(got) != 2 || !strings.HasPrefix(got[0], "Sources/Helpers.swift") || !strings.Contains(got[1], "[test]") {
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

// 呼び出しの数は callers と同じ当て方 (名前と引数ラベル) で数え、多いものを前に並べる。初期化子は型の名前で当てる。
func TestIndexCountsCallsAndRanksByThem(t *testing.T) {
	src := `struct Box {
    init(size: Int) {}
    func fill(with x: Int) {}
    func fill(_ x: String) {}
    var label: String { "" }
}
func use() {
    let b = Box(size: 1)
    b.fill(with: 2)
    b.fill(with: 3)
    b.fill("s")
    b.fill("t")
    b.fill("u")
    _ = Box(size: 2)
    _ = Box(other: 1)
}
`
	root := repoWith(t, map[string]string{"Sources/Box.swift": src})
	_, out, errs := index(t, "-name", "fill", root)
	want := "Sources/Box.swift:4 Box.fill(_ x: String) (呼び出し ~3)\nSources/Box.swift:3 Box.fill(with x: Int) (呼び出し ~2)\n"
	if out != want {
		t.Errorf("ラベルで当てて数え、多い方を前に:\n got: %q\nwant: %q err=%q", out, want, errs)
	}
	if _, out, _ := index(t, "-name", "init", root); !strings.Contains(out, "Box.init(size: Int) (呼び出し ~2)") {
		t.Errorf("初期化子は型の名前とラベルで数える (Box(other:) は外す): %q", out)
	}
	if _, out, _ := index(t, "-name", "label", root); strings.Contains(out, "呼び出し") {
		t.Errorf("property は数えない: %q", out)
	}
}

// 設定の shared (共通の置き場所) の定義は、名前の一致の強さの次に先に並べ、[shared] と印を付ける (prod / test より強い)。
func TestIndexRanksSharedPlaceFirst(t *testing.T) {
	support := "func bucketLocation(path: String) -> StorageLocation { make(path) }\n"
	root := repoWith(t, map[string]string{
		"Sources/Helpers.swift":          indexed,
		"ZTests/SharedSupport/Loc.swift": support,
	})
	writeFile(t, root, ".dupscan.json", `{"version": 1, "exclude": [], "shared": ["ZTests/SharedSupport/**"]}`)
	_, out, errs := index(t, "-name", "bucketLocation", root)
	got := lines(out)
	if len(got) != 2 || !strings.HasPrefix(got[0], "ZTests/SharedSupport/Loc.swift:1 ") || !strings.Contains(got[0], "[shared,test]") {
		t.Errorf("共通の置き場所の定義を先に [shared] で: %q err=%q", out, errs)
	}
	writeFile(t, root, ".dupscan.json", `{"version": 1, "exclude": [], "shared": ["["]}`)
	if rc, _, errs := index(t, "-name", "bucketLocation", root); rc != 2 || !strings.Contains(errs, "shared") {
		t.Errorf("読めない glob は rc=2: rc=%d err=%q", rc, errs)
	}
}
