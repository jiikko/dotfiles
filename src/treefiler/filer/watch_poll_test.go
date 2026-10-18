package filer

import (
	"os"
	"path/filepath"
	"testing"
)

// 1 周の記録 (metas) は今見ているフォルダのぶんだけ。見なくなったフォルダの記録は捨てる。
func TestPollDropsMetasOfUnwatchedDirs(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	for _, d := range []string{a, b} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	w := newWatcher()
	metas, _, _ := w.poll([]string{a, b}, nil)
	if len(metas) != 2 {
		t.Fatalf("記録 %d 件, want 2", len(metas))
	}
	metas, _, _ = w.poll([]string{b}, metas)
	if _, ok := metas[a]; ok || len(metas) != 1 {
		t.Fatalf("見なくなった a の記録が残った: %v", metas)
	}
}
