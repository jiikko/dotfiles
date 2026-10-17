package main

import (
	"go/ast"
)

// recvTypeName は型の式から型の名前を取る (`*T` / `T[K]` / `(T)` も T)。取れなければ ""。
func recvTypeName(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}
