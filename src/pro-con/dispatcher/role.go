package dispatcher

// dispatcher が起こして知らせる Claude の session の役 (PM = 437 / 取り込みの係 = 487)。起こし方・起こし直し・枠での保留・終了で止める
// までを 1 つの状態機械で回し、役は知らせる物 (key)・知らせの文 (notice)・指示書 (guide) と記録の置き場だけを持つ
// (同じ状態機械を役ごとに書くと、片方だけ直す形が必ず出る。失敗モードの表は 437 の本文)。
// 🚨 役の session は起動の記録 (sessions.json) に役のカード ID (r.cardID) で載せる。終了 (Shutdown) の確かめはカードで絞らないので役も止め、
// 閉じたカードの PG を止める側 (close.go) はカード ID で絞るので役には当たらない。
// 🚨 --limit (同時に動かす PG の数) には数えない。枠で「新しく起動・再開しない」ときだけ役も起こさない (理由は 437 の本文)。

import (
	"context"
	"errors"
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

// role は dispatcher が起こして知らせる session の役。
type role struct {
	cardID string // 起動の記録の行に付けるカード ID (C-%03d と重ならない)
	name   string // 出来事の文の主語
	file   string // 様子のファイル (状態の置き場の下。中身は store.PMState)
	prefix string // 起動する worktree と session の名前の頭
	intro  string // 起動のときに指示書の前に置く文
	told   string // 起動・再開の出来事の結び (何を知らせたか)
	guide  func(d *Dispatcher) string
	off    func(d *Dispatcher) bool
	key    func(c card.Card) (string, bool) // 知らせる物の鍵。偽なら知らせない
	notice func(d *Dispatcher, cards []card.Card, untold, pending []string) string
	labels func(keys []string) string // 出来事に書く、知らせた物の短い名前
}

// roles は dispatcher が回す役 (この順に Tick で知らせ、終了で止める)。
func roles() []*role { return []*role{pmRole, integratorRole} }

// roleFor は起動の記録のカード ID から役を引く (役でなければ nil)。
func roleFor(cardID string) *role {
	for _, r := range roles() {
		if r.cardID == cardID {
			return r
		}
	}
	return nil
}

// roleRun は役ごとの、dispatcher のメモリだけに持つ様子 (出来事を変わったときだけ書くための前の値と、受け付けられなかった回数)。
type roleRun struct {
	held    string // 枠などで起こさない理由
	failed  string // 起こせない理由
	status  string // 知らない session の status
	waiting bool   // 入力待ち (status waiting) を出来事にした (抜けた・生きていない Tick で外す)
	rejects int    // claude が起動・再開を受け付けなかったのが続いた回数 (launchRejectLimit 回で起こさない)
	// rejected は最後に受け付けなかった失敗
	rejected string
	// 画面に出す様子 (roleState)。どれも最後の tellRole の値で、held / failed (出来事を重ねないための前の値) と違い毎回決め直す
	fresh    bool   // この Tick に一覧と照らした (Tick の頭で下ろす。照らせなかった Tick の alive / seen は前の Tick のもの)
	alive    bool   // 今の session が生きている (一覧は起動・再開の前に取ったもの)
	seen     string // 今の session の status
	launched bool   // この Tick に起動・再開した (alive / seen は起動・再開の前の session のもの)
	blocked  string // この Tick に起こさなかった・起こせなかった理由
}

func (d *Dispatcher) roleRun(r *role) *roleRun {
	if d.runs == nil {
		d.runs = map[string]*roleRun{}
	}
	if d.runs[r.cardID] == nil {
		d.runs[r.cardID] = &roleRun{}
	}
	return d.runs[r.cardID]
}

// roleMax は同時に動かす役の数の上限 (PM も取り込みの係も 1 つ。415 の論点 6。数を変えられるようにするのは 456 の後)。
const roleMax = 1

// roleState は画面と card list に出す役 r の様子 (issue 476。writeState が Tick ごとに書く)。
// 最後の tellRole の値と、役の様子のファイル (起動の印・知らせ済みのカード) から決める。
// 起動・再開してから launchGrace の間は、一覧にまだ出ない / pid が無いのを「落ちた」と出さない (起動の直後によくある形)。
func (d *Dispatcher) roleState(r *role, now time.Time) card.RoleState {
	s := card.RoleState{Name: r.name, Max: roleMax}
	if d.PMRepo == "" || r.off(d) {
		s.Phase = card.RoleOff
		return s
	}
	pm, err := store.LoadRole(d.Dir, r.file, r.name)
	if err != nil {
		s.Phase, s.Why = card.RoleBroken, err.Error()
		return s
	}
	rr := d.roleRun(r)
	s.Session, s.Cards, s.Why = pm.Session, keyCards(append(slices.Clone(pm.Telling), pm.Told...)), rr.blocked
	switch {
	case pm.Launching != "" || rr.launched:
		s.Phase = card.RoleLaunch
	case !rr.fresh: // 一覧を取れない・記録を読めない Tick は tellRole が照らす前に抜ける。前の Tick の様子を今のものとして出さない
		s.Phase = card.RoleChecking
		if s.Why == "" {
			s.Why = "この Tick は session の一覧と照らせていない (dispatcher.log)"
		}
	case rr.alive && rr.seen == agents.StatusIdle:
		s.Phase = card.RoleIdle
	case rr.alive && rr.seen == "waiting":
		s.Phase = card.RoleAsking
	case rr.alive: // busy と知らない status (dispatcher は知らない値も turn の途中として扱う)
		s.Phase = card.RoleBusy
	case rr.blocked != "":
		s.Phase = card.RoleBlocked
	case pm.Session != "" && now.Sub(pm.LaunchedAt) < launchGrace:
		s.Phase = card.RoleLaunch
	case pm.Stopped:
		s.Phase = card.RoleStopped
	case !pm.DeadSince.IsZero():
		s.Phase = card.RoleDead
	default:
		s.Phase = card.RoleNone
	}
	return s
}

// keyCards は知らせる物の鍵 (r.key: 「C-001」か「C-001@時刻」) のカード ID を、重ねずに並びのまま返す。
func keyCards(keys []string) []string {
	var out []string
	for _, k := range keys {
		id, _, _ := strings.Cut(k, "@")
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// exists は PM の作業ディレクトリが在るか (Exists を差し替えられる。テストは本物のパスを持たない)。
func (d *Dispatcher) exists(dir string) bool {
	if d.Exists != nil {
		return d.Exists(dir)
	}
	_, err := os.Stat(dir)
	return err == nil
}

// pmRow は起動の記録にある今の PM の行。
func roleRow(reg []live.Owned, cardID string) (live.Owned, bool) {
	for _, o := range reg {
		if o.CardID == cardID {
			return o, true
		}
	}
	return live.Owned{}, false
}

// pmWorktree は claude --bg -w <name> が PM の repo に作る worktree。
func (d *Dispatcher) roleWorktree(name string) string {
	return filepath.Join(d.PMRepo, ".claude", "worktrees", name)
}

// roles は起こさない役 (画面・card list・知らせが人の番 = card.Turn を決めるのに使う)。PM の repo が空なら PM も取り込みの係も起こさない
// (tellRole)。e2e モードの偽の PM (FakePM) は依頼を分けるだけなので数えない (依頼は次の Tick の頭で分解済みになる)。
func (d *Dispatcher) roles() card.Roles {
	return card.Roles{PMOff: d.PMRepo == "" || d.PMOff, IntegratorOff: d.PMRepo == "" || d.IntegratorOff}
}

// tellRole は役 r に知らせる物 (r.key) を知らせる。居なければ起動し、居れば (idle になってから) 再開して知らせる。
// PMRepo が空 (e2e モード・設定で PM の repo が見つからない) か r.off なら何もしない。
func (d *Dispatcher) tellRole(ctx context.Context, now time.Time, ss []agents.Session, r *role) ([]eventlog.Event, error) {
	rr := d.roleRun(r)
	rr.blocked, rr.launched = "", false
	if d.PMRepo == "" || r.off(d) {
		return nil, nil
	}
	st, err := store.Load(d.Dir)
	if err != nil {
		return nil, err
	}
	pm, err := store.LoadRole(d.Dir, r.file, r.name)
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
		return store.SaveRole(d.Dir, r.file, pm)
	}
	row, hasRow := roleRow(reg, r.cardID)
	// 前の Tick の起動・再開の結果が分からない (印が残っている): 一覧で確かめる。立っていれば取り込み、launchGrace の間は待つ
	if pm.Launching != "" {
		if id, ok := d.adopt(pm, row, hasRow, ss); ok {
			notes = append(notes, ev(eventlog.KindLaunch, r.cardID, id, fmt.Sprintf(r.name+" の%sを一覧で確かめた (%s)", pm.Launching, id)))
			settleRole(&pm, id)
			rr.rejects = 0
		} else if now.Sub(pm.LaunchedAt) < launchGrace {
			return notes, save()
		} else {
			pm.Launching = "" // 待っても出なかった。起動・再開し直す (Telling は Told に入れない = また渡す)
		}
	}
	if pm.Session != "" {
		warn, err := d.registerRole(r, regPath, pm, row, hasRow, ss)
		notes = append(notes, warn...)
		if err != nil {
			return notes, err
		}
		if reg, err = live.LoadRegistry(regPath); err != nil {
			return notes, err
		}
		row, hasRow = roleRow(reg, r.cardID)
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
	rr.fresh, rr.alive, rr.seen = true, alive, cur.Status
	switch {
	case alive:
		pm.DeadSince = time.Time{}
		// 権限の確認か質問で止まっている。人が attach して答えるまで動かないので、入るたびに 1 度だけ出来事にする (487)。
		// 知らせる物の有無で決めない (知らせる物が無いときに止まるのが一番よくある形: 手元のカードの push の確認)
		switch waiting := cur.Status == "waiting"; {
		case waiting && !rr.waiting:
			rr.waiting = true
			notes = append(notes, ev(eventlog.KindHold, r.cardID, cur.ID, r.name+" が入力待ち (権限の確認か質問) で止まっている。pro-con ps で session を見て attach して答える"))
		case !waiting:
			rr.waiting = false
		}
	default:
		// 生きていない (落ちた・止めた): 外す。起こし直した session が busy を見せずに同じ確認で止まっても出す (別の session が現れるのは
		// idle での再開か落ちた後だけで、どちらも印を外す Tick を通る)
		rr.waiting = false
		if (hasRow || pm.Session != "") && pm.DeadSince.IsZero() {
			pm.DeadSince = now
		}
	}
	var pending, untold []string // 知らせる物の鍵 (r.key)
	told := map[string]bool{}
	for _, k := range pm.Told {
		told[k] = true
	}
	pm.Told = nil
	for _, c := range card.Board(st.Cards) { // 上ほど優先 (人がレーンで並べ替えられる。issue 470)。役は上から扱う
		k, ok := r.key(c)
		if !ok {
			continue
		}
		pending = append(pending, k)
		if told[k] {
			pm.Told = append(pm.Told, k) // 列を離れたカードは外れる
		} else {
			untold = append(untold, k)
		}
	}
	// 起こすのは、知らせていない物があるときと、PM が生きていないのに知らせる物が残っているとき (知らせた後に落ちた・止めた)
	if len(untold) == 0 && (alive || len(pending) == 0) {
		return notes, save()
	}
	switch {
	case alive && cur.Status != "idle": // 止めて再開すると作業中の turn・権限の確認を殺す。idle になってから知らせる (知らない値も殺さない側に倒す)
		if w := cur.Status; w != "busy" && w != "waiting" && rr.status != w {
			rr.status = w
			notes = append(notes, ev(eventlog.KindSuspect, r.cardID, cur.ID, fmt.Sprintf(r.name+" の status が %q (idle / busy / waiting のどれでもない)。idle になるまで知らせない (claude の版で値が変わった?)", w)))
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
		why := fmt.Sprintf(r.name+" が %s の間に %d 回起こし直しても生きていない。窓が過ぎるまで起こさない (様子: pro-con の dispatcher.log / claude agents)", defaultCrashWindow, len(pm.Revivals))
		rr.blocked = why
		if rr.held != why {
			rr.held = why
			notes = append(notes, ev(eventlog.KindHold, r.cardID, pm.Session, why))
		}
		return notes, save()
	}
	// 受け付けられない起動・再開 (trust していない repo・未ログイン・古い claude) を繰り返さない。起こし直しの上限は、1 度も起動できていない PM と
	// 終了で止めた PM には当たらない (462 の PM 版)
	if rr.rejects >= launchRejectLimit {
		why := fmt.Sprintf(r.name+" の起動・再開を claude が %d 回続けて受け付けなかったので、起こさない (最後: %s)。直してから dispatcher を起動し直すと、もう一度起こす", rr.rejects, rr.rejected)
		rr.blocked = why
		if rr.held != why {
			rr.held = why
			notes = append(notes, ev(eventlog.KindHold, r.cardID, pm.Session, why))
		}
		return notes, save()
	}
	if limit, why := d.capacity(now); limit == 0 {
		rr.blocked = why
		if rr.held != why {
			rr.held = why
			notes = append(notes, ev(eventlog.KindHold, r.cardID, "", r.name+" を起こさない ("+why+")"))
		}
		return notes, save()
	}
	rr.held = ""
	how, name, run, err := d.prepareRole(r, row, hasRow, cur, alive, now, st.Cards, untold, pending)
	if err != nil {
		rr.blocked = how + "できない: " + err.Error()
		if rr.failed != err.Error() { // 理由が変わらないまま Tick ごとにログを埋めない
			rr.failed = err.Error()
			notes = append(notes, ev(eventlog.KindLaunch, r.cardID, "", r.name+" を"+how+"できない: "+err.Error()))
		}
		return notes, save()
	}
	rr.failed = ""
	pm.Launching, pm.LaunchedAt, pm.Telling = how, now, pending
	if reviving {
		pm.Revivals = append(pm.Revivals, now)
	}
	if name != "" {
		pm.Name = name
	}
	if err := store.SaveRole(d.Dir, r.file, pm); err != nil { // 印は claude を走らせる前に書く
		return notes, err
	}
	before = fmt.Sprint(pm)
	id, launchErr := run(ctx)
	if errors.Is(launchErr, ErrRejected) {
		rr.rejects++
		rr.rejected = launchErr.Error()
		if rr.rejects >= launchRejectLimit {
			pm.Launching = "" // 何も立っていない (印を残すと、終了が一覧に出るのを待ち続ける)
		}
		return append(notes, ev(eventlog.KindLaunch, r.cardID, "", fmt.Sprintf(r.name+" の%sを claude が受け付けなかった (%d 回目): %v", how, rr.rejects, launchErr))), save()
	}
	rr.rejects = 0 // 続いていない
	if launchErr != nil {
		return append(notes, ev(eventlog.KindLaunch, r.cardID, "", fmt.Sprintf(r.name+" の%sに失敗したと返った (立っているかもしれないので、一覧で確かめてから起こし直す): %v", how, launchErr))), nil
	}
	settleRole(&pm, id)
	rr.launched = true
	notes = append(notes, ev(eventlog.KindLaunch, r.cardID, id, fmt.Sprintf(r.name+" を%sして"+r.told+" (%s: %s)", how, id, r.labels(pending))))
	return notes, save()
}

// settlePM は起動・再開が済んだ PM を今の PM にする (渡した最中のカードは知らせ済みになる)。
func settleRole(pm *store.PMState, id string) {
	for _, c := range pm.Telling {
		if !slices.Contains(pm.Told, c) {
			pm.Told = append(pm.Told, c)
		}
	}
	pm.Session, pm.Launching, pm.Telling, pm.DeadSince, pm.Stopped = id, "", nil, time.Time{}, false
}

// preparePM は起動・再開の前提を確かめて、実行する関数を返す。記録に PM の行があれば同じ session を再開し、無ければ起動する。
func (d *Dispatcher) prepareRole(r *role, row live.Owned, hasRow bool, cur agents.Session, alive bool, now time.Time, cards []card.Card, untold, pending []string) (how, name string, run func(context.Context) (string, error), err error) {
	notice := r.notice(d, cards, untold, pending)
	if hasRow && (alive || d.exists(row.Cwd)) {
		if row.Cwd == "" {
			return "再開", "", nil, fmt.Errorf("前の "+r.name+" (%s) の作業ディレクトリが記録に無い (別の cwd で再開すると別の tree を書く)", row.ID)
		}
		stop := ""
		if alive {
			stop = cur.ID
		}
		return "再開", "", func(ctx context.Context) (string, error) {
			// 名前は起動のときの -w / -n の名前で、worktree の名前と同じ (記録に名前の欄は無いので cwd から取る)
			return d.Launch.Resume(ctx, stop, row.SessionID, row.Cwd, filepath.Base(row.Cwd), notice)
		}, nil
	}
	if alive {
		// 起動した PM が一覧に出ているのに記録に載せられない (最後の起動より前に始まっている等)。もう 1 本起動しない
		return "起動", "", nil, fmt.Errorf("起動した "+r.name+" (%s) を記録に載せられていない", cur.ID)
	}
	// 記録が無い / 前の PM の worktree が消えた (消えた cwd へは再開できない): 新しい worktree で起動する。前の PM の行は、新しい PM を記録に載せるときに退く
	name = r.prefix + now.Format("20060102-150405")
	prompt := r.intro + "\n\n" + r.guide(d) + "\n---\n\n" + notice
	return "起動", name, func(ctx context.Context) (string, error) { return d.Launch.Start(ctx, d.PMRepo, name, prompt) }, nil
}

// pmAdopt は結果の分からない起動・再開の PM が一覧に出ているかを見る。印を書いた後 (LaunchedAt 以降) に始まったものだけ:
// 起動は名前と cwd (PM の worktree そのもの)、再開は前の session と同じ session id か同じ作業ディレクトリ (再開は別の session id を立てる = 427 の 3f)。
func (d *Dispatcher) adopt(pm store.PMState, row live.Owned, hasRow bool, ss []agents.Session) (string, bool) {
	for _, s := range ss {
		if s.ID == "" || s.Kind != "background" || s.Started().Before(pm.LaunchedAt) {
			continue
		}
		if pm.Launching == "起動" && pm.Name != "" && s.Name == pm.Name && samePath(s.Cwd, d.roleWorktree(pm.Name)) {
			return s.ID, true
		}
		if pm.Launching == "再開" && hasRow && (s.SessionID == row.SessionID || (strings.Contains(row.Cwd, worktreeMarker) && s.Cwd == row.Cwd)) {
			return s.ID, true
		}
	}
	return "", false
}

// registerPM は今の PM (短い id) が一覧に session id と pid 付きで出ていれば、起動の記録に r.cardID で載せる。
//   - 記録と同じ session: pid が変わった (Claude Code の自動の再開) / cwd が空なら書き直す。cwd は記録を優先する (落ちている間の一覧の cwd は repo root)
//   - 別の session: 最後の起動・再開の後に始まったものだけ、PM の行を置き換える (前の PM は sessions-retired.json へ退き、終了で止める対象に残る)
func (d *Dispatcher) registerRole(r *role, regPath string, pm store.PMState, row live.Owned, hasRow bool, ss []agents.Session) ([]eventlog.Event, error) {
	for _, s := range ss {
		if s.ID != pm.Session || s.Kind != "background" || s.SessionID == "" || s.PID == 0 {
			continue
		}
		o := live.Owned{SessionID: s.SessionID, ID: s.ID, PID: s.PID, CardID: r.cardID, StartedAt: s.Started(), Cwd: s.Cwd}
		switch {
		case hasRow && row.SessionID == s.SessionID:
			if row.PID == s.PID && row.ID == s.ID && row.Cwd != "" {
				return nil, nil
			}
			o.StartedAt, o.Cwd = row.StartedAt, firstNonEmpty(row.Cwd, s.Cwd)
			return nil, live.Register(regPath, o)
		case s.Started().Before(pm.LaunchedAt):
			return []eventlog.Event{ev(eventlog.KindSuspect, r.cardID, s.ID, fmt.Sprintf(r.name+" の短い id %s の session は pro-con の最後の起動・再開より前に始まっている (同じ短い id の別の session の疑い)。記録に載せない", s.ID))}, nil
		}
		return nil, live.ReplaceCard(regPath, o)
	}
	return nil, nil
}

// stopPM は終了のときに PM を止める (Shutdown の stopCards の周から呼ぶ)。wait が真なら、今は止められないが待てば止められる形 (起動・再開の直後・
// 自動の再開の途中)。止めようとした短い id は tried に入れる (記録にまだ無い PM も、後の ensureStopped で確かめる)。
func (d *Dispatcher) stopRole(r *role, ctx context.Context, now time.Time, ss []agents.Session, reg []live.Owned, last bool, tried map[string]string, notes *[]eventlog.Event) (wait bool) {
	pm, err := store.LoadRole(d.Dir, r.file, r.name)
	if err != nil {
		*notes = append(*notes, ev(eventlog.KindError, r.cardID, "", r.name+" の様子を読めない (記録にある "+r.name+" は後の確かめで止める): "+err.Error()))
		return false
	}
	if pm.Launching == "" && pm.Session == "" {
		return false // 1 度も起こしていない
	}
	row, hasRow := roleRow(reg, r.cardID)
	var targets []string
	adopted := ""
	if pm.Launching != "" {
		if id, ok := d.adopt(pm, row, hasRow, ss); ok {
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
			*notes = append(*notes, d.unknownStateNote(r.cardID, r.name, s)...)
		}
	}
	if len(targets) == 0 && pm.Launching == "" {
		wait = !pm.Stopped && !pm.DeadSince.IsZero() && now.Sub(pm.DeadSince) < restartWait
	}
	if wait && !last {
		return true
	}
	if wait && len(targets) == 0 {
		*notes = append(*notes, ev(eventlog.KindStop, r.cardID, pm.Session, r.name+" は落ちて戻らない / 一覧に出ないので止められない (記録にある "+r.name+" は後の確かめで止める)"))
		return false
	}
	for _, target := range targets {
		tried[target] = r.cardID
		sctx, cancel := context.WithTimeout(ctx, stopCallTimeout)
		err := d.Launch.Stop(sctx, target)
		cancel()
		if err != nil {
			*notes = append(*notes, ev(eventlog.KindStop, r.cardID, target, fmt.Sprintf(r.name+" (%s) を止められない: %v", target, err)))
			return false
		}
		*notes = append(*notes, ev(eventlog.KindStop, r.cardID, target, fmt.Sprintf(r.name+" (%s) を止めた", target)))
	}
	if adopted != "" {
		settleRole(&pm, adopted) // 取り込んだ起動・再開は今の PM にする (記録に載るのは次の起動の Tick)
		pm.Told = nil            // 止めたので知らせは届いていないかもしれない。次は依頼の列を全部知らせる
	}
	pm.Stopped = true
	if err := store.SaveRole(d.Dir, r.file, pm); err != nil {
		*notes = append(*notes, ev(eventlog.KindError, r.cardID, "", r.name+" の様子を書き直せない ("+r.name+" は止めた): "+err.Error()))
	}
	return false
}
