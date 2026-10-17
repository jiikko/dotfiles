//go:build !nofixture

// fixture.go は main のテスト用の口なので、到達不能のコードの検査 (scripts/check_unused_excluding_tests.sh) は
// -tags=nofixture でこのファイルを外して走る (ここを根に数えると、Fixture からしか呼ばれない内部を見落とす。issue 817)。

package commitview

import (
	"github.com/jiikko/dotfiles/src/tuikit/listnav"

	"glogx/internal/gitdata"
	"glogx/internal/linecache"
)

// DiffFixture / JobDetailFixture / PrStatusFixture は main のテスト (browseModel と組み合わせる配線・描画の予算・滑りのテスト) が
// overlay の状態を置く・読むための口 (doctorview の Fixture と同じ作法)。中の状態へのポインタを返す。
// 本番のコードは使わない (structure_ratchet_test.go の TestNoFixtureCallsInProduction が止める)。
type DiffFixture struct{ o *DiffOverlay }
type JobDetailFixture struct{ o *JobDetailOverlay }
type PrStatusFixture struct{ o *PrStatusOverlay }

// Fixture はテスト用の口を返す (上の型の注記)。
func (o *DiffOverlay) Fixture() DiffFixture           { return DiffFixture{o} }
func (o *JobDetailOverlay) Fixture() JobDetailFixture { return JobDetailFixture{o} }
func (o *PrStatusOverlay) Fixture() PrStatusFixture   { return PrStatusFixture{o} }

func (f DiffFixture) Sha() *string                             { return &f.o.sha }
func (f DiffFixture) Pager() *listnav.Pager                    { return &f.o.pager }
func (f DiffFixture) Cache() *linecache.Cache                  { return &f.o.cache }
func (f JobDetailFixture) Open() *bool                         { return &f.o.open }
func (f JobDetailFixture) Pager() *listnav.Pager               { return &f.o.pager }
func (f JobDetailFixture) Cache() *linecache.Cache             { return &f.o.cache }
func (f PrStatusFixture) Sha() *string                         { return &f.o.sha }
func (f PrStatusFixture) Busy() *map[string]bool               { return &f.o.busy }
func (f PrStatusFixture) Cache() *map[string]*gitdata.PRStatus { return &f.o.cache }
