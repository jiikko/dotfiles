//go:build !nofixture

// fixture.go は main のテスト用の口なので、到達不能のコードの検査 (scripts/check_unused_excluding_tests.sh) は
// -tags=nofixture でこのファイルを外して走る (ここを根に数えると、Fixture からしか呼ばれない内部を見落とす。issue 817)。

package filerview

import (
	"time"

	"treefiler/filer"
)

// Fixture は main のテスト (browseModel と組み合わせる配線・記憶・横断のテスト) が F の画面を読む・置くための口
// (doctorview の Fixture と同じ作法)。本番のコードは使わない (structure_ratchet_test.go の TestNoFixtureCallsInProduction が止める)。
type Fixture struct{ v *FilerView }

// Fixture はテスト用の口を返す (上の型の注記)。
func (v *FilerView) Fixture() Fixture { return Fixture{v} }

// Model は中の treefiler (閉じた後も持ち続ける。nil = 一度も開いていない)。
func (f Fixture) Model() *filer.Model { return f.v.f }

// FixtureChangedMsg は変更の合図の Msg を組む (Rearm の待ち直しの判断を見るテスト用)。
func FixtureChangedMsg(ch <-chan struct{}, ok bool) FilerChangedMsg {
	return FilerChangedMsg{ch: ch, ok: ok}
}

// FixtureSaveScreen / FixtureRemoveScreen は終了時の記憶を置く / 消す (起動時の復元を見るテスト用)。
func FixtureSaveScreen(p filer.Place, now time.Time) error {
	return saveFilerScreen(filerScreen{Place: p, SavedAt: now})
}
func FixtureRemoveScreen() { removeFilerScreen() }
