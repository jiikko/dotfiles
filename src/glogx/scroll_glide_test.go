package main

import (
	"fmt"
	"math"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"glogx/internal/issuesview"
	"glogx/internal/term"
	"glogx/issues"

	"github.com/jiikko/dotfiles/src/tuikit/listnav"
)

// 半ページ移動 (Space / ctrl+d) が 4 面すべてで glide に載る (ユーザー要望 2026-07-31 の回帰)。
// 以前は j/k の 1 行移動だけがアニメで、半ページは snap していた。
func TestHalfPageScrollGlidesOnAllSurfaces(t *testing.T) {
	t.Run("コミット一覧 (ctrl+d)", func(t *testing.T) {
		m := newTestBrowse(t, 30, map[string]CIState{}, nil)
		m.statuses = statusesFor(m, StateSuccess)
		m.height = 12
		prev := m.offset
		_, cmd := m.handleKey("ctrl+d")
		if m.offset == prev {
			// 🚨 Skip にしないこと: geometry は上の newTestBrowse + m.height で決定論的に
			// 組んでいるので、offset が動かないのは環境要因ではなく **実装の退行だけ**。
			// Skip にすると「browse の半ページ下スクロールが完全に死んでも ok glogx」になる
			// (実測 2026-08-21: pageSize()/2 を潰す変異で 2 箇所とも SKIP に化けた)。
			// 同ファイル :240 / :347 は同じ「前提の破れ」を既に Fatal で扱っている。
			t.Fatalf("半ページで offset が動かない (実装の退行): offset=%d", m.offset)
		}
		if !m.glide.Active() || cmd == nil {
			t.Errorf("半ページ移動が glide に載っていない: active=%v cmd=%v", m.glide.Active(), cmd != nil)
		}
		if got := m.glide.Offset(m.offset); got != prev {
			t.Errorf("glide 開始位置 = %d, want %d (移動前の offset)", got, prev)
		}
	})

	t.Run("diff pager (Space)", func(t *testing.T) {
		o := newDiffOverlay()
		o.sha = "abc"
		lines := make([]string, 100)
		for i := range lines {
			lines[i] = "line"
		}
		o.cache.store("abc", lines, "abc")
		o.scroll(" ", 20)
		if !o.pager.Animating() {
			t.Error("Space の半ページが glide に載っていない")
		}
		if got := pagerShown(&o.pager); got != 0 {
			t.Errorf("glide 開始位置 = %d, want 0", got)
		}
		// 1 行移動は glide 対象外 (距離 1 で滑らせる意味がない)
		o.pager.Stop()
		o.scroll("j", 20)
		if o.pager.Animating() {
			t.Error("1 行移動が glide に載っている")
		}
		// 端ジャンプは即時
		o.scroll(" ", 20)
		o.scroll("G", 20)
		if o.pager.Animating() {
			t.Error("端ジャンプ (G) で glide が残っている")
		}
	})

	t.Run("issues 本文 pager (Space)", func(t *testing.T) {
		v := newTestIssuesView()
		v.BodyPager.Offset = 0
		v.Body = nil
		// body が nil でも maxOffset=0 になり動かないため、offset を直接動かす経路で検証する
		prev := 3
		v.BodyPager.Offset = 10
		startPagerGlide(t, &v.BodyPager, prev, v.BodyPager.Offset)
		if !v.BodyPager.Animating() {
			t.Error("本文の半ページが glide に載らない")
		}
		if got := pagerShown(&v.BodyPager); got != prev {
			t.Errorf("glide 開始位置 = %d, want %d", got, prev)
		}
		// 本文を閉じたら glide を残さない (次に開いた瞬間に古い位置から滑らない)
		v.CloseBody()
		if v.BodyPager.Animating() {
			t.Error("本文を閉じても glide が残っている")
		}
	})

	t.Run("advanceGlide が本文 pager を進める", func(t *testing.T) {
		// 🚨 一覧は glide を持たない (幾何的にアニメしないため削除。issue 031)
		v := newTestIssuesView()
		v.BodyPager.Offset = 10
		startPagerGlide(t, &v.BodyPager, 0, 10)
		for range term.ScrollAnimFrames {
			v.AdvanceGlide()
		}
		if v.BodyPager.Animating() {
			t.Error("advanceGlide で着地しない")
		}
	})
}

// glide を立てたキー経路は必ず tick を返す (張り忘れると advanceGlide を呼ぶ者がいなくなり、
// 表示がスクロール前の位置で固まって「キーが効かない」ように見える。敵対的レビュー P1 の回帰)。
func TestGlideKeyPathsScheduleTick(t *testing.T) {
	t.Run("diff pager", func(t *testing.T) {
		m := newTestBrowse(t, 3, map[string]CIState{}, nil)
		m.usageOv.dismiss()
		lines := make([]string, 200)
		for i := range lines {
			lines[i] = "line"
		}
		sha := m.commits[0].SHA
		m.diffOv.sha = sha
		m.diffOv.cache.store(sha, lines, sha)
		m.ticking = false
		m.handleKey(" ")
		if !m.diffOv.pager.Animating() {
			t.Fatal("Space で glide が立たない (テスト前提の破れ)")
		}
		if !m.ticking {
			t.Error("glide が立ったのに tick チェーンが張られない")
		}
	})

	t.Run("コミット一覧", func(t *testing.T) {
		m := newTestBrowse(t, 30, map[string]CIState{}, nil)
		m.statuses = statusesFor(m, StateSuccess)
		m.usageOv.dismiss()
		m.height = 12
		m.ticking = false
		m.handleKey("ctrl+d")
		if m.glide.Active() && !m.ticking {
			t.Error("glide が立ったのに tick チェーンが張られない")
		}
	})
}

// resize は glide を持つ面すべて (一覧・diff・issues の本文とカーソル・status の pager) で捨てる (幅で行数が変わり着地点が古くなるため)。
// issues の一覧は glide を持たない (issue 031)。
func TestResizeStopsAllGlides(t *testing.T) {
	m := newTestBrowse(t, 10, map[string]CIState{}, nil)
	m.glide.Start(0, 10, term.ScrollAnimFrames)
	startPagerGlide(t, &m.diffOv.pager, 0, 10)
	startPagerGlide(t, &m.issuesOv.BodyPager, 0, 10)
	startPagerGlide(t, &m.statusOv.pager, 0, 10)
	m.issuesOv.CurGlide.Start(0, 10, term.CursorAnimFrames)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.glide.Active() || m.diffOv.pager.Animating() || m.issuesOv.BodyPager.Animating() ||
		m.statusOv.pager.Animating() || m.issuesOv.CurGlide.Active() {
		t.Errorf("resize で glide が残る: list=%v diff=%v issuesBody=%v status=%v issuesCursor=%v",
			m.glide.Active(), m.diffOv.pager.Animating(), m.issuesOv.BodyPager.Animating(),
			m.statusOv.pager.Animating(), m.issuesOv.CurGlide.Active())
	}
}

// shift+space は上方向の半ページ (less / vim の流儀。ユーザー要望 2026-07-31)。space は下方向。
//
// 🚨 このテストは handleKey に文字列を直接渡すので、**キーが端末から届くかは何も検査していない**
// (検査しているのは「届いた場合の向き」だけ)。実測 2026-09-17 で、届くのは kitty keyboard protocol /
// modifyOtherKeys 対応の端末だけで、macOS の Terminal.app では届かないことが確定した。条件の正本は
// docs/issues-viewer-spec.md「半ページ移動はカーソルが滑る (窓は滑らせない)」の末尾。
func TestShiftSpaceScrollsUp(t *testing.T) {
	t.Run("コミット一覧", func(t *testing.T) {
		m := newTestBrowse(t, 40, map[string]CIState{}, nil)
		m.statuses = statusesFor(m, StateSuccess)
		m.usageOv.dismiss()
		m.height = 12
		m.handleKey("ctrl+d") // まず下へ
		for range term.ScrollAnimFrames {
			m.glide.Advance(m.offset)
		}
		down := m.offset
		if down == 0 {
			// 上と同じ理由で Fatal (ここは前提として先に ctrl+d を打つので、Skip にすると
			// 二重にマスクされる)
			t.Fatal("前提の ctrl+d で offset が動かない (実装の退行)")
		}
		m.handleKey("shift+space")
		if m.offset >= down {
			t.Errorf("shift+space で上に戻らない: offset %d -> %d", down, m.offset)
		}
	})

	t.Run("diff pager", func(t *testing.T) {
		o := newDiffOverlay()
		o.sha = "abc"
		lines := make([]string, 200)
		for i := range lines {
			lines[i] = "line"
		}
		o.cache.store("abc", lines, "abc")
		o.scroll(" ", 20)
		down := o.pager.Offset
		if down == 0 {
			t.Fatal("Space で下スクロールしない (テスト前提の破れ)")
		}
		o.scroll("shift+space", 20)
		if o.pager.Offset >= down {
			t.Errorf("shift+space で上に戻らない: offset %d -> %d", down, o.pager.Offset)
		}
	})

	t.Run("issues 一覧 (handleKey 経由)", func(t *testing.T) {
		v := newTestIssuesView()
		v.Shown, v.Loaded = true, true
		all := make([]*issues.Issue, 60)
		for i := range all {
			all[i] = &issues.Issue{Number: fmt.Sprintf("%03d", i), Title: "t", Path: "p"}
		}
		v.All = all
		v.SetRows(all)
		v.Dirs = []string{"/x"}
		v.Cursor = 40
		page := 20
		v.HandleKey(" ", vp(page)) // 下へ (窓を進める)
		down := v.Cursor
		v.HandleKey("shift+space", vp(page))
		if v.Cursor >= down {
			t.Errorf("shift+space で上に戻らない: cursor %d -> %d", down, v.Cursor)
		}
	})

	t.Run("issues 本文 pager", func(t *testing.T) {
		v := newTestIssuesView()
		// handleBodyKey は body 前提 (本番でも open と同時にセットされる)。行数を稼ぐため
		// 長めの本文を与える。
		var src strings.Builder
		for i := range 200 {
			src.WriteString(fmt.Sprintf("line %d\n", i))
		}
		v.Body = issues.NewBody(src.String())
		v.Body.Lines(80, false) // Len() を確定させる (幅ごとに整形するため)
		v.BodyPager.Offset = 30
		before := v.BodyPager.Offset
		v.HandleBodyKey("shift+space", issuesview.IssuesViewport{Width: 80, Page: 20}, 20)
		if v.BodyPager.Offset >= before {
			t.Errorf("shift+space で上に戻らない: bodyOff %d -> %d", before, v.BodyPager.Offset)
		}
	})
}

// startPagerGlide は p を from から to への半ページ glide の途中に置く (テスト用)。
// 本番と同じ入口 (Pager.Move の半ページ移動) を通すので、Offset は着地点 to になる。
func startPagerGlide(t *testing.T, p *listnav.Pager, from, to int) {
	t.Helper()
	d := to - from
	m := listnav.HalfDown
	if d < 0 {
		d, m = -d, listnav.HalfUp
	}
	rows := 2 * d // listnav.Half(rows) == d
	p.Offset = from
	p.Stop()
	p.Move(m, max(from, to)+rows+1, rows, term.ScrollAnimFrames)
	if p.Offset != to || !p.Animating() {
		t.Fatalf("startPagerGlide の前提の破れ: offset=%d (want %d) animating=%v", p.Offset, to, p.Animating())
	}
}

// pagerShown は p の今の表示位置 (glide 中は途中位置)。行数の上限で丸めない。
func pagerShown(p *listnav.Pager) int { return p.DrawOffset(math.MaxInt32, 1) }
