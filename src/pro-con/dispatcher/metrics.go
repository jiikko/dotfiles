package dispatcher

// 所要の記録 (issue 516)。閉じたカード 1 枚ごとに 1 行を store の metrics.jsonl へ書き、90 日より古い行を消す。書き手は dispatcher だけ。
//
// いつ書くか (どれも「書いていないカードを見つけたら書く」形。落ちても次の Tick / 次の 1 時間が書き直す):
//   - 完了のカード: Apply の直後に記録を見て、PG を止め終えたもの (StopAfterClose が外れた) を書く。書庫へ移す (Archive) より先
//   - 依頼の列ですぐ消した削除 (store.Result.Dropped): Apply の直後に書く。🚨 書けなかったら取り戻せない (カードはもう無い)
//   - PG を止めてから消す削除: 記録から外す (dropCard) 前に書く。書けなければ外さない (次の Tick で書き直してから外す)
//   - 書庫のカード: 1 時間ごとの片付け (purge) で、1 週間の削除 (store.Purge) より先に書く。書けなければ消さない。
//     この issue より前に閉じたカードも、書庫に残っている分はここで埋め戻る
//
// 書いたカードは metered に終わり方と一緒に持つ (起動して最初に使うときにファイルから読む)。同じカードを 2 度書いても、読む側は後の行を正とする。
// 削除の行を書いたのに記録から外せず残ったカード (dropCard の失敗・外す前に落ちて諦めた) は、完了したら完了の行で書き直す。
// 完了の行を書いた後に削除したカードは、削除の行で上書きしない (完了が振り返りの対象。削除は片付け)。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"pro-con/card"
	"pro-con/eventlog"
	"pro-con/live"
	"pro-con/metrics"
	"pro-con/store"
)

// loadMetered は書いたカードの印をファイルから読む (最初の 1 度だけ)。読めない行があっても読めた分は使う (飛ばした行のカードは書き直す)。
func (d *Dispatcher) loadMetered() {
	if d.metered != nil {
		return
	}
	d.metered = map[string]string{}
	rows, _ := store.LoadMetrics(d.Dir)
	for _, r := range rows {
		d.metered[r.Card] = r.Ending
	}
}

// closedMetered は完了の行 (削除ではない行) を書いたか。
func (d *Dispatcher) closedMetered(id string) bool {
	e, ok := d.metered[id]
	return ok && e != metrics.EndDeleted
}

// meter は Apply の直後に、閉じたカードの行を書く (完了で PG を止め終えたもの・依頼の列ですぐ消した削除)。
func (d *Dispatcher) meter(now time.Time, res []store.Result) []eventlog.Event {
	d.loadMetered()
	var dropped []metrics.Row
	for _, r := range res {
		if r.Dropped != nil && r.Err == "" {
			dropped = append(dropped, d.metricRow(*r.Dropped, now, metrics.EndDeleted, now))
		}
	}
	notes := d.writeMetrics(dropped, "") // 記録を読めなくても書く (カードはもう記録に無く、後から取り戻せない)
	st, err := store.Load(d.Dir)
	if err != nil {
		return append(notes, ev(eventlog.KindError, "", "", "所要の記録を書くために記録を読めない (次の Tick で書き直す): "+err.Error()))
	}
	return append(notes, d.writeMetrics(d.unmetered(st.Cards, now), "")...)
}

// unmetered はまだ行を書いていない閉じたカードの行 (完了で、PG を止め終え、削除の途中でないもの)。
func (d *Dispatcher) unmetered(cs []card.Card, now time.Time) []metrics.Row {
	var rows []metrics.Row
	for _, c := range cs {
		if c.State == card.Done && !c.StopAfterClose && !c.Deleting() && !d.closedMetered(c.ID) {
			rows = append(rows, d.metricRow(c, c.Since, "", now))
		}
	}
	return rows
}

// writeMetrics は行を書き、書いたカードに印を付ける。書けなければ出来事を返す (印を付けないので、次に見つけたときに書き直す)。
func (d *Dispatcher) writeMetrics(rows []metrics.Row, then string) []eventlog.Event {
	if err := store.AppendMetrics(d.Dir, rows); err != nil {
		return []eventlog.Event{ev(eventlog.KindError, "", "", "所要の記録に書けない ("+then+"次に見つけたときに書き直す): "+err.Error())}
	}
	for _, r := range rows {
		d.metered[r.Card] = r.Ending
	}
	return nil
}

// meterDeleted は PG を止めてから消す削除のカードの行を、記録から外す前に書く。
func (d *Dispatcher) meterDeleted(c card.Card, now time.Time) error {
	d.loadMetered()
	if c.State == card.Done && d.closedMetered(c.ID) {
		return nil
	}
	if err := store.AppendMetrics(d.Dir, []metrics.Row{d.metricRow(c, now, metrics.EndDeleted, now)}); err != nil {
		return err
	}
	d.metered[c.ID] = metrics.EndDeleted
	return nil
}

// meterArchive は書庫のカードのうち、まだ行を書いていないものを書く (1 週間の削除より先。書けなければ false = 消さない)。
func (d *Dispatcher) meterArchive(now time.Time) ([]eventlog.Event, bool) {
	d.loadMetered()
	arch, err := store.LoadArchive(d.Dir)
	if err != nil && arch == nil {
		return []eventlog.Event{ev(eventlog.KindError, "", "", "所要の記録を埋めるために書庫を読めない (1 週間の削除も次の 1 時間へ): "+err.Error())}, false
	}
	notes := d.writeMetrics(d.unmetered(arch, now), "1 週間の削除も次の 1 時間へ。")
	return notes, len(notes) == 0
}

// pruneMetrics は 90 日より古い行を消す。消す前に件数を出来事の記録へ書く (消した後に落ちても、消した数が残る)。
func (d *Dispatcher) pruneMetrics(now time.Time) []eventlog.Event {
	n, err := store.OldMetrics(d.Dir, now)
	if err == nil && n > 0 {
		d.record([]eventlog.Event{ev(eventlog.KindArchive, "", "", fmt.Sprintf("所要の記録から、閉じてから %d 日たった %d 行を消す", int(store.MetricsKeep.Hours()/24), n))})
		err = store.PruneMetrics(d.Dir, now)
	}
	if err != nil {
		return []eventlog.Event{ev(eventlog.KindError, "", "", "所要の記録の古い行を消せない (次の 1 時間で消し直す): "+err.Error())}
	}
	return nil
}

// metricRow はカード c の行 (枠も埋める)。closed は閉じた時刻、ending は終わり方 (空ならカードから)。
func (d *Dispatcher) metricRow(c card.Card, closed time.Time, ending string, now time.Time) metrics.Row {
	r := metrics.FromCard(c, closed, ending, d.roles())
	r.RecordedAt = now
	u, why := d.cardUsage(c)
	if why != "" {
		r.UsageMissing = why
	} else {
		r.Usage = &u
	}
	return r
}

// cardUsage はカードの PG の枠 (pro-con が起動した session の transcript。今の分と再開で退いた分)。取れなければ理由を返す。
func (d *Dispatcher) cardUsage(c card.Card) (metrics.Usage, string) {
	regPath := filepath.Join(d.Dir, live.RegistryFile)
	var sessions []string
	seen := map[string]bool{}
	for _, load := range []func(string) ([]live.Owned, error){live.LoadRegistry, live.LoadRetired} {
		reg, err := load(regPath)
		if err != nil {
			return metrics.Usage{}, "起動の記録を読めない: " + err.Error()
		}
		for _, o := range reg {
			if o.CardID == c.ID && o.SessionID != "" && !seen[o.SessionID] {
				seen[o.SessionID] = true
				sessions = append(sessions, o.SessionID)
			}
		}
	}
	switch {
	case len(sessions) == 0 && c.Session == "":
		return metrics.Usage{}, "PG を起こしていない"
	case len(sessions) == 0:
		return metrics.Usage{}, "起動の記録に PG の session が無い (片付けで消した?)"
	case d.TranscriptPath == nil:
		return metrics.Usage{}, "transcript の置き場を知らない"
	}
	var files []string
	for _, s := range sessions {
		p, err := d.TranscriptPath(s)
		if errors.Is(err, os.ErrNotExist) {
			return metrics.Usage{}, "transcript が無い (session " + s + ")"
		}
		if err != nil {
			return metrics.Usage{}, "transcript を探せない (session " + s + "): " + err.Error()
		}
		files = append(files, metrics.TranscriptFiles(p)...)
	}
	u, err := metrics.ReadUsage(files)
	if err != nil {
		return metrics.Usage{}, "transcript を読めない: " + err.Error()
	}
	return u, ""
}
