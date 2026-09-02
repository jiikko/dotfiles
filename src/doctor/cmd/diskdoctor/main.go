package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"doctor/disk"
	"doctor/runner"
)

// 一覧 (dry-run) だけ。削除のフラグは無い (④ で足すときも既定は dry-run のまま)。
func main() {
	jsonOut := flag.Bool("json", false, "JSON で出力する")
	progress := flag.Bool("progress", false, "完了したエントリを stderr に順次出す")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: diskdoctor [-json] [-progress]\n\n既知の掃除候補 (allowlist) を占有量の降順で、リスクと復元方法つきで一覧する。\n削除は行わない (dry-run のみ)。\n\n終了コード:\n  0  一覧を出せた (候補の有無では変わらない)\n  1  出力に失敗した (-json のエンコード)\n  2  引数が不正、または走査できなかったエントリがある\n     (「検査できなかった」を緑にしないため。部分的な結果は表示したうえで 2 を返す)\n\n⚠️ svcdoctor は「候補あり」を 1 で返すが diskdoctor は 0 のまま (issue 177 で是正予定)。\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "diskdoctor: サブコマンドはありません (%q)\n", flag.Arg(0))
		os.Exit(2)
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
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintln(os.Stderr, "diskdoctor:", err)
			os.Exit(1)
		}
	} else {
		fmt.Print(disk.Format(rep, time.Now()))
	}
	// 走査できなかったエントリがあれば exit 2 (検査できなかったを緑にしない)
	for _, r := range rep.Results {
		if r.Status == disk.StatusFailed {
			os.Exit(2)
		}
	}
	if rep.Partial {
		os.Exit(2)
	}
}
