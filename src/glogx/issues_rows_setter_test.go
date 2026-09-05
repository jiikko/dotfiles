package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// issuesView.rows は setRows 経由でしか書き換えない (issue 268)。
//
// なぜ機械で縛るか: 世代 (rowsGen) が進まないと ensureDisplayRows が「同期済み」と判断し、
// **displayRows が古いまま描かれる**。build もテストも通るので silent に壊れる。
// 「書く人が覚えているか」に依存させない (waitdelay_discipline_test.go と同じ形)。
//
// 🚨 **脅威モデルと射程** (adversarial-review-own-safeguards §8):
//   - 止めるのは「うっかり `v.rows = xxx` と直接書く」典型形だけ
//   - **検出しない**: 別名の変数経由 (`p := v; p.rows = ...`)、reflect、別 struct への埋め込み経由、
//     issuesView 以外の型に見えるレシーバ経由。これらは review の責務
//   - 判定は「その代入を含む関数の**レシーバが issuesView**」または
//     「同じ関数内で `newTestIssuesView()` / `&issuesView{...}` から得た変数」に限る
//     (型解決に go/types を持ち込まない代わりの近似)
func TestIssuesRowsGoThroughSetter(t *testing.T) {
	fset := token.NewFileSet()
	var offenders []string
	scanned, candidates := 0, 0

	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == "tools" || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		scanned++
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			// この関数の中で issuesView を指すと分かっている識別子
			owners := map[string]bool{}
			if fn.Recv != nil && len(fn.Recv.List) > 0 && recvTypeName(fn.Recv.List[0].Type) == "issuesView" {
				for _, n := range fn.Recv.List[0].Names {
					owners[n.Name] = true
				}
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				as, ok := n.(*ast.AssignStmt)
				if !ok {
					return true
				}
				// テストが `v := newTestIssuesView()` / `v := &issuesView{}` で作った変数を覚える
				for i, lhs := range as.Lhs {
					id, ok := lhs.(*ast.Ident)
					if !ok || i >= len(as.Rhs) {
						continue
					}
					if rhsMakesIssuesView(as.Rhs[i]) {
						owners[id.Name] = true
					}
				}
				for _, lhs := range as.Lhs {
					sel, ok := lhs.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "rows" {
						continue
					}
					base, ok := sel.X.(*ast.Ident)
					if !ok || !owners[base.Name] {
						continue
					}
					candidates++
					// setRows の実装本体だけが直接代入してよい
					if fn.Name.Name == "setRows" {
						continue
					}
					pos := fset.Position(sel.Pos())
					offenders = append(offenders,
						filepath.ToSlash(path)+":"+strconv.Itoa(pos.Line)+" ("+fn.Name.Name+")")
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("走査できない: %v", err)
	}

	// 🚨 走査が壊れて何も見なくなっても緑にならないよう、下限を置く (issue 280 / 283 と同じ形)。
	// 2026-09-06 実測: .go 75 件超を走査し、issuesView.rows への代入候補は setRows の 1 件。
	if scanned < 60 {
		t.Fatalf("走査した .go が %d 件しかない (下限 60)。WalkDir の除外が壊れている", scanned)
	}
	if candidates == 0 {
		t.Fatal("issuesView.rows への代入を 1 件も見つけられなかった (setRows 自身が見えていない = 判定が壊れている)")
	}
	if len(offenders) > 0 {
		t.Errorf("issuesView.rows を直接書き換えている (setRows を使うこと。世代が進まないと "+
			"displayRows が古いまま描かれる。issue 268):\n  %s", strings.Join(offenders, "\n  "))
	}
	t.Logf("走査 .go=%d 件 / issuesView.rows への代入 %d 件 / 違反 %d 件", scanned, candidates, len(offenders))
}

func recvTypeName(e ast.Expr) string {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// rhsMakesIssuesView は式が issuesView を作るか (`newTestIssuesView()` / `&issuesView{}`)。
func rhsMakesIssuesView(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.CallExpr:
		if id, ok := v.Fun.(*ast.Ident); ok && id.Name == "newTestIssuesView" {
			return true
		}
	case *ast.UnaryExpr:
		return rhsMakesIssuesView(v.X)
	case *ast.CompositeLit:
		return recvTypeName(v.Type) == "issuesView"
	}
	return false
}
