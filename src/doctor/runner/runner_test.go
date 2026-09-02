package runner

import (
	"context"
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
