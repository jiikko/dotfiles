package main

import (
	"testing"

	"subproc/boundarycheck"
)

// 外部コマンドの起動口の規律: Cmd を作ったら WaitDelay を張る / os/exec を別名 import しない / Cmd を作るファイルを固定する。
// 検査の本体・脅威モデル・「検出しない形」は subproc/boundarycheck が正本 (glogx issue 105 / 303。ここに足したのは issue 777)。
// ssh / open / scutil は live.RunOut (subproc.CommandContext)、クリップボードは subproc/clipboard.Copy を通す。
func TestExecBoundary(t *testing.T) {
	boundarycheck.Run(t, boundarycheck.Spec{
		Root: ".",
		Allow: map[string]string{
			"discover/exec.go": "dns-sd を StdoutPipe で読み続ける Starter (Close で自分の pid だけを止める)。WaitDelay は 1 秒を自分で張る",
		},
		// 実測 2026-10-16: seen=1、enforced=1 (discover/exec.go だけ)。これ以上は減らせない (0 なら NoOsExec へ切り替える)
		SeenFloor: 1, EnforcedFloor: 1,
	})
}
