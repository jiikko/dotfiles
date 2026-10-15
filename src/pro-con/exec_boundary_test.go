package main

import (
	"testing"

	"subproc/boundarycheck"
)

// 外部コマンドの起動口の規律: Cmd を作ったら WaitDelay を張る (字句走査。注記で免除) / os/exec を別名 import しない /
// Cmd を作るファイルを execCmdAllow に固定する。検査の本体・脅威モデル・「検出しない形」は subproc/boundarycheck が正本
// (glogx issue 105 / 303。pro-con に足したのは issue 777: subproc を通さず pipe を持つ起動が 14 本あった)。
//
// pro-con の起動は原則 subproc.CommandContext (ctx が無ければ subproc.Command) を通す。それぞれ WaitDelay を自分で選んでいる
// ファイルだけをここに載せる。🚨 母集合は「Cmd を作るファイル」で、*exec.Cmd の型や exec.ExitError を使うだけのファイルは載せない。
var execCmdAllow = map[string]string{
	"dispupgrade.go":         "dispatcher の preflight (自分のバイナリ)。WaitDelay は 1 秒を自分で張る",
	"upgrade/upgrade.go":     "ライブアップグレードの前の新しいバイナリの検査。同上",
	"dispatcher/claude.go":   "claude --version の検査。同上",
	"dispatcher/launcher.go": "PG / PM の claude の起動 (止め方が複雑なので自前)。同上",
	"dispatcher/triage.go":   "受付の git status。同上",
	"dispatcher/usage.go":    "claude /usage (timeout と WaitDelay の組を自分で決めている)",
	"dispatcher/progress.go": "進み具合の git diff。同上",
	"agents/agents.go":       "claude agents --json。同上",
}

// 下限は実件数のすぐ下に置く。実測 2026-10-16: seen=8、enforced=8 (全部 `cmd.WaitDelay = time.Second` を自分で張る形)。
// 🚨 enforced の箇所に注記を付けて逃がさない (理由は boundarycheck.Spec の EnforcedFloor の doc)。
func TestExecBoundary(t *testing.T) {
	boundarycheck.Run(t, boundarycheck.Spec{
		Root:     ".",
		SkipDirs: []string{"samples"}, // 過去の issue の動作確認サンプル (本体から参照しない)
		Allow:    execCmdAllow, SeenFloor: 7, EnforcedFloor: 7,
	})
}
