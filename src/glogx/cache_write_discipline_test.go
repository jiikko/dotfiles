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

// `~/.cache/glog` (cacheBaseDir 配下) への書き込みは writeAtomic を通す (issue 219 / 286)。
//
// 素の os.WriteFile は O_TRUNC なので、書き込み中に落ちると**途中まで書けた JSON** が残り、
// 次回起動の復元が黙って失敗する。writeAtomic は temp + rename で、write / Close / rename の
// 3 分岐すべてで temp を掃除する。
//
// 🚨 なぜ機械で縛るか: issue 219 は 2026-09-03 にこの規約を確立したが「ruleguard は構文しか
// 見ないので『どの置き場への書き込みか』を表現できない」としてゲートを見送り、**trigger を
// 書いて閉じた**。その 1〜2 日後に doctor_resume.go (09-04) と ratelimit_resume.go (09-05) が
// 規約の外側で生えた。trigger が発火したので、ここで機械化する。
//
// 🚨 **脅威モデルと射程** (adversarial-review-own-safeguards §8):
//   - 止めるのは「cacheBaseDir / CachePath 由来のパスへ os.WriteFile する」典型形だけ
//   - 判定は 2 段: ①「cacheBaseDir / cachedir.Base を呼ぶ関数」= パス提供関数を集める
//     ②「パス提供関数 (または cacheBaseDir 系) を呼び、かつ os.WriteFile も呼ぶ関数」を違反にする
//   - 🚨 **1 段の近似では実際の違反を 1 件も捕まえられなかった**。saveIssuesScreen 等は
//     `path, _ := issuesStatePath()` と**別関数からパスを受け取る**形なので、
//     「同じ関数内に cacheBaseDir と os.WriteFile が両方ある」では 0 件になる (実測で踏んだ)
//   - **検出しない**: 提供関数を 2 段以上辿る / io.WriteString / os.Create + Write /
//     パスを引数で受け取るだけの関数 / 別 module 経由。これらは review の責務
func TestCacheWritesGoThroughWriteAtomic(t *testing.T) {
	const marker = "cache-write: raw-ok" // 例外はこの語 + 理由を関数内に書く
	fset := token.NewFileSet()

	type fnInfo struct {
		file  *ast.File
		decl  *ast.FuncDecl
		path  string
		calls map[string]bool // この関数が呼ぶ識別子 (メソッドは "pkg.Sel" も入れる)
		write token.Pos       // os.WriteFile の位置 (NoPos = 無い)
	}
	var fns []fnInfo
	scanned := 0

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
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			return perr
		}
		scanned++
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			info := fnInfo{file: file, decl: fn, path: path, calls: map[string]bool{}, write: token.NoPos}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch f := call.Fun.(type) {
				case *ast.Ident:
					info.calls[f.Name] = true
				case *ast.SelectorExpr:
					if x, ok := f.X.(*ast.Ident); ok {
						info.calls[x.Name+"."+f.Sel.Name] = true
						if x.Name == "os" && f.Sel.Name == "WriteFile" {
							info.write = call.Pos()
						}
					}
				}
				return true
			})
			fns = append(fns, info)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("走査できない: %v", err)
	}

	// ① パス提供関数を集める (cacheBaseDir / cachedir.Base を直接呼ぶもの)
	providers := map[string]bool{"cacheBaseDir": true, "cachedir.Base": true, "CachePath": true}
	for _, f := range fns {
		if f.calls["cacheBaseDir"] || f.calls["cachedir.Base"] {
			providers[f.decl.Name.Name] = true
		}
	}

	// ② 提供関数を呼び、かつ os.WriteFile も呼ぶ関数を違反にする
	var offenders []string
	cacheFuncs := 0
	for _, f := range fns {
		usesCacheDir := false
		for name := range f.calls {
			if providers[name] {
				usesCacheDir = true
				break
			}
		}
		if !usesCacheDir {
			continue
		}
		cacheFuncs++
		if f.write == token.NoPos || funcHasComment(f.file, f.decl, marker) {
			continue
		}
		pos := fset.Position(f.write)
		offenders = append(offenders,
			filepath.ToSlash(f.path)+":"+strconv.Itoa(pos.Line)+" ("+f.decl.Name.Name+")")
	}

	// 🚨 抽出が空でも緑にならないよう下限を置く (issue 280 / 283 と同じ形)。
	// 2026-09-06 実測: 非テストの .go 75 件を走査し、cacheBaseDir 系に届く関数は 14 件。
	if scanned < 40 {
		t.Fatalf("走査した .go が %d 件しかない (下限 40)。WalkDir の除外が壊れている", scanned)
	}
	if cacheFuncs < 8 {
		t.Fatalf("cacheBaseDir 系に届く関数を %d 件しか見つけられなかった (下限 8)。判定が壊れている", cacheFuncs)
	}
	if len(offenders) > 0 {
		t.Errorf("~/.cache/glog へ素の os.WriteFile で書いている (writeAtomic を使うこと。"+
			"O_TRUNC なので途中書きの JSON が残る。issue 219 / 286):\n  %s\n"+
			"意図的な例外は関数内に `%s` + 理由を書く", strings.Join(offenders, "\n  "), marker)
	}
	t.Logf("走査 .go=%d 件 / cacheBaseDir 系に届く関数 %d 件 / 違反 %d 件", scanned, cacheFuncs, len(offenders))
}

// funcHasComment は関数の範囲内にその語を含むコメントがあるか。
func funcHasComment(file *ast.File, fn *ast.FuncDecl, want string) bool {
	for _, cg := range file.Comments {
		if cg.Pos() < fn.Pos() || cg.End() > fn.End() {
			continue
		}
		if strings.Contains(cg.Text(), want) {
			return true
		}
	}
	return false
}
