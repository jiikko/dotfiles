package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// doctorCleanup は走行中の doctor 走査 (disk / svc / brew) の完了 latch。
//
// なぜ要るか (issue 211): `doctorView.stop()` は ctx を cancel するだけで、**子プロセスの死を
// 待たない**。`runner.Exec` の kill は `exec.CommandContext` が起こす watchdog goroutine が
// `Kill(-pgid)` を打つ形なので、cancel が戻った時点では brew の子孫 (bash → ruby → git) は
// まだ生きている (red team 実測 3/3: cancel 直後 alive=true / 2 秒後 alive=false)。
//
// 再起動 (`r`) は `syscall.Exec` でプロセス像を置き換えるため、**その watchdog goroutine ごと
// 消える**。子は Setpgid で別プロセスグループなので端末の SIGINT も届かず、走り続ける
// (pid は保たれるので厳密には「孤児」ではなく、誰も wait しないゾンビになる)。
//
// 形は pullCleanup (external_commands.go) と同じ: Add は走査を起こす側 (doctorView.start)、
// Wait は main.go。**pull 用の latch とは別に持つ**: 待ちの上限も出すメッセージも別で、
// 片方の遅れをもう片方の理由として表示すると診断を誤らせる。
// scanLatch は「走行中の走査の本数」を数える latch。
//
// 🚨 **sync.WaitGroup は使えない**。svc / brew / 削除は tea.Cmd の closure から登録するので、
// **Add が Wait と同時に走りうる**。WaitGroup はカウント 0 のときの Add と Wait の同時実行を
// 禁じており、`-race` が実際にデータ競合として検出した (実測 2026-09-03、敵対的レビューの
// 指摘を受けて -count=3 で再現)。状態を全部 mutex の下に置いて、遅れて来る登録も安全にする。
//
// 遅れた登録を Wait が取りこぼすのは**許容する**: まだ始まっていない Cmd は子プロセスを
// 起こしていないので、その時点で終了・再起動しても孤児は生まれない (closure が始まれば、
// 子を起こす前に add が済んでいる)。
type scanLatch struct {
	mu    sync.Mutex
	n     int
	waitC chan struct{} // 誰かが待っている間だけ非 nil。n が 0 になった時点で閉じる
}

func (l *scanLatch) add() {
	l.mu.Lock()
	l.n++
	l.mu.Unlock()
}

func (l *scanLatch) done() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.n--
	if l.n <= 0 && l.waitC != nil {
		close(l.waitC)
		l.waitC = nil
	}
}

// wait は「今走っている走査が全部帰った」ときに閉じる channel を返す。
// 走査が無ければ閉じ済みの channel を返す (呼び出し側は常に受信できる)。
func (l *scanLatch) wait() <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.n <= 0 {
		c := make(chan struct{})
		close(c)
		return c
	}
	if l.waitC == nil {
		l.waitC = make(chan struct{})
	}
	return l.waitC
}

var doctorCleanup scanLatch

// doctorTrack は走査 1 本を latch に登録して走らせる。goroutine は呼び出し側が作る
// (tea.Cmd の closure からも使うため)。
//
// 🚨 **add は f を呼ぶ前に済ませる**。子プロセスを起こすのは f の中なので、この順序が
// 「子が生まれる前に登録されている」を保証する。
func doctorTrack(f func()) {
	doctorCleanup.add()
	defer doctorCleanup.done()
	f()
}

// waitDoctorCleanup は走行中の doctor 走査の帰還を看取ってから戻る (終了・再起動の直前用)。
// 待ちは runner.Exec の ctx cancel + WaitDelay (2 秒) で構造的に有限。すぐ終わらないときだけ
// 理由を出す (無言で固まったように見せない。waitPullCleanup と同じ作法)。
func waitDoctorCleanup() {
	done := doctorCleanup.wait()
	select {
	case <-done:
		return
	case <-time.After(200 * time.Millisecond):
		fmt.Fprintln(os.Stderr, "glogx: doctor の走査 (brew / du) の終了を待っています...")
	}
	<-done
}
