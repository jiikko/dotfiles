package runner

import (
	"context"
	"os"
	"testing"
	"time"
)

// ctx の期限で子を殺した後、pipe を継承した孫が残っていても Run が戻る (WaitDelay)。
// 孫 (bg の sleep) は stdout を継承しているので、WaitDelay が無いと孫の 20 秒まで戻らない。
func TestExecReturnsAfterTimeoutEvenIfGrandchildHoldsPipe(t *testing.T) {
	t0 := time.Now()
	_, _, _, err := WithTimeout(context.Background(), Exec, time.Second, "sh", "-c", "sleep 20 & exec sleep 20")
	elapsed := time.Since(t0)
	if err == nil {
		t.Fatal("timeout なのに err が nil")
	}
	// 判定は「孫の 20 秒より桁で短いか」(成功側 ~1+2 秒 / 失敗側 20 秒。実測で 6 倍以上の差)
	if elapsed > 10*time.Second {
		t.Fatalf("孫が pipe を握っている間 Run が戻らない: %s", elapsed)
	}
}

// cancel で孫まで死ぬ (プロセスグループ)。孫が pipe に書かなくても残らない。
func TestExecKillsGrandchildOnCancel(t *testing.T) {
	marker := t.TempDir() + "/alive"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		// 孫 (bg の sh) は stdout を閉じて pipe を握らず、1.5 秒後に marker を書く。親は exec sleep
		_, _, _, _ = Exec(ctx, "sh", "-c", "(sleep 1.5; touch "+marker+") >/dev/null 2>&1 & exec sleep 20")
		close(done)
	}()
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cancel 後に Exec が戻らない")
	}
	time.Sleep(2 * time.Second)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("孫プロセスが cancel 後も生きて marker を書いた (プロセスグループごと殺していない)")
	}
}
