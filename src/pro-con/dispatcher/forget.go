package dispatcher

// 完了から 1 週間たったカードを記録から消し (store.Purge。その前に所要の記録を書き、90 日より古い所要の行を消す = metrics.go)、片付けが済んだカードの起動の記録の行と片付けの印を消す (issue 497)。
// どちらも dispatcher が書き手 (書庫・印・起動の記録。426 の決定 1)。
// 🚨 ここで消すのはカードの記録だけ。session・transcript・worktree・ブランチは pro-con worktree clean --yes が消す (schedule.go の予定が毎日回す。issue 550)。
// 書庫を消すのも予定の 1 行 (schedule.CardPurge。毎日 03:30。dispatcher の中で回す)。予定のタブに出て、schedule off で止まる。

import (
	"fmt"
	"path/filepath"
	"time"

	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/live"
	"pro-con/schedule"
	"pro-con/store"
	"pro-con/wtclean"
)

// purge は予定 (schedule.CardPurge。schedule.go の runInternal から) で書庫の古いカードを消し、予定の記録に書く rc と結果の 1 行を返す。
// 消せなくても Tick は続ける (書庫が大きいままになるだけ。次の予定で消し直す)。
func (d *Dispatcher) purge(now time.Time) (rc int, note string, notes []eventlog.Event) {
	notes = d.pruneMetrics(now)
	pruned := len(notes) == 0
	metered, ok := d.meterArchive(now) // 消すカードの所要を先に書く (issue 516)
	notes = append(notes, metered...)
	if !ok {
		return 1, "消すカードの所要の記録を書けないので、書庫から消さなかった (出来事の error を見る)", notes
	}
	gone, err := store.Purge(d.Dir, now, d.purgeMark)
	if err != nil {
		notes = append(notes, ev(eventlog.KindError, "", "", "完了から 1 週間たったカードを消せない (次の予定で消し直す): "+err.Error()))
		return 1, "書庫から消せない: " + err.Error(), notes
	}
	for _, c := range gone {
		notes = append(notes, ev(eventlog.KindArchive, c.ID, "", c.ID+": "+store.PurgeText))
	}
	body := fmt.Sprintf("書庫から消した %d 枚", len(gone))
	if !pruned { // 書庫からは消せたが、所要の記録の古い行は消せなかった (出来事に理由がある)
		body += "・所要の記録の古い行は消せない"
		rc = 1
	}
	return rc, schedule.ResultLine(body, rc), notes
}

// purgeMark は消すカードの片付けの印 (worktree・ブランチ・pro-con が起動した session)。起動の記録を読めなければ失敗 (印の無いまま消さない)。
func (d *Dispatcher) purgeMark(c card.Card) (store.Purged, error) {
	var m store.Purged
	if p := d.Repos[c.Repo]; p != "" {
		m.Worktree = card.WorktreePath(p, c)
		m.Branch = wtclean.BranchName(card.SessionName(c))
	}
	regPath := filepath.Join(d.Dir, live.RegistryFile)
	for _, load := range []func(string) ([]live.Owned, error){live.LoadRegistry, live.LoadRetired} {
		reg, err := load(regPath)
		if err != nil {
			return m, err
		}
		for _, o := range reg {
			if o.CardID == c.ID && o.SessionID != "" {
				m.Sessions = append(m.Sessions, store.PurgedSession{ID: o.ID, SessionID: o.SessionID})
			}
		}
	}
	return m, nil
}

// forget は片付け (worktree clean) が置いた forget の依頼を当てる: 挙げた session の行を起動の記録から消し、片付けの印を消す。
// 行を先に消す (印を先に消して落ちると、残った行のカードを片付けが「記録に無い」と見て二度と消せない)。
// 🚨 落ちて取りこぼしても、もう一度 worktree clean --yes を打てば同じ依頼を置き直す (消す物が無くなっても行と印が残っていれば依頼する)。
func (d *Dispatcher) forget(res []store.Result) []eventlog.Event {
	var notes []eventlog.Event
	for _, r := range res {
		if r.Kind != store.KindForget || r.Err != "" {
			continue
		}
		n, err := live.Forget(filepath.Join(d.Dir, live.RegistryFile), r.CardID, r.Sessions)
		if err == nil {
			err = store.DropPurged(d.Dir, r.CardID)
		}
		if err != nil {
			notes = append(notes, ev(eventlog.KindError, r.CardID, "", r.CardID+": 片付けの後の起動の記録と印を消せない (もう一度 pro-con worktree clean --yes で頼み直せる): "+err.Error()))
			continue
		}
		notes = append(notes, ev(eventlog.KindArchive, r.CardID, "", fmt.Sprintf("%s: 片付けが済んだので、起動の記録の行 %d 本と片付けの印を消した", r.CardID, n)))
	}
	return notes
}
