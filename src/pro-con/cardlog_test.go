package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/backend"
)

// card log はカードの PG の活動 (応答の文と道具の呼び出し) を、session を名乗ってから時刻の順に出す。--json は 1 行 1 活動。
func TestCardLog(t *testing.T) {
	env := viewFixture(t)
	appendTranscript(t, env, `{"type":"assistant","timestamp":"2026-09-25T01:01:00Z","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}`)
	rc, out, errOut := viewCmd(t, env, "log", "C-001")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if rc != 0 || errOut != "" || len(lines) != 3 || lines[0] != "── session s1 ──" ||
		!strings.HasSuffix(lines[1], "  色の候補を 3 つ作った") || !strings.HasSuffix(lines[2], "  Bash: go test ./...") {
		t.Fatalf("log: rc=%d out=%q err=%q", rc, out, errOut)
	}
	rc, out, _ = viewCmd(t, env, "log", "C-001", "--json")
	var got []backend.Activity
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		var a backend.Activity
		if json.Unmarshal([]byte(l), &a) != nil {
			t.Fatalf("--json の行が読めない: %q", l)
		}
		got = append(got, a)
	}
	if rc != 0 || len(got) != 2 || got[1].Tool != "Bash" || got[1].Text != "go test ./..." || got[1].Session != "s1" {
		t.Fatalf("--json: rc=%d %+v", rc, got)
	}
	if rc, _, errOut := viewCmd(t, env, "log", "C-002"); rc != 0 || !strings.Contains(errOut, "まだ無い") {
		t.Fatalf("session の無いカード: rc=%d err=%q", rc, errOut)
	}
	if rc, _, _ := viewCmd(t, env, "log", "C-999"); rc != 1 {
		t.Fatalf("無いカードは rc=1: %d", rc)
	}
	if rc, _, _ := viewCmd(t, env, "log"); rc != 2 {
		t.Fatalf("カードの無い呼び出しは rc=2: %d", rc)
	}
}

// --follow は transcript に足された活動を追って出す (dispatcher の知らせが無くても、読み直しの間隔で)。取り消しで rc=0。
func TestCardLogFollow(t *testing.T) {
	env := viewFixture(t)
	old := viewPoll
	viewPoll = 20 * time.Millisecond
	t.Cleanup(func() { viewPoll = old })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	env.ctx = ctx
	var out, errOut syncBuf
	rc := make(chan int, 1)
	go func() { rc <- runCard([]string{"log", "C-001", "--follow"}, env, &out, &errOut) }()
	waitUntil(t, "最初の活動を出さない", func() bool { return strings.Contains(out.String(), "色の候補") })
	appendTranscript(t, env, `{"type":"assistant","timestamp":"2026-09-25T01:02:00Z","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"close.go"}}]}}`)
	waitUntil(t, "足された活動を追わない", func() bool { return strings.Contains(out.String(), "Edit: close.go") })
	cancel()
	if r := <-rc; r != 0 {
		t.Fatalf("取り消しで rc=%d (%s)", r, errOut.String())
	}
	if n := strings.Count(out.String(), "色の候補"); n != 1 {
		t.Fatalf("前に出した活動を繰り返した (%d 回): %q", n, out.String())
	}
}

// appendTranscript は viewFixture の C-001 の PG の transcript (sess-1) に 1 行足す。
func appendTranscript(t *testing.T, env viewEnv, line string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(env.projects, "-repo", "sess-1.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
}
