package markdown

import (
	"os"
	"testing"

	"tuikit/widthenv"
)

// TestMain は「支持しない幅 env」でテストを走らせない (glogx issue 054)。
//
// RUNEWIDTH_EASTASIAN が真だと x/ansi は罫線 ─ や … を幅 2 として数えるため、罫線・表の桁揃えが
// 要求幅を超え、幅の不変条件テストが「行 25 が幅を超えた (w=38)」の形で落ちる。期待値の焼き付けでは
// なく、この env での描画を支持していないという設計判断の帰結なので、理由を 1 度だけ言って止める。
func TestMain(m *testing.M) {
	widthenv.ExitIfUnsupported()
	os.Exit(m.Run())
}
