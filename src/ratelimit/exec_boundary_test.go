package main

import (
	"testing"

	"subproc/boundarycheck"
)

// この module は外部プロセスを subproc.CommandContext (WaitDelay 付き) でしか起動しない。
// 起動口を増やさないことを「非テストの .go が os/exec を import しない」で固定する (boundarycheck の厳しい設定。
// 検査の本体・脅威モデルはそちらが正本。issue 105 / 777)。
//
// 🚨 なぜ要るか: 検査は module ごとに走査の根 (Root) の下しか歩かない。usage を glogx から出したとき、
// この module は何にも見られなくなった。
func TestNoOsExecImport(t *testing.T) {
	boundarycheck.Run(t, boundarycheck.Spec{Root: ".", NoOsExec: true})
}
