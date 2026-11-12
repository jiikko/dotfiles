// Package hook は yomiyasu hook (エージェントの hook から check へつなぐ入口) の Main を持つ。issue 1077 で実装する。
package hook

import (
	"context"

	"yomiyasu/cmdenv"
)

// Main は subcommand の入口。終了コードの意味は本家の argparse の定義に合わせる。
func Main(_ context.Context, env cmdenv.Env) int {
	return cmdenv.Unimplemented(env, "hook")
}
