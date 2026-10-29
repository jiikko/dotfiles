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
		got := lines(out)[1:]
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
		t.Errorf("当たらないときも件数を示す (クロージャは数えない): %q", out)
	}
}

func TestIndexLineShowsDocAndMarks(t *testing.T) {
	// test のファイルを場所の順で前に置き、並べ方が場所でなく prod / test で決まることを見る。
	root := repoWith(t, map[string]string{"Sources/Helpers.swift": indexed, "AppTests/TestSupport.swift": indexedTest})
	_, out, _ := index(t, "-name", "listLocation", root)
	if strings.TrimSpace(out) != swiftLabelApproximation+"\nSources/Helpers.swift:8-11 Helpers.listLocation(_ p: String, depth: Int) -> StorageLocation [private] (呼び出し ~0) // Location for listing." {
		t.Errorf("1 行の形 (許可の印の行は doc にしない): %q", out)
	}
	got := lines(func() string { _, o, _ := index(t, "-name", "bucketLocation", root); return o }())[1:]
	if len(got) != 2 || !strings.HasPrefix(got[0], "Sources/Helpers.swift") || !strings.Contains(got[1], "[test]") {
		t.Errorf("prod を test より前に並べる: %q", got)
	}
}

func TestIndexFileIsOutlineInLineOrder(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/Helpers.swift": indexed, "Sources/U.swift": unrelated})
	_, out, errs := index(t, "-file", filepath.Join(root, "Sources/Helpers.swift"), root)
	got := lines(out)[1:]
	if len(got) != 3 || !strings.Contains(got[0], ":3-5 ") || !strings.Contains(got[1], ":8-11 ") || !strings.Contains(got[2], ":12 ") {
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
	if _, out, _ := index(t, "-file", filepath.Join(root, "Sources/Many.swift"), root); len(lines(out)) != 26 {
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
	want := swiftLabelApproximation + "\nSources/Box.swift:4 Box.fill(_ x: String) (呼び出し ~3)\nSources/Box.swift:3 Box.fill(with x: Int) (呼び出し ~2)\n"
	if out != want {
		t.Errorf("ラベルで当てて数え、多い方を前に:\n got: %q\nwant: %q err=%q", out, want, errs)
	}
	if _, out, _ := index(t, "-name", "init", root); !strings.Contains(out, "Box.init(size: Int) (呼び出し ~2)") {
		t.Errorf("初期化子は型の名前とラベルで数える (Box(other:) は外す): %q", out)
	}
	if _, out, _ := index(t, "-name", "label", root); strings.Contains(out, "(呼び出し ~") {
		t.Errorf("property は数えない: %q", out)
	}
	// JSON でも近似と分かる欄の名前で出す (text の "~" と同じ印)。
	if _, out, _ := index(t, "-format", "json", "-name", "fill", root); !strings.Contains(out, `"calls_approx": 3`) || strings.Contains(out, `"calls"`) {
		t.Errorf("JSON の呼び出しの数は calls_approx の欄で出す: %q", out)
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
	got := lines(out)[1:]
	if len(got) != 2 || !strings.HasPrefix(got[0], "ZTests/SharedSupport/Loc.swift:1 ") || !strings.Contains(got[0], "[shared,test]") {
		t.Errorf("共通の置き場所の定義を先に [shared] で: %q err=%q", out, errs)
	}
	writeFile(t, root, ".dupscan.json", `{"version": 1, "exclude": [], "shared": ["["]}`)
	if rc, _, errs := index(t, "-name", "bucketLocation", root); rc != 2 || !strings.Contains(errs, "shared") {
		t.Errorf("読めない glob は rc=2: rc=%d err=%q", rc, errs)
	}
}

// 同じ型を返す関数と computed property が混ざる。-returns で関数を探すとき、-kind で property の雑音を外せる。
const kindMixed = `struct Plan {
    var location: StorageLocation { StorageLocation(path: "p") }
    func location(at path: String) -> StorageLocation { StorageLocation(path: path) }
    init(location: StorageLocation) { self.stored = location }
    var stored: StorageLocation
}
`

func TestIndexKindNarrowsToFunctionsAndInits(t *testing.T) {
	root := repoWith(t, map[string]string{"Plan.swift": kindMixed})
	_, all, _ := index(t, "-returns", "StorageLocation", "-limit", "0", root)
	if !strings.Contains(all, "var location") || !strings.Contains(all, "location(at") {
		t.Fatalf("前提: 既定は property も関数も出す: %q", all)
	}
	_, funcs, _ := index(t, "-returns", "StorageLocation", "-kind", "func", "-limit", "0", root)
	if !strings.Contains(funcs, "location(at") || strings.Contains(funcs, "var location") {
		t.Errorf("-kind func は関数だけ (property は出ない): %q", funcs)
	}
	_, props, _ := index(t, "-returns", "StorageLocation", "-kind", "property", "-limit", "0", root)
	if strings.Contains(props, "location(at") || !strings.Contains(props, "var location") {
		t.Errorf("-kind property は property だけ: %q", props)
	}
	_, both, _ := index(t, "-returns", "StorageLocation", "-kind", "func, property", "-limit", "0", root)
	if !strings.Contains(both, "location(at") || !strings.Contains(both, "var location") {
		t.Errorf("カンマ区切りで複数の種類を渡せる: %q", both)
	}
}

func TestIndexKindRejectsUnknownKindInsteadOfReturningNothing(t *testing.T) {
	root := repoWith(t, map[string]string{"Plan.swift": kindMixed})
	rc, out, errs := index(t, "-returns", "StorageLocation", "-kind", "function", root)
	if rc != 2 || !strings.Contains(errs, "知らない種類") || out != "" {
		t.Errorf("知らない種類は黙って 0 件にせずエラー: rc=%d out=%q err=%q", rc, out, errs)
	}
}

// -kind init は initializer だけを返す。-returns を併用しないので、戻り値の型を持たない init も対象になる
// (kind の対応表が init を別の種類に取り違えると、ここで落ちる)。
func TestIndexKindInitReturnsOnlyInitializers(t *testing.T) {
	root := repoWith(t, map[string]string{"Plan.swift": kindMixed})
	_, inits, _ := index(t, "-kind", "init", "-limit", "0", root)
	if !strings.Contains(inits, "init(location") || strings.Contains(inits, "location(at") || strings.Contains(inits, "var location") {
		t.Errorf("-kind init は initializer だけ: %q", inits)
	}
	_, funcs, _ := index(t, "-kind", "func", "-limit", "0", root)
	if strings.Contains(funcs, "init(location") || !strings.Contains(funcs, "location(at") {
		t.Errorf("-kind func に initializer は混ざらない: %q", funcs)
	}
}

// 他のファイル (Go は他の package) から呼べない単位は、呼べる範囲の呼び出しだけを数える。名前だけで数えると、同名の private な写しが
// ファイルごとにあるとき両方に合計が付き、どちらが多く使われているかを読み違える。
func TestIndexCountsHiddenUnitCallsOnlyWhereTheyAreVisible(t *testing.T) {
	swift := func(typ string, calls int) string {
		body := strings.Repeat("        waitUntil(\"x\")\n", calls)
		return "final class " + typ + " {\n    private func waitUntil(_ what: String) {}\n    func run() {\n" + body + "    }\n}\n"
	}
	root := repoWith(t, map[string]string{"Tests/A.swift": swift("A", 3), "Tests/B.swift": swift("B", 1)})
	_, out, errs := index(t, "-name", "waitUntil", root)
	if !strings.Contains(out, "A.waitUntil(_ what: String) [test,private] (呼び出し ~3)") ||
		!strings.Contains(out, "B.waitUntil(_ what: String) [test,private] (呼び出し ~1)") {
		t.Fatalf("private はそのファイルの呼び出しだけを数える: %q err=%q", out, errs)
	}
	goSrc := func(pkg string, calls int) string {
		return "package " + pkg + "\n\nfunc helper() {}\n\nfunc Run() {\n" + strings.Repeat("\thelper()\n", calls) + "}\n"
	}
	root = repoWith(t, map[string]string{"a/a.go": goSrc("a", 2), "a/a2.go": "package a\n\nfunc Other() {\n\thelper()\n}\n", "b/b.go": goSrc("b", 1)})
	_, out, errs = index(t, "-name", "helper", root)
	if !strings.Contains(out, "a/a.go:3 helper() [unexported] (呼び出し ~3)") || !strings.Contains(out, "b/b.go:3 helper() [unexported] (呼び出し ~1)") {
		t.Fatalf("Go の小文字の関数は同じ package (dir) の呼び出しを数える (別のファイルも含む): %q err=%q", out, errs)
	}
}
