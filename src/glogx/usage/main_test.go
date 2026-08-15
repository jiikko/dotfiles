package usage

import (
	"os"
	"testing"

	"glogx/widthenv"
)

// TestMain は「glogx が支持しない幅 env」でテストを走らせない (issue 054)。
//
// このパッケージも描画幅を ansi.StringWidth で測る (render.go) ため、幅を主張するテストを
// 足した瞬間に main / issues と同じ「幅 1 前提の assert が落ちる生ログ」になる。今はまだ
// 幅に依存する assert が無く env 下でも green だが、幅を測るパッケージだけガードが無い状態を
// 残すと、次に足す人が混乱する側に落ちる (2026-08-15 の敵対的レビューの指摘)。
func TestMain(m *testing.M) {
	widthenv.ExitIfUnsupported()
	os.Exit(m.Run())
}
