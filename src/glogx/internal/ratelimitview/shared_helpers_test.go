package ratelimitview

import (
	"path/filepath"
	"ratelimit/usage"
	"testing"
	"time"
)

// main のテスト (browseModel と組み合わせるもの) と同じヘルパーの、中のテスト用の写し (両方のテストが使っていたもの)。

func rlTestSnap() *usage.Snapshot {
	now := time.Date(2026, 8, 31, 22, 14, 0, 0, time.Local)
	return &usage.Snapshot{
		Version: "2.1.216",
		Windows: []usage.Window{
			{Label: "5h", Percent: 62, ResetAt: now.Add(108 * time.Minute), WindowMins: 300},
			{Label: "7d", Percent: 78, ResetAt: now.Add(3420 * time.Minute), WindowMins: 7 * 24 * 60},
		},
	}
}

// 記憶の置き場をテスト用に隔離する。
func withRatelimitStateDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
}
