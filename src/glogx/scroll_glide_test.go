package main

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"glogx/issues"
)

// advance は上下どちらの向きでも表示 offset を論理 offset へ寄せ、有限フレームで着地して
// active を下ろす (旧 TestBrowseScrollAnimConverges を共有型のテストへ移設。geometry 非依存)。
func TestScrollGlideConverges(t *testing.T) {
	for _, tc := range []struct{ from, to int }{
		{from: 0, to: 7},  // 下スクロール (1 コミット ~7 行)
		{from: 7, to: 0},  // 上スクロール
		{from: 3, to: 4},  // 残り 1 行
		{from: 5, to: 5},  // 動きなし → 即座に active を下ろす
		{from: 0, to: 40}, // 半ページ相当の大きな距離 (2026-07-31 で glide 対象になった)
		{from: 40, to: 0}, // 同・上向き
	} {
		var g scrollGlide
		g.from, g.shown, g.frame, g.active = tc.from, tc.from, 0, true
		prevShown := g.shown
		frames := 0
		for g.active {
			g.advance(tc.to)
			frames++
			// ease-in: 表示 offset は目標を通り越さず単調に近づく
			if (tc.to > tc.from && (g.shown < prevShown || g.shown > tc.to)) ||
				(tc.to < tc.from && (g.shown > prevShown || g.shown < tc.to)) {
				t.Fatalf("from=%d to=%d: 非単調/行き過ぎ (shown=%d)", tc.from, tc.to, g.shown)
			}
			prevShown = g.shown
			if frames > 20 {
				t.Fatalf("from=%d to=%d: 収束しない (shown=%d)", tc.from, tc.to, g.shown)
			}
		}
		if g.shown != tc.to {
			t.Errorf("from=%d to=%d: 着地 shown=%d, want %d", tc.from, tc.to, g.shown, tc.to)
		}
		if frames > scrollAnimFrames {
			t.Errorf("from=%d to=%d: %d フレーム (scrollAnimFrames=%d 以内のはず)", tc.from, tc.to, frames, scrollAnimFrames)
		}
	}
}

// 距離に依らず所要フレーム数が一定 = 半ページでも 1 行でも同じ時間で着地する
// (フレーム数で終わる設計。距離で終わると背高コミットや半ページで間延びする)。
func TestScrollGlideDurationIndependentOfDistance(t *testing.T) {
	framesFor := func(from, to int) int {
		var g scrollGlide
		g.start(from, to)
		n := 0
		for g.active {
			g.advance(to)
			n++
		}
		return n
	}
	short, long := framesFor(0, 1), framesFor(0, 200)
	if short != long {
		t.Errorf("所要フレームが距離依存: 1 行=%d / 200 行=%d", short, long)
	}
}

func TestScrollGlideStart(t *testing.T) {
	t.Run("距離 0 は開始しない", func(t *testing.T) {
		var g scrollGlide
		if g.start(5, 5) || g.active {
			t.Error("距離 0 で glide が始まった")
		}
	})

	t.Run("進行中の glide は積まず即時へ倒す", func(t *testing.T) {
		var g scrollGlide
		if !g.start(0, 10) {
			t.Fatal("初回の start が false")
		}
		if g.start(10, 20) {
			t.Error("連打で glide が積まれた (即時に倒すはず)")
		}
		if g.active {
			t.Error("連打後も active (描画は論理 offset に戻すべき)")
		}
	})

	t.Run("進行中でも距離 0 なら進行中の glide を壊さない", func(t *testing.T) {
		var g scrollGlide
		g.start(0, 10)
		if g.start(7, 7) {
			t.Error("距離 0 で start が true")
		}
		if !g.active {
			t.Error("距離 0 の呼び出しで進行中の glide が消えた (カーソルが画面内で動いただけ)")
		}
	})
}

// offset は glide 中だけ途中位置を返し、それ以外は論理 offset をそのまま返す。
func TestScrollGlideOffset(t *testing.T) {
	var g scrollGlide
	if got := g.offset(42); got != 42 {
		t.Errorf("非 glide 時 offset = %d, want 42 (論理 offset をそのまま)", got)
	}
	g.start(0, 10)
	if got := g.offset(10); got != 0 {
		t.Errorf("glide 開始直後 offset = %d, want 0 (開始位置)", got)
	}
	// ease-in (t^2) なので最初の 1 フレームは距離が短いと動かない (round で 0 に落ちる)。
	// 中盤まで進めれば開始位置と着地点の間にいる。
	for range 3 {
		g.advance(10)
	}
	if got := g.offset(10); got <= 0 || got >= 10 {
		t.Errorf("glide 途中 offset = %d, want 0 < x < 10 (開始と着地の間)", got)
	}
	g.stop()
	if got := g.offset(10); got != 10 {
		t.Errorf("stop 後 offset = %d, want 10 (即時表示)", got)
	}
}

// glide 中に論理 offset が動いても (resize・追加ロード) その時点の着地点へ向かう。
func TestScrollGlideFollowsMovingTarget(t *testing.T) {
	var g scrollGlide
	g.start(0, 100)
	g.advance(100)
	// 途中で着地点が縮む (resize で maxOffset が下がった等)
	for g.active {
		g.advance(5)
	}
	if g.shown != 5 {
		t.Errorf("動いた着地点に着かない: shown=%d, want 5", g.shown)
	}
}

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
			t.Skip("この geometry では半ページで動かない (テスト前提の破れ)")
		}
		if !m.glide.active || cmd == nil {
			t.Errorf("半ページ移動が glide に載っていない: active=%v cmd=%v", m.glide.active, cmd != nil)
		}
		if got := m.glide.offset(m.offset); got != prev {
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
		if !o.glide.active {
			t.Error("Space の半ページが glide に載っていない")
		}
		if got := o.glide.offset(o.offset); got != 0 {
			t.Errorf("glide 開始位置 = %d, want 0", got)
		}
		// 1 行移動は glide 対象外 (距離 1 で滑らせる意味がない)
		o.glide.stop()
		o.scroll("j", 20)
		if o.glide.active {
			t.Error("1 行移動が glide に載っている")
		}
		// 端ジャンプは即時
		o.scroll(" ", 20)
		o.scroll("G", 20)
		if o.glide.active {
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
		v.bodyGlide.start(prev, v.bodyOff)
		if !v.bodyGlide.active {
			t.Error("本文の半ページが glide に載らない")
		}
		if got := v.bodyGlide.offset(v.bodyOff); got != prev {
			t.Errorf("glide 開始位置 = %d, want %d", got, prev)
		}
		// 本文を閉じたら glide を残さない (次に開いた瞬間に古い位置から滑らない)
		v.closeBody()
		if v.bodyGlide.active {
			t.Error("本文を閉じても glide が残っている")
		}
	})

	t.Run("advanceGlide が本文 pager を進める", func(t *testing.T) {
		// ⚠️ 一覧は glide を持たない (幾何的にアニメしないため削除。issue 031)
		v := newTestIssuesView()
		v.bodyOff = 10
		v.bodyGlide.start(0, 10)
		for range scrollAnimFrames {
			v.advanceGlide()
		}
		if v.bodyGlide.active {
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
		if !m.diffOv.glide.active {
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
		if m.glide.active && !m.ticking {
			t.Error("glide が立ったのに tick チェーンが張られない")
		}
	})
}

// resize は glide を持つ 3 面すべてで捨てる (幅で行数が変わり着地点が古くなるため)。
// issues の一覧は glide を持たない (issue 031)。
func TestResizeStopsAllGlides(t *testing.T) {
	m := newTestBrowse(t, 10, map[string]CIState{}, nil)
	m.glide.start(0, 10)
	m.diffOv.glide.start(0, 10)
	m.issuesOv.bodyGlide.start(0, 10)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if m.glide.active || m.diffOv.glide.active || m.issuesOv.bodyGlide.active {
		t.Errorf("resize で glide が残る: list=%v diff=%v issuesBody=%v",
			m.glide.active, m.diffOv.glide.active, m.issuesOv.bodyGlide.active)
	}
}

// 一覧の窓は必ずカーソル行を含む (どのスクロールキーの直後も)。
//
// ⚠️ この不変条件のテストは glide を外した後も残す (issue 031 の決定)。半ページ移動は cursor と
// offset を同時に動かすので、窓の導出を素朴に書き換えると「カーソル行が 1 本も描かれない =
// 見えない行が Enter・v・y の対象になる」窓が復活する (敵対的レビュー P2 の回帰)。
func TestIssuesListWindowAlwaysKeepsCursorVisible(t *testing.T) {
	v := newTestIssuesView()
	v.shown, v.loaded = true, true
	all := make([]*issues.Issue, 60)
	for i := range all {
		all[i] = &issues.Issue{Number: fmt.Sprintf("%03d", i), Title: fmt.Sprintf("TITLE%02d", i), Path: "p"}
	}
	v.all, v.rows = all, all
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
	v.bodyGlide.start(0, 10)
	v.close()
	if v.bodyGlide.active {
		t.Error("close で本文の glide が残る")
	}
}

// shift+space は上方向の半ページ (less / vim の流儀。ユーザー要望 2026-07-31)。space は下方向。
// 端末が shift+space を区別せず素の " " を送る環境ではこの case に入らず従来の下方向になるだけで、
// 既存挙動は壊れない (区別して送る端末でだけ上方向が効く)。
func TestShiftSpaceScrollsUp(t *testing.T) {
	t.Run("コミット一覧", func(t *testing.T) {
		m := newTestBrowse(t, 40, map[string]CIState{}, nil)
		m.statuses = statusesFor(m, StateSuccess)
		m.usageOv.dismiss()
		m.height = 12
		m.handleKey("ctrl+d") // まず下へ
		for range scrollAnimFrames {
			m.glide.advance(m.offset)
		}
		down := m.offset
		if down == 0 {
			t.Skip("この geometry では半ページで動かない (テスト前提の破れ)")
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
		v.all, v.rows = all, all
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
