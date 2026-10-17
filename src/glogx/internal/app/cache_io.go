package app

import (
	"atomicfile"
	"doctor/cachedir"
	"os"
	"path/filepath"
)

// CacheBaseDir は glogx のキャッシュ置き場 ($XDG_CACHE_HOME/glog、未設定時は ~/.cache/glog)。
// CI キャッシュと claude バージョンキャッシュ (claude_version.go) で共有する。
// 実体は doctor module の cachedir (doctor のスキャン結果も同じ置き場に保存するため。issue 148)。
func CacheBaseDir() (string, error) { return cachedir.Base() }

// WriteAtomic は atomicfile.Write の薄いラッパー (親ディレクトリを作り、0600 で書く)。
// temp の命名・掃除の契約と既知の射程外は atomicfile.Write の doc が正本。
func WriteAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.Write(path, data, 0o600)
}
