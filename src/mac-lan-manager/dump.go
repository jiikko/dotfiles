package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"mac-lan-manager/lanmgr"
	"mac-lan-manager/setup"
)

// runDump は `mac-lan-manager dump`: TUI を起こさずに、この Mac の網・台帳・Bonjour の結果を画面と同じ判定で出す
// (LLM・人のデバッグ用。読むだけ: 台帳・設定・操作のログに書かない。初回だけ旧い置き場から移す)。走査が終わるか --wait の上限まで待つ。
// --peer を付けると、この Mac の代わりにその相手を ssh で読む (observe。段 4 の初期設定が見るのと同じ PeerNet)。
func runDump(args []string, stdout, stderr io.Writer, now func() time.Time, stop <-chan struct{}, newModel func() (*lanmgr.Model, error),
	observe func(context.Context, setup.Dest) setup.PeerNet) int {
	fs := flag.NewFlagSet("dump", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "JSON で出す")
	wait := fs.Duration("wait", 8*time.Second, "Bonjour の走査を待つ上限")
	peer := fs.String("peer", "", "この IP の相手を ssh で読む (鍵で入れるか・ルーターの無い有線のサービス)")
	user := fs.String("user", os.Getenv("USER"), "--peer で入るユーザー")
	host := fs.String("host", "", "--peer の相手の Bonjour のホスト名 (known_hosts の鍵の名前。既定は IP)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *peer != "" {
		return dumpPeer(setup.Dest{User: *user, IP: *peer, HostKeyAlias: or(*host, *peer)}, *asJSON, stdout, stderr, stop, observe)
	}
	start := now()
	m, err := newModel()
	if err != nil {
		fmt.Fprintln(stderr, "mac-lan-manager dump:", err)
		return 1
	}
	defer m.Close()
	// 時間: 開くまで (New = glogx の L のキーを止める分) と最初の描画 (L の最初のコマ) と、裏の読み込み・走査が終わるまで。
	// 「L が遅い」を測る口 (ユーザー報告 2026-10-16: 画面が変わるまで 1 秒)
	tm := dumpTimings{New: now().Sub(start)}
	t := now()
	m.Resize(200, 50)
	_ = m.View()
	tm.FirstView = now().Sub(t)
	deadline := start.Add(*wait)
	for m.Advance(now()); m.Busy() && now().Before(deadline); m.Advance(now()) {
		tm.observe(m.Report(), now().Sub(start))
		select { // 走査の合図か、刻み (ネットワークの読み込みは合図を出さないので刻みで見る)
		case <-m.Changed():
		case <-time.After(20 * time.Millisecond):
		case <-stop: // SIGINT / SIGTERM / SIGHUP: 待ちを打ち切り、defer の Close で dns-sd を止めてから戻る (監査の指摘)
			return 130
		}
	}
	rep := m.Report()
	tm.observe(rep, now().Sub(start))
	if !rep.Busy {
		tm.Settled = now().Sub(start)
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(struct {
			lanmgr.Report
			Timings dumpTimings `json:"timings"`
		}{rep, tm}); err != nil {
			fmt.Fprintln(stderr, "mac-lan-manager dump:", err)
			return 1
		}
		return 0
	}
	writeDumpText(stdout, rep, tm)
	return 0
}

// dumpTimings は開いてからの所要 (0 は「その時点まで起きなかった」)。
type dumpTimings struct {
	New       time.Duration `json:"new_ns"`        // lanmgr.New (glogx の L のキーを止める分)
	FirstView time.Duration `json:"first_view_ns"` // 最初の描画 (200x50)
	NetReady  time.Duration `json:"net_ready_ns"`  // この Mac のネットワークを読み終えた
	FirstPeer time.Duration `json:"first_peer_ns"` // Bonjour で最初の Mac が見えた
	Settled   time.Duration `json:"settled_ns"`    // 走査が落ち着いた (Busy でなくなった)
}

func (t *dumpTimings) observe(r lanmgr.Report, since time.Duration) {
	if t.NetReady == 0 && r.NetReady {
		t.NetReady = since
	}
	if t.FirstPeer == 0 && len(r.Peers) > 0 {
		t.FirstPeer = since
	}
}

func writeDumpText(w io.Writer, r lanmgr.Report, tm dumpTimings) {
	fmt.Fprintf(w, "host: %s  net_ready: %v  busy(打ち切り): %v  台帳: %d 台\n", or(r.Host, "(不明)"), r.NetReady, r.Busy, r.LedgerSize)
	if r.NetError != "" {
		fmt.Fprintf(w, "net_error: %s\n", r.NetError)
	}
	if r.ScanError != "" {
		fmt.Fprintf(w, "scan_error: %s  (0 台なら「居ない」ではなく「探せていない」)\n", r.ScanError)
	}
	fmt.Fprintln(w, "\n# この Mac のサービス (networksetup)")
	for _, s := range r.Services {
		fmt.Fprintf(w, "  %-24s %-8s active=%-5v ip=%-16s method=%-6s router=%-14s read=%-5v disabled=%v\n", s.Name, s.Device, s.Active, or(s.IP, "-"), or(s.Method, "-"), or(s.Router, "-"), s.Read, s.Disabled)
	}
	fmt.Fprintln(w, "\n# 一覧 (画面と同じ判定。target = s / Y / y / o が使う宛先、connect = その IP。hidden = 有線だけの表示で隠れる)")
	for _, x := range r.Rows {
		mark := " "
		switch {
		case x.Self:
			mark = "*"
		case x.Managed:
			mark = "M"
		}
		fmt.Fprintf(w, "%s %-28s alias=%-14s main=%-16s lan=%-16s fixed=%v connect=%-16s target=%-24s ssh=%v vnc=%v smb=%v seen=%v user=%s\n",
			mark, x.Host, or(or(x.Alias, x.Instance), "-"), or(x.MainIP, "-"), or(x.LanIP, "-"), x.LanFixed, or(x.ConnectIP, "-"), or(x.Target, "-"),
			x.SSH, x.VNC, x.SMB, x.Seen, or(x.User, "?"))
		if !x.Listed {
			fmt.Fprintln(w, "    hidden: 有線だけの表示で一覧に出ない")
		}
		if x.Pending {
			fmt.Fprintln(w, "    初期設定: 途中 (一覧の !)")
		}
		if x.WiredName != "" {
			fmt.Fprintf(w, "    有線の名前: %s %s\n", x.WiredName, map[bool]string{true: "✓ 引ける", false: or(x.WiredNote, "引けない")}[x.WiredOK])
		}
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
	ms := func(d time.Duration) string {
		if d == 0 {
			return "-"
		}
		return d.Round(time.Millisecond).String()
	}
	fmt.Fprintln(w, "\n# 時間 (開いてから。New と最初の描画が glogx の L で画面が変わるまでの分。- はその時点まで起きなかった)")
	fmt.Fprintf(w, "  New=%s  最初の描画=%s  ネットワーク読了=%s  最初の Mac=%s  走査の落ち着き=%s\n",
		ms(tm.New), ms(tm.FirstView), ms(tm.NetReady), ms(tm.FirstPeer), ms(tm.Settled))
	if len(r.Notices) > 0 {
		fmt.Fprintln(w, "\n# 知らせ")
		for _, n := range r.Notices {
			fmt.Fprintf(w, "  ok=%v %s\n", n.OK, n.Text)
		}
	}
}

// peerReport は dump --peer の出力。Key は KeyOK / KeyErr の読み (ok = 鍵で入れる / denied = 鍵で入れない /
// host_key_unknown = 初めての相手 (ホスト鍵をまだ知らない) / unknown = 判定できない)。
type peerReport struct {
	User         string `json:"user"`
	IP           string `json:"ip"`
	HostKeyAlias string `json:"host_key_alias"`
	Key          string `json:"key"`
	KeyOK        bool   `json:"key_ok"`
	KeyErr       string `json:"key_err,omitempty"`
	NetKnown     bool   `json:"net_known"`
	Service      string `json:"service"`
	PeerIP       string `json:"service_ip"`
	Method       string `json:"method"`
	Mask         string `json:"mask"`
	Router       string `json:"router"`
}

func dumpPeer(d setup.Dest, asJSON bool, stdout, stderr io.Writer, stop <-chan struct{}, observe func(context.Context, setup.Dest) setup.PeerNet) int {
	if err := d.Validate(); err != nil {
		fmt.Fprintln(stderr, "mac-lan-manager dump --peer:", err)
		return 2
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { // SIGINT などで ssh を止める
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	p := observe(ctx, d)
	r := peerReport{User: d.User, IP: d.IP, HostKeyAlias: d.HostKeyAlias, KeyOK: p.KeyOK, NetKnown: p.NetKnown,
		Service: p.Service, PeerIP: p.IP, Method: p.Method, Mask: p.Mask, Router: p.Router}
	switch {
	case p.KeyOK:
		r.Key = "ok"
	case p.HostKeyUnknown:
		r.Key = "host_key_unknown"
	case p.KeyErr == nil:
		r.Key = "denied"
	default:
		r.Key, r.KeyErr = "unknown", p.KeyErr.Error()
	}
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(r); err != nil {
			fmt.Fprintln(stderr, "mac-lan-manager dump:", err)
			return 1
		}
		return 0
	}
	key := map[string]string{"ok": "鍵で入れる", "denied": "鍵で入れない (Permission denied)",
		"host_key_unknown": "初めての相手 (ホスト鍵をまだ知らない。鍵を置く手順で指紋を確かめる)", "unknown": "判定できない (届かない・期限切れ)"}[r.Key]
	fmt.Fprintf(stdout, "peer: %s@%s  host_key_alias=%s\n", r.User, r.IP, r.HostKeyAlias)
	fmt.Fprintf(stdout, "key: %s (%s)", r.Key, key)
	if r.KeyErr != "" {
		fmt.Fprintf(stdout, "  %s", r.KeyErr)
	}
	fmt.Fprintf(stdout, "\nnet_known: %v", r.NetKnown)
	if r.KeyOK && !r.NetKnown {
		fmt.Fprint(stdout, "  (入れたが相手のネットワークを読めなかった)")
	}
	fmt.Fprintln(stdout)
	if r.NetKnown {
		if r.Service == "" {
			fmt.Fprintln(stdout, "有線 (ルーターの無いサービス): (無し)")
		} else {
			fmt.Fprintf(stdout, "有線 (ルーターの無いサービス): service=%s ip=%s method=%s mask=%s router=%s\n",
				r.Service, or(r.PeerIP, "-"), or(r.Method, "-"), or(r.Mask, "-"), or(r.Router, "-"))
		}
	}
	return 0
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
