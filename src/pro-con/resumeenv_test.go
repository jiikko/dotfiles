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
// 呼び出しの置き場を固定する。
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
}
