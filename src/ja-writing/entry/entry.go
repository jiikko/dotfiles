// Package entry は ja-writing のプロセスの入口 (subcommand の表・シグナルの受け方・Env の組み立て)。
//
// main だけでなく、check のテストが自分のバイナリを run の子 (lint・diff) として起こし直すときもここを通る。
// テストの子だけが別の起動の経路を持つと、本番の子と違うシグナルの扱いで走り、子を止める処理の退行がテストから見えなくなる。
package entry

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

// Body は subcommand の名前を含む Env を受けて終了コードを返す (Main か、テストの子の代わりの本体)。
type Body func(context.Context, cmdenv.Env) int

// subcommands は subcommand 名と入口の対応。順序は使い方の表示に関わらないので map にしている。
var subcommands = map[string]Body{
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

// Run はプロセスの入口。args は os.Args[1:]。シグナルの受け方と Env を整えてから body (本番は Main) を呼び、終了コードを返す。
func Run(args []string, body Body) int {
	// 取り消しの cause に受けたシグナルを入れる (check は 128+番号で終わる)。SIGHUP はここで受けない: lint・diff は本家と同じく SIGHUP で
	// そのまま終わり、check は run の間だけ自分で受ける。
	ctx, stop := cmdenv.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// 読む側が閉じたパイプへの書き込みを、SIGPIPE で落ちずに EPIPE の誤りとして受ける (本家の Python は BrokenPipeError を受けて rc 3 にする)。
	// signal.Ignore にしない: 無視は exec した子プロセス (git など) に引き継がれ、子の SIGPIPE の振る舞いまで変える。
	signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE)
	env, err := cmdenv.FromProcess(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ja-writing: %v\n", err)
		return 1
	}
	return body(ctx, env)
}
