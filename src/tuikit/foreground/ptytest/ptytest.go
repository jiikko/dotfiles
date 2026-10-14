// Package ptytest は、テストのバイナリを script(1) の擬似端末の上で helper として走らせる (検査のための部品。issue 518)。
// helper は擬似端末を制御端末に持ち、前面のグループにいる (画面と同じ立場)。go test 自身は端末を持たないことが多いので、ここで作る。
//
// 使い方: TestMain の先頭で Helper を呼び、検査から Run で helper を起こす。
package ptytest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jiikko/dotfiles/src/tuikit/foreground"
)

const env = "PTYTEST_HELPER"

// pidEnv は helper が自分の pid を書くファイル。script を止めても、止まった (SIGTTOU・SIGSTOP) helper は script の
// 子の session に残るので、上限を超えたら Run がこの pid を直接止める (敵対レビュー 2026-10-14)。
const pidEnv = "PTYTEST_PIDFILE"

// Helper は、helper として起こされていれば f(mode) の結果を書いて抜ける (そうでなければ何もしない)。TestMain の先頭で呼ぶ。
func Helper(f func(mode string) string) {
	mode := os.Getenv(env)
	if mode == "" {
		return
	}
	if f := os.Getenv(pidEnv); f != "" {
		_ = os.WriteFile(f, []byte(strconv.Itoa(os.Getpid())), 0o600) // 書けなくても検査は進める (止めるのは上限を超えたときだけ)
	}
	if fg, mine, err := foreground.Owner(int(os.Stdin.Fd())); err != nil || fg != mine {
		_, _ = fmt.Fprintf(os.Stdout, "RESULT not-foreground fg=%d mine=%d err=%v\n", fg, mine, err)
	} else {
		_, _ = fmt.Fprintln(os.Stdout, "RESULT "+f(mode)) // Run が script(1) の出力から読む
	}
	os.Exit(0)
}

// timeout は helper の 1 回の上限。helper は前面を確かめて 1 行書くだけで、手元では 0.02 秒で終わる。子が止まる退行
// (前面でないまま端末の設定を書いて SIGTTOU で止まる等) で suite ごと待ち続けないための安全網 (issue 758)。
const timeout = 60 * time.Second

// Run は自分 (テストのバイナリ) を擬似端末の上で mode の helper として走らせ、f(mode) の結果を返す。
// script・zsh が無い・擬似端末の前面に居られないときは Skip する。🚨 ただし CI (環境変数 CI) では落とす: CI は
// `go test` に -v を付けないので、Skip のまま緑だと pty の検査が 1 本も走っていないことがログから見えない (issue 758。
// treefiler の needGit と同じ扱い)。
func Run(t testing.TB, mode string) string {
	t.Helper()
	for _, bin := range []string{"script", "zsh"} {
		if _, err := exec.LookPath(bin); err != nil {
			skip(t, "%s が無い", bin)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "script", "-q", "/dev/null", os.Args[0], "-test.run=^$")
	pidFile := t.TempDir() + "/helper.pid"
	cmd.Env = append(os.Environ(), env+"="+mode, pidEnv+"="+pidFile)
	cmd.WaitDelay = 5 * time.Second // script を止めた後、出力を持ったまま残る孫を待ち続けない
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		killHelper(pidFile)
		t.Fatalf("script が %v で終わらない (helper が止まった):\n%s", timeout, out)
	}
	if err != nil {
		t.Fatalf("script: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if i := strings.Index(line, "RESULT "); i >= 0 {
			r := strings.TrimSpace(line[i+len("RESULT "):])
			if strings.HasPrefix(r, "not-foreground") {
				skip(t, "擬似端末の前面に居られない: %s", r)
			}
			return r
		}
	}
	t.Fatalf("helper の結果が無い:\n%s", out)
	return ""
}

// skip は検査を飛ばす。CI では飛ばさずに落とす (Run の doc)。
func skip(t testing.TB, format string, args ...any) {
	t.Helper()
	if os.Getenv("CI") != "" {
		t.Fatalf("CI で pty の検査を飛ばそうとした: "+format, args...)
	}
	t.Skipf(format, args...)
}

// killHelper は上限を超えた helper を止める。helper は script の子の session に居るので、script を止めても残る。
// SIGKILL は止まっている (T) プロセスにも届く。pid は helper 自身が書いたものだけを使う (広いパターンで kill しない)。
func killHelper(pidFile string) {
	b, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 1 {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}
