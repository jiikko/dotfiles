//go:build !nofixture

// fixture.go は main のテスト用の口なので、到達不能のコードの検査 (scripts/check_unused_excluding_tests.sh) は
// -tags=nofixture でこのファイルを外して走る (ここを根に数えると、Fixture からしか呼ばれない内部を見落とす。issue 817)。

package statusview

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/jiikko/dotfiles/src/tuikit/listnav"

	"glogx/internal/linecache"
)

// Fixture は main のテスト (browseModel と組み合わせる配線・横断の語彙・1 フレームの予算のテスト) が status viewer を
// 特定の状態に置く・読むための口 (doctorview / issuesview の Fixture と同じ作法)。本番のコードは使わない
// (structure_ratchet_test.go の TestNoFixtureCallsInProduction が止める)。browseModel が要らないテストは
// package statusview の中に書き、この口を使わない (issue 817)。
type Fixture struct{ v *StatusView }

// Fixture はテスト用の口を返す (上の型の注記)。
func (v *StatusView) Fixture() Fixture { return Fixture{v} }

// FixtureStubWorktree は git status の取得を fn (porcelain を返す) に差し替え、元に戻す関数を返す
// (本物の git を起こさずに status viewer を開くテスト用)。
func FixtureStubWorktree(fn func() (string, error)) (restore func()) {
	orig := loadWorktreeStatus
	loadWorktreeStatus = func() (worktreeStatus, error) {
		out, err := fn()
		if err != nil {
			return worktreeStatus{}, err
		}
		return parseWorktreeStatus(out), nil
	}
	return func() { loadWorktreeStatus = orig }
}

// FixtureLoadWorktree / FixtureLoadWorktreeDiff は本物の git status / diff の取得を 1 回走らせる
// (repo の設定のコマンドを起こさないことを見るテスト用)。
func FixtureLoadWorktree() error { _, err := loadWorktreeStatus(); return err }
func FixtureLoadWorktreeDiff(paths []string, staged bool) error {
	_, err := loadWorktreeDiff(paths, staged, false)
	return err
}

// ReceivePorcelain は porcelain (`git status --porcelain --branch -z` の出力) を今の世代の結果として受け取らせる。
func (f Fixture) ReceivePorcelain(porcelain string) tea.Cmd {
	return f.v.Receive(StatusLoadMsg{st: parseWorktreeStatus(porcelain), gen: f.v.gen})
}

// SetShown は開いているかを置く (取得や演出を起こさない)。SetCloseAnim は閉じる演出の有無。
func (f Fixture) SetShown(on bool)     { f.v.shown = on }
func (f Fixture) SetCloseAnim(on bool) { f.v.closeAnimOff = !on }

// ResetAnim は開く演出を着地済みにする。
func (f Fixture) ResetAnim() { f.v.animStart = time.Time{} }

func (f Fixture) Loading() bool             { return f.v.loading }
func (f Fixture) SetLoading(on bool)        { f.v.loading = on }
func (f Fixture) Cursor() int               { return f.v.cursor }
func (f Fixture) RowCount() int             { return len(f.v.rows) }
func (f Fixture) Discarding() bool          { return f.v.discarding }
func (f Fixture) SetDiscarding(on bool)     { f.v.discarding = on }
func (f Fixture) AskDiscard()               { f.v.askDiscard() }
func (f Fixture) Pager() *listnav.Pager     { return &f.v.pager }
func (f Fixture) Preview() *linecache.Cache { return &f.v.preview }

// OpenPager はカーソル行の全画面 diff を開く。SetPager は開いている pager のキーと見出しを置く (diff は取らない)。
func (f Fixture) OpenPager(vp StatusViewport) tea.Cmd { return f.v.openPager(vp) }
func (f Fixture) SetPager(key, title string)          { f.v.pagerKey, f.v.pagerTitle = key, title }

// PagerBox は pager の枠の行 (見出しの位置の表示を見るテスト用)。
func (f Fixture) PagerBox(o StatusRenderOpts) []string { return f.v.pagerBox(o) }

// SetPagerKey は開いている pager のキーだけを置く ("" = pager を閉じた状態)。
func (f Fixture) SetPagerKey(key string) { f.v.pagerKey = key }
