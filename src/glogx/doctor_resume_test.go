package main

import (
	"glogx/internal/app"
	"glogx/internal/doctorview"
	"glogx/internal/doctorview/doctortest"
	"testing"
	"time"
)

// 終了時: 開いていたら覚える / 開いていなければ**消す**。
// 🚨 消さないと、一覧を見て閉じた次の起動で 2 回前の doctor が蘇る (issues 側と同じ規律)。
func TestRememberDoctorScreen(t *testing.T) {
	doctortest.WithDoctorStateDir(t)
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
	doctortest.WithDoctorStateDir(t)
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
	doctortest.WithDoctorStateDir(t)
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	installInertDoctor(t, &m.doctorOv)
	_ = m.Init()
	if m.doctorOv.Visible() {
		t.Error("記憶が無いのに doctor が開いた")
	}
}
