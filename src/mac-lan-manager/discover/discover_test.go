package discover

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testdata は 2026-10-15 (21 時台) と 2026-10-16 (0 時台。*_0h.txt) にこの Mac で本物の dns-sd を起こして取った出力
// (bin/runtimeout で数秒で止めた)。dns-sd は時を 2 桁にそろえないので、0〜9 時台は行頭が空白になる。

func lines(t *testing.T, name string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(string(b), "\n")
}

func TestParseBrowse(t *testing.T) {
	var got []Browse
	for _, l := range lines(t, "browse_ssh.txt") {
		if b, ok := ParseBrowse(l); ok {
			got = append(got, b)
		}
	}
	want := []Browse{
		{true, 27, "_ssh._tcp.", "kojiのMac mini"},
		{true, 14, "_ssh._tcp.", "KOJI’s MacBook Pro"},
		{true, 14, "_ssh._tcp.", "kojiのMac meeini (2)"}, // 名前の衝突で " (2)" が付いたもの
	}
	if len(got) != len(want) {
		t.Fatalf("got %d lines: %+v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseBrowseSkipsHeaders(t *testing.T) {
	for _, l := range []string{
		"Browsing for _ssh._tcp.local",
		"DATE: ---Thu 15 Oct 2026---",
		"21:28:02.200  ...STARTING...",
		"Timestamp     A/R    Flags  if Domain               Service Type         Instance Name",
		"",
	} {
		if b, ok := ParseBrowse(l); ok {
			t.Errorf("ParseBrowse(%q) = %+v, want ok=false", l, b)
		}
	}
}

func TestParseLookup(t *testing.T) {
	var got []Resolved
	for _, l := range lines(t, "lookup.txt") {
		if r, ok := ParseLookup(l); ok {
			got = append(got, r)
		}
	}
	want := Resolved{Instance: "kojiのMac mini", Host: "m2macmini.local", Port: 22, If: 27} // \032 は空白に戻す
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v, want [%+v]", got, want)
	}
}

func TestParseAddr(t *testing.T) {
	var got []Addr
	for _, l := range lines(t, "getaddr.txt") {
		if a, ok := ParseAddr(l); ok {
			got = append(got, a)
		}
	}
	want := []Addr{
		{true, 14, "m2macmini.local", "192.168.50.7"},    // Wi-Fi 側
		{true, 27, "m2macmini.local", "169.254.217.100"}, // 有線側
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestUnescape(t *testing.T) {
	cases := map[string]string{
		`kojiのMac\032mini`: "kojiのMac mini",
		`a\.b`:             "a.b",
		`a\\b`:             `a\b`,
		`no escape`:        "no escape",
		`\999`:             `\999`, // 255 を超える番号は戻さない
	}
	for in, want := range cases {
		if got := unescape(in); got != want {
			t.Errorf("unescape(%q) = %q, want %q", in, got, want)
		}
	}
}

// 0〜9 時台の出力 (行頭の時刻が " 0:12:01.848" と空白で始まる) も読む。読めないと、その時間帯は Bonjour で 1 台も見つからない
// (ユーザー報告 2026-10-16 0 時台)。
func TestParseAtSingleDigitHour(t *testing.T) {
	var bs []Browse
	for _, l := range lines(t, "browse_ssh_0h.txt") {
		if b, ok := ParseBrowse(l); ok {
			bs = append(bs, b)
		}
	}
	if len(bs) != 3 || bs[0] != (Browse{true, 27, "_ssh._tcp.", "kojiのMac mini"}) {
		t.Errorf("-B: %+v", bs)
	}
	var rs []Resolved
	for _, l := range lines(t, "lookup_0h.txt") {
		if r, ok := ParseLookup(l); ok {
			rs = append(rs, r)
		}
	}
	if len(rs) != 1 || rs[0] != (Resolved{Instance: "kojiのMac mini", Host: "m2macmini.local", Port: 22, If: 27}) {
		t.Errorf("-L: %+v", rs)
	}
	var as []Addr
	for _, l := range lines(t, "getaddr_0h.txt") {
		if a, ok := ParseAddr(l); ok {
			as = append(as, a)
		}
	}
	if len(as) != 1 || as[0] != (Addr{true, 27, "m2macmini.local", "169.254.217.100"}) {
		t.Errorf("-G: %+v", as)
	}
}
