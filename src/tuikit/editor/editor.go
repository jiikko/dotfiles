// Package editor は実ファイルを 1 つエディタで開くコマンドを組む。$VISUAL → $EDITOR → nvim の順に見る
// (VISUAL を先に見るのは「全画面エディタは VISUAL」という POSIX の慣習)。
//
// 契約は glogx の src/glogx/external_commands.go の editorCommand と同じ (glogx はまだこの package に寄せていない):
//   - 値は空白で語分割する (EDITOR="code -w" のような引数つきの指定のため)。quote は解釈しない
//     (シェルの解釈をエディタ起動に持ち込まない)。空白を含むパスの指定は非対応で、起動に失敗する
//   - 呼び出し側は tea.ExecProcess で TUI を中断して待つ。**起動したプロセスの終了 = 編集の完了**が前提なので、
//     GUI エディタは -w / --wait 付きで指定する必要がある (git の GIT_EDITOR と同じ要求)
package editor

import (
	"os"
	"os/exec"
	"strings"
)

// Fallback は $VISUAL / $EDITOR がどちらも空のときのエディタ。
const Fallback = "nvim"

// Command は path を開くコマンド。env は環境変数の読み出し (テストで差し替える。nil なら os.Getenv)。
func Command(path string, env func(string) string) *exec.Cmd {
	if env == nil {
		env = os.Getenv
	}
	var fields []string
	for _, name := range []string{"VISUAL", "EDITOR"} {
		if f := strings.Fields(env(name)); len(f) > 0 {
			fields = f
			break
		}
	}
	if len(fields) == 0 {
		return exec.Command(Fallback, path)
	}
	args := append(append([]string{}, fields[1:]...), path)
	return exec.Command(fields[0], args...)
}
