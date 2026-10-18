package filer

import (
	"path/filepath"
	"strings"
	"testing"
)

// 配下の走査は名前の順に数える (budget で打ち切る位置を os.ReadDir の頃と変えない。まとめて読む経路はフォルダの中の順で返す)。
func TestWalkDirCountsInNameOrderUnderBudget(t *testing.T) {
	d := t.TempDir()
	// 作る順と名前の順を逆にする (フォルダの中の順が名前の順と違うようにする)
	for _, f := range []struct{ name, body string }{{"c", strings.Repeat("x", 100)}, {"b", strings.Repeat("x", 10)}, {"a", "x"}} {
		mustWrite(t, filepath.Join(d, f.name), f.body)
	}
	budget := 2
	r := walkDir(d, &budget, map[string]walkResult{})
	if r.complete || r.bytes != 11 {
		t.Fatalf("bytes = %d complete = %v (名前の順に a・b の 11 バイトで打ち切ること)", r.bytes, r.complete)
	}
}
