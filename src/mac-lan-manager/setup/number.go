package setup

import (
	"fmt"
	"net/netip"
)

// SuggestIP は帯 (band。"192.168.100.0/24") の中で、used (この Mac・台帳の内部 IP・今見えている IP) に無い最小の番号を返す。
// .1〜.9 は飛ばす (ルーターや手で振った機器が居がちな番号)。電源の切れた Mac は ping にも Bonjour にも出ないので、台帳の内部 IP も
// used に入れること (設計の反証レビュー)。
func SuggestIP(band string, used []string) (string, error) {
	p, err := netip.ParsePrefix(band)
	if err != nil || !p.Addr().Is4() || p.Bits() < 16 || p.Bits() > 30 {
		return "", fmt.Errorf("帯が使えない: %q", band)
	}
	p = p.Masked()
	taken := map[netip.Addr]bool{}
	for _, u := range used {
		if a, err := netip.ParseAddr(u); err == nil {
			taken[a] = true
		}
	}
	a := p.Addr()
	for range 9 { // .1〜.9 を飛ばす
		a = a.Next()
	}
	for a = a.Next(); p.Contains(a.Next()); a = a.Next() { // 最後 (ブロードキャスト) は使わない
		if !taken[a] {
			return a.String(), nil
		}
	}
	return "", fmt.Errorf("帯 %s に空きが無い", band)
}
