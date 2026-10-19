package main

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// `~/.cache/glog` (app.CacheBaseDir 配下) への書き込みは app.WriteAtomic を通す。
//
// 素の os.WriteFile は O_TRUNC なので、書き込み中に落ちると**途中まで書けた JSON** が残り、
// 次回起動の復元が黙って失敗する。app.WriteAtomic は temp + rename で、write / Close / rename の
// 3 分岐すべてで temp を掃除する。
//
// 🚨 なぜ機械で縛るか: 2026-09-03 にこの規約を確立したが「ruleguard は構文しか
// 見ないので『どの置き場への書き込みか』を表現できない」としてゲートを見送り、**trigger を
// 書いて閉じた**。その 1〜2 日後に doctor_resume.go (09-04) と ratelimit_resume.go (09-05) が
// 規約の外側で生えた。trigger が発火したので、ここで機械化する。
//
// 🚨 **脅威モデルと射程** (adversarial-review-own-safeguards §8):
//   - 止めるのは「app.CacheBaseDir / CachePath 由来のパスへ os.WriteFile する」典型形だけ
//   - 判定は 2 段: ①「app.CacheBaseDir / cachedir.Base を呼ぶ関数」= パス提供関数を集める
//     ②「パス提供関数 (または app.CacheBaseDir 系) を呼び、かつ os.WriteFile も呼ぶ関数」を違反にする
//   - 🚨 **1 段の近似では実際の違反を 1 件も捕まえられなかった**。saveIssuesScreen 等は
//     `path, _ := issuesMemory.path()` と**別関数からパスを受け取る**形なので、
//     「同じ関数内に app.CacheBaseDir と os.WriteFile が両方ある」では 0 件になる (実測で踏んだ)
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
	// 下限は実件数のすぐ下に置く (遠いと、対象がかなり縮んでも緑のまま)。2026-10-19 実測: 本番の .go 92 件
	for _, mf := range parseModuleNonTestFiles(t, fset, 85) {
		file, path := mf.file, mf.path
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
	}

	// ① パス提供関数を集める (app.CacheBaseDir / cachedir.Base を直接呼ぶもの)
	// 🚨 app.CacheBaseDir は internal/app へ移した。main からは app.CacheBaseDir、app の中からは CacheBaseDir と呼ぶ
	providers := map[string]bool{"app.CacheBaseDir": true, "CacheBaseDir": true, "cachedir.Base": true, "CachePath": true}
	for _, f := range fns {
		if f.calls["app.CacheBaseDir"] || f.calls["CacheBaseDir"] || f.calls["cachedir.Base"] {
			providers[f.decl.Name.Name] = true
		}
	}

	// ② 提供関数を呼び、かつ os.WriteFile も呼ぶ関数を違反にする
	offenders := make([]string, 0, len(fns))
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

	if cacheFuncs < 8 {
		t.Fatalf("app.CacheBaseDir 系に届く関数を %d 件しか見つけられなかった (下限 8)。判定が壊れている", cacheFuncs)
	}
	if len(offenders) > 0 {
		t.Errorf("~/.cache/glog へ素の os.WriteFile で書いている (app.WriteAtomic を使うこと。"+
			"O_TRUNC なので途中書きの JSON が残る。issue 219 / 286):\n  %s\n"+
			"意図的な例外は関数内に `%s` + 理由を書く", strings.Join(offenders, "\n  "), marker)
	}
	t.Logf("app.CacheBaseDir 系に届く関数 %d 件 / 違反 %d 件", cacheFuncs, len(offenders))
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
