package app

import "os"

// currentDir は探索の起点にする cwd (取れなければカレント相対)。glogx は tmux popup から
// -d '#{pane_current_path}' で起動されるので、cwd は repo のサブディレクトリになりうる。
func CurrentDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "." // cwd が取れない環境でも repo 直下として探索を試みる
	}
	return cwd
}
