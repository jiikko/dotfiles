// Package testsandbox は、ledger と prefs のテストの書き込みを一時ディレクトリの配下に閉じ込める関門を持つ。
// 本番からは使わない。拒否は書く前に置く (_claude/rules/sandbox-real-destructive-test-apis.md)。2 つの package に同じ関門を写していたので 1 つに寄せた。
package testsandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// New は一時ディレクトリを作り、実体のパスで返す (/var と /private/var の食い違いを Guard の比較に持ち込まない)。
func New(prefix string) (string, error) {
	d, err := os.MkdirTemp("", prefix)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(d)
	if err != nil {
		_ = os.RemoveAll(d) //nolint:forbidigo // 今作った空の一時ディレクトリを消すだけ
		return "", err
	}
	return real, nil
}

// Guard は sandbox の配下だけを通す関門 (sandbox そのものも拒否する)。親を実体にしてから比べる (symlink で外へ出る形を拒否する)。
func Guard(sandbox string) func(path string) error {
	return func(path string) error {
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
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
		if !strings.HasPrefix(abs, sandbox+string(filepath.Separator)) {
			return fmt.Errorf("テストの書き込みが一時ディレクトリの外を指している: %s (sandbox %s)", path, sandbox)
		}
		return nil
	}
}
