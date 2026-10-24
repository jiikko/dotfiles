package main

import (
	"testing"

	"subproc/boundarycheck"
)

// この module は外部プロセス (git) を subproc 経由でしか起動しない。起動口を増やさないことを
// 「非テストの .go が os/exec を import しない」で固定する (検査の本体・脅威モデルは boundarycheck が正本)。
func TestNoOsExecImport(t *testing.T) {
	boundarycheck.Run(t, boundarycheck.Spec{Root: ".", NoOsExec: true})
}
