package main

import (
	"fmt"
	"testing"
)

// overlay キャッシュはエントリ上限で古い順に落ち、表示中キーは消えない (issue 029 P2)。
func TestOverlayCacheEvictsOldestKeepingCurrent(t *testing.T) {
	o := newDiffOverlay()
	o.sha = "sha-0" // 最古を表示中にして「表示中は evict されない」を同時に検証
	for i := 0; i <= overlayCacheLimit+1; i++ {
		_ = o.receive(diffMsg{sha: fmt.Sprintf("sha-%d", i), lines: []string{"x"}})
	}
	if len(o.cache) != overlayCacheLimit {
		t.Fatalf("cache エントリ数 = %d; want 上限 %d", len(o.cache), overlayCacheLimit)
	}
	if _, ok := o.cache["sha-0"]; !ok {
		t.Error("表示中の sha-0 が evict された (画面が突然「diff はありません」になる)")
	}
	// 表示中でない最古 (sha-1, sha-2) が落ち、新しいものは残る
	for _, gone := range []string{"sha-1", "sha-2"} {
		if _, ok := o.cache[gone]; ok {
			t.Errorf("%s が evict されていない", gone)
		}
	}
	last := fmt.Sprintf("sha-%d", overlayCacheLimit+1)
	if _, ok := o.cache[last]; !ok {
		t.Errorf("最新の %s が消えた", last)
	}
}

// job 詳細側も同じ evict が効く (receive の currentKey が keep 対象)。
func TestJobDetailCacheEvicts(t *testing.T) {
	o := newJobDetailOverlay()
	for i := 0; i <= overlayCacheLimit; i++ {
		key := fmt.Sprintf("sha/%d", i)
		o.receive(jobDetailMsg{key: key, lines: []string{"log"}}, "sha/0", 10)
	}
	if len(o.cache) != overlayCacheLimit {
		t.Fatalf("cache エントリ数 = %d; want 上限 %d", len(o.cache), overlayCacheLimit)
	}
	if _, ok := o.cache["sha/0"]; !ok {
		t.Error("表示中 (currentKey) の sha/0 が evict された")
	}
	if _, ok := o.cache["sha/1"]; ok {
		t.Error("表示中でない最古 sha/1 が evict されていない")
	}
}
