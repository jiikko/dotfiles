package dispatcher

import (
	"context"
	"strings"
	"testing"
	"time"

	"pro-con/agents"
	"pro-con/eventlog"
	"pro-con/store"
)

func joinNotes(notes []eventlog.Event) string {
	var b strings.Builder
	for _, n := range notes {
		b.WriteString(n.Reason + "\n")
	}
	return b.String()
}

// Tick の出来事は種類・カード・session を持ち (文から読み取らせない)、画面への知らせ (Changed) より先に Record へ渡る
// (知らせで読みに来た pro-con log --follow が、その出来事をもう読める)。適用した依頼も出来事になる。
func TestTickRecordsEventsBeforeChanged(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	if _, err := store.Submit(dir, store.Request{Kind: "add", Title: "次", Repo: "dotfiles"}); err != nil {
		t.Fatal(err)
	}
	d := newDispatcher(t, dir, &fakeLauncher{}, nil)
	var got []eventlog.Event
	var order []string
	d.Record = func(evs []eventlog.Event) { got = append(got, evs...); order = append(order, "record") }
	d.Changed = func() { order = append(order, "changed") }
	notes, err := d.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(notes) {
		t.Fatalf("Record に渡した数が Tick の返り値と違う: %d / %d", len(got), len(notes))
	}
	if order[len(order)-1] != "changed" || !strings.Contains(strings.Join(order, " "), "record changed") {
		t.Fatalf("Record の後に Changed を呼んでいない: %v", order)
	}
	find := func(kind string) eventlog.Event {
		for _, e := range got {
			if e.Kind == kind {
				return e
			}
		}
		t.Fatalf("%s の出来事が無い: %+v", kind, got)
		return eventlog.Event{}
	}
	if e := find(eventlog.KindApply); e.Card != "C-002" || !strings.Contains(e.Reason, "(add) を適用した") {
		t.Fatalf("適用の出来事: %+v", e)
	}
	if e := find(eventlog.KindLaunch); e.Card != "C-001" || e.Session != "id-pc-c-001" {
		t.Fatalf("起動の出来事にカード・session が無い: %+v", e)
	}
}

// 終了 (Shutdown) の出来事も Record へ渡る (止めた・止め直した は止めている dispatcher にしか分からない)。
func TestShutdownRecordsEvents(t *testing.T) {
	r := newCrashRig(t) // C-001 が作業中・登録済み
	var got []eventlog.Event
	r.d.Record = func(evs []eventlog.Event) { got = append(got, evs...) }
	notes, err := r.d.Shutdown(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) == 0 || len(got) != len(notes) {
		t.Fatalf("Shutdown の出来事を Record に渡していない: %d / %d", len(got), len(notes))
	}
	for _, e := range got {
		if e.Kind == eventlog.KindStop && e.Card != "" && e.Session != "" {
			return
		}
	}
	t.Fatalf("止めた出来事にカード・session が無い: %+v", got)
}

// 閉じたカードの PG を止めた結果 (止めた / 既に止まっていた / 止められない) も、種類 stop・カード・session つきで Record へ渡る
// (close.go。pro-con log --card で追える)。
func TestCloseStopRecordsEvents(t *testing.T) {
	closed := func(t *testing.T, setup func(r *crashRig), want string) {
		t.Helper()
		r := newCrashRig(t)
		now := t0
		r.d.Now = func() time.Time { return now }
		setup(r)
		var got []eventlog.Event
		r.d.Record = func(evs []eventlog.Event) { got = append(got, evs...) }
		closeCard(t, r.dir, "C-001")
		r.tick(t)
		now = t0.Add(closeStopWait)
		r.tick(t)
		for _, e := range got {
			if e.Kind == eventlog.KindStop && e.Card == "C-001" && e.Session == "id-pc-c-001" && strings.Contains(e.Reason, want) {
				return
			}
		}
		t.Fatalf("「%s」の stop の出来事が無い: %+v", want, got)
	}
	closed(t, func(*crashRig) {}, "PG の session を止めた")
	closed(t, func(r *crashRig) { r.ss[0].PID, r.ss[0].State = 0, agents.StateStopped }, "既に止まっていた")
	closed(t, func(r *crashRig) { r.l.stopFail = true }, "止められない")
}
