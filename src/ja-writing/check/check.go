// Package check は ja-writing check (lint と diff をまとめて実行して記録を残す) の Main を持つ。
package check

import (
	"context"

	"ja-writing/cmdenv"
)

// Main は subcommand の入口。終了コードの意味は本家の argparse の定義に合わせる。
func Main(_ context.Context, env cmdenv.Env) int {
	return cmdenv.Unimplemented(env, "check")
}
