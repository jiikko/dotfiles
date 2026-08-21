package main

// グローバル chrome (どの画面でも出るべきオーバーレイ) が全ビューで載ることの回帰テスト
// (issue 085)。
//
// なぜ必要か: 以前は合成が「一覧用」と「viewer 用」に逐語 2 コピーあり、viewer が全画面だった頃に
// 一覧側にしか書いていなかったため「issues を開いている間は通知が画面に一切出ない」時期があった。
// 合成は finishWithGlobalChrome に一本化したので、この検査は「一本化が外れて片方の経路が
// 素の finishWindow に戻る」変異を捕まえる。
//
// ⚠️ 描画結果 (View().Content) で見ること。「トーストが visible か」だけを見る検査は、
// 合成を落としても緑のまま通る (状態は立つが画面に出ない = まさに起きた事故の形)。

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestGlobalChromeShowsInEveryView(t *testing.T) {
	const msg = "グローバル chrome の目印"

	// 一覧 / issues viewer / status viewer の 3 経路すべてで、同じトーストが画面に出る。
	cases := []struct {
		name string
		open func(t *testing.T, m *browseModel)
	}{
		{name: "コミット一覧", open: func(*testing.T, *browseModel) {}},
		{name: "issues viewer", open: func(t *testing.T, m *browseModel) {
			t.Helper()
			m.handleKey("i")
			m.issuesOv.finishAnim()
			if !m.issuesOv.visible() {
				t.Fatal("issues viewer が開かない")
			}
		}},
		{name: "status viewer", open: func(t *testing.T, m *browseModel) {
			t.Helper()
			stubWorktreeStatus(t, statusRec(" M a.go"), nil)
			m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
			if !m.statusOv.visible() {
				t.Fatal("status viewer が開かない")
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newTestBrowse(t, 3, nil, nil)
			m.width, m.height = 100, 24
			c.open(t, m)
			showToastLanded(t, m, msg)
			out := stripANSI(m.View().Content)
			if !strings.Contains(out, msg) {
				t.Fatalf("トーストが画面に出ていない (%s は共通 chrome を載せていない):\n%s", c.name, out)
			}
		})
	}
}

// 再起動ダイアログも全ビューで出る (答えられないと r で再起動できない = viewer に閉じ込められる)。
func TestRestartPromptShowsInViewers(t *testing.T) {
	for _, name := range []string{"コミット一覧", "issues viewer"} {
		t.Run(name, func(t *testing.T) {
			m := newTestBrowse(t, 3, nil, nil)
			m.width, m.height = 100, 24
			if name == "issues viewer" {
				m.handleKey("i")
				m.issuesOv.finishAnim()
			}
			m.restartPending = true
			out := stripANSI(m.View().Content)
			if !strings.Contains(out, "再起動") {
				t.Fatalf("再起動ダイアログが画面に出ていない (%s):\n%s", name, out)
			}
		})
	}
}

// showToastLanded はトーストを出して滑り込みアニメを着地させる (箱幅 0 の間は描かれないため)。
func showToastLanded(t *testing.T, m *browseModel, text string) {
	t.Helper()
	m.toast.show(text, true)
	for i := 0; m.toast.animating() && i < 100; i++ {
		m.toast.advance(m.colored)
	}
	if m.toast.animating() {
		t.Fatal("トーストのアニメが 100 フレームで着地しない (前提が崩れた)")
	}
}
