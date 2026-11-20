// Package entry は ja-writing のプロセスの入口 (subcommand の表・シグナルの受け方・Env の組み立て)。
//
// main だけでなく、check のテストが自分のバイナリを run の子 (lint・diff) として起こし直すときもここを通る。
// テストの子だけが別の起動の経路を持つと、本番の子と違うシグナルの扱いで走り、子を止める処理の退行がテストから見えなくなる。
package entry

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"ja-writing/check"
	"ja-writing/cmdenv"
	"ja-writing/diff"
	"ja-writing/hook"
	"ja-writing/internal/pyfs"
	"ja-writing/lint"
)

const usage = "使い方: ja-writing {lint|diff|check|hook} [オプション]\n"

// Body は subcommand の名前を含む Env を受けて終了コードを返す (Main か、テストの子の代わりの本体)。
type Body func(context.Context, cmdenv.Env) int

// subcommand は入口と、プロセスの間ずっと受けて ctx の取り消しにするシグナル。
type subcommand struct {
	main Body
	// stops は受けるシグナル。空なら受けない (Go の既定のまま、シグナルでそのまま終わる)。
	stops []os.Signal
}

// subcommands は subcommand 名と入口の対応。順序は使い方の表示に関わらないので map にしている。
//
// lint・diff・hook はシグナルを受けない: 本家の yomiyasu_lint.py・yomiyasu_diff.py・yomiyasu_hook.py はハンドラを持たず、SIGTERM と SIGHUP は
// 既定の扱いでそのまま終わり、SIGINT は KeyboardInterrupt を捕まえずにシグナルで終わる (シェルから見て 128+番号)。受けて ctx を取り消しても、
// lint.Main・diff.Main は ctx を見ないので最後まで走り、外から止められなくなる。
// check は本家の main と同じく SIGTERM と SIGHUP (_install_stop_handlers) と SIGINT (KeyboardInterrupt) を受け、128+番号で終わる。
var subcommands = map[string]subcommand{
	"lint":  {main: lint.Main},
	"diff":  {main: diff.Main},
	"check": {main: check.Main, stops: []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}},
	"hook":  {main: hook.Main},
}

// Main は env.Args の先頭の subcommand 名で入口を選んで呼び、終了コードを返す。
func Main(ctx context.Context, env cmdenv.Env) int {
	if len(env.Args) == 0 {
		// 標準エラーへの書き込みの失敗は、終了コードを変えられないので捨てる。
		_, _ = fmt.Fprint(env.Stderr, usage)
		return cmdenv.ExitUsage
	}
	sub, ok := subcommands[env.Args[0]]
	if !ok {
		_, _ = io.WriteString(env.Stderr, pyfs.BackslashReplace(fmt.Sprintf("ja-writing: 知らない subcommand です: %s\n%s", env.Args[0], usage)))
		return cmdenv.ExitUsage
	}
	rest := env
	rest.Args = env.Args[1:]
	return sub.main(ctx, rest)
}

// Run はプロセスの入口。args は os.Args[1:]。シグナルの受け方と Env を整えてから body (本番は Main) を呼び、終了コードを返す。
// 受けるシグナルは args の先頭の subcommand で決める (body を差し替えても、同じ名前の subcommand と同じ受け方になる)。
func Run(args []string, body Body) int {
	ctx := context.Background()
	if len(args) > 0 {
		if stops := listening(subcommands[args[0]].stops); len(stops) > 0 {
			var stop context.CancelFunc
			// 取り消しの cause に受けたシグナルを入れる (check はその番号で 128+番号と文を決める)
			ctx, stop = cmdenv.NotifyContext(ctx, stops...)
			defer stop()
		}
	}
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

// listening は sigs のうち、無視して起動されていないもの (本家の _install_stop_handlers は SIG_IGN のシグナルにハンドラを置かない。nohup など)。
// Go が起動時の無視を覚えているのは SIGHUP と SIGINT だけで、無視して起動された SIGTERM は受けてしまう (README の check の「揃わない範囲」)。
func listening(sigs []os.Signal) []os.Signal {
	var out []os.Signal
	for _, s := range sigs {
		if !signal.Ignored(s) {
			out = append(out, s)
		}
	}
	return out
}
