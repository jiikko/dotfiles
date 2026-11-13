// Command ja-writing は、日本語の文章の読みやすさを点検する本家 yomiyasu (Python) の Go 実装。
//
//	ja-writing lint  [オプション] [ファイル]
//	ja-writing diff  [オプション] 元の文 書き直した文
//	ja-writing check [run|verify|start|export|end] [オプション]
//	ja-writing hook  --host claude-code|codex
//
// 各 subcommand は <名前>.Main(ctx, env) の形で、本家の yomiyasu_<名前>.py の main と同じ終了コードを返す。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"ja-writing/check"
	"ja-writing/cmdenv"
	"ja-writing/diff"
	"ja-writing/hook"
	"ja-writing/lint"
)

const usage = "使い方: ja-writing {lint|diff|check|hook} [オプション]\n"

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
		_, _ = fmt.Fprintf(env.Stderr, "ja-writing: 知らない subcommand です: %s\n%s", env.Args[0], usage)
		return cmdenv.ExitUsage
	}
	sub := env
	sub.Args = env.Args[1:]
	return run(ctx, sub)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// 読む側が閉じたパイプへの書き込みを、SIGPIPE で落ちずに EPIPE の誤りとして受ける (本家の Python は BrokenPipeError を受けて rc 3 にする)。
	// signal.Ignore にしない: 無視は exec した子プロセス (git など) に引き継がれ、子の SIGPIPE の振る舞いまで変える。
	signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE)
	env, err := cmdenv.FromProcess(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "ja-writing: %v\n", err)
		stop()
		os.Exit(1)
	}
	code := Main(ctx, env)
	stop()
	os.Exit(code)
}
