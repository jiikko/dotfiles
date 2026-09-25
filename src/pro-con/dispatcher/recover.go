package dispatcher

// 起動時の確かめ (issue 483)。dispatcher が一覧を取れた最初の Tick で 1 度だけ、記録にある「動いているはずの session」
// (作業中のカードの PG・PM・取り込みの係) が居るかを調べ、マシンの再起動で消えたと示せるものだけを待たずに復旧する。
// 判定の表は 483 の本文。
//
// 🚨 判定を誤ると PG を二重に起こす・利用枠を焼く。示せないもの (起動時刻より後に始まった session が居ない・--all にも居ない・
// 起動時刻や --all を読めない) は復旧せず、出すだけにする (その後はいつもの経路 = 458 の restartWait に任せる)。
// 再起動で消えたと言える根拠は 2 つとも要る:
//   - session が始まったのがマシンの起動時刻より前 (プロセスは再起動を越えて生きられない)
//   - `claude agents --json --all` に pid 無し・止まった state (Session.Stopped) で出る (Claude Code が再開の途中ではない。
//     482 の実測: 再起動の後は 30 分たっても pid 無し・failed のまま)

import (
	"context"
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

// resumeAfterReboot は、マシンの再起動で止まった作業中の PG を再開するときに渡す文。
const resumeAfterReboot = "マシンの再起動 (クラッシュ) で PG の session が作業の途中で止まっていた。止まる前の続きから作業を再開して (規律は最初の指示のとおり)"

// startupNoteFor は起動時の確かめの要約を画面のヘッダーに出す長さ (dispatcher を起動してから)。
const startupNoteFor = 10 * time.Minute

// startupCheck は起動時の確かめの結果 (メモリと dispatcher-state.json に持つ)。
type startupCheck struct {
	at        time.Time
	recovered []string // 待たずに復旧した (カード ID か役の名前)
	reran     []string // テストの係に頼み直すカード (PG は結果が出てから再開する)
	alive     []string
	unsure    []string // 判定できないので待つ (いつもの経路に任せる)。「名前 (理由)」
}

// summary はヘッダーに出す短い 1 行。
func (s startupCheck) summary() string {
	n := len(s.recovered) + len(s.reran)
	if n == 0 && len(s.unsure) == 0 {
		return "起動時: 復旧は要らない"
	}
	return fmt.Sprintf("起動時: 復旧 %d / 判定できない %d (pro-con log)", n, len(s.unsure))
}

// recoverTarget は起動時に調べる 1 本 (作業中のカードの PG か役)。
type recoverTarget struct {
	name string // 出来事に書く名前 (C-001 / PM)
	row  live.Owned
}

// checkAtStart は起動時の確かめを 1 度だけ行う (ss は同じ Tick の一覧。取れなかった Tick では呼ばない)。
// 復旧は記録を書き換えるだけで、起動・再開は同じ Tick の割り当て (dispatch / tellRole) が行う。
func (d *Dispatcher) checkAtStart(ctx context.Context, now time.Time, ss []agents.Session) ([]eventlog.Event, error) {
	if d.started != nil {
		return nil, nil
	}
	st, err := store.Load(d.Dir)
	if err != nil {
		return nil, err
	}
	reg, err := live.LoadRegistry(filepath.Join(d.Dir, live.RegistryFile))
	if err != nil {
		return nil, err
	}
	var targets []recoverTarget
	for _, c := range st.Cards {
		if c.State != card.Running || c.Session == "" || c.Launching != "" { // 結果の分からない起動・再開は launchGrace の確かめに任せる
			continue
		}
		if o, ok := owned(c, reg); ok {
			targets = append(targets, recoverTarget{name: c.ID, row: o})
		}
	}
	roleStates := map[string]store.PMState{}
	for _, r := range roles() {
		pm, err := store.LoadRole(d.Dir, r.file, r.name)
		if err != nil { // 読めない役は tellRole が知らせる。ここでは判定の対象にしない
			continue
		}
		if pm.Session == "" || pm.Stopped || pm.Launching != "" { // 終了で止めた役は既に待たずに起こす
			continue
		}
		if o, ok := roleRow(reg, r.cardID); ok {
			roleStates[r.cardID] = pm
			targets = append(targets, recoverTarget{name: r.name, row: o})
		}
	}
	res := startupCheck{at: now}
	d.started = &res
	if len(targets) == 0 {
		return []eventlog.Event{ev(eventlog.KindRecover, "", "", "起動時の確かめ: 記録に動いているはずの session が無い (前回は止め処理を経て終わった)。復旧は要らない")}, nil
	}

	var dead []recoverTarget
	boot, bootErr := d.bootTime()
	all, allErr := d.listAllForCheck(ctx)
	for _, t := range targets {
		if liveSession(ss, t.row.SessionID) {
			res.alive = append(res.alive, t.name)
			continue
		}
		why := ""
		switch {
		case bootErr != nil:
			why = "マシンの起動時刻を読めない: " + bootErr.Error()
		case allErr != nil:
			why = "claude agents --all を読めない: " + allErr.Error()
		case t.row.StartedAt.IsZero():
			why = "起動の記録に session の開始時刻が無い"
		case !t.row.StartedAt.Before(boot):
			why = "マシンの再起動より後に始まった session。自動の再開の途中かもしれない"
		default:
			s, ok := findSession(all, t.row.SessionID)
			switch {
			case !ok:
				why = "--all の一覧にも居ない"
			case !s.Stopped():
				why = fmt.Sprintf("--all の一覧で pid %d・state %q (止まったと言えない)", s.PID, s.State)
			}
		}
		if why != "" {
			res.unsure = append(res.unsure, t.name+" ("+why+")")
			continue
		}
		dead = append(dead, t)
	}
	// 🚨 待たない理由は「消えた時刻 (DeadSince) = マシンの起動時刻」で与える (プロセスは遅くとも再起動で消えた)。終了の印 (Stopped) は使わない:
	// Stopped は落ちた回数・起こし直しの回数を数えない側で、PG・役がマシンを落としている形で、再起動のたびに上限なく起こし直す
	var asked []string
	fail := func(err error) ([]eventlog.Event, error) { // 途中で書けなくても、済んだものは出す (次の起動では対象から外れて見えなくなる)
		return []eventlog.Event{ev(eventlog.KindError, "", "", fmt.Sprintf("起動時の確かめの途中で記録を書けない (済んだもの: %s): %v", names(append(res.recovered, res.reran...)), err))}, err
	}
	for _, t := range dead {
		if r := roleFor(t.row.CardID); r != nil {
			pm := roleStates[r.cardID]
			if pm.DeadSince.IsZero() || boot.Before(pm.DeadSince) {
				pm.DeadSince = boot // 知らせる物があるときだけ tellRole が起こす (起こし直しの回数に数える)
			}
			if err := store.SaveRole(d.Dir, r.file, pm); err != nil {
				return fail(err)
			}
			res.recovered = append(res.recovered, t.name)
			continue
		}
		rerun, why := false, ""
		if err := d.update(t.row.CardID, func(cc *card.Card) {
			if cc.State != card.Running {
				return
			}
			if cc.DeadSince.IsZero() || boot.Before(cc.DeadSince) {
				cc.DeadSince = boot // prepare が自動の再開を待たない (起動の直後なら、起動から restartWait までは待つ)
			}
			if why = d.countCrash(cc, now, boot, "マシンの再起動で止まっていた"); why != "" { // 落ち続けるなら人の番へ (458 と同じ上限)
				return
			}
			cc.Revived = true // 再開で落ちた回数を数え直さない
			if cc.Run != "" { // テストの係の結果を待っている: 戻さない (tickRuns が頼み直し、結果を渡して再開する)
				rerun = true
				cc.History = append(cc.History, card.Event{At: now, Text: "起動時の確かめ: マシンの再起動で PG の session が止まっていた。テストの係の結果が出たら、待たずに再開する"})
				return
			}
			requeue(cc, now, resumeAfterReboot)
			cc.History = append(cc.History, card.Event{At: now, Text: "起動時の確かめ: マシンの再起動で PG の session が止まっていた。分解済みへ戻し、待たずに同じ session を再開する"})
		}); err != nil {
			return fail(err)
		}
		switch {
		case why != "":
			asked = append(asked, t.name)
		case rerun:
			res.reran = append(res.reran, t.name)
		default:
			res.recovered = append(res.recovered, t.name)
		}
	}
	text := "起動時の確かめ: "
	if bootErr == nil && len(dead) > 0 {
		text += "マシンは " + boot.Format("01-02 15:04") + " に再起動した。"
	}
	text += "待たずに復旧した: " + names(res.recovered) + " / テストの係の結果を待って再開する: " + names(res.reran) +
		" / 生きている: " + names(res.alive) + " / 判定できないので待つ: " + names(res.unsure)
	if len(asked) > 0 {
		text += " / 落ち続けたので人の番へ: " + names(asked)
	}
	return []eventlog.Event{ev(eventlog.KindRecover, "", "", text)}, nil
}

// bootTime はマシンの起動時刻 (BootTime が nil なら読めない = 判定できない)。
func (d *Dispatcher) bootTime() (time.Time, error) {
	if d.BootTime == nil {
		return time.Time{}, fmt.Errorf("起動時刻を読む口が無い")
	}
	return d.BootTime()
}

// listAllForCheck は止めた session も出す一覧 (ListAll が nil なら読めない = 判定できない。List で代えない: failed は List に出ない)。
func (d *Dispatcher) listAllForCheck(ctx context.Context) ([]agents.Session, error) {
	if d.ListAll == nil {
		return nil, fmt.Errorf("--all の一覧を読む口が無い")
	}
	return d.ListAll(ctx)
}

// liveSession は一覧に同じ session id で pid ありで居るか。
func liveSession(ss []agents.Session, sessionID string) bool {
	for _, s := range ss {
		if s.SessionID == sessionID && s.PID != 0 {
			return true
		}
	}
	return false
}

func findSession(ss []agents.Session, sessionID string) (agents.Session, bool) {
	for _, s := range ss {
		if s.SessionID == sessionID {
			return s, true
		}
	}
	return agents.Session{}, false
}

func names(xs []string) string {
	if len(xs) == 0 {
		return "なし"
	}
	return strings.Join(xs, "・")
}

// startupNote はヘッダーに出す起動時の確かめの要約 (起動から startupNoteFor を過ぎたら空)。
func (d *Dispatcher) startupNote(now time.Time) (string, bool) {
	if d.started == nil || now.Sub(d.started.at) > startupNoteFor {
		return "", false
	}
	s := *d.started
	return s.summary(), len(s.recovered)+len(s.reran)+len(s.unsure) > 0
}
