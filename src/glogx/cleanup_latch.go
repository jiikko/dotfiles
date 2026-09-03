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
	// 🚨 **n を負にしない**。`sync.WaitGroup` は done 過多で大声で落ちる
	// (`panic: sync: negative WaitGroup counter`) が、素朴に `l.n--` すると負のまま続行し、
	// **次の add() が n を 0 に戻した瞬間に、走行中の仕事があるのに wait() が閉じ済みを返す**
	// (= 看取りが素通りする)。載せ替えで「うるさい失敗」を「静かな取りこぼし」に
	// 交換してしまう形なので、0 で止める (敵対的レビューが実験で示した。issue 217)。
	//
	// 0 で止めるのを選び、panic を選ばなかった理由: この latch の目的は「終了前に看取る」で、
	// 壊れ方の最悪は**待たずに終わること**。負を許さなければ、対応が 1 つずれても
	// 「余分に待つ」側へ倒れる (安全側)。呼び出しは全て `add(); defer done()` の対なので、
	// ここに到達するのは実装ミスのときだけ。
	if l.n > 0 {
		l.n--
	}
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
