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
	"mac-lan-manager/reach"
)

// pingAddr / pingName / pingReport は ping の --json の形。
type pingAddr struct {
	IP        string  `json:"ip"`
	Sent      int     `json:"ping_sent"`
	Recv      int     `json:"ping_recv"`
	AvgMs     float64 `json:"ping_avg_ms,omitempty"`
	PingError string  `json:"ping_error,omitempty"` // 統計を読めなかった (届かないのとは別)
	SSHOpen   bool    `json:"ssh_open"`
	SSHError  string  `json:"ssh_error,omitempty"`
	Reachable bool    `json:"reachable"` // ping が返る か 22 番が開く
}

type pingName struct {
	Name        string     `json:"name"`
	LookupError string     `json:"lookup_error,omitempty"`
	Addrs       []pingAddr `json:"addrs"`
	Reachable   bool       `json:"reachable"`              // 引けた IP のどれかに届く
	Elsewhere   bool       `json:"lan_mismatch,omitempty"` // 有線の名前が内部 IP と違う IP に引けた
}

type pingReport struct {
	Host        string     `json:"host"`
	Name        string     `json:"name"`
	LanIP       string     `json:"lan_ip,omitempty"`
	Names       []pingName `json:"names"`
	ScanSettled bool       `json:"scan_settled"` // speedReport と同じ
	ScanError   string     `json:"scan_error,omitempty"`
}

// runPing は `mac-lan-manager ping`: 管理対象の相手 (引数を渡せばその相手だけ) の名前ごとに、名前を引いて ping と ssh の口 (22 番) を確かめる。
// 確かめる名前・相手の規則は画面の p と同じ (lanmgr.ReachTargets)。届いた相手が本人かは見ない (同じ IP の別の機器でも「届く」)。
// rc: 0 = 確かめた名前が全部届く / 1 = 届かない・引けない名前があった・Bonjour で探せない / 2 = 引数の誤り・知らない相手 / 130 = 止められた。
func runPing(args []string, stdout, stderr io.Writer, now func() time.Time, stop <-chan struct{}, newModel func() (*lanmgr.Model, error),
	check func(context.Context, []string) []reach.Name) int {
	fs := flag.NewFlagSet("ping", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "JSON で出す")
	wait := fs.Duration("wait", 8*time.Second, "Bonjour の走査を待つ上限")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	m, err := newModel()
	if err != nil {
		fmt.Fprintln(stderr, "mac-lan-manager ping:", err)
		return 1
	}
	defer m.Close()
	ok, settled := settle(m, now, now().Add(*wait), stop, nil)
	if !ok {
		return 130
	}
	if !settled && !*asJSON {
		fmt.Fprintln(stderr, "mac-lan-manager ping: 🚨 裏の読み込み (Bonjour の走査・有線の名前など) が --wait までに終わらなかった (途中の一覧で名前を決めた。--wait を延ばす)")
	}
	scanErr := m.Report().ScanError
	rc := 0
	if scanErr != "" {
		rc = 1
		if !*asJSON {
			fmt.Fprintln(stderr, "mac-lan-manager ping: 🚨 Bonjour で探せない:", scanErr)
		}
	}
	targets, err := m.ReachTargets()
	if err != nil {
		fmt.Fprintln(stderr, "mac-lan-manager ping:", err)
		return 1
	}
	if want := fs.Args(); len(want) > 0 {
		picked := make([]lanmgr.ReachTarget, 0, len(want))
		for _, w := range want {
			t, why := pickTarget(targets, func(t lanmgr.ReachTarget) (string, string) { return t.Host, t.Name }, w)
			if why != "" {
				fmt.Fprintln(stderr, "mac-lan-manager ping:", why)
				return 2
			}
			if !slices.ContainsFunc(picked, func(p lanmgr.ReachTarget) bool { return p.Host == t.Host }) {
				picked = append(picked, t)
			}
		}
		targets = picked
	}
	if len(targets) == 0 {
		fmt.Fprintln(stderr, "mac-lan-manager ping: 管理対象の Mac が無い (台帳に登録してから)")
		return 1
	}
	reports := make([]pingReport, 0, len(targets))
	for _, t := range targets {
		r := pingReport{Host: t.Host, Name: t.Name, LanIP: t.LanIP, Names: []pingName{}, ScanSettled: settled, ScanError: scanErr}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			select {
			case <-stop:
				cancel()
			case <-done:
			}
		}()
		names := check(ctx, t.Names)
		close(done)
		cancel()
		// 🚨 結果より先に止める合図を見る: 端末の Ctrl-C は子の ping にも届き、ping は統計 (0 received) を出してすぐ終わるので、
		// 結果だけ見ると「届かない」になる。ctx.Err() では見ない (合図を受けた goroutine が cancel する前に check が戻りうる)
		select {
		case <-stop:
			return 130
		default:
		}
		for _, n := range names {
			pn := toPingName(n, t.LanIP)
			if !pn.Reachable {
				rc = 1
			}
			r.Names = append(r.Names, pn)
		}
		if !*asJSON {
			printPing(stdout, r, names)
		}
		reports = append(reports, r)
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(reports); err != nil {
			fmt.Fprintln(stderr, "mac-lan-manager ping:", err)
			return 1
		}
	}
	return rc
}

func toPingName(n reach.Name, lanIP string) pingName {
	pn := pingName{Name: n.Name, Addrs: []pingAddr{}, Reachable: n.Reachable()}
	if n.LookupErr != nil {
		pn.LookupError = n.LookupErr.Error()
	}
	if strings.HasSuffix(n.Name, ".wired.local") {
		pn.Elsewhere = n.Elsewhere(lanIP)
	}
	for _, a := range n.Addrs {
		pa := pingAddr{IP: a.IP, Sent: a.Ping.Sent, Recv: a.Ping.Recv, AvgMs: a.Ping.AvgMs, SSHOpen: a.SSH == nil, Reachable: a.Reachable()}
		if a.Ping.Err != nil {
			pa.PingError = a.Ping.Err.Error()
		}
		if a.SSH != nil {
			pa.SSHError = a.SSH.Error()
		}
		pn.Addrs = append(pn.Addrs, pa)
	}
	return pn
}

func pingHead(r pingReport) string {
	if r.Name != r.Host {
		return r.Name + " (" + r.Host + ")"
	}
	return r.Name
}

func printPing(w io.Writer, r pingReport, names []reach.Name) {
	fmt.Fprintln(w, pingHead(r))
	for i, n := range names {
		mark := "✓"
		if !r.Names[i].Reachable {
			mark = "✗"
		}
		line := "  " + mark + " " + lanmgr.ReachSummary(n)
		if r.Names[i].Elsewhere {
			line += "  🚨 内部 IP (" + r.LanIP + ") と違う"
		}
		fmt.Fprintln(w, line)
	}
}
