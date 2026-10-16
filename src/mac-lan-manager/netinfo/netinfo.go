// Package netinfo は、この Mac のネットワークのサービスと I/F の IP を、networksetup と ifconfig の出力から読む。
//
// 読むだけで、設定は変えない。出力を読む部分は純関数 (Parse*) にしてあり、コマンドを起こす部分は Runner で差し替えられる。
package netinfo

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strings"
)

// ParseBand は内部の網の帯 ("192.168.100.0/24") を読む。IPv4 で /16〜/28 だけ (それより広い帯は番号の割り当てで回しきれず、
// /29 より狭い帯は setup.SuggestIP が飛ばす .1〜.9 で埋まって Mac が入らない)。帯に入るかの判定はすべて InBand を通す (文字列の前方一致と netip の 2 系統があり、"abc.def.ghi.0/24" が
// 前方一致の側だけを通った。監査の指摘)。
func ParseBand(s string) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(s)
	if err != nil || !p.Addr().Is4() || p.Bits() < 16 || p.Bits() > 28 {
		return netip.Prefix{}, fmt.Errorf("帯が使えない: %q (192.168.100.0/24 の形。/16〜/28)", s)
	}
	return p.Masked(), nil
}

// LinkLocalBand は帯がリンクローカル (169.254.0.0/16) の中か。
func LinkLocalBand(band string) bool {
	p, err := ParseBand(band)
	return err == nil && p.Addr().IsLinkLocalUnicast()
}

// MaskFor は帯で固定 IP にするときのサブネットマスク。リンクローカルの中なら /16 (自動で 169.254 を付けた機器と同じ網に居て、
// 固定していない Mac にも有線で届く。ユーザー決定 2026-10-16)、他は帯の長さ (/24 に決め打ちしていたので、/16 の帯で番号が別の /24 に
// 散ると届かなかった)。帯が読めなければ ""。
func MaskFor(band string) string {
	p, err := ParseBand(band)
	if err != nil {
		return ""
	}
	bits := p.Bits()
	if p.Addr().IsLinkLocalUnicast() {
		bits = 16
	}
	m := net.CIDRMask(bits, 32)
	return fmt.Sprintf("%d.%d.%d.%d", m[0], m[1], m[2], m[3])
}

// Overlaps は帯と、ip / mask の網が重なるか (どちらかが読めなければ false)。
func Overlaps(band, ip, mask string) bool {
	p, err := ParseBand(band)
	if err != nil {
		return false
	}
	a, err1 := netip.ParseAddr(ip)
	mk, err2 := netip.ParseAddr(mask)
	if err1 != nil || err2 != nil || !a.Is4() || !mk.Is4() {
		return false
	}
	m4 := mk.As4()
	bits, _ := net.IPv4Mask(m4[0], m4[1], m4[2], m4[3]).Size()
	q, err := a.Prefix(bits)
	return err == nil && p.Overlaps(q)
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
	Port   string // Hardware Port (I/F の種類の名前。"Wi-Fi" / "Thunderbolt Ethernet")
	Device string // I/F の名前 ("en13")
	Active bool   // ifconfig の status が active
	IP     string // IPv4 (無ければ空)
	Method string // "DHCP" / "manual" / "" (読めなかった)
	Router string // IPv4 のルーター (無ければ空)
	Mask   string // サブネットマスク (読めなければ空。手動の設定へ戻すときに要る)
	Read   bool   // -getinfo を読めた (読めなければルーターの有無は分からない)
	// Disabled は無効にしたサービス ("(*)")。I/F の分類に数えない (無効にした古い設定のルーターで、本物の有線の内部 IP が消えた。
	// 監査の第 3 波の敵対レビュー)
	Disabled bool
}

// NotWiredLAN は Mac 同士の有線の網に使わない I/F のサービスか (無線・Bluetooth・iPhone のテザリング)。Hardware Port で決める
// (サービス名は利用者が変えられる)。Thunderbolt Bridge は Mac 同士の有線なので含めない
func (s Service) NotWiredLAN() bool {
	switch s.Port {
	case "Wi-Fi", "AirPort", "Bluetooth PAN", "iPhone USB":
		return true
	}
	return false
}

// Internet はインターネット用のサービスか (ルーターがある)。
func (s Service) Internet() bool { return s.Router != "" }

// ifClass は I/F 1 つの分類。有線かどうかはサービスではなく I/F で決める (1 つの I/F に複数のサービスが載ると、サービスごとの判定と
// I/F ごとの判定が食い違った: 未接続の「Wi-Fi 2」が en0 を有線と言い、Wi-Fi の IP が内部 IP の候補に出た。監査の第 3 波の敵対レビュー)。
type ifClass int

const (
	ifNone     ifClass = iota // サービスとして登録されていない
	ifInternet                // ルーターのあるサービスが載っている (他のサービスより優先する)
	ifUnread                  // ルーターのあるサービスは無いが、-getinfo を読めないサービスが載っている (分からない)
	ifWired                   // 載っているサービスを全部読めて、どれにもルーターが無い
)

func (s Snapshot) class(dev string) ifClass {
	if dev == "" { // I/F を持たないサービス (VPN など) は I/F ではない。"" 同士の一致で有線と読み、VPN を固定 IP にしかけた (敵対レビュー)
		return ifNone
	}
	var registered, unread, wired bool
	for _, sv := range s.Services {
		if sv.Device != dev {
			continue
		}
		registered = true
		switch {
		case sv.Disabled: // 無効のサービスは判定に数えない (無効にした古い設定のルーターで、本物の有線の内部 IP が消えた)
		case sv.Internet():
			return ifInternet
		case !sv.Read:
			unread = true
		default:
			wired = true
		}
	}
	switch {
	case unread:
		return ifUnread
	case wired:
		return ifWired
	case registered: // 無効のサービスしか無い: 登録はあるので名前の判定 (bridge0 → 仮想) には落とさない
		return ifUnread
	}
	return ifNone
}

// Enabled は無効 ((*)) でないサービス (並び順のまま)。サービスを 1 つずつ使う経路 (Main・Offline・内部 IP の I/F・I/F の候補) は
// これを通す: 無効の古い設定 (ルーターあり) がメインになり、本当のメインの IP が消えた。分類 (class) は無効のサービスを自分で扱う。
func (s Snapshot) Enabled() []Service {
	return slices.DeleteFunc(slices.Clone(s.Services), func(sv Service) bool { return sv.Disabled })
}

// WiredIF は dev がルーターの無い有線の I/F か。内部 IP の候補・「未固定」・I/F で決める設定は全部これで決める。
func (s Snapshot) WiredIF(dev string) bool { return s.class(dev) == ifWired }

// Kind は dev で見えた addr の種類。サービスとして登録されているのに読めない I/F は「分からない」にする
// (名前で決めると、読めなかった Thunderbolt Bridge を「仮想」に、Wi-Fi の上の 169.254 を「自動割り当て」にした)。
func (s Snapshot) Kind(dev, addr string) Kind {
	switch s.class(dev) {
	case ifInternet:
		return KindInternet
	case ifUnread:
		return KindUnknown
	case ifWired:
		if IsLinkLocal(addr) {
			return KindLinkLocal
		}
		return KindWired
	case ifNone:
	}
	return classifyByName(dev, addr)
}

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

// IsLinkLocal は ip がリンクローカル (169.254.0.0/16) か。固定の帯も 169.254 の中に置ける (既定) ので、これだけで「固定していない」とは
// 決めない (固定かは方式か台帳で決める)。判定はここ 1 か所 (5 か所に写っていた。監査の指摘)。
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
	reService = regexp.MustCompile(`(?m)^\((\d+|\*)\) (.+)\n\(Hardware Port: (.*), Device: (.*)\)`)
	reInet    = regexp.MustCompile(`(?m)^\s+inet (\S+)`)
	// 🚨 行頭に固定する: getinfo は "IPv6 Router: none" の行も出すので、固定しないと IPv6 の行に当たる。
	// 未接続のサービスは IPv4 の Router 行そのものを出さず、有線の自動割り当ては "Router: (null)" を出す (2026-10-15 の実測)
	reRouter = regexp.MustCompile(`(?m)^Router: (.*)$`)
	reIP     = regexp.MustCompile(`(?m)^IP address: (.*)$`)
	reMask   = regexp.MustCompile(`(?m)^Subnet mask: (.*)$`)
)

// ParseServiceOrder は `networksetup -listnetworkserviceorder` の出力からサービス名と I/F の名前を並び順に読む。
func ParseServiceOrder(out string) []Service {
	ms := reService.FindAllStringSubmatch(out, -1)
	svcs := make([]Service, 0, len(ms))
	for _, m := range ms {
		svcs = append(svcs, Service{Name: m[2], Port: m[3], Device: m[4], Disabled: m[1] == "*"})
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

// ParseMask は `networksetup -getinfo` の出力のサブネットマスク (無い・"none" なら空)。
func ParseMask(out string) string {
	if m := reMask.FindStringSubmatch(out); m != nil {
		if v := strings.TrimSpace(m[1]); v != "none" && v != "(null)" {
			return v
		}
	}
	return ""
}

// ParseIfconfig は `ifconfig <I/F>` の出力から、active か と IPv4 の一覧を読む。
func ParseIfconfig(out string) (active bool, addrs []string) {
	active = strings.Contains(out, "status: active")
	for _, m := range reInet.FindAllStringSubmatch(out, -1) {
		addrs = append(addrs, m[1])
	}
	return active, addrs
}

// classifyByName は、サービスとして登録されていない I/F の種類を名前と番地で決める (登録のある I/F は Snapshot.Kind が先に決める。
// 🚨 名前で先に分けない: Thunderbolt Bridge は bridge0 という名前の本物の有線で、登録のある I/F をここへ落とすと VM のブリッジと取り違える)。
func classifyByName(dev, addr string) Kind {
	switch {
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
	for i := range svcs {
		s := &svcs[i]
		if s.Disabled {
			continue // 無効のサービスはどの判定にも使わない (ifconfig も -getinfo も打たない。dump にも I/F の IP を持って見せない)
		}
		if out, err := run(ctx, "ifconfig", s.Device); err == nil {
			var addrs []string
			s.Active, addrs = ParseIfconfig(out)
			if len(addrs) > 0 {
				s.IP = addrs[0]
			}
		}
		out, err := run(ctx, "networksetup", "-getinfo", s.Name)
		if err != nil {
			continue // ルーターの有無が分からない: Read を立てない (有線とも インターネット側とも決めない)
		}
		var ip string
		s.Method, ip, s.Router = ParseGetInfo(out)
		s.Read = true
		s.Mask = ParseMask(out)
		if s.IP == "" {
			s.IP = ip
		}
	}
	// 期限切れ・取り消しで途中から失敗したサービスは「読めない」に畳まれ、最後まで読めたように見える (相手に有線が無い・メインの網が
	// 無いと断定し、帯の重なりの検査を素通りした。敵対レビュー)。期限が来ていたら読めなかったことにする
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Services: svcs}
	list, err := run(ctx, "ifconfig", "-l")
	if err != nil {
		return snap, ctx.Err() // I/F の一覧が読めなくても、サービスは返す (期限切れは除く)
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
			snap.IPs = append(snap.IPs, IP{Device: dev, Addr: a, Kind: snap.Kind(dev, a)})
		}
	}
	return snap, ctx.Err()
}

// Main はメインのネットワーク (インターネット側) のサービス。並び順で最初の、繋がっていてルーターのあるもの。
func (s Snapshot) Main() (Service, bool) {
	for _, sv := range s.Enabled() {
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
	for _, sv := range s.Enabled() {
		// 無線は数えない: ルーターの無い Wi-Fi (どこにも繋がっていない・ルーターの無い網に繋いだ) を「ネットに繋がっていない有線」として
		// 固定 IP にしかけた (ユーザーの指示 2026-10-16「ネットに繋がっていない有線ネットワークに限定」)
		if sv.Active && !sv.NotWiredLAN() && s.WiredIF(sv.Device) && !slices.ContainsFunc(out, func(o Service) bool { return o.Device == sv.Device }) {
			out = append(out, sv)
		}
	}
	return out
}
