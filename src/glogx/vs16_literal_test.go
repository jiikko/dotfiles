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
	found, checked := 0, 0
	// 🚨 `parser.ParseDir` を使わないこと (issue 283)。あれは**非再帰**なので走査対象が
	// `package main` だけになり、usage/ issues/ termwidth/ widthenv/ subproc/ sgr/ が
	// 黙って対象外になる (実測: usage/banner.go に VS16 を置いても緑、box.go だと赤)。
	// しかも `len(pkgs) == 0` の 0 件ガードは package main が常に在るので**構造的に発火しない**。
	// width_test.go:TestNoSecondWidthEngine と同じ WalkDir + ParseFile へ揃える。
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// tools/ は幅そのものを測る道具なので VS16 を書いてよい (トップレベルのみ)。
			if path == "tools" || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		checked++
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
					filepath.ToSlash(path), pos.Line, lit.Value)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("走査できない: %v", err)
	}
	// 🚨 下限は 0 でなく実件数の近くに置く (issue 280 / 283)。走査が縮んでも
	// `checked == 0` では落ちず「違反 0 件 = 緑」になる。増える分には落とさない。
	// 2026-09-06 実測: 再帰 75 件 / トップレベルのみ (= ParseDir へ戻る退行) 53 件。
	// 下限はその間に置く — 「退行後の値では落ちる」ことが下限の存在意義。
	const minChecked = 60
	if checked < minChecked {
		t.Fatalf("走査した .go が %d 件しかない (下限 %d)。WalkDir の除外かフィルタが壊れている", checked, minChecked)
	}
	t.Logf("検査した .go=%d 件 / VS16 を含むリテラル=%d 件", checked, found)
}
