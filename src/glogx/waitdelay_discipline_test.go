package main

import (
	"testing"

	"subproc/boundarycheck"
)

// 外部コマンドの起動口の規律: Cmd を作ったら WaitDelay を張る (字句走査。注記で免除) / os/exec を別名 import しない /
// Cmd を作るファイルを execCmdAllow に固定する。検査の本体・脅威モデル・「検出しない形」は subproc/boundarycheck が
// 正本。glogx 固有なのは下の表と下限だけ。
//
// execCmdAllow は「*exec.Cmd を作ってよい」非テストファイルと、その理由。
//
// 🚨 母集合は「os/exec を import しているファイル」ではなく「**Cmd を作るファイル**」。
// 実測 2026-09-09: import しているのは 6 ファイルだが、cli_health.go と github.go は
// exec.ErrNotFound / exec.ExitError を使うだけで Cmd を一切作らない。
//
// 増やすときは、そのファイルが本当に外部プロセスの起動口であるべきかを考える
// (既存の入口 = internal/extcmd / internal/gitexec / subproc に寄せられないか)。
var execCmdAllow = map[string]string{
	"internal/filerview/filer_view.go": "treefiler の ! / s / e の前景実行 (filerExecCommand。issue 817 で画面ごと移した)",
	"internal/lanview/lan_view.go":     "mac-lan-manager の s / o の前景実行 (lanExecCommand。issue 817 で画面ごと移した)",
	"internal/extcmd/extcmd.go":        "ブラウザ / エディタ / クリップボードの起動口 (main と画面のパッケージが共有する。issue 817)",
	"internal/gitdata/gitlog.go":       "git log の取得の Cmd を組む (WaitDelay は gitexec.RunGitCmd が張る)",
	"internal/gitexec/git.go":          "git 実行の共通入口 (WaitDelay は RunGitCmd が張る)",
	"open_workspace.go":                "e / E の前景エディタ・ファイラー起動 (runEditorCmd = tea.Exec + foreground.Exec)",
	"tui.go":                           "job ログを nvim の scratch で開く前景実行",
}

// 下限は実件数のすぐ下に置く (0 や 1 だと「走査が壊れて対象を取りこぼした」が緑で通る)。
// 実測 2026-09-09: seen=9、enforced=2 (tui.go の job ログ nvim / gitlog.go の runGitTimeout。後者は internal/gitexec/git.go に移った)。
// 実測 2026-09-24: seen=7、enforced=2。extcmd.EditorCommand の exec.Command 2 か所を github.com/jiikko/dotfiles/src/tuikit/editor へ移した
// (glogx と pro-con でエディタの解決を 1 実装にするため) ので seen だけが 2 減った。
// 実測 2026-10-18: seen=9、enforced=2 (main と internal の画面への移設で数は変わらず)。下限を実件数のすぐ下へ上げた。
// 🚨 enforced の 2 件は**注記を付けてはいけない** (理由は boundarycheck.Spec の EnforcedFloor の doc)。
const (
	waitDelaySeenFloor     = 8
	waitDelayEnforcedFloor = 2
)

func TestEveryCommandContextSetsWaitDelay(t *testing.T) {
	boundarycheck.Run(t, boundarycheck.Spec{
		Root:     ".",
		SkipDirs: []string{"tools"}, // 本体から参照しない調査ツール
		Allow:    execCmdAllow, SeenFloor: waitDelaySeenFloor, EnforcedFloor: waitDelayEnforcedFloor,
	})
}
