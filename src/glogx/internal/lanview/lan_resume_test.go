package lanview

import (
	"glogx/internal/app"
	"os"
	"testing"
	"time"
)

// 期限切れ / 未来の時刻 / 時刻なし / 壊れた JSON は「復元しない」に倒す。
func TestLanScreenRejectsBadState(t *testing.T) {
	now := withFilerState(t) // 記憶の置き場の隔離と時計の固定 (F と同じ)
	if _, ok := LoadLanScreen(now); ok {
		t.Fatal("記憶が無いのに復元しようとした")
	}
	for _, c := range []struct {
		name string
		s    lanScreen
	}{
		{"期限切れ", lanScreen{SavedAt: now.Add(-app.ScreenMemoryTTL)}},
		{"未来の時刻", lanScreen{SavedAt: now.Add(time.Minute)}},
		{"時刻なし", lanScreen{}},
	} {
		if err := saveLanScreen(c.s); err != nil {
			t.Fatal(err)
		}
		if _, ok := LoadLanScreen(now); ok {
			t.Errorf("%s: 復元しようとした", c.name)
		}
	}
	path, _ := lanMemory.Path()
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadLanScreen(now); ok {
		t.Error("壊れた JSON から復元しようとした")
	}
}
