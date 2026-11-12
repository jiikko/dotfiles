// Package diff は ja-writing diff (元の文と書き直した文の差分の検査) の Main を持つ。
package diff

import (
	"context"

	"ja-writing/cmdenv"
)

// Main は subcommand の入口。終了コードの意味は本家の argparse の定義に合わせる。
func Main(_ context.Context, env cmdenv.Env) int {
	return cmdenv.Unimplemented(env, "diff")
}
