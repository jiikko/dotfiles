package main

import (
	"testing"

	"glogx/internal/app"
	"glogx/internal/filerview"
	"glogx/internal/gitdata"
	"glogx/internal/lanview"
	"glogx/internal/ratelimitview"
	"mac-lan-manager/lanmgr"
	"treefiler/filer"
)

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
	got, ok := lanview.LoadLanScreen(now)
	if !ok || got != (lanmgr.Place{Tab: 1, Host: "m2macmini.local"}) {
		t.Fatalf("L を出したまま終えたのに覚えていない: %+v ok=%v", got, ok)
	}

	m2 := newTestBrowse(t, 3, map[string]gitdata.CIState{}, nil)
	_ = m2.Init()
	if m2.activeFullScreen() != app.FullScreenLan {
		t.Fatalf("記憶があるのに L が開いていない: active=%v", m2.activeFullScreen())
	}
	if p := m2.lanV.Fixture().Model().Place(); p != got {
		t.Errorf("場所が戻らない: %+v, want %+v", p, got)
	}

	m3 := newTestBrowse(t, 3, map[string]gitdata.CIState{}, nil)
	m3.quit()
	if _, ok := lanview.LoadLanScreen(now); ok {
		t.Error("L を閉じて終えたのに記憶が残った")
	}
}

// 他の全画面の記憶 (F) が当たったら L は開かない (全画面は同時に 1 枚)。
func TestInitLanRestoreYieldsToOtherScreens(t *testing.T) {
	now := withFilerState(t)
	if err := lanview.FixtureSaveScreen(lanmgr.Place{}, now); err != nil {
		t.Fatal(err)
	}
	if err := ratelimitview.FixtureSaveScreen(now); err != nil {
		t.Fatal(err)
	}
	m := newTestBrowse(t, 3, map[string]gitdata.CIState{}, nil)
	_ = m.Init()
	if m.lanV.Visible() {
		t.Error("ratelimit の記憶があるのに L も開いた")
	}
}

// F と L の記憶が両方残っていたら (異常終了)、F を開いて L は開かない (全画面は同時に 1 枚。L の優先は F より下)。
func TestInitLanRestoreYieldsToFiler(t *testing.T) {
	now := withFilerState(t)
	m := newFilerBrowse(t)
	if err := filerview.FixtureSaveScreen(filer.Place{Start: app.CurrentDir()}, now); err != nil {
		t.Fatal(err)
	}
	if err := lanview.FixtureSaveScreen(lanmgr.Place{}, now); err != nil {
		t.Fatal(err)
	}
	_ = m.Init()
	if m.activeFullScreen() != app.FullScreenFiler || m.lanV.Visible() {
		t.Errorf("F の記憶もあるのに L を開いた: active=%v lan=%v", m.activeFullScreen(), m.lanV.Visible())
	}
}
