package main

import "sync"

// cleanupLatch は「終了前に看取るべき走行中の仕事の本数」を数える latch。
// 2 か所が別インスタンスとして使う: doctor の走査 (doctor_cleanup.go の doctorCleanup) と
// pull の後始末 (external_commands.go の pullCleanup)。
//
// 🚨 **sync.WaitGroup は使えない**。svc / brew / 削除は tea.Cmd の closure から登録するので、
// **Add が Wait と同時に走りうる**。WaitGroup はカウント 0 のときの Add と Wait の同時実行を
// 禁じており、`-race` が実際にデータ競合として検出した (実測 2026-09-03、敵対的レビューの
// 指摘を受けて -count=3 で再現)。状態を全部 mutex の下に置いて、遅れて来る登録も安全にする。
//
// 遅れた登録を Wait が取りこぼすのは**許容する**: まだ始まっていない Cmd は子プロセスを
// 起こしていないので、その時点で終了・再起動しても孤児は生まれない (closure が始まれば、
// 子を起こす前に add が済んでいる)。
type cleanupLatch struct {
	mu    sync.Mutex
	n     int
	waitC chan struct{} // 誰かが待っている間だけ非 nil。n が 0 になった時点で閉じる
}

func (l *cleanupLatch) add() {
	l.mu.Lock()
	l.n++
	l.mu.Unlock()
}

func (l *cleanupLatch) done() {
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
func (l *cleanupLatch) wait() <-chan struct{} {
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
