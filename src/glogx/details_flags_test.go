package main

import (
	"glogx/internal/gitdata"
	"glogx/internal/testenv"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// detailsWaiting は detailsLoading の部分集合 (tui.go の宣言)。片方だけ消すと
// 「waiting=true / loading=false」の孤児が残り、fetchPanelDetails の早期 return が外れて
// 実取得が飛ぶ。その札を startCIFetch の世代交代が落とすので、**取得中なのに
// 「(CI job 情報なし)」**になり、開き直すと同一 SHA へ 2 本目の GraphQL が飛ぶ。
//
// 272 のコメントはこの形を名指しで禁じていたが、detailMsg / basisMsg が loading しか
// 消していなかったため実際に起きた。
func TestDetailsFlagsStayPaired(t *testing.T) {
	m := newTestBrowse(t, 3, map[string]gitdata.CIState{}, nil)
	m.usageOv.Fixture().SetSnapshot(testenv.RatelimitTestSnap())
	m.hasRepo = true
	sha := m.commits[0].SHA

	for _, tc := range []struct {
		name string
		msg  tea.Msg
	}{
		{"detailMsg", detailMsg{sha: sha, batch: gitdata.CIBatch{}}},
		{"basisMsg", basisMsg{targets: []string{sha}, batch: gitdata.CIBatch{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m.detailsLoading, m.detailsWaiting = map[string]bool{}, map[string]bool{}
			m.toFetch, m.pendingFetches = []string{sha}, 1
			m.fetchPanelDetails(sha) // 待機分岐: loading + waiting
			if !m.detailsLoading[sha] || !m.detailsWaiting[sha] {
				t.Fatalf("前提が壊れている: loading=%v waiting=%v", m.detailsLoading[sha], m.detailsWaiting[sha])
			}
			m.Update(tc.msg)
			if m.detailsWaiting[sha] && !m.detailsLoading[sha] {
				t.Errorf("孤児ができた (waiting=true / loading=false)。対で消していない")
			}
		})
	}

	// 陽性対照: 生きている待機札は世代交代で残る (「常に両方消す」実装でも通らないように)
	// 🚨 details を消してから札を立て直すこと。上のサブテストが m.details[sha] を埋めるので、
	// そのままだと fetchPanelDetails が「もう持っている」で早期 return し、札が 1 つも
	// 立たないまま対照が通る (= 何も主張しない)。
	m.detailsLoading, m.detailsWaiting = map[string]bool{}, map[string]bool{}
	delete(m.details, sha)
	m.toFetch, m.pendingFetches = []string{sha}, 1
	m.fetchPanelDetails(sha)
	if !m.detailsLoading[sha] || !m.detailsWaiting[sha] {
		t.Fatalf("対照の前提が壊れている: 札が立っていない (loading=%v waiting=%v)",
			m.detailsLoading[sha], m.detailsWaiting[sha])
	}
	m.startCIFetch([]string{sha})
	if !m.detailsLoading[sha] || !m.detailsWaiting[sha] {
		t.Errorf("新しい集合に残る待機札を落とした: loading=%v waiting=%v",
			m.detailsLoading[sha], m.detailsWaiting[sha])
	}
}

// scanDetailsFlagDeletes は 1 ファイル分の「clearDetailsFlags の外にある直接 delete」を返す。
// 🚨 canary は**この関数を通す**こと (式をコピーすると本走査の破損を検出しない)。
func scanDetailsFlagDeletes(fset *token.FileSet, path string, f *ast.File) (offenders []string, seen int) {
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || id.Name != "delete" || len(call.Args) != 2 {
				return true
			}
			sel, ok := call.Args[0].(*ast.SelectorExpr)
			if !ok || (sel.Sel.Name != "detailsLoading" && sel.Sel.Name != "detailsWaiting") {
				return true
			}
			seen++
			if fn.Name.Name == "clearDetailsFlags" {
				return true
			}
			offenders = append(offenders, filepath.Base(path)+":"+fn.Name.Name+" が "+sel.Sel.Name+" を直接 delete している")
			return true
		})
	}
	return offenders, seen
}

// 「対で消す」責務を clearDetailsFlags 1 箇所に閉じ込める。ここを迂回して片方だけ消す
// コードが増えると、上の unit テストが見ていない経路で孤児が復活する。
func TestDetailsFlagDeletesGoThroughHelper(t *testing.T) {
	fset := token.NewFileSet()

	// canary: 既知の違反を本走査と同じ関数へ通す
	const canarySrc = `package main
func zzCanaryDirect(m *browseModel, sha string) { delete(m.detailsLoading, sha) }
func zzCanaryWaiting(m *browseModel, sha string) { delete(m.detailsWaiting, sha) }
func clearDetailsFlags(m *browseModel, sha string) { delete(m.detailsLoading, sha) }
`
	cf, err := parser.ParseFile(fset, "zz_canary.go", canarySrc, 0)
	if err != nil {
		t.Fatalf("canary をパースできない: %v", err)
	}
	hits, cseen := scanDetailsFlagDeletes(fset, "zz_canary.go", cf)
	if len(hits) != 2 || cseen != 3 {
		t.Fatalf("canary の検出が違反 %d 件 / 走査 %d 件 (期待 2 / 3)。判定が壊れている:\n  %s",
			len(hits), cseen, strings.Join(hits, "\n  "))
	}

	var offenders []string
	seen := 0
	// 下限は実件数のすぐ下に置く (遠いと、対象がかなり縮んでも緑のまま)。2026-10-19 実測: 本番の .go 92 件
	for _, mf := range parseModuleNonTestFiles(t, fset, 85) {
		o, s := scanDetailsFlagDeletes(fset, mf.path, mf.file)
		offenders = append(offenders, o...)
		seen += s
	}
	if seen == 0 {
		t.Fatal("delete(detailsLoading/detailsWaiting) を 1 件も見つけられなかった (helper 自身が見えていない = 判定が壊れている)")
	}
	if len(offenders) > 0 {
		t.Errorf("札は clearDetailsFlags で対にして消すこと (片方だけ消すと孤児が残る。issue 272):\n  %s",
			strings.Join(offenders, "\n  "))
	}
	t.Logf("札の delete %d 件 / 違反 %d 件", seen, len(offenders))
}
