package main

import (
	"bytes"
	"context"
	"maps"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pro-con/backend"
	"pro-con/card"
	"pro-con/live"
	"pro-con/store"
)

// pro-con ps は PG を、dispatcher が枠に数えたカード (様子の Slots) の「作業中」と、生きていて数えていない「待機中」に分けて出す
// (issue 557)。待機中の行には待っている訳 (pgState)。枠に数えたが起動の記録にまだ無いカードも作業中に行を出す (数と行を合わせる)。
// dispatcher が数え直していない (古い) ときは分けずに「判定できない」と出す。
func TestPSSplitsPGsBySlot(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	cards := []card.Card{
		{ID: "C-001", Title: "t", State: card.Running, Session: "aaaa0001", Since: t0ps},
		{ID: "C-002", Title: "t", State: card.Planned, Session: "aaaa0002", Since: t0ps, Resume: "回答: はい"},
		{ID: "C-003", Title: "t", State: card.Running, Session: "aaaa0003", Since: t0ps, Run: "make test"},
		{ID: "C-004", Title: "t", State: card.Planned, Since: t0ps, Launching: "起動", LaunchedAt: now.Add(-5 * time.Second)},
	}
	if err := store.Update(dir, func(s *store.State) error { s.NextID, s.Cards = 5, cards; return nil }); err != nil {
		t.Fatal(err)
	}
	reg := filepath.Join(dir, live.RegistryFile)
	for i, id := range []string{"C-001", "C-002", "C-003"} {
		if err := live.Register(reg, live.Owned{ID: "aaaa000" + string(rune('1'+i)), SessionID: "s-" + id, PID: 101 + i, CardID: id}); err != nil {
			t.Fatal(err)
		}
	}
	procs := map[int]string{101: "claude bg", 102: "claude bg", 103: "claude bg"}
	ps := func(slotsAt time.Time) (string, []Proc) {
		t.Helper()
		if err := store.SaveDispatcherState(dir, store.DispatcherState{Tick: now, Limit: 2, Cap: 2, Slots: []string{"C-001", "C-004"}, SlotsAt: slotsAt}); err != nil {
			t.Fatal(err)
		}
		var out, errOut bytes.Buffer
		if rc := runPS(nil, dir, func() time.Time { return now }, func(context.Context) (map[int]string, error) { return procs, nil }, &out, &errOut); rc != 0 {
			t.Fatalf("rc=%d: %s", rc, errOut.String())
		}
		rows, _, _ := collectProcs(dir, now, procs)
		return out.String(), rows
	}
	slotOf := func(rows []Proc) map[string]string {
		got := map[string]string{}
		for _, p := range rows {
			if p.Role == "PG" {
				got[p.Card] = p.Slot
			}
		}
		return got
	}

	out, rows := ps(now)
	t.Logf("pro-con ps:\n%s", out) // -v で見本として読む
	order := []string{"── PG 作業中 (枠を使う) 2 / 枠 2", "C-001", "起動の結果を確かめている", "C-004",
		"── PG 待機中 (枠を使わない) 2", "再開待ち (PG の空き待ち)", "C-002", "作業中 (テストの係の結果待ち)", "C-003"}
	at := -1
	for _, want := range order {
		i := strings.Index(out, want)
		if i <= at {
			t.Fatalf("%q が無い / 並びが違う (%v の順のはず):\n%s", want, order, out)
		}
		at = i
	}
	want := map[string]string{"C-001": backend.SlotHeld, "C-002": backend.SlotIdle, "C-003": backend.SlotIdle, "C-004": backend.SlotHeld}
	if got := slotOf(rows); !maps.Equal(got, want) {
		t.Fatalf("slot = %v, want %v", got, want)
	}

	out, rows = ps(now.Add(-backend.DispatcherStale - time.Second)) // 一覧を取れない Tick が続いて数え直していない
	if !strings.Contains(out, "── PG 3 (枠を使っているか判定できない") || strings.Contains(out, "作業中 (枠を使う)") || strings.Contains(out, "C-004") {
		t.Fatalf("dispatcher の数えが古いのに枠で分けた:\n%s", out)
	}
	want = map[string]string{"C-001": backend.SlotUnknown, "C-002": backend.SlotUnknown, "C-003": backend.SlotUnknown}
	if got := slotOf(rows); !maps.Equal(got, want) {
		t.Fatalf("slot = %v, want %v", got, want)
	}
}

// 再開で session が入れ替わったカードは、前の session の行を作業中に数えない (今の session の行だけ)。止まった前の行は区分を持たず、
// 生きている前の行は待機中 (どちらの区分からも落ちない)。今の session が止まっていても作業中の行はその 1 行で、合成の行を重ねない。
func TestPSSlotOnlyCurrentSession(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	if err := store.Update(dir, func(s *store.State) error {
		s.NextID, s.Cards = 2, []card.Card{{ID: "C-001", Title: "t", State: card.Running, Session: "bbbb0002", Since: t0ps}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reg := filepath.Join(dir, live.RegistryFile)
	for i, id := range []string{"bbbb0001", "bbbb0002"} {
		if err := live.Register(reg, live.Owned{ID: id, SessionID: "s" + id, PID: 101 + i, CardID: "C-001"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveDispatcherState(dir, store.DispatcherState{Tick: now, Limit: 2, Cap: 2, Slots: []string{"C-001"}, SlotsAt: now}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		procs map[int]string
		want  map[string]string
	}{
		{"今の session だけ生きている", map[int]string{102: "claude bg"}, map[string]string{"bbbb0001": "", "bbbb0002": backend.SlotHeld}},
		{"前の session だけ生きている", map[int]string{101: "claude bg"}, map[string]string{"bbbb0001": backend.SlotIdle, "bbbb0002": backend.SlotHeld}},
	} {
		rows, slots, _ := collectProcs(dir, now, tc.procs)
		got, pgs := map[string]string{}, 0
		for _, p := range rows {
			if p.Role == "PG" {
				got[p.Session] = p.Slot
				pgs++
			}
		}
		if !slots.known || pgs != 2 || !maps.Equal(got, tc.want) {
			t.Fatalf("%s: slot = %v (PG の行 %d 本, known=%v)", tc.name, got, pgs, slots.known)
		}
	}
}
