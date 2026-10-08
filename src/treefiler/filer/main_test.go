package filer

import (
	"fmt"
	"os"
	"testing"
)

// TestMain は設定と前回の場所の置き場所を一時ディレクトリへ向ける。
// 🚨 向けないと、テストが本物の ~/.config/glogx/treefiler.toml を書き換える (2026-10-09 に . のテストが実際に書いた)。
func TestMain(m *testing.M) {
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
