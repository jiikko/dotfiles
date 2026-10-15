package main

import (
	"testing"

	"subproc/boundarycheck"
)

// この module (treefiler) は外部プロセスを subproc.CommandContext (WaitDelay 付き) でしか起動しない。
// 起動口を増やさないことを「非テストの .go が os/exec を import しない」で固定する (boundarycheck の厳しい設定。
// 検査の本体・脅威モデル・走査の数の go list との突き合わせ (issue 695 の 2) はそちらが正本。issue 105 / 777)。
func TestNoOsExecImport(t *testing.T) {
	boundarycheck.Run(t, boundarycheck.Spec{Root: ".", NoOsExec: true})
}
