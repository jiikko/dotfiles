//go:build !nofixture

// fixture.go は main のテスト用の口なので、到達不能のコードの検査 (scripts/check_unused_excluding_tests.sh) は
// -tags=nofixture でこのファイルを外して走る (ここを根に数えると、Fixture からしか呼ばれない内部を見落とす。issue 817)。

package usageview

import (
	"time"

	"ratelimit/usage"
)

// Fixture は main のテスト (browseModel と組み合わせる配線のテスト) が usage の枠を特定の状態に置く・読むための口
// (doctorview などの Fixture と同じ作法)。本番のコードは使わない (TestNoFixtureCallsInProduction が止める。issue 817)。
type Fixture struct{ o *UsageOverlay }

// Fixture はテスト用の口を返す (上の型の注記)。
func (o *UsageOverlay) Fixture() Fixture { return Fixture{o} }

// FixtureUsageMsg は取得の結果の Msg を組む (Handle へ渡して受け取りを見るテスト用)。
func FixtureUsageMsg(snap *usage.Snapshot, err error, part *usage.Part) UsageMsg {
	return UsageMsg{snap: snap, err: err, part: part}
}

// FixtureSaveCache は起動時に読むキャッシュを snap で書く (キャッシュの数字を先に出す配線のテスト用)。
func FixtureSaveCache(snap *usage.Snapshot, now time.Time) error {
	path, err := usageCachePath()
	if err != nil {
		return err
	}
	return saveUsageCache(path, snap, now)
}

// InFlight / SetInFlight は取得の周が走っている印。Fetching は取得の ctx を持っているか (取り消せる取得が在るか)。
func (f Fixture) InFlight() bool      { return f.o.inFlight }
func (f Fixture) SetInFlight(on bool) { f.o.inFlight = on }
func (f Fixture) Fetching() bool      { return f.o.cancel != nil }

// SetFetchedAt は最後に取れた時刻を置く (鮮度の判定を動かすテスト用)。EndRound は取得の周を閉じる。
func (f Fixture) SetFetchedAt(t time.Time) { f.o.fetchedAt = t }
func (f Fixture) EndRound()                { f.o.endRound() }
