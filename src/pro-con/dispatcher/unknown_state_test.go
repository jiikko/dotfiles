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
