//go:build !nofixture

// fixture.go は main のテスト用の口なので、到達不能のコードの検査 (scripts/check_unused_excluding_tests.sh) は
// -tags=nofixture でこのファイルを外して走る (ここを根に数えると、Fixture からしか呼ばれない内部を見落とす)。

package lanview

import (
	"time"

	"mac-lan-manager/lanmgr"
)

// Fixture は main のテスト (browseModel と組み合わせる配線・記憶・横断のテスト) が L の画面を読むための口
// (doctorview の Fixture と同じ作法)。本番のコードは使わない (structure_ratchet_test.go の TestNoFixtureCallsInProduction が止める)。
type Fixture struct{ v *LanView }

// Fixture はテスト用の口を返す (上の型の注記)。
func (v *LanView) Fixture() Fixture { return Fixture{v} }

// Model は中の lanmgr (閉じた後も持ち続ける。nil = 一度も開いていない)。
func (f Fixture) Model() *lanmgr.Model { return f.v.m }

// Watching は今待っている走査の合図のチャネル。
func (f Fixture) Watching() <-chan struct{} { return f.v.watching }

// FixtureChangedMsg は走査の合図の Msg を組む (Rearm の待ち直しの判断を見るテスト用)。
func FixtureChangedMsg(ch <-chan struct{}, ok bool) LanChangedMsg {
	return LanChangedMsg{ch: ch, ok: ok}
}

// FixtureSetOptions は lanmgr を組む口を fn に差し替え、戻す関数を返す (本物のネットワークの読み取りと Bonjour の走査を起こさない)。
func FixtureSetOptions(fn func() (lanmgr.Options, error)) (restore func()) {
	orig := lanOptions
	lanOptions = fn
	return func() { lanOptions = orig }
}

// FixtureSaveScreen は終了時の記憶を置く (起動時の復元を見るテスト用)。
func FixtureSaveScreen(p lanmgr.Place, now time.Time) error {
	return saveLanScreen(lanScreen{Place: p, SavedAt: now})
}
