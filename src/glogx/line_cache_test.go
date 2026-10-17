package main

import (
	"fmt"
	"glogx/internal/linecache"
	"testing"
)

// overlay キャッシュはエントリ上限で古い順に落ち、表示中キーは消えない (issue 029 P2)。
func TestOverlayCacheEvictsOldestKeepingCurrent(t *testing.T) {
	o := newDiffOverlay()
	o.sha = "sha-0" // 最古を表示中にして「表示中は evict されない」を同時に検証
	for i := 0; i <= linecache.Limit+1; i++ {
		_ = o.receive(diffMsg{sha: fmt.Sprintf("sha-%d", i), lines: []string{"x"}})
	}
	if o.cache.Len() != linecache.Limit {
		t.Fatalf("cache エントリ数 = %d; want 上限 %d", o.cache.Len(), linecache.Limit)
	}
	if !o.cache.Has("sha-0") {
		t.Error("表示中の sha-0 が evict された (画面が突然「diff はありません」になる)")
	}
	// 表示中でない最古 (sha-1, sha-2) が落ち、新しいものは残る
	for _, gone := range []string{"sha-1", "sha-2"} {
		if o.cache.Has(gone) {
			t.Errorf("%s が evict されていない", gone)
		}
	}
	last := fmt.Sprintf("sha-%d", linecache.Limit+1)
	if !o.cache.Has(last) {
		t.Errorf("最新の %s が消えた", last)
	}
}

// job 詳細側も同じ evict が効く (receive の currentKey が keep 対象)。
func TestJobDetailCacheEvicts(t *testing.T) {
	o := newJobDetailOverlay()
	for i := 0; i <= linecache.Limit; i++ {
		key := fmt.Sprintf("sha/%d", i)
		o.receive(jobDetailMsg{key: key, lines: []string{"log"}}, "sha/0", 10)
	}
	if o.cache.Len() != linecache.Limit {
		t.Fatalf("cache エントリ数 = %d; want 上限 %d", o.cache.Len(), linecache.Limit)
	}
	if !o.cache.Has("sha/0") {
		t.Error("表示中 (currentKey) の sha/0 が evict された")
	}
	if o.cache.Has("sha/1") {
		t.Error("表示中でない最古 sha/1 が evict されていない")
	}
}

// --- lineCache 自体の契約 ---
