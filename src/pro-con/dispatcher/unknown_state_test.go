package dispatcher

import (
	"context"
	"slices"
	"strings"
	"testing"

	"pro-con/agents"
	"pro-con/eventlog"
)

// unknownStates は、pid 無しで止まった state (stopped / done) でも自動の再開の途中 (working) でもない形 (issue 466 の発火条件:
// 再開の途中を表す state の名前が変わった版 / state の欄が無くなった版)。
var unknownStates = []string{"resuming", ""}

// 閉じたカードの PG が pid 無し・知らない state なら、止まったと読まずに止めに行く。「既に止まっていた」と書かず、警告を出来事に出す。
func TestCloseStopsPGInUnknownState(t *testing.T) {
	for _, st := range unknownStates {
		t.Run("state="+st, func(t *testing.T) {
			r := newCrashRig(t)
			r.ss[0].PID, r.ss[0].State = 0, st
			var got []eventlog.Event
			r.d.Record = func(evs []eventlog.Event) { got = append(got, evs...) }
			closeCard(t, r.dir, "C-001")
			r.tick(t)
			c := states(t, r.dir)["C-001"]
			if !slices.Contains(r.l.stops, "id-pc-c-001") || strings.Contains(lastHistory(c), "既に止まっていた") {
				t.Fatalf("知らない state の PG を止まったと読んだ: stops=%v 履歴=%q", r.l.stops, lastHistory(c))
			}
			if !strings.Contains(joinNotes(got), "止まったと判定できない") {
				t.Fatalf("知らない state の警告を出来事に出さない: %s", joinNotes(got))
			}
		})
	}
}

// 終了でも同じ: 記録にある PG (カードの段) と、記録に無い PG (457。unregistered が拾う) のどちらも止め、警告を出す。
func TestShutdownStopsPGInUnknownState(t *testing.T) {
	for _, st := range unknownStates {
		t.Run("記録にある/state="+st, func(t *testing.T) {
			r := newCrashRig(t)
			r.ss[0].PID, r.ss[0].State = 0, st
			notes, err := r.d.Shutdown(context.Background())
			if err != nil || !slices.Contains(r.l.stops, "id-pc-c-001") {
				t.Fatalf("知らない state の PG を止めない: err=%v stops=%v", err, r.l.stops)
			}
			if !strings.Contains(joinNotes(notes), "止まったと判定できない") {
				t.Fatalf("知らない state の警告を出来事に出さない: %s", joinNotes(notes))
			}
		})
		t.Run("記録に無い/state="+st, func(t *testing.T) {
			r, _ := unregisteredRig(t, func(s *agents.Session) { s.Kind, s.PID, s.State = "bg", 0, st })
			notes, err := r.d.Shutdown(context.Background())
			if err != nil || !slices.Contains(r.l.stops, "id-pc-c-001") || strings.Contains(lastHistory(states(t, r.dir)["C-001"]), "既に止まっていた") {
				t.Fatalf("記録に無い知らない state の PG を止まったと読んだ: err=%v stops=%v", err, r.l.stops)
			}
			if !strings.Contains(joinNotes(notes), "止まったと判定できない") {
				t.Fatalf("知らない state の警告を出来事に出さない: %s", joinNotes(notes))
			}
		})
	}
}

// PM も同じ: pid 無し・知らない state の PM は止めに行き、警告を出す。
func TestShutdownStopsPMInUnknownState(t *testing.T) {
	r := startedPM(t)
	r.ss[0].PID, r.ss[0].State = 0, "resuming"
	notes, err := r.d.Shutdown(context.Background())
	if err != nil || !slices.Contains(r.l.stops, "id-"+pmName) {
		t.Fatalf("知らない state の PM を止めない: err=%v stops=%v", err, r.l.stops)
	}
	if !strings.Contains(joinNotes(notes), "止まったと判定できない") {
		t.Fatalf("知らない state の PM の警告を出来事に出さない: %s", joinNotes(notes))
	}
}

// 止まったか・再開の途中かを判定できる session (生きている / pid 無しで working) を止めるときは警告を出さない。
func TestNoUnknownStateWarningForKnownStates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		pid   int
		state string
	}{{"生きている", 42, "resuming"}, {"自動の再開の途中", 0, "working"}} {
		t.Run(tc.name, func(t *testing.T) {
			r := newCrashRig(t)
			r.ss[0].PID, r.ss[0].State = tc.pid, tc.state
			notes, err := r.d.Shutdown(context.Background())
			if err != nil || !slices.Contains(r.l.stops, "id-pc-c-001") {
				t.Fatalf("前提が違う: err=%v stops=%v", err, r.l.stops)
			}
			if strings.Contains(joinNotes(notes), "止まったと判定できない") {
				t.Fatalf("判定できる session に知らない state の警告を出した: %s", joinNotes(notes))
			}
		})
	}
}

// 止めても知らない state のまま一覧に残る版 (state の欄が無い) では、止め直しの周ごとに警告を重ねない (1 本につき 1 回)。
// 止まったと確かめられないので、終了は失敗として名指しする (ok にしない)。
func TestUnknownStateWarningOncePerSession(t *testing.T) {
	r := newCrashRig(t)
	r.ss[0].PID, r.ss[0].State = 0, ""
	r.d.ListAll = func(ctx context.Context) ([]agents.Session, error) { return r.d.List(ctx) } // 止めても state が変わらない
	notes, err := r.d.Shutdown(context.Background())
	if err == nil || !strings.Contains(err.Error(), "C-001 (id-pc-c-001)") {
		t.Fatalf("止まったと確かめられない PG を名指ししない: %v", err)
	}
	if n := strings.Count(joinNotes(notes), "止まったと判定できない"); n > 2 { // カードの段と確かめる段で 1 回ずつまで
		t.Fatalf("知らない state の警告を周ごとに重ねた (%d 回): %s", n, joinNotes(notes))
	}
}
