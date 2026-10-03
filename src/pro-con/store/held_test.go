package store

import (
	"path/filepath"
	"testing"
	"time"
)

// issue 638: Hold は置き場が無ければ作ってから印を置く (呼び手が --e2e か本物のモードかに依らない。e2e の分岐でだけ置き場を作る
// 実装では、本物のモードの初めての --stop が印を置けない)。
func TestHoldCreatesMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "live")
	if err := Hold(dir, time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("置き場の無いところで Hold: %v", err)
	}
	if !Held(dir) {
		t.Fatal("Hold したのに印が無い")
	}
}
