// Package supervisor は子プロセスを 1 つ起こして見張る (foreman / supervisord の 1 本ぶん。常駐の登録はしない = 呼んだプロセスが生きている間だけ)。
//
// 行うこと:
//   - 子を起こし、抜けたら間を空けて起こし直す
//   - 短い間 (CrashWindow) に落ち続けたら (CrashLimit を超えたら) 諦める。起こし直し続けて CPU や外の枠を焼かない
//   - 止める合図 (ctx の取り消し) で子を止める: 生命線を閉じる → StopSignal → StopWait 待って SIGKILL
//   - 生命線 (Lifeline): 子の stdin を親だけが書く側を握るパイプにする。親がどう死んでも (kill -9 でも) OS がパイプを閉じるので、
//     stdin の EOF で抜ける子は親と一緒に抜ける (macOS には PDEATHSIG が無い)
//
// 決めないこと (呼ぶ側が Spec の関数で決める): 子の終わり方の意味 (Classify)、起こし直す前に続けてよいか (Continue)、
// 出来事の言葉 (OnEvent)。
//
// 失敗モード:
//   - 子が抜けたのと止める合図が同時に来た → 止める合図を優先する (ReasonStopped。落ちたと数えない)
//   - 子が StopSignal を無視する → StopWait の後に SIGKILL。生命線があれば EOF で先に抜けうる
//   - 起こせない (実行ファイルが無い等) → 起こし直しても直らないので、出来事にして諦める (ReasonStartFailed)
//
// 🚨 止める信号は子のプロセスだけに送る (プロセスグループには送らない)。子が孫を持つなら、子が自分で止める。
// 🚨 親のファイル (lock 等) を子へ渡さない: Go の開くファイルは CLOEXEC なので、ExtraFiles に入れない限り渡らない。
package supervisor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"sync"
	"syscall"
	"time"
)

// Outcome は子が抜けたときの扱い (Spec.Classify が決める)。
type Outcome int

const (
	// Crash は落ちた: 数えて、RestartWait 後に起こし直す (CrashLimit を超えたら諦める)。
	Crash Outcome = iota
	// Retry は今は動けない (別の実体が lock を持っている等): 数えずに、RetryWait 後に起こし直す。
	Retry
	// Done は子が自分の判断で終えた: 起こし直さずに見張りを終える。
	Done
)

// EventKind は出来事の種類。
type EventKind int

const (
	// EventCrashed は落ちたので Wait 後に起こし直す。
	EventCrashed EventKind = iota + 1
	// EventRetrying は今は動けないので Wait 後に起こし直す (Repeat なら直前も同じ)。
	EventRetrying
	// EventGaveUp は落ち続けたので起こし直さない。
	EventGaveUp
	// EventStartFailed は起こせなかったので起こし直さない。
	EventStartFailed
)

// Event は OnEvent に渡す出来事。
type Event struct {
	Kind    EventKind
	Err     error         // 子の Wait の結果 (nil は rc=0) か、起こせなかった理由
	Wait    time.Duration // 起こし直すまでの間 (EventCrashed / EventRetrying)
	Crashes int           // CrashWindow の間に落ちた回数 (EventCrashed / EventGaveUp)
	Repeat  bool          // EventRetrying が続いている (直前の出来事も EventRetrying)
}

// Reason は見張りを終えた理由。
type Reason int

const (
	// ReasonStopped は止める合図 (ctx の取り消し) で子を止めた。
	ReasonStopped Reason = iota + 1
	// ReasonDone は子が自分の判断で終えた (Classify が Done)。
	ReasonDone
	// ReasonHalted は起こし直す前に Continue が偽を返した。
	ReasonHalted
	// ReasonGaveUp は落ち続けたので諦めた。
	ReasonGaveUp
	// ReasonStartFailed は起こせなかった。
	ReasonStartFailed
)

// Result は見張りを終えた理由と、最後の子の Wait の結果 (起こせなかったならその理由)。
type Result struct {
	Reason Reason
	Err    error
}

// Spec は 1 つの子の見張り方。
type Spec struct {
	// Command は起こすたびに新しい Cmd を作る (Cmd は 1 度しか Start できない)。Lifeline なら Stdin は上書きする。
	Command func() (*exec.Cmd, error)
	// Lifeline なら子の stdin を生命線のパイプにする (親が死ねば子の stdin が EOF になる)。
	Lifeline bool
	// Classify は子の Wait の結果 (nil は rc=0) を扱いに分ける。nil ならすべて Crash。
	Classify func(err error) Outcome
	// Continue は起こし直す前 (初回の起動の前は呼ばない) に呼ぶ。偽なら起こさずに終える (ReasonHalted)。nil なら常に続ける。
	Continue func() bool
	// OnEvent は出来事を受ける (Run を回す goroutine から順に呼ぶ)。nil なら捨てる。
	OnEvent func(Event)

	RestartWait time.Duration // 落ちてから起こし直すまで
	RetryWait   time.Duration // Retry のとき、起こし直すまで
	CrashLimit  int           // CrashWindow の間に落ちてよい回数 (超えたら諦める)
	CrashWindow time.Duration
	StopSignal  syscall.Signal // 止めるときに送る信号 (0 なら SIGTERM)
	StopWait    time.Duration  // StopSignal から SIGKILL までの待ち

	Now func() time.Time // 落ちた回数を数える時計 (nil なら time.Now)
}

// Run は子を起こし、見張りを終えるまで戻らない。
func Run(ctx context.Context, s Spec) Result {
	now := s.Now
	if now == nil {
		now = time.Now
	}
	emit := func(e Event) {
		if s.OnEvent != nil {
			s.OnEvent(e)
		}
	}
	var crashes []time.Time
	retrying := false
	for first := true; ; first = false {
		if ctx.Err() != nil {
			return Result{Reason: ReasonStopped}
		}
		if !first && s.Continue != nil && !s.Continue() {
			return Result{Reason: ReasonHalted}
		}
		err, started := s.runOnce(ctx)
		if !started {
			emit(Event{Kind: EventStartFailed, Err: err})
			return Result{Reason: ReasonStartFailed, Err: err}
		}
		if ctx.Err() != nil { // 止める途中に抜けた / 抜けたのと止める合図が重なった
			return Result{Reason: ReasonStopped, Err: err}
		}
		outcome := Crash
		if s.Classify != nil {
			outcome = s.Classify(err)
		}
		switch outcome {
		case Done:
			return Result{Reason: ReasonDone, Err: err}
		case Retry:
			emit(Event{Kind: EventRetrying, Err: err, Wait: s.RetryWait, Repeat: retrying})
			retrying = true
			sleep(ctx, s.RetryWait)
			continue
		case Crash:
		}
		retrying = false
		at := now()
		crashes = append(slices.DeleteFunc(crashes, func(c time.Time) bool { return at.Sub(c) > s.CrashWindow }), at)
		if len(crashes) > s.CrashLimit {
			emit(Event{Kind: EventGaveUp, Err: err, Crashes: len(crashes)})
			return Result{Reason: ReasonGaveUp, Err: err}
		}
		emit(Event{Kind: EventCrashed, Err: err, Wait: s.RestartWait, Crashes: len(crashes)})
		sleep(ctx, s.RestartWait)
	}
}

// Start は Run を裏で回す。返した関数は見張りを止め (子も止める)、終えるまで待って結果を返す。何度呼んでもよい。
// ctx の取り消しでも止まる (そのときも返した関数で結果を受け取れる)。
func Start(ctx context.Context, s Spec) (stop func() Result) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan Result, 1)
	go func() { done <- Run(ctx, s) }()
	var once sync.Once
	var res Result
	return func() Result {
		once.Do(func() {
			cancel()
			res = <-done
		})
		return res
	}
}

// runOnce は子を 1 回起こして、抜けるか ctx が終わる (子を止める) まで待つ。started が偽なら起こせなかった (err はその理由)。
func (s Spec) runOnce(ctx context.Context) (err error, started bool) {
	cmd, err := s.Command()
	if err != nil {
		return err, false
	}
	var lifeline *os.File // 書く側 (親だけが持つ)
	if s.Lifeline {
		r, w, err := os.Pipe()
		if err != nil {
			return err, false
		}
		cmd.Stdin, lifeline = r, w
		defer func() { _ = w.Close() }()
		// 読む側は子だけが使う。Start の後に閉じる (Start が失敗しても閉じる)。
		// EOF を決めるのは書く側の複製の数: 書く側は CLOEXEC なので子へは渡らず、親だけが持つ
		defer func() { _ = r.Close() }()
		if err := cmd.Start(); err != nil {
			return err, false
		}
		_ = r.Close()
	} else if err := cmd.Start(); err != nil {
		return err, false
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		return err, true
	case <-ctx.Done():
	}
	if lifeline != nil {
		_ = lifeline.Close() // EOF で抜ける子はここで抜ける
	}
	sig := s.StopSignal
	if sig == 0 {
		sig = syscall.SIGTERM
	}
	_ = cmd.Process.Signal(sig)
	t := time.NewTimer(s.StopWait)
	defer t.Stop()
	select {
	case err = <-exited:
	case <-t.C:
		_ = cmd.Process.Kill()
		err = <-exited
	}
	return err, true
}

// ExitCode は子の Wait の結果の終了コード。nil なら 0、信号で死んだ・終了コードを持たない失敗なら -1。
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

// sleep は d だけ待つ (ctx が終われば先に戻る)。
func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
