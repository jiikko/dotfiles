// Package ptytest は、テストのバイナリを script(1) の擬似端末の上で helper として走らせる (検査のための部品。issue 518)。
// helper は擬似端末を制御端末に持ち、前面のグループにいる (画面と同じ立場)。go test 自身は端末を持たないことが多いので、ここで作る。
//
// 使い方: TestMain の先頭で Helper を呼び、検査から Run で helper を起こす。
package ptytest

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"pro-con/foreground"
)

const env = "PTYTEST_HELPER"

// Helper は、helper として起こされていれば f(mode) の結果を書いて抜ける (そうでなければ何もしない)。TestMain の先頭で呼ぶ。
func Helper(f func(mode string) string) {
	mode := os.Getenv(env)
	if mode == "" {
		return
	}
	if fg, mine, err := foreground.Owner(int(os.Stdin.Fd())); err != nil || fg != mine {
		fmt.Printf("RESULT not-foreground fg=%d mine=%d err=%v\n", fg, mine, err)
	} else {
		fmt.Println("RESULT " + f(mode))
	}
	os.Exit(0)
}

// Run は自分 (テストのバイナリ) を擬似端末の上で mode の helper として走らせ、f(mode) の結果を返す。
// script・zsh が無い・擬似端末の前面に居られないときは Skip する。
func Run(t testing.TB, mode string) string {
	t.Helper()
	for _, bin := range []string{"script", "zsh"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s が無い", bin)
		}
	}
	cmd := exec.Command("script", "-q", "/dev/null", os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), env+"="+mode)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if i := strings.Index(line, "RESULT "); i >= 0 {
			r := strings.TrimSpace(line[i+len("RESULT "):])
			if strings.HasPrefix(r, "not-foreground") {
				t.Skipf("擬似端末の前面に居られない: %s", r)
			}
			return r
		}
	}
	t.Fatalf("helper の結果が無い:\n%s", out)
	return ""
}
