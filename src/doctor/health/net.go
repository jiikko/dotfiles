package health

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	pingWarnAvgMS = 50.0            // 家の中のゲートウェイまでの往復がこれを超えたら注意
	dnsWarn       = time.Second     // 名前解決がこれより遅ければ注意
	externalHost  = "1.1.1.1"       // 外への到達を見る相手 (DNS を通さない)
	resolveHost   = "www.apple.com" // 名前解決を見る相手
)

func (s *scanner) net(ctx context.Context) []Check {
	cs := []Check{}
	route := s.defaultRoute(ctx)
	cs = append(cs, route.check)
	if route.gateway != "" {
		cs = append(cs, s.ping(ctx, "net.gateway", "ゲートウェイまでの往復", route.gateway))
	}
	ext := s.ping(ctx, "net.external", "外部 ("+externalHost+") までの往復", externalHost)
	cs = append(cs, ext, s.dns(ctx), s.proxy(ctx), s.listen(ctx), s.wifi(ctx), s.netQuality(ctx))
	return gatewayIgnoresICMP(cs, ext)
}

// gatewayIgnoresICMP は、ゲートウェイに ping が届かないのに外部には届くとき、ゲートウェイの異常を情報に落とす
// (ICMP に応答しないルーターがある。外へ出られているので経路は生きている)。
func gatewayIgnoresICMP(cs []Check, ext Check) []Check {
	if ext.Status != StatusOK && ext.Status != StatusWarn {
		return cs
	}
	for i := range cs {
		if cs[i].ID == "net.gateway" && cs[i].Status == StatusBad {
			cs[i].Status, cs[i].Fix = StatusInfo, ""
			cs[i].Note = "ゲートウェイは ping に応答しないが、外部には届いている (ICMP に応答しないルーター)"
		}
	}
	return cs
}

type routeResult struct {
	check   Check
	gateway string
	iface   string
}

func (s *scanner) defaultRoute(ctx context.Context) routeResult {
	const id, label = "net.route", "既定の経路"
	out, stderr, rc, err := s.cmd(ctx, 5*time.Second, "/sbin/route", "-n", "get", "default")
	if err != nil {
		return routeResult{check: unknown(id, label, cmdNote("route", rc, err, stderr))}
	}
	if rc != 0 {
		// 経路が無いときも route は rc≠0 を返す。ネットワークにつながっていない状態と区別できないので異常として出す
		return routeResult{check: Check{ID: id, Label: label, Value: "無い", Status: StatusBad,
			Note: cmdNote("route", rc, nil, stderr), Fix: "Wi-Fi / 有線の接続を確かめる (システム設定 > ネットワーク)"}}
	}
	r := routeResult{}
	for line := range strings.SplitSeq(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "gateway":
			r.gateway = strings.TrimSpace(v)
		case "interface":
			r.iface = strings.TrimSpace(v)
		}
	}
	if r.iface == "" {
		r.check = unknown(id, label, "route の出力に interface の行が無い")
		return r
	}
	r.check = Check{ID: id, Label: label, Value: fmt.Sprintf("%s 経由 (gateway %s)", r.iface, cmp.Or(r.gateway, "-")), Status: StatusOK}
	return r
}

func (s *scanner) ping(ctx context.Context, id, label, host string) Check {
	out, stderr, rc, err := s.cmd(ctx, 10*time.Second, "/sbin/ping", "-n", "-q", "-c", "3", "-t", "5", host)
	if err != nil {
		return unknown(id, label, cmdNote("ping", rc, err, stderr))
	}
	loss, avg, ok := parsePing(out)
	if !ok {
		// ping は全部失われると rc=2 で統計の行だけ出す。統計の行すら無いのは名前が引けない等
		return unknown(id, label, cmdNote("ping", rc, nil, stderr))
	}
	c := Check{ID: id, Label: label, Status: StatusOK}
	switch {
	case loss >= 100:
		c.Value, c.Status = "届かない (100% 損失)", StatusBad
		c.Fix = "経路・ルーター・回線を確かめる。ゲートウェイには届くのに外部に届かないなら回線側"
		return c
	case loss > 0:
		c.Status = StatusWarn
		c.Note = "パケットが一部失われている (電波・混雑)"
	case avg > pingWarnAvgMS && id == "net.gateway":
		c.Status = StatusWarn
		c.Note = "家の中の機器までの往復が遅い (電波の弱さ・混雑)"
	}
	c.Value = fmt.Sprintf("平均 %.1fms / 損失 %.0f%%", avg, loss)
	return c
}

// parsePing は ping の統計の行 (損失 % と平均の往復) を読む。全部失われたときは平均の行が無いので avg=0。
func parsePing(out string) (loss, avg float64, ok bool) {
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "packet loss") {
			for f := range strings.SplitSeq(line, ",") {
				f = strings.TrimSpace(f)
				if v, found := strings.CutSuffix(f, "% packet loss"); found {
					n, err := strconv.ParseFloat(v, 64)
					if err != nil {
						return 0, 0, false
					}
					loss, ok = n, true
				}
			}
		}
		if _, rest, found := strings.Cut(line, "min/avg/max/stddev = "); found {
			parts := strings.Split(strings.TrimSuffix(rest, " ms"), "/")
			if len(parts) >= 2 {
				avg, _ = strconv.ParseFloat(parts[1], 64)
			}
		}
	}
	return loss, avg, ok
}

// dns は OS の名前解決 (mDNSResponder を通る経路) で 1 件引き、かかった時間を見る。
func (s *scanner) dns(ctx context.Context) Check {
	const id, label = "net.dns", "名前解決 (" + resolveHost + ")"
	start := s.o.Now()
	out, stderr, rc, err := s.cmd(ctx, 10*time.Second, "/usr/bin/dscacheutil", "-q", "host", "-a", "name", resolveHost)
	took := s.o.Now().Sub(start)
	if err != nil || rc != 0 {
		return unknown(id, label, cmdNote("dscacheutil", rc, err, stderr))
	}
	if !strings.Contains(out, "ip_address:") && !strings.Contains(out, "ipv6_address:") {
		return Check{ID: id, Label: label, Value: "引けない", Status: StatusBad,
			Note: "dscacheutil が住所を返さなかった", Fix: "scutil --dns で使っている DNS サーバを見る。VPN・プロキシの設定も疑う"}
	}
	c := Check{ID: id, Label: label, Value: fmt.Sprintf("%dms", took.Milliseconds()), Status: StatusOK}
	if took > dnsWarn {
		c.Status = StatusWarn
		c.Note = "名前解決が遅い (キャッシュに無い初回でも通常は数十 ms)"
		c.Fix = "scutil --dns で使っている DNS サーバを見る"
	}
	return c
}

func (s *scanner) proxy(ctx context.Context) Check {
	const id, label = "net.proxy", "プロキシ"
	out, stderr, rc, err := s.cmd(ctx, 5*time.Second, "/usr/sbin/scutil", "--proxy")
	if err != nil || rc != 0 {
		return unknown(id, label, cmdNote("scutil", rc, err, stderr))
	}
	var on []string
	for line := range strings.SplitSeq(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && strings.HasSuffix(strings.TrimSpace(k), "Enable") && strings.TrimSpace(v) == "1" {
			on = append(on, strings.TrimSuffix(strings.TrimSpace(k), "Enable"))
		}
	}
	if len(on) == 0 {
		return Check{ID: id, Label: label, Value: "使っていない", Status: StatusOK}
	}
	return Check{ID: id, Label: label, Value: strings.Join(on, ", "), Status: StatusInfo,
		Note: "意図して設定したものか確かめる (遅さ・つながらない原因になりうる)"}
}

// listen は全ての住所 (*) で待ち受けているポート。同じ LAN の他の機器から届く。
func (s *scanner) listen(ctx context.Context) Check {
	const id, label = "net.listen", "LAN に開いている待ち受け (* で listen)"
	out, stderr, rc, err := s.cmd(ctx, 20*time.Second, "/usr/sbin/lsof", "+c", "0", "-nP", "-iTCP", "-sTCP:LISTEN")
	if lsofFailed(rc, err, stderr) {
		return unknown(id, label, cmdNote("lsof", rc, err, stderr))
	}
	// 🚨 一般ユーザーの lsof は他のユーザー (root の launchd・システムのデーモン) のプロセスを返さない。画面共有 (5900)・
	// kerberos (88) などシステムが開く待ち受けは見えないので、見つからなくても ok にしない。全ユーザーを見る netstat は
	// Claude Code の sandbox の中では 0 行を返すので使わない
	const blind = "自分のユーザーのプロセスだけを見た。システムが開く待ち受け (画面共有・ファイル共有など) は見えないので、システム設定 > 一般 > 共有 で確かめる"
	open := parseWildcardListen(out)
	if len(open) == 0 {
		return Check{ID: id, Label: label, Value: "自分のプロセスには無い", Status: StatusInfo, Note: blind}
	}
	return Check{ID: id, Label: label, Value: strings.Join(open, " / "), Status: StatusWarn,
		Note: "同じネットワークの他の機器から接続できる。開発用のサーバなら 127.0.0.1 で待ち受ければ外から届かない。" + blind,
		Fix:  "要らないものは止める。要るものは 127.0.0.1 で待ち受けるか、ファイアウォールを有効にする"}
}

// lsofFailed は lsof の失敗。🚨 -w (警告を黙らせる) を付けない: 権限で読めなかったことは警告で出るので、-w を付けると
// 「rc=1・stderr 空・0 行」になり、見えなかったのを「無い」と読む (`lsof -w -Fn +d /private/var/root` で実測)。
// 🚨 lsof の rc は成否を表さない: 1 件も無いときも、見つけて出力したときも rc=1 を返すことがある
// (`lsof -Fn +d <dir>` が出力ありで rc=1・stderr 空を返した。2026-11-06 macOS 27.0.1)。stderr に何か出た rc≠0 だけを失敗とする。
func lsofFailed(rc int, err error, stderr string) bool {
	return err != nil || (rc != 0 && (rc != 1 || strings.TrimSpace(stderr) != ""))
}

// parseWildcardListen は lsof の出力から * / [::] / 0.0.0.0 で待ち受けている "プロセス名 ポート" を集める。
func parseWildcardListen(out string) []string {
	seen := map[string]bool{}
	var open []string
	for i, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if i == 0 || len(f) < 9 {
			continue
		}
		addr := f[8]
		port := addr[strings.LastIndex(addr, ":")+1:]
		host := strings.TrimSuffix(addr, ":"+port)
		if host != "*" && host != "[::]" && host != "0.0.0.0" {
			continue
		}
		k := f[0] + ":" + port
		if !seen[k] {
			seen[k] = true
			open = append(open, k)
		}
	}
	slices.Sort(open)
	return open
}

// wifi は今つないでいる Wi-Fi の信号とノイズ (場所で変わるので判定は弱め)。
func (s *scanner) wifi(ctx context.Context) Check {
	const id, label = "net.wifi", "Wi-Fi の信号 / ノイズ"
	out, stderr, rc, err := s.cmd(ctx, 30*time.Second, "/usr/sbin/system_profiler", "-json", "SPAirPortDataType")
	if err != nil || rc != 0 {
		return unknown(id, label, cmdNote("system_profiler", rc, err, stderr))
	}
	sig, noise, phy, ok, err := parseWifi(out)
	if errors.Is(err, errNoWifiInterface) {
		return Check{ID: id, Label: label, Value: "Wi-Fi の I/F が無い", Status: StatusInfo}
	}
	if err != nil {
		return unknown(id, label, "SPAirPortDataType の JSON が読めない: "+err.Error())
	}
	if !ok {
		return Check{ID: id, Label: label, Value: "つないでいない", Status: StatusInfo}
	}
	c := Check{ID: id, Label: label, Value: fmt.Sprintf("%d dBm / %d dBm (%s)", sig, noise, phy), Status: StatusOK}
	if snr := sig - noise; sig < -75 || snr < 20 {
		c.Status = StatusWarn
		c.Note = fmt.Sprintf("信号が弱い (SNR %d dB)。-67 dBm より強く、SNR 25 dB 以上が目安", snr)
		c.Fix = "ルーターに近づく・5GHz / 6GHz 帯に寄せる・中継器を置く"
	}
	return c
}

var errNoWifiInterface = errors.New("Wi-Fi の I/F が無い")

func parseWifi(out string) (sig, noise int, phy string, ok bool, err error) {
	var d struct {
		SP []struct {
			Ifaces []struct {
				Cur *struct {
					SignalNoise string `json:"spairport_signal_noise"`
					PHY         string `json:"spairport_network_phymode"`
				} `json:"spairport_current_network_information"`
			} `json:"spairport_airport_interfaces"`
		} `json:"SPAirPortDataType"`
	}
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		return 0, 0, "", false, err
	}
	// 節そのものが無いのは鍵の名前が変わった形なので、つないでいない (ok=false) と区別して読めないとする
	if len(d.SP) == 0 {
		return 0, 0, "", false, errors.New("SPAirPortDataType の節が無い")
	}
	ifaces := 0
	for _, sp := range d.SP {
		ifaces += len(sp.Ifaces)
	}
	if ifaces == 0 {
		return 0, 0, "", false, errNoWifiInterface
	}
	for _, sp := range d.SP {
		for _, itf := range sp.Ifaces {
			if itf.Cur == nil || itf.Cur.SignalNoise == "" {
				continue
			}
			a, b, found := strings.Cut(itf.Cur.SignalNoise, "/")
			if !found {
				continue
			}
			s1, e1 := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(a), "dBm")))
			s2, e2 := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(b), "dBm")))
			if e1 != nil || e2 != nil {
				return 0, 0, "", false, fmt.Errorf("signal / noise が読めない: %q", itf.Cur.SignalNoise)
			}
			return s1, s2, itf.Cur.PHY, true, nil
		}
	}
	return 0, 0, "", false, nil
}

// netQuality は Apple の回線品質の測定 (重い。-explicit のときだけ)。
func (s *scanner) netQuality(ctx context.Context) Check {
	const id, label = "net.quality", "回線の品質 (networkQuality)"
	if !s.o.Explicit {
		return Check{ID: id, Label: label, Value: "未検査", Status: StatusInfo,
			Note: "約 20 秒かかり数百 MB 通信するので既定では回さない。-explicit で回す"}
	}
	out, stderr, rc, err := s.cmd(ctx, 90*time.Second, "/usr/bin/networkQuality", "-c")
	if err != nil || rc != 0 {
		return unknown(id, label, cmdNote("networkQuality", rc, err, stderr))
	}
	var d struct {
		DL  float64 `json:"dl_throughput"`
		UL  float64 `json:"ul_throughput"`
		RPM float64 `json:"responsiveness"`
	}
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		return unknown(id, label, "networkQuality の JSON が読めない: "+err.Error())
	}
	c := Check{ID: id, Label: label, Status: StatusInfo,
		Value: fmt.Sprintf("下り %.0f Mbps / 上り %.0f Mbps / 応答 %.0f RPM", d.DL/1e6, d.UL/1e6, d.RPM)}
	if d.RPM > 0 && d.RPM < 200 {
		c.Status, c.Note = StatusWarn, "応答 (RPM) が Low の帯。負荷がかかると遅延が膨らむ (bufferbloat)"
	}
	return c
}
