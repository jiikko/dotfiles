// Package cachedir は glogx 系ツールのキャッシュ置き場 ($XDG_CACHE_HOME/glog、未設定時は ~/.cache/glog)。
// glogx 本体 (CI キャッシュ / claude バージョン / issues 状態) と doctor (スキャン結果) で共有する。
// ⚠️ ディレクトリ名は `glogx` ではなく `glog` (既存キャッシュとの互換。issue 148 の codex 反証)。
package cachedir

import (
	"os"
	"path/filepath"
)

// Base はキャッシュのベースディレクトリ (作成はしない)。
func Base() (string, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "glog"), nil
}
