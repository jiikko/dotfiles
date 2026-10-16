package netinfo

import "testing"

// 帯に入るかの判定は 1 つ (InBand)。数字でない帯・範囲外の番地・IPv6・広すぎる / 狭すぎる帯は使えない
// (文字列の前方一致の側は "abc.def.ghi.0/24" や "999.1.1.0/24" を帯として通した。監査の指摘)。
func TestParseBandAndInBand(t *testing.T) {
	for band, ok := range map[string]bool{"192.168.100.0/24": true, "10.0.0.0/16": true, "192.168.100.0/30": true, "192.168.100.5/24": true,
		"abc.def.ghi.0/24": false, "999.1.1.0/24": false, "10.0.0.0/8": false, "192.168.100.0/31": false, "fd00::/64": false, "": false} {
		if _, err := ParseBand(band); (err == nil) != ok {
			t.Errorf("ParseBand(%q) err=%v, want ok=%v", band, err, ok)
		}
	}
	for _, c := range []struct {
		band, ip string
		want     bool
	}{
		{"192.168.100.0/24", "192.168.100.13", true},
		{"192.168.100.0/24", "192.168.1.13", false},
		{"192.168.100.0/24", "192.168.1000.13", false},
		{"192.168.100.5/24", "192.168.100.200", true},
		{"10.0.0.0/16", "10.0.200.1", true},
		{"abc.def.ghi.0/24", "abc.def.ghi.1", false},
		{"192.168.100.0/24", "", false},
	} {
		if got := InBand(c.band, c.ip); got != c.want {
			t.Errorf("InBand(%q, %q) = %v", c.band, c.ip, got)
		}
	}
}
