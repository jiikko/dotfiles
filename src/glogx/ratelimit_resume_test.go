package main

import (
	"glogx/internal/app"
	"glogx/internal/doctorview"
	"glogx/internal/ratelimitview"
	"path/filepath"
	"testing"
	"time"
)

// 記憶の置き場をテスト用に隔離する。
func withRatelimitStateDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
}

// 終了時: 開いていたら覚える / 開いていなければ**消す**。
func TestRememberRatelimitScreen(t *testing.T) {
	withRatelimitStateDir(t)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.Local)
	orig := app.TimeNow
	app.TimeNow = func() time.Time { return now }
	t.Cleanup(func() { app.TimeNow = orig })

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.rlDash.Fixture().SetShown(true)
	m.rlDash.RememberScreen(now)
	if !ratelimitview.LoadRatelimitScreen(now) {
		t.Fatal("開いていたのに覚えていない")
	}

	m.rlDash.Fixture().SetShown(false)
	m.rlDash.RememberScreen(now)
	if ratelimitview.LoadRatelimitScreen(now) {
		t.Error("閉じて終了したのに記憶が残った (次の起動で蘇る)")
	}
}

// 起動時: 記憶があればダッシュボードを開いた状態で始まる (ユーザー要望 2026-09-05)。
func TestInitRestoresRatelimitScreen(t *testing.T) {
	withRatelimitStateDir(t)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.Local)
	orig := app.TimeNow
	app.TimeNow = func() time.Time { return now }
	t.Cleanup(func() { app.TimeNow = orig })
	if err := ratelimitview.SaveRatelimitScreen(ratelimitview.RatelimitScreen{SavedAt: now}); err != nil {
		t.Fatal(err)
	}

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	installInertDoctor(t, &m.doctorOv)
	_ = m.Init()
	if !m.rlDash.Visible() {
		t.Fatal("記憶があるのに ratelimit ダッシュボードが開いていない")
	}
	if id := m.activeFullScreen(); id != app.FullScreenRatelimit {
		t.Errorf("全画面が ratelimit になっていない: %v", id)
	}
}

// 記憶が無ければ素の画面から始まる (勝手にダッシュボードを出さない)。
func TestInitWithoutRatelimitMemoryStaysOnLog(t *testing.T) {
	withRatelimitStateDir(t)
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	installInertDoctor(t, &m.doctorOv)
	_ = m.Init()
	if m.rlDash.Visible() {
		t.Error("記憶が無いのにダッシュボードが開いた")
	}
}

// 両方の記憶が (異常終了などで) 残っていても、全画面は 1 枚だけ開く。
// 2 枚 shown になると「見えている画面」と「キーを受ける画面」が食い違う (fullscreen.go)。
func TestInitRestoresOnlyOneFullScreen(t *testing.T) {
	withRatelimitStateDir(t)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.Local)
	orig := app.TimeNow
	app.TimeNow = func() time.Time { return now }
	t.Cleanup(func() { app.TimeNow = orig })
	if err := ratelimitview.SaveRatelimitScreen(ratelimitview.RatelimitScreen{SavedAt: now}); err != nil {
		t.Fatal(err)
	}
	rememberDoctorShown(now, doctorview.TabSvc)

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	installInertDoctor(t, &m.doctorOv)
	_ = m.Init()
	if !m.rlDash.Visible() {
		t.Fatal("ratelimit の記憶が復元されていない")
	}
	if m.doctorOv.Visible() {
		t.Error("全画面が 2 枚同時に開いた (doctor が裏で shown)")
	}
}
