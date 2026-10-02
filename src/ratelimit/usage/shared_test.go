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
	advance(sharedFresh)
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
	if err == nil || !strings.Contains(err.Error(), "サーバが利用枠を返さない") || !strings.Contains(err.Error(), "12:30 まで取得を止める") {
		t.Fatalf("err = %v, want サーバが枠を返さない旨と再開時刻", err)
	}
	advance(noLimitsBackoff - time.Minute)
	if _, err := fetchT(t); err == nil || !strings.Contains(err.Error(), "12:30 まで") {
		t.Fatalf("止めている間の err = %v", err)
	}
	if got := calls(); got != 1 {
		t.Fatalf("止めている間に claude を起こした: %d 回, want 1", got)
	}
	advance(time.Minute)
	_, _ = fetchT(t)
	if got := calls(); got != 2 {
		t.Fatalf("30 分経っても取り直さない: claude %d 回, want 2", got)
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
	advance(sharedFresh)
	if _, err := fetchT(t); err == nil {
		t.Fatal("枠の無い応答でエラーにならない")
	}
	st := loadShared(filepath.Join(dir, sharedFile))
	if st.Snapshot == nil || st.BlockedUntil.IsZero() {
		t.Fatalf("共有ファイル = %+v, want 最後の枠を残したまま BlockedUntil が立つ", st)
	}
}

// usage_report を出さない版 (または書式変更) で読めないときは、止めずに従来どおりのパース失敗にする。
func TestFetchParseFailureWithoutUsageReportDoesNotBlock(t *testing.T) {
	isolateShared(t)
	setClock(t, time.Date(2026, 7, 20, 12, 0, 0, 0, time.Local))
	calls := countingClaude(t, `printf '%s' '{"result":"unknown format","is_error":false}'`)
	for range 2 {
		_, err := fetchT(t)
		if err == nil || strings.Contains(err.Error(), "サーバが利用枠を返さない") {
			t.Fatalf("err = %v, want 通常のパース失敗", err)
		}
	}
	if got := calls(); got != 2 {
		t.Fatalf("パース失敗で止めてしまった: claude %d 回, want 2", got)
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
	st := loadShared(path)
	if st.Snapshot == nil || strings.ContainsRune(st.Snapshot.Windows[0].Label, 0x1b) || strings.ContainsRune(st.Snapshot.Version, 0x1b) {
		t.Fatalf("制御列が残った: %+v", st.Snapshot)
	}
}
