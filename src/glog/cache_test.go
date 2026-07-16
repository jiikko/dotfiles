package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCacheTTLByState(t *testing.T) {
	now := time.Now()
	tests := []struct {
		state CIState
		age   time.Duration
		fresh bool
	}{
		// issue の TTL 表: success/failure 24h / neutral 1h / pending 10s / none 5m / unknown 30s
		{StateSuccess, 23 * time.Hour, true},
		{StateSuccess, 25 * time.Hour, false},
		{StateFailure, 23 * time.Hour, true},
		{StateNeutral, 30 * time.Minute, true},
		{StateNeutral, 2 * time.Hour, false},
		{StatePending, 5 * time.Second, true},
		{StatePending, 30 * time.Second, false},
		{StateNone, 4 * time.Minute, true},
		{StateNone, 6 * time.Minute, false},
		{StateUnknown, 10 * time.Second, true},
		{StateUnknown, time.Minute, false},
	}
	for _, tt := range tests {
		entry := cacheEntry{State: tt.state, FetchedAt: now.Add(-tt.age)}
		if got := entry.fresh(now); got != tt.fresh {
			t.Errorf("%s の %v 経過: fresh = %v; want %v", tt.state, tt.age, got, tt.fresh)
		}
	}
}

func TestCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner", "repo.json")
	now := time.Now()
	fetched := map[string]CIState{
		"sha-success": StateSuccess,
		"sha-pending": StatePending,
		"sha-unknown": StateUnknown, // 取得失敗は保存しない
	}
	if err := SaveCache(path, fetched, now); err != nil {
		t.Fatal(err)
	}
	got := LoadCache(path, now)
	if got["sha-success"] != StateSuccess {
		t.Errorf("sha-success = %v", got["sha-success"])
	}
	if got["sha-pending"] != StatePending {
		t.Errorf("sha-pending = %v", got["sha-pending"])
	}
	if _, ok := got["sha-unknown"]; ok {
		t.Errorf("unknown がキャッシュに保存されている")
	}
	// pending の TTL (10s) 経過後は返らないが success (24h) は残る
	later := now.Add(time.Minute)
	got = LoadCache(path, later)
	if _, ok := got["sha-pending"]; ok {
		t.Errorf("TTL 切れの pending が返った")
	}
	if got["sha-success"] != StateSuccess {
		t.Errorf("TTL 内の success が消えた")
	}
}

func TestCacheMergePreservesOtherSHAs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repo.json")
	now := time.Now()
	if err := SaveCache(path, map[string]CIState{"sha-a": StateSuccess}, now); err != nil {
		t.Fatal(err)
	}
	if err := SaveCache(path, map[string]CIState{"sha-b": StateFailure}, now); err != nil {
		t.Fatal(err)
	}
	got := LoadCache(path, now)
	if got["sha-a"] != StateSuccess || got["sha-b"] != StateFailure {
		t.Errorf("マージ結果 = %v; 既存エントリが消えた", got)
	}
}

func TestCachePruneOldEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "repo.json")
	old := time.Now().Add(-cacheRetention - time.Hour)
	if err := SaveCache(path, map[string]CIState{"sha-old": StateSuccess}, old); err != nil {
		t.Fatal(err)
	}
	if err := SaveCache(path, map[string]CIState{"sha-new": StateSuccess}, time.Now()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sha-old") {
		t.Errorf("保持期間超過のエントリが間引かれていない: %s", data)
	}
}

func TestLoadCacheMissingOrBroken(t *testing.T) {
	if got := LoadCache(filepath.Join(t.TempDir(), "nope.json"), time.Now()); len(got) != 0 {
		t.Errorf("欠損ファイルで %v", got)
	}
	path := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadCache(path, time.Now()); len(got) != 0 {
		t.Errorf("破損ファイルで %v", got)
	}
}

func TestCachePathUsesXDG(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-test")
	path, err := CachePath(Repo{Owner: "o", Name: "r"})
	if err != nil {
		t.Fatal(err)
	}
	want := "/tmp/xdg-test/glog/github.com/o/r.json"
	if path != want {
		t.Errorf("CachePath = %s; want %s", path, want)
	}
}
