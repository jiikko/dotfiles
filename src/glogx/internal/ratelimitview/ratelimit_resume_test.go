package ratelimitview

import (
	"glogx/internal/app"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRatelimitScreenRoundTrip(t *testing.T) {
	withRatelimitStateDir(t)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.Local)
	if LoadRatelimitScreen(now) {
		t.Fatal("記憶が無いのに復元しようとした")
	}
	if err := saveRatelimitScreen(ratelimitScreen{SavedAt: now}); err != nil {
		t.Fatal(err)
	}
	if !LoadRatelimitScreen(now.Add(time.Minute)) {
		t.Fatal("覚えたのに復元しない")
	}
}

// 期限切れ / 未来の時刻 / 壊れた JSON / 時刻なしは「復元しない」に倒す
// (外部ファイル由来なので、勝手に全画面を出さない方が安全側。doctor 側と同じ規律)。
func TestRatelimitScreenRejectsBadState(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.Local)
	write := func(t *testing.T, body string) {
		t.Helper()
		p, err := ratelimitMemory.Path()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("期限切れ", func(t *testing.T) {
		withRatelimitStateDir(t)
		if err := saveRatelimitScreen(ratelimitScreen{SavedAt: now.Add(-app.ScreenMemoryTTL)}); err != nil {
			t.Fatal(err)
		}
		if LoadRatelimitScreen(now) {
			t.Error("期限切れを復元した")
		}
	})
	t.Run("未来の時刻", func(t *testing.T) {
		withRatelimitStateDir(t)
		if err := saveRatelimitScreen(ratelimitScreen{SavedAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		if LoadRatelimitScreen(now) {
			t.Error("未来の時刻を復元した (時計のずれ)")
		}
	})
	t.Run("壊れた JSON", func(t *testing.T) {
		withRatelimitStateDir(t)
		write(t, "{ not json")
		if LoadRatelimitScreen(now) {
			t.Error("壊れた記憶を復元した")
		}
	})
	t.Run("時刻なし", func(t *testing.T) {
		withRatelimitStateDir(t)
		write(t, "{}")
		if LoadRatelimitScreen(now) {
			t.Error("保存時刻の無い記憶を復元した (TTL が効かない)")
		}
	})
}
