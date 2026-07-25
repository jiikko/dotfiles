package main

import (
	"sync"
	"testing"
	"time"
)

func TestDispatchQueueSeedAndSnapshotOrder(t *testing.T) {
	q := newDispatchQueue()
	q.seed([]string{"a", "b", "c"})
	if got := q.length(); got != 3 {
		t.Fatalf("length = %d, want 3", got)
	}
	want := []string{"a", "b", "c"}
	got := q.snapshot()
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("snapshot = %v, want %v", got, want)
		}
	}
}

func TestDispatchQueuePushTailAndFront(t *testing.T) {
	q := newDispatchQueue()
	q.seed([]string{"a"})
	q.push("tail", false)
	q.push("head", true)
	got := q.snapshot()
	want := []string{"head", "a", "tail"}
	if len(got) != len(want) {
		t.Fatalf("snapshot = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("snapshot = %v, want %v", got, want)
		}
	}
}

// peek は「取り出さない」= 実際に worker へ渡す (commit) まで pending に見えていること。
// TUI の queue view がこのスナップショットなので、消えるのが早いと「投入したのに消えた」に見える。
func TestDispatchQueuePeekDoesNotRemoveUntilCommit(t *testing.T) {
	q := newDispatchQueue()
	q.seed([]string{"a", "b"})
	j, ok := q.peek(nil)
	if !ok || j.line != "a" {
		t.Fatalf("peek = (%+v, %v), want head a", j, ok)
	}
	if q.length() != 2 {
		t.Fatalf("peek が要素を削った: length = %d, want 2", q.length())
	}
	q.commit(j.index)
	if q.length() != 1 || q.snapshot()[0] != "b" {
		t.Fatalf("commit 後の状態が不正: %v", q.snapshot())
	}
	q.commit(j.index) // 二重 commit は no-op
	if q.length() != 1 {
		t.Fatalf("二重 commit で余分に削れた: %v", q.snapshot())
	}
}

// commit を index 照合にしている理由の回帰テスト: peek と commit の間に pushFront が
// 割り込むと、位置 (先頭) で削ると「割り込んだ新しい先頭」を消してしまう。
func TestDispatchQueueCommitByIndexSurvivesRacingPushFront(t *testing.T) {
	q := newDispatchQueue()
	q.seed([]string{"a", "b"})
	j, _ := q.peek(nil)
	q.push("jumped-in", true) // peek と commit の間に新しい先頭が入る
	q.commit(j.index)
	got := q.snapshot()
	want := []string{"jumped-in", "b"}
	if len(got) != len(want) {
		t.Fatalf("snapshot = %v, want %v (a だけが消えるべき)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("snapshot = %v, want %v", got, want)
		}
	}
}

// 非 live: 空になったら peek は待たずに done=false を返す (dispatcher が終了できる)。
func TestDispatchQueuePeekDrainsWhenNotLive(t *testing.T) {
	q := newDispatchQueue()
	q.seed([]string{"only"})
	j, ok := q.peek(nil)
	if !ok {
		t.Fatal("要素があるのに peek が false")
	}
	q.commit(j.index)
	done := make(chan struct{}) // 閉じない: live=false の判定だけで返るはず
	got := make(chan bool, 1)
	go func() { _, ok := q.peek(done); got <- ok }()
	select {
	case ok := <-got:
		if ok {
			t.Fatal("空 + 非 live で peek が true を返した")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("空 + 非 live で peek がブロックした (dispatcher が終了できない)")
	}
}

// live: 空でもブロックし、push で起きる。
func TestDispatchQueuePeekBlocksWhenLiveAndWakesOnPush(t *testing.T) {
	q := newDispatchQueue()
	q.setLive(true)
	if !q.isLive() {
		t.Fatal("setLive(true) が反映されない")
	}
	done := make(chan struct{})
	got := make(chan runnerJob, 1)
	go func() {
		j, ok := q.peek(done)
		if ok {
			got <- j
		}
	}()
	select {
	case j := <-got:
		t.Fatalf("空 live で peek が即返した: %+v", j)
	case <-time.After(50 * time.Millisecond):
	}
	q.push("later", false)
	select {
	case j := <-got:
		if j.line != "later" {
			t.Fatalf("peek = %q, want later", j.line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("push で peek が起きない (dispatcher が固まる)")
	}
}

// live でブロック中でも done (stop) で抜ける。抜けないと RequestStop がハングする。
func TestDispatchQueuePeekUnblocksOnDone(t *testing.T) {
	q := newDispatchQueue()
	q.setLive(true)
	done := make(chan struct{})
	got := make(chan bool, 1)
	go func() { _, ok := q.peek(done); got <- ok }()
	time.Sleep(20 * time.Millisecond)
	close(done)
	select {
	case ok := <-got:
		if ok {
			t.Fatal("done 後の peek が true を返した")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("done で peek が抜けない (stop がハングする)")
	}
}

func TestDispatchQueueRemoveLine(t *testing.T) {
	q := newDispatchQueue()
	q.seed([]string{"a", "b", "c"})
	if !q.removeLine("b") {
		t.Fatal("pending な行の removeLine が false")
	}
	got := q.snapshot()
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Fatalf("removeLine 後 = %v, want [a c]", got)
	}
	if q.removeLine("b") {
		t.Fatal("既に消えた行の removeLine が true (呼び出し側が dedup を誤って消す)")
	}
}

// index は削除を挟んでも単調増加 = 生存中は一意。重複すると commit が別の job を消す。
func TestDispatchQueueIndexesStayUnique(t *testing.T) {
	q := newDispatchQueue()
	q.seed([]string{"a"})
	j1, _ := q.peek(nil)
	q.commit(j1.index)
	q.push("b", false)
	j2, _ := q.peek(nil)
	if j2.index == j1.index {
		t.Fatalf("index が再利用された: %d", j2.index)
	}
}

// 並行アクセス (-race で検出させる): push / peek+commit / snapshot を同時に叩いても
// 破綻せず、投入した全件がちょうど 1 回ずつ dispatch される。
func TestDispatchQueueConcurrentPushCommit(t *testing.T) {
	q := newDispatchQueue()
	q.setLive(true)
	const n = 200
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { // producer
		defer wg.Done()
		for i := range n {
			q.push(string(rune('a'+i%26))+string(rune('0'+i/26)), i%3 == 0)
		}
	}()
	wg.Add(1)
	go func() { // snapshot reader (TUI 相当)
		defer wg.Done()
		for range 300 {
			_ = q.snapshot()
			_ = q.length()
		}
	}()

	done := make(chan struct{})
	seen := map[int]bool{}
	consumed := 0
	consumerDone := make(chan struct{})
	go func() { // dispatcher 相当: peek → commit
		defer close(consumerDone)
		for consumed < n {
			j, ok := q.peek(done)
			if !ok {
				return
			}
			if seen[j.index] {
				t.Errorf("同じ index が 2 回 dispatch された: %d", j.index)
			}
			seen[j.index] = true
			q.commit(j.index)
			consumed++
		}
	}()

	wg.Wait()
	select {
	case <-consumerDone:
	case <-time.After(5 * time.Second):
		close(done)
		t.Fatalf("全件 dispatch されない: consumed=%d/%d remaining=%d", consumed, n, q.length())
	}
	if consumed != n {
		t.Fatalf("dispatch 数 = %d, want %d", consumed, n)
	}
	if q.length() != 0 {
		t.Fatalf("全 commit 後もキューが空でない: %v", q.snapshot())
	}
}
