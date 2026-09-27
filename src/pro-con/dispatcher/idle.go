package dispatcher

import (
	"time"

	"pro-con/card"
	"pro-con/store"
)

// defaultQuietListEvery は暇な間に session の一覧を取る間隔 (issue 559)。一覧の取得は毎回 claude 本体を起こす
// (1 回 CPU 約 0.13 秒。3 秒ごとだと 1 日 約 2.9 万回)。暇な間でも、外で起きたこと (完了したカードの PG が残っている・
// 知らない session) はこの間隔で拾う。
const defaultQuietListEvery = 30 * time.Second

// listing は、この Tick で session の一覧を取るか。取らないのは暇な Tick だけ:
//   - quiet (カードと役に、一覧と照らして扱うものが無い)。箱の依頼は先に記録へ適用してあるので、依頼でカードが増えた・動いた
//     Tick は暇ではない (その Tick で一覧を取って起動・登録する)
//   - 前に一覧を取った Tick も暇で、それから QuietListEvery 経っていない (忙しい Tick の直後は取る。画面には忙しい間の鮮度で
//     一覧を渡しているので、暇になったと分かった Tick で取り直して、間引いた鮮度 (store.Seen の Keep) で渡し直す)
//
// 暇の判定は記録 (カードと役の様子のファイル) から Tick ごとに決め直す。カードが増える・動く・役が起きると、次の Tick から毎回取る。
func (d *Dispatcher) listing(now time.Time) (list, quiet bool) {
	quiet = d.quiet()
	if !quiet || !d.listedQuiet || d.listedAt.IsZero() {
		return true, quiet
	}
	return now.Sub(d.listedAt) >= d.quietListEvery(), quiet
}

func (d *Dispatcher) quietListEvery() time.Duration {
	if d.QuietListEvery > 0 {
		return d.QuietListEvery
	}
	return defaultQuietListEvery
}

// quiet は、一覧と照らして扱うものが無いか。読めないものがあれば暇と言わない (一覧を取る側に倒す)。
//
// 一覧を使う経路 (tick の register 〜 collectDoing) が扱うのは、完了していないカード・止める途中 / 削除待ちのカード・
// 起動の結果待ち・生きている (または起こし直す) 役だけ。どれも無ければ、一覧を間引いても遅れるものは無い。
// 生きている役は、知らせる物が無くても暇にしない: tellRole が一覧で役の入力待ち (人への知らせ) と落ちた時刻 (DeadSince。終了の
// stopRole が自動の再開を待つかをこれで決める) を見張っている。間引くとどちらも最長 QuietListEvery 遅れる (issue 559 の敵対的レビュー 2 周目)。
func (d *Dispatcher) quiet() bool {
	st, err := store.Load(d.Dir)
	if err != nil {
		return false
	}
	for _, c := range st.Cards {
		if c.State != card.Done || c.StopAfterClose || c.Deleting() || c.Launching != "" {
			return false
		}
	}
	if d.PMRepo == "" {
		return true // 役を回さない (tellRole も何もしない)
	}
	for _, r := range roles() {
		if r.off(d) {
			continue
		}
		pm, err := store.LoadRole(d.Dir, r.file, r.name)
		if err != nil {
			return false
		}
		if pm.Launching != "" || (pm.Session != "" && !pm.Stopped) {
			return false
		}
	}
	return true
}
