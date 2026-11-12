package oracle

import (
	"bytes"
	"os"
	"testing"
)

func loadPyre(t *testing.T) PyreGolden {
	t.Helper()
	var g PyreGolden
	if err := ReadGolden(goldenDir, PyreFile, &g); err != nil {
		t.Fatalf("make golden で作る: %v", err)
	}
	return g
}

// 生成した大文字小文字の表が、記録 (pyre.json) から今の生成の規則で作り直したものと同じであること。
func TestGeneratedPyreTablesMatchGolden(t *testing.T) {
	want, err := GeneratePyreTables(loadPyre(t))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../" + PyreTablesPath)
	if err != nil {
		t.Fatalf("表がありません。make golden で作る: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s が pyre.json から作り直したものと違う。make golden で作り直す", PyreTablesPath)
	}
}

func TestMarshalPyreRoundTrips(t *testing.T) {
	g := loadPyre(t)
	body, err := MarshalPyre(g)
	if err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(goldenDir + "/" + PyreFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, disk) {
		t.Fatal("pyre.json を読んで書き直すと元と違う")
	}
	if len(g.Tables.Lower) < 1000 || len(g.Funcs) < 500 || len(g.Chars.Classes) < 60 {
		t.Fatalf("記録が少ない (lower %d・funcs %d・classes %d)", len(g.Tables.Lower), len(g.Funcs), len(g.Chars.Classes))
	}
}
