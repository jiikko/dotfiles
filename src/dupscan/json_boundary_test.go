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

// dupscan が書く JSON は internal/jsonout だけが作る (`<` `>` `&` を escape しない形を 1 か所で決めるため)。
// 非テストの .go が jsonout の外で json.Marshal / MarshalIndent / NewEncoder を呼んだら落とす。読む側 (NewDecoder・Unmarshal) は見ない。
// 検出しない形: encoding/json の別名 import・reflect 経由・別のパッケージに作った薄い wrapper (うっかりの退行だけを止める)。
func TestJSONIsWrittenOnlyByJSONOut(t *testing.T) {
	writers := map[string]bool{"Marshal": true, "MarshalIndent": true, "NewEncoder": true}
	var scanned, insideHelper int
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (path == "third_party" || strings.HasPrefix(d.Name(), ".")) && path != "." {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		scanned++
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || !writers[sel.Sel.Name] {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "json" {
				return true
			}
			if filepath.Dir(path) == filepath.Join("internal", "jsonout") {
				insideHelper++
				return true
			}
			t.Errorf("%s: json.%s を jsonout の外で呼んでいる (jsonout.Write / jsonout.Line を使う)", path, sel.Sel.Name)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// 走査が空振りしていない (jsonout 自身の NewEncoder を見つけた) ことを確かめてから、違反 0 件を信じる
	if scanned < 10 || insideHelper == 0 {
		t.Fatalf("走査が空振りした: 読んだファイル %d・jsonout の中の書き手 %d", scanned, insideHelper)
	}
}
