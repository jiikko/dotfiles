package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"testing"
	"time"

	"pro-con/card"
	"pro-con/dispatcher"
	"pro-con/live"
	"pro-con/store"
)

var t0ps = time.Date(2026, 9, 26, 1, 0, 0, 0, time.UTC)

// PG の行には、カードと session の食い違いだけを印す: 作業中なのに止まっている / 完了・片付け済みなのに動いている (issue 497)。
// 完了したカードの止まった session と、作業中のカードの動いている session は食い違いではない。
func TestPSMarksMismatch(t *testing.T) {
	dir := t.TempDir()
	cards := []card.Card{
		{ID: "C-001", Title: "t", State: card.Running, Session: "aaaa0001", Since: t0ps},
		{ID: "C-002", Title: "t", State: card.Running, Session: "aaaa0002", Since: t0ps},
		{ID: "C-003", Title: "t", State: card.Done, Ending: card.EndAnswered, Since: t0ps},
		{ID: "C-004", Title: "t", State: card.Done, Ending: card.EndAnswered, Since: t0ps},
		{ID: "C-006", Title: "t", State: card.Review, Session: "aaaa0006", Since: t0ps, Issues: []card.IssueRef{{Repo: "r", Number: 1, Status: "open"}}},
	}
	if err := store.Update(dir, func(s *store.State) error { s.NextID, s.Cards = 7, cards; return nil }); err != nil {
		t.Fatal(err)
	}
	reg := filepath.Join(dir, live.RegistryFile)
	for i, id := range []string{"C-001", "C-002", "C-003", "C-004", "C-005", "C-006"} {
		if err := live.Register(reg, live.Owned{ID: fmt.Sprintf("aaaa000%d", i+1), SessionID: "s-" + id, PID: 101 + i, CardID: id}); err != nil {
			t.Fatal(err)
		}
	}
	// 動いているのは C-001 (作業中)・C-003 (完了)・C-005 (記録に無い = 片付け済み)
	rows, _ := collectProcs(dir, time.Now(), map[int]string{101: "claude bg", 103: "claude bg", 105: "claude bg"})
	got := map[string]string{}
	for _, p := range rows {
		if p.Role == "PG" {
			got[p.Card] = p.Mismatch
		}
	}
	want := map[string]string{"C-001": "", "C-002": "止まっている (カードは作業中)", "C-003": "動いている (カードは完了)", "C-004": "", "C-005": "動いている (カードは片付け済み)", "C-006": ""}
	if !maps.Equal(got, want) {
		t.Fatalf("食い違い = %v\nwant %v", got, want)
	}
	// 記録を読めなければ食い違いを付けない (どの PG も片付け済みに見える)
	if err := os.WriteFile(filepath.Join(dir, store.StateFile), []byte("{壊れた"), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, _ = collectProcs(dir, time.Now(), map[int]string{101: "claude bg"})
	for _, p := range rows {
		if p.Mismatch != "" {
			t.Fatalf("記録を読めないのに食い違いを付けた: %+v", p)
		}
	}
}

// 見張り・supervisor の行は、lock の pid が生きていてコマンド行がその役のときだけ「動いている」(pid の使い回しで別のプロセスをその役と出さない)。
func TestPSShowsMonitor(t *testing.T) {
	for role, c := range map[string]struct{ lock, cmd string }{
		"見張り":        {dispatcher.MonitorLockFile, "/bin/pro-con monitor --until-stdin-closes"},
		"supervisor": {dispatcher.SupervisorLockFile, "/bin/pro-con supervise"},
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, c.lock), []byte("77\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		row := func(procs map[int]string) Proc {
			t.Helper()
			rows, _ := collectProcs(dir, time.Now(), procs)
			for _, p := range rows {
				if p.Role == role {
					return p
				}
			}
			t.Fatalf("%sの行が無い: %+v", role, rows)
			return Proc{}
		}
		if p := row(map[int]string{77: c.cmd}); p.PID != 77 || p.State != "動いている" {
			t.Fatalf("動いている%sを出さない: %+v", role, p)
		}
		if p := row(map[int]string{77: "/usr/bin/vim notes"}); p.PID != 0 || p.State != "止まっている" {
			t.Fatalf("pid を使い回した別のプロセスを%sと出した: %+v", role, p)
		}
	}
}
