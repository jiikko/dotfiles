// Package daemon は本物のモードの dispatcher (issue 427 の段階 3c-1)。1 回の Tick で:
//
//  1. 受付の箱を記録へ適用する (store.Apply)
//  2. 起動した PG の session を pro-con の記録 (live.Register) に登録する (session id と pid が一覧に出てから)
//  3. 分解済みのカードに、上限まで PG を割り当てる。回答を受けたカード (Resume が有る) は同じ session を再開し、それ以外は新しく起動する
//
// 書き手は daemon だけ (426 の決定 1)。PG の起動と再開は Launcher に任せ、テストでは偽物に差し替える。
// 起動・再開の前に印を記録へ書き、結果は次の Tick で session の一覧と照らして確かめる (claude の「失敗」と実際が食い違う / 途中で落ちる)。
// 落ちた回数で止める・watchdog は 3c-2、常駐と排他は 3c-3。
package daemon

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pro-con/agents"
	"pro-con/card"
	"pro-con/live"
	"pro-con/store"
)

// Launcher は PG の session を起動・再開する口。
type Launcher interface {
	// Start は repoPath で PG を起動し、claude --bg が返す短い id を返す。name は worktree と session の名前
	Start(ctx context.Context, repoPath, name, prompt string) (id string, err error)
	// Resume は stopID の session を止めてから (空なら止めない) 同じ session を text を渡して再開し、claude --bg が返す短い id を返す
	// (実行中の session に --resume するとコピーが起動するため。415 論点 11。再開で短い id が変わるかは未実測なので、返った id を使う)
	// cwd は session の作業ディレクトリ (PG の worktree)。再開はそこで走らせる
	Resume(ctx context.Context, stopID, sessionID, cwd, text string) (newID string, err error)
	// Stop は session を止める (落ち続けた PG。426 の決定 4)
	Stop(ctx context.Context, id string) error
}

// 落ちた回数の既定 (426 の決定 4。動かしながら直す): CrashWindow の間に CrashLimit 回落ちたら止める。
const (
	defaultCrashLimit  = 2
	defaultCrashWindow = 30 * time.Minute
)

// launchGrace は起動・再開の結果を確かめられないとき (claude が失敗と返した / daemon が途中で落ちた)、一覧に出るのを待つ長さ。
// 過ぎても出なければ起動し直す (待たずに起動し直すと、立っていた session の上にもう 1 本増える)。
const launchGrace = time.Minute

// restartWait は、前の session が一覧に無いときに再開を待つ長さ。プロセスが死んだ session は Claude Code が約 25 秒で自動で再開し
// (425 結果 1)、その間は一覧に出ないことがある。待たずに再開すると、自動の再開と重なって同じ session が 2 本立つ
const restartWait = time.Minute

// longToolLimit は、結果の返っていないツール呼び出しがある間 (長いコマンドの実行中) の停滞の閾値の下限。
// 本物のモードでは card.Exec (見込みの所要) を書く者がまだいないので、代わりにこれで延ばす
const longToolLimit = time.Hour

// errWait は、今は起動・再開せずに次の Tick を待つ (失敗ではないので履歴に書かない)。
var errWait = errors.New("待つ")

// Daemon は dispatcher の 1 つ。
type Daemon struct {
	Dir    string            // 本物のモードの状態の置き場 (記録・箱・pro-con が起動した session の記録)
	Limit  int               // 同時に動かす PG の上限
	Repos  map[string]string // repo の名前 → 絶対パス (カードの Repo から起動先を決める)
	Launch Launcher
	List   func(context.Context) ([]agents.Session, error)
	Now    func() time.Time
	// Transcript は session の transcript の末尾を読む (PG が落ちて Claude Code が自動で再開したかを見る)。nil なら読まない
	// (その場合、pid が変わった session はすべて外から操作された疑いとして扱う)
	Transcript func(sessionID string) (live.Transcript, error)
	// CrashLimit / CrashWindow は 0 なら既定値
	CrashLimit  int
	CrashWindow time.Duration
	// Runner はテストの係の実行 / Summarize は失敗したログの要約 (runner.go)。Runner が nil ならテストの係を動かさない
	Runner    Runner
	Summarize func(ctx context.Context, tail string) (string, error)
	active    *runJob // 実行中の 1 本 (無ければ nil)

	// Sleep は終了のときの待ち (Shutdown)。nil なら time.Sleep (テストで差し替える)
	Sleep func(time.Duration)
	// StallAfter は watchdog が「進捗なし」を停滞とみなすまでの通常の時間 (コマンドの実行中は card.StallThreshold が延ばす)。0 なら既定値
	StallAfter time.Duration
	// Publish は件数の文を tmux の status 用に書く / Notify は macOS の通知を出す (426 の決定 10)。nil なら知らせない
	Publish func(status string) error
	Notify  func(title, body string) error

	lastStatus  string          // 最後に Publish した文
	publishedAt time.Time       // 最後に Publish を試みた時刻
	published   bool            // 1 度でも Publish したか (起動の直後に空の文も書く。前の daemon が残した文を消す)
	notified    map[string]bool // 通知した回答待ちのカード (待ちを抜けたら消す)
}

// defaultStallAfter は停滞の通常の閾値の既定 (426: 既定値で始めて動かしながら直す)。
const defaultStallAfter = 15 * time.Minute

// Tick は 1 回ぶんの仕事をして、何をしたかの短い記録を返す (ログ用)。
// session の一覧を取れない Tick は、登録も割り当てもしない (一覧と照らさずに起動・再開すると、立っている session を見落として増やす /
// 別の session を止める)。
func (d *Daemon) Tick(ctx context.Context) ([]string, error) {
	now := d.Now()
	var notes []string
	res, err := store.Apply(d.Dir, now)
	if err != nil {
		return nil, err
	}
	for _, r := range res {
		if r.Err != "" {
			notes = append(notes, fmt.Sprintf("箱の依頼 %s (%s) を除けた: %s", r.ID, r.Kind, r.Err))
		}
	}
	ss, err := d.List(ctx)
	if err != nil {
		return append(notes, "session の一覧を取れない (登録と割り当ては次の Tick へ): "+err.Error()), nil
	}
	n, warn, err := d.register(now, ss)
	if err != nil {
		return notes, err
	}
	if n > 0 {
		notes = append(notes, fmt.Sprintf("PG の session を %d 本登録した", n))
	}
	notes = append(notes, warn...)
	if err := d.trackDead(now, ss); err != nil {
		return notes, err
	}
	stopped, err := d.stopCrashing(ctx, now, ss)
	notes = append(notes, stopped...)
	if err != nil {
		return notes, err
	}
	watched, err := d.watch(now)
	notes = append(notes, watched...)
	if err != nil {
		return notes, err
	}
	ran, err := d.tickRuns(ctx, now)
	notes = append(notes, ran...)
	if err != nil {
		return notes, err
	}
	more, err := d.dispatch(ctx, now, ss)
	notes = append(notes, more...)
	if err != nil {
		return notes, err
	}
	return append(notes, d.announce()...), nil // 割り当ての結果まで含めて知らせる
}

// register は作業中のカードの session (短い id) が一覧に出ていれば、session id と pid を添えて pro-con の記録に書く。
// 起動の直後は一覧にまだ出ないことがあるので、出るまで毎回見る。取り込むのは daemon が最後に起動・再開した (LaunchedAt) 後に
// 始まった session だけで、書き直せるのは記録の行がそれより前のもの (= daemon 自身の再開の後) だけ。それ以外は外から操作された疑いを知らせる。
// 判定の材料は記録に置く (daemon が起動し直しても失わない)。
func (d *Daemon) register(now time.Time, ss []agents.Session) (int, []string, error) {
	st, err := store.Load(d.Dir)
	if err != nil {
		return 0, nil, err
	}
	regPath := filepath.Join(d.Dir, live.RegistryFile)
	reg, err := live.LoadRegistry(regPath)
	if err != nil {
		return 0, nil, err
	}
	var warn []string
	known := map[string]live.Owned{}
	for _, o := range reg {
		known[o.CardID] = o
	}
	n := 0
	for _, c := range st.Cards {
		// 作業中に限らない: 質問待ちの間も PG のプロセスは生きていて、落ちれば Claude Code が自動で再開する (427 の 3f で実測)
		if c.State == card.Done || c.Session == "" {
			continue
		}
		for _, s := range ss {
			// pro-con が起動するのは bg の session だけ。対話の session (人間が PG の worktree で開いたもの等) は決して取り込まない
			if s.ID == c.Session && s.Kind != "background" {
				warn = append(warn, fmt.Sprintf("%s の短い id %s の session の kind が %q (background ではない)。取り込まない (claude の版で値が変わった?)", c.ID, s.ID, s.Kind))
				continue
			}
			if s.ID != c.Session || s.SessionID == "" || s.PID == 0 {
				continue
			}
			o, ok := known[c.ID]
			switch {
			case ok && o.SessionID == s.SessionID && o.PID == s.PID && (o.Cwd != "" || s.Cwd == ""):
				continue // 登録済み (cwd が記録に無く一覧に出ていれば書き直す。無いと再開できない)
			case ok && o.SessionID == s.SessionID && o.PID == s.PID:
				// 同じ session・同じプロセスで cwd だけ埋める (上の判定を通らない形なので、所有の判定には影響しない)
			case s.Started().Before(c.LaunchedAt):
				warn = append(warn, fmt.Sprintf("%s の session %s は pro-con の最後の起動・再開より前に始まっている (同じ短い id の別の session の疑い)。登録しない", c.ID, s.ID))
				continue
			case ok && o.SessionID != s.SessionID && o.ID != c.Session && o.StartedAt.Before(c.LaunchedAt):
				// daemon 自身の再開が返した新しい短い id の session (claude --bg --resume は別の session id の session を立てる)。
				// LaunchedAt 以降に始まっている (上の判定) ので、カードの行をこれに置き換える
				if err := live.ReplaceCard(regPath, live.Owned{SessionID: s.SessionID, ID: s.ID, PID: s.PID, CardID: c.ID, StartedAt: s.Started(), Cwd: s.Cwd}); err != nil {
					return n, warn, err
				}
				n++
				continue
			case ok && o.SessionID != s.SessionID:
				warn = append(warn, fmt.Sprintf("%s の短い id %s が別の session (%s) を指している。登録しない", c.ID, s.ID, s.SessionID))
				continue
			case ok && !o.StartedAt.Before(c.LaunchedAt):
				crashes := d.restartsSince(c, o, s.SessionID)
				if len(crashes) == 0 {
					warn = append(warn, fmt.Sprintf("%s の session %s の pid が %d → %d に変わった。pro-con は再開していない (外から操作された疑い)。記録は書き直さない",
						c.ID, s.ID, o.PID, s.PID))
					continue
				}
				// Claude Code 自身がプロセスの死から再開した (transcript に再開の文が新しく出た)。記録を書き直してから回数を数える
				// (逆の順だと、あいだで落ちたとき数えた文が since を進め、pid を二度と書き直せなくなる。この順なら 1 回数え漏れるだけ)
				// cwd は記録の方を優先する (落ちている間の一覧の cwd は repo root になる = 427 の 3f。repo root を記録すると cwd の照合が repo 全体に当たる)
				if err := live.Register(regPath, live.Owned{SessionID: s.SessionID, ID: s.ID, PID: s.PID, CardID: c.ID, StartedAt: s.Started(), Cwd: firstNonEmpty(o.Cwd, s.Cwd)}); err != nil {
					return n, warn, err
				}
				n++
				if err := d.update(c.ID, func(cc *card.Card) {
					cc.Crashes = append(cc.Crashes, crashes...)
					cc.History = append(cc.History, card.Event{At: now, Text: fmt.Sprintf("PG のプロセスが落ち、Claude Code が自動で再開した (pid %d → %d)", o.PID, s.PID)})
				}); err != nil {
					return n, warn, err
				}
				warn = append(warn, fmt.Sprintf("%s の PG が落ちて自動で再開した (pid %d → %d)", c.ID, o.PID, s.PID))
				continue
			}
			if err := live.Register(regPath, live.Owned{SessionID: s.SessionID, ID: s.ID, PID: s.PID, CardID: c.ID, StartedAt: s.Started(), Cwd: s.Cwd}); err != nil {
				return n, warn, err
			}
			n++
		}
	}
	return n, warn, nil
}

// trackDead は、PG の session を持つカードごとに「一覧に無い / pid 無し」を最初に見た時刻を DeadSince に書き、生きているのを見たら外す。
// 待ちの起点を「落ちた (のを見た) 時刻」にするため (直前の再開や回答の時刻から数えると、長く走ってから落ちた PG を待たずに扱う)
func (d *Daemon) trackDead(now time.Time, ss []agents.Session) error {
	st, err := store.Load(d.Dir)
	if err != nil {
		return err
	}
	reg, err := live.LoadRegistry(filepath.Join(d.Dir, live.RegistryFile))
	if err != nil {
		return err
	}
	for _, c := range st.Cards {
		if c.State == card.Done || c.Session == "" {
			continue
		}
		o, ok := owned(c, reg)
		if !ok {
			continue
		}
		alive := false
		for _, s := range ss {
			if s.SessionID == o.SessionID && s.PID != 0 {
				alive = true
			}
		}
		switch {
		case alive && !c.DeadSince.IsZero():
			if err := d.update(c.ID, func(cc *card.Card) { cc.DeadSince = time.Time{} }); err != nil {
				return err
			}
		case !alive && c.DeadSince.IsZero():
			if err := d.update(c.ID, func(cc *card.Card) { cc.DeadSince = now }); err != nil {
				return err
			}
		}
	}
	return nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// worktreeMarker は pro-con の PG の worktree のパスに入る部分 (claude --bg -w pc-<card> が <repo>/.claude/worktrees/pc-<card> に作る)。
// cwd で session を照らしてよいのはこの下だけ (repo root で照らすと、同じ repo の他の session に当たる)
const worktreeMarker = "/.claude/worktrees/pc-"

// restartsSince は、記録の行 (o) より後・最後に数えた落ちた時刻より後に、transcript へ出た再開の文の時刻を返す。
func (d *Daemon) restartsSince(c card.Card, o live.Owned, sessionID string) []time.Time {
	if d.Transcript == nil {
		return nil
	}
	t, err := d.Transcript(sessionID)
	if err != nil {
		return nil // 読めなければ数えない (外から操作された疑いとして知らせる側に倒す)
	}
	since := o.StartedAt
	if n := len(c.Crashes); n > 0 && c.Crashes[n-1].After(since) {
		since = c.Crashes[n-1]
	}
	var out []time.Time
	for _, at := range t.Restarts {
		if at.After(since) {
			out = append(out, at)
		}
	}
	return out
}

// stopCrashing は、作業中のカードのうち CrashWindow の間に CrashLimit 回以上落ちた PG を止め、カードを人間の回答待ちにする
// (回答が来たら同じ session を再開する)。止められなかったら作業中のまま残し、次の Tick でまた試す。
func (d *Daemon) stopCrashing(ctx context.Context, now time.Time, ss []agents.Session) ([]string, error) {
	limit, window := d.CrashLimit, d.CrashWindow
	if limit <= 0 {
		limit = defaultCrashLimit
	}
	if window <= 0 {
		window = defaultCrashWindow
	}
	st, err := store.Load(d.Dir)
	if err != nil {
		return nil, err
	}
	reg, err := live.LoadRegistry(filepath.Join(d.Dir, live.RegistryFile))
	if err != nil {
		return nil, err
	}
	var notes []string
	for _, c := range st.Cards {
		if c.State != card.Running || c.Session == "" {
			continue
		}
		recent := 0
		for _, at := range c.Crashes {
			// 最後の起動・再開より前の回数は数えない (止めた後に回答で再開した PG を、前の回数ですぐ止め直さない)
			if at.After(c.LaunchedAt) && now.Sub(at) <= window {
				recent++
			}
		}
		if recent < limit && !c.StopWanted {
			continue
		}
		// 止める前に、今の一覧で短い id が記録の session を指しているかを見る (別の session を止めない)。一覧に無ければ止めるものが無い
		o, hasOwned := owned(c, reg)
		target, listed := "", false
		for _, s := range ss {
			if s.ID != c.Session {
				continue
			}
			if hasOwned && s.SessionID == o.SessionID && s.PID == 0 {
				continue // 落ちて Claude Code の自動の再開を待っている (pid 無しで一覧に出る = 427 の 3f)。一覧に無いのと同じく待つ
			}
			listed = true
			if hasOwned && s.SessionID == o.SessionID && s.PID == o.PID { // pid が記録と違うものは止めない (登録の側と同じ基準)
				target = s.ID
			}
		}
		how := "止めた"
		switch {
		case target != "":
		case listed:
			how = "止めなかった (一覧の session が記録と一致しない。別の session か、外から操作された疑い)"
		default:
			// 一覧に無い: 死んで Claude Code の自動の再開を待っているのかもしれない (約 25 秒。425 結果 1)。落ちたのを最初に見た時刻 (DeadSince) から restartWait 待つ
			if c.DeadSince.IsZero() || now.Sub(c.DeadSince) < restartWait {
				continue
			}
			how = "止めなかった (session が一覧に無い)"
		}
		if target != "" {
			if err := d.Launch.Stop(ctx, target); err != nil {
				if !c.StopWanted {
					if err := d.update(c.ID, func(cc *card.Card) { cc.StopWanted = true }); err != nil {
						return notes, err
					}
				}
				notes = append(notes, fmt.Sprintf("%s の PG は落ち続けたが、止められない (次の Tick でまた試す): %v", c.ID, err))
				continue
			}
		}
		why := fmt.Sprintf("PG が %s の間に %d 回落ちたので%s。回答すると同じ session を再開する", window, max(recent, limit), how)
		if err := d.update(c.ID, func(cc *card.Card) {
			cc.DropRun() // 作業中の列を離れる (テストの係への頼みは取り下げる)
			cc.State, cc.Since, cc.Owner, cc.StopWanted = card.Waiting, now, "人間", false
			cc.Wait = card.Wait{Kind: card.WaitCrashed, Question: why}
			cc.History = append(cc.History, card.Event{At: now, Text: why})
		}); err != nil {
			return notes, err
		}
		notes = append(notes, c.ID+": "+why)
	}
	return notes, nil
}

// watch は作業中のカードの「実質的な進捗」を transcript から読み (それまでに無かった PG の出力が出たか)、止まっていれば停滞にする。
// 進捗が戻れば停滞を外す。正当な待ち (リソース・枠) は数えない。transcript を読めないカードは判定しない (読めないことを停滞にしない)。
// 🚨 見ているのは「新しい文が出たか」だけ。同じ文を繰り返すループは捕まえるが、毎回少しずつ違う文を出すループは進んでいるように見える
func (d *Daemon) watch(now time.Time) ([]string, error) {
	if d.Transcript == nil {
		return nil, nil
	}
	base := d.StallAfter
	if base <= 0 {
		base = defaultStallAfter
	}
	st, err := store.Load(d.Dir)
	if err != nil {
		return nil, err
	}
	reg, err := live.LoadRegistry(filepath.Join(d.Dir, live.RegistryFile))
	if err != nil {
		return nil, err
	}
	var notes []string
	for _, c := range st.Cards {
		if c.State != card.Running || c.Wait.Kind != card.WaitNone || c.Run != "" { // テストの係の実行を待っている間は PG の進み具合を見ない
			continue
		}
		o, ok := owned(c, reg)
		if !ok {
			continue // まだ登録していない (起動の直後)
		}
		t, err := d.Transcript(o.SessionID)
		if err != nil {
			continue
		}
		if t.LastNew.After(c.LastProgress) {
			if err := d.update(c.ID, func(cc *card.Card) {
				cc.LastProgress = t.LastNew
				if cc.Stalled {
					cc.Stalled = false
					cc.History = append(cc.History, card.Event{At: now, Text: "watchdog: 進捗が戻った"})
				}
			}); err != nil {
				return notes, err
			}
			continue
		}
		if c.Stalled {
			continue
		}
		since := c.LastProgress
		if c.Exec.Active() && c.Exec.Since.After(since) {
			since = c.Exec.Since
		}
		limit := card.StallThreshold(c, base)
		if !t.PendingSince.IsZero() { // 結果の返っていないツール呼び出しがある (長いコマンドの実行中)
			limit = max(limit, longToolLimit)
		}
		if now.Sub(since) < limit {
			continue
		}
		text := fmt.Sprintf("watchdog: %d 分進捗なし (新しい出力が無い)", int(limit/time.Minute))
		if err := d.update(c.ID, func(cc *card.Card) {
			cc.Stalled = true
			cc.History = append(cc.History, card.Event{At: now, Text: text})
		}); err != nil {
			return notes, err
		}
		notes = append(notes, c.ID+": "+text)
	}
	return notes, nil
}

// dispatch は分解済みのカードに、作業中が上限に達するまで PG を割り当てる (古い順)。
//   - 起動・再開の前に、印 (Launching) と時刻を記録に書く。結果が分かったら印を外して作業中にする
//   - 前の Tick の起動・再開の結果が分からないまま (印が残っている) のカードは、一覧で確かめる。立っていれば取り込み、
//     launchGrace を過ぎても出なければ起動し直す。待っている間は上限に数える
//   - 起動・再開の前提 (repo の場所 / 前の session の記録) が無いカードは、何も起動せずに履歴へ書く
func (d *Daemon) dispatch(ctx context.Context, now time.Time, ss []agents.Session) ([]string, error) {
	st, err := store.Load(d.Dir)
	if err != nil {
		return nil, err
	}
	running := 0
	var queue []card.Card
	for _, c := range st.Cards {
		switch c.State {
		case card.Running:
			running++
		case card.Planned:
			queue = append(queue, c)
		case card.Requested, card.Waiting, card.Review, card.Done:
		}
	}
	sort.SliceStable(queue, func(i, j int) bool { return queue[i].Since.Before(queue[j].Since) })
	reg, err := live.LoadRegistry(filepath.Join(d.Dir, live.RegistryFile))
	if err != nil {
		return nil, err
	}
	// 印の残ったカード (前の起動・再開の結果が分からない) は、上限の判定より先に片付ける。上限の後ろに置くと、実際に立っている PG を
	// 数えずに別のカードを起動する (上限を下げて起動し直したときも)
	var notes []string
	var fresh []card.Card
	for _, c := range queue {
		if c.Launching == "" {
			fresh = append(fresh, c)
			continue
		}
		if id, ok := adopt(c, d.Repos[c.Repo], ss, reg); ok {
			if err := d.settle(c.ID, now, c.Launching, id); err != nil {
				return notes, err
			}
			running++
			notes = append(notes, fmt.Sprintf("%s の PG の%sを一覧で確かめた (%s)", c.ID, c.Launching, id))
			continue
		}
		if now.Sub(c.LaunchedAt) < launchGrace {
			running++ // まだ一覧に出ていないだけかもしれない
			continue
		}
		fresh = append(fresh, c) // 待っても出なかった。起動・再開し直す (古い順は保つ)
	}
	for _, c := range fresh {
		if running >= d.Limit {
			break
		}
		how, run, err := d.prepare(c, now, ss, reg)
		if errors.Is(err, errWait) {
			continue
		}
		if err != nil {
			if err := d.noteOnce(c.ID, now, how+"できない: "+err.Error()); err != nil {
				return notes, err
			}
			notes = append(notes, fmt.Sprintf("%s の PG を%sできない: %v", c.ID, how, err))
			continue
		}
		if err := d.mark(c.ID, now, how); err != nil {
			return notes, err
		}
		running++ // 失敗と返っても立っているかもしれないので、確かめるまで上限に数える
		id, launchErr := run(ctx)
		if launchErr != nil {
			if err := d.note(c.ID, now, how+"に失敗したと返った (立っているかもしれないので、一覧で確かめてから起動し直す): "+launchErr.Error()); err != nil {
				return notes, err
			}
			notes = append(notes, fmt.Sprintf("%s の PG の%sに失敗: %v", c.ID, how, launchErr))
			continue
		}
		if err := d.settle(c.ID, now, how, id); err != nil {
			return notes, err
		}
		notes = append(notes, fmt.Sprintf("%s に PG を%sした (%s)", c.ID, how, id))
	}
	return notes, nil
}

// resumes は回答を受けて、同じ session を再開するカードか。
func resumes(c card.Card) bool { return c.Resume != "" && c.Session != "" }

// prepare は起動・再開の前提を確かめて、実行する関数を返す。再開は、前の session が pro-con の記録にあり、
// 今の一覧でその短い id が同じ session を指している (別の session を止めない) ときだけ。
func (d *Daemon) prepare(c card.Card, now time.Time, ss []agents.Session, reg []live.Owned) (string, func(context.Context) (string, error), error) {
	if resumes(c) {
		o, ok := owned(c, reg)
		if !ok {
			return "再開", nil, fmt.Errorf("前の session (%s) が pro-con の記録に無い", c.Session)
		}
		stop := "" // 一覧に無い session は止めない (無い id への stop が失敗すると、再開に届かないまま繰り返す)
		for _, s := range ss {
			if s.ID != c.Session {
				continue
			}
			if s.SessionID != o.SessionID {
				return "再開", nil, fmt.Errorf("短い id %s が今は別の session (%s) を指している", c.Session, s.SessionID)
			}
			if s.PID == 0 { // 落ちて自動の再開を待っている。一覧に無いのと同じく待ってから、止めずに再開する
				continue
			}
			if s.PID != o.PID { // 登録の側が「外から操作された疑い」として書き直さなかった形。止めも再開もしない
				return "再開", nil, fmt.Errorf("session %s の pid が記録 (%d) と違う (%d)。外から操作された疑い", c.Session, o.PID, s.PID)
			}
			stop = c.Session
		}
		if o.Cwd == "" {
			return "再開", nil, fmt.Errorf("前の session (%s) の作業ディレクトリが記録に無い (別の cwd で再開すると別の tree を書く)", c.Session)
		}
		if stop == "" && !c.Stopped && (c.DeadSince.IsZero() || now.Sub(c.DeadSince) < restartWait) { // 終了で止めたものは自動の再開を待たない
			return "再開", nil, errWait // Claude Code の自動の再開の途中かもしれない
		}
		return "再開", func(ctx context.Context) (string, error) {
			return d.Launch.Resume(ctx, stop, o.SessionID, o.Cwd, c.Resume)
		}, nil
	}
	path, ok := d.Repos[c.Repo]
	if !ok {
		return "起動", nil, fmt.Errorf("repo %q の場所が設定に無い", c.Repo)
	}
	return "起動", func(ctx context.Context) (string, error) { return d.Launch.Start(ctx, path, sessionName(c), Prompt(c)) }, nil
}

func sessionName(c card.Card) string { return "pc-" + strings.ToLower(c.ID) }

// worktreePath は claude --bg -w <name> が作る PG の worktree (427 の 3f で実測)。repo の場所が分からなければ空 (呼び出し側が空を弾く)。
func worktreePath(repoPath string, c card.Card) string {
	if repoPath == "" {
		return ""
	}
	return filepath.Join(repoPath, ".claude", "worktrees", sessionName(c))
}

// samePath は 2 つのパスが同じ場所か (symlink を解決して比べる。claude の一覧の cwd は解決済みのパスで出る見込みで、
// 設定の repo のパスは symlink を含みうる)。解決できなければ Clean した文字列で比べる。どちらかが空なら一致しない
func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	return resolve(a) == resolve(b)
}

func owned(c card.Card, reg []live.Owned) (live.Owned, bool) {
	for _, o := range reg {
		if o.CardID == c.ID && o.ID == c.Session {
			return o, true
		}
	}
	return live.Owned{}, false
}

// adopt は結果の分からない起動・再開の session が一覧に出ているかを見る。印を書いた後 (LaunchedAt 以降) に始まったものだけ:
// 起動はこのカードの session の名前、再開は前の session と同じ session id。
func adopt(c card.Card, repoPath string, ss []agents.Session, reg []live.Owned) (string, bool) {
	o, hasOwned := owned(c, reg)
	for _, s := range ss {
		if s.ID == "" || s.Kind != "background" || s.Started().Before(c.LaunchedAt) {
			continue
		}
		// 起動は、名前に加えて cwd がこのカードの repo の worktree そのもの (<repo>/.claude/worktrees/pc-<card>) のときだけ。
		// 名前だけだと、別の状態の置き場で動く daemon が同じカード ID で立てた PG に当たる (カード ID は置き場ごとに C-001 から振られる)
		if wt := worktreePath(repoPath, c); c.Launching == "起動" && wt != "" && s.Name == sessionName(c) && samePath(s.Cwd, wt) {
			return s.ID, true
		}
		// 再開は別の session id の session を立てる (427 の 3f で実測) ので、同じ作業ディレクトリ (PG の worktree) で印の後に始まったものも取り込む
		if c.Launching == "再開" && hasOwned && (s.SessionID == o.SessionID || (strings.Contains(o.Cwd, worktreeMarker) && s.Cwd == o.Cwd)) {
			return s.ID, true
		}
	}
	return "", false
}

// mark は起動・再開を始める印を記録に書く (結果を待つ前に)。
func (d *Daemon) mark(id string, now time.Time, how string) error {
	return d.update(id, func(c *card.Card) { c.Launching, c.LaunchedAt = how, now })
}

// settle は起動・再開が済んだカードを作業中にする。
func (d *Daemon) settle(id string, now time.Time, how, session string) error {
	return d.update(id, func(c *card.Card) {
		c.State, c.Since, c.Owner, c.Session = card.Running, now, "PG", session
		c.LastProgress, c.Resume, c.Launching, c.Stalled, c.StopWanted, c.Stopped = now, "", "", false, false, false
		c.History = append(c.History, card.Event{At: now, Text: "PG を" + how + "した (session " + session + ")"})
	})
}

func (d *Daemon) note(id string, now time.Time, text string) error {
	return d.update(id, func(c *card.Card) { c.History = append(c.History, card.Event{At: now, Text: text}) })
}

// noteOnce は直前と同じ文なら足さない。何も起動していない理由 (起動・再開できない) にだけ使う (理由が変わらないまま Tick ごとに記録が伸びないように)。
// 🚨 claude を実際に走らせた結果 (失敗と返った) には使わない: 走らせた回数 = 立っているかもしれない session の数が履歴から消える
func (d *Daemon) noteOnce(id string, now time.Time, text string) error {
	return d.update(id, func(c *card.Card) {
		if n := len(c.History); n > 0 && c.History[n-1].Text == text {
			return
		}
		c.History = append(c.History, card.Event{At: now, Text: text})
	})
}

func (d *Daemon) update(id string, f func(*card.Card)) error {
	return store.Update(d.Dir, func(s *store.State) error {
		for i := range s.Cards {
			if s.Cards[i].ID == id {
				f(&s.Cards[i])
			}
		}
		return nil
	})
}

// Prompt は PG に渡す最初の指示。PG の規律 (426 の決定 2・3) を前に置き、依頼の中身を後ろに置く。
func Prompt(c card.Card) string {
	var b strings.Builder
	fmt.Fprintf(&b, "あなたは pro-con の PG (作業担当) です。担当はカード %s「%s」。\n", c.ID, c.Title)
	b.WriteString("規律:\n")
	b.WriteString("- 作業は自分の worktree で行い、commit は自分のブランチまで push する (master へは push しない)\n")
	fmt.Fprintf(&b, "- 質問があるときは AskUserQuestion を使わず、`pro-con card ask %s \"<質問>\"` を実行してから turn を終える (回答は再開のときに届く)\n", c.ID)
	fmt.Fprintf(&b, "- make test・ビルド・実機 E2E など時間のかかるコマンドは自分で走らせず、`pro-con card run %s -- <コマンド>` で頼んでから turn を終える (結果は再開のときに届く。同時に頼めるのは 1 本)\n", c.ID)
	fmt.Fprintf(&b, "- 終えたら `pro-con card review %s` を実行してから turn を終える\n", c.ID)
	if len(c.Issues) > 0 {
		var refs []string
		for _, r := range c.Issues {
			refs = append(refs, r.String())
		}
		fmt.Fprintf(&b, "\n関わる issue: %s\n", strings.Join(refs, ", "))
	}
	if c.Prompt != "" {
		b.WriteString("\n指示:\n" + c.Prompt + "\n")
	} else if c.Request != "" {
		b.WriteString("\n依頼の原文:\n" + c.Request + "\n")
	}
	return b.String()
}
