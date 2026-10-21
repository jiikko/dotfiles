package tmpclean

import (
	"testing"

	"subproc/boundarycheck"
)

// 外部コマンドの起動口の規律 (Cmd を作ったら WaitDelay を張る / Cmd を作るファイルを Allow に固定)。検査の本体・脅威モデルは
// subproc/boundarycheck が正本。tmpclean の起動は internal/runner の subproc.CommandContext だけで、Cmd を自分で作るファイルは無い
// (runner は os/exec を ExitError のために import するので NoOsExec にはしない)。
func TestExecBoundary(t *testing.T) {
	boundarycheck.Run(t, boundarycheck.Spec{Root: ".", Allow: map[string]string{}})
}
