package usage

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeStatuslineFile は statusline が書き出す形のファイルを dir に置く。
func writeStatuslineFile(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, statuslineFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// 書き手 (_claude/statusline-command.sh の write_rate_limits) を実際に走らせ、読み手がその出力を読めることを固定する。
// ファイル名・項目の名前・値の無い項目の書き方 (null) のどれがずれても、ここが落ちる。
func TestStatuslineWriterMatchesReader(t *testing.T) {
	script, err := filepath.Abs("../../../_claude/statusline-command.sh")
	if err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	now := time.Now()
	in := fmt.Sprintf(`{"cwd":"/tmp","rate_limits":{"five_hour":{"used_percentage":42.7,"resets_at":%d},"seven_day":{"used_percentage":8}}}`, now.Add(time.Hour).Unix())
	cmd := exec.Command("bash", script)
	cmd.Stdin = strings.NewReader(in)
	cmd.Env = append(os.Environ(), "XDG_CACHE_HOME="+cache)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("statusline: %v\n%s", err, out)
	}
	snap := readStatusline(filepath.Join(cache, "glog"), time.Now())
	if snap == nil {
		t.Fatal("statusline が書いたファイルを読めない")
	}
	w5, ok := snap.Find("5h")
	if !ok || w5.Percent != 42 || w5.Unused || w5.ResetAt.Unix() != now.Add(time.Hour).Unix() || w5.WindowMins != 300 {
		t.Fatalf("5h = %+v (ok=%v)", w5, ok)
	}
	if w7, ok := snap.Find("7d"); !ok || !w7.Unused || w7.Percent != 0 {
		t.Fatalf("リセット時刻の無い 7d = %+v (ok=%v), want Unused", w7, ok)
	}
	if left, _ := filepath.Glob(filepath.Join(cache, "glog", "*.tmp.*")); len(left) != 0 {
		t.Fatalf("書きかけのファイルが残った: %v", left)
	}
}

// 値の無い描画 (rate_limits が無い) では書かない (前の観測を空で潰さない)。
func TestStatuslineWriterSkipsWithoutRateLimits(t *testing.T) {
	script, _ := filepath.Abs("../../../_claude/statusline-command.sh")
	cache := t.TempDir()
	cmd := exec.Command("bash", script)
	cmd.Stdin = strings.NewReader(`{"cwd":"/tmp"}`)
	cmd.Env = append(os.Environ(), "XDG_CACHE_HOME="+cache)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("statusline: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(cache, "glog", statuslineFile)); !os.IsNotExist(err) {
		t.Fatalf("rate_limits の無い描画でファイルを書いた: %v", err)
	}
}

func TestReadStatuslineRejectsUnusable(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.Local)
	reset := now.Add(time.Hour).Unix()
	body := func(observed time.Time) string {
		return fmt.Sprintf(`{"observedAt":%d,"five_hour":{"used_percentage":10,"resets_at":%d},"seven_day":null}`, observed.Unix(), reset)
	}
	for _, tc := range []struct{ name, body string }{
		{"未来の観測", body(now.Add(time.Minute))},
		{"古い観測", body(now.Add(-statuslineMaxAge))},
		{"壊れた JSON", "{"},
		{"枠が無い", fmt.Sprintf(`{"observedAt":%d,"five_hour":null,"seven_day":{"used_percentage":null,"resets_at":null}}`, now.Unix())},
	} {
		dir := t.TempDir()
		writeStatuslineFile(t, dir, tc.body)
		if snap := readStatusline(dir, now); snap != nil {
			t.Errorf("%s: 使えない観測を読んだ: %+v", tc.name, snap.Windows)
		}
	}
	dir := t.TempDir()
	writeStatuslineFile(t, dir, body(now.Add(-statuslineMaxAge+time.Minute)))
	if readStatusline(dir, now) == nil {
		t.Error("期限内の観測を捨てた")
	}
}

// 観測の後にリセットを過ぎた窓は 0% (Unused) にする (古い使用率を出し続けない)。
func TestReadStatuslineResetPassed(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.Local)
	dir := t.TempDir()
	writeStatuslineFile(t, dir, fmt.Sprintf(`{"observedAt":%d,"five_hour":{"used_percentage":90,"resets_at":%d},"seven_day":{"used_percentage":30,"resets_at":%d}}`,
		now.Add(-30*time.Minute).Unix(), now.Add(-time.Minute).Unix(), now.Add(48*time.Hour).Unix()))
	snap := readStatusline(dir, now)
	if snap == nil {
		t.Fatal("読めない")
	}
	if w, _ := snap.Find("5h"); w.Percent != 0 || !w.Unused {
		t.Errorf("リセット済みの 5h = %+v, want 0%% Unused", w)
	}
	if w, _ := snap.Find("7d"); w.Percent != 30 || w.Unused {
		t.Errorf("7d = %+v, want 30%%", w)
	}
}

// statusline の新しい観測があれば `claude -p /usage` を起こさない (force = R の r でも)。古ければ予備へ落ちる。
func TestFetchPrefersStatusline(t *testing.T) {
	dir := isolateShared(t)
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.Local)
	advance := setClock(t, now)
	calls := countingClaude(t, streamOK)
	writeStatuslineFile(t, dir, fmt.Sprintf(`{"observedAt":%d,"five_hour":{"used_percentage":55,"resets_at":%d},"seven_day":null}`,
		now.Add(-time.Minute).Unix(), now.Add(time.Hour).Unix()))
	snap, err := fetchT(t)
	if err != nil {
		t.Fatal(err)
	}
	if w, ok := snap.Find("5h"); !ok || w.Percent != 55 {
		t.Fatalf("statusline の値を使わない: %+v", snap.Windows)
	}
	if snap.Version != "9.9.9" {
		t.Errorf("版を補わない: %q", snap.Version)
	}
	if p := FetchClaudePartNow(context.Background()); p.Err != nil || len(p.Windows) != 1 || p.Windows[0].Percent != 55 {
		t.Fatalf("force でも statusline を使うはず: %+v", p)
	}
	if got := calls(); got != 0 {
		t.Fatalf("statusline の観測があるのに claude -p を %d 回起こした", got)
	}
	advance(statuslineMaxAge)
	snap, err = fetchT(t)
	if err != nil {
		t.Fatal(err)
	}
	if w, _ := snap.Find("5h"); w.Percent != 2 || calls() != 1 {
		t.Fatalf("古い観測で予備へ落ちない: 5h=%+v calls=%d", w, calls())
	}
}
