package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"doctor/disk"
	"glogx/internal/doctorview"
	"glogx/internal/gitdata"
)

// 選んでいた行が消えたら近傍へ寄せ、**寄せたことを画面に出す**。
//
// 🚨 フィールド (`pendingToast`) を見るだけでは足りない。表示するのは tui.go の
// `case doctorview.DoctorToast:` = handleKey の戻り値経路だけで、`restoreCursor` は View から呼ばれる。
// **browseModel 経由でトーストが出ることまで**見ないと、配線の穴を守れない (実測: 初版は
// フィールド代入しか見ておらず、本番では一度も出ない状態で緑だった)。
func TestCursorFallbackIsToldThroughBrowseModel(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]gitdata.CIState{}, nil)
	// 🚨 `doctorOv` は**値フィールド**なので `v := m.doctorOv` だと**コピー**になり、
	//    m 側に何も伝わらない (実測 2026-09-03: それで配線のテストが緑にならなかった)
	v := &m.doctorOv
	v.Fixture().Show()
	a := disk.Result{Entry: disk.Entry{ID: "a", Label: "A", Tier: 1}, Size: 300, Status: disk.StatusOK,
		Items: []disk.Item{{Path: "/tmp/a", Size: 300}}}
	b := disk.Result{Entry: disk.Entry{ID: "b", Label: "B", Tier: 1}, Size: 200, Status: disk.StatusOK,
		Items: []disk.Item{{Path: "/tmp/b", Size: 200}}}

	o := doctorview.DoctorRenderOpts{Page: 40, Width: 100}
	v.Fixture().DiskResults([]disk.Result{a, b})
	v.Lines(o)
	v.HandleKey("j", o.Page) // b の行へ
	if v.Fixture().CursorIndex() == 0 {
		t.Fatal("前提が作れていない: j でカーソルが動かない")
	}

	// 再走査で b が落ちた → 描画で寄せる
	v.Fixture().DiskResults([]disk.Result{a})
	v.Lines(o)
	if !v.Fixture().CursorFellBack() {
		t.Fatal("消えた行を指したまま (寄せの印が立っていない)")
	}

	// 次のキー操作で**画面に**出る。🚨 **browseModel.handleKey 経由**で見る
	// (tui.go に配線があるので、doctorview.DoctorView だけ叩くと配線の穴を守れない)
	m.toast.Clear()
	m.handleKey("j")
	if !strings.Contains(m.toast.Text(), "近くの行へ移りました") {
		t.Fatalf("寄せたことが画面に出ない (toast=%q)", m.toast.Text())
	}
	// 一度出したら再発しない (毎キー出ると邪魔)
	m.toast.Clear()
	m.handleKey("j")
	if strings.Contains(m.toast.Text(), "近くの行へ移りました") {
		t.Fatal("同じ寄せで 2 回目もトーストが出た")
	}

	// 🚨 **その打鍵が飲まれない**こと。
	//    handleKey の先頭で doctorview.DoctorToast を返すと、寄せた直後の
	//    q / esc / d / r / Enter が等しく 1 回空振りする
	v.Fixture().DiskResults([]disk.Result{a, b})
	v.Lines(o)
	v.HandleKey("j", o.Page)
	v.Fixture().DiskResults([]disk.Result{a})
	v.Lines(o) // ここで再度寄せる
	if !v.Fixture().CursorFellBack() {
		t.Fatal("前提が作れていない: 寄せの印が立っていない")
	}
	m.handleKey("q") // 閉じるキー
	if v.Visible() {
		t.Fatal("寄せた直後の q が飲まれた (doctor が閉じない)")
	}
}

// 再起動経路と終了経路の**配線**を固定する。
//
// 🚨 `syscall.Exec` を通る経路は単体テストで走らせられないので、ソースの構造で固定する。
// **文字列検索では駄目**: `WaitDoctorCleanup()` をコメントに変える変異が素通りした。
// go/ast で「その関数の呼び出し」だけを見る。
func TestRestartAndExitPathsWaitForDoctorCleanup(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("main.go を parse できない: %v", err)
	}

	// --- 再起動経路: `if model.restartRequested` の**ブロックの中だけ**を見る ---
	// 🚨 ファイル全体から WaitDoctorCleanup の呼び出しを探すと、**終了経路の defer を拾って**
	//    再起動経路の呼び出しが消えても緑になる (変異検証 2026-09-03 で実際に素通りした)。
	//    ブロックを特定してからその中を見る
	var block *ast.BlockStmt
	ast.Inspect(f, func(n ast.Node) bool {
		ifst, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		if sel, ok := ifst.Cond.(*ast.SelectorExpr); ok && sel.Sel.Name == "restartRequested" {
			block = ifst.Body
			return false
		}
		return true
	})
	if block == nil {
		t.Fatal("`if model.restartRequested` のブロックが見つからない (main.go の構造が変わった)")
	}

	var order []string
	ast.Inspect(block, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if name := callName(c); name != "" {
				order = append(order, name)
			}
		}
		return true
	})
	idx := func(name string) int {
		for i, n := range order {
			if n == name {
				return i
			}
		}
		return -1
	}
	iCancel, iWait, iExec := idx("browse.cancelAll"), idx("doctorview.WaitDoctorCleanup"), idx("restartSelf")
	if iExec < 0 {
		t.Fatalf("restart ブロックに restartSelf() が無い (呼び出し: %v)", order)
	}
	if iWait < 0 {
		t.Fatalf("restart ブロックで WaitDoctorCleanup() を呼んでいない (doctor の子孫が exec を越えて残る。呼び出し: %v)", order)
	}
	if iCancel < 0 {
		t.Fatalf("restart ブロックに browse.cancelAll() が無い (呼び出し: %v)", order)
	}
	if iCancel >= iWait || iWait >= iExec {
		t.Fatalf("順序が違う (cancelAll → WaitDoctorCleanup → restartSelf であること): %v", order)
	}

	// --- 終了経路: defer は LIFO なので「cancelAll を後に積む」= cancel が先に走る ---
	var dWait, dCancel token.Pos
	ast.Inspect(f, func(n ast.Node) bool {
		d, ok := n.(*ast.DeferStmt)
		if !ok {
			return true
		}
		switch callName(d.Call) {
		case "doctorview.WaitDoctorCleanup":
			dWait = d.Pos()
		case "browse.cancelAll":
			dCancel = d.Pos()
		}
		return true
	})
	if dWait == 0 {
		t.Fatal("defer WaitDoctorCleanup() が無い (シグナル終了で doctor の子孫が残る)")
	}
	if dCancel == 0 {
		t.Fatal("defer browse.cancelAll() が無い (main.go の構造が変わった)")
	}
	if dWait >= dCancel {
		t.Fatal("defer の順序が逆 (LIFO なので cancelAll を後に積む。cancel より先に待つと走査が終わるまで固まる)")
	}
}

// callName は呼び出し先の名前を "pkg.Fn" / "recv.Fn" / "Fn" の形で返す。
func callName(c *ast.CallExpr) string {
	switch fn := c.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		if x, ok := fn.X.(*ast.Ident); ok {
			return x.Name + "." + fn.Sel.Name
		}
		return fn.Sel.Name
	}
	return ""
}

// browseModel を作る**両方の工場**が、doctor の seam と後始末を通していることを固定する。
//
// ⚠️ 挙動で試すと非決定になる (join の有無は「Cleanup が戻る時点で goroutine が終わっているか」
// で、遅い fake を挟まないと差が出ず、挟むと join 側が待ちに入る)。**配線を静的に pin する**
// 形にした (verify-interactive-prompt-with-pty-driver.md の「pty が要る挙動は配線を pin する」と
// 同じ判断)。実測 2026-09-03: `WaitDoctorCleanup()` を消しても全テストが緑だった。
func TestBrowseFactoriesInstallInertDoctorAndJoin(t *testing.T) {
	for _, tc := range []struct{ file, fn string }{
		{"tui_helpers_test.go", "newTestBrowse"},
		{"gitlog_watch_test.go", "realRepoBrowse"},
	} {
		t.Run(tc.fn, func(t *testing.T) {
			src, err := os.ReadFile(tc.file)
			if err != nil {
				t.Fatal(err)
			}
			body := string(src)
			i := strings.Index(body, "func "+tc.fn+"(")
			if i < 0 {
				t.Fatalf("%s が %s に無い (走査が壊れている)", tc.fn, tc.file)
			}
			end := strings.Index(body[i:], "\n}\n")
			if end < 0 {
				t.Fatal("関数の終わりが見つからない")
			}
			fn := body[i : i+end]
			for _, want := range []string{"installInertDoctor(", "m.cancelAll()", "joinDoctorCleanup(t)"} {
				if !strings.Contains(fn, want) {
					t.Errorf("%s が %s を通していない (テストが実マシンを叩く / goroutine が跨ぐ)", tc.fn, want)
				}
			}
			if strings.Contains(fn, "t.Cleanup(m.cancel)") {
				t.Errorf("%s が m.cancel のまま (doctor に届かない。cancelAll にすること)", tc.fn)
			}
		})
	}
}
