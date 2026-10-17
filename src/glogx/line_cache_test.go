package main

import (
	"fmt"
	"glogx/internal/commitview"
	"glogx/internal/linecache"
	"testing"
)

// overlay キャッシュはエントリ上限で古い順に落ち、表示中キーは消えない (issue 029 P2)。
func TestOverlayCacheEvictsOldestKeepingCurrent(t *testing.T) {
	o := commitview.NewDiffOverlay()
	(*o.Fixture().Sha()) = "sha-0" // 最古を表示中にして「表示中は evict されない」を同時に検証
	for i := 0; i <= linecache.Limit+1; i++ {
		_ = o.Receive(commitview.NewDiffMsg(fmt.Sprintf("sha-%d", i), []string{"x"}, nil))
	}
	if o.Fixture().Cache().Len() != linecache.Limit {
		t.Fatalf("cache エントリ数 = %d; want 上限 %d", o.Fixture().Cache().Len(), linecache.Limit)
	}
	if !o.Fixture().Cache().Has("sha-0") {
		t.Error("表示中の sha-0 が evict された (画面が突然「diff はありません」になる)")
	}
	// 表示中でない最古 (sha-1, sha-2) が落ち、新しいものは残る
	for _, gone := range []string{"sha-1", "sha-2"} {
		if o.Fixture().Cache().Has(gone) {
			t.Errorf("%s が evict されていない", gone)
		}
	}
	last := fmt.Sprintf("sha-%d", linecache.Limit+1)
	if !o.Fixture().Cache().Has(last) {
		t.Errorf("最新の %s が消えた", last)
	}
}

// job 詳細側も同じ evict が効く (receive の currentKey が keep 対象)。
func TestJobDetailCacheEvicts(t *testing.T) {
	o := commitview.NewJobDetailOverlay()
	for i := 0; i <= linecache.Limit; i++ {
		key := fmt.Sprintf("sha/%d", i)
		o.Receive(commitview.NewJobDetailMsg(key, []string{"log"}, nil), "sha/0", 10)
	}
	if o.Fixture().Cache().Len() != linecache.Limit {
		t.Fatalf("cache エントリ数 = %d; want 上限 %d", o.Fixture().Cache().Len(), linecache.Limit)
	}
	if !o.Fixture().Cache().Has("sha/0") {
		t.Error("表示中 (currentKey) の sha/0 が evict された")
	}
	if o.Fixture().Cache().Has("sha/1") {
		t.Error("表示中でない最古 sha/1 が evict されていない")
	}
}

// --- lineCache 自体の契約 ---
