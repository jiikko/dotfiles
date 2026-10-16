// Package netinfo は、この Mac のネットワークのサービスと I/F の IP を、networksetup と ifconfig の出力から読む。
//
// 読むだけで、設定は変えない。出力を読む部分は純関数 (Parse*) にしてあり、コマンドを起こす部分は Runner で差し替えられる。
package netinfo

import (
	"context"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

// ParseBand は内部の網の帯 ("192.168.100.0/24") を読む。IPv4 で /16〜/30 だけ (それより広い帯は番号の割り当てで回しきれず、
// 狭い帯は Mac が入らない)。帯に入るかの判定はすべて InBand を通す (文字列の前方一致と netip の 2 系統があり、"abc.def.ghi.0/24" が
// 前方一致の側だけを通った。監査の指摘)。
func ParseBand(s string) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(s)
	if err != nil || !p.Addr().Is4() || p.Bits() < 16 || p.Bits() > 30 {
		return netip.Prefix{}, fmt.Errorf("帯が使えない: %q (192.168.100.0/24 の形。/16〜/30)", s)
	}
	return p.Masked(), nil
}

// InBand は ip が帯に入るか。帯が読めない・ip が IP でなければ false。
func InBand(band, ip string) bool {
	p, err := ParseBand(band)
	if err != nil {
		return false
	}
	a, err := netip.ParseAddr(ip)
	return err == nil && p.Contains(a)
}

// Service は networksetup のネットワークのサービス 1 つ (Wi-Fi・Thunderbolt Ethernet など)。
type Service struct {
	Name   string // サービス名 ("Thunderbolt Ethernet")
	Device string // I/F の名前 ("en13")
	Active bool   // ifconfig の status が active
	IP     string // IPv4 (無ければ空)
	Method string // "DHCP" / "manual" / "" (読めなかった)
	Router string // IPv4 のルーター (無ければ空)
}

// Internet はインターネット用のサービスか (ルーターがある)。
func (s Service) Internet() bool { return s.Router != "" }

// Kind は I/F の IP の種類。
type Kind int

// IP の種類。「その他の IP」に含めるかを設定で切り替える単位。
const (
	KindUnknown   Kind = iota // 設定 (networksetup) に出てこない I/F
	KindWired                 // ルーターの無いサービス (有線)
	KindInternet              // ルーターのあるサービス (Wi-Fi など)
	KindLinkLocal             // 169.254 (固定していない)
	KindVirtual               // VM・Docker のブリッジ (bridge / vmnet / vnic)
	KindVPN                   // utun (VPN・Tailscale)
)

// String は画面に出す種類の名前。
func (k Kind) String() string {
	switch k {
	case KindWired:
		return "有線"
	case KindInternet:
		return "インターネット"
	case KindLinkLocal:
		return "自動割り当て"
	case KindVirtual:
		return "仮想"
	case KindVPN:
		return "VPN"
	case KindUnknown:
		return "不明"
	}
	return "不明"
}

// Key は設定ファイルに書く種類の名前 (画面の名前 String とは分ける: 画面の語を直すと、保存した設定が黙って外れた。監査の指摘)。
func (k Kind) Key() string {
	switch k {
	case KindWired:
		return "wired"
	case KindInternet:
		return "internet"
	case KindLinkLocal:
		return "link-local"
	case KindVirtual:
		return "virtual"
	case KindVPN:
		return "vpn"
	case KindUnknown:
		return "unknown"
	}
	return "unknown"
}

// Kinds は種類の全部 (設定の読み替えと検査に使う)。
var Kinds = []Kind{KindUnknown, KindWired, KindInternet, KindLinkLocal, KindVirtual, KindVPN}

// IsLinkLocal は ip が自動割り当て (169.254.0.0/16。固定していない) か。判定はここ 1 か所 (5 か所に写っていた。監査の指摘)。
func IsLinkLocal(ip string) bool {
	a, err := netip.ParseAddr(ip)
	return err == nil && a.Is4() && a.IsLinkLocalUnicast()
}

// IP は I/F の IPv4 1 つと、その種類。
type IP struct {
	Device string
	Addr   string
	Kind   Kind
}

// Snapshot はある時点のこの Mac のネットワーク。
type Snapshot struct {
	Services []Service
	IPs      []IP // ifconfig の I/F 全部の IPv4 (lo0 を除く)
}

var (
	// サービス名は番号の行 ("(1) 社内の有線" / 無効なら "(*) …") から取る。Hardware Port は I/F の種類の名前で、サービスの名前を
	// 変えると食い違う (Hardware Port を -getinfo に渡すと、知らないサービスとして rc=4 になった。監査の指摘)
	reService = regexp.MustCompile(`(?m)^\((?:\d+|\*)\) (.+)\n\(Hardware Port: .*, Device: (.*)\)`)
	reInet    = regexp.MustCompile(`(?m)^\s+inet (\S+)`)
	// 🚨 行頭に固定する: getinfo は "IPv6 Router: none" の行も出すので、固定しないと IPv6 の行に当たる。
	// 未接続のサービスは IPv4 の Router 行そのものを出さず、有線の自動割り当ては "Router: (null)" を出す (2026-10-15 の実測)
	reRouter = regexp.MustCompile(`(?m)^Router: (.*)$`)
	reIP     = regexp.MustCompile(`(?m)^IP address: (.*)$`)
)

// ParseServiceOrder は `networksetup -listnetworkserviceorder` の出力からサービス名と I/F の名前を並び順に読む。
func ParseServiceOrder(out string) []Service {
	ms := reService.FindAllStringSubmatch(out, -1)
	svcs := make([]Service, 0, len(ms))
	for _, m := range ms {
		svcs = append(svcs, Service{Name: m[1], Device: m[2]})
	}
	return svcs
}

// ParseGetInfo は `networksetup -getinfo <サービス名>` の出力から、方式・IPv4・ルーターを読む。
func ParseGetInfo(out string) (method, ip, router string) {
	switch {
	case strings.Contains(out, "Manual Configuration"):
		method = "manual"
	case strings.Contains(out, "DHCP Configuration"):
		method = "DHCP"
	}
	if m := reIP.FindStringSubmatch(out); m != nil && m[1] != "none" {
		ip = strings.TrimSpace(m[1])
	}
	if m := reRouter.FindStringSubmatch(out); m != nil {
		if r := strings.TrimSpace(m[1]); r != "(null)" && r != "none" {
			router = r
		}
	}
	return method, ip, router
}

// ParseIfconfig は `ifconfig <I/F>` の出力から、active か と IPv4 の一覧を読む。
func ParseIfconfig(out string) (active bool, addrs []string) {
	active = strings.Contains(out, "status: active")
	for _, m := range reInet.FindAllStringSubmatch(out, -1) {
		addrs = append(addrs, m[1])
	}
	return active, addrs
}

// Classify は I/F の名前と IP から種類を決める。wired はルーターの無いサービスの I/F、internet はルーターのあるサービスの I/F。
func Classify(dev, addr string, wired, internet map[string]bool) Kind {
	// サービスとして登録されている I/F を先に見る。🚨 名前で先に分けない: Thunderbolt Bridge は bridge0 という名前の本物の有線
	// (Mac 同士を Thunderbolt ケーブルで直に繋ぐ) で、名前だけ見ると VM のブリッジと取り違える
	switch {
	case internet[dev]:
		return KindInternet
	case wired[dev]:
		if IsLinkLocal(addr) {
			return KindLinkLocal
		}
		return KindWired
	case strings.HasPrefix(dev, "utun"):
		return KindVPN
	case strings.HasPrefix(dev, "bridge") || strings.HasPrefix(dev, "vmnet") || strings.HasPrefix(dev, "vnic"):
		return KindVirtual
	case IsLinkLocal(addr):
		return KindLinkLocal
	}
	return KindUnknown
}

// Runner はコマンドを起こして stdout を返す (テストで差し替える)。
type Runner func(ctx context.Context, name string, args ...string) (string, error)

// Read はこの Mac のネットワークを読む。サービスごとに getinfo と ifconfig を起こし、I/F 全部 (ifconfig -l) の IPv4 も集める。
// 1 つのサービスが読めなくても全体は止めない (読めた所だけ埋める)。サービスの一覧そのものが読めなければ err を返す。
func Read(ctx context.Context, run Runner) (Snapshot, error) {
	order, err := run(ctx, "networksetup", "-listnetworkserviceorder")
	if err != nil {
		return Snapshot{}, err
	}
	svcs := ParseServiceOrder(order)
	wired, internet := map[string]bool{}, map[string]bool{}
	for i := range svcs {
		s := &svcs[i]
		if out, err := run(ctx, "ifconfig", s.Device); err == nil {
			var addrs []string
			s.Active, addrs = ParseIfconfig(out)
			if len(addrs) > 0 {
				s.IP = addrs[0]
			}
		}
		out, err := run(ctx, "networksetup", "-getinfo", s.Name)
		if err != nil {
			continue // ルーターの有無が分からない: 有線とも インターネット側とも決めない (決めないと「ルーター無し = 有線」に倒れた)
		}
		var ip string
		s.Method, ip, s.Router = ParseGetInfo(out)
		if s.IP == "" {
			s.IP = ip
		}
		if s.Internet() {
			internet[s.Device] = true
		} else {
			wired[s.Device] = true
		}
	}
	snap := Snapshot{Services: svcs}
	list, err := run(ctx, "ifconfig", "-l")
	if err != nil {
		return snap, nil // I/F の一覧が読めなくても、サービスは返す
	}
	for _, dev := range strings.Fields(list) {
		if dev == "lo0" {
			continue
		}
		out, err := run(ctx, "ifconfig", dev)
		if err != nil {
			continue
		}
		_, addrs := ParseIfconfig(out)
		for _, a := range addrs {
			snap.IPs = append(snap.IPs, IP{Device: dev, Addr: a, Kind: Classify(dev, a, wired, internet)})
		}
	}
	return snap, nil
}

// Main はメインのネットワーク (インターネット側) のサービス。並び順で最初の、繋がっていてルーターのあるもの。
func (s Snapshot) Main() (Service, bool) {
	for _, sv := range s.Services {
		if sv.Active && sv.Internet() && sv.IP != "" {
			return sv, true
		}
	}
	return Service{}, false
}

// Offline は「ネットに繋がっていない」有線の候補 (繋がっていてルーターの無いサービス) を並び順に返す。
// 内部 IP を決めるのは設定 (帯 / I/F) で、これは候補を出すだけ (どれを使うかを推測しない)。
func (s Snapshot) Offline() []Service {
	var out []Service
	for _, sv := range s.Services {
		if sv.Active && !sv.Internet() {
			out = append(out, sv)
		}
	}
	return out
}
