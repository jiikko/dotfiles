package main

import (
	"os"
	"testing"

	"github.com/jiikko/dotfiles/src/tuikit/widthenv"

	"treefiler/filer"
)

// TestMain は幅を数える env (RUNEWIDTH_EASTASIAN=1) では理由を出して止める。止めないと幅の検査が環境のせいで大量に落ち、
// 原因が環境だと分からない (tests/scripts/test_widthenv_guarded.sh が配線を見る)。
func TestMain(m *testing.M) {
	widthenv.ExitIfUnsupported()
	filer.LoadWait = -1 // フォルダの読み込みを同期で待つ (壁時計で同期 / 非同期が分かれると、同じキーの列で画面が変わる)
	os.Exit(m.Run())
}
