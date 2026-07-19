package main

import (
	"fmt"
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
		"sha-unknown": StateUnknown, // 取得失敗も 30 秒の負キャッシュとして保存する
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
	if got["sha-unknown"] != StateUnknown {
		t.Errorf("unknown が負キャッシュされていない: %v", got["sha-unknown"])
	}
	// pending (10s) と unknown (30s) は 1 分後に失効するが success (24h) は残る
	later := now.Add(time.Minute)
	got = LoadCache(path, later)
	if _, ok := got["sha-pending"]; ok {
		t.Errorf("TTL 切れの pending が返った")
	}
	if _, ok := got["sha-unknown"]; ok {
		t.Errorf("TTL 切れの unknown が返った")
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

func TestCachePruneExpiredEntries(t *testing.T) {
	// TTL 切れのエントリは LoadCache が無視する死データなので、保存時に間引かれる
	// (ファイルが膨れ続けない)
	path := filepath.Join(t.TempDir(), "repo.json")
	old := time.Now().Add(-25 * time.Hour) // success の TTL (24h) 超過
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
		t.Errorf("TTL 切れのエントリが間引かれていない: %s", data)
	}
	if !strings.Contains(string(data), "sha-new") {
		t.Errorf("有効なエントリまで消えている: %s", data)
	}
}

func TestCacheEntryCountCap(t *testing.T) {
	// エントリ数はハードキャップで頭打ちになり、新しいものが優先で残る
	path := filepath.Join(t.TempDir(), "repo.json")
	now := time.Now()
	older := map[string]CIState{}
	for i := range maxCacheEntries {
		older[fmt.Sprintf("sha-old-%04d", i)] = StateSuccess
	}
	if err := SaveCache(path, older, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := SaveCache(path, map[string]CIState{"sha-newest": StateFailure}, now); err != nil {
		t.Fatal(err)
	}
	got := LoadCache(path, now)
	if len(got) != maxCacheEntries {
		t.Errorf("エントリ数 = %d; want 上限 %d", len(got), maxCacheEntries)
	}
	if got["sha-newest"] != StateFailure {
		t.Errorf("最新エントリが上限間引きで消えた")
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

// rename が失敗した場合 (書き込み先がディレクトリ等) に temp ファイルが残らないこと。
// writeAtomic は Write/Close 失敗時は掃除していたが rename 失敗だけ漏れていて、
// キャッシュディレクトリに .glog-cache-* が蓄積しうる穴があった (2026-07-17 監査で検出)。
func TestSaveCacheCleansTempOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	// 書き込み先パスに既存ディレクトリを置くと os.Rename が失敗する
	path := filepath.Join(dir, "repo.json")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SaveCache(path, map[string]CIState{"sha": StateSuccess}, time.Now()); err == nil {
		t.Fatal("rename が失敗するはずの構成でエラーが返らない")
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".glog-cache-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Errorf("rename 失敗後に temp ファイルが残っている: %v", leftovers)
	}
}
