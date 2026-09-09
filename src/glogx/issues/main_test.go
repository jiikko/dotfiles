package issues

import (
	"fmt"
	"os"
	"testing"

	"doctor/testtmp"

	"glogx/widthenv"
)

// TestMain は「glogx が支持しない幅 env」でテストを走らせない (issue 054)。
//
// RUNEWIDTH_EASTASIAN が真だと x/ansi は罫線 ─ や … を幅 2 として数えるため、markdown の
// 罫線・表の桁揃えが要求幅を超え、幅の不変条件テストが「行 25 が幅を超えた (w=38)」の形で
// 落ちる。これは期待値の焼き付けではなく、glogx がこの env での描画を支持していないという
// 設計判断の帰結なので、理由を 1 度だけ言って止める (main パッケージの TestMain と同じ扱い)。
func TestMain(m *testing.M) {
	widthenv.ExitIfUnsupported()
	// 後始末を下の RemoveAll だけに預けない理由は tui_helpers_test.go の TestMain と同じ (issue 305)。
	dir, swept, cleanup, err := testtmp.Setup("glogx-issues")
	if err != nil {
		panic(err)
	}
	if swept > 0 {
		fmt.Fprintf(os.Stderr, "[testtmp] 前回の残骸を %d 件回収した\n", swept)
	}
	// Go 1.26 の testing.T.TempDir は GOTMPDIR を優先する。repo 内の GOTMPDIR は
	// issues.RepoRoot の「repo 外」テストを glogx の root に戻してしまうため隔離する。
	if err := os.Setenv("GOTMPDIR", dir); err != nil {
		panic(err)
	}
	code := m.Run()
	cleanup()
	os.Exit(code)
}
