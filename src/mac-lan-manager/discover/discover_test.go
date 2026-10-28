package discover

import (
	"fmt"
	"mac-lan-manager/internal/testfile"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// testdata は 2026-10-15 (21 時台) と 2026-10-16 (0 時台。*_0h.txt) にこの Mac で本物の dns-sd を起こして取った出力
// (bin/runtimeout で数秒で止めた)。dns-sd は時を 2 桁にそろえないので、0〜9 時台は行頭が空白になる。

func lines(t *testing.T, name string) []string {
	t.Helper()
	return strings.Split(testfile.Read(t, name), "\n")
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
		`\999`:             `\999`,   // 255 を超える番号は戻さない
		`a\\032b`:          `a\032b`, // \\ の後ろの数字は番号ではない (左から 1 回で読む)
		`a\\\032b`:         `a\ b`,
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

// -G の否定応答 ("0.0.0.0  1  No Such Record") と lo0 の 127.0.0.1 は IP として読まない (2026-10-16 0 時台に本物の dns-sd から取った出力)。
// 読むと 0.0.0.0 が「その他の IP」になり、それしか無い相手への s が自分へ ssh した。
func TestParseAddrSkipsNegativeAndLoopback(t *testing.T) {
	var got []Addr
	for _, l := range lines(t, "getaddr_negative.txt") {
		if a, ok := ParseAddr(l); ok {
			got = append(got, a)
		}
	}
	want := []Addr{
		{true, 27, "kojiM3MacBook-Pro.local", "169.254.13.42"},
		{true, 33, "kojiM3MacBook-Pro.local", "169.254.85.235"},
		{true, 14, "kojiM3MacBook-Pro.local", "192.168.50.250"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %+v, want %+v", i, got[i], want[i])
		}
	}
	// 行末が TTL でない行 (否定応答の文が付く) は、IP が 0.0.0.0 でなくても読まない (この行は作った値。本物の否定応答は 0.0.0.0 だった。
	// 0.0.0.0 を落とす検査と、行末を TTL に縛る検査の両方を、それぞれ 1 本で守るため)
	if a, ok := ParseAddr(" 0:51:54.383  Add  40000003      15  x.local.               10.0.0.9                                      1   No Such Record"); ok {
		t.Errorf("否定応答の行を読んだ: %+v", a)
	}
	// 行末が TTL でも 0.0.0.0 は読まない (この行も作った値。fixture の 0.0.0.0 は全部否定応答の文付きで、正規表現が先に落とすため)
	if a, ok := ParseAddr(" 0:51:54.383  Add  40000003      15  x.local.               0.0.0.0                                      4500"); ok {
		t.Errorf("0.0.0.0 を読んだ: %+v", a)
	}
}

// 消えた相手の Rmv を -B と -G の両方で読む (browse_rmv.txt / getaddr_rmv.txt は 2026-10-16 10 時台に、この Mac で _mlmtest._tcp を
// dns-sd -R / -P で数秒だけ広告して取り消し、本物の dns-sd -B / -G で取った出力)。Rmv を読めないと、相手の再起動で IP を引き直す
// (Rmv → Add) が黙って死んだ (Rmv の fixture が無く、(Add|Rmv) を (Add) にしても全部緑だった)。
func TestParseRmv(t *testing.T) {
	var add, rmv int
	for _, l := range lines(t, "browse_rmv.txt") {
		if b, ok := ParseBrowse(l); ok {
			if b.Add {
				add++
			} else {
				rmv++
			}
			if b.Instance != "mlm-rmv-test" || b.Service != "_mlmtest._tcp." {
				t.Errorf("-B: %+v", b)
			}
		}
	}
	if add != 6 || rmv != 6 {
		t.Errorf("-B: Add %d / Rmv %d (6 / 6)", add, rmv)
	}
	add, rmv = 0, 0
	for _, l := range lines(t, "getaddr_rmv.txt") {
		if a, ok := ParseAddr(l); ok {
			if a.Add {
				add++
			} else {
				rmv++
			}
			if a.Host != "mlmrmvtest.local" || a.IP != "192.168.100.250" {
				t.Errorf("-G: %+v", a)
			}
		}
	}
	if add != 6 || rmv != 6 {
		t.Errorf("-G: Add %d / Rmv %d (6 / 6)", add, rmv)
	}
}

// どの時間帯の出力も同じに読む。fixture の行頭の時刻を 0〜23 時に書き換えて (dns-sd は時を空白で 2 桁に詰める)、元と同じ結果になるかを見る
// (fixture が 21 時台だけだったころ、0〜9 時台の行がどれも読めなかった。時間帯ごとに fixture を取り直さずに全部の時間帯を見る)。
func TestParseEveryHour(t *testing.T) {
	head := regexp.MustCompile(`^\s*\d{1,2}:`)
	parse := map[string]func(string) (any, bool){
		"browse":  func(l string) (any, bool) { return ParseBrowse(l) },
		"lookup":  func(l string) (any, bool) { return ParseLookup(l) },
		"getaddr": func(l string) (any, bool) { return ParseAddr(l) },
	}
	files, err := filepath.Glob(filepath.Join("testdata", "*.txt"))
	if err != nil || len(files) < 7 {
		t.Fatalf("fixture が見つからない: %v %v", files, err)
	}
	for _, f := range files {
		base := filepath.Base(f)
		var p func(string) (any, bool)
		for k, fn := range parse {
			if strings.HasPrefix(base, k) {
				p = fn
			}
		}
		if p == nil {
			t.Fatalf("%s: どの読み取りの fixture か分からない (名前を browse / lookup / getaddr で始める)", base)
		}
		seen := 0
		for _, l := range lines(t, base) {
			want, ok := p(l)
			if !ok {
				continue
			}
			seen++
			for h := range 24 {
				moved := head.ReplaceAllString(l, fmt.Sprintf("%2d:", h))
				if got, ok := p(moved); !ok || got != want {
					t.Errorf("%s の %d 時台: %q を %+v と読んだ (元は %+v)", base, h, moved, got, want)
				}
			}
		}
		if seen == 0 && !strings.Contains(base, "negative") {
			t.Errorf("%s: 読めた行が 0 (時刻の書き換えを確かめられない)", base)
		}
	}
}
