package oracle

import (
	"bytes"
	"encoding/json"
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

// 単独のサロゲートは {"ws": [...]} でだけ持てる。JSON の文字列の "\ud800" は encoding/json が U+FFFD にするので断り、
// 上位と下位が続く組 (BMP の外の 1 字。ensure_ascii が書く形) は普通の文字として受ける。
func TestWStrKeepsLoneSurrogates(t *testing.T) {
	for _, raw := range []string{`"\ud800"`, `"a\udfffb"`, `"\ude00\ud83d"`, `"\ud83dx"`} {
		var w WStr
		if err := json.Unmarshal([]byte(raw), &w); err == nil {
			t.Errorf("単独のサロゲートの escape %s を文字列として受けた (%q)", raw, string(w))
		}
	}
	var pair WStr
	if err := json.Unmarshal([]byte(`"あ😀x"`), &pair); err != nil || string(pair) != "あ\U0001F600x" {
		t.Errorf("上位と下位の組は 1 字として読む: %q %v", string(pair), err)
	}
	for _, in := range []string{`{"ws":[97,55296,56320,98]}`, `{"ws":[57343]}`} {
		var w WStr
		if err := json.Unmarshal([]byte(in), &w); err != nil {
			t.Fatal(err)
		}
		out, err := json.Marshal(w)
		if err != nil || string(out) != in {
			t.Errorf("%s を読んで書くと %s (%v)", in, out, err)
		}
	}
	if out, _ := json.Marshal(WStr("あ")); string(out) != `"あ"` {
		t.Errorf("サロゲートを含まない文字列は文字列のまま: %s", out)
	}
}
