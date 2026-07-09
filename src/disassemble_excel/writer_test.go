package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A single far-flung cell (e.g. XFD1048576) must NOT materialize a dense
// maxRow*maxCol grid (~17e9 cells → OOM/hang). writeValuesCSV should detect the
// oversize dimension and write a short note instead, since .cells.tsv holds the
// full data.
func TestWriteValuesCSVSkipsHugeGrid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.values.csv")
	sh := &Sheet{
		Name: "S",
		// One value cell near A1 plus one at Excel's far corner.
		Cells: []Cell{
			{Row: 1, Col: 1, Addr: "A1", Value: "hi"},
			{Row: 1048576, Col: 16384, Addr: "XFD1048576", Value: "far"},
		},
	}
	if err := writeValuesCSV(path, sh); err != nil {
		t.Fatalf("writeValuesCSV: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.HasPrefix(got, "# grid skipped") {
		t.Errorf("expected oversize grid to be skipped with a note; got:\n%s", got[:min(len(got), 200)])
	}
	// Must be tiny — definitely not a multi-billion-cell CSV.
	if len(b) > 4096 {
		t.Errorf("skipped-grid note should be tiny, got %d bytes", len(b))
	}
}

// A normally-sized sheet still gets a real grid.
func TestWriteValuesCSVNormalGrid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.values.csv")
	sh := &Sheet{
		Name: "S",
		Cells: []Cell{
			{Row: 1, Col: 1, Addr: "A1", Value: "x"},
			{Row: 2, Col: 3, Addr: "C2", Value: "y"},
		},
	}
	if err := writeValuesCSV(path, sh); err != nil {
		t.Fatalf("writeValuesCSV: %v", err)
	}
	b, _ := os.ReadFile(path)
	got := string(b)
	if strings.HasPrefix(got, "# grid skipped") {
		t.Fatalf("normal grid must not be skipped; got:\n%s", got)
	}
	if !strings.Contains(got, "y") { // the C2 value must land in the grid
		t.Errorf("grid missing cell value; got:\n%s", got)
	}
}

func TestSanitizeFilenameTraversal(t *testing.T) {
	cases := map[string]string{
		"..":     "_",
		".":      "_",
		"":       "_",
		"a/b":    "a_b",
		"a\\b":   "a_b",
		"normal": "normal",
		"..bin":  "..bin", // only exact "."/".." are neutralized; "..bin" is a fine name
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}
