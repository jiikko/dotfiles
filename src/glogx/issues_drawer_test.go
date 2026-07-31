package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"glogx/issues"
)

// realIssuesView は実ファイルを持つ viewer を作る (openBody は ReadBody を通るので、
// fake パスの loadedView では本文モードに入れない)。
func realIssuesView(t *testing.T) *issuesView {
	t.Helper()
	dir := t.TempDir()
	names := []string{"030", "029"}
	list := make([]*issues.Issue, 0, len(names))
	for _, n := range names {
		path := filepath.Join(dir, n+"-feat-x.md")
		body := "# " + n + " feat: x\n\n本文の段落。\n\n- 箇条書き\n"
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		list = append(list, &issues.Issue{
			Path: path, Dir: dir, Rel: n + "-feat-x.md", Number: n, Category: "feat",
		})
	}
	return loadedView(list...)
}

func TestDrawerPhasesAndWidth(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.Local)
	var d issuesDrawer
	const total = 100
	target := d.targetWidth(total) // 80

	if d.width(total, now) != 0 {
		t.Error("閉じているのに幅がある")
	}
	d.open(now)
	if got := d.width(total, now); got != 0 {
		t.Errorf("開き始めの幅 = %d, want 0", got)
	}
	mid := d.width(total, now.Add(issuesDrawerDuration/2))
	if mid <= 0 || mid >= target {
		t.Errorf("途中の幅 = %d, want 0 < x < %d", mid, target)
	}
	if got := d.width(total, now.Add(issuesDrawerDuration)); got != target {
		t.Errorf("開ききった幅 = %d, want %d", got, target)
	}
	// 着地で静止状態へ
	if closed := d.settle(now.Add(issuesDrawerDuration)); closed || d.phase != drawerOpen {
		t.Errorf("開き切りの settle: closed=%v phase=%v", closed, d.phase)
	}
}

// 閉じるときは開くときの逆再生 (同じ進捗で同じ幅を通る)。
func TestDrawerCloseIsReverseOfOpen(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.Local)
	const total = 100
	var open issuesDrawer
	open.open(now)
	var closing issuesDrawer
	closing.phase, closing.started = drawerClosing, now

	for _, f := range []float64{0, 0.25, 0.5, 0.75, 1} {
		at := now.Add(time.Duration(f * float64(issuesDrawerDuration)))
		rev := now.Add(time.Duration((1 - f) * float64(issuesDrawerDuration)))
		if o, c := open.width(total, at), closing.width(total, rev); o != c {
			t.Errorf("進捗 %.2f: 開く幅 %d と逆再生の幅 %d が一致しない", f, o, c)
		}
	}
}

// 開き切る前に閉じても幅が飛ばない (今見えている幅から折り返す)。
func TestDrawerCloseFromPartialOpen(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.Local)
	const total = 100
	var d issuesDrawer
	d.open(now)
	at := now.Add(issuesDrawerDuration / 3)
	before := d.width(total, at)
	d.startClose(at)
	if after := d.width(total, at); after != before {
		t.Errorf("閉じ始めで幅が飛んだ: %d -> %d", before, after)
	}
	// そのまま進めば 0 へ着地する
	if got := d.width(total, at.Add(issuesDrawerDuration)); got != 0 {
		t.Errorf("閉じ切りの幅 = %d, want 0", got)
	}
	if closed := d.settle(at.Add(issuesDrawerDuration)); !closed || d.phase != drawerClosed {
		t.Errorf("閉じ切りの settle: closed=%v phase=%v", closed, d.phase)
	}
}

func TestDrawerFinishLandsImmediately(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.Local)
	var d issuesDrawer
	d.open(now)
	if closed := d.finish(); closed || d.phase != drawerOpen {
		t.Errorf("開く演出の finish: closed=%v phase=%v", closed, d.phase)
	}
	d.startClose(now)
	if closed := d.finish(); !closed || d.phase != drawerClosed {
		t.Errorf("閉じる演出の finish: closed=%v phase=%v", closed, d.phase)
	}
}

func TestDrawerAnimatingDrivesTick(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.Local)
	var d issuesDrawer
	if d.animating(now) {
		t.Error("閉じているのにアニメ中")
	}
	d.open(now)
	if !d.animating(now.Add(issuesDrawerDuration / 2)) {
		t.Error("開く途中がアニメ中でない (tick が止まって固まる)")
	}
	if d.animating(now.Add(issuesDrawerDuration)) {
		t.Error("着地後もアニメ中 (tick が残る)")
	}
}

func TestComposeDrawer(t *testing.T) {
	base := []string{"LLLLLLLLLLLLLLLLLLLL", "MMMMMMMMMMMMMMMMMMMM"}
	panel := []string{"PPPPPPPP", "QQQQQQQQ"}

	// 幅 0 では下地をそのまま返す
	if got := composeDrawer(base, panel, 0, 20, false); got[0] != base[0] {
		t.Errorf("幅 0 で下地が変わった: %q", got[0])
	}

	out := composeDrawer(base, panel, 10, 20, false)
	for i, ln := range out {
		if w := dispWidth(ln); w != 20 {
			t.Errorf("行 %d の幅 = %d, want 20 (合成で幅が変わってはいけない)", i, w)
		}
	}
	// 左に本文、境界に区切り、右に下地の続き
	if !strings.HasPrefix(out[0], "PPPPPPPP") {
		t.Errorf("左に本文が来ていない: %q", out[0])
	}
	if !strings.Contains(out[0], "▏") {
		t.Errorf("区切りが無い: %q", out[0])
	}
	if !strings.HasSuffix(out[0], "LLLLLLLLLL") {
		t.Errorf("右に一覧の続きが出ていない: %q", out[0])
	}
	// ⚠️ 区切りに "│" を使わない (本文のスクロールバーと隣り合って "││" に見える)
	if strings.Contains(out[0], "│") {
		t.Errorf("スクロールバーと紛らわしい区切りを使っている: %q", out[0])
	}
	// 下地より本文の行数が少なくても落ちない
	if got := composeDrawer(base, panel[:1], 10, 20, false); len(got) != len(base) {
		t.Errorf("行数が保たれない: %d", len(got))
	}
}

// 統合: 本文を開くと一覧が右に残り、閉じる演出のあいだ本文は生きている。
func TestIssuesViewDrawerIntegration(t *testing.T) {
	orig := timeNow
	base := time.Date(2026, 7, 31, 12, 0, 0, 0, time.Local)
	now := base
	timeNow = func() time.Time { return now }
	t.Cleanup(func() { timeNow = orig })

	v := realIssuesView(t)
	o := issuesRenderOpts{width: 80, page: 12}
	v.lines(o)
	v.handleKey("enter", o.page)
	if v.open == nil {
		t.Fatal("Enter で本文が開かない")
	}

	// 開ききると本文が主役、一覧はタブ行の右端が残る (全画面置き換えではない)
	now = base.Add(issuesDrawerDuration)
	out := strings.Join(v.lines(o), "\n")
	if !strings.Contains(out, "▏") {
		t.Errorf("引き出しの境界が出ていない:\n%s", out)
	}
	if !strings.Contains(stripANSI(out), v.open.Rel) {
		t.Errorf("本文のヘッダーが出ていない:\n%s", out)
	}
	// ⚠️ 下地は一覧のヘッダー (タブ) であって本文のヘッダーではない
	if strings.Count(stripANSI(out), v.open.Rel) != 1 {
		t.Errorf("本文のヘッダーが下地にも出ている:\n%s", out)
	}

	// 閉じる: 演出のあいだ本文は生きていて、幅は縮んでいく
	v.handleKey("h", o.page)
	if v.open == nil {
		t.Fatal("h の直後に本文が消えた (逆再生に何も映らない)")
	}
	now = base.Add(issuesDrawerDuration + issuesDrawerDuration/2)
	mid := v.drawer.width(o.width, now)
	if mid <= 0 || mid >= v.drawer.targetWidth(o.width) {
		t.Errorf("閉じ途中の幅 = %d (0 と満杯の間のはず)", mid)
	}
	// 着地で本文を捨てる
	now = base.Add(issuesDrawerDuration * 3)
	v.lines(o)
	if v.open != nil || v.drawer.phase != drawerClosed {
		t.Errorf("閉じ切っても本文が残る: open=%v phase=%v", v.open != nil, v.drawer.phase)
	}
}

// 演出中は tick が回り続ける (止まると開きかけで固まる)。
func TestIssuesViewDrawerKeepsTickAlive(t *testing.T) {
	orig := timeNow
	base := time.Date(2026, 7, 31, 12, 0, 0, 0, time.Local)
	now := base
	timeNow = func() time.Time { return now }
	t.Cleanup(func() { timeNow = orig })

	v := realIssuesView(t)
	v.lines(issuesRenderOpts{width: 80, page: 12})
	v.handleKey("enter", 12)
	now = base.Add(issuesDrawerDuration / 2)
	if !v.animating() {
		t.Error("開く演出の途中でアニメ扱いになっていない (tick が止まる)")
	}
	now = base.Add(issuesDrawerDuration)
	if v.animating() {
		t.Error("着地後もアニメ扱い (tick が残る)")
	}
}

// browseModel の tick チェーンで引き出しが実際に動くこと (end-to-end)。
//
// 型単体のテストは「幅の計算」しか見ておらず、tick が届かなければ画面は開きかけで固まる。
// ここは View の実出力で境界 (▏) の位置が tick ごとに右へ動き、着地することを見る。
func TestIssuesDrawerAnimatesThroughModelTicks(t *testing.T) {
	origNow := timeNow
	base := time.Date(2026, 7, 31, 12, 0, 0, 0, time.Local)
	now := base
	timeNow = func() time.Time { return now }
	t.Cleanup(func() { timeNow = origNow })

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.width, m.height = 100, 20
	m.usageOv.dismiss()
	m.issuesOv = *realIssuesView(t)
	m.issuesOv.finishAnim() // 開く演出は本題でないので着地させる

	// 境界の桁位置 = 引き出しの幅。無ければ -1。
	edge := func() int {
		for _, ln := range strings.Split(stripANSI(m.View().Content), "\n") {
			if i := strings.Index(ln, "▏"); i >= 0 {
				return dispWidth(ln[:i])
			}
		}
		return -1
	}

	if got := edge(); got != -1 {
		t.Fatalf("本文を開く前から境界がある (col=%d)", got)
	}
	m.handleKey("enter")
	if m.issuesOv.open == nil {
		t.Fatal("Enter で本文が開かない")
	}
	if !m.spinnerActive() {
		t.Fatal("開いた直後に spinnerActive=false (tick が回らず開きかけで固まる)")
	}

	// tick を送りながら時刻を進める。幅は単調に増えて着地する。
	prev := edge()
	grew := 0
	for range 20 {
		now = now.Add(tickIntervalForTest)
		m.Update(tickMsg{})
		cur := edge()
		if cur < prev {
			t.Fatalf("幅が縮んだ: %d -> %d", prev, cur)
		}
		if cur > prev {
			grew++
		}
		prev = cur
		if !m.spinnerActive() {
			break // 着地して tick が止まった
		}
	}
	if grew < 3 {
		t.Errorf("幅が増えたフレームが %d 回しかない (アニメになっていない)", grew)
	}
	// 境界 (▏) は引き出しの最終桁を占めるので、その開始桁は幅 -1
	target := m.issuesOv.drawer.targetWidth(m.contentWidth())
	if prev != target-1 {
		t.Errorf("着地した境界の桁 = %d, want %d (幅 %d の最終桁)", prev, target-1, target)
	}
	if m.spinnerActive() {
		t.Error("着地後も spinnerActive=true (tick が残る)")
	}
}

// tickIntervalForTest はアニメを進める 1 コマぶんの時間 (実機の tick 周期に相当)。
const tickIntervalForTest = 33 * time.Millisecond
