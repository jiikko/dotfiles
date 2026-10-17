package linecache

import "testing"

// begin は single-flight の唯一の入口。キャッシュ済み / 取得中なら発行させない。
func TestLineCacheBeginIsSingleFlight(t *testing.T) {
	c := New()
	if !c.Begin("a") {
		t.Fatal("初回の begin が false (取得を発行できない)")
	}
	if c.Begin("a") {
		t.Error("取得中の key で begin が true (二重発行になる)")
	}
	c.Store("a", []string{"x"}, "")
	if c.Begin("a") {
		t.Error("キャッシュ済みの key で begin が true (取り直してしまう)")
	}
	if c.Loading("a") {
		t.Error("store 後も取得中のまま (スピナーが止まらない)")
	}
}

// abort は結果を入れずに札だけ降ろす (取得失敗の経路)。次の begin は通ること。
func TestLineCacheAbortAllowsRetry(t *testing.T) {
	c := New()
	c.Begin("a")
	c.Abort("a")
	if c.Has("a") {
		t.Error("abort でキャッシュに入ってしまった")
	}
	if !c.Begin("a") {
		t.Error("abort 後に再取得できない (失敗したら二度と取れない)")
	}
}

// clearBusy は札だけ降ろしてキャッシュを残す (viewer を閉じたときの後始末)。
func TestLineCacheClearBusyKeepsEntries(t *testing.T) {
	c := New()
	c.Store("a", []string{"x"}, "")
	c.Begin("b")
	c.ClearBusy()
	if c.Fetching() {
		t.Error("clearBusy 後も fetching (フレーム tick が回り続ける)")
	}
	if !c.Has("a") {
		t.Error("clearBusy がキャッシュまで捨てた (閉じ直しで再取得になる)")
	}
}

// 同じ key を上書きしても order は増えない (order と entries がずれると evict が壊れる)。
func TestLineCacheRestoreDoesNotDuplicateOrder(t *testing.T) {
	c := New()
	c.Store("a", []string{"1"}, "")
	c.Store("a", []string{"2"}, "")
	if len(c.order) != 1 {
		t.Fatalf("order = %d, want 1 (同じ key の再格納で重複した)", len(c.order))
	}
	if len(c.entries) != len(c.order) {
		t.Fatalf("entries = %d, order = %d (ずれると evict が捨て漏れる)", len(c.entries), len(c.order))
	}
}

// clearEntries は中身だけ捨てて取得中の札を残す。
//
// 🚨 回帰防止 (セルフレビュー 2026-08-05): linecache.Cache へ集約したとき、statusView の
// 「外部編集で古い diff を捨てる」経路を reset() にしてしまい、走行中の取得の札まで降ろしていた。
// あの経路は直後に取り直しを予約するので、札が無いと同じキーを二重に取りに行く (git diff が
// 2 本走る)。移行前は cache と order だけを捨てていたので、これは移行で入れた挙動変化だった。
func TestLineCacheClearEntriesKeepsBusy(t *testing.T) {
	c := New()
	c.Store("cached", []string{"x"}, "")
	c.Begin("inflight")

	c.ClearEntries()
	if c.Has("cached") {
		t.Error("clearEntries が中身を捨てていない")
	}
	if len(c.order) != 0 {
		t.Errorf("order が残った (%d) — evict の数え方がずれる", len(c.order))
	}
	if !c.Loading("inflight") {
		t.Fatal("走行中の札まで降ろした (直後の取り直し予約と二重に取りに行く)")
	}
	if c.Begin("inflight") {
		t.Error("走行中なのに begin が通った (二重発行になる)")
	}
}
