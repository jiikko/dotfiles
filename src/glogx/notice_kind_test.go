package main

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
	"testing"
)

// viewer の通知の種類 (noticeKind) が、showWarning の doc の 3 分類どおりに付いていることを、
// setNotice の全呼び出しについて固定する (issue 663 の敵対的レビュー: 分類の誤りは 30 件中 2 件の
// 経路でしかテストされておらず、「本文を読めませんでした」を noticeRefused にしても緑だった)。
//
//   - エラー詳細 (firstLine(err…)) を含む通知は noticeError (w でコピーできるよう lastWarning に積む)
//   - クリップボード失敗 (clipboardFailText) は noticeRefused (lastWarning を汚さない)
//   - それ以外の失敗は noticeRefused。例外は Msg 経路で置く 2 件 (noticeErrorWithoutDetail):
//     次の打鍵が q だとトーストを読む前に終わるので、理由を lastWarning に残す (issue 059)
func TestSetNoticeKindsFollowWarningClassification(t *testing.T) {
	noticeErrorWithoutDetail := []string{"開いていた issue が見つかりません", "見出しが変わりました"}
	counts := map[string]int{}
	fset := token.NewFileSet()
	for _, file := range []string{"issues_view.go", "issues_linkjump.go", "status_view.go"} {
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "setNotice" {
				return true
			}
			kind, ok := call.Args[1].(*ast.Ident)
			if !ok {
				t.Errorf("%s: setNotice の種類が定数でない (検査できない)", fset.Position(call.Pos()))
				return true
			}
			var b strings.Builder
			_ = printer.Fprint(&b, fset, call.Args[0])
			text := b.String()
			counts[kind.Name]++
			want := "noticeRefused"
			switch {
			case strings.Contains(text, "clipboardFailText("):
				want = "noticeRefused"
			case strings.Contains(text, "firstLine("):
				want = "noticeError"
			}
			for _, s := range noticeErrorWithoutDetail {
				if strings.Contains(text, s) {
					want = "noticeError"
				}
			}
			if kind.Name != "noticeOK" && kind.Name != want {
				t.Errorf("%s: %s の種類が %s (want %s)", fset.Position(call.Pos()), text, kind.Name, want)
			}
			return true
		})
	}
	// canary: 抽出が空振りすると全部緑になる。3 種類とも 1 件以上拾えていること
	for _, k := range []string{"noticeOK", "noticeRefused", "noticeError"} {
		if counts[k] == 0 {
			t.Errorf("%s の setNotice を 1 件も拾えていない (抽出が壊れている): %v", k, counts)
		}
	}
	if total := counts["noticeOK"] + counts["noticeRefused"] + counts["noticeError"]; total < 30 {
		t.Errorf("setNotice の件数が少なすぎる (%d 件。抽出の対象ファイルが足りないか、抽出が壊れている): %v", total, counts)
	}
}
