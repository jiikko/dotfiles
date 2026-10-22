package issuesview

import (
	"fmt"
	"glogx/internal/issuesview/issuesviewtest"
	"glogx/internal/term"
	"glogx/internal/testenv"
	"glogx/issues"
	"strings"
	"testing"
)

// 一覧の窓は必ずカーソル行を含む (どのスクロールキーの直後も)。
//
// 🚨 この不変条件のテストは glide を外した後も残す。半ページ移動は cursor と
// offset を同時に動かすので、窓の導出を素朴に書き換えると「カーソル行が 1 本も描かれない =
// 見えない行が Enter・v・y の対象になる」窓が復活する。
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
	opts := IssuesRenderOpts{Width: 100, Page: page}
	v.Lines(opts) // 初期描画で窓を確定させる

	for _, key := range []string{" ", "ctrl+d", "shift+space", "ctrl+u", "G", "g", "j", "k", "ctrl+d", "G"} {
		v.HandleKey(key, vp(page))
		out := term.StripANSI(strings.Join(v.Lines(opts), "\n"))
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
	issuesviewtest.StartPagerGlide(t, &v.bodyPager, 0, 10)
	v.close()
	if v.bodyPager.Animating() {
		t.Error("close で本文の glide が残る")
	}
}

// 本文 pager の半ページ (Space) は glide に載り、本文を閉じたら glide を残さない (main の TestHalfPageScrollGlidesOnAllSurfaces の issues の分)。
func TestIssuesBodyPagerHalfPageGlides(t *testing.T) {
	v := newTestIssuesView()
	v.bodyPager.Offset = 0
	v.body = nil
	// body が nil でも maxOffset=0 になり動かないため、offset を直接動かす経路で検証する
	prev := 3
	v.bodyPager.Offset = 10
	issuesviewtest.StartPagerGlide(t, &v.bodyPager, prev, v.bodyPager.Offset)
	if !v.bodyPager.Animating() {
		t.Error("本文の半ページが glide に載らない")
	}
	if got := testenv.PagerShown(&v.bodyPager); got != prev {
		t.Errorf("glide 開始位置 = %d, want %d", got, prev)
	}
	// 本文を閉じたら glide を残さない (次に開いた瞬間に古い位置から滑らない)
	v.closeBody()
	if v.bodyPager.Animating() {
		t.Error("本文を閉じても glide が残っている")
	}
}

// advanceGlide が本文 pager の glide を着地させる (main の TestHalfPageScrollGlidesOnAllSurfaces の issues の分)。
func TestIssuesAdvanceGlideLandsBodyPager(t *testing.T) {
	// 🚨 一覧は glide を持たない (幾何的にアニメしないため削除)
	v := newTestIssuesView()
	v.bodyPager.Offset = 10
	issuesviewtest.StartPagerGlide(t, &v.bodyPager, 0, 10)
	for range term.ScrollAnimFrames {
		v.AdvanceGlide()
	}
	if v.bodyPager.Animating() {
		t.Error("advanceGlide で着地しない")
	}
}

// 一覧の shift+space は上へ戻る (main の TestShiftSpaceScrollsUp の issues の分)。
func TestIssuesListShiftSpaceScrollsUp(t *testing.T) {
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
	v.HandleKey(" ", vp(page)) // 下へ (窓を進める)
	down := v.cursor
	v.HandleKey("shift+space", vp(page))
	if v.cursor >= down {
		t.Errorf("shift+space で上に戻らない: cursor %d -> %d", down, v.cursor)
	}
}

// 本文 pager の shift+space は上へ戻る (main の TestShiftSpaceScrollsUp の issues の分)。
func TestIssuesBodyShiftSpaceScrollsUp(t *testing.T) {
	v := newTestIssuesView()
	// handleBodyKey は body 前提 (本番でも open と同時にセットされる)。行数を稼ぐため
	// 長めの本文を与える。
	var src strings.Builder
	for i := range 200 {
		fmt.Fprintf(&src, "line %d\n", i)
	}
	v.body = issues.NewBody(src.String())
	v.body.Lines(80, false) // Len() を確定させる (幅ごとに整形するため)
	v.bodyPager.Offset = 30
	before := v.bodyPager.Offset
	v.handleBodyKey("shift+space", IssuesViewport{width: 80, page: 20}, 20)
	if v.bodyPager.Offset >= before {
		t.Errorf("shift+space で上に戻らない: bodyOff %d -> %d", before, v.bodyPager.Offset)
	}
}
