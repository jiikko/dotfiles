package dispatcher

import (
	"path/filepath"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/live"
	"pro-con/store"
)

// trackPrompts は、PG の session が入力待ち (status: waiting = 権限の確認か AskUserQuestion。agents.Session.Waiting) で止まった作業中のカードを
// 質問待ち (WaitPermission = 人の番) へ移し、入力待ちでなくなった (答えられて動き出した / turn を終えた) か session が生きていない
// カードを作業中へ戻す (C-054。役の入力待ち = role.go の rr.waiting と同じ判定)。
// 入力待ちかは列そのものが覚えている (記録に残る) ので、出来事と人の番の知らせは入るたびに 1 度だけになる。
// 戻すのは「生きていて入力待ちでない」か「生きていない」とき: 落ちた・消えた PG は作業中の側の経路 (stopCrashing / requeueVanished) が扱う
func (d *Dispatcher) trackPrompts(now time.Time, ss []agents.Session) ([]eventlog.Event, error) {
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
		if c.Launching != "" || c.Deleting() { // 起動・再開の結果待ち / 止めて消すのを待っている
			continue
		}
		var s agents.Session
		ok := false
		if o, has := owned(c, reg); has {
			s, ok = ownedSession(o, ss)
		}
		switch {
		// テストの係の結果を待つ PG は idle で入力待ちにならない。待ち (WaitResource) を上書きすると頼みを見失うので移さない
		case c.State == card.Running && c.Wait.Kind == card.WaitNone && !c.AwaitsRun() && ok && s.Waiting():
			what := s.WaitingLabel()
			if err := d.update(c.ID, func(cc *card.Card) { cc.EnterPrompt(now, what) }); err != nil {
				return notes, err
			}
			notes = append(notes, ev(eventlog.KindHold, c.ID, c.Session, c.ID+": PG が入力待ち ("+what+") で止まっている。attach して答える (人の番)"))
		case c.WaitsOnPrompt() && ok && s.Waiting():
			// 入力待ちのまま中身が変わった (権限の確認に答えた直後に AskUserQuestion で止まった等): 文面だけ直す (列も Since も変えない = 知らせ直さない)
			if q := card.PromptQuestion(s.WaitingLabel()); c.Wait.Question != q {
				if err := d.update(c.ID, func(cc *card.Card) { cc.Wait.Question = q }); err != nil {
					return notes, err
				}
			}
		case c.WaitsOnPrompt():
			why := "PG が入力待ちから動き出した (status " + firstNonEmpty(s.Status, "なし") + ")。作業中へ戻した"
			if !ok {
				why = "入力待ちの PG の session が生きていない (落ちた・止められた)。作業中へ戻した"
			}
			if err := d.update(c.ID, func(cc *card.Card) { cc.LeavePrompt(now, why) }); err != nil {
				return notes, err
			}
		}
	}
	return notes, nil
}
