// Package hook は ja-writing hook (エージェントの hook から check へつなぐ入口) の Main を持つ。
// 実装は check の HookMainWith にある (本家と比べる記録の突き合わせが check のテストの道具を使うため。理由は check/hook.go の冒頭)。
package hook

import (
	"context"

	"ja-writing/check"
	"ja-writing/cmdenv"
)

// Main は subcommand の入口。
func Main(ctx context.Context, env cmdenv.Env) int {
	return check.HookMain(ctx, env)
}
