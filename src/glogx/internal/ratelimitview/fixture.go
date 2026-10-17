//go:build !nofixture

// fixture.go は main のテスト用の口なので、到達不能のコードの検査 (scripts/check_unused_excluding_tests.sh) は
// -tags=nofixture でこのファイルを外して走る (issue 817)。

package ratelimitview

// Fixture は main のテストが残量ダッシュボードを特定の状態に置くための口 (doctorview などの Fixture と同じ作法)。
// 本番のコードは使わない (TestNoFixtureCallsInProduction が止める。issue 817)。
type Fixture struct{ d *RatelimitDash }

// Fixture はテスト用の口を返す (上の型の注記)。
func (d *RatelimitDash) Fixture() Fixture { return Fixture{d} }

// SetShown は開いているかを置く (取得を起こさない)。
func (f Fixture) SetShown(on bool) { f.d.shown = on }
