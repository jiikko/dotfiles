package main

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// この module は外部プロセスを subproc.CommandContext (WaitDelay 付き) でしか起動しない。
// 起動口を増やさないことを「非テストの .go が os/exec を import しない」で固定する。
//
// 🚨 なぜ要るか: 起動の規律は glogx の waitdelay_discipline_test.go が持つが、あれは src/glogx の下
// だけを歩く。usage を glogx から出したとき、この module は何にも見られなくなった。
//
// 脅威モデル: 止めるのは「usage / main に exec.Command を直接書く」うっかり (WaitDelay の張り忘れ =
// 孫プロセスがパイプを握って Wait が戻らない。issue 105)。syscall.Exec・reflect・別 module に
// 薄いラッパを作る形は検出しない (glogx の gate と同じく review の責務)。
func TestNoOsExecImport(t *testing.T) {
	// canary: 別名 import も素の import も、同じ判定関数で拾えること
	for _, src := range []string{
		"package x\nimport \"os/exec\"\n",
		"package x\nimport (\n\t\"fmt\"\n\txe \"os/exec\"\n)\n",
	} {
		if !importsOsExec(t, "canary.go", src) {
			t.Fatalf("canary を検出できない (判定が壊れている):\n%s", src)
		}
	}

	var offenders []string
	scanned := 0
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++
		if importsOsExec(t, path, nil) {
			offenders = append(offenders, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// 2026-09-26 実測: 非テストの .go は 8 件 (main.go + usage/)。根がずれると 0 件 = 緑に化ける
	if scanned < 7 {
		t.Fatalf("走査した .go が %d 件しかない (下限 7)。走査の根が壊れている", scanned)
	}
	if len(offenders) > 0 {
		t.Fatalf("os/exec を import している (外部プロセスは subproc.CommandContext で起動する):\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

func importsOsExec(t *testing.T, path string, src any) bool {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, src, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("%s をパースできない: %v", path, err)
	}
	for _, imp := range f.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err == nil && p == "os/exec" {
			return true
		}
	}
	return false
}
