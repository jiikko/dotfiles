package filer

import (
	"fmt"
	"github.com/jiikko/dotfiles/src/tuikit/widthenv"
	"os"
	"strings"
	"testing"

	"safefs/sandbox"
)

// sb はファイル操作 (fileops.go) の検査点。🚨 テストは本物の作成・rename・ゴミ箱への移動を呼ぶので、登録した一時ディレクトリの外は
// 実行前に拒否する。拒否が 1 件でも残ったら、テストが全部緑でもスイートを落とす (拒否はテストが壊れている証拠)。
// ゴミ箱の場所 (trashDir) は既定の ~/.Trash のままにしておく: 差し替え忘れたテストはここで拒否されて落ちる。
var sb sandbox.Sandbox

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
	fsGuard = sb.Guard
	code := m.Run()
	if v := sb.Violations(); len(v) > 0 {
		fmt.Fprintf(os.Stderr, "sandbox が拒否したファイル操作が残っています (テストが一時ディレクトリの外を触ろうとした):\n  %s\n", strings.Join(v, "\n  "))
		code = 1
	}
	_ = os.RemoveAll(dir) // 消せなくても結果は変わらない (一時ディレクトリ)
	os.Exit(code)
}
