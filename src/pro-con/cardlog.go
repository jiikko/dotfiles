package main

// pro-con card log — カードの PG の活動 (応答の文と道具の呼び出し) を時刻の順に読む口 (issue 467。画面の詳細の「活動」と同じ中身)。
//
// 🚨 読むだけ (cardview.go の list / show / wait と同じ。TestViewCommandsDoNotWrite が固定する)。
// 読むのは pro-con が起動したそのカードの session (再開で入れ替わった前の session も) の transcript だけ。

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"pro-con/backend"
	"pro-con/live"
	"pro-con/store"
)

const cardLogUsage = "usage: pro-con card log <カード> [--follow] [--json]"

func runCardLog(args []string, env viewEnv, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pro-con card log", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	follow := fs.Bool("follow", false, "")
	asJSON := fs.Bool("json", false, "")
	var pos []string
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		pos, args = append(pos, args[0]), args[1:]
	}
	if err := fs.Parse(args); errors.Is(err, flag.ErrHelp) {
		_, _ = fmt.Fprintln(stdout, cardLogUsage)
		return 0
	} else if err != nil || len(pos)+fs.NArg() != 1 {
		_, _ = fmt.Fprintln(stderr, cardLogUsage)
		return 2
	}
	id := append(pos, fs.Args()...)[0]
	c, ok, err := store.Find(env.dir, id) // 書庫へ移した終えたカードも読める
	if !ok {
		if err == nil {
			err = fmt.Errorf("カード %q が無い", id)
		}
		_, _ = fmt.Fprintln(stderr, "pro-con card log:", err)
		return 1
	}
	l := live.NewCardLog(filepath.Join(env.dir, live.RegistryFile), func(s string) (string, error) { return live.FindTranscript(env.projects, s) }, c.ID, c.Session)
	enc := json.NewEncoder(stdout) // --json は 1 行 1 活動 (--follow でも行ごとに読める)
	session, wrote := "", false
	write := func(as []backend.Activity) error {
		for _, a := range as {
			wrote = true
			if *asJSON {
				if err := enc.Encode(a); err != nil {
					return err
				}
				continue
			}
			if a.Session != session { // 再開で session が入れ替わった境目 (最初の session も名乗る)
				session = a.Session
				if _, err := fmt.Fprintf(stdout, "── session %s ──\n", orDashCLI(a.Session)); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(stdout, "%s  %s\n", a.At.Local().Format("01-02 15:04:05"), a.Line()); err != nil {
				return err
			}
		}
		return nil
	}
	as, err := l.Next()
	if werr := write(as); werr != nil {
		err = werr
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con card log:", err)
		return 1
	}
	if !*follow {
		if !wrote && !*asJSON {
			_, _ = fmt.Fprintf(stderr, "pro-con card log: %s の PG の活動はまだ無い (pro-con が起動した session の transcript が見つからない)\n", c.ID)
		}
		return 0
	}
	ctx := env.ctx
	if ctx == nil { // --follow は ctrl+c で終わる (rc=0。pro-con log と同じ)
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
	}
	// transcript の追記は dispatcher から知らされないので、viewPoll ごとの読み直しで追う (知らせは再開で session が入れ替わったのを早く拾う)
	// 読めない transcript があっても追い続ける (ほかの session は読める。消えた transcript は次に探し直す)。同じ理由は 1 度だけ知らせる
	warned := ""
	err = watchDir(ctx, env.dir, stderr, func() (bool, error) {
		as, err := l.Next()
		if werr := write(as); werr != nil {
			return false, werr // 出力先が閉じた等 (続けても読む人がいない)
		}
		if err != nil && err.Error() != warned {
			warned = err.Error()
			_, _ = fmt.Fprintln(stderr, "pro-con card log: 読めないところがある (追い続ける):", err)
		}
		return false, nil
	})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con card log:", err)
		return 1
	}
	return 0
}
