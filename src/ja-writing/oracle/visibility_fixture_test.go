package oracle

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// Python の driver が op write で書くバイト列 (JSON を通ってきた op から。content は UTF-8、bytes_b64 は復号して作業フォルダの印を置き換える)。
func pythonWrites(t *testing.T, op Op, root string) []byte {
	t.Helper()
	raw, err := json.Marshal(op)
	if err != nil {
		t.Fatal(err)
	}
	var over Op
	if err := json.Unmarshal(raw, &over); err != nil {
		t.Fatal(err)
	}
	return (&Fixture{Root: root}).mustWriteData(t, over)
}

func (f *Fixture) mustWriteData(t *testing.T, op Op) []byte {
	t.Helper()
	data, err := f.WriteData(op)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// fixture の write は、Go が書くバイト列と、JSON を通って Python が書くバイト列が同じ。違うと、Go と Python が別の入力を比べたまま記録の突き合わせが緑になる。
func TestFixtureWritesSameBytesInGoAndPython(t *testing.T) {
	c, err := LoadCorpus(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	cases, err := VisibilityCases(c)
	if err != nil {
		t.Fatal(err)
	}
	const root = "/r/o/o/t"
	writes, invalid := 0, 0
	for _, vc := range cases {
		for i, call := range vc.Calls {
			if call.Op == nil || call.Op.Kind != "write" {
				continue
			}
			writes++
			goBytes := (&Fixture{Root: root}).mustWriteData(t, *call.Op)
			if !bytes.Equal(goBytes, pythonWrites(t, *call.Op, root)) {
				t.Errorf("case %s の %d 番目の write: Go と Python が別のバイト列を書く (%q)", vc.Name, i, goBytes)
			}
			if !utf8.Valid(goBytes) {
				invalid++
			}
		}
	}
	if writes == 0 {
		t.Fatal("write の操作が 1 件も無い (空振り)")
	}
	if invalid < 4 {
		t.Errorf("不正な UTF-8 を書く fixture が %d 件しかない (bad-utf8 の case が消えた疑い)", invalid)
	}
}

// 不正な UTF-8 を Content に入れた op は、JSON を通ると U+FFFD になる。fixtureCase.op は BytesB64 に直す。
func TestFixtureCaseMovesInvalidUTF8ToBytes(t *testing.T) {
	f := newFixtureCase("x")
	f.op(Op{Kind: "write", Path: "p", Content: "a\xffb"})
	op := f.calls[len(f.calls)-1].Op
	if op.Content != "" || op.BytesB64 == "" {
		t.Fatalf("不正な UTF-8 の中身が BytesB64 に移っていない: %+v", op)
	}
	if got := (&Fixture{Root: "/r"}).mustWriteData(t, *op); string(got) != "a\xffb" {
		t.Fatalf("書くバイト列 %q", got)
	}
	if string(pythonWrites(t, Op{Kind: "write", Content: "a\xffb"}, "/r")) == "a\xffb" {
		t.Fatal("JSON を通った不正な UTF-8 が元のバイト列のまま (この検査が U+FFFD への変換を見ていない)")
	}
}

// fixture の操作が Python で失敗したら、記録を作らずに case 名・操作の位置・例外を出して止まる (Go の Fixture.Apply は同じ操作を成功させうる)。
func TestAssembleVisibilityStopsOnFailedOperation(t *testing.T) {
	cases := []VisCase{{Name: "c1", Calls: []Call{
		vop(Op{Kind: "mkdir", Path: "<<ROOT>>/a"}),
		vop(Op{Kind: "remove", Path: "<<ROOT>>/nowhere"}),
	}}}
	ok := CallResult{Digest: "01234567"}
	failed := CallResult{Digest: "89abcdef", Raised: true, Exception: &PyException{Type: "FileNotFoundError", Message: "nowhere"}}
	_, err := assembleVisibility(Header{}, cases, []CallResult{ok, failed})
	if err == nil {
		t.Fatal("操作の失敗を見つけずに記録を作った")
	}
	for _, want := range []string{"c1", "1 番目", "remove", "FileNotFoundError", "nowhere"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("エラー %q に %q が無い", err, want)
		}
	}
	// 関数の例外は記録の中身なので止めない。
	fn := []VisCase{{Name: "c2", Calls: []Call{vcall("check_pattern", "(")}}}
	if _, err := assembleVisibility(Header{}, fn, []CallResult{{Digest: "01234567", Raised: true}}); err != nil {
		t.Fatalf("関数の例外で止まった: %v", err)
	}
}
