package main

import (
	"testing"

	"subproc/boundarycheck"
)

// 外部コマンドの起動口の規律: Cmd を作ったら WaitDelay を張る (字句走査。注記で免除) / os/exec を別名 import しない /
// Cmd を作るファイルを Allow に固定する。検査の本体・脅威モデル・「検出しない形」は subproc/boundarycheck が正本
// (glogx issue 105 / 303。pro-con に足したのは issue 777: subproc を通さず pipe を持つ起動が 14 本あった)。
//
// pro-con の起動は全部 subproc.CommandContext (ctx が無ければ subproc.Command) を通す。Cmd を自分で作ってよいファイルは無い (Allow は空)。
// WaitDelay = 1 秒を自分で張っていた 8 ファイル (preflight・upgrade・claude --version・PG / PM の起動・受付の git status・/usage・git diff・
// claude agents) は issue 782 で寄せた (1 秒の理由はどこにも無く、どれも上限が 10 秒以上なので subproc の 2 秒で困らない)。
// os/exec は *exec.Cmd の型・ExitError・LookPath に要るので NoOsExec にはしない。Cmd を作る行が 0 なので canary の下限も 0
// (走査の空振りは boundarycheck が go list との突き合わせで止める)。
func TestExecBoundary(t *testing.T) {
	boundarycheck.Run(t, boundarycheck.Spec{
		Root:     ".",
		SkipDirs: []string{"samples"}, // 過去の issue の動作確認サンプル (本体から参照しない)
		Allow:    map[string]string{},
	})
}
