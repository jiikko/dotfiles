package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"mac-lan-manager/lanmgr"
)

// runDump は `mac-lan-manager dump`: TUI を起こさずに、この Mac の網・台帳・Bonjour の結果を画面と同じ判定で出す
// (LLM・人のデバッグ用。読むだけで何も書かない)。走査が終わるか --wait の上限まで待つ。
func runDump(args []string, stdout, stderr io.Writer, now func() time.Time, newModel func() (*lanmgr.Model, error)) int {
	fs := flag.NewFlagSet("dump", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "JSON で出す")
	wait := fs.Duration("wait", 8*time.Second, "Bonjour の走査を待つ上限")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	m, err := newModel()
	if err != nil {
		fmt.Fprintln(stderr, "mac-lan-manager dump:", err)
		return 1
	}
	defer m.Close()
	deadline := now().Add(*wait)
	for m.Advance(now()); m.Busy() && now().Before(deadline); m.Advance(now()) {
		select { // 走査の合図か、刻み (ネットワークの読み込みは合図を出さないので刻みで見る)
		case <-m.Changed():
		case <-time.After(100 * time.Millisecond):
		}
	}
	rep := m.Report()
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintln(stderr, "mac-lan-manager dump:", err)
			return 1
		}
		return 0
	}
	writeDumpText(stdout, rep)
	return 0
}

func writeDumpText(w io.Writer, r lanmgr.Report) {
	fmt.Fprintf(w, "host: %s  net_ready: %v  busy(打ち切り): %v  台帳: %d 台\n", or(r.Host, "(不明)"), r.NetReady, r.Busy, r.LedgerSize)
	if r.NetError != "" {
		fmt.Fprintf(w, "net_error: %s\n", r.NetError)
	}
	fmt.Fprintln(w, "\n# この Mac のサービス (networksetup)")
	for _, s := range r.Services {
		fmt.Fprintf(w, "  %-24s %-8s active=%-5v ip=%-16s method=%-6s router=%s\n", s.Name, s.Device, s.Active, or(s.IP, "-"), or(s.Method, "-"), or(s.Router, "-"))
	}
	fmt.Fprintln(w, "\n# 一覧 (画面と同じ判定。connect = s / y / o が繋ぐ先)")
	for _, x := range r.Rows {
		mark := " "
		switch {
		case x.Self:
			mark = "*"
		case x.Managed:
			mark = "M"
		}
		fmt.Fprintf(w, "%s %-28s alias=%-14s main=%-16s lan=%-16s connect=%-16s ssh=%v vnc=%v smb=%v seen=%v user=%s\n",
			mark, x.Host, or(or(x.Alias, x.Instance), "-"), or(x.MainIP, "-"), or(x.LanIP, "-"), or(x.ConnectIP, "-"), x.SSH, x.VNC, x.SMB, x.Seen, or(x.User, "?"))
		for _, o := range x.Others {
			fmt.Fprintf(w, "    その他: %s %s (%s)\n", o.Device, o.Addr, o.Kind)
		}
	}
	fmt.Fprintln(w, "\n# Bonjour の生の結果 (分類の前。addrs は I/F の番号 → IPv4)")
	if len(r.Peers) == 0 {
		fmt.Fprintln(w, "  (無し)")
	}
	for _, p := range r.Peers {
		addrs := make([]string, 0, len(p.Addrs))
		for _, i := range slices.Sorted(maps.Keys(p.Addrs)) {
			addrs = append(addrs, fmt.Sprintf("%d:%s", i, p.Addrs[i]))
		}
		fmt.Fprintf(w, "  %-28s ssh=%v vnc=%v smb=%v addrs=[%s] instances=%q\n", p.Host, p.SSH, p.VNC, p.SMB, strings.Join(addrs, " "), p.Instances)
	}
	if len(r.Notices) > 0 {
		fmt.Fprintln(w, "\n# 知らせ")
		for _, n := range r.Notices {
			fmt.Fprintf(w, "  ok=%v %s\n", n.OK, n.Text)
		}
	}
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
