package setup

import (
	"fmt"
	"net/netip"

	"mac-lan-manager/netinfo"
)

// PickIP は prevs (その Mac に前に振った IP。先に書いたものほど優先) のうち、帯の中で used に無い最初のものを、無ければ
// 帯 (band。"192.168.100.0/24") の中で used (この Mac・台帳の内部 IP・今見えている IP) に無い最小の番号を返す。後者は .1〜.9 を
// 飛ばす (ルーターや手で振った機器が居がちな番号)。電源の切れた Mac は ping にも Bonjour にも出ないので、台帳の内部 IP も used に入れること。reused は prevs のどれかを使ったか。LAN アダプタを差し替えると新しい I/F のサービスは固定の設定を持たずに
// 始まるので、固定し直すたびに空いている最小の番号を振ると IP が変わり、IP を直に書いた接続先が黙って古い IP を指す
// (初期設定を流し直しても同じ IP に戻るようにする)。prev は .1〜.9 でも使う (前に手で振った IP を変えない)。帯の頭 (網の番号) と
// 最後 (ブロードキャスト) は使わない。
func PickIP(band string, used []string, prevs ...string) (ip string, reused bool, err error) {
	p, err := netinfo.ParseBand(band)
	if err != nil {
		return "", false, err
	}
	taken := map[netip.Addr]bool{}
	for _, u := range used {
		if a, err := netip.ParseAddr(u); err == nil {
			taken[a.Unmap()] = true
		}
	}
	for _, prev := range prevs {
		if a, err := netip.ParseAddr(prev); err == nil && a.Is4() && p.Contains(a) && a != p.Addr() && p.Contains(a.Next()) && !taken[a] {
			return a.String(), true, nil
		}
	}
	a := p.Addr()
	for range 9 { // .1〜.9 を飛ばす
		a = a.Next()
	}
	for a = a.Next(); p.Contains(a.Next()); a = a.Next() { // 最後 (ブロードキャスト) は使わない
		if !taken[a] {
			return a.String(), false, nil
		}
	}
	return "", false, fmt.Errorf("帯 %s に空きが無い", band)
}
