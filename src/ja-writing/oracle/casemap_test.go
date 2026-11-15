package oracle

import (
	"bytes"
	"os"
	"testing"
)

func loadCasemap(t *testing.T) CasemapGolden {
	t.Helper()
	var g CasemapGolden
	if err := ReadGolden(goldenDir, CasemapFile, &g); err != nil {
		t.Fatalf("make golden で作る: %v", err)
	}
	return g
}

// 生成した大文字小文字の表が、記録 (casemap.json) から今の生成の規則で作り直したものと同じであること。
func TestGeneratedCasemapTablesMatchGolden(t *testing.T) {
	want, err := GenerateCasemapTables(loadCasemap(t))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../" + CasemapTablesPath)
	if err != nil {
		t.Fatalf("表がありません。make golden で作る: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s が casemap.json から作り直したものと違う。make golden で作り直す", CasemapTablesPath)
	}
}

func TestMarshalCasemapRoundTrips(t *testing.T) {
	body, err := MarshalCasemap(loadCasemap(t))
	if err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(goldenDir + "/" + CasemapFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, disk) {
		t.Fatal("casemap.json を読んで書き直すと元と違う")
	}
}
