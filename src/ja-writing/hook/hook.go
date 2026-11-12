// Package hook は ja-writing hook (エージェントの hook から check へつなぐ入口) の Main を持つ。
package hook

import (
	"context"

	"ja-writing/cmdenv"
)

// Main は subcommand の入口。終了コードの意味は本家の argparse の定義に合わせる。
func Main(_ context.Context, env cmdenv.Env) int {
	return cmdenv.Unimplemented(env, "hook")
}
