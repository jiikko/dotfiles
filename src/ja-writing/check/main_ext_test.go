package check_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"strconv"
	"testing"
	"time"

	"ja-writing/check"
	"ja-writing/cmdenv"
	"ja-writing/entry"
)

// TestMain は、印を見たらテストのバイナリを run の子として走らせる (child_test.go)。entry を import するので外部テストの package に置く
// (entry は check を import する)。
func TestMain(m *testing.M) {
	switch os.Getenv(check.ChildEnvName) {
	case check.ChildReal:
		os.Exit(entry.Run(os.Args[1:], entry.Main))
	case check.ChildHang:
		os.Exit(entry.Run(os.Args[1:], hang))
	}
	// 子として起こされたのに印が無い (Env の環境変数を子に渡し損ねた退行) ときは、テストの全部を子として走らせない。
	// 走らせると、子がまた子を起こして run の締め切りまで増え続ける。
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-test.") {
		fmt.Fprintf(os.Stderr, "テストのバイナリが %s の子の印なしで %q として起こされました\n", check.ChildEnvName, os.Args[1:])
		os.Exit(70)
	}
	os.Exit(m.Run())
}

// hang は止まらない子の本体 (pid を書いてから眠り、ctx を見ない)。
func hang(context.Context, cmdenv.Env) int {
	if err := os.WriteFile(os.Getenv("JA_PIDFILE"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		return 70
	}
	// sleep-ok: dummy: 止まらない子を演じる (run が kill するまで眠る。上限は親が止め損ねたときに子を残さないため)。
	time.Sleep(60 * time.Second)
	return 0
}
