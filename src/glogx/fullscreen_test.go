package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// 全画面ビューアの membership (「今どれが出ているか」) は activeFullScreen が唯一の出典で、
// それを問う 5 つのサイト — 見送り (gitLogReloadDeferred) / 描画 (viewLines) / 最下行 (hintLine) /
// 復元の破棄 (issuesRestoreMsg) / キーの routing (handleKey) — は全部そこから導出している。
//
// この表は **ID ごとに 5 サイトを 1 回ずつ通す**ので、ビューアを 1 枚足して配線をどれか 1 つ
// 忘れると red になる。issue 227 の発火条件 (doctor を後から足したとき見送りの列挙から漏れ、
// build もテストも通ったまま silent に壊れた) を機械で塞ぐのがこのテストの役目。
type fullScreenCase struct {
	id   fullScreenID
	name string
	// show はそのビューアが出ている状態を作る (**作り方はここに集約する**。テストごとに
	// 別々に作ると、片方だけ実装に追従して「別の状態を検査している」ことに気づけない)
	show func(m *browseModel)
	// hint はそのビューア自身の最下行 (hintLine の期待値)
	hint func(m *browseModel) string
	// lines はそのビューア自身が描く窓。**「一覧が出ていない」だけでなく「このビューアが
	// 出ている」まで見る**ために使う (敵対レビュー 2026-09-04: viewLines の doctor ↔ status を
	// 入れ替える変異が、一覧の SHA だけを見ていた当初のテストを素通りした)
	lines func(m *browseModel) []string
}

// 🚨 新しい全画面ビューアを足したら**ここへ 1 行足す** (足し忘れは
// TestFullScreenCasesCoverEveryID が red にする)。
var fullScreenCases = []fullScreenCase{
	{
		id: fullScreenRatelimit, name: "残量ダッシュボード",
		show:  func(m *browseModel) { m.rlDash.shown = true },
		hint:  func(m *browseModel) string { return m.rlDash.hint() },
		lines: func(m *browseModel) []string { return m.rlDash.lines(m.ratelimitOpts()) },
	},
	{
		id: fullScreenDoctor, name: "doctor",
		show:  func(m *browseModel) { m.doctorOv.shown = true },
		hint:  func(m *browseModel) string { return m.doctorOv.hint(m.hintWidth()) },
		lines: func(m *browseModel) []string { return m.doctorOv.lines(m.doctorOpts()) },
	},
	{
		id: fullScreenStatus, name: "status viewer",
		show:  func(m *browseModel) { m.statusOv.shown = true },
		hint:  func(m *browseModel) string { return m.statusOv.hint(m.hintWidth()) },
		lines: func(m *browseModel) []string { return m.statusOv.lines(m.statusOpts()) },
	},
	{
		id: fullScreenIssues, name: "issues viewer",
		show:  func(m *browseModel) { m.issuesOv.shown = true },
		hint:  func(m *browseModel) string { return m.issuesOv.hint(m.hintWidth()) },
		lines: func(m *browseModel) []string { return m.issuesOv.lines(m.issuesOpts()) },
	},
}

// 表が ID を全部覆っていること。fullScreenCount の直前に ID を足した人は、ここが red になって
// 表へ 1 行足すことになり、その 1 行が下の全サイト検査を連れてくる (これがこの番兵の目的)。
func TestFullScreenCasesCoverEveryID(t *testing.T) {
	covered := map[fullScreenID]int{}
	for _, c := range fullScreenCases {
		covered[c.id]++
	}
	for id := fullScreenNone + 1; id < fullScreenCount; id++ {
		switch covered[id] {
		case 1:
		case 0:
			t.Errorf("fullScreenID %v (%d) が fullScreenCases に無い"+
				" (見送り / 描画 / hint / 復元の破棄 / routing の配線が無検査になる)", id, int(id))
		default:
			t.Errorf("fullScreenID %v が %d 行ある (表の重複)", id, covered[id])
		}
	}
	if len(fullScreenCases) != int(fullScreenCount)-1 {
		t.Errorf("表の行数 %d が ID の数 %d と合わない", len(fullScreenCases), int(fullScreenCount)-1)
	}
	// 🚨 識別に使う行が**ビューアごとに違う**こと。同じ行になっていると「別のビューアが
	// 描かれている」を検出できない (識別の assert が黙って vacuous になる)。
	// 🚨 状態はケースごとに作るので、ここが捕まえるのは「**どの状態でも同じ行になる**」重複だけ
	// (片方だけ shown の状態で比べるため)。ケース単位の取り違えは上の assert 3 が受け持つ
	seen := map[string]fullScreenID{}
	for _, c := range fullScreenCases {
		m := newTestBrowse(t, 3, map[string]CIState{}, nil)
		c.show(m)
		mark := firstMeaningfulLine(c.lines(m))
		if mark == "" {
			t.Errorf("%v: 識別に使える行が無い", c.id)
			continue
		}
		if other, dup := seen[mark]; dup {
			t.Errorf("%v と %v の識別行が同じ (%q): 取り違えを検出できない", c.id, other, mark)
		}
		seen[mark] = c.id
	}
}

// firstMeaningfulLine は「そのビューアだけが描く」識別に使える最初の行。
// 空行と枠・余白だけの行は飛ばす (どのビューアでも同じになるため識別に使えない)。
func firstMeaningfulLine(lines []string) string {
	for _, l := range lines {
		// 🚨 **畳む前に左端を切る**。右側は overlay (usage の箱・トースト) に上書きされるので、
		// 先に空白を畳むと「上書きされた領域の語」まで marker に入って一致しなくなる
		r := []rune(strings.TrimLeft(stripANSI(l), " ")) // 中央寄せの盤 (残量) は左が余白
		if len(r) > 12 {
			r = r[:12]
		}
		if t := squashSpaces(string(r)); len([]rune(t)) >= 4 {
			return t
		}
	}
	return ""
}

// squashSpaces は連続する空白を 1 つに畳んで前後を落とす (枠と溝のパディングを無視して
// 「同じ行が描かれているか」だけを見るため)。
func squashSpaces(s string) string { return strings.Join(strings.Fields(s), " ") }

// frameWindow は 1 フレームのうち**窓の部分**を比較用に正規化する (最下行 = hint を落とす)。
func frameWindow(content string) string {
	lines := strings.Split(stripANSI(content), "\n")
	if len(lines) > 1 {
		lines = lines[:len(lines)-1]
	}
	return squashSpaces(strings.Join(lines, " "))
}

// 各ビューアについて、membership を問う全サイトが「そのビューアが出ている」と扱うこと。
func TestFullScreenSurfacesWireEverySite(t *testing.T) {
	for _, c := range fullScreenCases {
		t.Run(c.name, func(t *testing.T) {
			m := newTestBrowse(t, 3, map[string]CIState{}, nil)
			listSHA := m.commits[0].ShortSHA // 一覧だけが描くもの (viewer の窓には出ない)
			listHint := m.hintLine()
			if !strings.Contains(m.View().Content, listSHA) {
				t.Fatalf("前提が作れていない: 一覧に %q が出ていない", listSHA)
			}
			if m.activeFullScreen() != fullScreenNone {
				t.Fatal("前提が作れていない: 何も開いていないのに全画面ビューア扱い")
			}

			c.show(m)

			// 1. 出典そのもの
			if got := m.activeFullScreen(); got != c.id {
				t.Fatalf("activeFullScreen = %v, want %v", got, c.id)
			}
			// 2. 見送り: 開いている間の外部変更は反映しない (裏の全面リロードを起こさない)
			if !m.gitLogReloadDeferred() {
				t.Error("見送りにならない (裏で git log の全面リロード + カーソルのリセット + CI の再取得が走る)")
			}
			// 3. 描画: 窓ごと差し替わり、**そのビューアが**描かれる
			content := m.View().Content
			if strings.Contains(content, listSHA) {
				t.Errorf("全画面のはずが一覧が描かれている (%q が残っている)", listSHA)
			}
			mark := firstMeaningfulLine(c.lines(m))
			if mark == "" {
				t.Fatal("判定不能: ビューアが空の窓しか描いていない (識別に使える行が無い)")
			}
			// 🚨 比べるのは**窓の部分だけ / 行の左端だけ / 色と余白を落として**。
			// 右上には usage の箱が重なる (起動時グランス) ので行まるごとは一致せず、
			// 最下行は hint なので窓に含めない (hint の一致は下で別に見る)
			if !strings.Contains(frameWindow(content), mark) {
				t.Errorf("別のビューアが描かれている (%q が窓に無い)", mark)
			}
			// 4. 最下行: ビューア自身の hint が出る (一覧の hint のままにしない)
			gotHint := m.hintLine()
			if want := m.hintLineText(c.hint(m)); gotHint != want {
				t.Errorf("hintLine がビューアのものでない\n got=%q\nwant=%q", gotHint, want)
			}
			if gotHint == listHint {
				t.Error("hintLine が一覧のまま (viewer の語彙が出ない)")
			}
			// 5. routing: キーはビューアが飲む (裏の一覧が動かない)
			before := m.cursor
			m.handleKey("j")
			if m.cursor != before {
				t.Errorf("j が裏の一覧に届いてカーソルが動いた (%d → %d)", before, m.cursor)
			}
			// 6. 復元の破棄: 既に 1 枚出ているところへ issues の復元を割り込ませない
			// (2 枚同時 shown = 「見えている画面」と「キーを受ける画面」が食い違う)。
			// 🚨 issues 自身は対象外 (復元先が自分なので捨てない)
			if c.id != fullScreenIssues {
				m.Update(issuesRestoreMsg{})
				if m.issuesOv.visible() {
					t.Error("別の全画面ビューアが出ているのに issues の復元が通った")
				}
			}
		})
	}

	// 🚨 対称のケース: **何も出ていなければ復元は通る**。捨てる側だけを固定すると、
	// ガードを「常に捨てる」へ変える変異 (= 起動時の復元が二度と効かない) が全テストを
	// 素通りする (敵対レビュー 2026-09-04 が実測)
	t.Run("全画面が無ければ復元は通る", func(t *testing.T) {
		m := newTestBrowse(t, 3, map[string]CIState{}, nil)
		if m.activeFullScreen() != fullScreenNone {
			t.Fatal("前提が作れていない")
		}
		m.Update(issuesRestoreMsg{})
		if !m.issuesOv.visible() {
			t.Error("何も出ていないのに issues の復元が捨てられた (起動時の再開記憶が効かない)")
		}
	})
}

// 全画面ビューアの**描画**はレジストリ (activeFullScreen の switch) を通ること。
//
// 🚨 これが無いと、5 枚目を「ID を足さずに switch の前へ `if m.newOv.visible()` を挿す」形で
// 配線できてしまい、lint も既存テストも緑のまま issue 227 が再発する (敵対レビュー 2026-09-04 が
// 複製の上で実測: 描画・hint・routing は効くのに、見送りと復元だけが黙って壊れる)。
// **全画面ビューアは finishWithGlobalChrome を通らないとトースト・usage・モーダルが載らない**
// ので、描画をレジストリ経由に縛れば ID を足さざるを得ず、そこから先は lint と上の表が捕まえる。
//
// 🚨 検出しない形は fullscreen.go の脅威モデルに書いてある (自前で全画面を描く / 描画以外を
// `if` で足す)。構文で全部を塞ぎにいかない。
func TestFullScreenDrawingGoesThroughTheRegistry(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "tui.go", nil, 0)
	if err != nil {
		t.Fatalf("tui.go を parse できない: %v", err)
	}
	var fn *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "viewLines" {
			fn = fd
		}
	}
	if fn == nil {
		t.Fatal("判定不能: viewLines が見つからない (走査が壊れている)")
	}

	// activeFullScreen を tag に持つ switch の位置 (この中の呼び出しだけが正当)
	var lo, hi token.Pos
	ast.Inspect(fn, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok || sw.Tag == nil {
			return true
		}
		if call, ok := sw.Tag.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "activeFullScreen" {
				lo, hi = sw.Pos(), sw.End()
			}
		}
		return true
	})
	if lo == token.NoPos {
		t.Fatal("viewLines に activeFullScreen の switch が無い (レジストリ経由の描画が消えている)")
	}

	inside, outside := 0, 0
	var outsideAt []string
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "finishWithGlobalChrome" {
			return true
		}
		if call.Pos() >= lo && call.End() <= hi {
			inside++
		} else {
			outside++
			outsideAt = append(outsideAt, fset.Position(call.Pos()).String())
		}
		return true
	})
	// switch の中は **ID の数ちょうど**、外は **1 本だけ** (末尾のコミット一覧の経路)。
	// 🚨 件数で固定するのが要点: 「0 件でも緑」にすると走査が壊れたときに気づけないし、
	// 外の 1 本を許すだけの緩い規則にすると 5 枚目の `if` が紛れ込める。
	if inside != int(fullScreenCount)-1 {
		t.Errorf("switch の中の finishWithGlobalChrome が %d 件 (ID の数 %d と一致しない)",
			inside, int(fullScreenCount)-1)
	}
	if outside != 1 {
		t.Errorf("switch の外の finishWithGlobalChrome が %d 件 (想定はコミット一覧の 1 本だけ): %v\n"+
			"全画面ビューアを足すなら fullScreenID を足して switch へ入れること (issue 227)",
			outside, outsideAt)
	}
}
