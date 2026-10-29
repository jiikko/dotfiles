package main

import (
	"strings"
	"testing"
)

const callSites = `struct Store {
    func make(x: Int, _ y: Int) -> Int { x + y }
    func use() -> Int {
        let a = make(x: 1, 2)
        // make(x: 9, 9) はコメントなので数えない
        let s = "make(x: 3, 4)"
        return a + other.make(x: 5, 6) + make(y: 7)
    }
    func each() { list.make(x: 1) { $0 } }
}
`

const callSitesTest = `import XCTest
final class StoreTests: XCTestCase {
    func testMake() { XCTAssertEqual(Store().make(x: 1, 2), 3) }
}
`

// dupscan:allow-group 8 intentional サブコマンドの名前を付けるだけの 1 行の wrapper (callers / contextCmd / def / refs / evidence / index / near / sweep)。寄せると呼び出し側が名前を文字列で書くことになる
func callers(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	return runReport(t, append([]string{"callers"}, args...)...)
}

func TestCallersFindsCallSitesNotCommentsOrStrings(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/Store.swift": callSites, "Tests/StoreTests.swift": callSitesTest})
	rc, out, errs := callers(t, "make", root)
	got := lines(out)[1:]
	want := []string{
		"Sources/Store.swift:4 Store.use() -> Int | let a = make(x: 1, 2)",
		"Sources/Store.swift:7 Store.use() -> Int | return a + other.make(x: 5, 6) + make(y: 7)",
		"Sources/Store.swift:7 Store.use() -> Int | return a + other.make(x: 5, 6) + make(y: 7)",
		"Sources/Store.swift:9 Store.each() | func each() { list.make(x: 1) { $0 } }",
		"Tests/StoreTests.swift:3 StoreTests.testMake() [test] | func testMake() { XCTAssertEqual(Store().make(x: 1, 2), 3) }",
	}
	if rc != 0 || strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("呼び出しの箇所だけを prod → test の順に (コメント・文字列は数えない):\n got: %q\nwant: %q\nerr=%q", got, want, errs)
	}
}

func TestCallersNarrowsByLabels(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/Store.swift": callSites})
	_, out, _ := callers(t, "make(x:_:)", root)
	var at []string
	for _, l := range lines(out)[1:] {
		at = append(at, strings.Fields(l)[0])
	}
	if strings.Join(at, " ") != "Sources/Store.swift:4 Sources/Store.swift:7 Sources/Store.swift:9" {
		t.Errorf("make(x:_:) は x と _ の呼び出し 2 件 (7 行目の make(y:) は外す) と、後置クロージャで 2 つ目を補う 1 件: %q", out)
	}
	if _, out, _ := callers(t, "make(y:)", root); len(lines(out)) != 2 || !strings.Contains(out, ":7 ") {
		t.Errorf("make(y:) は 1 件: %q", out)
	}
	if _, out, _ := callers(t, "make(x:)", root); strings.Contains(out, ":9 ") || !strings.Contains(out, "呼び出しなし") {
		t.Errorf("後置クロージャのある呼び出しは、括弧の中より多いラベルにだけ当たる: %q", out)
	}
	if rc, _, errs := callers(t, "make(x:", root); rc != 2 || !strings.Contains(errs, "の形ではない") {
		t.Errorf("閉じていない括弧は rc=2: rc=%d err=%q", rc, errs)
	}
}

// 呼び出し元は名前の付いた単位で出す (複数行のクロージャの中でも囲む関数)。test のファイルを場所の順で前に置き、
// 並べ方が場所でなく prod / test で決まることを見る。後置クロージャの無い呼び出しは、ラベルの数が q と同じときだけ当てる。
func TestCallersNamesEnclosingUnitAndOrdersProdFirst(t *testing.T) {
	src := `struct Feed {
    func load() {
        items.forEach { item in
            send(to: item)
        }
        send(to: x, retry: 2)
    }
}
`
	test := "struct FeedChecks {\n    func check() { send(to: 1) }\n}\n"
	root := repoWith(t, map[string]string{"Sources/Feed.swift": src, "AppTests/FeedChecks.swift": test})
	_, out, _ := callers(t, "send(to:)", root)
	want := swiftLabelApproximation + "\nSources/Feed.swift:4 Feed.load() | send(to: item)\nAppTests/FeedChecks.swift:2 FeedChecks.check() [test] | func check() { send(to: 1) }\n"
	if out != want {
		t.Errorf("send(to:) は 2 件 (send(to:retry:) は外す)・クロージャの中は囲む関数・prod を先に:\n got: %q\nwant: %q", out, want)
	}
	if _, out, _ := callers(t, "send(to:retry:)", root); len(lines(out)) != 2 || !strings.Contains(out, "Feed.swift:6 ") {
		t.Errorf("send(to:retry:) は 1 件 (後置クロージャの無い send(to:) は、ラベルが足りないので外す): %q", out)
	}
}
