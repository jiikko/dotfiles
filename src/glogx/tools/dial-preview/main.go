// dial-preview は全画面 ratelimit ダッシュボード (usage.RenderDashboard) の見た目プレビュー。
// 実端末で走らせて配色・目盛り・文字量を決めるための道具で、本体の描画そのものを呼ぶので
// 「プレビューでは良かったのに本体で違う」が起きない。
//
//	go run ./tools/dial-preview            # 端末サイズ (既定 120x36) で 4 枠
//	go run ./tools/dial-preview -w 90 -h 24
//	go run ./tools/dial-preview -mono      # 色なし (幅ズレの確認用)
//	go run ./tools/dial-preview -unused    # codex の 5h を未消費 (resetsAt 無し) にする
package main

import (
	"flag"
	"fmt"
	"time"

	"glogx/usage"
)

func main() {
	w := flag.Int("w", 120, "幅 (桁)")
	h := flag.Int("h", 36, "高さ (行)")
	mono := flag.Bool("mono", false, "色を付けない")
	unused := flag.Bool("unused", false, "codex の 5h を未消費 (リセット時刻なし) にする")
	flag.Parse()

	now := time.Date(2026, 8, 31, 22, 14, 0, 0, time.Local)
	mins := func(d time.Duration) int64 { return int64(d / time.Minute) }
	snap := &usage.Snapshot{
		Version:      "2.1.216",
		CodexVersion: "0.144.6",
		Windows: []usage.Window{
			{Label: "5h", Percent: 62, ResetAt: now.Add(108 * time.Minute), WindowMins: mins(5 * time.Hour)},
			{Label: "7d", Percent: 78, ResetAt: now.Add(3420 * time.Minute), WindowMins: mins(7 * 24 * time.Hour)},
			{Label: "cx5h", Source: usage.SourceCodex, Percent: 31, ResetAt: now.Add(185 * time.Minute), WindowMins: mins(5 * time.Hour)},
			{Label: "cx7d", Source: usage.SourceCodex, Percent: 44, ResetAt: now.Add(5900 * time.Minute), WindowMins: mins(7 * 24 * time.Hour)},
		},
	}
	if *unused {
		snap.Windows[2] = usage.Window{Label: "cx5h", Source: usage.SourceCodex, Unused: true, WindowMins: mins(5 * time.Hour)}
	}
	for _, line := range usage.RenderDashboard(snap, now, *w, *h, !*mono) {
		fmt.Println(line)
	}
}
