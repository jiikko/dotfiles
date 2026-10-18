package filerview

import (
	"glogx/internal/app"
	"path/filepath"
	"testing"
	"time"
)

// main のテスト (browseModel と組み合わせるもの) と同じヘルパーの、中のテスト用の写し (両方のテストが使っていたもの)。

// 記憶の置き場を隔離し、時計を固定する。
func withFilerState(t *testing.T) time.Time {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.Local)
	orig := app.TimeNow
	app.TimeNow = func() time.Time { return now }
	t.Cleanup(func() { app.TimeNow = orig })
	return now
}
