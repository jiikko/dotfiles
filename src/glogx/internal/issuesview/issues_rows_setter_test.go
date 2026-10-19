package issuesview

import (
	"glogx/internal/testenv"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// issuesView.rows は setRows 経由でしか書き換えない。
//
// なぜ機械で縛るか: 世代 (rowsGen) が進まないと ensureDisplayRows が「同期済み」と判断し、
// **displayRows が古いまま描かれる**。build もテストも通るので silent に壊れる。
// 「書く人が覚えているか」に依存させない (waitdelay_discipline_test.go と同じ形)。
//
// 🚨 **脅威モデルと射程** (adversarial-review-own-safeguards §8):
//   - 止めるのは「うっかり `v.rows = xxx` と直接書く」典型形だけ
//   - 見るのは 3 形: `v.rows = x` (直接代入) / `&issuesView{rows: ...}` (複合リテラル) /
//     `v.rows[i] = x` (要素の書き換え)
//   - **検出しない**: 別名の変数経由 (`p := v; p.rows = ...`)、reflect、別 struct への埋め込み経由、
//     issuesView 以外の型に見えるレシーバ経由、`sort.Slice(v.rows, ...)` のような
//     **関数へ渡してから中で並べ替える**形。これらは review の責務
//   - 🚨 要素の書き換えと in-place ソートは**長さが変わらない**ので、世代でも
//     全件比較でも観測できない。ここで字句的に止めるのが唯一の防御
//   - 判定は「その代入を含む関数の**レシーバが issuesView**」「**引数の型が *issuesView**」
//     「同じ関数内で `newTestIssuesView()` / `&issuesView{...}` から得た変数 (代入・var 宣言の
//     どちらでも)」に限る (型解決に go/types を持ち込まない代わりの近似)
//   - 🚨 **この射程は実装後に実物と突き合わせて直したもの** (§8「『検出しない』は実装後に
//     もう一度突き合わせる」)。初版は owners をレシーバと AssignStmt からしか埋めておらず、
//     `func seed(v *issuesView, …) { v.rows = … }` と `var v = newTestIssuesView()` を
//     **「検出しない」に挙げないまま素通し**していた (実測 2026-09-06)。
//     どちらも「うっかり書く典型形」なので、除外ではなく検出側へ足した
//
// 検査が照合する名前 (型・フィールド・setter・テストの作り手)。🚨 名前を変えたらここも直す: 直し忘れると
// canary (下) が 7 件を検出できずに落ちる。
const (
	rowsOwner    = "IssuesView"
	rowsField    = "rows"
	rowsSetter   = "setRows"
	rowsTestCtor = "newTestIssuesView"
)

// scanIssuesRowsWrites は 1 ファイル分の違反と候補数を返す (本走査と canary の共通経路)。
//
// 🚨 canary は**この関数を通す**こと。式をコピーして別に書くと、canary は「コピーした
// ロジック」を検査するだけで本走査の破損を検出しない。
func scanIssuesRowsWrites(fset *token.FileSet, path string, file *ast.File) (offenders []string, candidates int) {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		owners := map[string]bool{}
		if fn.Recv != nil && len(fn.Recv.List) > 0 && testenv.RecvTypeName(fn.Recv.List[0].Type) == rowsOwner {
			for _, n := range fn.Recv.List[0].Names {
				owners[n.Name] = true
			}
		}
		// 🚨 引数で受け取った *issuesView も owner に数える。
		// `func seed(v *issuesView, list []*issues.Issue) { v.rows = list }` は
		// 「うっかり書く典型形」そのものなのに、レシーバと AssignStmt の RHS からしか
		// owners を埋めていなかったので**素通りしていた** (ヘッダの「検出しない」にも
		// 挙げていなかった = 宣言した射程と実射程のズレ)。
		if fn.Type != nil && fn.Type.Params != nil {
			for _, f := range fn.Type.Params.List {
				if testenv.RecvTypeName(f.Type) != rowsOwner {
					continue
				}
				for _, n := range f.Names {
					owners[n.Name] = true
				}
			}
		}
		// Fixture (main のテスト用の口) の受け手は `f.v` が issuesView を指す (Fixture に setter を足すと
		// 素通りする)
		fixtureRecv := map[string]bool{}
		if fn.Recv != nil && len(fn.Recv.List) > 0 && testenv.RecvTypeName(fn.Recv.List[0].Type) == "Fixture" {
			for _, n := range fn.Recv.List[0].Names {
				fixtureRecv[n.Name] = true
			}
		}
		isOwner := func(x ast.Expr) bool {
			switch b := x.(type) {
			case *ast.Ident:
				return owners[b.Name]
			case *ast.SelectorExpr:
				id, ok := b.X.(*ast.Ident)
				return ok && fixtureRecv[id.Name] && b.Sel.Name == "v"
			}
			return false
		}
		report := func(pos token.Pos, what string) {
			candidates++
			if fn.Name.Name == rowsSetter {
				return
			}
			p := fset.Position(pos)
			offenders = append(offenders,
				filepath.ToSlash(path)+":"+strconv.Itoa(p.Line)+" ("+fn.Name.Name+", "+what+")")
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			// 複合リテラルで rows を直接埋める形 (&issuesView{rows: ...})。
			// 🚨 世代が 0 のまま displayRows と食い違うので、ensureDisplayRows の
			// 自己回復に頼ることになる
			if cl, ok := n.(*ast.CompositeLit); ok && testenv.RecvTypeName(cl.Type) == rowsOwner {
				for _, elt := range cl.Elts {
					if kv, ok := elt.(*ast.KeyValueExpr); ok {
						if k, ok := kv.Key.(*ast.Ident); ok && k.Name == rowsField {
							report(kv.Pos(), "複合リテラル")
						}
					}
				}
			}
			// var v = newTestIssuesView() / var v *issuesView — AssignStmt ではないので別に拾う
			if vs, ok := n.(*ast.ValueSpec); ok {
				owned := testenv.RecvTypeName(vs.Type) == rowsOwner
				for i, name := range vs.Names {
					if owned || (i < len(vs.Values) && rhsMakesIssuesView(vs.Values[i])) {
						owners[name.Name] = true
					}
				}
			}
			as, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i, lhs := range as.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && i < len(as.Rhs) && rhsMakesIssuesView(as.Rhs[i]) {
					owners[id.Name] = true
				}
			}
			for _, lhs := range as.Lhs {
				// v.rows = x
				if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == rowsField {
					if isOwner(sel.X) {
						report(sel.Pos(), "直接代入")
					}
					continue
				}
				// v.rows[i] = x (要素の書き換え。長さが同じなので世代でも比較でも見えない)
				if idx, ok := lhs.(*ast.IndexExpr); ok {
					if sel, ok := idx.X.(*ast.SelectorExpr); ok && sel.Sel.Name == rowsField {
						if isOwner(sel.X) {
							report(idx.Pos(), "要素の書き換え")
						}
					}
				}
			}
			return true
		})
	}
	return offenders, candidates
}

func TestIssuesRowsGoThroughSetter(t *testing.T) {
	fset := token.NewFileSet()
	var offenders []string
	scanned, candidates := 0, 0

	// 🚨 canary: 既知の違反を**本走査と同じ関数**に通して、検出できることを先に確かめる。
	// これが無いと、判定が壊れて 0 件になっても「違反 0 件 = 緑」で通る。
	canarySrc := strings.NewReplacer("OWNER", rowsOwner, "FIELD", rowsField, "CTOR", rowsTestCtor).Replace(`package issuesview
type zzView struct{}
func (v *OWNER) zzCanaryDirect() { v.FIELD = nil }
func zzCanaryLiteral() *OWNER { return &OWNER{FIELD: nil} }
func (v *OWNER) zzCanaryIndex() { v.FIELD[0] = nil }
func zzCanaryTestStyle() { v := CTOR(); v.FIELD = nil }
func zzCanaryParam(v *OWNER) { v.FIELD = nil }
func zzCanaryParamIndex(v *OWNER) { v.FIELD[0] = nil }
func zzCanaryVarDecl() { var v = CTOR(); v.FIELD = nil }
func (f Fixture) zzCanaryFixture() { f.v.FIELD = nil }
func zzCanaryNew() { v := NewIssuesView(); v.FIELD = nil }
`)
	canaryFile, cerr := parser.ParseFile(fset, "zz_canary.go", canarySrc, 0)
	if cerr != nil {
		t.Fatalf("canary をパースできない: %v", cerr)
	}
	canaryHits, _ := scanIssuesRowsWrites(fset, "zz_canary.go", canaryFile)
	if len(canaryHits) != 9 {
		t.Fatalf("canary の検出が %d 件 (期待 9: 直接代入 / 複合リテラル / 要素の書き換え / "+
			"テストが作った変数経由 / 引数経由 x2 / var 宣言経由 / Fixture 経由 / NewIssuesView 経由)。判定が壊れている:\n  %s",
			len(canaryHits), strings.Join(canaryHits, "\n  "))
	}

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
		o, c := scanIssuesRowsWrites(fset, path, file)
		offenders = append(offenders, o...)
		candidates += c
		return nil
	})
	if err != nil {
		t.Fatalf("走査できない: %v", err)
	}

	// 🚨 走査が壊れて何も見なくなっても緑にならないよう、下限を置く。
	// 2026-09-06 実測: .go 167 件 (テスト込み。当時は main) を走査し、issuesView.rows への代入候補は setRows の 1 件。
	// 2026-10-19: このパッケージの 29 件を走査する (下限は実件数のすぐ下)
	// (rows を触れるのはこのパッケージの中だけ。main からは型の外の名前に見えない)
	if scanned < 25 {
		t.Fatalf("走査した .go が %d 件しかない (下限 25)。WalkDir の除外が壊れている", scanned)
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

// rhsMakesIssuesView は式が issuesView を作るか (`newTestIssuesView()` / `&issuesView{}`)。
func rhsMakesIssuesView(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.CallExpr:
		// テストの作り手と、本番の作り手 (NewIssuesView) / 読み込み済みのテストの作り手 (loadedView)
		if id, ok := v.Fun.(*ast.Ident); ok && (id.Name == rowsTestCtor || id.Name == "NewIssuesView" || id.Name == "loadedView") {
			return true
		}
	case *ast.UnaryExpr:
		return rhsMakesIssuesView(v.X)
	case *ast.CompositeLit:
		return testenv.RecvTypeName(v.Type) == rowsOwner
	}
	return false
}
