package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Throttle は slack-cli の呼び出しを 1 列に並べ、呼び出しの間を空ける Runner (Slack のレート制限の作法を守る。ユーザー要望 2026-11-11:
// 都度呼ぶと制限に当たる)。読み直しを全チャンネルで同時に始めても (R)、slack-cli は 1 本ずつ、決まった間を空けて走る。
//
// 列はプロセスをまたぐ: slack-cli を走らせている間だけ lockPath のファイルの flock を持ち、最後の呼び出しの終わりと、レート制限で
// 止めた期限をファイルに書く。読む画面と 2 段の実行 (prepare) が同時に走っても、Slack へ届く頻度は合計で数える。
//
// 🚨 間を空ける待ちの間は flock を持たない。持ったまま待つと、続けて呼ぶプロセスが flock を手放す隙がマイクロ秒しか無く、
// 他のプロセスが 1 回も呼べなくなる (飢える)。待ちは flock を放してから行い、起きたら状態を読み直す。
//
// 守れないもの: slack-cli 1 回の中のページ送りの間は空けられない (slack-cli 自身が空ける。slack-cli の internal/slack/retry.go の pace)。
type Throttle struct {
	r      Runner
	g      *gate
	notify io.Writer // レート制限で止めることを知らせる先 (端末の stderr)。nil なら知らせない (読む画面の中。警告は板の注記に載る)
}

type gate struct {
	tok      chan struct{} // プロセスの中の列 (ctx で待つのをやめられるよう、Mutex でなく容量 1 の chan)
	lockPath string        // "" ならプロセスの中だけで並べる
	now      func() time.Time
	sleep    func(ctx context.Context, d time.Duration) error // 間を空ける待ち
	poll     func(ctx context.Context, d time.Duration) error // 他のプロセスが slack-cli を走らせている間の取り直しの待ち
	gap      func(cmd string) time.Duration
	floor    time.Duration // メソッドを問わず、前の呼び出しの終わりから必ず空ける間
	mem      paceState     // lockPath が無い・使えないときの覚え
	warned   bool          // ファイルを使えないことを知らせたか (呼び出しのたびに注記へ足さない)

	stats callStats // 呼び出しの様子 (Stats。画面の goroutine からも読むので mu で守る)
}

// callStats は列の様子の記録 (このプロセスの呼び出しだけ。別に動かしている slack-bbs の分は入らない)。
type callStats struct {
	mu       sync.Mutex
	starts   []time.Time // 直近の呼び出しの始まり (statsWindow より古いものは捨てる)
	waiting  int         // 列で番を待っている呼び出しの数
	sleeping bool        // 番を持ったまま、間を空けて (止めた期限まで) 待っている
	running  bool        // slack-cli を走らせている最中
	paused   time.Time   // レート制限で止めている期限
}

// statsWindow は呼び出しの頻度を数える窓。
const statsWindow = time.Minute

// Stats は Slack の呼び出しの列の様子 (読む画面の右下に出す)。
type Stats struct {
	LastMinute int           // 直近 1 分に始めた呼び出しの数
	MeanGap    time.Duration // 直近 1 分の呼び出しの始まりの間の平均 (2 回未満なら 0)
	Waiting    int           // まだ走っていない呼び出しの数 (列で番を待つものと、番を持って間を空けているもの)
	Running    bool          // slack-cli を走らせている最中
	PausedFor  time.Duration // レート制限で止めている残り (0 なら止めていない)
}

// Stats は列の様子を返す (このプロセスの呼び出しだけ)。
func (t *Throttle) Stats() Stats {
	g := t.g
	now := g.now()
	st := &g.stats
	st.mu.Lock()
	defer st.mu.Unlock()
	st.trim(now)
	out := Stats{LastMinute: len(st.starts), Waiting: st.waiting, Running: st.running}
	if st.sleeping {
		out.Waiting++ // 番を持って間を空けている 1 本も、まだ走っていないので待ちに数える
	}
	if n := len(st.starts); n >= 2 {
		out.MeanGap = st.starts[n-1].Sub(st.starts[0]) / time.Duration(n-1)
	}
	if d := st.paused.Sub(now); d > 0 {
		out.PausedFor = d
	}
	return out
}

func (st *callStats) trim(now time.Time) {
	i := 0
	for i < len(st.starts) && now.Sub(st.starts[i]) > statsWindow {
		i++
	}
	st.starts = st.starts[i:]
}

// String は画面に出す 1 行 (例「Slack 12回/分 間隔 2.3秒 待ち 3」。止めている間は「Slack 429 で停止中 あと 25 秒」)。
// 数えているのはこのプロセスの呼び出しだけ。
func (s Stats) String() string {
	if s.PausedFor > 0 {
		return fmt.Sprintf("Slack 429 で停止中 あと %d 秒", int((s.PausedFor+time.Second-1)/time.Second))
	}
	out := fmt.Sprintf("Slack %d回/分", s.LastMinute)
	if s.MeanGap > 0 {
		out += fmt.Sprintf(" 間隔 %.1f秒", s.MeanGap.Seconds())
	}
	if s.Waiting > 0 {
		out += fmt.Sprintf(" 待ち %d", s.Waiting)
	}
	return out
}

// Active は描き直し続ける価値があるか (直近 1 分に呼び出しがある・待っている・止めている)。
func (s Stats) Active() bool {
	return s.LastMinute > 0 || s.Waiting > 0 || s.Running || s.PausedFor > 0
}

// paceState は列の状態 (lockPath のファイルの中身)。
//
// 間はサブコマンドごとの最後の終わり (LastEndBy) から測る。最後の呼び出し (どのサブコマンドでも) からは floor だけ空ける。
// 最後の呼び出しの終わりから自分の段の間を測ると、間の短いサブコマンド (history) を続けて呼ぶプロセスがいる間、
// 間の長いサブコマンド (search) は起きるたびに後回しになり、相手が終わるまで呼べない。
type paceState struct {
	LastEnd     time.Time            `json:"last_end"`              // 最後の呼び出し (どのサブコマンドでも) が終わった時刻
	LastEndBy   map[string]time.Time `json:"last_end_by,omitempty"` // サブコマンドごとの最後の呼び出しが終わった時刻
	PausedUntil time.Time            `json:"paused_until"`          // レート制限で止めた期限 (この時刻まで次を呼ばない)
}

// NewThrottle は r の呼び出しを並べる Runner を返す。lockPath はプロセスをまたいで列を共有するファイル (空ならプロセスの中だけ)。
// notify はレート制限で止めることを知らせる先 (nil なら知らせない)。
func NewThrottle(r Runner, lockPath string, notify io.Writer) *Throttle {
	return &Throttle{r: r, notify: notify, g: &gate{
		tok: make(chan struct{}, 1), lockPath: lockPath, now: time.Now, sleep: sleepCtx, poll: sleepCtx, gap: gapBefore, floor: gapFloor,
	}}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// 呼び出しの間。Slack の Web API のレート制限の段 (https://docs.slack.dev/apis/web-api/rate-limits) に合わせる:
// search.messages・users.list・conversations.list は Tier 2 (1 分に 20 回ほど)、conversations.history・replies は Tier 3 (50 回ほど)。
// 間は段の上限ちょうどで、余りは呼び出しにかかる時間の分だけ。Slack の画面など他の道具の呼び出しと合わせて超えたら、429 の扱い (下) が拾う。
const (
	gapTier2   = 3 * time.Second
	gapTier3   = 1200 * time.Millisecond
	gapDefault = time.Second
	gapFloor   = 500 * time.Millisecond // サブコマンドが違っても続けて打たない

	// rateLimitWait は待つ秒数が分からない 429 (Retry-After が HTTP-date など) で列を止める長さ。
	rateLimitWait = 30 * time.Second
	// maxRetryWait より長い待ちを指定された 429 は、やり直さずに列を止めて誤りを返す (人が待てる長さ。slack-cli の defaultRetryMaxWait と同じ)。
	maxRetryWait = 60 * time.Second
	// maxPause は列を止める長さの上限。ファイルの時刻がこれより先なら壊れた・時計がずれたとみて詰める (止まったまま戻らなくしない)。
	maxPause = 5 * time.Minute
	// lockPoll は他のプロセスが slack-cli を走らせている間に、flock を取り直すまで待つ間。
	lockPoll = 50 * time.Millisecond
)

// gapBefore は slack-cli のサブコマンド cmd を呼ぶ前に、前の呼び出しの終わりから空ける間。
func gapBefore(cmd string) time.Duration {
	switch cmd {
	case "search", "users", "channels":
		return gapTier2
	case "history", "thread":
		return gapTier3
	}
	return gapDefault
}

// rateLimitRe は slack-cli が諦めたときの最終の誤り (internal/slack/retry.go の RateLimitError.Error:
// 「レート制限（429）: <method>。<N 秒|しばらく…>待って再実行してください」)。slack-cli が待ってやり直すたびに stderr へ出す
// 「…待って再試行します」の行 (callRetrying) とは「再実行してください」で見分ける。slack-cli が文言を変えたら、ここも合わせる
// (合わないと 429 で列を止めず、誤りをそのまま返すだけ)。
var rateLimitRe = regexp.MustCompile(`レート制限（429）: [^\n。]*。(?:(\d+) 秒|しばらく[^\n]*?)待って再実行してください`)

// rateLimited は誤りが slack-cli のレート制限で諦めたものか、列を止める長さ、やり直してよいか。最後に出た誤りの行で決める。
// やり直すのは待つ秒数が書いてあって maxRetryWait 以下のときだけ (それより長い・分からない待ちの前にやり直すと、サーバーの指定より早く打つ)。
func rateLimited(err error) (wait time.Duration, retry, ok bool) {
	if err == nil {
		return 0, false, false
	}
	ms := rateLimitRe.FindAllStringSubmatch(err.Error(), -1)
	if ms == nil {
		return 0, false, false
	}
	m := ms[len(ms)-1]
	n, e := strconv.Atoi(m[1])
	switch {
	case e != nil:
		return rateLimitWait, false, true
	case time.Duration(n)*time.Second > maxRetryWait:
		return min(time.Duration(n)*time.Second, maxPause), false, true
	}
	return time.Duration(n) * time.Second, true, true
}

// Run は列に並び、前の呼び出しから間を空けて r を呼ぶ。レート制限で止める指定があれば列ごと止め、短ければ待ってから 1 回だけやり直す。
// 待ったこと・列をプロセスの間で共有できなかったこと (1 回だけ) は、stderr の頭に「警告:」の行で足す (Client が板の注記に載せる)。
func (t *Throttle) Run(ctx context.Context, args ...string) ([]byte, []byte, error) {
	g := t.g
	g.stats.add(func(st *callStats) { st.waiting++ })
	select {
	case g.tok <- struct{}{}:
		g.stats.add(func(st *callStats) { st.waiting-- })
	case <-ctx.Done():
		g.stats.add(func(st *callStats) { st.waiting-- })
		return nil, nil, ctx.Err()
	}
	defer func() { <-g.tok }()
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	var warns []string
	for attempt := 0; ; attempt++ {
		out, errb, w, err := t.runInTurn(ctx, cmd, args)
		warns = append(warns, w...)
		wait, retry, limited := rateLimited(err)
		if limited {
			if t.notify != nil {
				fmt.Fprintf(t.notify, "slack-bbs: Slack のレート制限に当たった。%d 秒は Slack を呼ばない\n", int(wait/time.Second))
			}
			if retry && attempt == 0 && ctx.Err() == nil {
				warns = append(warns, fmt.Sprintf("警告: Slack のレート制限に当たったので、%d 秒待って slack %s をやり直した", int(wait/time.Second), cmd))
				continue // 止めた期限は runInTurn がファイルに書いた。次の runInTurn がその期限まで待つ
			}
		}
		if slices.ContainsFunc(warns, isShareWarning) && (err != nil || !warningsShown[cmd]) {
			g.warned = false // 警告が板に届かない呼び出しだったので、次の呼び出しで出し直す
		}
		if len(warns) > 0 {
			var head []byte
			for _, w := range warns {
				head = append(head, w+"\n"...)
			}
			errb = append(head, errb...)
		}
		return out, errb, err
	}
}

// runInTurn は自分の番まで待ってから r を 1 回呼び、終わった時刻 (とレート制限で止める期限) を列の状態に書く。
// 番を待つ間は flock を持たない: flock を取って状態を読み、まだ早ければ放して待ち、起きたら読み直す。
func (t *Throttle) runInTurn(ctx context.Context, cmd string, args []string) (out, errb []byte, warns []string, err error) {
	g := t.g
	for {
		st, save, unlock, lerr := g.lock(ctx)
		if lerr != nil {
			if ctx.Err() != nil {
				return nil, nil, warns, ctx.Err()
			}
			if !g.warned {
				g.warned = true
				warns = append(warns, shareWarning+lerr.Error())
			}
		}
		now := g.now()
		if fixed, ok := clampState(st, now); ok {
			st = fixed
			save(st) // 先の時刻を書き直す (読むたびに詰めると、詰めた期限が動き続けて誰も呼べない)
		}
		wake := st.LastEnd.Add(g.floor)
		for _, w := range []time.Time{st.LastEndBy[cmd].Add(g.gap(cmd)), st.PausedUntil} {
			if w.After(wake) {
				wake = w
			}
		}
		if d := wake.Sub(now); d > 0 {
			unlock()
			paused := st.PausedUntil // 別のプロセスが 429 で止めた期限も、ファイルから読んだこの値で画面に出す
			g.stats.add(func(cs *callStats) {
				cs.sleeping = true
				if paused.After(cs.paused) {
					cs.paused = paused
				}
			})
			err := g.sleep(ctx, d)
			g.stats.add(func(cs *callStats) { cs.sleeping = false })
			if err != nil {
				return nil, nil, warns, err
			}
			continue
		}
		begin := g.now()
		g.stats.add(func(cs *callStats) { cs.starts, cs.running = append(cs.starts, begin), true })
		out, errb, err = t.r.Run(ctx, args...)
		g.stats.add(func(cs *callStats) { cs.running = false })
		st.LastEnd = g.now()
		if st.LastEndBy == nil {
			st.LastEndBy = map[string]time.Time{}
		}
		st.LastEndBy[cmd] = st.LastEnd
		if wait, _, limited := rateLimited(err); limited {
			st.PausedUntil = st.LastEnd.Add(wait)
			g.stats.add(func(cs *callStats) { cs.paused = st.PausedUntil })
		}
		save(st)
		unlock()
		return out, errb, warns, err
	}
}

// warningsShown は stderr の警告を板の注記へ載せるサブコマンド (Client の History・Thread)。他 (whoami・users・channels・search) は
// 警告を受け取らないので、そこで出した 1 回きりの警告は届かない。Client が誤りのとき (JSON が読めないときも) stderr を捨てるのも同じ
// (rc=0 で JSON が読めない形は Throttle から見えず、その 1 回の警告は落ちる。稀なので受け入れる)。
var warningsShown = map[string]bool{"history": true, "thread": true}

const shareWarning = "警告: Slack の呼び出しの列をプロセスの間で共有できない (この slack-bbs の中だけで間を空ける): "

func isShareWarning(w string) bool { return strings.HasPrefix(w, shareWarning) }

// clampState は列の状態の時刻が now から見て先すぎれば詰める (壊れたファイル・時計を戻した・手で書いた)。詰めたら ok。
func clampState(st paceState, now time.Time) (paceState, bool) {
	ok := false
	if st.LastEnd.After(now) {
		st.LastEnd, ok = now, true
	}
	for k, v := range st.LastEndBy {
		if v.After(now) {
			st.LastEndBy = maps.Clone(st.LastEndBy) // 呼び出し側の覚え (g.mem) の map を書き換えない
			st.LastEndBy[k], ok = now, true
		}
	}
	if st.PausedUntil.After(now.Add(maxPause)) {
		st.PausedUntil, ok = now.Add(maxPause), true
	}
	return st, ok
}

// lock は flock を取り (他のプロセスが slack-cli を走らせている間は poll で待って取り直す)、列の状態を読む。save は状態を書き、
// unlock は flock を放す。ファイルが使えなければ err を返し、プロセスの中の覚えで続ける (save / unlock は使える)。
func (g *gate) lock(ctx context.Context) (st paceState, save func(paceState), unlock func(), err error) {
	memSave := func(s paceState) { g.mem = s }
	if g.lockPath == "" {
		return g.mem, memSave, func() {}, nil
	}
	f, err := openLock(g.lockPath)
	if err != nil {
		return g.mem, memSave, func() {}, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = f.Close()
			return g.mem, memSave, func() {}, err
		}
		if err := g.poll(ctx, lockPoll); err != nil {
			_ = f.Close()
			return g.mem, memSave, func() {}, err
		}
	}
	st = g.mem
	if b, rerr := os.ReadFile(g.lockPath); rerr == nil && len(b) > 0 {
		var fs paceState
		if json.Unmarshal(b, &fs) == nil { // 壊れた中身は捨てて、プロセスの中の覚えで続ける (次の save が書き直す)
			st = fs
		}
	}
	save = func(s paceState) {
		g.mem = s
		if b, err := json.Marshal(s); err == nil {
			_ = f.Truncate(0)
			_, _ = f.WriteAt(b, 0) // 書けなくても、このプロセスの中の間は g.mem が守る (他のプロセスが間を詰めうるだけ)
		}
	}
	return st, save, func() { _ = f.Close() }, nil // Close が flock を放す
}

func openLock(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
}

// add は列の様子の記録を書き換える (画面の goroutine の Stats と並んで走るので mu で守る)。
func (st *callStats) add(f func(*callStats)) {
	st.mu.Lock()
	defer st.mu.Unlock()
	f(st)
}
