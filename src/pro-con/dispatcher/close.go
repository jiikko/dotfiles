package dispatcher

// PG を止めてから片付けるカード: 閉じたカード (issue 447。StopAfterClose) と、削除の依頼を受けたカード (issue 451。DeleteAt)。
// 印は依頼の適用と同時に付き、dispatcher が Tick ごとに止める。止めた・止まったかの判定は終了のとき (shutdown.go の ensureStopped) と同じ部品を使う。
// 🚨 PG の worktree とブランチは消さない (PM が cherry-pick で取り込む。削除しても取り込み前の作業が入っている)。

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/live"
	"pro-con/store"
)

// closeStopWait は、印を付けてから止まったのを確かめられるまで Tick ごとに止め直す長さ。過ぎたら諦めて履歴に書く
// (close はそのまま成立している。削除はカードを消さずに残す)。
const closeStopWait = time.Minute

// stopMarked は印の付いたカードの PG の session (起動の記録にあるもの・再開で入れ替わった前のもの) を止める。
// 1 回の Tick では待たない (ensureStopped を 0 周で回す)。止まらなければ次の Tick で止め直す。
// 削除のカードは作業の途中でも印が付くので、起動・再開の直後でまだ記録に無い session も止める (終了のときと同じ stopTarget)。
// 閉じたカードは PG が review を打った後なので、記録に載っている。
func (d *Dispatcher) stopMarked(ctx context.Context, now time.Time, ss []agents.Session) ([]eventlog.Event, error) {
	st, err := store.Load(d.Dir)
	if err != nil {
		return nil, err
	}
	reg, err := live.LoadRegistry(filepath.Join(d.Dir, live.RegistryFile))
	if err != nil {
		return nil, err
	}
	var notes []eventlog.Event
	for _, c := range st.Cards {
		deleting := c.Deleting()
		if !c.StopAfterClose && !deleting {
			continue
		}
		// 諦めるまでの時間は、この dispatcher が止めに入った時刻からも数える (落ちていた dispatcher が起動し直した最初の Tick で、1 度も待たずに諦めない)
		if _, ok := d.stopFrom[c.ID]; !ok {
			if d.stopFrom == nil {
				d.stopFrom = map[string]time.Time{}
			}
			d.stopFrom[c.ID] = now
		}
		since := c.Since
		if deleting {
			since = c.DeleteAt
		}
		if from := d.stopFrom[c.ID]; from.After(since) {
			since = from
		}
		// wait: 待てば分かる形 (時間で終わる)。この間は消さず、諦めもしない。unsure: 止まったと確かめられない形。消さず、上限で諦める
		var extra map[string]string
		var wait bool
		var unsure []string
		if deleting {
			extra, wait, unsure = d.deleteTargets(c, now, ss, reg)
		}
		more, remaining, sent, err := d.ensureStopped(ctx, extra, map[string]bool{c.ID: true}, 0)
		notes = append(notes, more...)
		stopped := sent > 0 || c.StopSent // 前の Tick で止める要求を出して、この Tick で止まったのを見た形も「止めた」
		if err == nil && len(remaining) == 0 && !wait && len(unsure) == 0 {
			delete(d.stopFrom, c.ID)
			if deleting {
				n, err := d.dropCard(c, now, stopped)
				if n != "" {
					notes = append(notes, ev(eventlog.KindDelete, c.ID, c.Session, c.ID+": "+n))
				}
				if err != nil {
					return notes, err
				}
				continue
			}
			text := "閉じたので PG の session を止めた (worktree とブランチは残す)"
			if !stopped {
				text = "閉じた後に確かめたら PG の session は既に止まっていた"
			}
			notes = append(notes, ev(eventlog.KindStop, c.ID, c.Session, c.ID+": "+text))
			if err := d.finishMarkedStop(c.ID, now, text); err != nil {
				return notes, err
			}
			continue
		}
		if sent > 0 && !c.StopSent {
			if err := d.update(c.ID, func(cc *card.Card) { cc.StopSent = true }); err != nil {
				return notes, err
			}
		}
		if wait || now.Sub(since) < closeStopWait {
			continue // 次の Tick で止め直す
		}
		delete(d.stopFrom, c.ID)
		reasons := make([]string, 0, len(more)+len(remaining)+len(unsure))
		for _, m := range more {
			reasons = append(reasons, m.Reason)
		}
		reasons = append(reasons, remaining...)
		if err != nil {
			reasons = append(reasons[:len(more)], err.Error())
		}
		reasons = append(reasons, unsure...)
		why := strings.Join(reasons, " / ")
		kind, text := eventlog.KindStop, fmt.Sprintf("閉じたが PG の session を止められない: %s (止める: claude stop <id>)", why)
		if deleting {
			kind, text = eventlog.KindDelete, fmt.Sprintf("削除できない: PG の session を止められない: %s (カードは残した。止めてからもう一度削除する: claude stop <id>)", why)
		}
		notes = append(notes, ev(kind, c.ID, c.Session, c.ID+": "+text))
		if err := d.finishMarkedStop(c.ID, now, text); err != nil {
			return notes, err
		}
	}
	return notes, nil
}

// deleteTargets は削除のカードで、記録の行に加えて止める session (短い id → カード) と、消すのを待つ理由を返す。
//   - 起動・再開の直後・落ちて自動の再開の途中は、終了のときと同じ stopTarget で扱う (待てば分かる = wait)
//   - 🚨 stopTarget の「止めるものが無い」は、終了のときは列を変えないだけだが、削除ではカードを消す根拠になる。記録に載っていない
//     カードの session は ensureStopped が終了と同じ unregistered で拾う (launchGrace の内でも。短い id と worktree で示せれば止め、
//     示せない・session id が無ければ残りとして名指しする = 消さない)
//   - 再開で入れ替わった前の session の記録が読めなければ、その分を確かめられないので消さない
//   - テストの係への頼みが残っている間も消さない (実行の印を失うと、残った実行を止められない。tickRuns が止めて取り下げる)
func (d *Dispatcher) deleteTargets(c card.Card, now time.Time, ss []agents.Session, reg []live.Owned) (map[string]string, bool, []string) {
	extra := map[string]string{}
	var unsure []string
	plan := d.stopTarget(c, now, ss, reg)
	if plan.target != "" {
		extra[plan.target] = c.ID
	}
	if _, err := live.LoadRetired(filepath.Join(d.Dir, live.RegistryFile)); err != nil {
		unsure = append(unsure, "再開で入れ替わった前の session の記録を読めない: "+err.Error())
	}
	return extra, plan.wait || c.Run != "" || c.Exec.Active(), unsure
}

// finishMarkedStop は印を外して履歴に書く (止め終えた / 諦めた)。削除の印も外す (諦めたカードは残り、もう一度削除を頼める)。
func (d *Dispatcher) finishMarkedStop(id string, now time.Time, text string) error {
	return d.update(id, func(cc *card.Card) {
		cc.StopAfterClose, cc.StopSent, cc.DeleteAt, cc.DeleteBy = false, false, time.Time{}, ""
		cc.History = append(cc.History, card.Event{At: now, Text: text})
	})
}

// dropCard は PG が止まったのを確かめた削除のカードを記録から外し、記録に残す出来事の文を返す。
// 外せない (不変条件に反する = 子カードが後から付いた等) ときは印を外して履歴に書き、カードを残す。
// 🚨 pro-con が起動した session の記録の行は消さない (終了のときの確かめで、消したカードの PG も止まっていることを見続ける)。
func (d *Dispatcher) dropCard(c card.Card, now time.Time, stopped bool) (string, error) {
	how := "PG の session を止めた"
	if !stopped {
		how = "PG の session は動いていなかった"
	}
	err := store.Update(d.Dir, func(s *store.State) error {
		s.Cards = slices.DeleteFunc(s.Cards, func(cc card.Card) bool { return cc.ID == c.ID })
		return nil
	})
	if err == nil {
		return fmt.Sprintf("「%s」を削除した (%s が依頼。%s。worktree とブランチは残す)", c.Title, c.DeleteBy, how), nil
	}
	text := fmt.Sprintf("削除できない: %v (%s。カードは残した)", err, how)
	return text, d.finishMarkedStop(c.ID, now, text)
}
