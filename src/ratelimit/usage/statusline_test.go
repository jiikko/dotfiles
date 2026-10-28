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
	runStatuslineEnv(t, cache, input)
}

// runStatuslineEnv は extra を足した環境で書き手を走らせる。テストを起こした環境の CLAUDE_CODE_ENTRYPOINT は外す
// (desktop の中で go test を回すと、CLI の書き手を見るテストが desktop のファイルへ書く)
func runStatuslineEnv(t *testing.T, cache, input string, extra ...string) {
	t.Helper()
	script, err := filepath.Abs("../../../_claude/statusline-command.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script)
	cmd.Stdin = strings.NewReader(input)
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "CLAUDE_CODE_ENTRYPOINT=") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(append(env, "XDG_CACHE_HOME="+cache), extra...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("statusline: %v\n%s", err, out)
	}
}

// desktop (CLI と別のアカウントで動く) の利用枠は別のファイルへ書き、CLI の読み手のファイルには触れない。
// 印は desktop の mod が渡す surface と、desktop が起こしたセッションの環境の 2 経路
func TestStatuslineDesktopWritesSeparateFile(t *testing.T) {
	now := time.Now()
	cliInput := fmt.Sprintf(`{"cwd":"/tmp","transcript_path":%q,"rate_limits":{"five_hour":{"used_percentage":56,"resets_at":%d}}}`,
		freshTranscript(t), now.Add(2*time.Hour).Unix())
	// desktop のアカウントの窓は後ろにある (CLI の窓のリセットより後)。同じファイルなら「次の窓」として必ず採られていた形
	desktopInput := fmt.Sprintf(`{"cwd":"/tmp","surface":"desktop","rate_limits":{"five_hour":{"used_percentage":82,"resets_at":%d}}}`,
		now.Add(3*time.Hour).Unix())
	for _, c := range []struct {
		name  string
		input string
		env   []string
	}{
		{"mod の surface", desktopInput, nil},
		{"desktop の環境", strings.Replace(desktopInput, `"surface":"desktop",`, "", 1), []string{"CLAUDE_CODE_ENTRYPOINT=claude-desktop"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			cache := t.TempDir()
			runStatusline(t, cache, cliInput)
			runStatuslineEnv(t, cache, c.input, c.env...)
			snap := readStatusline(filepath.Join(cache, "glog"), time.Now())
			if w5, ok := snap.Find("5h"); !ok || w5.Percent != 56 {
				t.Fatalf("CLI の読み手に desktop の値が入った: %+v (ok=%v)", w5, ok)
			}
			b, err := os.ReadFile(filepath.Join(cache, "glog", "claude-rate-limits-desktop.json"))
			if err != nil || !strings.Contains(string(b), `"used_percentage":82`) {
				t.Fatalf("desktop の値が desktop のファイルに無い: %v %s", err, b)
			}
		})
	}
}

// freshTranscript はファイルより新しい transcript (= 書いた後にやり取りがあったセッション) を作る。
// mtime を未来へずらす: bash 3.2 の -nt は秒単位なので、直前の run が書いたファイルと同じ秒だと新しいと数えない。
func freshTranscript(t *testing.T) string {
	t.Helper()
	tr := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(tr, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(5 * time.Second)
	if err := os.Chtimes(tr, future, future); err != nil {
		t.Fatal(err)
	}
	return tr
}

func readStatuslineRaw(t *testing.T, cache string) statuslineState {
	t.Helper()
	return readStatuslineAs[statuslineState](t, cache)
}

// readStatuslineAs は書き手が置いた statusline のファイルを T として読む (読めない・JSON でないならテストを止め、中身を出す)。
func readStatuslineAs[T any](t *testing.T, cache string) T {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(cache, "glog", statuslineFile))
	if err != nil {
		t.Fatal(err)
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	return v
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

// 送信の無い描画 (refreshInterval) は、最後に受け取った古い値を毎回持ってくる。放置したセッションは同じ窓の値を書き換えず
// (使用率が高くても)、observedAt も進めない (新しい値が来ない間に observedAt が古くなる)。版は引き継ぐ。
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
		fmt.Sprintf(`{"rate_limits":{"five_hour":{"used_percentage":86,"resets_at":%d},"seven_day":{"used_percentage":99,"resets_at":%d}}}`, r5, r7), // 放置したセッションの高い値
		fmt.Sprintf(`{"rate_limits":{"five_hour":{"used_percentage":99,"resets_at":%d}}}`, r5-3600),                                                  // 前の窓の値
	} {
		runStatusline(t, cache, stale)
		if st := readStatuslineRaw(t, cache); st.ObservedAt != old || *st.FiveHour.UsedPercentage != 85 || *st.SevenDay.UsedPercentage != 60 {
			t.Fatalf("古い観測で書き換えた (%s): %+v", stale, st)
		}
	}
	runStatusline(t, cache, fmt.Sprintf(`{"transcript_path":%q,"rate_limits":{"seven_day":{"used_percentage":61,"resets_at":%d}}}`, freshTranscript(t), r7))
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
		// 7d は正常な値にする (7d も空だと「使える枠が無い」で nil になり、この形を見分けられない)
		{"使用率があるのにリセット時刻が無い", fmt.Sprintf(`{"observedAt":%d,"five_hour":{"used_percentage":97,"resets_at":null},"seven_day":{"used_percentage":10,"resets_at":%d}}`, now.Unix(), reset)},
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

// writeRaw は既存の観測として、observedAt を指定してファイルを置く。
func writeRaw(t *testing.T, cache string, observed time.Time, p5 int, r5 int64, p7 int, r7 int64) string {
	t.Helper()
	path := filepath.Join(cache, "glog", statuslineFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"observedAt":%d,"five_hour":{"used_percentage":%d,"resets_at":%d},"seven_day":{"used_percentage":%d,"resets_at":%d},"version":"2.1.287"}`+"\n",
		observed.Unix(), p5, r5, p7, r7)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, observed, observed); err != nil {
		t.Fatal(err)
	}
	return path
}

// リセット時刻が数秒ずれても同じ窓とみなす (放置したセッションの古い観測が「後のリセット時刻」で勝たない)。
func TestStatuslineWriterToleratesResetJitter(t *testing.T) {
	cache := t.TempDir()
	now := time.Now()
	r5, r7 := now.Add(time.Hour).Unix(), now.Add(48*time.Hour).Unix()
	old := now.Add(-10 * time.Minute)
	writeRaw(t, cache, old, 30, r5, 60, r7)
	runStatusline(t, cache, fmt.Sprintf(`{"rate_limits":{"five_hour":{"used_percentage":20,"resets_at":%d}}}`, r5+1))
	if st := readStatuslineRaw(t, cache); *st.FiveHour.UsedPercentage != 30 || st.ObservedAt != old.Unix() {
		t.Fatalf("揺れたリセット時刻の古い観測で書き換えた: %+v", st.FiveHour)
	}
	runStatusline(t, cache, fmt.Sprintf(`{"transcript_path":%q,"rate_limits":{"five_hour":{"used_percentage":31,"resets_at":%d}}}`, freshTranscript(t), r5+1))
	if st := readStatuslineRaw(t, cache); *st.FiveHour.UsedPercentage != 31 {
		t.Fatalf("同じ窓の新しい観測を採らない: %+v", st.FiveHour)
	}
}

// 値が同じでも、そのセッションの transcript がファイルより新しければ observedAt だけ進める (使用率は整数に切り捨てるので、
// 軽い利用では値が動かない)。transcript が古い (放置したセッション) なら進めない。
func TestStatuslineWriterBumpsObservedAtOnActivity(t *testing.T) {
	now := time.Now()
	r5, r7 := now.Add(time.Hour).Unix(), now.Add(48*time.Hour).Unix()
	in := func(transcript string) string {
		return fmt.Sprintf(`{"transcript_path":%q,"rate_limits":{"five_hour":{"used_percentage":30,"resets_at":%d},"seven_day":{"used_percentage":60,"resets_at":%d}}}`, transcript, r5, r7)
	}
	old := now.Add(-10 * time.Minute)
	for _, tc := range []struct {
		name  string
		mtime time.Time
		bump  bool
	}{{"やり取りがあったセッション", now.Add(-time.Minute), true}, {"放置したセッション", now.Add(-time.Hour), false}} {
		cache := t.TempDir()
		writeRaw(t, cache, old, 30, r5, 60, r7)
		tr := filepath.Join(t.TempDir(), "s.jsonl")
		if err := os.WriteFile(tr, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(tr, tc.mtime, tc.mtime); err != nil {
			t.Fatal(err)
		}
		runStatusline(t, cache, in(tr))
		if got := readStatuslineRaw(t, cache).ObservedAt != old.Unix(); got != tc.bump {
			t.Errorf("%s: observedAt を進めた = %v, want %v", tc.name, got, tc.bump)
		}
	}
}

// 読めないファイル・桁あふれの値でも、描画は壊れず stderr にも何も出さない。
func TestStatuslineWriterQuietOnBadInput(t *testing.T) {
	script, _ := filepath.Abs("../../../_claude/statusline-command.sh")
	now := time.Now()
	run := func(cache, input string) string {
		cmd := exec.Command("bash", script)
		cmd.Stdin = strings.NewReader(input)
		cmd.Env = append(os.Environ(), "XDG_CACHE_HOME="+cache)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("rc=%v", err)
		}
		return stderr.String()
	}
	cache := t.TempDir()
	path := writeRaw(t, cache, now.Add(-time.Minute), 30, now.Add(time.Hour).Unix(), 60, now.Add(48*time.Hour).Unix())
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if e := run(cache, fmt.Sprintf(`{"rate_limits":{"five_hour":{"used_percentage":5,"resets_at":%d}}}`, now.Add(time.Hour).Unix())); e != "" {
		t.Errorf("読めないファイルで stderr: %q", e)
	}
	// 既存の観測がある状態で渡す (無いと比べるまでもなく書くので、桁あふれの比較を通らない)
	huge := t.TempDir()
	writeRaw(t, huge, now.Add(-time.Minute), 30, now.Add(time.Hour).Unix(), 60, now.Add(48*time.Hour).Unix())
	if e := run(huge, `{"rate_limits":{"five_hour":{"used_percentage":5,"resets_at":99999999999999999999}}}`); e != "" {
		t.Errorf("桁あふれの値で stderr: %q", e)
	}
}

// 桁あふれ (ミリ秒など) のリセット時刻は書かない。既にファイルに入っていても、正しい観測で上書きできる。
func TestStatuslineWriterRejectsOverlongReset(t *testing.T) {
	now := time.Now()
	r5 := now.Add(time.Hour).Unix()
	cache := t.TempDir()
	runStatusline(t, cache, fmt.Sprintf(`{"rate_limits":{"five_hour":{"used_percentage":10,"resets_at":%d},"seven_day":{"used_percentage":5,"resets_at":%d}}}`,
		r5*1000, now.Add(48*time.Hour).Unix()))
	if st := readStatuslineRaw(t, cache); st.FiveHour.UsedPercentage != nil {
		t.Fatalf("桁あふれのリセット時刻を書いた: %+v", st.FiveHour)
	}
	broken := t.TempDir()
	path := filepath.Join(broken, "glog", statuslineFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, fmt.Appendf(nil, `{"observedAt":%d,"five_hour":{"used_percentage":10,"resets_at":%d},"seven_day":{"used_percentage":null,"resets_at":null},"version":""}`+"\n",
		now.Add(-time.Minute).Unix(), r5*1000), 0o600); err != nil {
		t.Fatal(err)
	}
	runStatusline(t, broken, fmt.Sprintf(`{"rate_limits":{"five_hour":{"used_percentage":12,"resets_at":%d}}}`, r5))
	if st := readStatuslineRaw(t, broken); *st.FiveHour.UsedPercentage != 12 || *st.FiveHour.ResetsAt != r5 {
		t.Fatalf("ファイルに入った壊れた値を正しい観測で上書きしない: %+v", st.FiveHour)
	}
}

// 同じ窓でも、やり取りのあったセッションの値はファイルの値より低くても採る。高い方を残すと、一度入った高すぎる値
// (2026-10-19 に 7d=99%。今の値は 38%) がリセットまで下がらない。
func TestStatuslineWriterFreshLowersValues(t *testing.T) {
	now := time.Now()
	r5, r7 := now.Add(time.Hour).Unix(), now.Add(48*time.Hour).Unix()
	cache := t.TempDir()
	old := now.Add(-10 * time.Minute)
	writeRaw(t, cache, old, 30, r5, 99, r7)
	runStatusline(t, cache, fmt.Sprintf(`{"transcript_path":%q,"rate_limits":{"five_hour":{"used_percentage":12,"resets_at":%d},"seven_day":{"used_percentage":38,"resets_at":%d}}}`, freshTranscript(t), r5, r7))
	st := readStatuslineRaw(t, cache)
	if st.ObservedAt == old.Unix() || *st.FiveHour.UsedPercentage != 12 || *st.SevenDay.UsedPercentage != 38 {
		t.Fatalf("やり取りのあったセッションの値で書き換えない: %+v / 5h %d / 7d %d", st.ObservedAt, *st.FiveHour.UsedPercentage, *st.SevenDay.UsedPercentage)
	}
}

// ファイルのリセット時刻が後ろへずれて入っていても (前の窓に見える観測)、やり取りのあったセッションの値で直す。
// 放置したセッションの前の窓の値は採らない (TestStatuslineWriterKeepsNewerObservation)。
func TestStatuslineWriterFreshFixesShiftedReset(t *testing.T) {
	now := time.Now()
	r5, r7 := now.Add(time.Hour).Unix(), now.Add(48*time.Hour).Unix()
	cache := t.TempDir()
	writeRaw(t, cache, now.Add(-10*time.Minute), 30, r5, 99, r7+3*86400)
	runStatusline(t, cache, fmt.Sprintf(`{"transcript_path":%q,"rate_limits":{"five_hour":{"used_percentage":30,"resets_at":%d},"seven_day":{"used_percentage":38,"resets_at":%d}}}`, freshTranscript(t), r5, r7))
	if st := readStatuslineRaw(t, cache); *st.SevenDay.UsedPercentage != 38 || *st.SevenDay.ResetsAt != r7 {
		t.Fatalf("ずれたリセット時刻の値を直さない: 7d %d / %d", *st.SevenDay.UsedPercentage, *st.SevenDay.ResetsAt)
	}
}

// statuslineWriters は書き手が窓ごとに残す「値を書いたセッション」(読み手は使わない。出所を後から辿るための記録)。
type statuslineWriters struct {
	Writers struct {
		FiveHour *struct {
			By      string `json:"by"`
			Version string `json:"version"`
			At      int64  `json:"at"`
		} `json:"five_hour"`
		SevenDay *struct {
			By      string `json:"by"`
			Version string `json:"version"`
			At      int64  `json:"at"`
		} `json:"seven_day"`
	} `json:"writers"`
}

func readWriters(t *testing.T, cache string) statuslineWriters {
	t.Helper()
	return readStatuslineAs[statuslineWriters](t, cache)
}

// 値を書き換えた窓には書いたセッション (transcript のパス)・版・時刻を残し、書き換えなかった窓は前の記録を引き継ぐ。
// observedAt だけ進める書き込みでは記録を変えない。読み手は記録が付いたファイルも読める。
func TestStatuslineWriterRecordsWriters(t *testing.T) {
	now := time.Now()
	r5, r7 := now.Add(time.Hour).Unix(), now.Add(48*time.Hour).Unix()
	cache := t.TempDir()
	a := freshTranscript(t)
	runStatusline(t, cache, fmt.Sprintf(`{"transcript_path":%q,"version":"2.1.300","rate_limits":{"five_hour":{"used_percentage":10,"resets_at":%d},"seven_day":{"used_percentage":20,"resets_at":%d}}}`, a, r5, r7))
	w := readWriters(t, cache)
	if w.Writers.FiveHour == nil || w.Writers.FiveHour.By != a || w.Writers.FiveHour.Version != "2.1.300" || w.Writers.FiveHour.At == 0 ||
		w.Writers.SevenDay == nil || w.Writers.SevenDay.By != a {
		t.Fatalf("書いたセッションを残さない: %+v", w.Writers)
	}
	if readStatusline(filepath.Join(cache, "glog"), time.Now()) == nil {
		t.Fatal("記録の付いたファイルを読み手が読めない")
	}

	// 別のセッションが 7d だけを書き換える: 5h の記録は a のまま
	b := freshTranscript(t)
	runStatusline(t, cache, fmt.Sprintf(`{"transcript_path":%q,"version":"2.1.303","rate_limits":{"five_hour":{"used_percentage":10,"resets_at":%d},"seven_day":{"used_percentage":21,"resets_at":%d}}}`, b, r5, r7))
	w = readWriters(t, cache)
	if w.Writers.FiveHour == nil || w.Writers.FiveHour.By != a || w.Writers.SevenDay == nil || w.Writers.SevenDay.By != b || w.Writers.SevenDay.Version != "2.1.303" {
		t.Fatalf("書き換えた窓だけ記録を変える: 5h %+v / 7d %+v", w.Writers.FiveHour, w.Writers.SevenDay)
	}

	// 値が同じで observedAt だけ進める書き込み (別のセッション c) は記録を変えない
	c := freshTranscript(t)
	runStatusline(t, cache, fmt.Sprintf(`{"transcript_path":%q,"rate_limits":{"five_hour":{"used_percentage":10,"resets_at":%d},"seven_day":{"used_percentage":21,"resets_at":%d}}}`, c, r5, r7))
	w = readWriters(t, cache)
	if w.Writers.FiveHour.By != a || w.Writers.SevenDay.By != b {
		t.Fatalf("値を書き換えていないのに記録を変えた: 5h %+v / 7d %+v", w.Writers.FiveHour, w.Writers.SevenDay)
	}
}

// 形の違う記録 (壊れた・手で書いた) はそのまま書き戻さず null にする (JSON を壊さない)。
func TestStatuslineWriterDropsMalformedWriters(t *testing.T) {
	now := time.Now()
	r5, r7 := now.Add(time.Hour).Unix(), now.Add(48*time.Hour).Unix()
	cache := t.TempDir()
	path := writeRaw(t, cache, now.Add(-10*time.Minute), 10, r5, 20, r7)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body = []byte(strings.TrimSuffix(strings.TrimSpace(string(body)), "}") + `,"writers":{"five_hour":{"by":"x\",\"evil\":\"1","version":"","at":1},"seven_day":null}}` + "\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, now.Add(-10*time.Minute), now.Add(-10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	runStatusline(t, cache, fmt.Sprintf(`{"transcript_path":%q,"rate_limits":{"seven_day":{"used_percentage":21,"resets_at":%d}}}`, freshTranscript(t), r7))
	w := readWriters(t, cache)
	if w.Writers.FiveHour != nil || w.Writers.SevenDay == nil {
		t.Fatalf("形の違う記録を引き継いだ / 書いた窓を残さない: 5h %+v / 7d %+v", w.Writers.FiveHour, w.Writers.SevenDay)
	}
}

// パスの形でない transcript (引用符・改行など) は記録に入れない (JSON を壊さない)。
func TestStatuslineWriterSanitizesWriter(t *testing.T) {
	now := time.Now()
	cache := t.TempDir()
	runStatusline(t, cache, fmt.Sprintf(`{"transcript_path":"/x/a\"b.jsonl","session_id":"SID-1","rate_limits":{"seven_day":{"used_percentage":21,"resets_at":%d}}}`, now.Add(48*time.Hour).Unix()))
	if w := readWriters(t, cache); w.Writers.SevenDay == nil || w.Writers.SevenDay.By != "SID-1" {
		t.Fatalf("パスの形でない transcript の代わりにセッション ID を残さない: %+v", w.Writers.SevenDay)
	}
}
