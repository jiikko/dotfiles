package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const goHelperA = `package a

func location(path string) Location {
	return Location{Conn: ConnectionID("conn"), Path: ObjectPath(path), Retries: retries(path, 3)}
}
`

const goHelperB = `package b

func makeLocation(p string) Location {
	return Location{Conn: ConnectionID("conn"), Path: ObjectPath(p), Retries: retries(p, 3)}
}
`

// Go と Swift が同じ repo にあっても、それぞれの写しを組にする (言語の違う単位どうしは組にならない)。
func TestGoCopyIsReportedAlongsideSwift(t *testing.T) {
	root := repoWith(t, map[string]string{
		"Sources/A.swift": helperA, "Sources/B.swift": helperB,
		"pkg/a/a.go": goHelperA, "pkg/b/b.go": goHelperB,
	})
	var out, errb bytes.Buffer
	if rc := run([]string{"-format", "json", "-min-nodes", "5", root}, &out, &errb); rc != 0 {
		t.Fatalf("rc=%d: %s", rc, errb.String())
	}
	var s struct {
		Groups []struct {
			Level   string
			Members []struct{ File, Name string }
		}
	}
	if err := json.Unmarshal(out.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	langs := map[string]bool{}
	for _, g := range s.Groups {
		ext := ""
		for _, m := range g.Members {
			e := m.File[strings.LastIndex(m.File, "."):]
			if ext != "" && e != ext {
				t.Errorf("言語の違う単位が 1 つの組に入った: %+v", g.Members)
			}
			ext = e
		}
		langs[ext] = true
	}
	if !langs[".go"] || !langs[".swift"] {
		t.Errorf("Go と Swift の両方の組を期待: %s", out.String())
	}
}

func TestGateStopsNewGoCopy(t *testing.T) {
	root := repoWith(t, map[string]string{"pkg/a/a.go": goHelperA})
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-q", "-m", "base")
	writeFile(t, root, "pkg/b/b.go", goHelperB)
	var out, errb bytes.Buffer
	rc := run([]string{"gate", "-base", "HEAD", "-min-nodes", "5", root}, &out, &errb)
	if rc != 1 || !strings.Contains(out.String(), "pkg/b/b.go") {
		t.Errorf("rc=%d (1 を期待)\n%s%s", rc, out.String(), errb.String())
	}
}

// def / refs は Go の定義・参照を集めない。黙って「定義なし」にせず、読まなかった Go のファイルの数を出す。
func TestDefAndRefsSayGoIsNotCovered(t *testing.T) {
	goOnly := repoWith(t, map[string]string{"pkg/a/a.go": goHelperA})
	for _, sub := range []string{"def", "refs"} {
		var out, errb bytes.Buffer
		rc := run([]string{sub, "location", goOnly}, &out, &errb)
		if rc != 2 || !strings.Contains(errb.String(), "go のファイル 1 個は def / refs の対象外") {
			t.Errorf("%s (Go だけの repo): rc=%d %q", sub, rc, errb.String())
		}
	}
	mixed := repoWith(t, map[string]string{"Sources/A.swift": helperA, "pkg/a/a.go": goHelperA})
	for _, sub := range []string{"def", "refs"} {
		var out, errb bytes.Buffer
		rc := run([]string{sub, "location", mixed}, &out, &errb)
		if (sub == "def" && rc != 0) || !strings.Contains(errb.String(), "go のファイル 1 個は def / refs の対象外") {
			t.Errorf("%s: Swift の location (見つかっても注記を出す) rc=%d %q", sub, rc, errb.String())
		}
		if sub == "refs" {
			// refs も見つかったときに注記を出す (StorageLocation は helperA の Swift に型の位置の参照がある)
			var o3, e3 bytes.Buffer
			if rc := run([]string{sub, "StorageLocation", mixed}, &o3, &e3); rc != 0 || !strings.Contains(e3.String(), "go のファイル 1 個は def / refs の対象外") {
				t.Errorf("refs: 見つかっても注記を出す rc=%d %q", rc, e3.String())
			}
		}
		var o2, e2 bytes.Buffer
		rc = run([]string{sub, "makeLocation", mixed}, &o2, &e2)
		if rc != 1 || !strings.Contains(e2.String(), "go のファイル 1 個は def / refs の対象外") {
			t.Errorf("%s (見つからない・Go も在る): rc=%d %q", sub, rc, e2.String())
		}
	}
}

// Go の小文字で始まる関数は別の package から使えないので、index で印を付けて公開の関数より後ろに並べる。
func TestIndexMarksUnexportedGoFuncs(t *testing.T) {
	root := repoWith(t, map[string]string{
		"pkg/a/a.go": "package a\n\nfunc parseLocation(s string) Location { return Location{Path: s} }\n",
		"pkg/b/b.go": "package b\n\n// ParseLocation は公開の版。\nfunc ParseLocation(s string) Location { return Location{Path: s} }\n",
	})
	var out, errb bytes.Buffer
	if rc := run([]string{"index", "-name", "parselocation", root}, &out, &errb); rc != 0 {
		t.Fatalf("rc=%d: %s", rc, errb.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "pkg/b/b.go") || strings.Contains(lines[0], "unexported") ||
		!strings.Contains(lines[1], "pkg/a/a.go") || !strings.Contains(lines[1], "unexported") {
		t.Errorf("公開の関数を先に、非公開に印を付けて並べるはず:\n%s", out.String())
	}
}
