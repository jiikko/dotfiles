package diff

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"ja-writing/internal/bold"
	"ja-writing/internal/pyjson"
	"ja-writing/oracle"
	"ja-writing/oracle/replay"
)

// 本家 (Python) の記録 (testdata/golden/diff-base.json) と、群 B (9 キーの下ごしらえ)・C (文末の種類と立場)・E (太字) の Go の実装を突き合わせる。
// 記録は oracle.DiffBaseCases の呼び出しを本家に通した結果の digest (正規の JSON の SHA-256 の先頭 8 字) で、Go も同じ呼び出しを通して同じ正規の JSON を作る
// (本家の sort_keys=True を再現する代わりに、組む側が鍵を昇順に並べて入れる)。

func unitValue(u EndingUnit) pyjson.Value {
	o := pyjson.NewObject()
	o.Set("full", u.Full)
	o.Set("kind", u.Kind)
	o.Set("sentence", u.Sentence)
	o.Set("where", u.Where)
	return o
}

func unitsValue(rows []EndingUnit) pyjson.Value { return replay.MapList(rows, unitValue) }

func flagRowsValue(rows []*EndingUnit) pyjson.Value {
	return replay.MapList(rows, func(r *EndingUnit) pyjson.Value { return unitValue(*r) })
}

func argString(t *testing.T, a any) string {
	t.Helper()
	s, ok := a.(string)
	if !ok {
		t.Fatalf("文字列でない引数 %T", a)
	}
	return s
}

func argBool(t *testing.T, a any) bool {
	t.Helper()
	b, ok := a.(bool)
	if !ok {
		t.Fatalf("bool でない引数 %T", a)
	}
	return b
}

// invoke は記録の呼び出し 1 つを Go の実装に通した結果。
func invoke(t *testing.T, c oracle.Call) pyjson.Value {
	t.Helper()
	a := c.Args
	switch c.Function {
	case "normalize":
		return Normalize(argString(t, a[0]))
	case "has_list":
		return HasList(argString(t, a[0]))
	case "sentences":
		return replay.MapList(Sentences(argString(t, a[0])), func(s string) pyjson.Value { return s })
	case "paragraphs":
		return replay.MapList(Paragraphs(argString(t, a[0])), func(s string) pyjson.Value { return s })
	case "logic_points":
		return replay.MapList(LogicPoints(argString(t, a[0])), func(p LogicPoint) pyjson.Value {
			o := pyjson.NewObject()
			o.Set("kind", p.Kind)
			o.Set("sentence", p.Sentence)
			return o
		})
	case "bare_end":
		return BareEnd(argString(t, a[0]))
	case "register":
		return Register(argString(t, a[0]))
	case "ending_kind":
		return EndingKind(argString(t, a[0]))
	case "ending_units":
		return unitsValue(EndingUnits(argString(t, a[0]), argBool(t, a[1])))
	case "stance_flags":
		stance := ""
		if a[1] != nil {
			stance = argString(t, a[1])
		}
		rows, flags := StanceFlags(argString(t, a[0]), stance, argBool(t, a[2]))
		return replay.List(unitsValue(rows), replay.MapList(flags, func(f StanceFlag) pyjson.Value { return replay.List(f.Note, flagRowsValue(f.Rows)) }))
	case "ending_changes":
		return replay.MapList(EndingChanges(argString(t, a[0]), argString(t, a[1])), func(c EndingChange) pyjson.Value {
			o := pyjson.NewObject()
			o.Set("orig", c.Orig)
			o.Set("orig_kind", c.OrigKind)
			o.Set("rewrite", c.Rewrite)
			o.Set("rewrite_kind", c.RewriteKind)
			return o
		})
	case "bold_problems":
		problems, err := bold.Problems(argString(t, a[0]), argBool(t, a[1]))
		if err != nil {
			t.Fatalf("bold_problems: %v", err)
		}
		return replay.MapList(problems, func(p bold.Problem) pyjson.Value {
			o := pyjson.NewObject()
			o.Set("found", p.Found)
			o.Set("how", p.How)
			o.Set("line", int64(p.Line))
			o.Set("suggest", p.Suggest)
			return o
		})
	}
	t.Fatalf("関数 %s を Go に持っていない", c.Function)
	return nil
}

func loadBase(t *testing.T) (oracle.DiffBaseGolden, []oracle.VisCase) {
	t.Helper()
	var g oracle.DiffBaseGolden
	if err := oracle.ReadGolden(goldenDir, oracle.DiffBaseFile, &g); err != nil {
		t.Fatal(err)
	}
	corpus, err := oracle.LoadCorpus(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := oracle.LoadDiffInputs(diffDir)
	if err != nil {
		t.Fatal(err)
	}
	cases, err := oracle.DiffBaseCases(corpus, diffDir, inputs)
	if err != nil {
		t.Fatal(err)
	}
	return g, cases
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func TestBaseMatchesPythonRecording(t *testing.T) {
	g, cases := loadBase(t)
	if len(cases) != len(g.Cases) {
		t.Fatalf("case %d 件に対して記録は %d 件。make golden で作り直す", len(cases), len(g.Cases))
	}
	calls := map[string]int{}
	failed := 0
	for i, c := range cases {
		rec := g.Cases[i]
		if rec.Name != c.Name || rec.Calls != len(c.Calls) || len(rec.Digests) != rec.Calls*oracle.DigestWidth {
			t.Fatalf("case %d が記録と違う (%s %d 件 / 記録 %s %d 件)。make golden で作り直す", i, c.Name, len(c.Calls), rec.Name, rec.Calls)
		}
		kept := map[int]oracle.VisKept{}
		for _, k := range rec.Kept {
			kept[k.Index] = k
		}
		for j, call := range c.Calls {
			calls[call.Function]++
			canon, digest, err := replay.ResultDigest(invoke(t, call))
			if err != nil {
				t.Fatal(err)
			}
			if want := rec.Digests[j*oracle.DigestWidth : (j+1)*oracle.DigestWidth]; digest != want {
				failed++
				if failed <= 15 {
					msg := fmt.Sprintf("case %s の %d 番目 (%s %.60q): digest %s、記録 %s\n  Go: %s", c.Name, j, call.Function, fmt.Sprint(call.Args[0]), digest, want, clip(canon, 400))
					if k, ok := kept[j]; ok {
						msg += "\n  記録: " + clip(string(k.Result), 400)
					}
					t.Error(msg)
				}
			}
		}
	}
	if failed > 15 {
		t.Errorf("ほかにも %d 件の食い違いがあります", failed-15)
	}
	for _, f := range g.Functions {
		if calls[f.Name] != f.Calls {
			t.Errorf("%s の呼び出し数が記録と違う: Go %d、記録 %d", f.Name, calls[f.Name], f.Calls)
		}
	}
	total := 0
	for _, n := range calls {
		total += n
	}
	t.Logf("関数の記録 %d case・呼び出し %d 件を全部比べた", len(cases), total)
}

// 空振りを止める: 関数ごとに記録の件数が 0 でなく、記録に一覧にない名前が無く、例外になる呼び出しが無い。
// 文末の関数は、種類ごとに当たる文と当たらない文が記録の手書きの入力にある (記録の中の結果 Kept で数える)。
func TestBaseRecordingCoversEveryFunction(t *testing.T) {
	g, _ := loadBase(t)
	byName := map[string]oracle.VisFunctionRecord{}
	for _, f := range g.Functions {
		byName[f.Name] = f
	}
	for _, name := range oracle.DiffBaseFunctions {
		if rec, ok := byName[name]; !ok || rec.Calls == 0 {
			t.Errorf("%s の記録が 0 件 (空振り)", name)
		}
	}
	for _, f := range g.Functions {
		if !slices.Contains(oracle.DiffBaseFunctions, f.Name) {
			t.Errorf("記録に、一覧 (oracle.DiffBaseFunctions) にない名前 %s がある", f.Name)
		}
		if f.Raised != 0 {
			t.Errorf("%s が例外になる呼び出しを %d 件持つ", f.Name, f.Raised)
		}
	}
	kinds := map[string]bool{}
	for _, c := range g.Cases {
		if c.Name != "snippet/endings" {
			continue
		}
		for _, k := range c.Kept {
			var s string
			if json.Unmarshal(k.Result, &s) == nil {
				kinds[s] = true
			}
		}
	}
	for _, want := range []string{"依頼", "勧め", "推量・考え", "義務", "評価", "過去", "説明（〜ています）", "説明（〜ます）", "動作（〜します）", "断定（〜です）", "常体", "体言止めなど", "敬体", ""} {
		if !kinds[want] {
			t.Errorf("手書きの文に、結果が %q になるものが無い (その枝が記録に当たらない)", want)
		}
	}
}
