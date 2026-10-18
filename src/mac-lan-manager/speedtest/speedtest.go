// Package speedtest は相手の Mac との通信速度を ssh 越しに測る。命令を組んで流し、数えるだけで、プロセスを起こす口 (Start) と
// 時計 (Clock) は呼び出し側が渡す (本物は live、テストは偽物)。
//
// 値は ssh の暗号化を通した速さで、回線そのものの速さではない。100 Mbps の頭打ちや無線の遅さは見分けられるが、1 Gbps を超える回線は
// 暗号化の速さで頭打ちになりうる (回線の速さが要るなら iperf3 を両方に入れる)。
package speedtest

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"mac-lan-manager/setup"
)

// Duration は上り・下りそれぞれを流す時間。量ではなく時間で切る (Thunderbolt と Wi-Fi で同じ量だと所要が桁で違う)。
const Duration = 5 * time.Second

// downMax は下りで相手に出させる量の上限 (Duration の間に 10 Gbps でも届かない量)。こちらが止め損ねても相手の head が自分で終わる。
const downMax = 8_000_000_000

const chunk = 256 << 10

// Proc は起こした ssh 1 本。Stdin は起こすときに頼んだときだけ非 nil。Wait は stdout を読み終えてから呼ぶ。
type Proc interface {
	Stdin() io.WriteCloser
	Stdout() io.Reader
	Wait() error // 失敗の文には stderr を含める (鍵で入れないかを "Permission denied (" で分ける)
}

// Start は argv を起こす。ctx が終わればプロセスを止める。
type Start func(ctx context.Context, argv []string, withStdin bool) (Proc, error)

// Clock は今の時刻と、d 後に 1 度届くチャネル。
type Clock struct {
	Now   func() time.Time
	After func(d time.Duration) <-chan time.Time
}

// Result は測った結果 (bit/s)。Err があれば値は使わない。
type Result struct {
	UpBps, DownBps float64
	Err            error
}

// ErrNoKey は鍵で入れない (BatchMode の ssh が Permission denied)。測る前に分かる。
var ErrNoKey = errors.New("鍵で入れないので測れない (初期設定で鍵を置く)")

// CheckArgv / UpArgv / DownArgv は測定で起こす ssh。BatchMode と接続の使い回し (SSHArgv の batch) で、最初の check が張った接続を
// 上り・下りが使う (繋ぎ直しの時間を測定に入れない)。
func CheckArgv(d setup.Dest) []string { return d.SSHArgv(false, true, "true") }

// UpArgv は相手が受け取った量を数えて返す (こちらが書けた量はパイプと ssh の窓に溜まった分だけ多い)。
func UpArgv(d setup.Dest) []string { return d.SSHArgv(false, true, "exec wc -c") }

// DownArgv は相手に 0 を流させる。
func DownArgv(d setup.Dest) []string {
	return d.SSHArgv(false, true, fmt.Sprintf("exec head -c %d /dev/zero", downMax))
}

// Measure は鍵で入れるかを確かめてから、上り・下りを dur ずつ測る。
func Measure(ctx context.Context, d setup.Dest, start Start, clk Clock, dur time.Duration) Result {
	if err := d.Validate(); err != nil {
		return Result{Err: err}
	}
	if err := check(ctx, d, start); err != nil {
		return Result{Err: err}
	}
	up, err := measureUp(ctx, d, start, clk, dur)
	if err != nil {
		return Result{Err: fmt.Errorf("上りを測れなかった: %w", err)}
	}
	down, err := measureDown(ctx, d, start, clk, dur)
	if err != nil {
		return Result{Err: fmt.Errorf("下りを測れなかった: %w", err)}
	}
	return Result{UpBps: up, DownBps: down}
}

func check(ctx context.Context, d setup.Dest, start Start) error {
	p, err := start(ctx, CheckArgv(d), false)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, p.Stdout())
	if err := p.Wait(); err != nil {
		if strings.Contains(err.Error(), "Permission denied (") {
			return ErrNoKey
		}
		return fmt.Errorf("繋げなかった: %w", err)
	}
	return nil
}

func measureUp(ctx context.Context, d setup.Dest, start Start, clk Clock, dur time.Duration) (float64, error) {
	p, err := start(ctx, UpArgv(d), true)
	if err != nil {
		return 0, err
	}
	in := p.Stdin()
	if in == nil {
		return 0, errors.New("stdin が無い")
	}
	t0 := clk.Now()
	stop := clk.After(dur)
	// 時間が来たら stdin を閉じる (相手の wc に終わりを知らせる)。書き込みのループの外から閉じる: 相手が読まずに Write が詰まっても、
	// 閉じれば Write が戻る (ループの中で時計を見ると、詰まった Write の間は SpeedTimeout まで止まらなかった)
	var timeUp atomic.Bool
	done, exited := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(exited)
		select {
		case <-stop:
			timeUp.Store(true)
			_ = in.Close()
		case <-done:
		}
	}()
	buf := make([]byte, chunk)
	var werr error
	for werr == nil {
		_, werr = in.Write(buf)
	}
	close(done)
	<-exited // 閉じる側が終わってから判定する (stop と done が同時に来ても 2 度閉じない)
	if timeUp.Load() {
		werr = nil // 閉じたので書けなくなった (失敗ではない)
	} else {
		_ = in.Close()
	}
	out, rerr := io.ReadAll(p.Stdout())
	waitErr := p.Wait()
	t1 := clk.Now() // 相手が全部を受け取って数え終えた時刻 (こちらで溜まっていた分も含めて届いた)
	switch {
	case waitErr != nil:
		return 0, waitErr
	case werr != nil:
		return 0, werr
	case rerr != nil:
		return 0, rerr
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("相手の数えた量が読めない: %q", strings.TrimSpace(string(out)))
	}
	return rate(n, t1.Sub(t0))
}

func measureDown(ctx context.Context, d setup.Dest, start Start, clk Clock, dur time.Duration) (float64, error) {
	pctx, cancel := context.WithCancel(ctx)
	defer cancel()
	p, err := start(pctx, DownArgv(d), false)
	if err != nil {
		return 0, err
	}
	got := make(chan int)
	readErr := make(chan error, 1)
	go func() {
		r := bufio.NewReaderSize(p.Stdout(), chunk)
		buf := make([]byte, chunk)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				select {
				case got <- n:
				case <-pctx.Done():
					readErr <- pctx.Err()
					return
				}
			}
			if err != nil {
				readErr <- err
				return
			}
		}
	}()
	// 時間は最初の 1 回が届いてから数える (接続と相手の head の起動を測定に入れない)。最初の 1 回の量は数えない (届いた瞬間が起点)
	var t0 time.Time
	var stop <-chan time.Time
	var total int64
	var stopped bool
	var rerr error
loop:
	for {
		select {
		case n := <-got:
			if stop == nil {
				t0, stop = clk.Now(), clk.After(dur)
				continue
			}
			total += int64(n)
		case <-stop:
			stopped = true
			break loop
		case rerr = <-readErr:
			break loop
		case <-ctx.Done():
			cancel()
			_ = p.Wait() // 取り消しでも ssh を回収する (回収しないと glogx の中で取り消すたびにゾンビと fd が溜まる)
			return 0, ctx.Err()
		}
	}
	t1 := clk.Now()
	cancel() // 相手の head は ssh が切れたところで書き先を失って終わる (止め損ねても downMax で終わる)
	waitErr := p.Wait()
	switch {
	case stop == nil:
		return 0, fmt.Errorf("何も届かなかった: %w", errors.Join(rerr, waitErr))
	case !stopped && !errors.Is(rerr, io.EOF):
		return 0, fmt.Errorf("途中で切れた: %w", errors.Join(rerr, waitErr))
	}
	return rate(total, t1.Sub(t0))
}

func rate(bytes int64, d time.Duration) (float64, error) {
	if d <= 0 {
		return 0, errors.New("測った時間が 0")
	}
	return float64(bytes) * 8 / d.Seconds(), nil
}

// Format は bit/s を画面の字にする。
func Format(bps float64) string {
	switch {
	case bps >= 999.5e6: // 丸めて「1000 Mbps」と出さない
		return fmt.Sprintf("%.1f Gbps", bps/1e9)
	case bps < 0.95e6: // 丸めて「0 Mbps」と出さない (何も流れなかったと読まれる)
		return fmt.Sprintf("%.2f Mbps", bps/1e6)
	}
	return fmt.Sprintf("%.0f Mbps", bps/1e6)
}

// Capped100M は上り・下りの速い方が 100 Mbps 付近で止まっているか (途中に 100BASE-TX の機器・ケーブルがある疑い)。
// 100 Mbps の線は ssh を通すと 90 前後になる。80 未満は 100M の頭打ちでなく、もっと遅い何か (無線・混雑) と見る。
func Capped100M(r Result) bool {
	fast := max(r.UpBps, r.DownBps)
	return r.Err == nil && fast >= 80e6 && fast < 100e6
}
