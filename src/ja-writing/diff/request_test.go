package diff

import (
	"context"
	"strings"
	"testing"

	"ja-writing/cmdenv"
	"ja-writing/internal/pyjson"
	"ja-writing/oracle"
	"ja-writing/oracle/replay"
	"ja-writing/visibility"
)

// requestValue は検査の本体 (本家の diff()) に渡す値を、記録の driver (oracle/py/record_diff_cli.py の _capturing_diff) と同じ鍵で組んだ JSON の値にする。
func requestValue(r request) pyjson.Value {
	notes := make([]pyjson.Value, len(r.notes))
	for i, n := range r.notes {
		notes[i] = n.Value()
	}
	materials := make([]pyjson.Value, len(r.materials))
	for i, m := range r.materials {
		materials[i] = m
	}
	var stance, words pyjson.Value
	if r.stance != "" {
		stance = r.stance
	}
	if r.words != nil {
		words = visibility.WordsContextOf(r.words).Value()
	}
	o := pyjson.NewObject()
	o.Set("orig", r.old)
	o.Set("rw", r.new)
	o.Set("stance", stance)
	o.Set("encoding_notes", notes)
	o.Set("doc_type", pyjson.OptionalString(r.docType))
	o.Set("reader", pyjson.OptionalString(r.reader))
	o.Set("words", words)
	o.Set("materials", materials)
	return o
}

// 検査の本体へ渡す値 (元と後の順・注記・立場・種類・読者・語の一覧・資料の順) を、本家の記録と突き合わせる。
// 検査の本体が入るまで CLI の出力は比べられない記録 (TestCLIMatchesPythonRecording の「比べていない」) も、本体へ渡す値は全部ここで比べる。
// 本体まで進む記録と、本体まで進む Go の呼び出しが、1 件ずつ一致することも見る (どちらかだけが進むと落ちる)。
func TestRequestMatchesPythonRecording(t *testing.T) {
	root := fixture.Root(t)
	var g oracle.LintCLIGolden
	if err := oracle.ReadGolden(goldenDir, oracle.DiffCLIFile, &g); err != nil {
		t.Fatal(err)
	}
	cases := oracle.DiffCLICases(fixtureCorpus, fixtureInputs, visibility.DocTypeValues(), visibility.ReaderValues())
	if len(cases) != len(g.Cases) {
		t.Fatalf("case が %d 件、記録が %d 件 (make golden で作り直す)", len(cases), len(g.Cases))
	}
	var got *request
	capture := func(_ context.Context, env cmdenv.Env) int {
		req, code, _ := prepare(env)
		got = req
		return code
	}
	reached, withStance, withDocType, withReader, withWords, withMaterials, withNotes := 0, 0, 0, 0, 0, 0, 0
	for i, c := range cases {
		rec := g.Cases[i]
		if rec.Name != c.Name {
			t.Fatalf("%d 番目の case の名前 %q が記録の %q と違う (make golden で作り直す)", i, c.Name, rec.Name)
		}
		got = nil
		oracle.RunMain(t, capture, root, c.Cwd, c.Stdin, c.Args...)
		if (got != nil) != (rec.Req != "") {
			t.Errorf("%s: 本体まで進むかが本家と違う (Go %v・本家 %v)", rec.Name, got != nil, rec.Req != "")
			continue
		}
		if got == nil {
			continue
		}
		reached++
		canon, err := pyjson.Dumps(replay.SortKeys(requestValue(*got)), pyjson.Options{Separators: &pyjson.Separators{Item: ",", Key: ":"}})
		if err != nil {
			t.Fatal(err)
		}
		if digest := oracle.CLIDigest(strings.ReplaceAll(canon, root, oracle.RootToken)); digest != rec.Req {
			t.Errorf("%s: 本体へ渡す値が本家と違う: %s、記録 %s\n%.400s", rec.Name, digest, rec.Req, canon)
		}
		withStance += b2i(got.stance != "")
		withDocType += b2i(got.docType != nil)
		withReader += b2i(got.reader != nil)
		withWords += b2i(got.words != nil)
		withMaterials += b2i(len(got.materials) > 0)
		withNotes += b2i(len(got.notes) > 0)
	}
	// 空振りで緑にしない: 本体へ渡す値の全部の欄に、値の入った記録がある。
	for name, n := range map[string]int{"本体まで進む記録": reached, "立場": withStance, "種類": withDocType, "読者": withReader, "語の一覧": withWords, "資料": withMaterials, "注記": withNotes} {
		if n == 0 {
			t.Errorf("%s が 0 件 (その欄が記録で当たらない)", name)
		}
	}
	t.Logf("本体へ渡す値を %d 件比べた (立場 %d・種類 %d・読者 %d・語の一覧 %d・資料 %d・注記 %d)", reached, withStance, withDocType, withReader, withWords, withMaterials, withNotes)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
