// Package live は本物の backend (issue 424 / 427 の段階 3d)。カードは dispatcher が書く記録 (store) から読み、作業中のカードには
// **pro-con が起動した session** (registry.go の記録にあるもの) の様子 (PG の出力の末尾・pid) を `claude agents --json` と transcript から足す。
// Desktop や他の shell で立ち上げた session は出さない (選べると、pro-con の外の session に入力・停止できてしまう)。
//
// 書き込みは受付の箱に置くだけ (store.Submit)。記録へ適用するのは dispatcher (426 の決定 1)。新しい依頼・回答・追加オーダー・btw・
// 片付けを受ける。PG へ届ける・答えるのは dispatcher (dispatcher/orders.go・btw.go。issue 438)。
package live

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"pro-con/agents"
	"pro-con/backend"
	"pro-con/card"
	"pro-con/diskuse"
	"pro-con/eventlog"
	"pro-con/presence"
	"pro-con/store"
	"pro-con/wake"
)

// Interval は一覧と transcript を読み直す間隔。claude agents --json は 1 回 0.15 秒ほどかかるので、画面の tick (1 秒) では呼ばない。
const Interval = 3 * time.Second

// Backend は本物の backend。
type Backend struct {
	attach   func(sessionID string) *exec.Cmd // attach のコマンド (nil なら本物の claude attach。e2e モードは偽の attach)
	stopAll  func(context.Context) error      // 終了のときに dispatcher と PG を止める (main が dispatcher の停止をつなぐ。live は dispatcher を import できない)
	repos    []backend.Repo
	dir      string // 本物のモードの状態の置き場 (カードの記録・受付の箱・pro-con が起動した session の記録)
	registry string // pro-con が起動した session の記録 (registry.go)
	list     func(context.Context) ([]agents.Session, error)
	findPath func(sessionID string) (string, error)
	read     func(path string) (Transcript, error)
	now      func() time.Time

	mu      sync.Mutex
	snap    backend.Snapshot
	pending int  // 受付の箱の適用待ちの数 (dispatcher が動いていないと溜まる。ヘッダーに出す)
	ready   bool // 最初の読み取りが済んだか
	done    chan struct{}
	tcache  *TranscriptCache  // transcript の読み取り結果 (大きさ・更新時刻が変わっていなければ読み直さない)
	paths   map[string]string // sessionId → transcript のパス
	ssSeen  bool              // 今の一覧 (ss) は dispatcher が書いた store.Seen から来た (Start の goroutine だけが触る)
	// ss / ssErr は最後に取った session の一覧 (dispatcher に知らされた読み直しは一覧を取り直さない。Refresh の goroutine だけが触る)
	ss       []agents.Session
	ssErr    error
	listed   bool          // 1 度でも一覧を取ったか (一覧が空 (nil) のときも、知らせのたびに取り直さない)
	interval time.Duration // 一覧つきで読み直す間隔 (Interval。テストが延ばす)
	// subscribe は dispatcher の購読を回す (テストで差し替える)
	subscribe func(ctx context.Context, s *wake.Subscriber)
	// screen はこの画面が開いている印 (package presence。閉じるとき、ほかの画面の有無を数える)。Start で置く
	screen *presence.Screen
	// keeper は dispatcher が居なければ起こす (main がつなぐ。画面が開いている間は dispatcher を動かし続ける)。leaving は閉じる途中
	// (自分が頼んだ停止の後で起こし直さない)
	keeper  func() error
	leaving atomic.Bool
	// viewOnly は読み取りだけで開く (pro-con --view)。画面の印を置かない (数えない) / ほかの画面へ知らせない
	viewOnly bool
	// join は加わった画面で開く (pro-con --join。issue 481)。印は join として置き、quit で閉じても止めない (keeper・stopAll をつながない)
	join bool
	// label / tty は画面の印に書く見分け (--as <ラベル> と端末。空でよい)
	label, tty string
	// mine はこの画面が受付の箱に置いた依頼 (依頼 ID → 中身の要点)。dispatcher が除けたら理由をこの画面にだけ出す (issue 481)。mu で守る
	mine     map[string]mineReq
	rejected []backend.Rejected // 除けられて、まだ画面に渡していないもの。mu で守る
	changed  chan struct{}      // 読み直したら値が入る (画面が 1 秒の tick を待たずに描き直す。backend.Notifier)
	// refused は socket の逃がし先を使えないので購読をつながなかった理由 (空ならつながっている / まだ試していない)。画面の違反の行に 1 行出す。mu で守る
	refused string
	// logs はカード ID → 読んでいる活動 (Activity。画面が裏で呼ぶ。actMu で守る。Refresh の goroutine とは別)
	// procs / disk は設定画面の見る所を読む口 (SetInspector。main がつなぐ)
	procs func() ([]backend.Proc, error)
	disk  func() (diskuse.Usage, error)
	// events は設定画面のログのタブが読み進める記録 (Events。evMu で守る。最初に呼ばれたときに作る)
	evMu   sync.Mutex
	events *eventlog.Follower
	actMu  sync.Mutex
	logs   map[string]*cardActivity
}

// mineReq はこの画面が置いた、まだ適用も除けもされていない依頼。
type mineReq struct {
	kind, cardID string
	at           time.Time
}

// mineKeep は、適用も除けもされないまま置いておく依頼の上限の時間 (dispatcher が長く止まっていた等。過ぎたら知らせずに忘れる)。
const mineKeep = 24 * time.Hour

// cardActivity は 1 枚のカードの、読んだ活動 (末尾の activityKeep 件) と続きを読む位置。
type cardActivity struct {
	log   *CardLog
	items []backend.Activity
}

// LogOutputs はカードに出す PG の出力の末尾の数 (画面が自分で読むときも、dispatcher が store.Seen に書くときも同じ)。
const LogOutputs = 3

// seenFresh は dispatcher が書いた一覧 (store.Seen) を今の様子として使う古さの上限。dispatcher の tick (3 秒) と、一覧の取得の上限
// (agents.Timeout = 10 秒) を足した余裕。これより古ければ画面が自分で読む。
const seenFresh = 15 * time.Second

// activityKeep は画面が 1 枚のカードについて持つ活動の上限 (古いものから捨てる。全部は pro-con card log で読める)。
const activityKeep = 1000

// New は本物の claude と ~/.claude/projects を読む backend を作る。stateDir は本物のモードの状態の置き場 (記録はその下)。
// Start で読み直しを始める。
func New(repos []backend.Repo, home, stateDir string) *Backend {
	projects := filepath.Join(home, ".claude", "projects")
	now := time.Now()
	b := &Backend{
		repos:     repos,
		dir:       stateDir,
		registry:  filepath.Join(stateDir, RegistryFile),
		snap:      backend.Snapshot{Now: now, DispatcherTick: now},
		done:      make(chan struct{}),
		list:      execList,
		findPath:  func(id string) (string, error) { return FindTranscript(projects, id) },
		read:      ReadTail,
		now:       time.Now,
		paths:     map[string]string{},
		changed:   make(chan struct{}, 1),
		logs:      map[string]*cardActivity{},
		mine:      map[string]mineReq{},
		interval:  Interval,
		subscribe: func(ctx context.Context, s *wake.Subscriber) { s.Run(ctx) },
	}
	b.tcache = &TranscriptCache{Read: func(p string) (Transcript, error) { return b.read(p) }} // read はテストが差し替える
	return b
}

// execList は本物の claude agents を読む。🚨 画面は PATH の claude を素の名前で呼ぶ (画面の cwd は 1 つなので repo ごとには変わらないが、
// dispatcher が起動時に解決した実体とは版がずれうる。464 の残り)
func execList(ctx context.Context) ([]agents.Session, error) {
	return agents.List(ctx, agents.ExecRunner("claude"))
}

// Changed は読み直すたびに値が入る (溜まった分は 1 つにまとめる。backend.Notifier)。
func (b *Backend) Changed() <-chan struct{} { return b.changed }

// SetList は session の一覧の読み方を差し替える (e2e モードは偽の一覧を読む)。Start の前に呼ぶ。
func (b *Backend) SetList(f func(context.Context) ([]agents.Session, error)) { b.list = f }

// SetInspector は設定画面の見る所を読む口をつなぐ (Start の前に呼ぶ)。procs は pro-con ps と同じ集め方 (main が持つ。
// dispatcher の定数を使うので live からは組めない)。つながなければ Procs / DiskUsage は ErrNoInspector を返す。
func (b *Backend) SetInspector(procs func() ([]backend.Proc, error), disk func() (diskuse.Usage, error)) {
	b.procs, b.disk = procs, disk
}

// ErrNoInspector は見る所を読む口がつながっていないとき。
var ErrNoInspector = errors.New("この画面には見る所を読む口が無い")

// Procs は pro-con が起動したプロセスを役ごとに返す (backend.Inspector。読むだけ)。
func (b *Backend) Procs() ([]backend.Proc, error) {
	if b.procs == nil {
		return nil, ErrNoInspector
	}
	return b.procs()
}

// DiskUsage は pro-con が作った物のディスクの使用量と内訳を測る (backend.Inspector。読むだけ。数秒かかる)。
func (b *Backend) DiskUsage() (diskuse.Usage, error) {
	if b.disk == nil {
		return diskuse.Usage{}, ErrNoInspector
	}
	return b.disk()
}

// Events は前に呼んだ後に events.jsonl へ足された出来事を返す (backend.EventLog。最初は全部。読むだけ)。
func (b *Backend) Events() ([]eventlog.Event, error) {
	b.evMu.Lock()
	defer b.evMu.Unlock()
	if b.events == nil {
		b.events = eventlog.NewFollower(b.dir)
	}
	return b.events.Next()
}

// SetAttach は attach のコマンドを差し替える (e2e モードは本物の claude を起動しない)。
func (b *Backend) SetAttach(f func(sessionID string) *exec.Cmd) { b.attach = f }

// View は読み取りだけの画面 (pro-con --view) の backend を返す (Start の前に呼ぶ)。依頼・回答・attach を受けず、止める口を持たないので、
// quit はこの画面を閉じるだけになる。画面の印を置かないので、ほかの画面の「最後の画面か」の数えにも入らない。
// 🚨 SetStopper / SetKeeper をつながないこと (つながっていても View の backend からは呼べないが、keeper は Start の読み直しから呼ばれる)
func (b *Backend) View() backend.Backend {
	b.viewOnly = true
	return viewOnly{b}
}

// viewOnly は読み取りだけの backend (backend.ReadOnly)。Stopper / AttachRecorder を持たない (型の上で止める・書く口が無い)。
type viewOnly struct{ b *Backend }

func (v viewOnly) Poll() backend.Snapshot     { return v.b.Poll() }
func (v viewOnly) Snapshot() backend.Snapshot { return v.b.Snapshot() }
func (v viewOnly) Changed() <-chan struct{}   { return v.b.Changed() }
func (v viewOnly) Accepts(backend.Op) bool    { return false }
func (v viewOnly) ReadOnly()                  {}
func (v viewOnly) Describe() string {
	return "view (読み取りだけ・quit で何も止めない) / " + v.b.Describe()
}
func (v viewOnly) Activity(cardID string) ([]backend.Activity, error) {
	return v.b.Activity(cardID) // 読むだけ (transcript と起動の記録を開いて読む)
}
func (v viewOnly) Procs() ([]backend.Proc, error)          { return v.b.Procs() }     // 読むだけ (ps と記録)
func (v viewOnly) DiskUsage() (diskuse.Usage, error)       { return v.b.DiskUsage() } // 読むだけ (測るだけ)
func (v viewOnly) Events() ([]eventlog.Event, error)       { return v.b.Events() }    // 読むだけ (events.jsonl)
func (v viewOnly) Apply(backend.Command) (string, error)   { return "", ErrViewOnly }
func (v viewOnly) AttachCommand(string) (*exec.Cmd, error) { return nil, ErrViewOnly }

// ErrViewOnly は読み取りだけの画面で書く操作をしたとき。
var ErrViewOnly = errors.New("見ているだけの画面 (pro-con --view) なので受けない")

// Join は加わった画面 (pro-con --join) の backend を返す (Start の前に呼ぶ。issue 481)。依頼・回答・attach などは持ち主の画面と同じく
// 受けるが、dispatcher を起こさない (c も受けない) し、quit で閉じても止めない。画面の印は join として置く (持ち主の「最後の画面か」の
// 数えには入らないが、dispatcher の「画面が無ければ抜ける」の数えには入る)。
// 🚨 SetStopper / SetKeeper をつながないこと (つながっていても join の StopAll は止めず、keeper は起こさない)
func (b *Backend) Join() backend.Backend {
	b.join = true
	return joined{b}
}

// SetScreenInfo は画面の印に書く見分け (--as <ラベル> と端末) を渡す (Start の前に呼ぶ)。
func (b *Backend) SetScreenInfo(label, tty string) { b.label, b.tty = label, tty }

// joined は加わった画面の backend (backend.Joiner)。書く口は持ち主と同じで、dispatcher を起こす口 (c) だけ受けない。
type joined struct{ *Backend }

func (j joined) Joined() {}

func (j joined) Accepts(op backend.Op) bool { return op != backend.OpResume }

func (j joined) Describe() string {
	return "join (読み書き・dispatcher を起こさない・quit で何も止めない) / " + j.Backend.Describe()
}

// ErrJoinNoResume は加わった画面で dispatcher を起こそうとしたとき (起こすのは持ち主の画面か、手で起動した dispatcher)。
var ErrJoinNoResume = errors.New("join の画面からは dispatcher を起こさない (持ち主の画面の c か、手で pro-con dispatcher を起動する)")

// Apply は持ち主と同じく受付の箱に置く。dispatcher が動いていなければ、箱で待っていると知らせる (join は起こさない)。
func (j joined) Apply(cmd backend.Command) (string, error) {
	if _, ok := cmd.(backend.ResumeDispatcher); ok {
		return "", ErrJoinNoResume
	}
	msg, err := j.Backend.Apply(cmd)
	if err == nil && msg != "" && j.dispatcherIdle() {
		msg += " / dispatcher が動いていないので受付の箱で待っている (起こすのは持ち主の画面か pro-con dispatcher)"
	}
	return msg, err
}

// dispatcherIdle は dispatcher が人に止められている / 1 度も回っていない / keepAfter より長く回っていないか (最後に読んだ様子で)。
func (b *Backend) dispatcherIdle() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.snap.DispatcherHeld || b.snap.DispatcherTick.IsZero() || b.now().Sub(b.snap.DispatcherTick) > keepAfter
}

// SetKeeper は dispatcher が居ないときに起こす口をつなぐ (Start の前に呼ぶ)。
func (b *Backend) SetKeeper(f func() error) { b.keeper = f }

// keepAfter は dispatcher がこれより長く回っていなければ起こし直す (Tick は 3 秒ごと。起こしたばかりの dispatcher を二重に起こさない
// のは dispatcher の lock が守る)。
const keepAfter = 10 * time.Second

// keep は dispatcher が居なければ起こす (閉じる途中の画面は起こさない)。
// 🚨 開いている印 (presence) を置けなかった画面も起こさない: 画面が起こした dispatcher は、数えられる画面が無いと 1 分で PG を止めて抜けるので、
// 起こし直すたびに PG の停止と再開を繰り返して枠を使う
func (b *Backend) keep() {
	if b.keeper == nil || b.join || b.leaving.Load() || b.screen == nil { // join は起こさない (issue 481)
		return
	}
	b.mu.Lock()
	tick, now := b.snap.DispatcherTick, b.snap.Now
	b.mu.Unlock()
	if tick.IsZero() || now.Sub(tick) > keepAfter {
		_ = b.keeper()
	}
}

// SetStopper は終了のときの停止をつなぐ。
func (b *Backend) SetStopper(f func(context.Context) error) { b.stopAll = f }

// StopAll は dispatcher と、pro-con が起動した PG を止める (backend.Stopper)。
// ほかの持ち主の画面が開いていれば止めずに backend.KeptRunning を返す (止めるのは最後に閉じる持ち主の画面だけ。package presence が
// 数えるので、dispatcher が居なくても数えられる。join の画面は数えない = join が残っていても止める。issue 481)。
// この画面の印を置けなかった・数えられなければ、最後の画面として止める。join の画面は何も止めずに閉じる。
func (b *Backend) StopAll(ctx context.Context) error {
	b.leaving.Store(true) // この後は dispatcher を起こし直さない (最後の画面なら、これから止める)
	if b.join {
		left := "開いている印を置けなかった画面"
		if b.screen != nil {
			others, err := b.screen.Leave()
			_ = wake.Notify(b.dir) // ほかの画面の一覧を直す
			left = fmt.Sprintf("ほかに持ち主 %d・join %d 画面", others.Owners, others.Joins)
			if err != nil {
				left = "ほかの画面を数えられない: " + err.Error()
			}
		}
		b.event("quit で閉じた: join の画面なので dispatcher と PG は止めない (" + left + ")")
		return backend.KeptRunning{Join: true}
	}
	if b.stopAll == nil {
		return errors.New("止める口がつながっていない")
	}
	why := "画面の印を置けなかったので最後の画面として"
	if b.screen != nil {
		others, err := b.screen.Leave()
		_ = wake.Notify(b.dir) // ほかの画面の一覧を直す
		switch {
		case err != nil:
			why = "ほかの画面を数えられない (" + err.Error() + ") ので最後の画面として"
		case others.Owners > 0:
			b.event(fmt.Sprintf("quit で閉じた: ほかに持ち主の画面が %d 開いているので dispatcher と PG は止めなかった", others.Owners))
			return backend.KeptRunning{Others: others.Owners}
		case others.Joins > 0:
			why = fmt.Sprintf("最後の持ち主の画面なので (join の画面 %d は残り、止めた後は表示が止まる)", others.Joins)
		default:
			why = "最後の画面なので"
		}
	}
	// 止める前に置く: 止める dispatcher が Shutdown の前に箱を適用するので、その dispatcher が書く
	b.event("quit で閉じた: " + why + " dispatcher と PG を止める")
	err := b.stopAll(ctx)
	if err != nil {
		b.event("quit で止めきれなかった: " + err.Error()) // 止めた後なので、次に起動した dispatcher が書く
	}
	return err
}

// event は画面の出来事を受付の箱に置く (出来事の記録 events.jsonl へ書くのは dispatcher。書き手を 1 つに保つ = issue 445)。
// dispatcher が居なければ次に起動した dispatcher が書く。画面が quit を通らずに消えた (落ちた・端末を閉じた) ときは何も残らない
// (dispatcher の「画面が無い」の出来事 (screens) が代わりになる)。置けなくても画面の動きは変えない。
// 🚨 --view の画面からは呼ばない (受付の箱にも書かない。Start は viewOnly なら呼ばず、StopAll は View の backend から呼べない)
func (b *Backend) event(text string) {
	who := "画面"
	if n := b.screenName(); n != "" {
		who += " " + n
	}
	_, _ = store.Submit(b.dir, store.Request{Kind: store.KindEvent, Note: fmt.Sprintf("%s (pid %d): %s", who, os.Getpid(), text)})
}

// screenName は依頼の履歴・出来事に残すこの画面の名前 (印を置けなければ空)。
func (b *Backend) screenName() string {
	if b.screen == nil {
		return ""
	}
	return b.screen.Info().Name()
}

// submit は依頼を受付の箱に置き、除けられたら知らせるよう覚える (画面の出来事 = event は覚えない)。
func (b *Backend) submit(r store.Request) error {
	r.Screen = b.screenName()
	id, err := store.Submit(b.dir, r)
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.mine[id] = mineReq{kind: r.Kind, cardID: r.CardID, at: b.now()}
	b.mu.Unlock()
	return nil
}

// settle は置いた依頼を記録と照らす: 適用された分は忘れ、除けられた分は理由 (dispatcher が書いたもの) を渡す分に移す。
func (b *Backend) settle(st store.State) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.mine) == 0 {
		return
	}
	why := make(map[string]string, len(st.Rejected))
	for _, r := range st.Rejected {
		why[r.ID] = r.Why
	}
	applied := make(map[string]bool, len(st.Applied))
	for _, id := range st.Applied {
		applied[id] = true
	}
	now := b.now()
	for id, m := range b.mine {
		if w, ok := why[id]; ok {
			b.rejected = append(b.rejected, backend.Rejected{Kind: m.kind, CardID: m.cardID, Why: w})
			delete(b.mine, id)
		} else if applied[id] || now.Sub(m.at) > mineKeep {
			delete(b.mine, id)
		}
	}
}

// TakeRejected は、この画面が置いて除けられた依頼を渡して忘れる (backend.RejectReader)。
func (b *Backend) TakeRejected() []backend.Rejected {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.rejected
	b.rejected = nil
	return out
}

// FindTranscript は projects (~/.claude/projects) の下から sessionID の transcript を探す。
// ディレクトリ名は cwd から Claude Code が組む (規則を真似ず、sessionId で探す)。
func FindTranscript(projects, sessionID string) (string, error) {
	ms, err := filepath.Glob(filepath.Join(projects, "*", sessionID+".jsonl"))
	if err != nil || len(ms) == 0 {
		return "", os.ErrNotExist
	}
	// 同じ session id の transcript が複数の project にあれば (別の cwd で再開した等)、更新の新しい方 (今書かれている方)
	best, bestAt := "", time.Time{}
	for _, m := range ms {
		if st, err := os.Stat(m); err == nil && (best == "" || st.ModTime().After(bestAt)) {
			best, bestAt = m, st.ModTime()
		}
	}
	if best == "" {
		return "", os.ErrNotExist
	}
	return best, nil
}

// Start は裏で読み直しを始める (最初の読み取りも裏で行う。claude agents --json は最大 10 秒待つので、画面を出す前に待たない)。
// ctx が終わったら止める。止まったかは Wait で待てる。
// dispatcher が記録を変えたと知らせてきたら (package wake の購読)、一覧を取り直さずにすぐ読み直す。
func (b *Backend) Start(ctx context.Context) {
	kick := make(chan struct{}, 1)
	subDone := make(chan struct{})
	// 見ているだけの画面は数えない: 数えると、普通の画面が閉じるときに「ほかに画面が開いている」と見て、止めるべきものを止めない
	if !b.viewOnly {
		mode := presence.Owner
		if b.join {
			mode = presence.Join
		}
		if sc, err := presence.OpenAs(b.dir, presence.Info{Mode: mode, Label: b.label, TTY: b.tty}); err == nil { // 置けなければ、閉じるときは最後の画面として止める (StopAll)
			b.screen = sc
			n, _ := presence.Count(b.dir)
			b.event(fmt.Sprintf("開いた (開いている画面 %d)", n)) // ctrl+r の入れ替えでも新版が開き直すので出る
			_ = wake.Notify(b.dir)                      // ほかの画面の「画面 N」を直す
		} else {
			b.event("開いた (開いている印を置けない: " + err.Error() + ")")
		}
	}
	sub := wake.NewSubscriber(b.dir, func() {
		b.mu.Lock()
		b.refused = "" // つながった (つながるたびに 1 度呼ばれる)
		b.mu.Unlock()
		select {
		case kick <- struct{}{}:
		default:
		}
	}).OnRefused(func(err error) {
		// 🚨 画面は逃がし先の権限を直さない (--view は読むだけ。直すのは dispatcher だけ。issue 445)。つながずに 3 秒の読み直しで出す
		b.mu.Lock()
		b.refused = "dispatcher の知らせを受けずに " + b.interval.String() + " ごとの読み直しで出す: " + err.Error()
		b.mu.Unlock()
	})
	go func() {
		defer close(subDone)
		b.subscribe(ctx, sub)
	}()
	go func() {
		defer close(b.done)
		defer func() { <-subDone }() // Wait は購読の goroutine も待つ
		b.Refresh(ctx)
		t := time.NewTicker(b.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				b.Refresh(ctx)
				b.keep()
			case <-kick:
				b.refresh(ctx, false)
			}
		}
	}()
}

// Wait は Start の読み直しが止まるのを待つ (終了のとき。止まる前に抜けると、claude の子プロセスが残りうる)。
func (b *Backend) Wait() { <-b.done }

// Refresh はカードの記録を読み、作業中のカードに pro-con が起動した session の様子を足して Snapshot を作り直す。
// 🚨 1 つの goroutine からだけ呼ぶ (Start の中)。transcript のパス (paths) は lock の外で触っている。
// 記録を読めなければ前の Snapshot を残して理由を出す (0 枚と区別する)。session の一覧を取れないときは、カードは出して理由を足す。
func (b *Backend) Refresh(ctx context.Context) { b.refresh(ctx, true) }

// refresh は Refresh の本体。withList が偽なら session の一覧は前に取ったものを使う (dispatcher に知らされたとき。
// claude agents --json は最大 10 秒かかるので、カードの記録の変化を画面へ出すのを待たせない)。
func (b *Backend) refresh(ctx context.Context, withList bool) {
	defer func() {
		select {
		case b.changed <- struct{}{}:
		default:
		}
	}()
	st, err := store.Load(b.dir)
	if err != nil {
		b.fail("カードの記録を読めない: " + err.Error())
		return
	}
	reg, err := LoadRegistry(b.registry)
	if err != nil {
		b.fail("pro-con が起動した session の記録を読めない (" + b.registry + "): " + err.Error())
		return
	}
	now := b.now()
	var extra []card.Violation
	// dispatcher が書いた一覧と出力の末尾が新しければ使い、自分では claude agents も transcript も読まない (issue 502 / 503)。
	// 古い・無い (dispatcher が回っていない・一覧を取れていない) ときだけ自分で読む (正しさを dispatcher に預けない)
	// 未来の時刻 (時計が戻った) と、dispatcher が一覧を取れなかった印 (Err) は古いとみなす
	seen, seenErr := store.LoadSeen(b.dir)
	age := now.Sub(seen.At)
	fresh := seenErr == nil && !seen.At.IsZero() && seen.Err == "" && age >= 0 && age <= seenFresh
	switch {
	case fresh:
		b.ss, b.ssErr, b.listed, b.ssSeen = seen.Sessions, nil, true, true
	case withList || !b.listed || b.ssSeen: // 持っている一覧が dispatcher の記録から来たものなら、古くなった時点で (知らせの読み直しでも) 取り直す
		b.ss, b.ssErr, b.listed, b.ssSeen = nil, nil, true, false
		b.ss, b.ssErr = b.list(ctx)
	}
	ss, err := b.ss, b.ssErr
	if err != nil {
		extra = append(extra, card.Violation{Reason: "session の一覧を取れない (PG の様子は古いまま): " + err.Error()})
	}
	owned := OwnedSessions(reg, ss)
	// PG が今走らせているもの・進捗・取り込みの衝突 (dispatcher と見張りが集める。画面は ps も git も transcript の全体も読まない = 473 / 469)
	derived, errs := store.LoadDerived(b.dir)
	for _, err := range errs {
		extra = append(extra, card.Violation{Reason: "集めた様子を読めない: " + err.Error()})
	}
	cards := append([]card.Card(nil), st.Cards...)
	var cons []backend.Consumer
	for i, c := range cards {
		cards[i].Request = clip(c.Request, requestRunes) // 貼り付けた巨大な依頼を、詳細の描画のたびに折り返さない
		derived.Attach(&cards[i])
		s, ok := owned[c.Session]
		if c.Session == "" || !ok {
			continue // 記録に無い session (外のもの) の様子は足さない
		}
		// LastProgress は dispatcher の watchdog だけが書く (活動と進捗を分けて判定するため)
		if fresh {
			cards[i].Log = seen.Logs[s.ID]
		} else if t := b.transcript(s.SessionID); len(t.Outputs) > 0 {
			cards[i].Log = slices.Clip(tail(t.Outputs, LogOutputs)) // Outputs は transcript のキャッシュと共有 (append で書き込ませない)
		}
		if c.State == card.Running || c.WaitsOnPrompt() { // 入力待ちで止まった PG も生きていて枠を使う (dispatcher と同じ = card.HoldsPGSlot)
			cons = append(cons, backend.Consumer{Session: s.ID, CardID: c.ID, Status: s.Status, PID: s.PID})
		}
	}
	// 数えられなければ 0 (「画面 N」を出さず、終了は止める側の案内になる)。
	// 数えるとき落ちた画面の印を消す (screens/ を書く)。--view の画面も消してよい: どの画面が数えても同じ後始末で、
	// 画面・dispatcher・PG の状態を変えない (読むだけの例外 (b)。issue 445)
	infos, _ := presence.List(b.dir)
	screens := make([]backend.Screen, len(infos))
	for i, in := range infos {
		screens[i] = backend.Screen{ID: in.Short(), Join: in.Mode == presence.Join, Label: in.Label, TTY: in.TTY, PID: in.PID, Opened: in.Opened,
			Self: b.screen != nil && in.ID == b.screen.Info().ID}
	}
	b.settle(st)

	ds, _, err := store.LoadDispatcherState(b.dir) // 無ければ zero (dispatcher が 1 度も回っていない)
	if err != nil {
		extra = append(extra, card.Violation{Reason: "dispatcher の様子を読めない: " + err.Error()})
	}
	pending, configs := store.PendingCounts(b.dir)
	cfg := backend.Config{LimitFrom: ds.LimitFrom, Pending: configs}
	if set, err := store.LoadSettings(b.dir); err != nil {
		cfg.Err = err.Error()
	} else {
		cfg.Limit, cfg.PMs = set.Limit, set.PMs
	}
	b.mu.Lock()
	if b.refused != "" {
		extra = append(extra, card.Violation{Reason: b.refused})
	}
	b.snap = backend.Snapshot{Now: now, Cards: cards, Consumers: cons, Limit: ds.Cap, LimitMax: ds.Limit, LimitWhy: ds.Why,
		DispatcherTick: ds.Tick, Screens: screens, DispatcherHeld: store.Held(b.dir),
		DispatcherGone: store.DispatcherGone(b.dir), Startup: ds.Startup, StartupAlert: ds.StartupAlert, Upgrade: ds.Upgrade, UpgradeAlert: ds.UpgradeAlert, Roles: ds.Roles, RoleStates: ds.RoleStates, Violations: append(card.Check(cards), extra...), Config: cfg}
	b.pending, b.ready = pending, true
	b.mu.Unlock()
}

// fail は読み取りに失敗したとき、前のカードを残して理由を出す (0 本と区別する。画面は件数を隠さない)。
func (b *Backend) fail(reason string) {
	b.mu.Lock()
	b.snap.Violations = []card.Violation{{Reason: reason}}
	b.ready = true
	b.mu.Unlock()
}

// transcript は sessionId の transcript の末尾 (大きさ・更新時刻が前と同じなら読み直さない)。見つからなければ空。
func (b *Backend) transcript(id string) Transcript {
	p, ok := b.paths[id]
	if !ok {
		var err error
		if p, err = b.findPath(id); err != nil {
			return Transcript{}
		}
		b.paths[id] = p
	}
	t, err := b.tcache.Get(p)
	if err != nil {
		delete(b.paths, id) // 消えた・読めない: 次は探し直す
		return Transcript{}
	}
	return t
}

func (b *Backend) Poll() backend.Snapshot { return b.Snapshot() }

func (b *Backend) Snapshot() backend.Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.snap
	s.Cards = append([]card.Card(nil), b.snap.Cards...)
	return s
}

// Apply は受付の箱に依頼を置く (記録へ適用するのは dispatcher)。操作はすべて受ける (backend.Accepter を持たない。断るのは読み取りだけの画面 = viewOnly)。
func (b *Backend) Apply(cmd backend.Command) (string, error) {
	var r store.Request
	done := "受け付けた (dispatcher が適用するとカードに出る)"
	switch c := cmd.(type) {
	case backend.NewRequest:
		if strings.TrimSpace(c.Text) == "" && c.Issue == nil {
			return "", backend.ErrEmptyText
		}
		r = store.Request{Kind: "add", Title: clip(firstLine(c.Text), 40), Request: c.Text, Prompt: backend.PMPrompt(c.Repo, c.Text), Repo: c.Repo.Name, Owner: "PM"}
		if c.Issue != nil {
			r.Title, r.Request, r.Issues = backend.IssueRequest(c.Repo.Name, *c.Issue, c.Text)
			r.Prompt = backend.PMPrompt(c.Repo, backend.IssuePrompt(*c.Issue, c.Text))
		}
	case backend.Answer:
		if strings.TrimSpace(c.Text) == "" {
			return "", backend.ErrEmptyText
		}
		r = store.Request{Kind: "answer", CardID: c.CardID, Answer: c.Text, From: firstNonEmpty(c.From, "人間")}
	case backend.DeleteCard:
		r = store.Request{Kind: "delete", CardID: c.CardID, From: firstNonEmpty(c.From, "人間")}
	case backend.MoveCard:
		r = store.Request{Kind: "move", CardID: c.CardID, Repo: c.Repo, Delta: c.Delta, Seen: c.Seen}
		done = "" // 動いたカードそのものが知らせ (押すたびに通知を重ねない)
	case backend.AddOrder:
		if strings.TrimSpace(c.Text) == "" {
			return "", backend.ErrEmptyText
		}
		if c.Kind == card.OrderSeparate { // 別件は元のカードの子の新しい依頼 (PM が分けて issue に紐づける)
			parent, ok := b.card(c.CardID)
			if !ok {
				return "", backend.ErrNotFound
			}
			repo := b.repo(parent.Repo)
			r = store.Request{Kind: "add", ParentID: parent.ID, Title: clip(firstLine(c.Text), 40), Request: c.Text,
				Prompt: backend.PMPrompt(repo, parent.ID+" の追加オーダー (別件) として出た依頼です。同じファイルを触るなら "+parent.ID+" の後に着手してください。\n\n"+c.Text),
				Repo:   repo.Name, Owner: "PM"}
			done = "別件として受け付けた (dispatcher が適用すると " + parent.ID + " の子の新しいカードになる)"
			break
		}
		r = store.Request{Kind: "order", CardID: c.CardID, Order: c.Kind, Text: c.Text}
		done = "追加オーダーを受け付けた (追記は PG の turn の区切りで、方針変更は PG を止めて届ける)"
	case backend.Btw:
		if strings.TrimSpace(c.Question) == "" {
			return "", backend.ErrEmptyText
		}
		r = store.Request{Kind: "btw", CardID: c.CardID, Question: c.Question}
		done = "btw を受け付けた (PG は止めない。答えはカードの履歴に出る)"
	case backend.ResumeDispatcher:
		return b.resume()
	case backend.SetConfig:
		if _, err := store.CheckSetting(c.Key, c.Value); err != nil { // 置く前に弾く (pro-con config と同じ検査。dispatcher も同じ検査で除ける)
			return "", err
		}
		r = store.Request{Kind: store.KindConfig, Key: c.Key, Value: c.Value}
		done = fmt.Sprintf("%s を %s にするよう受け付けた (dispatcher の次の Tick から効く)", c.Key, c.Value)
	case backend.ClearDone:
		var ids []string
		for _, cc := range b.Snapshot().Cards { // 画面が見ている完了のカードだけ (適用までに完了になったカードを巻き込まない)
			if cc.State == card.Done && !cc.Archived && (c.Repo == "" || cc.Repo == c.Repo) {
				ids = append(ids, cc.ID)
			}
		}
		if len(ids) == 0 {
			return "片付ける完了のカードが無い", nil
		}
		r = store.Request{Kind: "clear", Cards: ids}
		done = fmt.Sprintf("完了のカード %d 枚の片付けを受け付けた (記録には残る)", len(ids))
	default:
		return "", backend.ErrUnknownKind
	}
	if err := b.submit(r); err != nil {
		return "", err
	}
	if r.Kind == "move" {
		b.moveAhead(r)
	}
	if r.Kind == "delete" {
		return r.CardID + " の削除を受け付けた (依頼の列ならすぐ、ほかは PG の session を止めてから消える)", nil
	}
	return done, nil
}

// moveAhead は箱に置いた並べ替えを、読んだ記録 (画面が見ている並び) にも先に当てる。dispatcher の適用 (約 130 ms) を待つ間に
// 続けて押したキーが、古い並びの端の判定で断られたり、同じ隣を指したりしないように (押した回数だけ動く)。
// 正本は dispatcher が書く記録で、次の読み直しで置き換わる。🚨 当てられなくても (端・列を移った) 何もしない: 判定は dispatcher が下す
func (b *Backend) moveAhead(r store.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	i := slices.IndexFunc(b.snap.Cards, func(c card.Card) bool { return c.ID == r.CardID })
	if i < 0 || (!r.Seen.IsZero() && !r.Seen.Equal(b.snap.Cards[i].Since)) {
		return
	}
	cards := slices.Clone(b.snap.Cards) // Snapshot で渡した slice を書き換えない
	if _, err := card.Move(cards, r.CardID, r.Repo, r.Delta); err == nil {
		b.snap.Cards = cards
	}
}

// resume は人が止めた印を外して dispatcher を起こす (c。issue 459)。印が無くても、居なければ起こす。
func (b *Backend) resume() (string, error) {
	if b.keeper == nil {
		return "", errors.New("dispatcher を起こす口がつながっていない")
	}
	released, err := store.Release(b.dir)
	if err != nil {
		return "", fmt.Errorf("人が止めた印を外せない: %w", err)
	}
	if released {
		b.event("c で人が止めた印を外した (dispatcher を起こす)") // 起こす前に置く: 起きた dispatcher が箱を適用して書く
	}
	if err := b.keeper(); err != nil {
		return "", fmt.Errorf("印は外したが dispatcher を起こせない: %w", err)
	}
	return "dispatcher を起こした (作業中のカードの PG は続きから再開する)", nil
}

// card は最後に読んだ記録のカード。
func (b *Backend) card(id string) (card.Card, bool) {
	for _, c := range b.Snapshot().Cards {
		if c.ID == id {
			return c, true
		}
	}
	return card.Card{}, false
}

// repo は repo の名前から場所を引く (設定に無ければ名前だけ。PMPrompt は名前と場所を前置きに出す)。
func (b *Backend) repo(name string) backend.Repo {
	for _, r := range b.repos {
		if r.Name == name {
			return r
		}
	}
	return backend.Repo{Name: name}
}

// AttachCommand は裏の session を claude attach で開く。対話 session (Desktop) には attach の口が無い。
// 🚨 撃つ直前に一覧を取り直し、記録と照合し直す (最大 3 秒前の一覧の短い id のまま撃つと、その間に入れ替わった外の session へ attach しうる)。
// 照合し直してから claude attach が id を解決するまでの窓は閉じられない (claude attach は短い id で引き直す。pid では固定できない)。
// 一覧の取り直しで最大 3 秒待つので画面は裏で呼び (ui の attach)、窓は「照合の終わり → 画面の Update → 端末の明け渡し」まで延びる。
func (b *Backend) AttachCommand(sessionID string) (*exec.Cmd, error) {
	if sessionID == "" {
		return nil, errors.New("対話の session は Desktop で開く (attach できるのは claude --bg の session だけ)")
	}
	reg, err := LoadRegistry(b.registry)
	if err != nil {
		return nil, fmt.Errorf("pro-con が起動した session の記録を読めない: %w", err)
	}
	ss, err := b.list(context.Background())
	if err != nil {
		return nil, err
	}
	for _, s := range ss {
		if s.ID == sessionID && owns(reg, s.SessionID, s.ID, s.PID) {
			if b.attach != nil {
				return b.attach(sessionID), nil
			}
			return exec.Command("claude", "attach", sessionID), nil // 画面は素の名前 (execList と同じ。464 の残り)
		}
	}
	return nil, errors.New("pro-con が起動した session ではない (または終わった): " + sessionID)
}

// RecordAttach は attach の間 (from〜to) に人間が打った指示を、受付の箱に置く (記録へ残すのは dispatcher。backend.AttachRecorder)。
// sessionID は短い id (カードの Session)。transcript は記録の session id で引く。attach の間に再開で入れ替わった前の session も引ける
// (退いた側の記録も見る)。指示が無ければ何も置かない。
func (b *Backend) RecordAttach(cardID, sessionID string, from, to time.Time) (int, error) {
	full, err := b.fullSessionID(sessionID)
	if err != nil {
		return 0, err
	}
	path, err := b.findPath(full)
	if err != nil {
		return 0, fmt.Errorf("session %s の transcript が見つからない: %w", sessionID, err)
	}
	ps, err := HumanPrompts(path, from, to)
	if err != nil || len(ps) == 0 {
		return 0, err
	}
	said := make([]card.Event, len(ps))
	for i, p := range ps {
		said[i] = card.Event{At: p.At, Text: p.Text}
	}
	if err := b.submit(store.Request{Kind: "attach", CardID: cardID, Said: said}); err != nil {
		return 0, err
	}
	return len(ps), nil
}

// Activity はカードの PG の活動を古い順に返す (backend.ActivityReader。末尾の activityKeep 件)。呼ぶたびに前の続きだけを読む。
// 🚨 読むだけ。カードが最後に読んだ記録に無ければ (片付けた等) 空。
func (b *Backend) Activity(cardID string) ([]backend.Activity, error) {
	c, ok := b.card(cardID)
	if !ok {
		return nil, nil
	}
	b.actMu.Lock()
	defer b.actMu.Unlock()
	a := b.logs[cardID]
	if a == nil || a.log.session != c.Session { // 起動の記録にカードの無い古い行は、今の短い id で拾う
		a = &cardActivity{log: NewCardLog(b.registry, b.findPath, cardID, c.Session)}
		b.logs[cardID] = a
	}
	got, err := a.log.Next()
	// 後から見つかった前の session の分は、今までの分より古いことがある: 時刻の位置に差し込む (同じ時刻は前からの順のまま)
	a.items = append(a.items, got...)
	slices.SortStableFunc(a.items, func(x, y backend.Activity) int { return x.At.Compare(y.At) })
	a.items = tail(a.items, activityKeep)
	return slices.Clone(a.items), err
}

// fullSessionID は pro-con が起動した session の短い id から session id を引く (今の記録、無ければ退いた側)。
func (b *Backend) fullSessionID(id string) (string, error) {
	reg, err := LoadRegistry(b.registry)
	if err != nil {
		return "", err
	}
	retired, err := LoadRetired(b.registry)
	if err != nil {
		return "", err
	}
	for _, o := range append(reg, retired...) {
		if id != "" && o.ID == id && o.SessionID != "" {
			return o.SessionID, nil
		}
	}
	return "", errors.New("pro-con が起動した session の記録に無い: " + id)
}

// requestRunes は依頼の原文を詳細に出す上限 (文字数)。
const requestRunes = 2000

// Describe はヘッダーに出す、この backend の説明。
func (b *Backend) Describe() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case !b.ready:
		return "live: 読み込み中… (模擬は pro-con --mock)"
	case b.pending > 0:
		return fmt.Sprintf("live: 受付の箱に適用待ち %d 件 (pro-con dispatcher が動いていない? 模擬は pro-con --mock)", b.pending)
	}
	return "live: 本物のカード (操作は受付の箱へ。dispatcher が適用する。模擬は pro-con --mock)"
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

func tail[T any](xs []T, n int) []T {
	if len(xs) <= n {
		return xs
	}
	return xs[len(xs)-n:]
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return l
}
