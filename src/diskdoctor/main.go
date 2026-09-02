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
)

// 一覧 (dry-run) だけ。削除のフラグは無い (④ で足すときも既定は dry-run のまま)。
func main() {
	jsonOut := flag.Bool("json", false, "JSON で出力する")
	progress := flag.Bool("progress", false, "完了したエントリを stderr に順次出す")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: diskdoctor [-json] [-progress]\n\n既知の掃除候補 (allowlist) を占有量の降順で、リスクと復元方法つきで一覧する。\n削除は行わない (dry-run のみ)。\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "diskdoctor: サブコマンドはありません (%q)\n", flag.Arg(0))
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	opt := Options{Env: realEnv(), Run: execRunner}
	if *progress {
		opt.OnResult = func(r Result) {
			fmt.Fprintf(os.Stderr, "  %-28s %-8s %9s  %s\n", r.Entry.ID, r.Status, humanSize(r.Size), r.Elapsed.Round(time.Millisecond))
		}
	}
	rep := Scan(ctx, opt)
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintln(os.Stderr, "diskdoctor:", err)
			os.Exit(1)
		}
	} else {
		fmt.Print(Format(rep, time.Now()))
	}
	// 走査できなかったエントリがあれば exit 2 (検査できなかったを緑にしない)
	for _, r := range rep.Results {
		if r.Status == StatusFailed {
			os.Exit(2)
		}
	}
	if rep.Partial {
		os.Exit(2)
	}
}
