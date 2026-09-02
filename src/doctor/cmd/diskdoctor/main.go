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
	"time"

	"doctor/disk"
	"doctor/exitcode"
	"doctor/runner"
)

// 一覧 (dry-run) だけ。削除のフラグは無い (④ で足すときも既定は dry-run のまま)。
func main() {
	jsonOut := flag.Bool("json", false, "JSON で出力する")
	progress := flag.Bool("progress", false, "完了したエントリを stderr に順次出す")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: diskdoctor [-json] [-progress]\n\n既知の掃除候補 (allowlist) を占有量の降順で、リスクと復元方法つきで一覧する。\n削除は行わない (dry-run のみ)。\n\n終了コード (svcdoctor と共通の語彙):\n  0  診断できた + 候補なし\n  1  診断できた + 候補あり\n  2  引数が不正、または走査できなかったエントリがある (2 が 1 より優先)\n     (「検査できなかった」を緑にしないため。部分的な結果は表示したうえで 2 を返す)\n  3  実行環境・出力の失敗 (-json のエンコード)\n\n-json でも同じ終了コードを返す。\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "diskdoctor: サブコマンドはありません (%q)\n", flag.Arg(0))
		os.Exit(exitcode.Undiagnosed)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	opt := disk.Options{Env: disk.RealEnv(), Run: runner.Exec}
	if *progress {
		opt.OnResult = func(r disk.Result) {
			fmt.Fprintf(os.Stderr, "  %-28s %-8s %9s  %s\n", r.Entry.ID, r.Status, disk.HumanSize(r.Size), r.Elapsed.Round(time.Millisecond))
		}
	}
	rep := disk.Scan(ctx, opt)
	os.Exit(emit(rep, *jsonOut, time.Now(), os.Stdout, os.Stderr))
}

// emit は出力して終了コードを返す。**出力の分岐の外**で終了コードを決めるのが要点
// (cmd/svcdoctor/main.go の同名関数と同じ理由: issue 177 (b))。
func emit(rep disk.Report, jsonOut bool, now time.Time, stdout, stderr io.Writer) int {
	if jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			_, _ = fmt.Fprintln(stderr, "diskdoctor:", err)
			return exitcode.EnvFailure
		}
	} else {
		_, _ = fmt.Fprint(stdout, disk.Format(rep, now))
	}
	return diskExitCode(rep)
}
