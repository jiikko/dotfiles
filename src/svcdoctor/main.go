package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// サブコマンドは持たない (一覧だけ)。停止・削除の入口が無いことが「実行しない」の担保。
func main() {
	jsonOut := flag.Bool("json", false, "JSON で出力する")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: svcdoctor [-json]\n\n壊れた launchd 登録 (実行ファイル不在 / 失敗し続けている / Homebrew 台帳に無い) を検出して表示する。\n停止・削除は行わない。手で実行するコマンドを提示するだけ。\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "svcdoctor: サブコマンドはありません (%q)\n", flag.Arg(0))
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "svcdoctor:", err)
		os.Exit(1)
	}
	rep := Scan(ctx, Options{Dirs: defaultDirs(home, os.Getuid()), Run: execRunner})
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintln(os.Stderr, "svcdoctor:", err)
			os.Exit(1)
		}
		return
	}
	fmt.Print(Format(rep))
	// 診断できなかったものがあれば exit 2 (検査できなかったを緑にしない)。候補あり = 1、無し = 0
	switch {
	case rep.StatusErr != "" || len(rep.Undiagnosed) > 0 || len(rep.DirErrs) > 0:
		os.Exit(2)
	case len(rep.Findings) > 0:
		os.Exit(1)
	}
}
