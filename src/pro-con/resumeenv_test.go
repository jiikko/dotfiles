package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// 引き継ぎの印 (PRO_CON_RESUME) は、プロセスの入口 (runInherited) で何かを起こす前に環境変数から消す (issue 548 の敵対的レビュー)。
// run の中で消すと、画面が先に起こす supervisor・dispatcher・claude が印を受け継ぎ、子の pro-con が alt screen の見張りを張って
// SIGTERM で止める処理を飛ばす (実機で確認: dispatcher が rc 143 で落ち、PG を止めずに抜ける)。exec の順序は単体のテストで作れないので、
// 呼び出しの置き場と順序を固定する。
func TestResumeEnvIsTakenOnlyAtEntry(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var where []string
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "takeResumeEnv" {
					where = append(where, fn.Name.Name)
				}
			}
			return true
		})
	}
	if len(where) != 1 || where[0] != "runInherited" {
		t.Fatalf("takeResumeEnv の呼び出し = %v, want runInherited の 1 か所だけ", where)
	}
	// runInherited の中では、run を呼ぶより前に、defer でなく直に呼ぶ (後で消すと、run の中で起こした子が受け継ぐ)
	var takeAt, runAt token.Pos
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "runInherited" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if ds, ok := n.(*ast.DeferStmt); ok {
				if id, ok := ds.Call.Fun.(*ast.Ident); ok && id.Name == "takeResumeEnv" {
					t.Fatal("takeResumeEnv を defer で呼んでいる (run の後に消すことになる)")
				}
			}
			if c, ok := n.(*ast.CallExpr); ok {
				if id, ok := c.Fun.(*ast.Ident); ok {
					switch {
					case id.Name == "takeResumeEnv" && takeAt == 0:
						takeAt = c.Pos()
					case id.Name == "run" && runAt == 0:
						runAt = c.Pos()
					}
				}
			}
			return true
		})
	}
	if takeAt == 0 || runAt == 0 || takeAt > runAt {
		t.Fatalf("runInherited で takeResumeEnv (%d) を run (%d) より前に呼んでいない", takeAt, runAt)
	}
}

// 検出しない形 (この固定の射程の外。review で見る): 関数の値を経由した呼び出し (f := takeResumeEnv; f())、
// main.go 以外のファイルで os.Getenv(upgrade.ResumeEnv) を読んで子を起こす形、goroutine の中で消す形。
