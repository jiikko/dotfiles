package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"glogx/usage"
)

// usageSnapFixture は本物の /usage 出力を usage.Parse に通して作る。手組みの Snapshot だと
// 「実際に Fetch が返す形」と食い違っても気づけない: usage.Snapshot / Window は json タグを
// 持たずフィールド名で符号化され、ResetAt は time.Time。ここが往復で壊れると症状は
// 「永久に cache miss = 毎起動 claude を起こし続ける」で、どこも失敗しないまま劣化する。
func usageSnapFixture(t *testing.T) *usage.Snapshot {
	t.Helper()
	const realResult = `You are currently using your subscription to power your Claude Code usage

Current session: 2% used · resets Jul 22 at 3:09am (Asia/Tokyo)
Current week (all models): 29% used · resets Jul 24 at 8am (Asia/Tokyo)
Current week (Fable): 48% used · resets Jul 24 at 8am (Asia/Tokyo)

What's contributing to your limits usage?
Last 24h · 875 requests · 7 sessions`
	snap, err := usage.Parse(realResult, time.Date(2026, 7, 21, 12, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatalf("fixture の Parse に失敗: %v", err)
	}
	snap.Version = "2.1.216"
	return snap
}

// 保存 → TTL 内は読める / TTL 到達で切れる (境界は「以上で切れる」)。
func TestUsageCacheRoundTripAndTTL(t *testing.T) {
	path := filepath.Join(t.TempDir(), usageCacheFile)
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.Local)
	if err := saveUsageCache(path, usageSnapFixture(t), now); err != nil {
		t.Fatalf("saveUsageCache: %v", err)
	}

	got, ok := loadUsageCache(path, now.Add(usageCacheTTL-time.Second))
	if !ok {
		t.Fatal("TTL 内なのにキャッシュが読めない")
	}
	// 全フィールドの往復を突き合わせる (どれか 1 つ欠けても表示が静かに劣化する)
	want := usageSnapFixture(t)
	if got.Version != want.Version {
		t.Errorf("Version = %q, want %q", got.Version, want.Version)
	}
	if len(got.Windows) != len(want.Windows) {
		t.Fatalf("枠数 = %d, want %d", len(got.Windows), len(want.Windows))
	}
	for i, w := range want.Windows {
		g := got.Windows[i]
		if g.Label != w.Label || g.Raw != w.Raw || g.Percent != w.Percent || !g.ResetAt.Equal(w.ResetAt) {
			t.Errorf("枠 %d が往復で壊れた:\n got  %+v\n want %+v", i, g, w)
		}
	}

	if _, ok := loadUsageCache(path, now.Add(usageCacheTTL)); ok {
		t.Error("TTL ちょうどで切れていない")
	}
}

// 欠損・破損・枠 0 件はすべて「キャッシュなし」に落ちる (表示を壊さない)。
func TestUsageCacheFallsBackToMiss(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	if _, ok := loadUsageCache(filepath.Join(dir, "absent.json"), now); ok {
		t.Error("欠損ファイルで hit した")
	}

	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadUsageCache(broken, now); ok {
		t.Error("破損ファイルで hit した")
	}

	// 枠 0 件 = Fetch なら error になる状態。キャッシュ経由でも表示に載せない
	empty := filepath.Join(dir, "empty.json")
	if err := saveUsageCache(empty, &usage.Snapshot{Version: "2.1.216"}, now); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadUsageCache(empty, now); ok {
		t.Error("枠 0 件で hit した")
	}
}

// キャッシュ契約は Claude 枠が必須、codex 枠は best-effort。
func TestUsageCacheRequiresClaude(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	codexOnly := filepath.Join(dir, "codex-only.json")
	if err := saveUsageCache(codexOnly, &usage.Snapshot{Windows: []usage.Window{
		{Label: "cx7d", Source: usage.SourceCodex},
	}}, now); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadUsageCache(codexOnly, now); ok {
		t.Error("fresh な codex-only キャッシュで hit した")
	}

	claudeOnly := filepath.Join(dir, "claude-only.json")
	if err := saveUsageCache(claudeOnly, &usage.Snapshot{Windows: []usage.Window{
		{Label: "5h"},
	}}, now); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadUsageCache(claudeOnly, now); !ok {
		t.Error("fresh な Claude 枠ありキャッシュが miss になった")
	}
}

// 起動時 fetchCmd はキャッシュが fresh なら claude を起こさず即答する。
// (subprocess を起こさないことは「claude が PATH に無い環境でも snap が返る」で担保する)
func TestFetchCmdUsesCacheOnStartup(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path, err := usageCachePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := saveUsageCache(path, usageSnapFixture(t), time.Now()); err != nil {
		t.Fatal(err)
	}
	// PATH を空にして claude を見つけられなくする: subprocess 経路へ落ちたら err になる
	t.Setenv("PATH", "")

	var o usageOverlay
	msg, ok := o.fetchCmd(true)().(usageMsg)
	if !ok {
		t.Fatalf("usageMsg が返らない: %T", msg)
	}
	if msg.err != nil {
		t.Fatalf("キャッシュ hit のはずが subprocess 経路へ落ちた: %v", msg.err)
	}
	if msg.snap == nil || msg.snap.Version != "2.1.216" {
		t.Errorf("キャッシュのスナップショットが返っていない: %+v", msg.snap)
	}
}

// 定期リフレッシュ (useCache=false) はキャッシュを読まない — 鮮度を作るのが役目なので、
// fresh なキャッシュがあっても subprocess 経路へ進む。
func TestFetchCmdRefreshIgnoresCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path, err := usageCachePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := saveUsageCache(path, usageSnapFixture(t), time.Now()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "") // subprocess 経路に入ったことを err で観測する

	var o usageOverlay
	msg, ok := o.fetchCmd(false)().(usageMsg)
	if !ok {
		t.Fatalf("usageMsg が返らない: %T", msg)
	}
	if msg.err == nil {
		t.Error("キャッシュを読んでしまった (リフレッシュは常に取得しに行くべき)")
	}
}
