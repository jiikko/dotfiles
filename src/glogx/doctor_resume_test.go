package main

import (
	"glogx/internal/app"
	"glogx/internal/doctorview"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 記憶の置き場をテスト用に隔離する。
func withDoctorStateDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
}

func TestDoctorScreenRoundTrip(t *testing.T) {
	withDoctorStateDir(t)
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
		withDoctorStateDir(t)
		if err := doctorview.SaveDoctorScreen(doctorview.DoctorScreen{Tab: int(doctorview.TabSvc), SavedAt: now.Add(-app.ScreenMemoryTTL)}); err != nil {
			t.Fatal(err)
		}
		if _, ok := doctorview.LoadDoctorScreen(now); ok {
			t.Error("期限切れを復元した")
		}
	})
	t.Run("未来の時刻", func(t *testing.T) {
		withDoctorStateDir(t)
		if err := doctorview.SaveDoctorScreen(doctorview.DoctorScreen{Tab: int(doctorview.TabSvc), SavedAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		if _, ok := doctorview.LoadDoctorScreen(now); ok {
			t.Error("未来の時刻を復元した (時計のずれ)")
		}
	})
	t.Run("壊れた JSON", func(t *testing.T) {
		withDoctorStateDir(t)
		write(t, "{ not json")
		if _, ok := doctorview.LoadDoctorScreen(now); ok {
			t.Error("壊れた記憶を復元した")
		}
	})
	t.Run("範囲外のタブは既定へ畳む", func(t *testing.T) {
		withDoctorStateDir(t)
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

// 終了時: 開いていたら覚える / 開いていなければ**消す**。
// 🚨 消さないと、一覧を見て閉じた次の起動で 2 回前の doctor が蘇る (issues 側と同じ規律)。
func TestRememberDoctorScreen(t *testing.T) {
	withDoctorStateDir(t)
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.Local)
	orig := app.TimeNow
	app.TimeNow = func() time.Time { return now }
	t.Cleanup(func() { app.TimeNow = orig })

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.doctorOv.Shown, m.doctorOv.Tab = true, doctorview.TabBrew
	m.rememberDoctorScreen()
	if tb, ok := doctorview.LoadDoctorScreen(now); !ok || tb != doctorview.TabBrew {
		t.Fatalf("開いていたのに覚えていない: %v ok=%v", tb, ok)
	}

	m.doctorOv.Shown = false
	m.rememberDoctorScreen()
	if _, ok := doctorview.LoadDoctorScreen(now); ok {
		t.Error("閉じて終了したのに記憶が残った (次の起動で蘇る)")
	}
}

// 起動時: 記憶があれば doctor を開いた状態で始まる。
func TestInitRestoresDoctorScreen(t *testing.T) {
	withDoctorStateDir(t)
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.Local)
	orig := app.TimeNow
	app.TimeNow = func() time.Time { return now }
	t.Cleanup(func() { app.TimeNow = orig })
	if err := doctorview.SaveDoctorScreen(doctorview.DoctorScreen{Tab: int(doctorview.TabSvc), SavedAt: now}); err != nil {
		t.Fatal(err)
	}

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	// 🚨 実スキャンを起こさない (start は同期で goroutine を起こす。issue 216)
	installInertDoctor(t, &m.doctorOv)
	_ = m.Init()
	if !m.doctorOv.Visible() {
		t.Fatal("記憶があるのに doctor が開いていない")
	}
	if m.doctorOv.Tab != doctorview.TabSvc {
		t.Errorf("タブが戻らない: %v", m.doctorOv.Tab)
	}
}

// 記憶が無ければ素の画面から始まる (勝手に doctor を出さない)。
func TestInitWithoutMemoryStaysOnLog(t *testing.T) {
	withDoctorStateDir(t)
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	installInertDoctor(t, &m.doctorOv)
	_ = m.Init()
	if m.doctorOv.Visible() {
		t.Error("記憶が無いのに doctor が開いた")
	}
}
