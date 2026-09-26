package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ratelimit/usage"
)

var baseNow = time.Date(2026, 9, 26, 12, 0, 0, 0, time.Local)

func win(label string, pct int, span time.Duration, resetIn time.Duration) usage.Window {
	return usage.Window{Label: label, Percent: pct, WindowMins: int64(span / time.Minute), ResetAt: baseNow.Add(resetIn)}
}

type fakeWorld struct {
	fetched map[source]int
	spawned map[source]int
	fetchWs map[source][]usage.Window
	fetchEr error
	envs    map[string]string
}

func newWorld(t *testing.T) (*fakeWorld, env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	w := &fakeWorld{fetched: map[source]int{}, spawned: map[source]int{}, fetchWs: map[source][]usage.Window{}, envs: map[string]string{}}
	var out, errb bytes.Buffer
	e := env{
		now:      baseNow,
		cacheDir: filepath.Join(t.TempDir(), "glog"),
		fetch: func(_ context.Context, s source) ([]usage.Window, error) {
			w.fetched[s]++
			if w.fetchEr != nil {
				return nil, w.fetchEr
			}
			return append([]usage.Window(nil), w.fetchWs[s]...), nil
		},
		spawn:     func(s source) error { w.spawned[s]++; return nil },
		stdout:    &out,
		stderr:    &errb,
		getenv:    func(k string) string { return w.envs[k] },
		lookupBin: func(string) bool { return true },
	}
	return w, e, &out, &errb
}

func seedCache(t *testing.T, e env, s source, fetchedAt time.Time, ws ...usage.Window) {
	t.Helper()
	if err := saveCache(e.cacheDir, s, cacheEntry{Windows: ws, FetchedAt: fetchedAt}); err != nil {
		t.Fatal(err)
	}
}

func TestCacheFreshRejectsFutureTimestamp(t *testing.T) {
	if (cacheEntry{FetchedAt: baseNow.Add(time.Hour)}).fresh(baseNow) {
		t.Fatal("未来の FetchedAt を fresh と判定した (時計の巻き戻しで古い値を使い続ける)")
	}
	if !(cacheEntry{FetchedAt: baseNow.Add(-time.Minute)}).fresh(baseNow) {
		t.Fatal("1 分前の取得を fresh と判定しない")
	}
}

func TestOverLimitThresholdsAndSkips(t *testing.T) {
	r := sourceResult{windows: []usage.Window{
		win("5h", 80, 5*time.Hour, time.Hour),          // 閾値ちょうど → 超過
		win("7d", 84, 7*24*time.Hour, 24*time.Hour),    // 閾値未満
		win("7d(X)", 90, 7*24*time.Hour, -time.Minute), // リセット済み → 判定しない
		{Label: "cx5h", Percent: 99, WindowMins: 300, Unused: true},
		win("??", 99, 0, time.Hour), // 長さ不明 → 判定しない
		win("cx7d", 85, 7*24*time.Hour, time.Hour),
	}}
	got, judged := overLimit(r, baseNow, 80, 85)
	if judged != 3 {
		t.Fatalf("判定できた枠 = %d, want 3 (5h / 7d / cx7d)", judged)
	}
	labels := make([]string, 0, len(got))
	for _, w := range got {
		labels = append(labels, w.Label)
	}
	if strings.Join(labels, ",") != "5h,cx7d" {
		t.Fatalf("超過判定 = %v, want [5h cx7d]", labels)
	}
}

func TestCheckExitCodes(t *testing.T) {
	t.Run("超過あり", func(t *testing.T) {
		_, e, out, _ := newWorld(t)
		seedCache(t, e, srcClaude, baseNow, win("5h", 90, 5*time.Hour, time.Hour))
		if rc := run([]string{"-source", "claude", "-check"}, e); rc != rcOver {
			t.Fatalf("rc = %d, want %d", rc, rcOver)
		}
		if !strings.Contains(out.String(), "claude 5h 90%") {
			t.Fatalf("出力に超過枠が無い: %q", out.String())
		}
	})
	t.Run("超過なし", func(t *testing.T) {
		_, e, out, _ := newWorld(t)
		seedCache(t, e, srcClaude, baseNow, win("5h", 10, 5*time.Hour, time.Hour))
		if rc := run([]string{"-source", "claude", "-check"}, e); rc != rcOK || out.Len() != 0 {
			t.Fatalf("rc = %d out=%q, want 0 / 空", rc, out.String())
		}
	})
	t.Run("値が無い = 判定不能 (緑にしない)", func(t *testing.T) {
		w, e, _, _ := newWorld(t)
		w.fetchEr = errors.New("boom")
		if rc := run([]string{"-source", "claude", "-check"}, e); rc != rcUnknown {
			t.Fatalf("rc = %d, want %d", rc, rcUnknown)
		}
	})
}

func TestCachedNeverFetchesAndThrottlesSpawn(t *testing.T) {
	w, e, _, _ := newWorld(t)
	seedCache(t, e, srcClaude, baseNow.Add(-10*time.Minute), win("5h", 90, 5*time.Hour, time.Hour))

	if rc := run([]string{"-source", "claude", "-check", "-cached"}, e); rc != rcOver {
		t.Fatalf("古いキャッシュで答えない: rc = %d", rc)
	}
	if w.fetched[srcClaude] != 0 {
		t.Fatal("-cached なのに同期で取得した (hook がプロンプトを待たせる)")
	}
	if w.spawned[srcClaude] != 1 {
		t.Fatalf("古いキャッシュで裏の更新を起こさない: spawned = %d", w.spawned[srcClaude])
	}

	e.now = baseNow.Add(refreshInterval / 2)
	run([]string{"-source", "claude", "-check", "-cached"}, e)
	if w.spawned[srcClaude] != 1 {
		t.Fatalf("refreshInterval 内に 2 度起こした: spawned = %d", w.spawned[srcClaude])
	}

	e.now = baseNow.Add(refreshInterval + time.Second)
	run([]string{"-source", "claude", "-check", "-cached"}, e)
	if w.spawned[srcClaude] != 2 {
		t.Fatalf("refreshInterval 経過後に起こさない: spawned = %d", w.spawned[srcClaude])
	}
}

func TestCachedDoesNotSpawnInsideRefresh(t *testing.T) {
	w, e, _, _ := newWorld(t)
	w.envs[envRefreshing] = "1"
	seedCache(t, e, srcClaude, baseNow.Add(-time.Hour), win("5h", 10, 5*time.Hour, time.Hour))
	run([]string{"-source", "claude", "-check", "-cached"}, e)
	if w.spawned[srcClaude] != 0 {
		t.Fatal("更新プロセスの中 (claude -p の hook) から更新を起こした = 再帰の連鎖")
	}
}

func TestFetchResultSurvivesUnwritableCache(t *testing.T) {
	w, e, out, errb := newWorld(t)
	w.fetchWs[srcClaude] = []usage.Window{win("5h", 90, 5*time.Hour, time.Hour)}
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	e.cacheDir = filepath.Join(blocker, "glog") // ファイルの下 = 作れない
	if rc := run([]string{"-source", "claude", "-check"}, e); rc != rcOver {
		t.Fatalf("保存に失敗しただけで取得結果を捨てた: rc = %d stderr=%q", rc, errb.String())
	}
	if !strings.Contains(out.String(), "5h 90%") || !strings.Contains(errb.String(), "キャッシュを書けない") {
		t.Fatalf("out=%q err=%q", out.String(), errb.String())
	}
}

func TestSourceAllSkipsMissingCLIButNamedDoesNot(t *testing.T) {
	w, e, _, _ := newWorld(t)
	e.lookupBin = func(name string) bool { return name != "codex" }
	w.fetchWs[srcClaude] = []usage.Window{win("5h", 10, 5*time.Hour, time.Hour)}
	run(nil, e)
	if w.fetched[srcCodex] != 0 {
		t.Fatal("codex 未導入なのに -source all で codex を取りに行った")
	}
	run([]string{"-source", "codex"}, e)
	if w.fetched[srcCodex] != 1 {
		t.Fatal("名指しされた codex を黙って外した")
	}
}

func TestCheckIsUnknownWhenNoWindowIsJudgeable(t *testing.T) {
	_, e, _, _ := newWorld(t)
	// キャッシュ後に全枠がリセットを跨いだ = 判定できる枠が 0 本。「超過なし」と答えない
	seedCache(t, e, srcClaude, baseNow, win("5h", 99, 5*time.Hour, -time.Minute))
	if rc := run([]string{"-source", "claude", "-check"}, e); rc != rcUnknown {
		t.Fatalf("rc = %d, want %d", rc, rcUnknown)
	}
}

func TestStaleCacheHasAnAgeLimit(t *testing.T) {
	for name, fetchedAt := range map[string]time.Time{
		"古すぎる":    baseNow.Add(-maxStale - time.Minute),
		"取得時刻が未来": baseNow.Add(time.Hour),
	} {
		for _, args := range [][]string{
			{"-source", "claude", "-check", "-cached"}, // 取得せずに答える経路
			{"-source", "claude", "-check"},            // 取得に失敗して手元へ落ちる経路
		} {
			t.Run(name+" "+strings.Join(args[2:], " "), func(t *testing.T) {
				w, e, _, _ := newWorld(t)
				w.fetchEr = errors.New("boom")
				seedCache(t, e, srcClaude, fetchedAt, win("5h", 99, 5*time.Hour, 2*time.Hour))
				if rc := run(args, e); rc != rcUnknown {
					t.Fatalf("取得が失敗し続けた古いキャッシュで判定した: rc = %d", rc)
				}
			})
		}
	}
}

func TestFetchFailureFallsBackToStaleCache(t *testing.T) {
	w, e, out, errb := newWorld(t)
	w.fetchEr = errors.New("boom")
	seedCache(t, e, srcClaude, baseNow.Add(-10*time.Minute), win("5h", 90, 5*time.Hour, time.Hour))
	if rc := run([]string{"-source", "claude", "-check"}, e); rc != rcOver {
		t.Fatalf("取得失敗で手元の値を捨てた: rc = %d", rc)
	}
	if !strings.Contains(out.String(), "5h 90%") || !strings.Contains(errb.String(), "boom") {
		t.Fatalf("out=%q err=%q (失敗は stderr に出す)", out.String(), errb.String())
	}
}

func TestRefreshEnvMarksChild(t *testing.T) {
	base := append(make([]string, 0, 4), "PATH=/bin") // 余裕のある cap: 共有していれば下の append が上書きする
	got := refreshEnv(base)
	_ = append(base, "OTHER=1")
	if len(got) != 2 || got[1] != envRefreshing+"=1" {
		t.Fatalf("refreshEnv = %v: 印が無いと子の claude -p の hook から更新が連鎖する", got)
	}
}

func TestSourceAllWithNoCLIIsUnknown(t *testing.T) {
	_, e, _, _ := newWorld(t)
	e.lookupBin = func(string) bool { return false }
	if rc := run(nil, e); rc != rcUnknown {
		t.Fatalf("見る出所が 0 件なのに rc = %d", rc)
	}
}

func TestJSONWithNoCLIIsUnknown(t *testing.T) {
	_, e, _, _ := newWorld(t)
	e.lookupBin = func(string) bool { return false }
	if rc := run([]string{"-json"}, e); rc != rcUnknown {
		t.Fatalf("見る出所が 0 件なのに rc = %d", rc)
	}
}
