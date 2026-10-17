package main

import (
	"strings"
	"testing"
	"time"

	"github.com/jiikko/dotfiles/src/tuikit/layout"

	"glogx/internal/issuesview"
	"glogx/internal/issuesview/issuesviewtest"
	"glogx/internal/term"
	"glogx/internal/testenv"
	"glogx/issues"
)

// realIssuesView は実ファイルを持つ viewer を作る (openBody は ReadBody を通るので、
// fake パスの loadedView では本文モードに入れない)。
func realIssuesView(t *testing.T) *issuesview.IssuesView {
	t.Helper()
	dir := t.TempDir()
	names := []string{"030", "029"}
	list := make([]*issues.Issue, 0, len(names))
	for _, n := range names {
		list = append(list, issuesviewtest.WriteRealIssue(t, dir, n+"-feat-x.md", "# "+n+" feat: x\n\n本文の段落。\n\n- 箇条書き\n"))
	}
	return loadedView(list...)
}

func TestComposeDrawer(t *testing.T) {
	base := []string{"LLLLLLLLLLLLLLLLLLLL", "MMMMMMMMMMMMMMMMMMMM"}
	panel := []string{"PPPPPPPP", "QQQQQQQQ"}

	// 幅 0 では下地をそのまま返す
	if got := layout.ComposeDrawer(base, panel, 0, 20, false); got[0] != base[0] {
		t.Errorf("幅 0 で下地が変わった: %q", got[0])
	}

	out := layout.ComposeDrawer(base, panel, 10, 20, false)
	for i, ln := range out {
		if w := term.DispWidth(ln); w != 20 {
			t.Errorf("行 %d の幅 = %d, want 20 (合成で幅が変わってはいけない)", i, w)
		}
	}
	// 板は右から入るので、左に一覧の先頭・境界・右に本文の先頭が並ぶ
	if !strings.HasPrefix(out[0], "LLLLLLLLLL") {
		t.Errorf("左に一覧の先頭が残っていない: %q", out[0])
	}
	if !strings.Contains(out[0], "▏") {
		t.Errorf("区切りが無い: %q", out[0])
	}
	// 板の中身は区切りの直後から (短い行は板幅ぶん空白で埋める)
	if !strings.Contains(out[0], "▏PPPPPPPP") {
		t.Errorf("区切りの右に本文が来ていない: %q", out[0])
	}
	// 見えている幅が増えるほど板の左辺は左へ動く (幅が伸びるのではなく位置が動く)
	narrow := layout.ComposeDrawer(base, panel, 6, 20, false)
	if a, b := strings.Index(narrow[0], "▏"), strings.Index(out[0], "▏"); a <= b {
		t.Errorf("板が左へ動いていない: 幅 6 の左辺=%d, 幅 10 の左辺=%d", a, b)
	}
	// 🚨 区切りに "│" を使わない (本文のスクロールバーと隣り合って "││" に見える)
	if strings.Contains(out[0], "│") {
		t.Errorf("スクロールバーと紛らわしい区切りを使っている: %q", out[0])
	}
	// 下地より本文の行数が少なくても落ちない
	if got := layout.ComposeDrawer(base, panel[:1], 10, 20, false); len(got) != len(base) {
		t.Errorf("行数が保たれない: %d", len(got))
	}
}

// browseModel の tick チェーンで引き出しが実際に動くこと (end-to-end)。
//
// 型単体のテストは「幅の計算」しか見ておらず、tick が届かなければ画面は開きかけで固まる。
// ここは View の実出力で境界 (▏) の位置が tick ごとに右へ動き、着地することを見る。
func TestIssuesDrawerAnimatesThroughModelTicks(t *testing.T) {
	base := time.Date(2026, 7, 31, 12, 0, 0, 0, time.Local)
	now := base
	testenv.StubClockFrom(t, &now)

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.width, m.height = 100, 20
	m.usageOv.dismiss()
	m.issuesOv = *realIssuesView(t)
	m.issuesOv.Fixture().FinishAnim() // 開く演出は本題でないので着地させる

	// 境界の桁位置 = 引き出しの幅。無ければ -1。
	edge := func() int {
		for _, ln := range strings.Split(term.StripANSI(m.View().Content), "\n") {
			if i := strings.Index(ln, "▏"); i >= 0 {
				return term.DispWidth(ln[:i])
			}
		}
		return -1
	}

	if got := edge(); got != -1 {
		t.Fatalf("本文を開く前から境界がある (col=%d)", got)
	}
	m.handleKey("enter")
	if !m.issuesOv.Fixture().BodyOpen() {
		t.Fatal("Enter で本文が開かない")
	}
	if !m.spinnerActive() {
		t.Fatal("開いた直後に spinnerActive=false (tick が回らず開きかけで固まる)")
	}

	// tick を送りながら時刻を進める。幅は単調に増えて着地する。
	prev := m.contentWidth() // 板が画面外にある = 左辺は右端
	moved := 0
	for range 20 {
		now = now.Add(tickIntervalForTest)
		m.Update(tickMsg{})
		cur := edge()
		if cur > prev {
			t.Fatalf("板が右へ戻った: %d -> %d (右から左へ入るはず)", prev, cur)
		}
		if cur < prev {
			moved++
		}
		prev = cur
		if !m.spinnerActive() {
			break // 着地して tick が止まった
		}
	}
	if moved < 3 {
		t.Errorf("板が動いたフレームが %d 回しかない (アニメになっていない)", moved)
	}
	// 着地: 板の左辺 = 画面幅 - 引き出しの幅
	target := m.issuesOv.Fixture().DrawerTargetWidth(m.contentWidth())
	if want := m.contentWidth() - target; prev != want {
		t.Errorf("着地した板の左辺 = %d, want %d", prev, want)
	}
	if m.spinnerActive() {
		t.Error("着地後も spinnerActive=true (tick が残る)")
	}
}

// tickIntervalForTest はアニメを進める 1 コマぶんの時間 (実機の tick 周期に相当)。
const tickIntervalForTest = 33 * time.Millisecond
