package dispatcher

// 追加オーダーの届け方 (issue 438。426 の決定 3):
//   - dispatcher は SendMessage を呼べない (Claude Code のツールで、CLI の口が無い) ので、回答と同じ「止めて同じ session を再開」で届ける
//   - 追記は PG の turn の区切りを待つ: 作業中のまま session が idle になった (turn を終えた) ときに再開する。PG が `pro-con card ask / run / review`
//     で turn を終えたときは、その先の再開 (回答・テストの結果・差し戻し) に添えて届ける (再開の文に積む。resumeText)
//   - レビュー待ちのカードに未達が残っていたら (PG が review で turn を終えた後に届いた / 同じ Apply で来た)、PG へ戻して届ける
//     (人間の指示を読まないままの作業を PM にレビューさせない)
//   - 方針変更は待たない: busy でも止めて、指示を差し替えて再開する (worktree は claude stop で消えないので、途中の変更は残る)
//   - 届いたか (Delivered) は再開・起動を確かめたとき (settle) に付ける。失敗と返った再開では付けない (次の試みでまた送る)
//
// PG の様子ごとの扱い:
//   - busy: 追記は積んだまま待つ (未達がカードに見える)
//   - AskUserQuestion / 権限の確認で止まっている (status: waiting): 追記は届けない (止めると問いが消える。人間が attach して進めれば
//     turn が終わり、idle で届く)。方針変更は止めて再開する
//   - 落ちている (一覧に無い / pid 無し): 追記は待つ (自動で再開されれば、その turn の後で届く)。方針変更は prepare の待ち
//     (restartWait) を経て、止めずに再開する。落ち続けて質問待ちになったら、回答の再開に添えて届く

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/live"
	"pro-con/store"
)

// deliverOrders は未達の追加オーダーを持つ作業中のカードを、再開の列 (分解済み) へ戻す (dispatch が同じ session を再開して届ける)。
func (d *Dispatcher) deliverOrders(now time.Time, ss []agents.Session) ([]eventlog.Event, error) {
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
		pending := c.Pending()
		// 削除の依頼を受けたカードは戻さない (PG を止めて消すのを待っている。issue 451)
		if (c.State != card.Running && c.State != card.Review) || c.Launching != "" || c.Deleting() || len(pending) == 0 {
			continue
		}
		o, ok := owned(c, reg)
		if !ok {
			continue // まだ登録していない (起動の直後)。登録を待つ
		}
		// 箱に適用待ちがあれば次の Tick へ (方針変更も): PG が turn の最後に置いた `card review` / `run` を、この後の再開で追い越して除けさせない
		// (箱の依頼は次の Tick の頭で適用される)
		if inboxBusy(d.Dir) {
			continue
		}
		why := ""
		switch {
		case hasRedirect(pending):
			why = "方針変更を受けた。PG を止めて、指示を差し替えて同じ session を再開する (worktree の途中の変更は残る)"
			if c.Run != "" {
				why += "。テストの係への頼み (" + clipLine(c.Run) + ") は取り下げた"
			}
		case c.Run != "" || c.Exec.Active():
			continue // テストの係の結果を渡す再開に添えて届ける
		case idle(o, ss):
			why = "PG が turn を終えた (idle)。追加オーダーを届けるため同じ session を再開する"
			if c.State == card.Review {
				why = "レビュー待ちだが、PG へ届いていない追加オーダーがある。同じ session を再開して届ける"
			}
		default:
			continue // busy / 問いで止まっている / 落ちている: 次の Tick で見直す
		}
		if err := d.update(c.ID, func(cc *card.Card) {
			cc.DropRun()
			cc.State, cc.Since, cc.Stalled = card.Planned, now, false
			cc.History = append(cc.History, card.Event{At: now, Text: why})
		}); err != nil {
			return notes, err
		}
		notes = append(notes, ev(eventlog.KindOrder, c.ID, c.Session, c.ID+": "+why))
	}
	return notes, nil
}

// inboxBusy は受付の箱に適用待ちの依頼がある (読めない・壊れたファイルも数える = store.Pending) か。
func inboxBusy(dir string) bool {
	return store.Pending(dir) > 0
}

func hasRedirect(orders []card.Order) bool {
	for _, o := range orders {
		if o.Kind == card.OrderRedirect {
			return true
		}
	}
	return false
}

// idle は記録の session (同じ session id・同じ pid) が一覧で turn を終えている (status idle) か。
func idle(o live.Owned, ss []agents.Session) bool {
	for _, s := range ss {
		if s.SessionID == o.SessionID && s.PID != 0 && s.PID == o.PID {
			return s.Status == agents.StatusIdle
		}
	}
	return false
}

// ordersText は未達の追加オーダーを PG へ渡す文にする (原文のまま。空なら空)。
func ordersText(c card.Card) string {
	pending := c.Pending()
	if len(pending) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("人間から追加オーダーが届いた (古い順):\n")
	for _, o := range pending {
		switch o.Kind {
		case card.OrderRedirect:
			b.WriteString("- 方針変更 (今までの指示より優先する。途中の作業はこれに合わせて直す):\n")
		default:
			b.WriteString("- 追記 (今の作業の範囲に足す):\n")
		}
		b.WriteString(indent(o.Text) + "\n")
	}
	return b.String()
}

func indent(s string) string { return "  " + strings.ReplaceAll(s, "\n", "\n  ") }

// resumeText は再開で PG に渡す文: 再開の理由 (回答・テストの結果・差し戻し・終了からの再開) に未達の追加オーダーを添える。
func resumeText(c card.Card) string {
	orders := ordersText(c)
	switch {
	case orders == "":
		return c.Resume
	case c.Resume == "":
		return orders + "\n反映して作業を続け、終えたらもう一度 `pro-con card review " + c.ID + "` を実行してから turn を終える。"
	}
	return c.Resume + "\n\n" + orders
}

// markDelivered は起動・再開を確かめたカードの追加オーダーに届いた印を付ける。付けるのは印 (LaunchedAt) の時点で積まれていたものだけ
// (prepare はその Tick の Apply の後に文を組むので、Apply と同じ時刻までのものを全部含む。結果を後の Tick で確かめたとき (adopt) は、
// その間に積まれたものを含まない)。付けた数を返す。
func markDelivered(c *card.Card) int {
	n := 0
	for i := range c.Orders {
		if !c.Orders[i].Delivered && !c.Orders[i].At.After(c.LaunchedAt) {
			c.Orders[i].Delivered = true
			n++
		}
	}
	return n
}

// deliveredNote は届けた数の履歴の文。
func deliveredNote(n int) string {
	return fmt.Sprintf("追加オーダーを %d 件 PG へ届けた", n)
}
