package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// glogx 自身が画面へ出す文字列リテラルに VS16 (U+FE0F) を入れない。
//
// VS16 付き記号の表示幅は端末で割れる (実測 2026-09-02 / issue 136: 同じマシンで
// tmux 内は 2 桁、Apple Terminal は 1 桁)。外部由来の文字列は termsafe.DropEmojiVS16 が
// bare へ正規化しているのに、自前のリテラルだけがその関門を通らないまま残っていた
// (`main.go` の起動時警告。issue 136 で検出)。
//
// 「揃える先を grapheme か wc のどちらかに決める」は環境で答えが反転するので成立しない
// (issue 124)。成立するのは「幅が割れる文字を出さない」side なので、それをここで固定する。
func TestOwnStringLiteralsHaveNoVS16(t *testing.T) {
	const vs16 = '️'
	fset := token.NewFileSet()
	// tools/ は幅そのものを測る道具なので VS16 を書いてよい (対象外)。
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("パースできない: %v", err)
	}
	found := 0
	for _, pkg := range pkgs {
		for path, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				if strings.ContainsRune(lit.Value, vs16) {
					found++
					pos := fset.Position(lit.Pos())
					t.Errorf("%s:%d 文字列リテラルに VS16 (U+FE0F) がある: %s\n"+
						"  VS16 付き記号は端末で幅が割れる (issue 136 の実測)。bare 記号へ倒すこと",
						filepath.Base(path), pos.Line, lit.Value)
				}
				return true
			})
		}
	}
	t.Logf("検査した package 数=%d / VS16 を含むリテラル=%d 件", len(pkgs), found)
}
