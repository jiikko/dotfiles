package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"pro-con/eventlog"
)

var logT0 = time.Date(2026, 9, 25, 1, 0, 0, 0, time.UTC)

// logFixture は出来事を 3 つ置いた置き場 (C-001 の起動、C-002 の適用、カードの無いエラー)。
func logFixture(t *testing.T) string { t.Helper(); return logFixtureIn(t, viewDir(t)) }

// logFixtureIn は logFixture を置き場 dir に作る。
func logFixtureIn(t *testing.T, dir string) string {
	t.Helper()
	if err := eventlog.Append(dir, []eventlog.Event{
		{At: logT0, Kind: eventlog.KindLaunch, Card: "C-001", Session: "s1", Reason: "C-001 に PG を起動した (s1)"},
		{At: logT0.Add(time.Minute), Kind: eventlog.KindApply, Card: "C-002", Reason: "箱の依頼 r2 (add) を適用した"},
		{At: logT0.Add(2 * time.Minute), Kind: eventlog.KindError, Reason: "session の一覧を取れない"},
	}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func logCmd(t *testing.T, ctx context.Context, dir string, args ...string) (int, string, string) {
	t.Helper()
	var o, e bytes.Buffer
	rc := runLog(ctx, args, dir, func() time.Time { return logT0.Add(3 * time.Minute) }, &o, &e)
	return rc, o.String(), e.String()
}

// --card / --since で絞り、--json は 1 行 1 出来事で出す。
func TestLogFilters(t *testing.T) {
	dir := logFixture(t)
	ctx := context.Background()
	rc, out, errOut := logCmd(t, ctx, dir)
	if rc != 0 || errOut != "" || strings.Count(out, "\n") != 3 || !strings.Contains(out, "launch    C-001   s1") {
		t.Fatalf("全部: rc=%d out=%q err=%q", rc, out, errOut)
	}
	if _, out, _ := logCmd(t, ctx, dir, "--card", "c-001"); strings.Count(out, "\n") != 1 || !strings.Contains(out, "C-001 に PG を起動した") {
		t.Fatalf("--card: %q", out)
	}
	if _, out, _ := logCmd(t, ctx, dir, "--since", logT0.Add(time.Minute).Format(time.RFC3339)); strings.Count(out, "\n") != 2 || strings.Contains(out, "起動した") {
		t.Fatalf("--since (RFC 3339): %q", out)
	}
	if _, out, _ := logCmd(t, ctx, dir, "--since", "90s"); strings.Count(out, "\n") != 1 || !strings.Contains(out, "一覧を取れない") {
		t.Fatalf("--since (長さ = 今から前): %q", out)
	}
	rc, out, _ = logCmd(t, ctx, dir, "--json", "--card", "C-002")
	var e eventlog.Event
	if rc != 0 || strings.Count(out, "\n") != 1 || json.Unmarshal([]byte(out), &e) != nil || e.Kind != eventlog.KindApply || e.Card != "C-002" {
		t.Fatalf("--json: rc=%d %q", rc, out)
	}
	if rc, _, _ := logCmd(t, ctx, dir, "--since", "きのう"); rc != 2 {
		t.Fatalf("読めない --since は rc=2: %d", rc)
	}
	if rc, out, _ := logCmd(t, ctx, viewDir(t)); rc != 0 || out != "" {
		t.Fatalf("記録の無い置き場: rc=%d %q", rc, out)
	}
}

func TestParseSince(t *testing.T) {
	loc := time.FixedZone("JST", 9*3600)
	now := time.Date(2026, 9, 25, 10, 30, 0, 0, loc)
	for in, want := range map[string]time.Time{
		"15:04":            time.Date(2026, 9, 25, 15, 4, 0, 0, loc),
		"09:05:06":         time.Date(2026, 9, 25, 9, 5, 6, 0, loc),
		"2026-09-24 08:00": time.Date(2026, 9, 24, 8, 0, 0, 0, loc),
		"2026-09-24":       time.Date(2026, 9, 24, 0, 0, 0, 0, loc),
		"10m":              now.Add(-10 * time.Minute),
	} {
		if got, err := parseSince(in, now); err != nil || !got.Equal(want) {
			t.Fatalf("%q: %v %v (期待 %v)", in, got, err, want)
		}
	}
}

// --follow は dispatcher の知らせですぐ読み直す (ポーリングの間隔を 1 時間にしても待たない)。絞り込みは足された分にも効く。
func TestLogFollowWakesOnNotification(t *testing.T) {
	dir := logFixture(t)
	f := listenFake(t, dir)
	old := viewPoll
	viewPoll = time.Hour
	t.Cleanup(func() { viewPoll = old })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out, errOut syncBuf
	rc := make(chan int, 1)
	go func() {
		rc <- runLog(ctx, []string{"--follow", "--card", "C-003"}, dir, time.Now, &out, &errOut)
	}()
	waitUntil(t, "--follow が購読しない", f.subscribed)
	if err := eventlog.Append(dir, []eventlog.Event{
		{At: time.Now(), Kind: eventlog.KindLaunch, Card: "C-004", Reason: "C-004 に PG を起動した"},
		{At: time.Now(), Kind: eventlog.KindWatchdog, Card: "C-003", Reason: "C-003: watchdog: 15 分進捗なし"},
	}); err != nil {
		t.Fatal(err)
	}
	f.broadcast()
	waitUntil(t, "知らせを受けても足された出来事を出さない", func() bool { return strings.Contains(out.String(), "watchdog: 15 分進捗なし") })
	if strings.Contains(out.String(), "C-004") || strings.Contains(out.String(), "C-001") {
		t.Fatalf("--card で絞っていない: %q", out.String())
	}
	cancel()
	if r := <-rc; r != 0 {
		t.Fatalf("取り消しで rc=%d (%s)", r, errOut.String())
	}
}

// --follow --since は、読み始めた後に足された出来事を時刻で落とさない (画面の出来事は画面が置いた時刻のまま後から書かれる。issue 445)。
// 読み始めの分は --since で絞る。
func TestLogFollowKeepsLateEvents(t *testing.T) {
	dir := logFixture(t)
	f := listenFake(t, dir)
	old := viewPoll
	viewPoll = time.Hour
	t.Cleanup(func() { viewPoll = old })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out, errOut syncBuf
	rc := make(chan int, 1)
	go func() {
		rc <- runLog(ctx, []string{"--follow", "--since", "30s"}, dir, func() time.Time { return logT0.Add(3 * time.Minute) }, &out, &errOut)
	}()
	waitUntil(t, "--follow が購読しない", f.subscribed)
	if err := eventlog.Append(dir, []eventlog.Event{{At: logT0, Kind: eventlog.KindScreen, Reason: "画面 (pid 1): 開いた"}}); err != nil {
		t.Fatal(err)
	}
	f.broadcast()
	waitUntil(t, "遅れて書かれた画面の出来事を --since で落とした", func() bool { return strings.Contains(out.String(), "画面 (pid 1): 開いた") })
	if strings.Contains(out.String(), "C-001 に PG を起動した") {
		t.Fatalf("読み始めの分を --since で絞っていない: %q", out.String())
	}
	cancel()
	if r := <-rc; r != 0 {
		t.Fatalf("取り消しで rc=%d (%s)", r, errOut.String())
	}
}

// 🚨 pro-con log (--follow も) は、状態の置き場を 1 バイトも変えず、socket へ wake / notify を送らない (441 の守ること 1 / 445)。
// 購読の sub だけは送ってよい (TestViewCommandsDoNotWrite と同じ形)。
func TestLogDoesNotWrite(t *testing.T) {
	dir := logFixture(t)
	f := listenFake(t, dir)
	old := viewPoll
	viewPoll = 20 * time.Millisecond
	t.Cleanup(func() { viewPoll = old })
	before := snapshotTree(t, dir)
	for _, args := range [][]string{
		{}, {"--json"}, {"--card", "C-001"}, {"--since", "10m"}, {"--since", "nosuch"}, {"--help"},
		{"--follow"}, {"--follow", "--json", "--card", "C-002"},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		logCmd(t, ctx, dir, args...)
		cancel()
	}
	after := snapshotTree(t, dir)
	if len(before) != len(after) {
		t.Fatalf("置き場のファイルが増減した: 前 %d / 後 %d", len(before), len(after))
	}
	for p, v := range before {
		if after[p] != v {
			t.Fatalf("pro-con log が %s を変えた:\n前 %s\n後 %s", p, v, after[p])
		}
	}
	lines := f.drain()
	if !f.subscribed() {
		t.Fatal("前提: --follow が購読していない (socket の検査が空振りしている)")
	}
	for _, l := range lines {
		if l != "sub" {
			t.Fatalf("pro-con log が socket に %q を送った (送ってよいのは sub だけ)", l)
		}
	}
}

// 🚨 log --follow は、socket の逃がし先の緩い権限を直さず、つながずに読み直しで待ち、そう 1 行知らせる (issue 445)。
func TestLogDoesNotFixFallbackDir(t *testing.T) {
	long, fallback := looseFallback(t)
	dir := logFixtureIn(t, long)
	old := viewPoll
	viewPoll = 20 * time.Millisecond
	t.Cleanup(func() { viewPoll = old })
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	rc, out, errOut := logCmd(t, ctx, dir, "--follow")
	if rc != 0 || strings.Count(out, "\n") != 3 || strings.Count(errOut, "読み直しで待つ") != 1 {
		t.Fatalf("逃がし先を使えないことを 1 行知らせない / 読めない: rc=%d %q %q", rc, out, errOut)
	}
	stillLoose(t, fallback)
}
