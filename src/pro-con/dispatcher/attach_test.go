package dispatcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"pro-con/store"
)

// Tick は記録に無いカード (削除した・書庫へ移した) の添付を消し、記録にあるカードの添付は残す (issue 453)。
func TestTickSweepsAttachmentsOfGoneCards(t *testing.T) {
	dir := t.TempDir()
	planned(t, dir, 1)
	for _, id := range []string{"C-001", "C-099"} {
		p := filepath.Join(dir, store.AttachDir, id, "x.png")
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	d := newDispatcher(t, dir, &fakeLauncher{}, nil)
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, store.AttachDir, "C-099")); err == nil {
		t.Fatal("記録に無いカードの添付が残る")
	}
	if _, err := os.Stat(filepath.Join(dir, store.AttachDir, "C-001", "x.png")); err != nil {
		t.Fatalf("記録にあるカードの添付を消した: %v", err)
	}
}
