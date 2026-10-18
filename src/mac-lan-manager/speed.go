package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"mac-lan-manager/lanmgr"
	"mac-lan-manager/netinfo"
	"mac-lan-manager/setup"
	"mac-lan-manager/speedtest"
)

// speedReport は speedtest の 1 台分 (--json の要素)。
type speedReport struct {
	Host       string  `json:"host"`
	Name       string  `json:"name"`
	IP         string  `json:"ip,omitempty"`
	Link       string  `json:"link,omitempty"` // "有線" / "無線" / "" (分からない)
	UpBps      float64 `json:"up_bps"`
	DownBps    float64 `json:"down_bps"`
	Capped100M bool    `json:"capped_100m"` // 有線で 100 Mbps 付近の頭打ち (途中に 100M の機器・ケーブルの疑い)
	Error      string  `json:"error,omitempty"`
	Skipped    string  `json:"skipped,omitempty"` // 測らなかった理由 (鍵で入れない・ユーザー名が分からない等)
}

// runSpeed は `mac-lan-manager speedtest`: 管理対象の相手 (引数を渡せばその相手だけ) との通信速度を ssh 越しに 1 台ずつ測る。
// 宛先・経路・測らない相手の規則は画面の t と同じ (lanmgr.SpeedTargets)。画面と違って確認の板を出さない (コマンドを打つことが確認)。
// rc: 0 = 全部測れた / 1 = 測れない相手がいた (1 台でも) / 2 = 引数の誤り・知らない相手 / 130 = 止められた。
func runSpeed(args []string, stdout, stderr io.Writer, now func() time.Time, stop <-chan struct{}, newModel func() (*lanmgr.Model, error),
	test func(context.Context, setup.Dest, func() time.Time) speedtest.Result) int {
	fs := flag.NewFlagSet("speedtest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "JSON で出す")
	wait := fs.Duration("wait", 8*time.Second, "Bonjour の走査を待つ上限")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	m, err := newModel()
	if err != nil {
		fmt.Fprintln(stderr, "mac-lan-manager speedtest:", err)
		return 1
	}
	defer m.Close()
	if !settle(m, now, now().Add(*wait), stop, nil) {
		return 130
	}
	targets, err := m.SpeedTargets()
	if err != nil {
		fmt.Fprintln(stderr, "mac-lan-manager speedtest:", err)
		return 1
	}
	if want := fs.Args(); len(want) > 0 {
		picked := make([]lanmgr.SpeedTarget, 0, len(want))
		for _, w := range want {
			i := slices.IndexFunc(targets, func(t lanmgr.SpeedTarget) bool {
				return t.Host == w || t.Name == w || strings.TrimSuffix(t.Host, ".local") == w
			})
			if i < 0 {
				fmt.Fprintf(stderr, "mac-lan-manager speedtest: 管理対象に %q が無い (台帳のホスト名か別名で指定する)\n", w)
				return 2
			}
			picked = append(picked, targets[i])
		}
		targets = picked
	}
	if len(targets) == 0 {
		fmt.Fprintln(stderr, "mac-lan-manager speedtest: 管理対象の Mac が無い (台帳に登録してから)")
		return 1
	}
	rc := 0
	reports := make([]speedReport, 0, len(targets))
	for _, t := range targets { // 1 台ずつ (同時に流すと互いの帯域を食って値が意味を失う)
		r := speedReport{Host: t.Host, Name: t.Name, IP: t.Dest.IP, Link: t.Link.Label()}
		switch {
		case t.Refusal != "":
			r.Skipped, rc = t.Refusal, 1
		default:
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { // 止める合図で測定を取り消す (ssh を止めれば相手の wc / head も終わる)
				select {
				case <-stop:
					cancel()
				case <-done:
				}
			}()
			res := test(ctx, t.Dest, now)
			close(done)
			stopped := ctx.Err() != nil
			cancel()
			if stopped {
				return 130
			}
			r.UpBps, r.DownBps = res.UpBps, res.DownBps
			if res.Err != nil {
				r.Error, rc = res.Err.Error(), 1
			} else {
				r.Capped100M = t.Link == netinfo.LinkWired && speedtest.Capped100M(res)
			}
		}
		reports = append(reports, r)
		if !*asJSON {
			printSpeed(stdout, r)
		}
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(reports); err != nil {
			fmt.Fprintln(stderr, "mac-lan-manager speedtest:", err)
			return 1
		}
	}
	return rc
}

func printSpeed(w io.Writer, r speedReport) {
	head := r.Name
	if r.Name != r.Host {
		head += " (" + r.Host + ")"
	}
	switch {
	case r.Skipped != "":
		fmt.Fprintf(w, "%s  - 測らない: %s\n", head, r.Skipped)
	case r.Error != "":
		fmt.Fprintf(w, "%s  ✗ %s  [%s %s]\n", head, strings.ReplaceAll(r.Error, "\n", " / "), r.IP, or(r.Link, "経路不明"))
	default:
		fmt.Fprintf(w, "%s  ↑ %s  ↓ %s  [%s %s]", head, speedtest.Format(r.UpBps), speedtest.Format(r.DownBps), r.IP, or(r.Link, "経路不明"))
		if r.Capped100M {
			fmt.Fprint(w, "  🚨 100 Mbps 付近で頭打ち (途中に 100M の機器・ケーブル?)")
		}
		fmt.Fprintln(w)
	}
}
