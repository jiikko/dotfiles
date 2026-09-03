package main

import (
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
}

// 🚨 新しい全画面ビューアを足したら**ここへ 1 行足す** (足し忘れは
// TestFullScreenCasesCoverEveryID が red にする)。
var fullScreenCases = []fullScreenCase{
	{
		id: fullScreenRatelimit, name: "残量ダッシュボード",
		show: func(m *browseModel) { m.rlDash.shown = true },
		hint: func(m *browseModel) string { return m.rlDash.hint() },
	},
	{
		id: fullScreenDoctor, name: "doctor",
		show: func(m *browseModel) { m.doctorOv.shown = true },
		hint: func(m *browseModel) string { return m.doctorOv.hint(m.hintWidth()) },
	},
	{
		id: fullScreenStatus, name: "status viewer",
		show: func(m *browseModel) { m.statusOv.shown = true },
		hint: func(m *browseModel) string { return m.statusOv.hint(m.hintWidth()) },
	},
	{
		id: fullScreenIssues, name: "issues viewer",
		show: func(m *browseModel) { m.issuesOv.shown = true },
		hint: func(m *browseModel) string { return m.issuesOv.hint() },
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
			// 3. 描画: 窓ごと差し替わる (コミット一覧が残っていない)
			if content := m.View().Content; strings.Contains(content, listSHA) {
				t.Errorf("全画面のはずが一覧が描かれている (%q が残っている)", listSHA)
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
}
