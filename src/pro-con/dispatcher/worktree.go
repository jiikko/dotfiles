package dispatcher

// PG の worktree を、カードを閉じる・消すその場で片付ける (issue 553)。残り物 (「master に無い commit」「記録に無いカード」で
// 自動の片付けが永久に残す worktree) を作らない:
//   - close: PG の worktree・ブランチに取り込み先 (origin/master) に無い commit があれば、完了を除ける (理由を出す)。
//     取り込まない終わり方 (card.Ending.Discards。--ending rejected / answered / investigated) を付けたときだけ通す
//   - 取り込まない終わり方で閉じたカードと、削除したカードは、PG を止め終えた直後に worktree を片付ける (wtclean.Settle。
//     取り込み先に無い commit は refs/pro-con/removed/<名前>/<sha> に残してから、worktree とブランチを消す)
//   - 未 commit の変更・動いている session・人の lock があるものは消さない (退避しない。設定画面のディスクのタブに理由つきで残り、
//     人が commit してから「消す」「残す」を決める)
// 「取り込み済み」の判定は片付けと同じ wtclean の 1 か所 (Unlanded / Judge) を使う。

import (
	"context"
	"fmt"
	"time"

	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/store"
	"pro-con/wtclean"
)

// WorktreeOps は PG の worktree を見る・片付ける口 (main が組む。nil なら close を検査せず片付けもしない = e2e モードの偽の worktree)。
type WorktreeOps interface {
	// Unlanded はカードの PG の worktree・ブランチに取り込み先に無い commit があれば理由を返す (wtclean.Unlanded)
	Unlanded(ctx context.Context, repoPath string, c card.Card) (string, error)
	// Settle はカードの PG の worktree を片付ける (wtclean.Settle。worktree clean --yes とは worktree-clean.lock で重ねない)
	Settle(ctx context.Context, repoName string, c card.Card) wtclean.Result
}

// applyInbox は受付の箱を記録へ適用する (close は取り込み済みかを見てから)。
func (d *Dispatcher) applyInbox(ctx context.Context, now time.Time) ([]store.Result, error) {
	return store.ApplyChecked(d.Dir, now, d.Repos, func(st store.State, r store.Request) error { return d.checkClose(ctx, st, r) })
}

// checkClose は close の依頼を当てる前に、PG の作業が取り込み先に入っているかを見る。入っていなければ除ける。
func (d *Dispatcher) checkClose(ctx context.Context, st store.State, r store.Request) error {
	if r.Kind != "close" || d.Worktrees == nil || r.Ending.Discards() {
		return nil
	}
	var c card.Card
	found := false
	for _, x := range st.Cards {
		if x.ID == r.CardID {
			c, found = x, true
		}
	}
	repo := d.Repos[c.Repo]
	if !found || repo == "" { // カードが無い・repo が分からないものは適用の側が除けるか、PG の worktree を持たない
		return nil
	}
	why, err := d.Worktrees.Unlanded(ctx, repo, c)
	if err != nil {
		return fmt.Errorf("PG の作業が取り込み先に入っているか確かめられない: %v (確かめられるようにしてから閉じ直す。取り込まずに閉じるなら --ending rejected)", err)
	}
	if why != "" {
		return fmt.Errorf("PG の作業が取り込まれていない: %s (取り込んで push してから閉じる。取り込まずに閉じるなら --ending rejected / answered / investigated を付ける = commit を %s に残して worktree とブランチを消す)",
			why, wtclean.RemovedRef(card.SessionName(c)))
	}
	return nil
}

// recheckClosed は、取り込んで閉じたカードの PG を止め終えた後に、取り込み先に無い commit が無いかを見直す。close の検査は依頼を
// 当てるときなので、その後・止め終える前に PG が commit を足せる (レビュー待ちの PG が turn を終える前に閉じた等)。あれば出来事と履歴に
// 書く (完了は戻さない。worktree は判定で人が決めるものになり、設定画面のディスクのタブに出る)。
func (d *Dispatcher) recheckClosed(ctx context.Context, c card.Card) *eventlog.Event {
	repo := d.Repos[c.Repo]
	if d.Worktrees == nil || repo == "" {
		return nil
	}
	why, err := d.Worktrees.Unlanded(ctx, repo, c)
	var text string
	switch {
	case err != nil:
		text = "閉じた後に PG の作業が取り込み先に入っているか確かめられない: " + err.Error() + " (設定画面のディスクのタブで見る)"
	case why != "":
		text = "閉じた後に PG の作業に取り込まれていないものができた: " + why + " (取り込むか、設定画面のディスクのタブで消す・残すを決める)"
	default:
		return nil
	}
	e := ev(eventlog.KindError, c.ID, c.Session, c.ID+": "+text)
	if err := d.update(c.ID, func(cc *card.Card) { cc.History = append(cc.History, card.Event{At: d.Now(), Text: text}) }); err != nil {
		e.Reason += " (履歴に書けない: " + err.Error() + ")"
	}
	return &e
}

// settleWorktree は、閉じた (取り込まない終わり方)・削除したカードの PG の worktree を片付け、出来事を返す (片付ける物が無ければ nil)。
// 閉じたカードは履歴にも書く (削除したカードはもう記録に無い)。
func (d *Dispatcher) settleWorktree(ctx context.Context, c card.Card, kind string, closed bool) *eventlog.Event {
	if d.Worktrees == nil {
		return nil
	}
	r := d.Worktrees.Settle(ctx, c.Repo, c)
	var text string
	switch r.Outcome {
	case wtclean.Removed:
		text = "PG の worktree とブランチを消した (" + r.Detail + ")"
	case wtclean.TreeRemoved:
		text = "PG の worktree を消した (" + r.Detail + ")"
	case wtclean.Skipped:
		if r.Detail == wtclean.NoWorktree {
			return nil
		}
		text = "PG の worktree は残した: " + r.Detail + " (設定画面のディスクのタブに出る)"
	case wtclean.Failed, wtclean.Held:
		kind = eventlog.KindError
		text = "PG の worktree を片付けられない: " + r.Detail + " (設定画面のディスクのタブに出る)"
	}
	e := ev(kind, c.ID, c.Session, c.ID+": "+text)
	if closed {
		if err := d.update(c.ID, func(cc *card.Card) { cc.History = append(cc.History, card.Event{At: d.Now(), Text: text}) }); err != nil {
			e.Reason += " (履歴に書けない: " + err.Error() + ")"
		}
	}
	return &e
}
