package main

import (
	"strings"
	"testing"
)

// index の注意書きは、出した結果に関わるものだけにする。
// Swift の既定引数の注意は、結果に Swift の単位があるときだけ (Go のファイルだけの結果に出さない)。
// 名前を指定しない -file の outline は、並べた単位の名前を含む読めないファイルだけを「影響しうる」と数える。

const swiftDefaultArgNote = "Swiftの既定引数を省略した呼び出しは取りこぼす"

func TestIndexSwiftNoteOnlyWhenResultsHaveSwiftUnits(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a.go":      "package a\n\nfunc runIt() {}\n\nfunc use() { runIt() }\n",
		"Box.swift": "func other() {}\n",
	})
	for _, tc := range []struct {
		name string
		args []string
		want bool
	}{
		{"go_file", []string{"index", "-file", root + "/a.go"}, false},
		{"go_name", []string{"index", "-name", "runIt"}, false},
		{"swift_file", []string{"index", "-file", root + "/Box.swift"}, true},
		{"no_match_keeps_scope_rule", []string{"index", "-name", "missing"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, out, _ := runReport(t, append(tc.args, root)...)
			first, _, _ := strings.Cut(out, "\n")
			if got := strings.Contains(first, swiftDefaultArgNote); got != tc.want {
				t.Errorf("Swift の注意の有無 = %v, want %v: %q", got, tc.want, first)
			}
		})
	}
}

func TestIndexFileOutlineIgnoresUnrelatedUnreadableFiles(t *testing.T) {
	root := repoWith(t, map[string]string{
		"a.go":                 "package a\n\nfunc outlineTarget() {}\n",
		"testdata/broken/b.go": "package b\n\nfunc broken( {\n",
		"mentions/c.go":        "package c\n\nfunc alsoBroken( { outlineTarget()\n",
	})
	_, _, errs := runReport(t, "index", "-file", root+"/a.go", root)
	if !strings.Contains(errs, "解析不完全") || !strings.Contains(errs, "mentions/c.go") {
		t.Fatalf("並べた単位の名前を含む読めないファイルは全文の警告に残す: %q", errs)
	}

	root = repoWith(t, map[string]string{
		"a.go":                 "package a\n\nfunc outlineTarget() {}\n",
		"testdata/broken/b.go": "package b\n\nfunc broken( {\n",
	})
	_, _, errs = runReport(t, "index", "-file", root+"/a.go", root)
	if errs != "dupscan index: 読めないファイル 1 個は並べた単位の名前を含まない\n" {
		t.Errorf("並べた単位の名前を含まない読めないファイルは 1 行にする: %q", errs)
	}

	_, _, errs = runReport(t, "index", "-file", root+"/testdata/broken/b.go", root)
	if !strings.Contains(errs, "解析不完全") {
		t.Errorf("指定したファイル自身が読めないときは全文の警告: %q", errs)
	}
}
