package dispatcher

// 閉じたカードの PG を止める (issue 447)。close の適用でカードに StopAfterClose が付き、dispatcher が Tick ごとに止める。
// 止めた・止まったかの判定は終了のとき (shutdown.go の ensureStopped) と同じ部品を使う。
// 🚨 PG の worktree とブランチは消さない (PM が cherry-pick で取り込む)。

import (
	"context"
	"fmt"
	"strings"
	"time"

	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/store"
)

// closeStopWait は、閉じてから止まったのを確かめられるまで Tick ごとに止め直す長さ。過ぎたら諦めて履歴に書く (close 自体は成立している)。
const closeStopWait = time.Minute

// stopClosed は StopAfterClose の付いたカードの PG の session (起動の記録にあるもの・再開で入れ替わった前のもの) を止める。
// 1 回の Tick では待たない (ensureStopped を 0 周で回す)。止まらなければ次の Tick で止め直す。
func (d *Dispatcher) stopClosed(ctx context.Context, now time.Time) ([]eventlog.Event, error) {
	st, err := store.Load(d.Dir)
	if err != nil {
		return nil, err
	}
	var notes []eventlog.Event
	for _, c := range st.Cards {
		if !c.StopAfterClose {
			continue
		}
		more, remaining, sent, err := d.ensureStopped(ctx, nil, map[string]bool{c.ID: true}, 0)
		if err == nil && len(remaining) == 0 {
			text := "閉じたので PG の session を止めた (worktree とブランチは残す)"
			if sent == 0 && !c.CloseStopSent { // 前の Tick で止める要求を出して、この Tick で止まったのを見た形は「止めた」
				text = "閉じた後に確かめたら PG の session は既に止まっていた"
			}
			notes = append(notes, ev(eventlog.KindStop, c.ID, c.Session, c.ID+": "+text))
			if err := d.finishCloseStop(c.ID, now, text); err != nil {
				return notes, err
			}
			continue
		}
		if sent > 0 && !c.CloseStopSent {
			if err := d.update(c.ID, func(cc *card.Card) { cc.CloseStopSent = true }); err != nil {
				return notes, err
			}
		}
		if now.Sub(c.Since) < closeStopWait {
			continue // 次の Tick で止め直す
		}
		reasons := make([]string, 0, len(more)+len(remaining))
		for _, m := range more {
			reasons = append(reasons, m.Reason)
		}
		reasons = append(reasons, remaining...)
		if err != nil {
			reasons = append(reasons[:len(more)], err.Error())
		}
		why := strings.Join(reasons, " / ")
		text := fmt.Sprintf("閉じたが PG の session を止められない: %s (止める: claude stop <id>)", why)
		notes = append(notes, ev(eventlog.KindStop, c.ID, c.Session, c.ID+": "+text))
		if err := d.finishCloseStop(c.ID, now, text); err != nil {
			return notes, err
		}
	}
	return notes, nil
}

func (d *Dispatcher) finishCloseStop(id string, now time.Time, text string) error {
	return d.update(id, func(cc *card.Card) {
		cc.StopAfterClose, cc.CloseStopSent = false, false
		cc.History = append(cc.History, card.Event{At: now, Text: text})
	})
}
