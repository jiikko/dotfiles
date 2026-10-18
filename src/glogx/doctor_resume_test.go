package main

import (
	"glogx/internal/app"
	"glogx/internal/doctorview"
	"glogx/internal/doctorview/doctortest"
	"glogx/internal/gitdata"
	"testing"
	"time"
)

// 起動時: 記憶があれば doctor を開いた状態で始まる。
func TestInitRestoresDoctorScreen(t *testing.T) {
	doctortest.WithDoctorStateDir(t)
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.Local)
	orig := app.TimeNow
	app.TimeNow = func() time.Time { return now }
	t.Cleanup(func() { app.TimeNow = orig })
	rememberDoctorShown(now, doctorview.TabSvc)

	m := newTestBrowse(t, 1, map[string]gitdata.CIState{}, nil)
	// 🚨 実スキャンを起こさない (start は同期で goroutine を起こす)
	installInertDoctor(t, &m.doctorOv)
	_ = m.Init()
	if !m.doctorOv.Visible() {
		t.Fatal("記憶があるのに doctor が開いていない")
	}
	if tb := m.doctorOv.Fixture().Tab(); tb != doctorview.TabSvc {
		t.Errorf("タブが戻らない: %v", tb)
	}
}

// 記憶が無ければ素の画面から始まる (勝手に doctor を出さない)。
func TestInitWithoutMemoryStaysOnLog(t *testing.T) {
	doctortest.WithDoctorStateDir(t)
	m := newTestBrowse(t, 1, map[string]gitdata.CIState{}, nil)
	installInertDoctor(t, &m.doctorOv)
	_ = m.Init()
	if m.doctorOv.Visible() {
		t.Error("記憶が無いのに doctor が開いた")
	}
}

// rememberDoctorShown は「doctor を tab で出したまま終了した」記憶を書く (起動時の復元のテストの前提)。
func rememberDoctorShown(now time.Time, tab doctorview.DoctorTab) {
	var v doctorview.DoctorView
	v.Fixture().Show()
	v.Fixture().SetTab(tab)
	v.RememberScreen(now)
}
