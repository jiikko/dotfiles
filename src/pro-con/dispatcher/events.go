package dispatcher

import (
	"fmt"

	"pro-con/eventlog"
	"pro-con/store"
)

// ev は出来事を 1 つ作る (時刻は書く側 = Record が付ける)。
func ev(kind, cardID, session, text string) eventlog.Event {
	return eventlog.Event{Kind: kind, Card: cardID, Session: session, Reason: text}
}

// noteKinds は、記録を変えずに出来事として書くだけの依頼の種類と、書く出来事の種類。
var noteKinds = map[string]string{
	store.KindEvent:      eventlog.KindScreen,
	store.KindMonitor:    eventlog.KindMonitor,
	store.KindSupervisor: eventlog.KindSupervisor,
}

// applied は箱の依頼を適用した・除けた結果を出来事にする。
func applied(res []store.Result) []eventlog.Event {
	var out []eventlog.Event
	for _, r := range res {
		// 画面の出来事・見張り (issue 475)・supervisor (issue 506) の知らせは、その出来事として書く (「依頼を適用した」を重ねない)
		if kind, ok := noteKinds[r.Kind]; ok && r.Err == "" {
			out = append(out, eventlog.Event{At: r.At, Kind: kind, Card: r.CardID, Reason: r.Note})
			continue
		}
		from := "" // どの画面から置いた依頼か (issue 481)
		if r.Screen != "" {
			from = "・画面 " + r.Screen
		}
		if r.Err != "" {
			out = append(out, ev(eventlog.KindReject, r.CardID, "", fmt.Sprintf("箱の依頼 %s (%s%s) を除けた: %s", r.ID, r.Kind, from, r.Err)))
			continue
		}
		out = append(out, ev(eventlog.KindApply, r.CardID, "", fmt.Sprintf("箱の依頼 %s (%s%s) を適用した", r.ID, r.Kind, from)))
		switch {
		case r.Kind == store.KindConfig:
			out = append(out, ev(eventlog.KindConfig, "", "", r.Note))
		case r.Kind == store.KindForget: // 消した結果は forget が出来事にする
		case r.Note != "": // 記録から外したカードは履歴に残せないので、出来事にだけ残る
			out = append(out, ev(eventlog.KindDelete, r.CardID, "", r.CardID+": "+r.Note))
		}
	}
	return out
}

func (d *Dispatcher) record(evs []eventlog.Event) {
	if d.Record != nil && len(evs) > 0 {
		d.Record(evs)
	}
}
