// Package prefs は mac-lan-manager の設定 (一覧に出す IP・内部 IP の決め方) を読み書きする。
//
// 置き場所は台帳と同じ ~/.config/mac-lan-manager/ (settings.toml。XDG_CONFIG_HOME に従う)。ledger.EnvDir で差し替える。
package prefs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"atomicfile"
	"userdirs"

	"mac-lan-manager/ledger"
	"mac-lan-manager/netinfo"
)

// Prefs は設定。ゼロ値ではなく Default から始める。
type Prefs struct {
	ShowMain, ShowLan, ShowOthers bool
	WiredOnly                     bool     // 一覧に出す相手を、この Mac の有線の I/F に見えた相手だけにする (issue 747 の決定)
	LanByBand                     bool     // true: 帯で決める / false: I/F で決める
	Band                          string   // "192.168.100.0/24" (IPv4 の /16〜/28。netinfo.ParseBand)
	LanIF                         string   // I/F で決めるときの I/F の名前 ("en13")
	Kinds                         []string // 「その他の IP」に含める種類 (netinfo.Kind.Key())
}

// Default は最初の設定 (仮想と VPN は含めない)。
func Default() Prefs {
	return Prefs{ShowMain: true, ShowLan: true, LanByBand: true, Band: "169.254.100.0/24", Kinds: []string{netinfo.KindWired.Key(), netinfo.KindLinkLocal.Key(), netinfo.KindUnknown.Key()}}
}

// EnvDir は置き場所を差し替える環境変数。台帳と同じ 1 つの名前を使う (別の定数に写していたので、片方の改名で黙ってずれた。監査の指摘)。
const EnvDir = ledger.EnvDir

// DefaultPath は設定の置き場所 (台帳の隣。前の置き場 ~/.config/glogx/mac-lan-manager にあれば初回に移す。issue 777)。
func DefaultPath() (string, error) {
	if d := os.Getenv(EnvDir); d != "" {
		return filepath.Join(d, "settings.toml"), nil
	}
	return userdirs.ConfigFile("mac-lan-manager", "settings.toml", filepath.Join(".config", "glogx", "mac-lan-manager", "settings.toml"))
}

// writeGuard は書き込みの直前に通す関門 (本番は nil。テストは一時ディレクトリの外を拒否する)。
var writeGuard func(path string) error

// Load は設定を読む。無ければ Default。知らない鍵は読み飛ばす (設定は失っても困らないので台帳ほど厳しくしない)。
func Load(path string) (Prefs, error) {
	p := Default()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "show_main", "show_lan", "show_others", "lan_by_band", "wired_only":
			bv, err := strconv.ParseBool(v)
			if err != nil {
				continue
			}
			*map[string]*bool{"show_main": &p.ShowMain, "show_lan": &p.ShowLan, "show_others": &p.ShowOthers, "lan_by_band": &p.LanByBand, "wired_only": &p.WiredOnly}[k] = bv
		case "band", "lan_if":
			s, err := strconv.Unquote(v)
			if err != nil {
				continue
			}
			if k == "band" {
				if b, err := netinfo.ParseBand(s); err == nil {
					s = b.String() // 手で書いた "192.168.100.5/24" も判定と同じ形で表示する。読めない値はそのまま残す (画面が「使えない形」と出す)
				}
				p.Band = s
			} else {
				p.LanIF = s
			}
		case "kinds":
			s, err := strconv.Unquote(v)
			if err != nil {
				continue
			}
			p.Kinds = nil
			for _, x := range strings.Split(s, ",") {
				if x = kindKey(strings.TrimSpace(x)); x != "" && !slices.Contains(p.Kinds, x) { // 古い名前と新しい名前の重なりを 1 つに
					p.Kinds = append(p.Kinds, x)
				}
			}
		}
	}
	return p, nil
}

// kindKey は保存された種類の名前を Key に揃える。前の版は画面の名前 (「有線」) を書いていたので、それも読む。
// 知らない名前はそのまま残す (新しい版が足した種類を古い版が読んで Save すると、捨てた分が設定から消えた。opus の敵対レビュー)。
func kindKey(x string) string {
	for _, k := range netinfo.Kinds {
		if x == k.Key() || x == k.String() {
			return k.Key()
		}
	}
	return x
}

// Save は設定を書く (temp + rename)。
func Save(path string, p Prefs) error {
	if writeGuard != nil {
		if err := writeGuard(path); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	kinds := slices.Clone(p.Kinds)
	slices.Sort(kinds)
	s := fmt.Sprintf("# mac-lan-manager の設定 (中の , で変える)\nshow_main = %t\nshow_lan = %t\nshow_others = %t\nlan_by_band = %t\nwired_only = %t\nband = %s\nlan_if = %s\nkinds = %s\n",
		p.ShowMain, p.ShowLan, p.ShowOthers, p.LanByBand, p.WiredOnly, strconv.Quote(p.Band), strconv.Quote(p.LanIF), strconv.Quote(strings.Join(kinds, ",")))
	return atomicfile.Write(path, []byte(s), 0o644)
}
