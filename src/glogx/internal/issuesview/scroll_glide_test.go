package issuesview

import (
	"fmt"
	"glogx/internal/term"
	"glogx/issues"
	"strings"
	"testing"
)

// 一覧の窓は必ずカーソル行を含む (どのスクロールキーの直後も)。
//
// 🚨 この不変条件のテストは glide を外した後も残す (issue 031 の決定)。半ページ移動は cursor と
// offset を同時に動かすので、窓の導出を素朴に書き換えると「カーソル行が 1 本も描かれない =
// 見えない行が Enter・v・y の対象になる」窓が復活する (敵対的レビュー P2 の回帰)。
func TestIssuesListWindowAlwaysKeepsCursorVisible(t *testing.T) {
	v := newTestIssuesView()
	v.Shown, v.Loaded = true, true
	all := make([]*issues.Issue, 60)
	for i := range all {
		all[i] = &issues.Issue{Number: fmt.Sprintf("%03d", i), Title: fmt.Sprintf("TITLE%02d", i), Path: "p"}
	}
	v.All = all
	v.SetRows(all)
	v.Dirs = []string{"/x/issues"} // 空だと emptyMessage が早期 return して一覧を描かない
	const page = 20
	opts := IssuesRenderOpts{Width: 100, Page: page}
	v.Lines(opts) // 初期描画で窓を確定させる

	for _, key := range []string{" ", "ctrl+d", "shift+space", "ctrl+u", "G", "g", "j", "k", "ctrl+d", "G"} {
		v.HandleKey(key, vp(page))
		out := term.StripANSI(strings.Join(v.Lines(opts), "\n"))
		if want := fmt.Sprintf("TITLE%02d", v.Cursor); !strings.Contains(out, want) {
			t.Fatalf("%q の後にカーソル行 %q が描かれていない (cursor=%d offset=%d)\n%s",
				key, want, v.Cursor, v.offset, out)
		}
	}
}

// 閉じたら本文の glide を残さない (再表示の一瞬だけ古い位置から滑るのを防ぐ)。
func TestIssuesCloseStopsBodyGlide(t *testing.T) {
	v := newTestIssuesView()
	v.Shown = true
	startPagerGlide(t, &v.BodyPager, 0, 10)
	v.Close()
	if v.BodyPager.Animating() {
		t.Error("close で本文の glide が残る")
	}
}
