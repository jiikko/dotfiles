package doctorview_test

import (
	"glogx/internal/app"
	"glogx/internal/doctorview"
	"glogx/internal/doctorview/doctortest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDoctorScreenRoundTrip(t *testing.T) {
	doctortest.WithDoctorStateDir(t)
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.Local)
	if _, ok := doctorview.LoadDoctorScreen(now); ok {
		t.Fatal("記憶が無いのに復元しようとした")
	}
	if err := doctorview.SaveDoctorScreen(doctorview.DoctorScreen{Tab: int(doctorview.TabBrew), SavedAt: now}); err != nil {
		t.Fatal(err)
	}
	tb, ok := doctorview.LoadDoctorScreen(now.Add(time.Minute))
	if !ok || tb != doctorview.TabBrew {
		t.Fatalf("タブが戻らない: %v ok=%v", tb, ok)
	}
}

// 🚨 外部ファイル由来の値をそのまま添字に使わない (一般ユーザー権限で書き換えられる)。
// 期限切れ / 未来の時刻 / 壊れた JSON は「復元しない」に倒す (勝手に画面を出さない方が安全側)。
func TestDoctorScreenRejectsBadState(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.Local)
	write := func(t *testing.T, body string) {
		t.Helper()
		p, err := doctorview.DoctorMemory.Path()
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
		doctortest.WithDoctorStateDir(t)
		if err := doctorview.SaveDoctorScreen(doctorview.DoctorScreen{Tab: int(doctorview.TabSvc), SavedAt: now.Add(-app.ScreenMemoryTTL)}); err != nil {
			t.Fatal(err)
		}
		if _, ok := doctorview.LoadDoctorScreen(now); ok {
			t.Error("期限切れを復元した")
		}
	})
	t.Run("未来の時刻", func(t *testing.T) {
		doctortest.WithDoctorStateDir(t)
		if err := doctorview.SaveDoctorScreen(doctorview.DoctorScreen{Tab: int(doctorview.TabSvc), SavedAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		if _, ok := doctorview.LoadDoctorScreen(now); ok {
			t.Error("未来の時刻を復元した (時計のずれ)")
		}
	})
	t.Run("壊れた JSON", func(t *testing.T) {
		doctortest.WithDoctorStateDir(t)
		write(t, "{ not json")
		if _, ok := doctorview.LoadDoctorScreen(now); ok {
			t.Error("壊れた記憶を復元した")
		}
	})
	t.Run("範囲外のタブは既定へ畳む", func(t *testing.T) {
		doctortest.WithDoctorStateDir(t)
		if err := doctorview.SaveDoctorScreen(doctorview.DoctorScreen{Tab: 99, SavedAt: now}); err != nil {
			t.Fatal(err)
		}
		tb, ok := doctorview.LoadDoctorScreen(now)
		if !ok {
			t.Fatal("復元自体はする")
		}
		if tb != doctorview.TabDisk {
			t.Errorf("範囲外のタブを添字に使った: %v", tb)
		}
	})
}
