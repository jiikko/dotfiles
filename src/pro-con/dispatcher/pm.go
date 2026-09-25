package dispatcher

// PM (人間の依頼を受けてカードを分ける Claude の session) を起こして、依頼の列に来たカードを知らせる (issue 437。設計と失敗モードの表は 437 の本文)。
// PM は 1 つ (415 の論点 6)。起こし方は PG の回答と同じ Launcher.Resume (止めてから --resume)。記録が無ければ起動する。
// 🚨 PM の session は起動の記録 (sessions.json) にカード ID PMCardID で載せる。終了 (Shutdown) の確かめはカードで絞らないので PM も止め、
// 閉じたカードの PG を止める側 (close.go) はカード ID で絞るので PM には当たらない。
// 🚨 --limit (同時に動かす PG の数) には数えない。枠で「新しく起動・再開しない」ときだけ PM も起こさない (理由は 437 の本文)。

import (
	"context"
	"fmt"
	"os"
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

// PMCardID は起動の記録で PM の行に付けるカード ID。カード ID (C-%03d) とは重ならない。
const PMCardID = "PM"

// pmReviveLimit は、生きていない PM を crash の窓 (defaultCrashWindow) の間に起こし直す回数の上限。
const pmReviveLimit = 3

// exists は PM の作業ディレクトリが在るか (Exists を差し替えられる。テストは本物のパスを持たない)。
func (d *Dispatcher) exists(dir string) bool {
	if d.Exists != nil {
		return d.Exists(dir)
	}
	_, err := os.Stat(dir)
	return err == nil
}

// pmRow は起動の記録にある今の PM の行。
func pmRow(reg []live.Owned) (live.Owned, bool) {
	for _, o := range reg {
		if o.CardID == PMCardID {
			return o, true
		}
	}
	return live.Owned{}, false
}

// pmWorktree は claude --bg -w <name> が PM の repo に作る worktree。
func (d *Dispatcher) pmWorktree(name string) string {
	return filepath.Join(d.PMRepo, ".claude", "worktrees", name)
}

// tellPM は依頼の列のカードを PM に知らせる。PM が居なければ起動し、居れば (idle になってから) 再開して知らせる。
// PMRepo が空なら何もしない (e2e モード・設定で PM の repo が見つからない)。
func (d *Dispatcher) tellPM(ctx context.Context, now time.Time, ss []agents.Session) ([]eventlog.Event, error) {
	if d.PMRepo == "" {
		return nil, nil
	}
	st, err := store.Load(d.Dir)
	if err != nil {
		return nil, err
	}
	pm, err := store.LoadPM(d.Dir)
	if err != nil {
		return nil, err
	}
	regPath := filepath.Join(d.Dir, live.RegistryFile)
	reg, err := live.LoadRegistry(regPath)
	if err != nil {
		return nil, err
	}
	var notes []eventlog.Event
	before := fmt.Sprint(pm)
	save := func() error {
		if fmt.Sprint(pm) == before {
			return nil
		}
		return store.SavePM(d.Dir, pm)
	}
	row, hasRow := pmRow(reg)
	// 前の Tick の起動・再開の結果が分からない (印が残っている): 一覧で確かめる。立っていれば取り込み、launchGrace の間は待つ
	if pm.Launching != "" {
		if id, ok := d.pmAdopt(pm, row, hasRow, ss); ok {
			notes = append(notes, ev(eventlog.KindLaunch, PMCardID, id, fmt.Sprintf("PM の%sを一覧で確かめた (%s)", pm.Launching, id)))
			settlePM(&pm, id)
		} else if now.Sub(pm.LaunchedAt) < launchGrace {
			return notes, save()
		} else {
			pm.Launching = "" // 待っても出なかった。起動・再開し直す (Telling は Told に入れない = また渡す)
		}
	}
	if pm.Session != "" {
		warn, err := d.registerPM(regPath, pm, row, hasRow, ss)
		notes = append(notes, warn...)
		if err != nil {
			return notes, err
		}
		if reg, err = live.LoadRegistry(regPath); err != nil {
			return notes, err
		}
		row, hasRow = pmRow(reg)
	}
	// 今の PM の session。記録があれば session id で、無ければ (起動の直後で pid がまだ出ない) 短い id で引く
	var cur agents.Session
	listed := false
	for _, s := range ss {
		if s.Kind != "background" || s.Stopped() {
			continue
		}
		if (hasRow && s.SessionID == row.SessionID) || (!hasRow && pm.Session != "" && s.ID == pm.Session) {
			cur, listed = s, true
		}
	}
	alive := listed && cur.PID != 0
	switch {
	case alive:
		pm.DeadSince = time.Time{}
	case (hasRow || pm.Session != "") && pm.DeadSince.IsZero():
		pm.DeadSince = now
	}
	var requested, untold []string
	told := map[string]bool{}
	for _, id := range pm.Told {
		told[id] = true
	}
	pm.Told = nil
	for _, c := range st.Cards {
		if c.State != card.Requested || c.Archived {
			continue
		}
		requested = append(requested, c.ID)
		if told[c.ID] {
			pm.Told = append(pm.Told, c.ID) // 列を離れたカードは外れる
		} else {
			untold = append(untold, c.ID)
		}
	}
	// 起こすのは、知らせていないカードがあるときと、PM が生きていないのに依頼の列にカードが残っているとき (知らせた後に落ちた・止めた)
	if len(untold) == 0 && (alive || len(requested) == 0) {
		return notes, save()
	}
	switch {
	case alive && cur.Status != "idle": // 止めて再開すると作業中の turn・権限の確認を殺す。idle になってから知らせる (知らない値も殺さない側に倒す)
		if w := cur.Status; w != "busy" && w != "waiting" && d.pmStatus != w {
			d.pmStatus = w
			notes = append(notes, ev(eventlog.KindSuspect, PMCardID, cur.ID, fmt.Sprintf("PM の status が %q (idle / busy / waiting のどれでもない)。idle になるまで知らせない (claude の版で値が変わった?)", w)))
		}
		return notes, save()
	case !alive && (hasRow || pm.Session != "") && !pm.Stopped && now.Sub(pm.DeadSince) < restartWait:
		// 落ちて自動の再開を待っている (pid 無しの working) / 一覧から消えた直後。待たずに再開すると 2 本立つ (終了で止めた PM は待たない)
		return notes, save()
	}
	// 落ち続ける PM を起こし直し続けて枠を焼かない: 生きていない PM を起こし直すのは crash の窓 (30 分) に pmReviveLimit 回まで
	pm.Revivals = slices.DeleteFunc(pm.Revivals, func(at time.Time) bool { return now.Sub(at) > defaultCrashWindow })
	reviving := !alive && (hasRow || pm.Session != "") && !pm.Stopped
	if reviving && len(pm.Revivals) >= pmReviveLimit {
		why := fmt.Sprintf("PM が %s の間に %d 回起こし直しても生きていない。窓が過ぎるまで起こさない (様子: pro-con の dispatcher.log / claude agents)", defaultCrashWindow, len(pm.Revivals))
		if d.pmHeld != why {
			d.pmHeld = why
			notes = append(notes, ev(eventlog.KindHold, PMCardID, pm.Session, why))
		}
		return notes, save()
	}
	if limit, why := d.capacity(now); limit == 0 {
		if d.pmHeld != why {
			d.pmHeld = why
			notes = append(notes, ev(eventlog.KindHold, PMCardID, "", "PM を起こさない ("+why+")"))
		}
		return notes, save()
	}
	d.pmHeld = ""
	how, name, run, err := d.preparePM(row, hasRow, cur, alive, now, st.Cards, untold, requested)
	if err != nil {
		if d.pmFailed != err.Error() { // 理由が変わらないまま Tick ごとにログを埋めない
			d.pmFailed = err.Error()
			notes = append(notes, ev(eventlog.KindLaunch, PMCardID, "", "PM を"+how+"できない: "+err.Error()))
		}
		return notes, save()
	}
	d.pmFailed = ""
	pm.Launching, pm.LaunchedAt, pm.Telling = how, now, requested
	if reviving {
		pm.Revivals = append(pm.Revivals, now)
	}
	if name != "" {
		pm.Name = name
	}
	if err := store.SavePM(d.Dir, pm); err != nil { // 印は claude を走らせる前に書く
		return notes, err
	}
	before = fmt.Sprint(pm)
	id, launchErr := run(ctx)
	if launchErr != nil {
		return append(notes, ev(eventlog.KindLaunch, PMCardID, "", fmt.Sprintf("PM の%sに失敗したと返った (立っているかもしれないので、一覧で確かめてから起こし直す): %v", how, launchErr))), nil
	}
	settlePM(&pm, id)
	notes = append(notes, ev(eventlog.KindLaunch, PMCardID, id, fmt.Sprintf("PM を%sして依頼の列のカードを知らせた (%s: %s)", how, id, strings.Join(requested, ", "))))
	return notes, save()
}

// settlePM は起動・再開が済んだ PM を今の PM にする (渡した最中のカードは知らせ済みになる)。
func settlePM(pm *store.PMState, id string) {
	for _, c := range pm.Telling {
		if !slices.Contains(pm.Told, c) {
			pm.Told = append(pm.Told, c)
		}
	}
	pm.Session, pm.Launching, pm.Telling, pm.DeadSince, pm.Stopped = id, "", nil, time.Time{}, false
}

// preparePM は起動・再開の前提を確かめて、実行する関数を返す。記録に PM の行があれば同じ session を再開し、無ければ起動する。
func (d *Dispatcher) preparePM(row live.Owned, hasRow bool, cur agents.Session, alive bool, now time.Time, cards []card.Card, untold, requested []string) (how, name string, run func(context.Context) (string, error), err error) {
	notice := pmNotice(cards, untold, requested)
	if hasRow && (alive || d.exists(row.Cwd)) {
		if row.Cwd == "" {
			return "再開", "", nil, fmt.Errorf("前の PM (%s) の作業ディレクトリが記録に無い (別の cwd で再開すると別の tree を書く)", row.ID)
		}
		stop := ""
		if alive {
			stop = cur.ID
		}
		return "再開", "", func(ctx context.Context) (string, error) {
			return d.Launch.Resume(ctx, stop, row.SessionID, row.Cwd, notice)
		}, nil
	}
	if alive {
		// 起動した PM が一覧に出ているのに記録に載せられない (最後の起動より前に始まっている等)。もう 1 本起動しない
		return "起動", "", nil, fmt.Errorf("起動した PM (%s) を記録に載せられていない", cur.ID)
	}
	// 記録が無い / 前の PM の worktree が消えた (消えた cwd へは再開できない): 新しい worktree で起動する。前の PM の行は、新しい PM を記録に載せるときに退く
	name = "pc-pm-" + now.Format("20060102-150405")
	prompt := "あなたは pro-con の PM です。次の指示書に従う (`pro-con card guide` でいつでも読み直せる)。\n\n" + d.PMGuide + "\n---\n\n" + notice
	return "起動", name, func(ctx context.Context) (string, error) { return d.Launch.Start(ctx, d.PMRepo, name, prompt) }, nil
}

// pmNotice は PM に渡す知らせ。新しいカードを ID・題・repo で並べ、まだ依頼の列にある他のカードは ID だけ添える。指示は書かない (指示の正本は pm-guide.md)。
func pmNotice(cards []card.Card, untold, requested []string) string {
	var b strings.Builder
	b.WriteString("pro-con: 依頼の列に次のカードがある。指示書のとおりに扱って。\n")
	byID := map[string]card.Card{}
	for _, c := range cards {
		byID[c.ID] = c
	}
	for _, id := range untold {
		c := byID[id]
		fmt.Fprintf(&b, "- 新しい依頼 %s「%s」(repo: %s)\n", c.ID, c.Title, orNone(c.Repo))
	}
	var rest []string
	for _, id := range requested {
		if !slices.Contains(untold, id) {
			rest = append(rest, id)
		}
	}
	if len(rest) > 0 {
		fmt.Fprintf(&b, "- まだ依頼の列に残っている: %s\n", strings.Join(rest, ", "))
	}
	b.WriteString("中身は `pro-con card show <カード>` で読む。\n")
	return b.String()
}

func orNone(s string) string {
	if s == "" {
		return "指定なし"
	}
	return s
}

// pmAdopt は結果の分からない起動・再開の PM が一覧に出ているかを見る。印を書いた後 (LaunchedAt 以降) に始まったものだけ:
// 起動は名前と cwd (PM の worktree そのもの)、再開は前の session と同じ session id か同じ作業ディレクトリ (再開は別の session id を立てる = 427 の 3f)。
func (d *Dispatcher) pmAdopt(pm store.PMState, row live.Owned, hasRow bool, ss []agents.Session) (string, bool) {
	for _, s := range ss {
		if s.ID == "" || s.Kind != "background" || s.Started().Before(pm.LaunchedAt) {
			continue
		}
		if pm.Launching == "起動" && pm.Name != "" && s.Name == pm.Name && samePath(s.Cwd, d.pmWorktree(pm.Name)) {
			return s.ID, true
		}
		if pm.Launching == "再開" && hasRow && (s.SessionID == row.SessionID || (strings.Contains(row.Cwd, worktreeMarker) && s.Cwd == row.Cwd)) {
			return s.ID, true
		}
	}
	return "", false
}

// registerPM は今の PM (短い id) が一覧に session id と pid 付きで出ていれば、起動の記録に PMCardID で載せる。
//   - 記録と同じ session: pid が変わった (Claude Code の自動の再開) / cwd が空なら書き直す。cwd は記録を優先する (落ちている間の一覧の cwd は repo root)
//   - 別の session: 最後の起動・再開の後に始まったものだけ、PM の行を置き換える (前の PM は sessions-retired.json へ退き、終了で止める対象に残る)
func (d *Dispatcher) registerPM(regPath string, pm store.PMState, row live.Owned, hasRow bool, ss []agents.Session) ([]eventlog.Event, error) {
	for _, s := range ss {
		if s.ID != pm.Session || s.Kind != "background" || s.SessionID == "" || s.PID == 0 {
			continue
		}
		o := live.Owned{SessionID: s.SessionID, ID: s.ID, PID: s.PID, CardID: PMCardID, StartedAt: s.Started(), Cwd: s.Cwd}
		switch {
		case hasRow && row.SessionID == s.SessionID:
			if row.PID == s.PID && row.ID == s.ID && row.Cwd != "" {
				return nil, nil
			}
			o.StartedAt, o.Cwd = row.StartedAt, firstNonEmpty(row.Cwd, s.Cwd)
			return nil, live.Register(regPath, o)
		case s.Started().Before(pm.LaunchedAt):
			return []eventlog.Event{ev(eventlog.KindSuspect, PMCardID, s.ID, fmt.Sprintf("PM の短い id %s の session は pro-con の最後の起動・再開より前に始まっている (同じ短い id の別の session の疑い)。記録に載せない", s.ID))}, nil
		}
		return nil, live.ReplaceCard(regPath, o)
	}
	return nil, nil
}

// stopPM は終了のときに PM を止める (Shutdown の stopCards の周から呼ぶ)。wait が真なら、今は止められないが待てば止められる形 (起動・再開の直後・
// 自動の再開の途中)。止めようとした短い id は tried に入れる (記録にまだ無い PM も、後の ensureStopped で確かめる)。
func (d *Dispatcher) stopPM(ctx context.Context, now time.Time, ss []agents.Session, reg []live.Owned, last bool, tried map[string]string, notes *[]eventlog.Event) (wait bool) {
	pm, err := store.LoadPM(d.Dir)
	if err != nil {
		*notes = append(*notes, ev(eventlog.KindError, PMCardID, "", "PM の様子を読めない (記録にある PM は後の確かめで止める): "+err.Error()))
		return false
	}
	if pm.Launching == "" && pm.Session == "" {
		return false // 1 度も起こしていない
	}
	row, hasRow := pmRow(reg)
	var targets []string
	adopted := ""
	if pm.Launching != "" {
		if id, ok := d.pmAdopt(pm, row, hasRow, ss); ok {
			adopted = id
			targets = append(targets, id)
		} else {
			wait = now.Sub(pm.LaunchedAt) < launchGrace // 印の直後ならまだ一覧に出ていないだけかもしれない
		}
	}
	for _, s := range ss {
		if s.Kind != "background" || s.Stopped() || slices.Contains(targets, s.ID) {
			continue
		}
		// 記録の session (pid 無し = 自動の再開の途中でも止める。claude stop が再開を抑える) と、記録に載る前の今の PM (再開・起動が返った直後。
		// 記録の行はまだ前の PM を指している)。今の PM は最後の起動・再開の後に始まったものだけ (同じ短い id の別の session を止めない)
		if (hasRow && s.SessionID == row.SessionID) || (pm.Session != "" && s.ID == pm.Session && !s.Started().Before(pm.LaunchedAt)) {
			targets = append(targets, s.ID)
		}
	}
	if len(targets) == 0 && pm.Launching == "" {
		wait = !pm.Stopped && !pm.DeadSince.IsZero() && now.Sub(pm.DeadSince) < restartWait
	}
	if wait && !last {
		return true
	}
	if wait && len(targets) == 0 {
		*notes = append(*notes, ev(eventlog.KindStop, PMCardID, pm.Session, "PM は落ちて戻らない / 一覧に出ないので止められない (記録にある PM は後の確かめで止める)"))
		return false
	}
	for _, target := range targets {
		tried[target] = PMCardID
		sctx, cancel := context.WithTimeout(ctx, stopCallTimeout)
		err := d.Launch.Stop(sctx, target)
		cancel()
		if err != nil {
			*notes = append(*notes, ev(eventlog.KindStop, PMCardID, target, fmt.Sprintf("PM (%s) を止められない: %v", target, err)))
			return false
		}
		*notes = append(*notes, ev(eventlog.KindStop, PMCardID, target, fmt.Sprintf("PM (%s) を止めた", target)))
	}
	if adopted != "" {
		settlePM(&pm, adopted) // 取り込んだ起動・再開は今の PM にする (記録に載るのは次の起動の Tick)
		pm.Told = nil          // 止めたので知らせは届いていないかもしれない。次は依頼の列を全部知らせる
	}
	pm.Stopped = true
	if err := store.SavePM(d.Dir, pm); err != nil {
		*notes = append(*notes, ev(eventlog.KindError, PMCardID, "", "PM の様子を書き直せない (PM は止めた): "+err.Error()))
	}
	return false
}
