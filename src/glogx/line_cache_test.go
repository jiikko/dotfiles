package main

import (
	"fmt"
	"testing"
)

// overlay キャッシュはエントリ上限で古い順に落ち、表示中キーは消えない (issue 029 P2)。
func TestOverlayCacheEvictsOldestKeepingCurrent(t *testing.T) {
	o := newDiffOverlay()
	o.sha = "sha-0" // 最古を表示中にして「表示中は evict されない」を同時に検証
	for i := 0; i <= lineCacheLimit+1; i++ {
		_ = o.receive(diffMsg{sha: fmt.Sprintf("sha-%d", i), lines: []string{"x"}})
	}
	if len(o.lines.entries) != lineCacheLimit {
		t.Fatalf("cache エントリ数 = %d; want 上限 %d", len(o.lines.entries), lineCacheLimit)
	}
	if _, ok := o.lines.entries["sha-0"]; !ok {
		t.Error("表示中の sha-0 が evict された (画面が突然「diff はありません」になる)")
	}
	// 表示中でない最古 (sha-1, sha-2) が落ち、新しいものは残る
	for _, gone := range []string{"sha-1", "sha-2"} {
		if _, ok := o.lines.entries[gone]; ok {
			t.Errorf("%s が evict されていない", gone)
		}
	}
	last := fmt.Sprintf("sha-%d", lineCacheLimit+1)
	if _, ok := o.lines.entries[last]; !ok {
		t.Errorf("最新の %s が消えた", last)
	}
}

// job 詳細側も同じ evict が効く (receive の currentKey が keep 対象)。
func TestJobDetailCacheEvicts(t *testing.T) {
	o := newJobDetailOverlay()
	for i := 0; i <= lineCacheLimit; i++ {
		key := fmt.Sprintf("sha/%d", i)
		o.receive(jobDetailMsg{key: key, lines: []string{"log"}}, "sha/0", 10)
	}
	if len(o.logs.entries) != lineCacheLimit {
		t.Fatalf("cache エントリ数 = %d; want 上限 %d", len(o.logs.entries), lineCacheLimit)
	}
	if _, ok := o.logs.entries["sha/0"]; !ok {
		t.Error("表示中 (currentKey) の sha/0 が evict された")
	}
	if _, ok := o.logs.entries["sha/1"]; ok {
		t.Error("表示中でない最古 sha/1 が evict されていない")
	}
}

// --- lineCache 自体の契約 ---

// begin は single-flight の唯一の入口。キャッシュ済み / 取得中なら発行させない。
func TestLineCacheBeginIsSingleFlight(t *testing.T) {
	c := newLineCache()
	if !c.begin("a") {
		t.Fatal("初回の begin が false (取得を発行できない)")
	}
	if c.begin("a") {
		t.Error("取得中の key で begin が true (二重発行になる)")
	}
	c.store("a", []string{"x"}, "")
	if c.begin("a") {
		t.Error("キャッシュ済みの key で begin が true (取り直してしまう)")
	}
	if c.loading("a") {
		t.Error("store 後も取得中のまま (スピナーが止まらない)")
	}
}

// abort は結果を入れずに札だけ降ろす (取得失敗の経路)。次の begin は通ること。
func TestLineCacheAbortAllowsRetry(t *testing.T) {
	c := newLineCache()
	c.begin("a")
	c.abort("a")
	if c.has("a") {
		t.Error("abort でキャッシュに入ってしまった")
	}
	if !c.begin("a") {
		t.Error("abort 後に再取得できない (失敗したら二度と取れない)")
	}
}

// clearBusy は札だけ降ろしてキャッシュを残す (viewer を閉じたときの後始末)。
func TestLineCacheClearBusyKeepsEntries(t *testing.T) {
	c := newLineCache()
	c.store("a", []string{"x"}, "")
	c.begin("b")
	c.clearBusy()
	if c.fetching() {
		t.Error("clearBusy 後も fetching (フレーム tick が回り続ける)")
	}
	if !c.has("a") {
		t.Error("clearBusy がキャッシュまで捨てた (閉じ直しで再取得になる)")
	}
}

// 同じ key を上書きしても order は増えない (order と entries がずれると evict が壊れる)。
func TestLineCacheRestoreDoesNotDuplicateOrder(t *testing.T) {
	c := newLineCache()
	c.store("a", []string{"1"}, "")
	c.store("a", []string{"2"}, "")
	if len(c.order) != 1 {
		t.Fatalf("order = %d, want 1 (同じ key の再格納で重複した)", len(c.order))
	}
	if len(c.entries) != len(c.order) {
		t.Fatalf("entries = %d, order = %d (ずれると evict が捨て漏れる)", len(c.entries), len(c.order))
	}
}
