package oracle

import (
	"bytes"
	"os"
	"testing"
)

func loadUnicode(t *testing.T) UnicodeGolden {
	t.Helper()
	var g UnicodeGolden
	if err := ReadGolden(goldenDir, UnicodeFile, &g); err != nil {
		t.Fatalf("make golden で作る: %v", err)
	}
	return g
}

// 生成した Go の表が、記録 (unicode.json) から今の生成の規則で作り直したものと同じであること。
// 記録だけ作り直して表を作り忘れた・表を手で書き換えた、のどちらでも落ちる。
func TestGeneratedTablesMatchGolden(t *testing.T) {
	g := loadUnicode(t)
	want, err := GenerateUnicodeTables(g)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../" + PyunicodeTablesPath)
	if err != nil {
		t.Fatalf("表がありません。make golden で作る: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s が unicode.json から作り直したものと違う (%d バイトと %d バイト)。make golden で作り直す", PyunicodeTablesPath, len(got), len(want))
	}
}

func TestMarshalUnicodeRoundTrips(t *testing.T) {
	g := loadUnicode(t)
	body, err := MarshalUnicode(g)
	if err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(goldenDir + "/" + UnicodeFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, disk) {
		t.Fatal("unicode.json を読んで書き直すと元と違う (書き出しの形が読み側と食い違っている)")
	}
	parsed, err := ParseUnicode(body)
	if err != nil || len(parsed.Categories) != len(g.Categories) || len(parsed.Cases) != len(g.Cases) {
		t.Fatalf("ParseUnicode: %v", err)
	}
}

func TestGoldenFilesAreNotVacuous(t *testing.T) {
	u := loadUnicode(t)
	if u.Header.UpstreamCommit != Commit || u.Header.PythonVersion == "" || u.Header.UnicodedataVersion == "" {
		t.Fatalf("unicode.json の header: %+v", u.Header)
	}
	if len(u.Categories) < 1000 || len(u.CCC) < 100 || len(u.NFD) < 1000 || len(u.NFKD) < 1000 || len(u.Compose) < 500 || len(u.Cases) < 1000 {
		t.Fatalf("unicode.json が小さすぎる (空振り): 区間 %d・結合クラス %d・NFD %d・NFKD %d・合成 %d・文字列 %d",
			len(u.Categories), len(u.CCC), len(u.NFD), len(u.NFKD), len(u.Compose), len(u.Cases))
	}
	var j JSONGolden
	if err := ReadGolden(goldenDir, JSONFile, &j); err != nil {
		t.Fatalf("make golden で作る: %v", err)
	}
	if j.Header.UpstreamCommit != Commit || len(j.Dumps) < 1000 || len(j.Loads) < 500 || len(j.Round) < 1000 || len(j.RoundInt) < 100 {
		t.Fatalf("json.json が小さすぎる (空振り): dumps %d・loads %d・round %d・round_int %d", len(j.Dumps), len(j.Loads), len(j.Round), len(j.RoundInt))
	}
	var sawLoadErr, sawLoadOK, sawDumpErr bool
	for _, c := range j.Loads {
		sawLoadErr = sawLoadErr || c.Error != nil
		sawLoadOK = sawLoadOK || len(c.V) > 0 || c.Summary != nil
	}
	for _, c := range j.Dumps {
		sawDumpErr = sawDumpErr || c.Error != nil
	}
	if !sawLoadErr || !sawLoadOK || !sawDumpErr {
		t.Fatal("json.json に loads の成功・失敗と dumps の失敗の記録がそろっていない")
	}
	var s PystrGolden
	if err := ReadGolden(goldenDir, PystrFile, &s); err != nil {
		t.Fatalf("make golden で作る: %v", err)
	}
	if s.Header.UpstreamCommit != Commit || len(s.Cases) < 1000 {
		t.Fatalf("pystr.json が小さすぎる (空振り): %d 件", len(s.Cases))
	}
}
