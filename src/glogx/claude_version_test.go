package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestVersionLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"2.1.216", "2.1.216", false}, // 等しい
		{"2.1.216", "2.1.217", true},  // patch 差
		{"2.1.217", "2.1.216", false},
		{"2.1.999", "2.2.0", true},  // minor 差 (patch の桁違いに勝つ)
		{"2.9.0", "2.10.0", true},   // 数値比較 (辞書順なら逆転する)
		{"1.99.99", "2.0.0", true},  // major 差
		{"", "2.1.216", false},      // パース不能は通知しない側へ
		{"2.1", "2.1.216", false},   // セグメント数不一致
		{"2.1.x", "2.1.216", false}, // 数値でない
	}
	for _, c := range cases {
		if got := versionLess(c.a, c.b); got != c.want {
			t.Errorf("versionLess(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestClaudeVersionCacheRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude-latest-version.json")
	now := time.Now()

	if _, ok := loadClaudeVersionCache(path, now); ok {
		t.Fatal("欠損ファイルから ok が返った")
	}
	if err := saveClaudeVersionCache(path, "2.1.220", now); err != nil {
		t.Fatalf("save: %v", err)
	}
	latest, ok := loadClaudeVersionCache(path, now.Add(claudeVersionTTL-time.Minute))
	if !ok || latest != "2.1.220" {
		t.Fatalf("fresh 読み出し = (%q, %v), want (2.1.220, true)", latest, ok)
	}
	if _, ok := loadClaudeVersionCache(path, now.Add(claudeVersionTTL)); ok {
		t.Fatal("TTL 切れなのに ok が返った")
	}
}

// swapClaudeVersionFetchers は差し替え点 2 つを一時的に固定値へ差し替える。
func swapClaudeVersionFetchers(t *testing.T, latest, installed string) (latestCalls *int) {
	t.Helper()
	origLatest, origInstalled := fetchLatestClaudeVersion, fetchInstalledClaudeVersion
	t.Cleanup(func() {
		fetchLatestClaudeVersion, fetchInstalledClaudeVersion = origLatest, origInstalled
	})
	calls := 0
	fetchLatestClaudeVersion = func(context.Context) string { calls++; return latest }
	fetchInstalledClaudeVersion = func(context.Context) string { return installed }
	return &calls
}

func TestCheckClaudeVersionCmd(t *testing.T) {
	t.Run("新しいバージョンがあれば msg", func(t *testing.T) {
		t.Setenv("XDG_CACHE_HOME", t.TempDir())
		swapClaudeVersionFetchers(t, "2.1.220", "2.1.216")
		msg := checkClaudeVersionCmd()()
		got, ok := msg.(claudeUpdateAvailableMsg)
		if !ok || got.latest != "2.1.220" {
			t.Fatalf("msg = %#v, want claudeUpdateAvailableMsg{2.1.220}", msg)
		}
	})
	t.Run("同じバージョンなら nil", func(t *testing.T) {
		t.Setenv("XDG_CACHE_HOME", t.TempDir())
		swapClaudeVersionFetchers(t, "2.1.216", "2.1.216")
		if msg := checkClaudeVersionCmd()(); msg != nil {
			t.Fatalf("msg = %#v, want nil", msg)
		}
	})
	t.Run("latest 取得失敗なら nil でキャッシュも残さない", func(t *testing.T) {
		cacheDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cacheDir)
		swapClaudeVersionFetchers(t, "", "2.1.216")
		if msg := checkClaudeVersionCmd()(); msg != nil {
			t.Fatalf("msg = %#v, want nil", msg)
		}
		path := filepath.Join(cacheDir, "glog", claudeVersionCacheFile)
		if _, ok := loadClaudeVersionCache(path, time.Now()); ok {
			t.Fatal("取得失敗なのにキャッシュが保存された")
		}
	})
	t.Run("installed 取得失敗なら nil", func(t *testing.T) {
		t.Setenv("XDG_CACHE_HOME", t.TempDir())
		swapClaudeVersionFetchers(t, "2.1.220", "")
		if msg := checkClaudeVersionCmd()(); msg != nil {
			t.Fatalf("msg = %#v, want nil", msg)
		}
	})
	t.Run("fresh キャッシュがあれば registry へ出ない", func(t *testing.T) {
		cacheDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", cacheDir)
		path := filepath.Join(cacheDir, "glog", claudeVersionCacheFile)
		if err := saveClaudeVersionCache(path, "2.1.220", time.Now()); err != nil {
			t.Fatalf("save: %v", err)
		}
		calls := swapClaudeVersionFetchers(t, "9.9.9", "2.1.216")
		msg := checkClaudeVersionCmd()()
		got, ok := msg.(claudeUpdateAvailableMsg)
		if !ok || got.latest != "2.1.220" {
			t.Fatalf("msg = %#v, want キャッシュ値 2.1.220", msg)
		}
		if *calls != 0 {
			t.Fatalf("fresh キャッシュがあるのに fetch が %d 回呼ばれた", *calls)
		}
	})
}
