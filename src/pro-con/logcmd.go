package main

// pro-con log — dispatcher の出来事の記録 (状態の置き場の events.jsonl) を読む口 (issue 444。親の設計は 441)。
//
// 🚨 読むだけ (441 の守ること 1)。受付の箱・記録・events.jsonl に何も書かず、socket へ wake / notify を送らない
// (--follow が送るのは購読の sub だけ)。TestLogDoesNotWrite が固定する。

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"pro-con/eventlog"
)

const logUsage = "usage: pro-con log [--card <カード>] [--follow] [--since <時刻>] [--json]\n" +
	"  --since は 15:04 / 2006-01-02 15:04 / RFC 3339 / 長さ (10m = 10 分前から)"

func runLog(ctx context.Context, args []string, dir string, now func() time.Time, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pro-con log", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cardID := fs.String("card", "", "")
	follow := fs.Bool("follow", false, "")
	sinceArg := fs.String("since", "", "")
	asJSON := fs.Bool("json", false, "")
	if err := fs.Parse(args); errors.Is(err, flag.ErrHelp) {
		_, _ = fmt.Fprintln(stdout, logUsage)
		return 0
	} else if err != nil || fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, logUsage)
		return 2
	}
	var since time.Time
	if *sinceArg != "" {
		t, err := parseSince(*sinceArg, now())
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "pro-con log: %v\n%s\n", err, logUsage)
			return 2
		}
		since = t
	}
	keep := func(e eventlog.Event) bool {
		return (*cardID == "" || strings.EqualFold(e.Card, *cardID)) && !e.At.Before(since)
	}
	enc := json.NewEncoder(stdout) // --json は 1 行 1 出来事 (--follow でも行ごとに読める)
	write := func(evs []eventlog.Event) error {
		for _, e := range evs {
			if !keep(e) {
				continue
			}
			if *asJSON {
				if err := enc.Encode(e); err != nil {
					return err
				}
				continue
			}
			if _, err := fmt.Fprintln(stdout, formatEvent(e)); err != nil {
				return err
			}
		}
		return nil
	}
	f := eventlog.NewFollower(dir)
	evs, err := f.Next()
	if err == nil {
		err = write(evs)
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con log:", err)
		return 1
	}
	if !*follow {
		return 0
	}
	// dispatcher は出来事を書いてから知らせる (Dispatcher.Record → Changed) ので、知らせで読み直せば即時に出る
	err = watchDir(ctx, dir, func() (bool, error) {
		evs, err := f.Next()
		if err != nil {
			return false, err
		}
		return false, write(evs)
	})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con log:", err)
		return 1
	}
	return 0
}

// formatEvent は人が読む 1 行 (種類・カード・session は ASCII なので桁で揃う)。
func formatEvent(e eventlog.Event) string {
	return fmt.Sprintf("%s  %-8s  %-6s  %-12s  %s", e.At.Local().Format("01-02 15:04:05"), e.Kind, orDashCLI(e.Card), orDashCLI(e.Session), e.Reason)
}

// parseSince は --since の時刻を読む。時刻だけなら今日 (手元の時刻)、長さなら今からその分前。
func parseSince(v string, now time.Time) (time.Time, error) {
	if d, err := time.ParseDuration(v); err == nil {
		return now.Add(-d), nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, v, now.Location()); err == nil {
			return t, nil
		}
	}
	for _, layout := range []string{"15:04:05", "15:04"} {
		if t, err := time.ParseInLocation(layout, v, now.Location()); err == nil {
			y, m, d := now.Date()
			return time.Date(y, m, d, t.Hour(), t.Minute(), t.Second(), 0, now.Location()), nil
		}
	}
	return time.Time{}, fmt.Errorf("--since の時刻を読めない: %q", v)
}
