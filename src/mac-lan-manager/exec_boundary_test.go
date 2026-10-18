package main

import (
	"testing"

	"subproc/boundarycheck"
)

// 外部コマンドの起動口の規律: Cmd を作ったら WaitDelay を張る / os/exec を別名 import しない / Cmd を作るファイルを固定する。
// 検査の本体・脅威モデル・「検出しない形」は subproc/boundarycheck が正本。
// ssh / open / scutil は live.RunOut (subproc.CommandContext)、クリップボードは subproc/clipboard.Copy を通す。
func TestExecBoundary(t *testing.T) {
	boundarycheck.Run(t, boundarycheck.Spec{
		Root: ".",
		// Cmd を自分で作ってよいファイルは無い。dns-sd の Starter (discover/exec.go) も subproc.CommandContext を通す
		// (WaitDelay は subproc の 2 秒。1 秒を選んだ理由は無かった)。os/exec は *exec.Cmd の型に要るので NoOsExec にはしない
		Allow: map[string]string{},
	})
}
