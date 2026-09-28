package dispatcher

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"pro-con/card"
	"pro-con/eventlog"
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
//   - 見張っている役 (watchable) の様子が動いた合図が無い (roleMoved)
//
// 暇の判定は記録 (カードと役の様子のファイル) から Tick ごとに決め直す。カードが増える・動く・役が起きると、次の Tick から毎回取る。
func (d *Dispatcher) listing(now time.Time) (list, quiet bool) {
	quiet = d.quiet()
	if !quiet || !d.listedQuiet || d.listedAt.IsZero() {
		return true, quiet
	}
	return now.Sub(d.listedAt) >= d.quietListEvery() || d.rolesMoved(), quiet
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
// 生きている役は、tellRole が一覧で入力待ち (人への知らせ) と落ちた時刻 (DeadSince。終了の stopRole が自動の再開を待つかをこれで決める) を
// 見張っているので、一覧を取り直す合図を持てる役 (watchable) だけを暇の妨げから外す (issue 576。559 の敵対的レビュー 2 周目で外さなかった理由)。
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
		if pm.Launching != "" || (pm.Session != "" && !pm.Stopped && !d.watchable(r, pm)) {
			return false
		}
	}
	return true
}

// jobMark は Claude Code が session ごとに書く JobsDir/<id>/state.json の様子。session の様子 (入力待ちに入る・抜ける) が変わると
// 書き換わる (issue 576 の観測。起動・権限の確認で止まる・自動の再開のたびに同じ秒に書かれた)。中身は読まない: 一覧の取り直しの合図にだけ使い、
// 様子の出典は claude agents のまま
type jobMark struct {
	exists bool
	mod    int64 // ns
	size   int64
}

func (d *Dispatcher) jobMark(id string) jobMark {
	if d.JobsDir == "" || id == "" || strings.ContainsAny(id, `/\`) {
		return jobMark{}
	}
	fi, err := os.Stat(filepath.Join(d.JobsDir, id, "state.json"))
	if err != nil {
		return jobMark{}
	}
	return jobMark{exists: true, mod: fi.ModTime().UnixNano(), size: fi.Size()}
}

// watchable は、生きている役 r を一覧なしで見張れるか: 前に一覧と照らしたとき記録の session が pid つきで生きていて、その直前に
// state.json の様子を控えてある。落ちた・再開待ち (DeadSince) の役は、照らしたとき生きていないので見張れない (落ちたのは state.json に出ず、自動の再開は一覧でしか分からない)
func (d *Dispatcher) watchable(r *role, pm store.PMState) bool {
	rr := d.roleRun(r)
	return rr.marked && rr.job == pm.Session // marked は、照らしたとき pid つきで生きていて state.json を控えられた役にだけ立つ (settleMarks)
}

// rolesMoved は、見張っている役の様子が前に一覧を取ったときから動いた合図があるか: state.json が書き換わった・消えた / pid が生きていない。
// 🚨 落ちたのは state.json に出ない (SIGKILL の後 約 12 秒、自動の再開まで何も書かれなかった。issue 576 の観測) ので pid を見る
func (d *Dispatcher) rolesMoved() bool {
	for _, r := range roles() {
		rr := d.roleRun(r)
		if !rr.marked {
			continue
		}
		if d.jobMark(rr.job) != rr.mark || !d.pidAlive(rr.pid) {
			return true
		}
	}
	return false
}

// pidAlive は pid のプロセスが居るか (Alive を差し替えられる)。EPERM (別のユーザーのプロセスが同じ pid を使っている) も居ないとみなす
// (役の session は自分のプロセス)。pid の再利用は見分けないので、落ちた後に別のプロセスがその pid を取ると、気づくのは次に一覧を取るとき (最長 QuietListEvery)
func (d *Dispatcher) pidAlive(pid int) bool {
	if d.Alive != nil {
		return d.Alive(pid)
	}
	return pid > 0 && syscall.Kill(pid, 0) == nil
}

// roleSnap は一覧を取る直前に控えた、役の見張りの材料 (markRoles)。
type roleSnap struct {
	job   string  // 前に照らした session の短い id (state.json を引く)
	mark  jobMark // 一覧を取る直前の state.json
	was   jobMark // 前に控えた様子 (was が意味を持つのは held のとき)
	held  bool    // 前の Tick から見張っていた (rr.marked)
	alive bool    // 前に照らしたとき生きていた
	seen  string  // 前に照らしたときの status
}

// markRoles は一覧を取る直前に、前に照らした session の state.json の様子を控える (取る前に控える: 取っている間の変化は
// 次の Tick の合図に残る)。
func (d *Dispatcher) markRoles() map[*roleRun]roleSnap {
	snaps := map[*roleRun]roleSnap{}
	for _, r := range roles() {
		if rr := d.roleRun(r); rr.job != "" {
			snaps[rr] = roleSnap{job: rr.job, mark: d.jobMark(rr.job), was: rr.mark, held: rr.marked, alive: rr.alive, seen: rr.seen}
		}
	}
	return snaps
}

// settleMarks は tellRole が照らした後に、控えた様子を照らした session のものとして持つ (照らした session が控えたときと違えば
// 持たない: 見張れないので次の Tick で取り直し、そのとき控え直す)。
// 控えた様子が前の Tick から変わらないまま status が変わっていたら、合図が鳴らない形 (Claude Code の版で書く場所が変わった?) なので
// 1 度だけ出来事にする: 見張っている間の知らせは最長 QuietListEvery 遅れる
func (d *Dispatcher) settleMarks(snaps map[*roleRun]roleSnap) []eventlog.Event {
	var notes []eventlog.Event
	for _, r := range roles() {
		rr := d.roleRun(r)
		s, ok := snaps[rr]
		same := ok && s.job == rr.job // 照らせなかった役は Tick が見張りから外す (dispatcher.go の Tick)
		if same && s.held && s.alive && rr.alive && s.mark == s.was && rr.seen != s.seen && d.jobMark(rr.job) == s.mark && !d.hintSuspected {
			d.hintSuspected = true
			notes = append(notes, ev(eventlog.KindSuspect, r.cardID, rr.job, r.name+" の status が "+s.seen+" → "+rr.seen+
				" に変わったのに state.json が変わっていない。一覧を間引く間の知らせが最長 "+d.quietListEvery().String()+" 遅れる (claude の版で書く場所が変わった?)"))
		}
		rr.mark, rr.marked = s.mark, same && s.mark.exists && rr.alive && rr.pid != 0 // 生きていない役は見張らない (pid 0 の見張りは毎 Tick 鳴る)
	}
	return notes
}
