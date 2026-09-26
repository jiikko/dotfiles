package main

// pro-con du — pro-con が作った物のディスクの使用量と内訳 (issue 456。読むだけ。測り方は package diskuse)。
// 🚨 重い (worktree を全部歩くので数秒)。画面は描くたびに呼ばない。

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"pro-con/diskuse"
	"pro-con/live"
)

const duUsage = `usage: pro-con du [--json] [--all]   (pro-con が作った物のディスクの使用量と内訳。読むだけ。数秒かかる)
  --all   内訳を全部出す (既定は置き場ごとに大きい順の 5 つ)`

func runDU(args []string, home string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pro-con du", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "")
	all := fs.Bool("all", false, "")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, duUsage)
		return 2
	}
	bin, _ := os.Executable()
	in, warns := live.DiskInput(stateDir(home), liveDir(home), filepath.Join(home, ".claude", "projects"), bin)
	u := diskuse.Measure(in)
	u.Warnings = append(warns, u.Warnings...)
	for _, w := range u.Warnings {
		_, _ = fmt.Fprintln(stderr, "pro-con du:", w)
	}
	if *asJSON {
		data, _ := json.MarshalIndent(u, "", "  ")
		_, _ = fmt.Fprintln(stdout, string(data))
	} else {
		writeDU(stdout, u, *all)
	}
	if len(u.Warnings) > 0 {
		return 1
	}
	return 0
}

// writeDU は合計、置き場ごとの大きさと数、内訳 (大きい順) を出す。
func writeDU(w io.Writer, u diskuse.Usage, all bool) {
	_, _ = fmt.Fprintf(w, "合計 %s  (%s に測った。%s かかった)\n", diskuse.Human(u.Total), u.MeasuredAt.Local().Format("01-02 15:04:05"), u.Took.Round(10*time.Millisecond))
	for _, g := range u.Groups {
		_, _ = fmt.Fprintf(w, "%-6s  %3d 個  %s\n", diskuse.Human(g.Bytes), g.Count, g.Name)
		for i, it := range g.Items {
			if !all && i == 5 {
				_, _ = fmt.Fprintf(w, "          … ほか %d 個 (--all で全部)\n", len(g.Items)-i)
				break
			}
			note := ""
			if it.Card != "" {
				note = "  " + it.Card
				if it.Done {
					note += " (完了)"
				}
			}
			_, _ = fmt.Fprintf(w, "  %-6s  %s%s\n", diskuse.Human(it.Bytes), it.Name, note)
		}
	}
}
