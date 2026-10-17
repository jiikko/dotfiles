package usageview

import (
	"glogx/internal/testenv"
	"path/filepath"
	"ratelimit/usage"
	"strings"
	"testing"
	"time"
)

// usage の枠タイトルに載る CLI バージョンは外部バイナリの出力。
func TestUsageBoxSanitizesVersion(t *testing.T) {
	o := UsageOverlay{
		Visible: true,
		Snap: &usage.Snapshot{
			Version: "1.2.3\a" + testenv.OSC8 + "0;PWNED" + testenv.ST8,
			Windows: []usage.Window{{Label: "5h", Percent: 20}},
		},
	}
	assertSanitizedAndVisible(t, "usage の枠", o.BoxLines(80, false, "⠋"), "1.2.3")
}

// usage の **codex 枠の Label** はキャッシュ由来 (`~/.cache/glog/claude-usage.json` は一般ユーザー
// 権限で書き換えられる)。live の取得経路は安全だが、codex 枠は allowlist ではなく Source で拾うので、
// キャッシュに書かれた Label がそのまま 3 経路 (RenderLine / RenderTableGroups / RenderDashboard) へ出る。
// 🚨 fixture は **codex 枠**で作ること: Claude 枠だと defaultOrder の allowlist に阻まれて
// 退行しても最初から不可視になり、何も守らないテストになる (issue 230)。
func TestUsageCacheSanitizesCodexLabel(t *testing.T) {
	stubLookPath(t, nil)
	path := filepath.Join(t.TempDir(), usageCacheFile)
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.Local)
	snap := usageSnapFixture(t)
	snap.Windows = append(snap.Windows, usage.Window{
		Label:   "cx7d\a" + testenv.OSC8 + "0;PWNED" + testenv.ST8,
		Percent: 20, Source: usage.SourceCodex, ResetAt: now.Add(time.Hour),
	})
	if err := saveUsageCache(path, snap, now); err != nil {
		t.Fatal(err)
	}
	got, ok := loadUsageCache(path, now)
	if !ok {
		t.Fatal("キャッシュを読めない (前提が違う)")
	}
	// 🚨 **観測面は production 到達可能な 2 経路にする** (issue 317)。
	// 以前は `RenderLine` / `RenderTable` を見ていたが、実測 (2026-09-06 / 2026-09-08 に再確認)
	// で **どちらも production 呼び出し 0 件**だった。入口 (loadUsageCache) のサニタイズを
	// 外す変異はそれでも red になる (どの出口から見ても汚染が見えるため。2026-09-08 実測) が、
	// **live 経路そのものが Label を別の形で出すようになった退行**は到達不能な面からは見えない。
	// 到達可能な面で観測しておけば、その両方が同じテストで閉じる。
	//
	//	RenderTableGroups … usage_overlay.go / usage/render.go 内部 (2 件)
	//	RenderDashboard   … ratelimit_dashboard.go / tools/dial-preview (2 件)
	assertNoTerminalControl := func(where string, lines ...string) {
		t.Helper()
		for _, l := range lines {
			if testenv.HasTerminalControl(l) {
				t.Errorf("%s に制御シーケンスが残った: %q", where, l)
			}
			if strings.Contains(l, "PWNED") {
				t.Errorf("%s に OSC の中身が残った: %q", where, l)
			}
		}
	}
	header, groups := usage.RenderTableGroups(got, now, false)
	rows := []string{header}
	for _, g := range groups {
		rows = append(rows, g...)
	}
	assertNoTerminalControl("RenderTableGroups", rows...)
	assertNoTerminalControl("RenderDashboard", usage.RenderDashboard(got, now, 120, 40, false)...)

	// 🚨 **観測面が汚染ラベルを実際に描いていることを固定する** (positive control)。
	// これが無いと「Label をどこにも出さない描画」に変えた瞬間、無害化が外れても緑になる
	// (issue 284 と同型)。サニタイズ後に残るのは制御文字を落とした "cx7d" の部分。
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "cx7d") {
		t.Fatalf("RenderTableGroups が codex のラベルを描いていない。観測面が的を外している:\n%s", joined)
	}
}
