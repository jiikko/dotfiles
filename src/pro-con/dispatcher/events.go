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

// applied は箱の依頼を適用した・除けた結果を出来事にする。
func applied(res []store.Result) []eventlog.Event {
	var out []eventlog.Event
	for _, r := range res {
		if r.Err != "" {
			out = append(out, ev(eventlog.KindReject, r.CardID, "", fmt.Sprintf("箱の依頼 %s (%s) を除けた: %s", r.ID, r.Kind, r.Err)))
			continue
		}
		out = append(out, ev(eventlog.KindApply, r.CardID, "", fmt.Sprintf("箱の依頼 %s (%s) を適用した", r.ID, r.Kind)))
		if r.Note != "" { // 記録から外したカードは履歴に残せないので、出来事にだけ残る
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
