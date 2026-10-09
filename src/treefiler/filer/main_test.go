package filer

import (
	"fmt"
	"github.com/jiikko/dotfiles/src/tuikit/widthenv"
	"os"
	"testing"
)

// TestMain は設定と前回の場所の置き場所を一時ディレクトリへ向ける。
// 🚨 向けないと、テストが本物の ~/.config/glogx/treefiler.toml を書き換える (2026-10-09 に . のテストが実際に書いた)。
func TestMain(m *testing.M) {
	widthenv.ExitIfUnsupported() // 幅を数える env (RUNEWIDTH_EASTASIAN=1) では枠の検査が環境のせいで落ちる。理由を出して止める
	dir, err := os.MkdirTemp("", "treefiler-config-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1) // 隔離できないまま走らせると本物を触る
	}
	if err := os.Setenv("TREEFILER_CONFIG_DIR", dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir) // 消せなくても結果は変わらない (一時ディレクトリ)
	os.Exit(code)
}
