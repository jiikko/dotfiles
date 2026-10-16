package netinfo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// testdata は 2026-10-15 にこの Mac で本物のコマンドを起こして取った出力 (MAC アドレスと IPv6 のリンクローカルの値だけ伏せた)。

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseServiceOrder(t *testing.T) {
	got := ParseServiceOrder(fixture(t, "serviceorder.txt"))
	want := []Service{
		{Name: "Thunderbolt Bridge", Port: "Thunderbolt Bridge", Device: "bridge0"},
		{Name: "USB 10/100/1G/2.5G LAN", Port: "USB 10/100/1G/2.5G LAN", Device: "en10"},
		{Name: "Thunderbolt Ethernet", Port: "Thunderbolt Ethernet", Device: "en13"},
		{Name: "Wi-Fi", Port: "Wi-Fi", Device: "en0"},
		{Name: "iPhone USB", Port: "iPhone USB", Device: "en11"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d services, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseGetInfo(t *testing.T) {
	cases := []struct {
		file               string
		method, ip, router string
	}{
		// 有線の自動割り当ては "Router: (null)"
		{"getinfo_tb_ethernet.txt", "DHCP", "169.254.13.42", ""},
		// 未接続のサービスは IPv4 の Router 行を出さない。"IPv6 Router: none" の行に当ててはいけない
		{"getinfo_tb_bridge.txt", "DHCP", "", ""},
		{"getinfo_wifi.txt", "DHCP", "192.168.50.250", "192.168.50.1"},
	}
	for _, c := range cases {
		m, ip, r := ParseGetInfo(fixture(t, c.file))
		if m != c.method || ip != c.ip || r != c.router {
			t.Errorf("%s: got (%q, %q, %q), want (%q, %q, %q)", c.file, m, ip, r, c.method, c.ip, c.router)
		}
	}
}

func TestParseIfconfig(t *testing.T) {
	cases := []struct {
		file   string
		active bool
		addrs  []string
	}{
		{"ifconfig_en13.txt", true, []string{"169.254.13.42"}},
		{"ifconfig_en18.txt", true, []string{"169.254.85.235"}}, // networksetup に出てこない I/F
		{"ifconfig_en0.txt", true, []string{"192.168.50.250"}},
		{"ifconfig_bridge0.txt", false, nil},
	}
	for _, c := range cases {
		active, addrs := ParseIfconfig(fixture(t, c.file))
		if active != c.active || strings.Join(addrs, ",") != strings.Join(c.addrs, ",") {
			t.Errorf("%s: got (%v, %v), want (%v, %v)", c.file, active, addrs, c.active, c.addrs)
		}
	}
}

func TestKind(t *testing.T) {
	snap := Snapshot{Services: []Service{
		{Name: "Wi-Fi", Device: "en0", Router: "192.168.50.1", Read: true},
		{Name: "Thunderbolt Ethernet", Device: "en13", Read: true},
		{Name: "Thunderbolt Bridge", Device: "bridge0", Read: true},
	}}
	cases := []struct {
		dev, addr string
		want      Kind
	}{
		{"en0", "192.168.50.250", KindInternet},
		{"en13", "192.168.100.11", KindWired},
		{"en13", "169.254.13.42", KindLinkLocal},
		{"en18", "169.254.85.235", KindLinkLocal}, // サービスに無いが 169.254
		{"en18", "10.0.0.5", KindUnknown},
		{"utun4", "100.101.5.7", KindVPN},
		{"bridge100", "192.168.64.1", KindVirtual},
		{"bridge0", "192.168.100.20", KindWired}, // Thunderbolt Bridge (サービスの bridge0) は本物の有線。名前で仮想に分けない
	}
	for _, c := range cases {
		if got := snap.Kind(c.dev, c.addr); got != c.want {
			t.Errorf("Kind(%q, %q) = %v, want %v", c.dev, c.addr, got, c.want)
		}
	}
}

// 1 つの I/F に複数のサービスが載っても、I/F ごとに 1 つに決める (ルーターのあるサービスが優先、読めないサービスがあれば分からない)。
// サービスごとに判定していたころは、未接続の「Wi-Fi 2」(ルーター無し) が en0 を有線と言い、Wi-Fi の IP が内部 IP の候補に出た。
// Wi-Fi が読めず Wi-Fi 2 だけ読めると、Wi-Fi の上の相手が内部 IP になった (監査の第 3 波の敵対レビュー)。
func TestKindWithServicesSharingIF(t *testing.T) {
	both := Snapshot{Services: []Service{
		{Name: "Wi-Fi 2", Device: "en0", Active: true, IP: "192.168.50.250", Read: true},
		{Name: "Wi-Fi", Device: "en0", Active: true, IP: "192.168.50.250", Router: "192.168.50.1", Read: true},
	}}
	if k := both.Kind("en0", "192.168.50.20"); k != KindInternet || both.WiredIF("en0") || len(both.Offline()) != 0 {
		t.Errorf("ルーターのあるサービスが載る en0: Kind=%v WiredIF=%v Offline=%+v", k, both.WiredIF("en0"), both.Offline())
	}
	unread := Snapshot{Services: []Service{
		{Name: "Wi-Fi", Device: "en0", Active: true},
		{Name: "Wi-Fi 2", Device: "en0", Active: true, Read: true},
	}}
	if k := unread.Kind("en0", "192.168.50.20"); k != KindUnknown || unread.WiredIF("en0") {
		t.Errorf("読めないサービスが載る en0: Kind=%v WiredIF=%v", k, unread.WiredIF("en0"))
	}
	slices.Reverse(unread.Services) // 読めたサービスが先に並んでも同じ
	if k := unread.Kind("en0", "192.168.50.20"); k != KindUnknown || unread.WiredIF("en0") {
		t.Errorf("読めないサービスが後に載る en0: Kind=%v WiredIF=%v", k, unread.WiredIF("en0"))
	}
	// 読めないサービスが先でも、ルーターのあるサービスが載る I/F はインターネット側 (最優先)
	unreadThenInternet := Snapshot{Services: []Service{
		{Name: "Wi-Fi 2", Device: "en0"},
		{Name: "Wi-Fi", Device: "en0", Router: "192.168.50.1", Read: true},
	}}
	if k := unreadThenInternet.Kind("en0", "192.168.50.20"); k != KindInternet {
		t.Errorf("読めないサービスの後にルーターのあるサービスが載る en0: Kind=%v", k)
	}
	// 無効にしたサービス ((*)) は分類に数えない: 無効にした古い設定のルーターで、本物の有線の内部 IP が消えた
	disabled := Snapshot{Services: []Service{
		{Name: "TB", Device: "en13", Active: true, IP: "192.168.100.5", Method: "manual", Read: true},
		{Name: "古い設定", Device: "en13", Router: "192.168.100.1", Read: true, Disabled: true},
	}}
	if !disabled.WiredIF("en13") || disabled.Kind("en13", "192.168.100.5") != KindWired {
		t.Errorf("無効のサービスのルーターで有線を外した: WiredIF=%v Kind=%v", disabled.WiredIF("en13"), disabled.Kind("en13", "192.168.100.5"))
	}
	twice := Snapshot{Services: []Service{
		{Name: "A", Device: "en13", Active: true, Read: true}, {Name: "B", Device: "en13", Active: true, Read: true},
	}}
	if off := twice.Offline(); len(off) != 1 {
		t.Errorf("同じ I/F を 2 回候補にした: %+v", off)
	}
}

// fakeRunner は fixture を返す偽物。知らないコマンドは失敗として返す (読めなかったサービスがあっても全体が止まらないことを見る)。
func fakeRunner(t *testing.T, files map[string]string) Runner {
	t.Helper()
	return func(_ context.Context, name string, args ...string) (string, error) {
		key := strings.TrimSpace(name + " " + strings.Join(args, " "))
		f, ok := files[key]
		if !ok {
			return "", errors.New("no fixture: " + key)
		}
		if strings.HasPrefix(f, "=") {
			return f[1:], nil
		}
		return fixture(t, f), nil
	}
}

func TestRead(t *testing.T) {
	run := fakeRunner(t, map[string]string{
		"networksetup -listnetworkserviceorder":      "serviceorder.txt",
		"networksetup -getinfo Thunderbolt Ethernet": "getinfo_tb_ethernet.txt",
		"networksetup -getinfo Thunderbolt Bridge":   "getinfo_tb_bridge.txt",
		"networksetup -getinfo Wi-Fi":                "getinfo_wifi.txt",
		"ifconfig en13":                              "ifconfig_en13.txt",
		"ifconfig en18":                              "ifconfig_en18.txt",
		"ifconfig en0":                               "ifconfig_en0.txt",
		"ifconfig bridge0":                           "ifconfig_bridge0.txt",
		"ifconfig -l":                                "=lo0 en0 bridge0 en13 en18",
	})
	snap, err := Read(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Services) != 5 {
		t.Fatalf("services = %d, want 5", len(snap.Services))
	}
	main, ok := snap.Main()
	if !ok || main.Device != "en0" || main.IP != "192.168.50.250" {
		t.Errorf("Main() = %+v, %v", main, ok)
	}
	off := snap.Offline()
	if len(off) != 1 || off[0].Device != "en13" {
		t.Errorf("Offline() = %+v, want only en13", off)
	}
	got := make([]string, 0, len(snap.IPs))
	for _, ip := range snap.IPs {
		got = append(got, ip.Device+"="+ip.Addr+"/"+ip.Kind.String())
	}
	want := "en0=192.168.50.250/インターネット en13=169.254.13.42/自動割り当て en18=169.254.85.235/自動割り当て"
	if strings.Join(got, " ") != want {
		t.Errorf("IPs = %v\nwant %v", strings.Join(got, " "), want)
	}
}

// サービスの名前を変えた Mac: 名前は番号の行から取り、-getinfo にもその名前を渡す (Hardware Port の名前を渡すと rc=4 で、
// ルーターが分からないまま Wi-Fi が「有線」に倒れた。監査の指摘)。-getinfo が読めないサービスは有線とも決めない。
// serviceorder_renamed.txt は作った値 (この Mac のサービスは名前を変えていない)。番号の行と Hardware Port の行の形は本物の出力に合わせた。
func TestReadUsesRenamedServiceNames(t *testing.T) {
	got := ParseServiceOrder(fixture(t, "serviceorder_renamed.txt"))
	want := []Service{{Name: "社内の有線", Port: "Thunderbolt Ethernet", Device: "en13"}, {Name: "Wi-Fi (自宅)", Port: "Wi-Fi", Device: "en0"},
		{Name: "Thunderbolt Bridge", Port: "Thunderbolt Bridge", Device: "bridge0", Disabled: true}, {Name: "Wi-Fi 2", Port: "Wi-Fi", Device: "en0"}}
	if !slices.Equal(got, want) {
		t.Fatalf("ParseServiceOrder = %+v\nwant %+v", got, want)
	}
	run := fakeRunner(t, map[string]string{
		"networksetup -listnetworkserviceorder": "serviceorder_renamed.txt",
		"networksetup -getinfo 社内の有線":           "getinfo_tb_ethernet.txt",
		"networksetup -getinfo Wi-Fi (自宅)":      "getinfo_wifi.txt",
		"ifconfig en13":                         "ifconfig_en13.txt",
		"ifconfig en0":                          "ifconfig_en0.txt",
		"ifconfig bridge0":                      "=bridge0: flags=8863<UP>\n\tinet 192.168.100.5 netmask 0xffffff00 broadcast 192.168.100.255\n\tstatus: active\n",
		"ifconfig -l":                           "=lo0 en0 bridge0 en13",
	})
	snap, err := Read(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := snap.Main(); !ok || m.Name != "Wi-Fi (自宅)" || m.Device != "en0" {
		t.Errorf("Main() = %+v, %v (名前を変えた Wi-Fi をインターネット側と読めない)", m, ok)
	}
	seen := false
	for _, ip := range snap.IPs {
		if ip.Device == "bridge0" {
			seen = true
			if ip.Kind != KindUnknown { // 有線とも、名前から「仮想」とも決めない (bridge0 は本物の有線のことがある)
				t.Errorf("-getinfo が読めない bridge0 を %v と決めた: %+v", ip.Kind, ip)
			}
		}
	}
	if !seen {
		t.Fatal("前提: bridge0 の IP が読めていない")
	}
	for _, s := range snap.Offline() { // ルーターの無い有線の候補にも入れない (内部 IP の「未固定」や I/F の候補に出た)
		if s.Device == "bridge0" {
			t.Errorf("-getinfo が読めない bridge0 をルーターの無い有線の候補にした: %+v", s)
		}
	}
}

func TestReadFailsWhenServiceOrderFails(t *testing.T) {
	if _, err := Read(context.Background(), fakeRunner(t, nil)); err == nil {
		t.Fatal("サービスの一覧が読めないのに err が nil")
	}
}

// 無効のサービスはサービスを 1 つずつ使う経路 (Main・Offline) にも出さない (無効の古い設定 (ルーターあり) がメインになり、本当の
// メインの IP が消えた。監査の第 3 波の敵対レビュー)。
func TestDisabledServiceIsNotUsed(t *testing.T) {
	snap := Snapshot{Services: []Service{
		{Name: "古い設定", Device: "en13", Active: true, IP: "10.9.9.9", Router: "10.9.9.1", Read: true, Disabled: true},
		{Name: "TB", Device: "en13", Active: true, IP: "192.168.100.5", Read: true},
		{Name: "Wi-Fi", Device: "en0", Active: true, IP: "192.168.50.250", Router: "192.168.50.1", Read: true},
	}}
	if m, ok := snap.Main(); !ok || m.Name != "Wi-Fi" {
		t.Errorf("Main() = %+v (無効のサービスをメインにした)", m)
	}
	if off := snap.Offline(); len(off) != 1 || off[0].Name != "TB" {
		t.Errorf("Offline() = %+v (無効のサービスを有線の候補にした)", off)
	}
}

// 無効のサービスには ifconfig も -getinfo も打たない (どの判定にも使わないので、引いても観測が紛らわしくなるだけ)。
func TestReadSkipsDisabledService(t *testing.T) {
	base := fakeRunner(t, map[string]string{
		"networksetup -listnetworkserviceorder": "serviceorder_renamed.txt",
		"networksetup -getinfo 社内の有線":           "getinfo_tb_ethernet.txt",
		"networksetup -getinfo Wi-Fi (自宅)":      "getinfo_wifi.txt",
		"ifconfig en13":                         "ifconfig_en13.txt",
		"ifconfig en0":                          "ifconfig_en0.txt",
		"ifconfig -l":                           "=lo0 en0 en13",
	})
	run := func(ctx context.Context, name string, args ...string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "Thunderbolt Bridge") || (name == "ifconfig" && len(args) == 1 && args[0] == "bridge0") {
			t.Errorf("無効のサービスに %s %v を打った", name, args)
		}
		return base(ctx, name, args...)
	}
	snap, err := Read(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range snap.Services {
		if s.Disabled && (s.Active || s.IP != "" || s.Read) {
			t.Errorf("無効のサービスに I/F の状態を入れた: %+v", s)
		}
	}
}

func TestParseMask(t *testing.T) {
	if got := ParseMask(fixture(t, "getinfo_wifi.txt")); got != "255.255.255.0" {
		t.Errorf("ParseMask(wifi) = %q", got)
	}
	if got := ParseMask("Manual Configuration\nSubnet mask: none\n"); got != "" {
		t.Errorf("ParseMask(none) = %q", got)
	}
}

// ネットに繋がっていない有線 (Offline) に無線を数えない。ルーターの無い Wi-Fi (サービス名を変えていても Hardware Port で分かる) を
// 固定 IP の対象にしかけた (ユーザーの指示 2026-10-16「ネットに繋がっていない有線ネットワークに限定」)。
func TestOfflineExcludesWireless(t *testing.T) {
	s := Snapshot{Services: []Service{
		{Name: "家の外の網", Port: "Wi-Fi", Device: "en0", Active: true, IP: "169.254.1.2", Method: "DHCP", Read: true},
		{Name: "Thunderbolt Ethernet", Port: "Thunderbolt Ethernet", Device: "en13", Active: true, IP: "169.254.13.42", Method: "DHCP", Read: true},
		{Name: "Bluetooth PAN", Port: "Bluetooth PAN", Device: "en7", Active: true, IP: "169.254.7.7", Method: "DHCP", Read: true},
	}}
	off := s.Offline()
	if len(off) != 1 || off[0].Device != "en13" {
		t.Errorf("Offline = %+v (有線の en13 だけ)", off)
	}
}

// 固定 IP のマスクは帯から決める。169.254 の中なら /16 (自動で 169.254 を付けた機器と同じ網に居る)、他は帯の長さ
// (/24 に決め打ちしていたので、/16 の帯で番号が別の /24 に散ると届かなかった)。
func TestMaskFor(t *testing.T) {
	for band, want := range map[string]string{
		"169.254.100.0/24": "255.255.0.0",
		"192.168.100.0/24": "255.255.255.0",
		"10.20.0.0/16":     "255.255.0.0",
		"172.16.5.0/28":    "255.255.255.240",
		"x":                "",
	} {
		if got := MaskFor(band); got != want {
			t.Errorf("MaskFor(%q) = %q, want %q", band, got, want)
		}
	}
	if !LinkLocalBand("169.254.100.0/24") || LinkLocalBand("192.168.100.0/24") {
		t.Error("LinkLocalBand の判定が違う")
	}
}

// 帯とメインの網の重なりは、IP 1 点ではなく IP とマスクの網で見る (Wi-Fi が 192.168.0.10/16 だと帯 192.168.100.0/24 を含むのに止まらなかった)。
func TestOverlaps(t *testing.T) {
	for _, c := range []struct {
		ip, mask string
		want     bool
	}{
		{"192.168.0.10", "255.255.0.0", true},
		{"192.168.50.250", "255.255.255.0", false},
		{"192.168.100.7", "255.255.255.0", true},
		{"192.168.0.10", "", false},
	} {
		if got := Overlaps("192.168.100.0/24", c.ip, c.mask); got != c.want {
			t.Errorf("Overlaps(%s/%s) = %v, want %v", c.ip, c.mask, got, c.want)
		}
	}
}
