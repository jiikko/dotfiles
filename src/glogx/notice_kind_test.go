package main

import (
	"go/ast"
	"go/printer"
	"go/token"
	"strings"
	"testing"
)

// viewer の通知の種類 (app.NoticeKind) が、showWarning の doc の 3 分類どおりに付いていることを、
// setNotice の全呼び出しについて固定する (分類の誤りは 30 件中 2 件の
// 経路でしかテストされないと、「本文を読めませんでした」を app.NoticeRefused にしても緑だった)。
//
//   - エラー詳細 (app.FirstLine(err…)) を含む通知は app.NoticeError (w でコピーできるよう lastWarning に積む)
//   - クリップボード失敗 (app.ClipboardFailText) は app.NoticeRefused (lastWarning を汚さない)
//   - それ以外の失敗は app.NoticeRefused。例外は Msg 経路で置く 2 件 (noticeErrorWithoutDetail):
//     次の打鍵が q だとトーストを読む前に終わるので、理由を lastWarning に残す
func TestSetNoticeKindsFollowWarningClassification(t *testing.T) {
	noticeErrorWithoutDetail := []string{"開いていた issue が見つかりません", "見出しが変わりました"}
	counts := map[string]int{}
	fset := token.NewFileSet()
	// glogx の本番の .go を全部見る (手で並べたファイルだけだと、別のファイルに足した setNotice が検査の外になる)。2026-10-19 実測: 92 件
	for _, mf := range parseModuleNonTestFiles(t, fset, 80) {
		f := mf.file
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "setNotice" {
				return true
			}
			// 種類は app の定数 (`app.NoticeError`)。
			kindSel, ok := call.Args[1].(*ast.SelectorExpr)
			if !ok {
				t.Errorf("%s: setNotice の種類が定数でない (検査できない)", fset.Position(call.Pos()))
				return true
			}
			if pkg, ok := kindSel.X.(*ast.Ident); !ok || pkg.Name != "app" {
				t.Errorf("%s: setNotice の種類が app の定数でない (検査できない)", fset.Position(call.Pos()))
				return true
			}
			kind := kindSel.Sel
			var b strings.Builder
			_ = printer.Fprint(&b, fset, call.Args[0])
			text := b.String()
			counts[kind.Name]++
			want := "NoticeRefused"
			switch {
			case strings.Contains(text, "app.ClipboardFailText("):
				want = "NoticeRefused"
			case strings.Contains(text, "app.FirstLine("):
				want = "NoticeError"
			}
			for _, s := range noticeErrorWithoutDetail {
				if strings.Contains(text, s) {
					want = "NoticeError"
				}
			}
			// 成功 (NoticeOK) は断りの代わりにだけ置ける。エラーの詳細を含む知らせを成功の色で出すのも誤り (w でコピーできない)
			if kind.Name != want && (kind.Name != "NoticeOK" || want == "NoticeError") {
				t.Errorf("%s: %s の種類が %s (want %s)", fset.Position(call.Pos()), text, kind.Name, want)
			}
			return true
		})
	}
	// canary: 抽出が空振りすると全部緑になる。3 種類とも 1 件以上拾えていること
	for _, k := range []string{"NoticeOK", "NoticeRefused", "NoticeError"} {
		if counts[k] == 0 {
			t.Errorf("%s の setNotice を 1 件も拾えていない (抽出が壊れている): %v", k, counts)
		}
	}
	if total := counts["NoticeOK"] + counts["NoticeRefused"] + counts["NoticeError"]; total < 30 {
		t.Errorf("setNotice の件数が少なすぎる (%d 件。抽出の対象ファイルが足りないか、抽出が壊れている): %v", total, counts)
	}
}
