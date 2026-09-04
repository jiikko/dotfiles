package docker

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// errNotFound は PATH に見つからなかったとき。
var errNotFound = errors.New("PATH に見つかりません")

// lookPath は $PATH から実行可能ファイルを探す。
//
// 🚨 `os/exec.LookPath` を使わないのは、この module の depguard が `os/exec` を禁じているため
// (外部コマンドは runner 経由で起こす規律。探索だけのために例外を開けない)。
// 探索の意味論は LookPath と同じ: PATH を : で分け、実行ビットの立った通常ファイルを返す。
func lookPath(name string) (string, error) {
	if strings.ContainsRune(name, os.PathSeparator) {
		if executable(name) {
			return name, nil
		}
		return "", errNotFound
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		p := filepath.Join(dir, name)
		if executable(p) {
			return p, nil
		}
	}
	return "", errNotFound
}

func executable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0
}
