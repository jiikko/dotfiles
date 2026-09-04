package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 記憶の置き場をテスト用に隔離する。
func withRatelimitStateDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
}

func TestRatelimitScreenRoundTrip(t *testing.T) {
	withRatelimitStateDir(t)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.Local)
	if loadRatelimitScreen(now) {
		t.Fatal("記憶が無いのに復元しようとした")
	}
	if err := saveRatelimitScreen(ratelimitScreen{SavedAt: now}); err != nil {
		t.Fatal(err)
	}
	if !loadRatelimitScreen(now.Add(time.Minute)) {
		t.Fatal("覚えたのに復元しない")
	}
}

// 期限切れ / 未来の時刻 / 壊れた JSON / 時刻なしは「復元しない」に倒す
// (外部ファイル由来なので、勝手に全画面を出さない方が安全側。doctor 側と同じ規律)。
func TestRatelimitScreenRejectsBadState(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.Local)
	write := func(t *testing.T, body string) {
		t.Helper()
		p, err := ratelimitStatePath()
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
		if err := saveRatelimitScreen(ratelimitScreen{SavedAt: now.Add(-ratelimitStateTTL)}); err != nil {
			t.Fatal(err)
		}
		if loadRatelimitScreen(now) {
			t.Error("期限切れを復元した")
		}
	})
	t.Run("未来の時刻", func(t *testing.T) {
		withRatelimitStateDir(t)
		if err := saveRatelimitScreen(ratelimitScreen{SavedAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		if loadRatelimitScreen(now) {
			t.Error("未来の時刻を復元した (時計のずれ)")
		}
	})
	t.Run("壊れた JSON", func(t *testing.T) {
		withRatelimitStateDir(t)
		write(t, "{ not json")
		if loadRatelimitScreen(now) {
			t.Error("壊れた記憶を復元した")
		}
	})
	t.Run("時刻なし", func(t *testing.T) {
		withRatelimitStateDir(t)
		write(t, "{}")
		if loadRatelimitScreen(now) {
			t.Error("保存時刻の無い記憶を復元した (TTL が効かない)")
		}
	})
}

// 終了時: 開いていたら覚える / 開いていなければ**消す**。
func TestRememberRatelimitScreen(t *testing.T) {
	withRatelimitStateDir(t)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.Local)
	orig := timeNow
	timeNow = func() time.Time { return now }
	t.Cleanup(func() { timeNow = orig })

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	m.rlDash.shown = true
	m.rememberRatelimitScreen()
	if !loadRatelimitScreen(now) {
		t.Fatal("開いていたのに覚えていない")
	}

	m.rlDash.shown = false
	m.rememberRatelimitScreen()
	if loadRatelimitScreen(now) {
		t.Error("閉じて終了したのに記憶が残った (次の起動で蘇る)")
	}
}

// 起動時: 記憶があればダッシュボードを開いた状態で始まる (ユーザー要望 2026-09-05)。
func TestInitRestoresRatelimitScreen(t *testing.T) {
	withRatelimitStateDir(t)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.Local)
	orig := timeNow
	timeNow = func() time.Time { return now }
	t.Cleanup(func() { timeNow = orig })
	if err := saveRatelimitScreen(ratelimitScreen{SavedAt: now}); err != nil {
		t.Fatal(err)
	}

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	installInertDoctor(t, &m.doctorOv)
	_ = m.Init()
	if !m.rlDash.visible() {
		t.Fatal("記憶があるのに ratelimit ダッシュボードが開いていない")
	}
	if id := m.activeFullScreen(); id != fullScreenRatelimit {
		t.Errorf("全画面が ratelimit になっていない: %v", id)
	}
}

// 記憶が無ければ素の画面から始まる (勝手にダッシュボードを出さない)。
func TestInitWithoutRatelimitMemoryStaysOnLog(t *testing.T) {
	withRatelimitStateDir(t)
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	installInertDoctor(t, &m.doctorOv)
	_ = m.Init()
	if m.rlDash.visible() {
		t.Error("記憶が無いのにダッシュボードが開いた")
	}
}

// 両方の記憶が (異常終了などで) 残っていても、全画面は 1 枚だけ開く。
// 2 枚 shown になると「見えている画面」と「キーを受ける画面」が食い違う (fullscreen.go)。
func TestInitRestoresOnlyOneFullScreen(t *testing.T) {
	withRatelimitStateDir(t)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.Local)
	orig := timeNow
	timeNow = func() time.Time { return now }
	t.Cleanup(func() { timeNow = orig })
	if err := saveRatelimitScreen(ratelimitScreen{SavedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := saveDoctorScreen(doctorScreen{Tab: int(tabSvc), SavedAt: now}); err != nil {
		t.Fatal(err)
	}

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	installInertDoctor(t, &m.doctorOv)
	_ = m.Init()
	if !m.rlDash.visible() {
		t.Fatal("ratelimit の記憶が復元されていない")
	}
	if m.doctorOv.visible() {
		t.Error("全画面が 2 枚同時に開いた (doctor が裏で shown)")
	}
}
