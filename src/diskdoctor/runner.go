package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// Runner は外部コマンドの実行口。stdout / stderr / exit code を分けて返す (混ぜると
// どの stream が判定材料か確定できない。simctl は rc=24 + stderr のみ、という形を実際に返す)。
// err は「起動できなかった / タイムアウト」。exit code が非 0 なだけなら err == nil で rc に入る。
// svcdoctor と同じ契約 (別 module なので写している)。
type Runner func(ctx context.Context, name string, args ...string) (stdout, stderr string, rc int, err error)

func execRunner(ctx context.Context, name string, args ...string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
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

// cmdTimeout は補助コマンド (simctl / brew / pgrep) 1 回の上限。
const cmdTimeout = 30 * time.Second

func runWithTimeout(ctx context.Context, run Runner, name string, args ...string) (string, string, int, error) {
	ctx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()
	return run(ctx, name, args...)
}
