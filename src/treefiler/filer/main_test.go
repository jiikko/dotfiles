package filer

import (
	"fmt"
	"github.com/jiikko/dotfiles/src/tuikit/widthenv"
	"os"
	"slices"
	"strings"
	"testing"

	"safefs/sandbox"
	"subproc"
)

// sb はファイル操作 (fileops.go) の検査点。🚨 テストは本物の作成・rename・ゴミ箱への移動を呼ぶので、登録した一時ディレクトリの外は
// 実行前に拒否する。拒否が 1 件でも残ったら、テストが全部緑でもスイートを落とす (拒否はテストが壊れている証拠)。
// ゴミ箱の場所 (trashDir) は既定の ~/.Trash のままにしておく: 差し替え忘れたテストはここで拒否されて落ちる。
var sb sandbox.Sandbox

// TestMain は設定と前回の場所の置き場所を一時ディレクトリへ向ける。
// 🚨 向けないと、テストが本物の ~/.config/treefiler/treefiler.toml を書き換える (2026-10-09 に . のテストが実際に書いた)。
func TestMain(m *testing.M) {
	LoadWait = -1                                         // 打鍵で頼むフォルダの読み込みを同期で待つ (壁時計で同期 / 非同期が分かれると、同じキーの列で画面が変わる。非同期は load_test.go が見る)
	if err := subproc.UnsetGitLocationEnv(); err != nil { // 継承した GIT_DIR などで、一時 dir に作る repo の git が別の repo を書かない (issue 740)
		panic(err)
	}
	widthenv.ExitIfUnsupported() // 幅を数える env (RUNEWIDTH_EASTASIAN=1) では枠の検査が環境のせいで落ちる。理由を出して止める
	if os.Getenv(bangHelperEnv) != "" && slices.Contains(os.Args[1:], bangHelperRunArg) {
		// TestBangGivesTerminalBackOnPTY が起こした helper の子。helper は os.Exit で終わり m.Run から戻らないので、ここで置き場を作ると
		// 消されずに $TMPDIR に残る。親が向けた置き場を env で継いでいるので、それを使う。
		// 起動の引数も見る: env だけを外から設定して go test を打つと、ここに入って全部のテストが継いだ置き場 (本物でありうる) で走る
		if os.Getenv("TREEFILER_CONFIG_DIR") == "" {
			fmt.Fprintln(os.Stderr, "helper の子に TREEFILER_CONFIG_DIR が渡っていない")
			os.Exit(1) // 隔離できないまま走らせると本物を触る
		}
		fsGuard = sb.Guard
		os.Exit(m.Run())
	}
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
