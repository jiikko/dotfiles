package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"doctor/exitcode"
	"doctor/runner"
	"doctor/svc"
)

// サブコマンドは持たない (一覧だけ)。停止・削除の入口が無いことが「実行しない」の担保。
func main() {
	jsonOut := flag.Bool("json", false, "JSON で出力する")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: svcdoctor [-json]\n\n壊れた launchd 登録 (実行ファイル不在 / 失敗し続けている / Homebrew 台帳に無い) を検出して表示する。\n停止・削除は行わない。手で実行するコマンドを提示するだけ。\n\n終了コード (diskdoctor と共通の語彙):\n  0  診断できた + 候補なし\n  1  診断できた + 候補あり\n  2  引数が不正、または診断できなかったものがある (2 が 1 より優先)\n     (中断 / launchctl の失敗 / brew 台帳が取れない / 読めないディレクトリ。\n      「検査できなかった」を緑にしない)\n  3  実行環境・出力の失敗 (home の解決 / JSON のエンコード)\n\n-json でも同じ終了コードを返す。\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "svcdoctor: サブコマンドはありません (%q)\n", flag.Arg(0))
		os.Exit(exitcode.Undiagnosed)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// HOME が解決できなければ**走査せず即エラー**にする (ユーザー判断 2026-09-03 / issue 191)。
	//
	// ⚠️ これは意図的に diskdoctor と非対称。`svc.DefaultDirs` の 3 つのうち HOME が要るのは
	// `$HOME/Library/LaunchAgents` だけで、`/Library/LaunchAgents` と `/Library/LaunchDaemons` は
	// 絶対パスなので走査を続けることは**できる**。それでも中止するのは、HOME も引けない環境で
	// 出した部分的な診断を「その環境の全体像」と読まれる方が危ないため。
	// (diskdoctor 側はエントリ単位で弾く形にしてある = issue 175。あちらはカタログの大半が
	// HOME 非依存なので、残る診断の情報量が違う)
	//
	// 再評価の条件: HOME 非依存の 2 ディレクトリだけでも診断を残したくなったとき。
	// そのときは失敗を `svc.Options` へ渡し、該当ディレクトリだけ `DirErrs` に落とす
	// (rc は 2 になり diskdoctor と揃う)。判断の経緯は issues/done/191。
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "svcdoctor:", err)
		os.Exit(exitcode.EnvFailure)
	}
	rep := svc.Scan(ctx, svc.Options{Dirs: svc.DefaultDirs(home, os.Getuid()), Run: runner.Exec})
	os.Exit(emit(rep, *jsonOut, os.Stdout, os.Stderr))
}

// emit は出力して終了コードを返す。**出力の分岐の外**で終了コードを決めるのが要点:
// 分岐の中で return / os.Exit すると、片方の経路だけ判定を飛ばす形 (issue 177 (b) の -json) が
// 再び書けてしまう。構造でそれを禁じる。
func emit(rep svc.Report, jsonOut bool, stdout, stderr io.Writer) int {
	if jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			_, _ = fmt.Fprintln(stderr, "svcdoctor:", err)
			return exitcode.EnvFailure
		}
	} else {
		_, _ = fmt.Fprint(stdout, svc.Format(rep))
	}
	return svcExitCode(rep)
}
