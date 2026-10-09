package filer

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// \r で終わるパスは前回の場所に書かない (bufio.Scanner が行末の \r を落とし、別のパスとして読み戻すため。issue 696 の 14)。
// カーソルの行と、開いたフォルダの行 (opened) を別々に見る (カーソルに \r があると場所ごと書かないので、opened の判定まで届かない)。
func TestPlacesSkipCarriageReturn(t *testing.T) {
	for _, tc := range []string{"cursor", "opened"} {
		m := newTest(t)
		m.set.Remember = true
		cr := filepath.Join(m.root.abs, "cr\r")
		mustWrite(t, cr, "")
		dcr := filepath.Join(m.root.abs, "dcr\r")
		if err := os.Mkdir(dcr, 0o755); err != nil {
			t.Fatal(err)
		}
		m.Refresh()
		if m.findNode(cr) == nil || m.findNode(dcr) == nil {
			t.Fatal("前提: cr\\r / dcr\\r が木に無い")
		}
		if tc == "cursor" {
			m.setCur(m.findNode(cr))
		} else {
			m.findNode(dcr).expanded = true // カーソルは fixture の先頭のまま
		}
		m.Close()
		ps, ok := readPlaces(placesPath())
		if !ok || len(ps) == 0 && tc == "opened" {
			t.Fatalf("%s: 記録を読めない / 場所が無い (%v, %d)", tc, ok, len(ps))
		}
		for _, p := range ps {
			if p.cursor == filepath.Join(m.root.abs, "cr") || slices.Contains(p.opened, filepath.Join(m.root.abs, "dcr")) {
				t.Fatalf("%s: \\r を落とした別のパスとして読み戻された: %+v", tc, p)
			}
		}
	}
}
