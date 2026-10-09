package ui

import (
	"os"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/widthenv"
)

// TestMain は幅を数える env (RUNEWIDTH_EASTASIAN=1) では理由を出して止める。止めないと幅の検査が環境のせいで大量に落ち、
// 原因が環境だと分からない (issue 701 の 2。tests/scripts/test_widthenv_guarded.sh が配線を見る)。
func TestMain(m *testing.M) {
	widthenv.ExitIfUnsupported()
	os.Exit(m.Run())
}
