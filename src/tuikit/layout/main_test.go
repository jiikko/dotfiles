package layout

import (
	"os"
	"testing"

	"tuikit/widthenv"
)

// 枠・影・区切り線を「グリフ数 = 表示幅」で組むので、幅モデルが支持しない env の下では
// 説明の無い失敗を並べる代わりに 1 本の理由付き失敗で止める (widthenv の doc)。
func TestMain(m *testing.M) {
	widthenv.ExitIfUnsupported()
	os.Exit(m.Run())
}
