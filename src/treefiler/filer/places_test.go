package filer

import (
	"path/filepath"
	"slices"
	"testing"
)

// Place は、カーソルの経路の外で開いたフォルダも記録し、RestorePlace がそれを開く (カーソルの祖先は RestorePlace が
// 開くので、祖先だけを見るテストでは Opened の欠落が見えない)。
func TestPlaceRoundTripsFoldersOffCursorPath(t *testing.T) {
	m := newTest(t)
	a := m.loadPath(filepath.Join(m.root.abs, "a"))
	b := m.loadPath(filepath.Join(m.root.abs, "b"))
	if a == nil || b == nil || !a.dir || !b.dir {
		t.Fatal("fixture の a / b が無い")
	}
	a.expanded = true
	m.setCur(b) // a の外 (b は開かない)
	p := m.Place()
	if p.Start != m.startDir || p.Cursor != b.path() || !slices.Contains(p.Opened, a.path()) {
		t.Fatalf("Place = %+v (a = %s を開き、カーソルは b のはず)", p, a.path())
	}

	m2, err := New(m.root.abs, Options{Now: m.now})
	if err != nil {
		t.Fatal(err)
	}
	m2.RestorePlace(p)
	if n := m2.loadPath(a.path()); n == nil || !n.expanded {
		t.Error("カーソルの経路の外で開いていた a が開かない")
	}
	if m2.cur.path() != b.path() {
		t.Errorf("カーソル = %s, want %s", m2.cur.path(), b.path())
	}
}
