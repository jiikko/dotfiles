package toast

import (
	"os"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/widthenv"
)

// 箱の罫線と幅の上限を「グリフ数 = 表示幅」で組むので、幅モデルが支持しない env の下では
// 説明の無い失敗 (「行の幅 77 が窓の幅 40 を超える」等) を並べる代わりに 1 本の理由付き失敗で止める (widthenv の doc)。
func TestMain(m *testing.M) {
	widthenv.ExitIfUnsupported()
	os.Exit(m.Run())
}
