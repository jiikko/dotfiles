package main

import (
	"context"
	"strings"
	"testing"
)

// バージョン出力のパースは usage パッケージ側 (TestParseCodexVersion) でカバーする。

// swapCodexVersionFetchers は差し替え点 2 つを一時的に固定値へ差し替える
// (claude 側 swapClaudeVersionFetchers の鏡像)。
func swapCodexVersionFetchers(t *testing.T, latest, installed string) {
	t.Helper()
	origLatest, origInstalled := fetchLatestCodexVersion, fetchInstalledCodexVersion
	t.Cleanup(func() {
		fetchLatestCodexVersion, fetchInstalledCodexVersion = origLatest, origInstalled
	})
	fetchLatestCodexVersion = func(context.Context) string { return latest }
	fetchInstalledCodexVersion = func(context.Context) string { return installed }
}

// X → codex update (C の codex 版。確認なし即実行・モーダル題字と結果トーストが codex 表記)。
func TestBrowseCodexUpdateFlow(t *testing.T) {
	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	var calls int
	orig := runCodexUpdate
	runCodexUpdate = func() (string, string, error) { calls++; return "0.144.6", "0.150.0", nil }
	t.Cleanup(func() { runCodexUpdate = orig })

	// X 直後は「既に latest か」の判定中でモーダルはまだ出ない (2 段構え。tui_actions_test.go
	// の TestBrowseUpdateFlow と同じ)。updateBeginMsg を配送して実更新の開始を確認する
	_, cmd := m.handleKey("X")
	if cmd == nil || m.actModal.anyUpdating() {
		t.Fatalf("X 直後にモーダルが立っている: cmd=%v updating=%v", cmd != nil, m.actModal.updatingTargets())
	}
	mm, _ := m.Update(updateBeginMsg{target: "codex"})
	m = mm.(*browseModel)
	if !m.actModal.isUpdating("codex") {
		t.Fatalf("updateBeginMsg で codex update が始まらない: updating=%v", m.actModal.updatingTargets())
	}
	m.width, m.height = 80, 20
	if v := stripANSI(m.View().Content); !strings.Contains(v, "codex update") {
		t.Fatal("codex update 実行中モーダルが描画されない")
	}
	// updateMsg (codex) の結果トーストは CLI 名を前置して claude と区別できる
	m2, _ := m.Update(updateMsg{target: "codex", before: "0.144.6", after: "0.150.0"})
	bm := m2.(*browseModel)
	if bm.actModal.anyUpdating() {
		t.Fatal("updateMsg 後も updating が立ったまま")
	}
	if !strings.Contains(bm.toast.text, "codex ") || !strings.Contains(bm.toast.text, "0.150.0") {
		t.Fatalf("codex 表記の結果トーストが出ない: %q", bm.toast.text)
	}
}

func TestCheckCodexVersionCmd(t *testing.T) {
	t.Run("新しいバージョンがあれば msg", func(t *testing.T) {
		t.Setenv("XDG_CACHE_HOME", t.TempDir())
		swapCodexVersionFetchers(t, "0.150.0", "0.144.6")
		msg := checkCodexVersionCmd()()
		got, ok := msg.(codexUpdateAvailableMsg)
		if !ok || got.latest != "0.150.0" {
			t.Fatalf("msg = %#v, want codexUpdateAvailableMsg{0.150.0}", msg)
		}
	})
	t.Run("同じバージョンなら nil", func(t *testing.T) {
		t.Setenv("XDG_CACHE_HOME", t.TempDir())
		swapCodexVersionFetchers(t, "0.144.6", "0.144.6")
		if msg := checkCodexVersionCmd()(); msg != nil {
			t.Fatalf("msg = %#v, want nil", msg)
		}
	})
	t.Run("未インストール (installed 空) なら nil", func(t *testing.T) {
		t.Setenv("XDG_CACHE_HOME", t.TempDir())
		swapCodexVersionFetchers(t, "0.150.0", "")
		if msg := checkCodexVersionCmd()(); msg != nil {
			t.Fatalf("msg = %#v, want nil", msg)
		}
	})
	t.Run("claude とキャッシュが混ざらない", func(t *testing.T) {
		t.Setenv("XDG_CACHE_HOME", t.TempDir())
		// 先に claude 側が latest をキャッシュしても codex 側の照会に使われないこと
		// (ファイル名が分かれている = 別 CLI の latest を自分の latest と誤認しない)
		swapClaudeVersionFetchers(t, "9.9.9", "9.9.9")
		_ = checkClaudeVersionCmd()()
		swapCodexVersionFetchers(t, "0.150.0", "0.144.6")
		msg := checkCodexVersionCmd()()
		got, ok := msg.(codexUpdateAvailableMsg)
		if !ok || got.latest != "0.150.0" {
			t.Fatalf("msg = %#v, want codexUpdateAvailableMsg{0.150.0} (claude の latest が混入?)", msg)
		}
	})
}
