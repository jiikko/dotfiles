package main

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

// cleanupLatch が守る中心の不変条件: **カウント 0 での登録が、看取りと同時に走っても壊れない**。
//
// これが sync.WaitGroup を使えない理由そのもの。WaitGroup はカウント 0 のときの Add と Wait の
// 同時実行を禁じており、`sync: WaitGroup misuse: Add called concurrently with Wait` で panic する。
// その panic は `race.Enabled` に囲まれていないので **-race なしの production でも落ちる**
// (issue 214 / 217)。
//
// ⚠️ このテストは latch を**直接**叩く。doctorTrack 経由のテストは「走査が看取られるか」を見て
// いて、0 件での同時実行という**最も危ない瞬間**を作っていない (issue 217 の敵対レビューが
// 指摘した取り残しは、まさにこの瞬間が誰にも検査されていなかったことに由来する)。
func TestCleanupLatchAddAtZeroConcurrentWithWait(t *testing.T) {
	// 🚨 **2 CPU 以上が要る**。この検査は「登録と看取りが**同時に**走る」形を作ることが本体で、
	// `GOMAXPROCS=1` では 2 つの goroutine が交互にしか動かず、交差が 1 度も起きない。
	// 実測 (敵対的レビュー, 2026-09-03): 被検体を sync.WaitGroup に差し替えて測ると、
	// GOMAXPROCS=2 / 4 では 1 run 目で DATA RACE を検出するのに、**GOMAXPROCS=1 では
	// -count=10 (2000 周) でも rc=0** だった。つまり 1 CPU では検査が静かに消える。
	if old := runtime.GOMAXPROCS(0); old < 2 {
		defer runtime.GOMAXPROCS(old)
		runtime.GOMAXPROCS(2)
	}
	for range 200 {
		var l cleanupLatch
		start := make(chan struct{})
		var wg sync.WaitGroup // ハーネス側の同期 (被検体ではない)
		wg.Add(2)
		go func() { defer wg.Done(); <-start; <-l.wait() }()
		go func() { defer wg.Done(); <-start; l.add(); l.done() }()
		close(start)
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("固まった (wait が閉じない)")
		}
	}
}

// 待ち手が**実際に park した状態**で登録・完了が起きる経路を決定論的に通す。
//
// 上の並行テストはスケジューラ任せなので、park する周が何回あるかは環境で変わる
// (実測: 200 周のうち park は 3 回で、197 回は「仕事が無い → 閉じ済みを返す」経路だった)。
// park 側は**最も壊れやすい経路** (waitC の生成・close・nil 化が全部ここで動く) なので、
// 運任せにせず名指しで通す。
func TestCleanupLatchAddAndDoneWhileWaiterIsParked(t *testing.T) {
	var l cleanupLatch
	l.add() // 走行中の仕事を 1 本作り、待ち手を park させる
	w := l.wait()
	select {
	case <-w:
		t.Fatal("仕事が走っているのに wait が閉じている (park していない)")
	case <-time.After(20 * time.Millisecond):
	}
	l.add() // park 中に 2 本目が登録される
	l.done()
	select {
	case <-w:
		t.Fatal("まだ 1 本走っているのに wait が閉じた")
	case <-time.After(20 * time.Millisecond):
	}
	l.done() // 最後の 1 本が帰る
	select {
	case <-w:
	case <-time.After(5 * time.Second):
		t.Fatal("全部帰ったのに wait が閉じない")
	}
}

// latch は package グローバルで **pull / 再スキャンのたびに使い回される**。
// 1 周しか回さないテストは、2 周目で壊れる形 (waitC を nil に戻し忘れる等) を守らない。
// 実測 (敵対的レビュー): `done()` の `l.waitC = nil` を落とす変異は、latch のテストだけでは
// 緑のまま通り、**無関係な doctor のテストが偶然 panic で落として**いた。
func TestCleanupLatchIsReusableAcrossCycles(t *testing.T) {
	var l cleanupLatch
	for cycle := range 3 {
		l.add()
		w := l.wait()
		select {
		case <-w:
			t.Fatalf("%d 周目: 仕事が走っているのに wait が閉じている", cycle)
		case <-time.After(20 * time.Millisecond):
		}
		l.done()
		select {
		case <-w:
		case <-time.After(5 * time.Second):
			t.Fatalf("%d 周目: done の後も wait が閉じない", cycle)
		}
	}
}

// done が add より多く呼ばれても n を負にしない。
//
// 素朴に `l.n--` すると、負のまま続行して**次の add() が n を 0 に戻した瞬間に、
// 走行中の仕事があるのに wait() が閉じ済みを返す** (看取りが素通りする)。
// sync.WaitGroup は同じ操作で大声で落ちる (`panic: sync: negative WaitGroup counter`) ので、
// 対策しないと載せ替えが「うるさい失敗」を「静かな取りこぼし」に交換することになる。
func TestCleanupLatchDoesNotGoNegative(t *testing.T) {
	var l cleanupLatch
	l.add()
	l.done()
	l.done() // 余分な done。WaitGroup ならここで panic する
	l.add()  // 本物の走行中の仕事
	select {
	case <-l.wait():
		t.Fatal("走行中の仕事があるのに wait が即座に閉じた (n が負に沈んでいる)")
	case <-time.After(50 * time.Millisecond):
	}
	l.done()
	select {
	case <-l.wait():
	case <-time.After(5 * time.Second):
		t.Fatal("全部帰ったのに wait が閉じない")
	}
}

// 看取りは「今走っている仕事が帰るまで」戻らない。
func TestCleanupLatchWaitBlocksUntilDone(t *testing.T) {
	var l cleanupLatch
	l.add()
	w := l.wait()
	select {
	case <-w:
		t.Fatal("仕事が走っているのに wait が閉じている")
	case <-time.After(30 * time.Millisecond):
	}
	l.done()
	select {
	case <-w:
	case <-time.After(5 * time.Second):
		t.Fatal("done の後も wait が閉じない")
	}
}

// 仕事が無ければ閉じ済みの channel を返す (呼び出し側が常に受信できる契約)。
func TestCleanupLatchWaitIsClosedWhenIdle(t *testing.T) {
	var l cleanupLatch
	select {
	case <-l.wait():
	case <-time.After(time.Second):
		t.Fatal("仕事が無いのに wait が閉じていない")
	}
}

// 🚨 **pull 側も latch に載っていること**を配線として固定する (issue 217)。
// 型が sync.WaitGroup へ戻るとここがコンパイルできない。doctor 側だけ直して pull 側を
// 取り残したのが 217 の症状なので、両方を名指しで書く。
//
// ⚠️ **宣言で pin する** (テスト関数の中で wait() を読まない)。以前は関数の中で
// 「仕事が無ければ wait が閉じている」を assert していたが、それは
// TestCleanupLatchWaitIsClosedWhenIdle の重複であるうえ、**グローバルを読むので
// 先行テストが doctorCleanup に仕事を残していると 1 秒待って落ちる**
// (敵対的レビューが実験で再現。自然な到達は shuffle 17 seed で出なかったが、
// glogx のスイート中に本物の doctor 走査が並走する窓は実在する)。
// 得られる価値は型の pin だけなので、コストのかからない形に寄せた。
var (
	_ *cleanupLatch = &pullCleanup
	_ *cleanupLatch = &doctorCleanup
)
