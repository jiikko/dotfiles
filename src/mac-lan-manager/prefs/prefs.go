// Package prefs は mac-lan-manager の設定 (一覧に出す IP・内部 IP の決め方) を読み書きする。
//
// 置き場所は台帳と同じ ~/.config/glogx/mac-lan-manager/ (settings.toml)。ledger.EnvDir で差し替える。
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
)

// Prefs は設定。ゼロ値ではなく Default から始める。
type Prefs struct {
	ShowMain, ShowLan, ShowOthers bool
	LanByBand                     bool     // true: 帯で決める / false: I/F で決める
	Band                          string   // "192.168.100.0/24" (/24 だけを扱う)
	LanIF                         string   // I/F で決めるときの I/F の名前 ("en13")
	Kinds                         []string // 「その他の IP」に含める種類の名前 (netinfo.Kind.String())
}

// Default は最初の設定 (仮想と VPN は含めない)。
func Default() Prefs {
	return Prefs{ShowMain: true, ShowLan: true, LanByBand: true, Band: "192.168.100.0/24", Kinds: []string{"有線", "自動割り当て", "不明"}}
}

// BandPrefix は帯の先頭 3 つ ("192.168.100.")。/24 でなければ "" (帯で決められない)。
func (p Prefs) BandPrefix() string {
	ip, ok := strings.CutSuffix(p.Band, ".0/24")
	if !ok || strings.Count(ip, ".") != 2 {
		return ""
	}
	return ip + "."
}

// EnvDir は置き場所を差し替える環境変数 (ledger と同じ)。
const EnvDir = "MAC_LAN_MANAGER_CONFIG_DIR"

// DefaultPath は設定の置き場所。
func DefaultPath() (string, error) {
	if d := os.Getenv(EnvDir); d != "" {
		return filepath.Join(d, "settings.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "glogx", "mac-lan-manager", "settings.toml"), nil
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
		case "show_main", "show_lan", "show_others", "lan_by_band":
			bv, err := strconv.ParseBool(v)
			if err != nil {
				continue
			}
			*map[string]*bool{"show_main": &p.ShowMain, "show_lan": &p.ShowLan, "show_others": &p.ShowOthers, "lan_by_band": &p.LanByBand}[k] = bv
		case "band", "lan_if":
			s, err := strconv.Unquote(v)
			if err != nil {
				continue
			}
			if k == "band" {
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
				if x = strings.TrimSpace(x); x != "" {
					p.Kinds = append(p.Kinds, x)
				}
			}
		}
	}
	return p, nil
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
	s := fmt.Sprintf("# mac-lan-manager の設定 (中の , で変える)\nshow_main = %t\nshow_lan = %t\nshow_others = %t\nlan_by_band = %t\nband = %s\nlan_if = %s\nkinds = %s\n",
		p.ShowMain, p.ShowLan, p.ShowOthers, p.LanByBand, strconv.Quote(p.Band), strconv.Quote(p.LanIF), strconv.Quote(strings.Join(kinds, ",")))
	return atomicfile.Write(path, []byte(s), 0o644)
}
