package term

import (
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/widthenv"
)

// TestMain は支持しない幅 env (RUNEWIDTH_EASTASIAN) では理由を 1 度だけ言って止める (issue 054)。main パッケージ
// (tui_helpers_test.go) の TestMain と同じガードで、issue 742 で枠のテストをここへ移したとき通らなくなっていた。
func TestMain(m *testing.M) {
	widthenv.ExitIfUnsupported()
	m.Run()
}
