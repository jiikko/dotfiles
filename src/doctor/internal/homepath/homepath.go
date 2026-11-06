// Package homepath はホームの下のパスを ~ で始まる形にする (レポート・表示にユーザー名を出さない)。health と uninstall が共有する。
package homepath

import "strings"

// Tilde は home の下のパスを "~/..." にする。home が空・home の外なら p をそのまま返す。
func Tilde(home, p string) string {
	if home == "" {
		return p
	}
	if rest, ok := strings.CutPrefix(p, home+"/"); ok {
		return "~/" + rest
	}
	return p
}
