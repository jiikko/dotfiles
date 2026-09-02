// Package runner は外部コマンドの実行口。stdout / stderr / exit code を分けて返す (混ぜると
// どの stream が判定材料か確定できない。simctl は rc=24 + stderr のみ、という形を実際に返す)。
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// Runner は実行口の型。err は「起動できなかった / タイムアウト」。exit code が非 0 なだけなら
// err == nil で rc に入る。テストは fake を差す。
type Runner func(ctx context.Context, name string, args ...string) (stdout, stderr string, rc int, err error)

// waitDelay は ctx 終了後に pipe の EOF を待つ上限 (孫プロセスが pipe を継承している場合の保険)。
const waitDelay = 2 * time.Second

// Exec は実際に exec する Runner。ctx のキャンセルで子プロセスを殺す。
func Exec(ctx context.Context, name string, args ...string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	// ⚠️ WaitDelay が無いと、ctx で子を殺した後も孫 (brew → ruby → git 等) が pipe を握っている限り
	// Run が戻らない (実測 2026-09-02: 1 秒の timeout で 20 秒。WaitDelay 1 秒なら 2 秒)。
	// timeout は「直接の子を殺す」だけなので、pipe を閉じる期限を別に持つ。孫を殺すのは本ツールの責務外
	cmd.WaitDelay = waitDelay
	err := cmd.Run()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ctx.Err() == nil {
			return out.String(), errb.String(), ee.ExitCode(), nil
		}
		if ctx.Err() != nil {
			return out.String(), errb.String(), -1, fmt.Errorf("%s: %w", name, ctx.Err())
		}
		return out.String(), errb.String(), -1, err
	}
	return out.String(), errb.String(), 0, nil
}

// WithTimeout は 1 回の呼び出しに上限を付けて run を呼ぶ。
func WithTimeout(ctx context.Context, run Runner, timeout time.Duration, name string, args ...string) (string, string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return run(ctx, name, args...)
}
