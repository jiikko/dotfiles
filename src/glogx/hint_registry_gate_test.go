package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// hintLine は面ごとの hint を**レジストリからしか**取らない (issue 289)。
//
// なぜ機械で縛るか: 直接 `hint = "..."` と書くと、予算計算 (fitHintItems) と幅ゲートの
// 両方を迂回できる。迂回した分岐は末尾 = 出口が黙って切れるので、279 / 281 の P1 2 本と
// 同じ形が再生産される。build もテストも通るため silent に壊れる。
//
// 🚨 **脅威モデルと射程** (adversarial-review-own-safeguards §8。実装前に書いたもの):
//   - 止めるのは「hintLine の中で hint を直接組む」典型形だけ
//   - 判定: hintLine の本体で `hint` への代入は **1 回だけ**で、その右辺は
//     `fitHintItems(...)` の呼び出しであること
//   - **検出しない**: hintLine の外の関数で組んでから渡す形 / レジストリの builder が
//     返す項目の中身 (語の妥当性・実行時の長さ) / 全画面ビューア 4 枚の早期 return
//     (前置を積まないのが意図で、幅は fullScreenCases 経由で別に検査している)。
//     これらは review と幅ゲートの責務
//   - この射程は実装後に実物と突き合わせて確定させた
func scanHintLineAssigns(f *ast.File) (assigns int, viaFit int, found bool) {
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "hintLine" || fn.Body == nil {
			continue
		}
		found = true
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i, lhs := range as.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || id.Name != "hint" {
					continue
				}
				assigns++
				if i >= len(as.Rhs) {
					continue
				}
				call, ok := as.Rhs[i].(*ast.CallExpr)
				if !ok {
					continue
				}
				if fid, ok := call.Fun.(*ast.Ident); ok && fid.Name == "fitHintItems" {
					viaFit++
				}
			}
			return true
		})
	}
	return assigns, viaFit, found
}

func TestHintLineHasNoInlineHintText(t *testing.T) {
	fset := token.NewFileSet()

	// 🚨 canary: 既知の違反を**本走査と同じ関数**に通して、検出できることを先に確かめる
	// (判定が壊れて 0 件になっても「違反 0 件 = 緑」で通るのを塞ぐ)。
	const canarySrc = `package main
func (m *browseModel) hintLine() string {
	hint := fitHintItems(hw, hintBuilders[m.activeHintSurface()](m))
	if x {
		hint = "直接組んだ案内"
	}
	return m.hintLineText(hint)
}
`
	cf, err := parser.ParseFile(fset, "zz_canary.go", canarySrc, 0)
	if err != nil {
		t.Fatalf("canary をパースできない: %v", err)
	}
	ca, cv, cfound := scanHintLineAssigns(cf)
	if !cfound || ca != 2 || cv != 1 {
		t.Fatalf("canary の検出が 代入 %d / fitHintItems 経由 %d / 発見 %v (期待 2 / 1 / true)。判定が壊れている",
			ca, cv, cfound)
	}

	f, err := parser.ParseFile(fset, "tui.go", nil, 0)
	if err != nil {
		t.Fatalf("tui.go をパースできない: %v", err)
	}
	assigns, viaFit, found := scanHintLineAssigns(f)
	if !found {
		t.Fatal("hintLine が見つからない (改名したなら、このゲートの対象名も直すこと)")
	}
	if assigns != 1 || viaFit != 1 {
		t.Errorf("hintLine の中で hint への代入が %d 件 / うち fitHintItems 経由が %d 件 (期待 1 / 1)。"+
			"面ごとの hint は hint_surfaces.go のレジストリから取ること (直接組むと予算計算と幅ゲートを迂回する)",
			assigns, viaFit)
	}
	t.Logf("hintLine の hint 代入 %d 件 / fitHintItems 経由 %d 件", assigns, viaFit)
}

// レジストリの面がすべて「幅が足りなくても消えない語」を持つこと。
// fitHintItems は入らない項目を落とすので、優先度 1 が無い面は狭い幅で丸ごと消える。
func TestEveryHintSurfaceHasPrioOne(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.actModal.updating = map[string]bool{"claude": true} // updating 面の項目を非空にする
	checked := 0
	for id := hintSurfaceID(0); id < hintSurfaceCount; id++ {
		build := hintBuilders[id]
		if build == nil {
			continue // TestHintSurfacesCoverEveryID が別に落とす
		}
		items := build(m)
		if len(items) == 0 {
			t.Errorf("%s: hint 項目が空", hintSurfaceSetup[id].name)
			continue
		}
		if shortestPrio1(items) == "" {
			t.Errorf("%s: 優先度 1 の項目が無い (狭い幅で案内が丸ごと消える)", hintSurfaceSetup[id].name)
		}
		checked++
	}
	// 🚨 走査 0 件 = 緑 を塞ぐ
	if checked != int(hintSurfaceCount) {
		t.Fatalf("検査した面が %d 件 (期待 %d)", checked, hintSurfaceCount)
	}
	if !strings.Contains(hintBuilders[hintSurfaceBase](m)[len(hintBuilders[hintSurfaceBase](m))-1].text, "終了") {
		t.Error("基底一覧の最後の項目が出口でなくなっている (並び順の前提が変わった)")
	}
	t.Logf("面 %d 件すべてに優先度 1 の項目がある", checked)
}
