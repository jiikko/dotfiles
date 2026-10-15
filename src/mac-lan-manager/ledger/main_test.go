package ledger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sandbox はこの package のテストが書いてよい唯一の場所。TestMain が作り、writeGuard はその外への書き込みを書く前に拒否する
// (_claude/rules/sandbox-real-destructive-test-apis.md)。台帳の置き場所 (EnvDir) もここへ向け、DefaultPath が本物を指さないようにする。
var sandbox string

func TestMain(m *testing.M) {
	d, err := os.MkdirTemp("", "mac-lan-manager-ledger-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if sandbox, err = filepath.EvalSymlinks(d); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := os.Setenv(EnvDir, filepath.Join(sandbox, "config")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	writeGuard = func(path string) error {
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		// 親を実体にしてから比べる (/var と /private/var の食い違い・symlink で外へ出る形を拒否する)
		parent := filepath.Dir(abs)
		for {
			if real, err := filepath.EvalSymlinks(parent); err == nil {
				abs = filepath.Join(real, strings.TrimPrefix(abs, parent))
				break
			}
			next := filepath.Dir(parent)
			if next == parent {
				break
			}
			parent = next
		}
		if !strings.HasPrefix(abs, sandbox+string(filepath.Separator)) { // sandbox の配下だけ (sandbox そのものも拒否する)
			return fmt.Errorf("テストの書き込みが一時ディレクトリの外を指している: %s (sandbox %s)", path, sandbox)
		}
		return nil
	}
	code := m.Run()
	_ = os.RemoveAll(d) // テストが作った一時ディレクトリの後始末
	os.Exit(code)
}
