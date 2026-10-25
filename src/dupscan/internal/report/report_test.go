package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// 出力の形だけを、adapter を通さずに組み立てた Summary で固める (CLI の通しのテストは main_test.go)。

func member(file string, line int, enclosing, sig string) UnitRef {
	return UnitRef{Key: file + "|" + sig, File: file, Line: line, Col: 5, Kind: "func", Name: sig, Signature: sig, Enclosing: enclosing, Test: true, Visibility: "private"}
}

func sampleSummary() Summary {
	g := func(level string, nodes int, ms ...UnitRef) GroupJSON {
		return GroupJSON{Level: level, Hash: "h", Partition: "test", Members: ms, Nodes: nodes, Exclude: `{"group":["a","b"]}`}
	}
	groups := []GroupJSON{
		g("L1", 40, member("A.swift", 3, "ATests", "make(path: String)"), member("B.swift", 7, "", "make(path: String)")),
		g("L1", 30, member("A.swift", 20, "ATests", "other()"), member("B.swift", 30, "BTests", "other()")),
		g("L2", 25, member("C.swift", 1, "", "x()"), member("D.swift", 2, "", "y()")),
	}
	return Summary{Root: "/repo", Files: 4, Units: 12, FilesWithErrors: []string{}, Unreadable: []UnitRef{},
		Groups: groups, FilePairs: filePairs(groups), Exclusions: exclusions(nil)}
}

func TestTextWithoutGroupsIsOneLineAndStillShowsUnreadable(t *testing.T) {
	var b bytes.Buffer
	s := Summary{Files: 3, Units: 9, FilesWithErrors: []string{"Bad.swift"}, Unreadable: []UnitRef{member("Bad.swift", 1, "", "f()")}, Exclusions: exclusions(nil)}
	if err := Text(&b, s, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	want := "dupscan: 重複なし (3 ファイル・9 単位)・読めない構文があるファイル 1・比べなかった単位 1 (-v で一覧)\n"
	if b.String() != want {
		t.Errorf("got %q\nwant %q", b.String(), want)
	}
}

func TestTextListsGroupsWithLimitAndFilePairs(t *testing.T) {
	var b bytes.Buffer
	if err := Text(&b, sampleSummary(), TextOptions{Limit: 2}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"dupscan: 組 3 (L1 2・L2 1)・4 ファイル・12 単位\n",
		// 組が 2 つあるファイルの組み合わせだけを要約に出す (1 組の C ↔ D は出さない)。
		"  2 組・節 70: A.swift ↔ B.swift\n",
		"L1 test (節 40):\n  A.swift:3 ATests.make(path: String)\n  B.swift:7 make(path: String)\n",
		"… 他 1 組 (-limit 0 か -format json で全部)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q が無い:\n%s", want, out)
		}
	}
	if strings.Contains(out, "C.swift ↔ D.swift") || strings.Contains(out, "C.swift:1") {
		t.Errorf("組が 1 つのファイルの組み合わせと、上限を超えた組は出さない:\n%s", out)
	}
}

func TestXcodeMarksFirstMemberAsWarningAndRestAsNotes(t *testing.T) {
	var b bytes.Buffer
	s := sampleSummary()
	s.Groups = s.Groups[:1]
	s.FilesWithErrors = []string{"Bad.swift"}
	if err := Xcode(&b, s); err != nil {
		t.Fatal(err)
	}
	want := "/repo/A.swift:3:5: warning: ATests.make(path: String) は他の 1 個と本体が同じ形 (dupscan.L1)\n" +
		"/repo/B.swift:7:5: note: 同じ形: make(path: String) (dupscan.L1)\n" +
		"/repo/Bad.swift:1:1: note: 読めない構文があり、比べていない部分がある (dupscan)\n"
	if b.String() != want {
		t.Errorf("got %q\nwant %q", b.String(), want)
	}
}

func TestJSONUsesEmptyArraysAndMarksAllowedUnits(t *testing.T) {
	s := sampleSummary()
	s.Groups[0].Members[1].Allowed = true
	var b bytes.Buffer
	if err := JSON(&b, s); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("JSON として読めない: %v\n%s", err, b.String())
	}
	for _, k := range []string{"files_with_errors", "unreadable_units", "stale_exclusions", "ambiguous_exclusions"} {
		if _, ok := got[k].([]any); !ok {
			t.Errorf("%s は空でも配列 (null にしない): %v", k, got[k])
		}
	}
	var t2 bytes.Buffer
	if err := Text(&t2, s, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(t2.String(), "B.swift:7 make(path: String) (許可)") || !strings.Contains(b.String(), `"allowed": true`) {
		t.Errorf("許可の付いた単位に印が無い:\n%s\n%s", t2.String(), b.String())
	}
}
