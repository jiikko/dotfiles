package main

import (
	"os"
	"testing"
	"time"

	"glogx/internal/app"
	"mac-lan-manager/lanmgr"
	"treefiler/filer"
)

// 期限切れ / 未来の時刻 / 時刻なし / 壊れた JSON は「復元しない」に倒す。
func TestLanScreenRejectsBadState(t *testing.T) {
	now := withFilerState(t) // 記憶の置き場の隔離と時計の固定 (F と同じ)
	if _, ok := loadLanScreen(now); ok {
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
		if _, ok := loadLanScreen(now); ok {
			t.Errorf("%s: 復元しようとした", c.name)
		}
	}
	path, _ := lanMemory.Path()
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadLanScreen(now); ok {
		t.Error("壊れた JSON から復元しようとした")
	}
}

// L を出したまま glogx を終えたら (C-g) タブとカーソルの Mac を覚え、次の起動で L を開き直してそこへ戻る。
// L を閉じてから終えたら消す (次の起動で蘇らない)。
func TestLanScreenRememberedAndRestored(t *testing.T) {
	now := withFilerState(t)
	m := openLan(t)
	m.handleKey("l") // 未管理のタブ
	m.handleKey("j") // テストの Mac mini
	if _, cmd := m.handleKey("ctrl+g"); !isQuitCmd(cmd) {
		t.Fatal("C-g で終わらない")
	}
	got, ok := loadLanScreen(now)
	if !ok || got != (lanmgr.Place{Tab: 1, Host: "m2macmini.local"}) {
		t.Fatalf("L を出したまま終えたのに覚えていない: %+v ok=%v", got, ok)
	}

	m2 := newTestBrowse(t, 3, map[string]CIState{}, nil)
	_ = m2.Init()
	if m2.activeFullScreen() != app.FullScreenLan {
		t.Fatalf("記憶があるのに L が開いていない: active=%v", m2.activeFullScreen())
	}
	if p := m2.lanV.m.Place(); p != got {
		t.Errorf("場所が戻らない: %+v, want %+v", p, got)
	}

	m3 := newTestBrowse(t, 3, map[string]CIState{}, nil)
	m3.quit()
	if _, ok := loadLanScreen(now); ok {
		t.Error("L を閉じて終えたのに記憶が残った")
	}
}

// 他の全画面の記憶 (F) が当たったら L は開かない (全画面は同時に 1 枚)。
func TestInitLanRestoreYieldsToOtherScreens(t *testing.T) {
	now := withFilerState(t)
	if err := saveLanScreen(lanScreen{SavedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := saveRatelimitScreen(ratelimitScreen{SavedAt: now}); err != nil {
		t.Fatal(err)
	}
	m := newTestBrowse(t, 3, map[string]CIState{}, nil)
	_ = m.Init()
	if m.lanV.visible() {
		t.Error("ratelimit の記憶があるのに L も開いた")
	}
}

// F と L の記憶が両方残っていたら (異常終了)、F を開いて L は開かない (全画面は同時に 1 枚。L の優先は F より下)。
func TestInitLanRestoreYieldsToFiler(t *testing.T) {
	now := withFilerState(t)
	m := newFilerBrowse(t)
	if err := saveFilerScreen(filerScreen{Place: filer.Place{Start: app.CurrentDir()}, SavedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := saveLanScreen(lanScreen{SavedAt: now}); err != nil {
		t.Fatal(err)
	}
	_ = m.Init()
	if m.activeFullScreen() != app.FullScreenFiler || m.lanV.visible() {
		t.Errorf("F の記憶もあるのに L を開いた: active=%v lan=%v", m.activeFullScreen(), m.lanV.visible())
	}
}
