package usage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// isolateShared はこのテスト専用の共有ファイルの置き場を作る (テスト間で取得結果を共有しない)。
func isolateShared(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	return filepath.Join(dir, "glog")
}

// setClock はゲートの時刻を固定し、返す関数で進める。
func setClock(t *testing.T, start time.Time) func(time.Duration) {
	t.Helper()
	now := start
	var mu sync.Mutex
	clock = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	t.Cleanup(func() { clock = time.Now })
	return func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
}

// countingClaude は `-p` で起こされた回数を calls ファイルへ 1 行ずつ書く偽の claude を PATH に置く。
// stdout は body (sh の printf 文) が書く。
func countingClaude(t *testing.T, body string) (calls func() int) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	writeStub(t, dir, "claude", `case "$1" in
--version) printf '%s\n' "9.9.9 (Claude Code)";;
*) echo x >> `+log+`
`+body+`;;
esac
`)
	t.Setenv("PATH", dir)
	return func() int {
		b, err := os.ReadFile(log)
		if err != nil {
			return 0
		}
		return strings.Count(string(b), "\n")
	}
}

// 実物 (2.1.287) の stream-json の形: hook の system 行・assistant 行 (usage_report)・result 行。
const streamOK = `printf '%s\n' '{"type":"system","subtype":"init"}'
printf '%s\n' '{"type":"assistant","usage_report":{"session":{},"rate_limits":{"limits":[]}}}'
printf '%s\n' '{"type":"result","result":"Current session: 2% used ` + "·" + ` resets Jul 22 at 3:09am (Asia/Tokyo)","is_error":false}'
`

// サーバが 429 を返したときの形: rate_limits が null で、result には枠の行が無い。
const streamNoLimits = `printf '%s\n' '{"type":"assistant","usage_report":{"session":{},"rate_limits":null}}'
printf '%s\n' '{"type":"result","result":"You are currently using your subscription to power your Claude Code usage","is_error":false}'
`

func fetchT(t *testing.T) (*Snapshot, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return Fetch(ctx)
}

func TestFetchSharesRecentResultAcrossCalls(t *testing.T) {
	isolateShared(t)
	advance := setClock(t, time.Date(2026, 7, 20, 12, 0, 0, 0, time.Local))
	calls := countingClaude(t, streamOK)

	for i := range 2 {
		snap, err := fetchT(t)
		if err != nil {
			t.Fatalf("Fetch %d: %v", i, err)
		}
		if w, ok := snap.Find("5h"); !ok || w.Percent != 2 {
			t.Fatalf("Fetch %d: 5h 枠が無い: %+v", i, snap.Windows)
		}
	}
	if got := calls(); got != 1 {
		t.Fatalf("5 分以内の 2 回目で claude を %d 回起こした, want 1", got)
	}
	advance(SharedFresh)
	if _, err := fetchT(t); err != nil {
		t.Fatal(err)
	}
	if got := calls(); got != 2 {
		t.Fatalf("5 分経っても取り直さない: claude %d 回, want 2", got)
	}
}

func TestFetchBacksOffWhenServerReturnsNoLimits(t *testing.T) {
	isolateShared(t)
	advance := setClock(t, time.Date(2026, 7, 20, 12, 0, 0, 0, time.Local))
	calls := countingClaude(t, streamNoLimits)

	_, err := fetchT(t)
	if err == nil || !strings.Contains(err.Error(), "サーバから利用枠を受け取れない") || !strings.Contains(err.Error(), "12:10 まで取得を止める") {
		t.Fatalf("err = %v, want サーバが枠を返さない旨と再開時刻", err)
	}
	advance(noLimitsBackoff - time.Minute)
	if _, err := fetchT(t); err == nil || !strings.Contains(err.Error(), "12:10 まで") {
		t.Fatalf("止めている間の err = %v", err)
	}
	if got := calls(); got != 1 {
		t.Fatalf("止めている間に claude を起こした: %d 回, want 1", got)
	}
	advance(time.Minute)
	_, _ = fetchT(t)
	if got := calls(); got != 2 {
		t.Fatalf("止める期間が過ぎても取り直さない: claude %d 回, want 2", got)
	}
}

// サーバが枠を返さなくなっても、その前に取れていた結果は 5 分の間そのまま返る
// (止める判定は、取りに行って枠が無かったときだけ)。
func TestFetchNoLimitsKeepsFreshResult(t *testing.T) {
	dir := isolateShared(t)
	advance := setClock(t, time.Date(2026, 7, 20, 12, 0, 0, 0, time.Local))
	countingClaude(t, streamOK)
	if _, err := fetchT(t); err != nil {
		t.Fatal(err)
	}
	countingClaude(t, streamNoLimits)
	advance(SharedFresh)
	if _, err := fetchT(t); err == nil {
		t.Fatal("枠の無い応答でエラーにならない")
	}
	st := loadShared(filepath.Join(dir, sharedFile), clock())
	if st.Snapshot == nil || st.BlockedUntil.IsZero() {
		t.Fatalf("共有ファイル = %+v, want 最後の枠を残したまま BlockedUntil が立つ", st)
	}
}

// usage_report を出さない版 (または書式変更) で読めないときは、10 分止めずに通常の失敗として返す。
// ただし失敗も共有し、5 分は claude を起こし直さない (読めない応答のたびに起こすとサーバを高頻度に叩く)。
func TestFetchParseFailureIsSharedButNotBlocked(t *testing.T) {
	isolateShared(t)
	advance := setClock(t, time.Date(2026, 7, 20, 12, 0, 0, 0, time.Local))
	calls := countingClaude(t, `printf '%s' '{"result":"unknown format","is_error":false}'`)
	for range 2 {
		_, err := fetchT(t)
		if err == nil || strings.Contains(err.Error(), "受け取れない") || !strings.Contains(err.Error(), "検出できず") {
			t.Fatalf("err = %v, want 通常のパース失敗", err)
		}
	}
	if got := calls(); got != 1 {
		t.Fatalf("5 分以内の失敗の後に claude を起こし直した: %d 回, want 1", got)
	}
	advance(SharedFresh)
	_, _ = fetchT(t)
	if got := calls(); got != 2 {
		t.Fatalf("5 分経っても取り直さない: claude %d 回, want 2", got)
	}
}

// usage_report はあるが rate_limits キーが無い (null ではない) なら、止める理由にしない。
func TestFetchUsageReportWithoutRateLimitsKeyIsNotNoLimits(t *testing.T) {
	isolateShared(t)
	setClock(t, time.Date(2026, 7, 20, 12, 0, 0, 0, time.Local))
	countingClaude(t, `printf '%s\n' '{"type":"assistant","usage_report":{"session":{}}}'
printf '%s\n' '{"type":"result","result":"x","is_error":false}'
`)
	if _, err := fetchT(t); err == nil || strings.Contains(err.Error(), "受け取れない") {
		t.Fatalf("err = %v, want 通常のパース失敗", err)
	}
}

// 呼び出し側の持ち時間切れは共有しない (他の呼び出し元を 5 分止めない)。
func TestFetchCallerTimeoutIsNotShared(t *testing.T) {
	isolateShared(t)
	setClock(t, time.Date(2026, 7, 20, 12, 0, 0, 0, time.Local))
	// sleep-ok: 入力: 呼び出し側の持ち時間 (200ms) より遅い claude を演じる
	calls := countingClaude(t, "sleep 2\n"+streamOK)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	_, err := Fetch(ctx)
	cancel()
	if err == nil {
		t.Fatal("持ち時間切れでエラーにならない")
	}
	countingClaude(t, streamOK)
	if _, err := fetchT(t); err != nil {
		t.Fatalf("持ち時間切れが共有されて次の呼び出しが失敗した: %v", err)
	}
	_ = calls
}

// force は 5 分の共有を飛ばすが、止めている間は飛ばさない。
func TestFetchSharedForce(t *testing.T) {
	isolateShared(t)
	setClock(t, time.Date(2026, 7, 20, 12, 0, 0, 0, time.Local))
	calls := countingClaude(t, streamOK)
	ctx := context.Background()
	if _, err := FetchShared(ctx, fetchClaude, false); err != nil {
		t.Fatal(err)
	}
	if _, err := FetchShared(ctx, fetchClaude, true); err != nil {
		t.Fatal(err)
	}
	if got := calls(); got != 2 {
		t.Fatalf("force で取り直さない: claude %d 回, want 2", got)
	}
	countingClaude(t, streamNoLimits)
	_, _ = FetchShared(ctx, fetchClaude, true)
	calls = countingClaude(t, streamOK)
	if _, err := FetchShared(ctx, fetchClaude, true); err == nil || !strings.Contains(err.Error(), "まで取得を止める") {
		t.Fatalf("止めている間に force で取った: err = %v", err)
	}
	if got := calls(); got != 0 {
		t.Fatalf("止めている間に force で claude を起こした: %d 回", got)
	}
}

// 未来の時刻 (時計の巻き戻り・壊れたファイル) を信用しない。
func TestLoadSharedDistrustsFutureTimes(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), sharedFile)
	st := sharedState{
		Snapshot:     &Snapshot{Windows: []Window{{Label: "5h", Percent: 1}}},
		FetchedAt:    now.Add(time.Hour),
		AttemptedAt:  now.Add(time.Hour),
		LastErr:      "x",
		BlockedUntil: now.Add(noLimitsBackoff + time.Minute),
	}
	if err := saveShared(path, st); err != nil {
		t.Fatal(err)
	}
	got := loadShared(path, now)
	if got.Snapshot != nil {
		t.Error("未来の fetchedAt の枠を新しいものとして読んだ")
	}
	if got.LastErr != "" {
		t.Error("未来の attemptedAt の失敗を読んだ")
	}
	if !got.BlockedUntil.IsZero() {
		t.Error("now + 止める期間を超える blockedUntil を読んだ")
	}
	st.BlockedUntil = now.Add(noLimitsBackoff)
	if err := saveShared(path, st); err != nil {
		t.Fatal(err)
	}
	if loadShared(path, now).BlockedUntil.IsZero() {
		t.Error("期間内の blockedUntil まで捨てた")
	}
}

// result キーを持っていても type が result でない行は結果にしない。
func TestParseStreamIgnoresNonResultLinesWithResultKey(t *testing.T) {
	out := []byte(`{"type":"result","result":"Current session: 2% used · resets Jul 22 at 3:09am (Asia/Tokyo)","is_error":false}
{"type":"system","subtype":"hook_response","result":"garbage"}
`)
	snap, err := ParseStream(out, time.Date(2026, 7, 20, 12, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatalf("ParseStream: %v", err)
	}
	if w, ok := snap.Find("5h"); !ok || w.Percent != 2 {
		t.Fatalf("snap = %+v", snap.Windows)
	}
}

// 同時に呼ばれても claude を起こすのは 1 本だけ (flock)。
func TestFetchConcurrentCallsRunClaudeOnce(t *testing.T) {
	isolateShared(t)
	setClock(t, time.Date(2026, 7, 20, 12, 0, 0, 0, time.Local))
	// sleep-ok: 窓: 1 本目が claude を起こしている間に、残りがロック待ちへ入る時間を作る
	calls := countingClaude(t, "sleep 0.3\n"+streamOK)
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Go(func() {
			_, err := fetchT(t)
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := calls(); got != 1 {
		t.Fatalf("同時の 4 本で claude を %d 回起こした, want 1", got)
	}
}

// 別プロセスが書いたファイルの表示用文字列は、読むときに制御列を落とす。
func TestLoadSharedStripsControlSequences(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, sharedFile)
	if err := os.WriteFile(path, []byte(`{"snapshot":{"Windows":[{"Label":"5h\u001b[2J","Percent":1}],"Version":"1\u001b]0;x\u0007"},"fetchedAt":"2026-07-20T12:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st := loadShared(path, time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC))
	if st.Snapshot == nil || strings.ContainsRune(st.Snapshot.Windows[0].Label, 0x1b) || strings.ContainsRune(st.Snapshot.Version, 0x1b) {
		t.Fatalf("制御列が残った: %+v", st.Snapshot)
	}
}

// 読めないときの誤りには、実際に返った文の 1 行目を 80 文字まで添える (pro-con の dispatcher から移した検査)。
func TestParseStreamErrorQuotesFirstLine(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.Local)
	_, err := ParseStream([]byte(`{"type":"result","result":"Total cost: $0.00\nTotal duration: 1s","is_error":false}`), now)
	if err == nil || !strings.Contains(err.Error(), "Total cost: $0.00") || strings.Contains(err.Error(), "Total duration") {
		t.Fatalf("1 行目を添えない: %v", err)
	}
	_, err = ParseStream([]byte(`{"type":"result","result":"`+strings.Repeat("あ", 200)+`","is_error":false}`), now)
	if err == nil || strings.Count(err.Error(), "あ") != 80 || !strings.Contains(err.Error(), "…") {
		t.Fatalf("1 行目を切らない: %v", err)
	}
}
