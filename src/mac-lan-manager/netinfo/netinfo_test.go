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
		{Name: "Thunderbolt Bridge", Device: "bridge0"},
		{Name: "USB 10/100/1G/2.5G LAN", Device: "en10"},
		{Name: "Thunderbolt Ethernet", Device: "en13"},
		{Name: "Wi-Fi", Device: "en0"},
		{Name: "iPhone USB", Device: "en11"},
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
		internet           bool
	}{
		// 有線の自動割り当ては "Router: (null)"
		{"getinfo_tb_ethernet.txt", "DHCP", "169.254.13.42", "", false},
		// 未接続のサービスは IPv4 の Router 行を出さない。"IPv6 Router: none" の行に当ててはいけない
		{"getinfo_tb_bridge.txt", "DHCP", "", "", false},
		{"getinfo_wifi.txt", "DHCP", "192.168.50.250", "192.168.50.1", true},
	}
	for _, c := range cases {
		m, ip, r := ParseGetInfo(fixture(t, c.file))
		if m != c.method || ip != c.ip || r != c.router {
			t.Errorf("%s: got (%q, %q, %q), want (%q, %q, %q)", c.file, m, ip, r, c.method, c.ip, c.router)
		}
		if (Service{Router: r}).Internet() != c.internet {
			t.Errorf("%s: Internet() = %v, want %v", c.file, !c.internet, c.internet)
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

func TestClassify(t *testing.T) {
	wired := map[string]bool{"en13": true, "bridge0": true}
	internet := map[string]bool{"en0": true}
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
		if got := Classify(c.dev, c.addr, wired, internet); got != c.want {
			t.Errorf("Classify(%q, %q) = %v, want %v", c.dev, c.addr, got, c.want)
		}
	}
}

// fakeRunner は fixture を返す偽物。知らないコマンドは失敗として返す (読めなかったサービスがあっても全体が止まらないことを見る)。
func fakeRunner(t *testing.T, files map[string]string) Runner {
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
	want := []Service{{Name: "社内の有線", Device: "en13"}, {Name: "Wi-Fi (自宅)", Device: "en0"}, {Name: "Thunderbolt Bridge", Device: "bridge0"}, {Name: "Wi-Fi 2", Device: "en0"}}
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
			if ip.Kind == KindWired {
				t.Errorf("-getinfo が読めない bridge0 を有線と決めた: %+v", ip)
			}
		}
	}
	if !seen {
		t.Fatal("前提: bridge0 の IP が読めていない")
	}
}

func TestReadFailsWhenServiceOrderFails(t *testing.T) {
	if _, err := Read(context.Background(), fakeRunner(t, nil)); err == nil {
		t.Fatal("サービスの一覧が読めないのに err が nil")
	}
}
