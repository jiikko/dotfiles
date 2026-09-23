package main

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"glogx/issues"
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
		if !o.glide.Active() {
			t.Error("Space の半ページが glide に載っていない")
		}
		if got := o.glide.Offset(o.offset); got != 0 {
			t.Errorf("glide 開始位置 = %d, want 0", got)
		}
		// 1 行移動は glide 対象外 (距離 1 で滑らせる意味がない)
		o.glide.Stop()
		o.scroll("j", 20)
		if o.glide.Active() {
			t.Error("1 行移動が glide に載っている")
		}
		// 端ジャンプは即時
		o.scroll(" ", 20)
		o.scroll("G", 20)
		if o.glide.Active() {
			t.Error("端ジャンプ (G) で glide が残っている")
		}
	})

	t.Run("issues 本文 pager (Space)", func(t *testing.T) {
		v := newTestIssuesView()
		v.bodyOff = 0
		v.body = nil
		// body が nil でも maxOffset=0 になり動かないため、offset を直接動かす経路で検証する
		prev := 3
		v.bodyOff = 10
		v.bodyGlide.Start(prev, v.bodyOff, scrollAnimFrames)
		if !v.bodyGlide.Active() {
			t.Error("本文の半ページが glide に載らない")
		}
		if got := v.bodyGlide.Offset(v.bodyOff); got != prev {
			t.Errorf("glide 開始位置 = %d, want %d", got, prev)
		}
		// 本文を閉じたら glide を残さない (次に開いた瞬間に古い位置から滑らない)
		v.closeBody()
		if v.bodyGlide.Active() {
			t.Error("本文を閉じても glide が残っている")
		}
	})

	t.Run("advanceGlide が本文 pager を進める", func(t *testing.T) {
		// 🚨 一覧は glide を持たない (幾何的にアニメしないため削除。issue 031)
		v := newTestIssuesView()
		v.bodyOff = 10
		v.bodyGlide.Start(0, 10, scrollAnimFrames)
		for range scrollAnimFrames {
			v.advanceGlide()
		}
		if v.bodyGlide.Active() {
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
		if !m.diffOv.glide.Active() {
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

// resize は glide を持つ 3 面すべてで捨てる (幅で行数が変わり着地点が古くなるため)。
// issues の一覧は glide を持たない (issue 031)。
func TestResizeStopsAllGlides(t *testing.T) {
	m := newTestBrowse(t, 10, map[string]CIState{}, nil)
	m.glide.Start(0, 10, scrollAnimFrames)
	m.diffOv.glide.Start(0, 10, scrollAnimFrames)
	m.issuesOv.bodyGlide.Start(0, 10, scrollAnimFrames)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.glide.Active() || m.diffOv.glide.Active() || m.issuesOv.bodyGlide.Active() {
		t.Errorf("resize で glide が残る: list=%v diff=%v issuesBody=%v",
			m.glide.Active(), m.diffOv.glide.Active(), m.issuesOv.bodyGlide.Active())
	}
}

// 一覧の窓は必ずカーソル行を含む (どのスクロールキーの直後も)。
//
// 🚨 この不変条件のテストは glide を外した後も残す (issue 031 の決定)。半ページ移動は cursor と
// offset を同時に動かすので、窓の導出を素朴に書き換えると「カーソル行が 1 本も描かれない =
// 見えない行が Enter・v・y の対象になる」窓が復活する (敵対的レビュー P2 の回帰)。
func TestIssuesListWindowAlwaysKeepsCursorVisible(t *testing.T) {
	v := newTestIssuesView()
	v.shown, v.loaded = true, true
	all := make([]*issues.Issue, 60)
	for i := range all {
		all[i] = &issues.Issue{Number: fmt.Sprintf("%03d", i), Title: fmt.Sprintf("TITLE%02d", i), Path: "p"}
	}
	v.all = all
	v.setRows(all)
	v.dirs = []string{"/x/issues"} // 空だと emptyMessage が早期 return して一覧を描かない
	const page = 20
	opts := issuesRenderOpts{width: 100, page: page}
	v.lines(opts) // 初期描画で窓を確定させる

	for _, key := range []string{" ", "ctrl+d", "shift+space", "ctrl+u", "G", "g", "j", "k", "ctrl+d", "G"} {
		v.handleKey(key, vp(page))
		out := stripANSI(strings.Join(v.lines(opts), "\n"))
		if want := fmt.Sprintf("TITLE%02d", v.cursor); !strings.Contains(out, want) {
			t.Fatalf("%q の後にカーソル行 %q が描かれていない (cursor=%d offset=%d)\n%s",
				key, want, v.cursor, v.offset, out)
		}
	}
}

// 閉じたら本文の glide を残さない (再表示の一瞬だけ古い位置から滑るのを防ぐ)。
func TestIssuesCloseStopsBodyGlide(t *testing.T) {
	v := newTestIssuesView()
	v.shown = true
	v.bodyGlide.Start(0, 10, scrollAnimFrames)
	v.close()
	if v.bodyGlide.Active() {
		t.Error("close で本文の glide が残る")
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
		for range scrollAnimFrames {
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
		down := o.offset
		if down == 0 {
			t.Fatal("Space で下スクロールしない (テスト前提の破れ)")
		}
		o.scroll("shift+space", 20)
		if o.offset >= down {
			t.Errorf("shift+space で上に戻らない: offset %d -> %d", down, o.offset)
		}
	})

	t.Run("issues 一覧 (handleKey 経由)", func(t *testing.T) {
		v := newTestIssuesView()
		v.shown, v.loaded = true, true
		all := make([]*issues.Issue, 60)
		for i := range all {
			all[i] = &issues.Issue{Number: fmt.Sprintf("%03d", i), Title: "t", Path: "p"}
		}
		v.all = all
		v.setRows(all)
		v.dirs = []string{"/x"}
		v.cursor = 40
		page := 20
		v.handleKey(" ", vp(page)) // 下へ (窓を進める)
		down := v.cursor
		v.handleKey("shift+space", vp(page))
		if v.cursor >= down {
			t.Errorf("shift+space で上に戻らない: cursor %d -> %d", down, v.cursor)
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
		v.body = issues.NewBody(src.String())
		v.body.Lines(80, false) // Len() を確定させる (幅ごとに整形するため)
		v.bodyOff = 30
		before := v.bodyOff
		v.handleBodyKey("shift+space", 20)
		if v.bodyOff >= before {
			t.Errorf("shift+space で上に戻らない: bodyOff %d -> %d", before, v.bodyOff)
		}
	})
}
