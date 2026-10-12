// Package clipboard は文字列を OS のクリップボードへ書く (glogx と treefiler の両方が使う。issue 718)。
//
// OS のクリップボードコマンド (pbcopy / xclip) を真実とし、tmux 内では tmux バッファへも積む (tmux paste 用のおまけ・best effort)。
// 本家 glog は load-buffer -w の成功 (exit 0) を「システム側にも届いた」とみなすが、-w の実体は OSC52 転送で、外側端末が OSC52 を
// 解釈しなければ exit 0 のままクリップボードに入らない (glogx で実測 2026-07-19)。
//
// 呼び出し側は UI の Update から同期で呼ぶので Timeout で区切る: xclip は X サーバ無応答で戻らないことがある既知挙動で、上限が無いと
// TUI ごと固まったうえ強制終了後に子プロセスが残る (glogx issue 029 P3 で xdg-open に入れたのと同じ手当て)。
package clipboard

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"subproc"
)

// Timeout は 1 つのコマンド (本命・おまけそれぞれ) の実行上限。ハングしてもコピーが 1 回失敗するだけで済む操作なので短く切る。
const Timeout = 5 * time.Second

// Copy は text をクリップボードへ書く。返すのは本命 (OS のクリップボードコマンド) の失敗だけ。
func Copy(text string) error {
	if os.Getenv("TMUX") != "" {
		// おまけ側は timeout を本命と分ける (tmux のハングが OS クリップボードを巻き添えにしない)
		tmuxCtx, tmuxCancel := context.WithTimeout(context.Background(), Timeout)
		// WaitDelay (subproc.CommandContext が張る) が無いと ctx の kill 後も stdin の copy goroutine
		// の write ブロックを Wait が待ち続け、timeout が効かない (tmux は stdin fd をサーバへ渡すため、
		// payload がパイプバッファを超えるとハング中のサーバが read 側を握ったままになる)
		cmd := subproc.CommandContext(tmuxCtx, "tmux", "load-buffer", "-w", "-")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run() // 失敗しても OS クリップボードが本命なので無視
		tmuxCancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = subproc.CommandContext(ctx, "pbcopy")
	default:
		cmd = subproc.CommandContext(ctx, "xclip", "-selection", "clipboard")
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
