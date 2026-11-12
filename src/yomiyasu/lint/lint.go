// Package lint は yomiyasu lint (表現と書式の検査) の Main を持つ。issue 1075 で実装する。
package lint

import (
	"context"

	"yomiyasu/cmdenv"
)

// Main は subcommand の入口。終了コードの意味は本家の argparse の定義に合わせる。
func Main(_ context.Context, env cmdenv.Env) int {
	return cmdenv.Unimplemented(env, "lint")
}
