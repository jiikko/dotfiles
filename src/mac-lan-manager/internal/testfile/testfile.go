// Package testfile はテストが package の testdata/ から固定の入力 (本物のコマンドの出力を取ったもの) を読む helper。
package testfile

import (
	"os"
	"path/filepath"
	"testing"
)

// Read は testdata/<name> の中身。読めなければテストを止める (go test は package の dir を cwd にする)。
func Read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
