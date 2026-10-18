//go:build !nofixture

// fixture.go は main のテスト用の口なので、到達不能のコードの検査 (scripts/check_unused_excluding_tests.sh) は
// -tags=nofixture でこのファイルを外して走る。

package ratelimitview

import "time"

// Fixture は main のテストが残量ダッシュボードを特定の状態に置くための口 (doctorview などの Fixture と同じ作法)。
// 本番のコードは使わない (TestNoFixtureCallsInProduction が止める)。
type Fixture struct{ d *RatelimitDash }

// Fixture はテスト用の口を返す (上の型の注記)。
func (d *RatelimitDash) Fixture() Fixture { return Fixture{d} }

// SetShown は開いているかを置く (取得を起こさない)。
func (f Fixture) SetShown(on bool) { f.d.shown = on }

// FixtureSaveScreen / FixtureRemoveScreen は終了時の記憶を置く / 消す (起動時の復元を見るテスト用)。
func FixtureSaveScreen(now time.Time) error {
	return saveRatelimitScreen(ratelimitScreen{SavedAt: now})
}
func FixtureRemoveScreen() { removeRatelimitScreen() }
