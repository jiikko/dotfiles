package main

import (
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
	// -race 付きで回すと、WaitGroup 実装では DATA RACE + panic になる形を作る。
	// 交差を確実にするため、待ち側と登録側を同じ barrier で解き放って何度も繰り返す。
	for i := range 200 {
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
			t.Fatalf("%d 回目で固まった (wait が閉じない)", i)
		}
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
// 型が WaitGroup へ戻ると、この行がコンパイルできなくなって気づける。
// doctor 側だけ直して pull 側を取り残したのが 217 の症状なので、両方を名指しで見る。
func TestBothCleanupLatchesAreTheSameKind(t *testing.T) {
	for name, l := range map[string]*cleanupLatch{
		"pullCleanup":   &pullCleanup,
		"doctorCleanup": &doctorCleanup,
	} {
		select {
		case <-l.wait():
		case <-time.After(time.Second):
			t.Errorf("%s: 仕事が無いのに wait が閉じていない", name)
		}
	}
}
