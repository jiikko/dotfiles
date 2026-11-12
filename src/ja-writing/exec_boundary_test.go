package main

import (
	"testing"

	"subproc/boundarycheck"
)

// 外部コマンドの起動口の規律 (検査の本体・脅威モデルは subproc/boundarycheck が正本)。
// ja-writing の外部プロセスは oracle の Runner が subproc.CommandContext で起こす python3 だけで、Cmd を自分で作るファイルは無い
// (oracle は os/exec を LookPath と ExitError のために import するので NoOsExec にはしない)。
func TestExecBoundary(t *testing.T) {
	boundarycheck.Run(t, boundarycheck.Spec{Root: ".", Allow: map[string]string{}})
}
