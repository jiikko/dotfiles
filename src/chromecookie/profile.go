package chromecookie

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Profile は Chrome のプロファイル 1 件。
type Profile struct {
	Dir   string // ディレクトリ名（Default / Profile 3 …）。ReadCookies に渡す値
	Name  string // 表示名（Local State の name）
	Email string // ログイン中 Google アカウント（対象サービスのログインとは限らない）
}

// ListProfiles は Local State からプロファイルを列挙する。
// 読み取れない場合は実在するディレクトリ（Default / Profile N）へフォールバックする。
//
// 🚨 Local State から読むのは info_cache のキーと表示名だけ。暗号鍵（os_crypt）には触らない。
func ListProfiles() []Profile {
	root, err := SupportDir()
	if err != nil {
		return []Profile{{Dir: "Default"}}
	}
	data, err := os.ReadFile(filepath.Join(root, "Local State"))
	if err != nil {
		return fallbackProfiles(root)
	}
	var ls struct {
		Profile struct {
			InfoCache map[string]struct {
				Name     string `json:"name"`
				UserName string `json:"user_name"`
				GaiaName string `json:"gaia_name"`
			} `json:"info_cache"`
			LastUsed string `json:"last_used"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(data, &ls); err != nil || len(ls.Profile.InfoCache) == 0 {
		return fallbackProfiles(root)
	}
	out := make([]Profile, 0, len(ls.Profile.InfoCache))
	for dir, info := range ls.Profile.InfoCache {
		email := info.UserName
		if email == "" {
			email = info.GaiaName
		}
		out = append(out, Profile{Dir: dir, Name: info.Name, Email: email})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	// 直近に使われたプロファイルを先頭へ寄せる（検出を速くする）。
	if lu := ls.Profile.LastUsed; lu != "" {
		for i := range out {
			if out[i].Dir == lu {
				p := out[i]
				out = append(out[:i], out[i+1:]...)
				out = append([]Profile{p}, out...)
				break
			}
		}
	}
	return out
}

// fallbackProfiles は Local State が読めないときに、実在するディレクトリを走査する。
func fallbackProfiles(root string) []Profile {
	entries, err := os.ReadDir(root)
	if err != nil {
		return []Profile{{Dir: "Default"}}
	}
	var out []Profile
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "Default" || strings.HasPrefix(name, "Profile ") {
			out = append(out, Profile{Dir: name})
		}
	}
	if len(out) == 0 {
		return []Profile{{Dir: "Default"}}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out
}
