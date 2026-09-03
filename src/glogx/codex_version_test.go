package main

import (
	"context"
	"strings"
	"testing"
	"time"
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
	runCodexUpdate = func() (string, string, string, error) { calls++; return "0.144.6", "0.150.0", "", nil }
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

// 起動時トーストが新版を告げた直後に X を押しても codex が更新しなかったケース (実測 2026-09-03)。
// codex は自前の latest キャッシュ (~/.codex/version.json) が stale だと "already up to date" を
// 出して exit 0 で終わるため、glogx から見ると before == after の成功に見える。🚨 これを
// 「すでに最新版です」と出すと、直前の「新版が公開されています」トーストと真っ向から矛盾した
// 案内になる (ユーザー報告の元の症状)。glogx は registry 直取りの latest を握っているので、
// それより古いままなら警告として出し、codex の言い分 (note) を添える。
func TestCodexUpdateThatDidNotUpdateIsNotReportedAsLatest(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	// 起動時チェックが「公開は 0.153.0」を保存済み。インストール済みは 0.152.0 なので
	// installedIsLatest は早期リターンせず、実際に codex update が走る。
	writeVersionCache(t, codexVersionCacheFile, "0.153.0", time.Now())
	origFetch := fetchInstalledCodexVersion
	fetchInstalledCodexVersion = func(context.Context) string { return "0.152.0" }
	t.Cleanup(func() { fetchInstalledCodexVersion = origFetch })

	origRun := runCodexUpdate
	var runCalls int
	runCodexUpdate = func() (string, string, string, error) {
		runCalls++
		return "0.152.0", "0.152.0", "Codex is already up to date.", nil
	}
	t.Cleanup(func() { runCodexUpdate = origRun })

	m := newTestBrowse(t, 1, map[string]CIState{}, nil)
	_, cmd := m.handleKey("X")
	deliverUpdateMsg(m, cmd)

	if runCalls != 1 {
		t.Fatalf("codex update 実行回数 = %d, want 1 (早期リターンしてはいけない)", runCalls)
	}
	if strings.Contains(m.toast.text, "最新版") {
		t.Fatalf("更新されなかったのに「最新版」と案内している: text=%q", m.toast.text)
	}
	// 警告として出す (ok=false)。w でコピーできるよう lastWarning にも積まれる。
	if m.toast.ok {
		t.Fatalf("公開版より古いままなのに成功トーストで出している: text=%q", m.toast.text)
	}
	if !strings.Contains(m.toast.text, "v0.152.0") || !strings.Contains(m.toast.text, "v0.153.0") {
		t.Fatalf("現行版と公開版の両方がトーストに出ない: text=%q", m.toast.text)
	}
	if !strings.Contains(m.toast.text, "Codex is already up to date.") {
		t.Fatalf("codex の言い分 (note) がトーストに出ない: text=%q", m.toast.text)
	}
	if m.actModal.anyUpdating() {
		t.Fatal("updateMsg 後も updating のまま")
	}
}
