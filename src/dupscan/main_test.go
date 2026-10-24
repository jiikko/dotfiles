package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"subproc"
)

func TestMain(m *testing.M) {
	if err := subproc.UnsetGitLocationEnv(); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func repoWith(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if out, err := subproc.GitCommand(context.Background(), "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const helperA = `struct A {
    func location(path: String) -> StorageLocation {
        StorageLocation(connectionID: ConnectionID(rawValue: "conn"), path: ObjectPath(rawValue: path))
    }
}
`

const helperB = `struct B {
    func makeLocation(_ p: String) -> StorageLocation {
        StorageLocation(connectionID: ConnectionID(rawValue: "conn"), path: ObjectPath(rawValue: p))
    }
}
`

func TestNoDuplicatesIsOneLine(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA})
	var out, errb bytes.Buffer
	if rc := run([]string{root}, &out, &errb); rc != 0 {
		t.Fatalf("rc=%d: %s", rc, errb.String())
	}
	if got := strings.Count(out.String(), "\n"); got != 1 || !strings.Contains(out.String(), "重複なし") {
		t.Errorf("1 行の「重複なし」を期待: %q", out.String())
	}
}

func TestRenamedCopyIsReported(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA, "Sources/B.swift": helperB})
	var out, errb bytes.Buffer
	if rc := run([]string{"-format", "json", root}, &out, &errb); rc != 0 {
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
	if len(s.Groups) != 1 || s.Groups[0].Level != "L1" || len(s.Groups[0].Members) != 2 {
		t.Fatalf("名前の違う写しの L1 の組 1 つを期待: %s", out.String())
	}
}

func TestNoFilesIsUndecidable(t *testing.T) {
	root := repoWith(t, map[string]string{"README.md": "x"})
	var out, errb bytes.Buffer
	if rc := run([]string{root}, &out, &errb); rc != 2 || !strings.Contains(errb.String(), "判定不能") {
		t.Errorf("rc=%d (期待 2), stderr=%q", rc, errb.String())
	}
}

func TestXcodeFormat(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA, "Sources/B.swift": helperB})
	var out, errb bytes.Buffer
	if rc := run([]string{"-format", "xcode", root}, &out, &errb); rc != 0 {
		t.Fatalf("rc=%d: %s", rc, errb.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], ": warning: ") || !strings.Contains(lines[1], ": note: ") ||
		!strings.HasPrefix(lines[0], "/") {
		t.Errorf("絶対パスの warning 1 行と note 1 行を期待: %q", out.String())
	}
}

// 読めない構文に飲み込まれた関数は単位として出てこない。1 行の出力でも、比べていない部分があることを出す。
func TestSyntaxErrorIsShownInOneLine(t *testing.T) {
	root := repoWith(t, map[string]string{
		"Sources/A.swift": "struct Z { func f(_ q: Int) -> Int { let a = q * 2 + 3; return a - q } }\n",
		"Sources/B.swift": "struct Y {\n    let x = [1, 2,\n    func g(_ q: Int) -> Int { let a = q * 2 + 3; return a - q }\n}\n",
	})
	var out, errb bytes.Buffer
	if rc := run([]string{"-min-nodes", "3", root}, &out, &errb); rc != 0 {
		t.Fatalf("rc=%d: %s", rc, errb.String())
	}
	if !strings.Contains(out.String(), "読めない構文があるファイル 1") {
		t.Errorf("読めない構文のファイルの件数が出ない: %q", out.String())
	}
	out.Reset()
	if rc := run([]string{"-format", "xcode", "-min-nodes", "3", root}, &out, &errb); rc != 0 || !strings.Contains(out.String(), "B.swift:1:1: note:") {
		t.Errorf("xcode の形式に読めない構文の note が出ない (rc=%d): %q", rc, out.String())
	}
}
