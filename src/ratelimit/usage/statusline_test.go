package usage

import (
	"context"
	"encoding/json"
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

// runStatusline は書き手 (_claude/statusline-command.sh の write_rate_limits) を実際に走らせる。
func runStatusline(t *testing.T, cache, input string) {
	t.Helper()
	script, err := filepath.Abs("../../../_claude/statusline-command.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script)
	cmd.Stdin = strings.NewReader(input)
	cmd.Env = append(os.Environ(), "XDG_CACHE_HOME="+cache)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("statusline: %v\n%s", err, out)
	}
}

func readStatuslineRaw(t *testing.T, cache string) statuslineState {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(cache, "glog", statuslineFile))
	if err != nil {
		t.Fatal(err)
	}
	var st statuslineState
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	return st
}

// 書き手の出力を読み手が読めることを固定する (ファイル名・項目の名前・値の無い項目の書き方 (null)・版)。
// 5h のキーが無い入力は、Claude Code がリセットを過ぎた窓を落とした形 (2.1.287 の h5e)。
func TestStatuslineWriterMatchesReader(t *testing.T) {
	cache := t.TempDir()
	now := time.Now()
	runStatusline(t, cache, fmt.Sprintf(`{"cwd":"/tmp","version":"2.1.287","rate_limits":{"five_hour":{"used_percentage":42.7,"resets_at":%d},"seven_day":{"used_percentage":8,"resets_at":%d}}}`,
		now.Add(time.Hour).Unix(), now.Add(48*time.Hour).Unix()))
	snap := readStatusline(filepath.Join(cache, "glog"), time.Now())
	if snap == nil {
		t.Fatal("statusline が書いたファイルを読めない")
	}
	w5, ok := snap.Find("5h")
	if !ok || w5.Percent != 42 || w5.Unused || w5.ResetAt.Unix() != now.Add(time.Hour).Unix() || w5.WindowMins != 300 {
		t.Fatalf("5h = %+v (ok=%v)", w5, ok)
	}
	if w7, ok := snap.Find("7d"); !ok || w7.Percent != 8 || w7.WindowMins != 7*24*60 {
		t.Fatalf("7d = %+v (ok=%v)", w7, ok)
	}
	if snap.Version != "2.1.287" {
		t.Fatalf("版 = %q", snap.Version)
	}
	if left, _ := filepath.Glob(filepath.Join(cache, "glog", "*.tmp.*")); len(left) != 0 {
		t.Fatalf("書きかけのファイルが残った: %v", left)
	}

	only7 := t.TempDir()
	runStatusline(t, only7, fmt.Sprintf(`{"cwd":"/tmp","rate_limits":{"seven_day":{"used_percentage":8,"resets_at":%d}}}`, now.Add(48*time.Hour).Unix()))
	snap = readStatusline(filepath.Join(only7, "glog"), time.Now())
	if snap == nil {
		t.Fatal("5h のキーが無い観測を読めない")
	}
	if w5, ok := snap.Find("5h"); !ok || !w5.Unused || w5.Percent != 0 {
		t.Fatalf("キーの無い 5h = %+v (ok=%v), want 0%% Unused (-check が判定できるように)", w5, ok)
	}
}

// 送信の無い描画 (refreshInterval) は、最後に受け取った古い値を毎回持ってくる。古い観測では書き換えず、observedAt も
// 進めない (放置したセッションが新しい値を上書きしない / 新しい値が来ない間に observedAt が古くなる)。版は引き継ぐ。
func TestStatuslineWriterKeepsNewerObservation(t *testing.T) {
	cache := t.TempDir()
	now := time.Now()
	r5, r7 := now.Add(time.Hour).Unix(), now.Add(48*time.Hour).Unix()
	path := filepath.Join(cache, "glog", statuslineFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-10 * time.Minute).Unix()
	if err := os.WriteFile(path, fmt.Appendf(nil, `{"observedAt":%d,"five_hour":{"used_percentage":85,"resets_at":%d},"seven_day":{"used_percentage":60,"resets_at":%d},"version":"2.1.287"}`+"\n", old, r5, r7), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, stale := range []string{
		fmt.Sprintf(`{"rate_limits":{"five_hour":{"used_percentage":12,"resets_at":%d},"seven_day":{"used_percentage":30,"resets_at":%d}}}`, r5, r7), // 放置したセッション
		fmt.Sprintf(`{"rate_limits":{"five_hour":{"used_percentage":85,"resets_at":%d},"seven_day":{"used_percentage":60,"resets_at":%d}}}`, r5, r7), // 同じ値の描画し直し
		fmt.Sprintf(`{"rate_limits":{"five_hour":{"used_percentage":99,"resets_at":%d}}}`, r5-3600),                                                  // 前の窓の値
	} {
		runStatusline(t, cache, stale)
		if st := readStatuslineRaw(t, cache); st.ObservedAt != old || *st.FiveHour.UsedPercentage != 85 || *st.SevenDay.UsedPercentage != 60 {
			t.Fatalf("古い観測で書き換えた (%s): %+v", stale, st)
		}
	}
	runStatusline(t, cache, fmt.Sprintf(`{"rate_limits":{"seven_day":{"used_percentage":61,"resets_at":%d}}}`, r7))
	st := readStatuslineRaw(t, cache)
	if st.ObservedAt == old || *st.FiveHour.UsedPercentage != 85 || *st.SevenDay.UsedPercentage != 61 || st.Version != "2.1.287" {
		t.Fatalf("新しい観測で書き換えない / 他の窓・版を引き継がない: %+v", st)
	}
	runStatusline(t, cache, fmt.Sprintf(`{"rate_limits":{"five_hour":{"used_percentage":3,"resets_at":%d}}}`, r5+5*3600))
	if st := readStatuslineRaw(t, cache); *st.FiveHour.UsedPercentage != 3 || *st.FiveHour.ResetsAt != r5+5*3600 {
		t.Fatalf("次の窓の観測を採らない: %+v", st.FiveHour)
	}
}

// 書けない置き場でも描画は壊れず、stderr にも何も出さない。
func TestStatuslineWriterQuietWhenUnwritable(t *testing.T) {
	cache := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cache, "glog"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(cache, "glog"), 0o700) })
	script, _ := filepath.Abs("../../../_claude/statusline-command.sh")
	cmd := exec.Command("bash", script)
	cmd.Stdin = strings.NewReader(fmt.Sprintf(`{"cwd":"/tmp","rate_limits":{"five_hour":{"used_percentage":5,"resets_at":%d}}}`, time.Now().Add(time.Hour).Unix()))
	cmd.Env = append(os.Environ(), "XDG_CACHE_HOME="+cache)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil || stderr.Len() != 0 {
		t.Fatalf("rc=%v stderr=%q", err, stderr.String())
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
		{"使用率があるのにリセット時刻が無い", fmt.Sprintf(`{"observedAt":%d,"five_hour":{"used_percentage":97,"resets_at":null},"seven_day":null}`, now.Unix())},
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
		now.Add(-10*time.Minute).Unix(), now.Add(-time.Minute).Unix(), now.Add(48*time.Hour).Unix()))
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
	if got := calls(); got != 0 {
		t.Fatalf("statusline の観測があるのに claude -p を %d 回起こした", got)
	}
	// R の r (force) は statusline を飛ばしてサーバの値を取る (statusline は PG や他のデバイスの消費を含まない)
	if p := FetchClaudePartNow(context.Background()); p.Err != nil || calls() != 1 {
		t.Fatalf("force で claude -p を起こさない: %+v calls=%d", p, calls())
	}
	advance(statuslineMaxAge)
	snap, err = fetchT(t)
	if err != nil {
		t.Fatal(err)
	}
	if w, _ := snap.Find("5h"); w.Percent != 2 || calls() != 2 {
		t.Fatalf("古い観測で予備へ落ちない: 5h=%+v calls=%d", w, calls())
	}
}
