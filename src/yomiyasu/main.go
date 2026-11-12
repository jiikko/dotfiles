// Command yomiyasu は、日本語の文章の読みやすさを点検する本家 yomiyasu (Python) の Go 実装。
//
//	yomiyasu lint  [オプション] [ファイル]
//	yomiyasu diff  [オプション] 元の文 書き直した文
//	yomiyasu check [run|verify|start|export|end] [オプション]
//	yomiyasu hook  --host claude-code|codex
//
// 各 subcommand は <名前>.Main(ctx, env) の形で、本家の yomiyasu_<名前>.py の main と同じ終了コードを返す。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"yomiyasu/check"
	"yomiyasu/cmdenv"
	"yomiyasu/diff"
	"yomiyasu/hook"
	"yomiyasu/lint"
)

const usage = "使い方: yomiyasu {lint|diff|check|hook} [オプション]\n"

// subcommands は subcommand 名と入口の対応。順序は使い方の表示に関わらないので map にしている。
var subcommands = map[string]func(context.Context, cmdenv.Env) int{
	"lint":  lint.Main,
	"diff":  diff.Main,
	"check": check.Main,
	"hook":  hook.Main,
}

// Main は env.Args の先頭の subcommand 名で入口を選んで呼び、終了コードを返す。
func Main(ctx context.Context, env cmdenv.Env) int {
	if len(env.Args) == 0 {
		// 標準エラーへの書き込みの失敗は、終了コードを変えられないので捨てる。
		_, _ = fmt.Fprint(env.Stderr, usage)
		return cmdenv.ExitUsage
	}
	run, ok := subcommands[env.Args[0]]
	if !ok {
		_, _ = fmt.Fprintf(env.Stderr, "yomiyasu: 知らない subcommand です: %s\n%s", env.Args[0], usage)
		return cmdenv.ExitUsage
	}
	sub := env
	sub.Args = env.Args[1:]
	return run(ctx, sub)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	env, err := cmdenv.FromProcess(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "yomiyasu: %v\n", err)
		stop()
		os.Exit(1)
	}
	code := Main(ctx, env)
	stop()
	os.Exit(code)
}
