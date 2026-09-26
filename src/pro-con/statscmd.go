package main

// pro-con stats — 閉じたカードの所要の記録 (状態の置き場の metrics.jsonl。issue 516) を束ねて、件数と中央値・最大を出す口。
// 見積もりのポイントごとに実際の時間を並べて比べる (既定)。repo ごと・週ごとにも束ねる。
//
// 🚨 読むだけ。記録・所要の記録・受付の箱に何も書かない (書き手は dispatcher だけ)。

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"pro-con/metrics"
	"pro-con/store"
)

const statsUsage = "usage: pro-con stats [--since <時刻>] [--by points|repo|week|all] [--json]\n" +
	"  --since は閉じた時刻で絞る (30d = 30 日前から / 2006-01-02 / 15:04 / RFC 3339。既定は残っている全部 = 最長 90 日)\n" +
	"  --by は束ね方 (既定 points = 見積もりのポイントごと)。時間は「中央値 / 最大」、枠は PG の API 料金換算と 5 時間枠の % (粗い値。issue 449)"

func runStats(args []string, dir string, now func() time.Time, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pro-con stats", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	sinceArg := fs.String("since", "", "")
	by := fs.String("by", metrics.ByPoints, "")
	asJSON := fs.Bool("json", false, "")
	if err := fs.Parse(args); errors.Is(err, flag.ErrHelp) {
		_, _ = fmt.Fprintln(stdout, statsUsage)
		return 0
	} else if err != nil || fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, statsUsage)
		return 2
	}
	var since time.Time
	if *sinceArg != "" {
		t, err := parseSince(*sinceArg, now())
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "pro-con stats: %v\n%s\n", err, statsUsage)
			return 2
		}
		since = t
	}
	if *by == "all" {
		*by = ""
	}
	rows, err := store.LoadMetrics(dir)
	if err != nil && rows == nil {
		_, _ = fmt.Fprintln(stderr, "pro-con stats:", err)
		return 1
	}
	if err != nil { // 読めない行は飛ばして出す (何行飛ばしたかは知らせる)
		_, _ = fmt.Fprintln(stderr, "pro-con stats:", err)
	}
	rows = metrics.Since(rows, since)
	groups, gerr := metrics.Summarize(rows, *by)
	if gerr != nil {
		_, _ = fmt.Fprintf(stderr, "pro-con stats: %v\n%s\n", gerr, statsUsage)
		return 2
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(groups); err != nil {
			_, _ = fmt.Fprintln(stderr, "pro-con stats:", err)
			return 1
		}
		return 0
	}
	if _, err := io.WriteString(stdout, formatStats(rows, groups, since)); err != nil {
		_, _ = fmt.Fprintln(stderr, "pro-con stats:", err)
		return 1
	}
	return 0
}

// formatStats は人が読む形 (束ね 1 つを 1 行。全角の混じる見出しを桁で揃えない)。
func formatStats(rows []metrics.Row, groups []metrics.Group, since time.Time) string {
	var b strings.Builder
	from := "残っている全部"
	if !since.IsZero() {
		from = since.Local().Format("2006-01-02 15:04") + " から"
	}
	fmt.Fprintf(&b, "閉じたカード %d 枚 (%s。時間は中央値 / 最大)\n", len(rows), from)
	if len(rows) == 0 {
		b.WriteString("(所要の記録がまだ無い。dispatcher がカードを閉じたときに書く)\n")
		return b.String()
	}
	for _, g := range groups {
		fmt.Fprintf(&b, "%s: %d 枚  所要 %s  作業中 %s  人の番 %s  枠 %s\n", g.Key, g.N,
			spanPair(g.Total, g.N), spanPair(g.Running, g.N), spanPair(g.Human, g.N), usagePair(g.USD, g.Pct, g.N))
	}
	return b.String()
}

// spanPair は時間の「中央値 / 最大」。束ねの一部の行にしか無ければ、何枚から出したかを添える。
func spanPair(s metrics.Summary, n int) string {
	if s.N == 0 {
		return "—"
	}
	return fmt.Sprintf("%s / %s", span(s.Median), span(s.Max)) + partOf(s.N, n)
}

func usagePair(usd, pct metrics.Summary, n int) string {
	if usd.N == 0 {
		return "— (取れなかった)"
	}
	return fmt.Sprintf("$%.2f / $%.2f (5 時間枠 %.1f%%)", usd.Median, usd.Max, pct.Median) + partOf(usd.N, n)
}

func partOf(k, n int) string {
	if k == n {
		return ""
	}
	return fmt.Sprintf(" (%d 枚から)", k)
}

// span は秒を短い長さの文字列にする (1d2h / 3h05m / 42m / 30s)。
func span(sec float64) string {
	d := time.Duration(sec) * time.Second
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}
