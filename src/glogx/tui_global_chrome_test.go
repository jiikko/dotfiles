package main

// グローバル chrome (どの画面でも出るべきオーバーレイ) が全ビューで載ることの回帰テスト
// (issue 085)。
//
// なぜ必要か: 以前は合成が「一覧用」と「viewer 用」に逐語 2 コピーあり、viewer が全画面だった頃に
// 一覧側にしか書いていなかったため「issues を開いている間は通知が画面に一切出ない」時期があった。
// 合成は finishWithGlobalChrome に一本化したので、この検査は「一本化が外れて片方の経路が
// 素の finishWindow に戻る」変異を捕まえる。
//
// 🚨 描画結果 (View().Content) で見ること。「トーストが visible か」だけを見る検査は、
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
	m.toast.Show(text, true)
	for i := 0; m.toast.Animating() && i < 100; i++ {
		m.toast.Advance()
	}
	if m.toast.Animating() {
		t.Fatal("トーストのアニメが 100 フレームで着地しない (前提が崩れた)")
	}
}

// showWarningsLanded は重要警告を積み、2 枚とも holding まで進める。
func showWarningsLanded(t *testing.T, m *browseModel, texts ...string) {
	t.Helper()
	for _, text := range texts {
		m.toast.Show(text, false)
	}
	for i := 0; m.toast.Animating() && i < 100; i++ {
		m.toast.Advance()
	}
	if m.toast.Animating() {
		t.Fatal("重要警告のアニメが 100 フレームで着地しない (前提が崩れた)")
	}
}

func TestToastDrawBudgetIsWiredThroughViewContent(t *testing.T) {
	const (
		olderWarning = "重要警告 A"
		newerWarning = "重要警告 B"
	)

	t.Run("page=9 では重要警告2枚", func(t *testing.T) {
		m := newTestBrowse(t, 3, nil, nil)
		m.width, m.height = 100, 10
		// newTestBrowse は NoFrame なので pageSize は height-1。height=10 は page=9
		// となり、toastDrawBudget の「重要警告2枚を確保」が効く帯に入る。
		if got := m.pageSize(); got != 9 {
			t.Fatalf("height=10 の page = %d, want 9", got)
		}
		showWarningsLanded(t, m, olderWarning, newerWarning)

		out := stripANSI(m.View().Content)
		for _, want := range []string{olderWarning, newerWarning} {
			if !strings.Contains(out, want) {
				t.Fatalf("page=9 の View().Content に %q がない:\n%s", want, out)
			}
		}
	})

	t.Run("page=8 では2枚目を出さず箱を切らない", func(t *testing.T) {
		m := newTestBrowse(t, 3, nil, nil)
		m.width, m.height = 100, 9
		// newTestBrowse は NoFrame なので pageSize は height-1。height=9 は page=8
		// となり、2枚目の箱を描くには足りないが、1枚目の箱4行は収まる帯になる。
		if got := m.pageSize(); got != 8 {
			t.Fatalf("height=9 の page = %d, want 8", got)
		}
		showWarningsLanded(t, m, olderWarning, newerWarning)

		out := stripANSI(m.View().Content)
		if !strings.Contains(out, newerWarning) {
			t.Fatalf("page=8 の View().Content に最新の警告がない:\n%s", out)
		}
		if strings.Contains(out, olderWarning) {
			t.Fatalf("page=8 で2枚目の警告まで描かれた:\n%s", out)
		}

		lines := strings.Split(out, "\n")
		if len(lines) > m.pageSize()+1 { // +1 は窓の下の hint 行
			t.Fatalf("page=8 の描画行数が窓を超えた: got=%d want<=%d", len(lines), m.pageSize()+1)
		}
		warningLine := -1
		for i, line := range lines {
			if strings.Contains(line, newerWarning) {
				warningLine = i
				break
			}
		}
		// 警告本文の前後に上辺・下辺・下影がある = 1箱4行が途中で切れていない。
		if warningLine < 1 || warningLine+2 >= len(lines) {
			t.Fatalf("page=8 の警告箱が途中で切れている: 本文行=%d, 全体=%d:\n%s", warningLine, len(lines), out)
		}
	})
}

// 窓より長い通知は右端で切らず、箱の中で折り返して末尾まで見せる (2026-09-25 のユーザーの指摘: 右端で
// 文の末尾と枠が切れていた)。配線 (BoxLines に窓の幅を渡す) を View 経由で固定する。
func TestToastLongTextWrapsWithinViewWidth(t *testing.T) {
	m := newTestBrowse(t, 3, nil, nil)
	m.width, m.height = 50, 20
	showWarningsLanded(t, m, "push failed: remote rejected the update because the branch is protected TAILMARK")
	out := stripANSI(m.View().Content)
	if !strings.Contains(out, "TAILMARK") {
		t.Fatalf("長い通知の末尾が画面に出ていない:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if w := dispWidth(line); w > m.contentWidth() {
			t.Errorf("行の幅 %d が窓の幅 %d を超える: %q", w, m.contentWidth(), line)
		}
	}
}

// 折り返した長い警告 2 枚も、窓に余裕があれば 2 枚とも描く (予算を 1 行の箱の高さで数えると 2 枚目が落ちる。敵対レビュー P2-1)。
func TestToastDrawBudgetFitsTwoWrappedWarnings(t *testing.T) {
	m := newTestBrowse(t, 3, nil, nil)
	m.width, m.height = 50, 17 // page=16: 半ページは 8 行 = 1 行の箱なら 2 枚ぶん
	showWarningsLanded(t, m,
		"push failed: remote rejected the update OLDERMARK",
		"pull failed: could not resolve host github.com NEWERMARK")
	out := stripANSI(m.View().Content)
	for _, want := range []string{"OLDERMARK", "NEWERMARK"} {
		if !strings.Contains(out, want) {
			t.Fatalf("折り返した警告 %q が描かれていない:\n%s", want, out)
		}
	}
}

// 警告の無い 1 行の箱は、中くらいの窓でも 2 枚まで (予算の下限を一律に上げると 3 枚入って窓を覆う。敵対レビュー 2 周目)。
func TestToastShortSuccessesStayWithinHalfPage(t *testing.T) {
	m := newTestBrowse(t, 3, nil, nil)
	m.width, m.height = 100, 14 // page=13
	for _, s := range []string{"OLDEST-SUCCESS", "pulled", "コピーした"} {
		m.toast.Show(s, true)
	}
	for i := 0; m.toast.Animating() && i < 100; i++ {
		m.toast.Advance()
	}
	out := stripANSI(m.View().Content)
	if strings.Contains(out, "OLDEST-SUCCESS") || !strings.Contains(out, "pulled") {
		t.Fatalf("page=13 で 1 行の成功が 2 枚に収まっていない (最古が残る / 2 枚目が消えた):\n%s", out)
	}
}

// 長い警告 2 枚の後に成功が来ても、窓に余裕があれば警告 2 枚とも描く (最新の成功の箱が警告の予算を先に使い、
// 古い警告が落ちていた。issue 484 の page=20 の再現)。
func TestToastBudgetReservesNewestSuccessAndTwoWarnings(t *testing.T) {
	m := newTestBrowse(t, 3, nil, nil)
	m.width, m.height = 50, 21 // page=20
	long := strings.Repeat("remote rejected the update ", 4)
	m.toast.Show(long+"OLDERMARK", false)
	m.toast.Show(long+"NEWERMARK", false)
	showToastLanded(t, m, "SUCCESSMARK")
	out := stripANSI(m.View().Content)
	for _, want := range []string{"OLDERMARK", "NEWERMARK", "SUCCESSMARK"} {
		if !strings.Contains(out, want) {
			t.Fatalf("page=20 で %q が描かれていない:\n%s", want, out)
		}
	}
}
