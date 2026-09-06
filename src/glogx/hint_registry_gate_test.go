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
func scanHintLineAssigns(f *ast.File) (assigns int, viaFit int, returns int, found bool) {
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "hintLine" || fn.Body == nil {
			continue
		}
		found = true
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			// 🚨 hintLineText の呼び出し数も数える (敵対的レビュー 2026-09-06 の P2)。
			// 代入を経ずに `return m.hintLineText(prefix + "...")` と書く形は、
			// 代入だけを数える判定を素通りする。しかも本体には既に 5 件の
			// `return m.hintLineText(...)` が並んでいて**コピペの雛形がその場にある**。
			// 実測: 新しい状態を足してこの形で返すと、レジストリにも幅ゲートにも
			// 掛からず全パッケージ緑のまま 279 / 281 の症状が再現した。
			if call, ok := n.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "hintLineText" {
					returns++
				}
			}
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
	return assigns, viaFit, returns, found
}

// hintLineHintLineTextCalls は hintLine 本体の hintLineText 呼び出しの想定数。
// 内訳: 全画面ビューア 4 枚の早期 return + 末尾の 1 件。
//
// 🚨 全画面ビューアを増減させたときだけ更新する。そちらは
// TestFullScreenCasesCoverEveryID が ID の追加を強制するので、更新漏れには気づける。
const hintLineHintLineTextCalls = 5

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
	ca, cv, cr, cfound := scanHintLineAssigns(cf)
	if !cfound || ca != 2 || cv != 1 || cr != 1 {
		t.Fatalf("canary の検出が 代入 %d / fitHintItems 経由 %d / hintLineText %d / 発見 %v "+
			"(期待 2 / 1 / 1 / true)。判定が壊れている", ca, cv, cr, cfound)
	}

	f, err := parser.ParseFile(fset, "tui.go", nil, 0)
	if err != nil {
		t.Fatalf("tui.go をパースできない: %v", err)
	}
	assigns, viaFit, returns, found := scanHintLineAssigns(f)
	if !found {
		t.Fatal("hintLine が見つからない (改名したなら、このゲートの対象名も直すこと)")
	}
	if assigns != 1 || viaFit != 1 {
		t.Errorf("hintLine の中で hint への代入が %d 件 / うち fitHintItems 経由が %d 件 (期待 1 / 1)。"+
			"面ごとの hint は hint_surfaces.go のレジストリから取ること (直接組むと予算計算と幅ゲートを迂回する)",
			assigns, viaFit)
	}
	if returns != hintLineHintLineTextCalls {
		t.Errorf("hintLine の hintLineText 呼び出しが %d 件 (期待 %d = 全画面 4 + 末尾 1)。"+
			"代入を経ずに `return m.hintLineText(...)` で返す形は、レジストリも幅ゲートも通らない",
			returns, hintLineHintLineTextCalls)
	}
	t.Logf("hintLine の hint 代入 %d 件 / fitHintItems 経由 %d 件 / hintLineText %d 件",
		assigns, viaFit, returns)
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

// activeHintSurface() の**分岐の順序**が意味を持つことを、対の状態で固定する。
//
// 🚨 単独の状態だけを並べたテストでは順序を 1 mm も守らない (敵対的レビュー 2026-09-06 の P1)。
// 実測: overlay 3 case を actModal 7 case の上へ移す変異を当てても全パッケージ緑だった。
//
// 対の状態は理論上の話ではない: askRerun() の呼び出し元は job パネルの `r` (tui.go の
// panel キー処理) と job 詳細の `r` の**2 箇所だけ**なので、`rerunConfirm == true` は
// **必ず** `panelSHA != ""` か `detailOv.visible()` と同時に真になる。順序が入れ替わると
// 「job 詳細で r を押した直後、『job を再実行しますか? [Y/n]』が出ないまま y/N を待つ」形になる。
func TestActiveHintSurfacePrecedence(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*browseModel)
		want  hintSurfaceID
	}{
		// actModal は overlay より先 (確認・進行中の案内が最優先)
		{"job 詳細 + 再実行確認", func(m *browseModel) {
			m.detailOv.open = true
			m.actModal.rerunConfirm = true
		}, hintSurfaceRerunConfirm},
		{"job パネル + 再実行確認", func(m *browseModel) {
			m.panelSHA, m.panelCursor = m.commits[0].SHA, 0
			m.actModal.rerunConfirm = true
		}, hintSurfaceRerunConfirm},
		{"job 詳細 + 再実行中", func(m *browseModel) {
			m.detailOv.open = true
			m.actModal.rerunning = true
		}, hintSurfaceRerunning},
		{"diff + push 確認", func(m *browseModel) {
			m.diffOv.sha = m.commits[0].SHA
			m.actModal.pushConfirm = true
		}, hintSurfacePushConfirm},
		{"job パネル + pull 中", func(m *browseModel) {
			m.panelSHA, m.panelCursor = m.commits[0].SHA, -1
			m.actModal.pulling = true
		}, hintSurfacePulling},
		// overlay どうしの前後 (深い方が勝つ)
		{"job パネル + job 詳細", func(m *browseModel) {
			m.panelSHA, m.panelCursor = m.commits[0].SHA, 0
			m.detailOv.open = true
		}, hintSurfaceJobDetail},
		{"job パネル + diff", func(m *browseModel) {
			m.panelSHA, m.panelCursor = m.commits[0].SHA, 0
			m.diffOv.sha = m.commits[0].SHA
		}, hintSurfaceDiff},
		// panel の 2 case (カーソルの有無)
		{"パネル カーソルあり", func(m *browseModel) {
			m.panelSHA, m.panelCursor = m.commits[0].SHA, 0
		}, hintSurfacePanelCursor},
		{"パネル カーソル無し", func(m *browseModel) {
			m.panelSHA, m.panelCursor = m.commits[0].SHA, -1
		}, hintSurfacePanelNoCursor},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestBrowse(t, 1, map[string]CIState{}, nil)
			m.showFrame, m.width, m.height = true, 100, 40
			tc.setup(m)
			if got := m.activeHintSurface(); got != tc.want {
				t.Errorf("面 %d を期待したが %d が返った (分岐の順序が変わっている)", tc.want, got)
			}
		})
	}
	t.Logf("対の状態 %d 件で順序を固定した", len(cases))
}
