//go:build !race

// 記録との突き合わせの重い検査 (race を付けると約 40 秒かかる)。1 つの goroutine で順に呼ぶだけなので、Makefile の test が race なしで別に回す。

package diff

import (
	"fmt"
	"slices"
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/internal/pyre"
	"ja-writing/oracle"
	"ja-writing/oracle/replay"
)

// 本家の記録 (testdata/golden/diff-ground.json) と、資料との照合 (Ground)・事実の穴の候補 (FactGaps)・引数を渡さない Diff・ExtractNumberDicts を突き合わせる。
// 戻り値は鍵を昇順にした正規の JSON の digest で比べ、続く dumps の呼び出しで、check が書くのと同じ json.dumps(ensure_ascii=True, indent=1) の文字列
// (鍵の順も含む) を比べる。

func groundConstantValue(t *testing.T, name string) pyjson.Value {
	t.Helper()
	patterns := map[string]*pyre.Lazy{"_NAME": groundName, "_URL_SCHEME": groundURLScheme, "_GROUND_WORD": groundWord, "_CERTAINTY": certainty,
		"_GROUND_CERTAINTY": groundCertainty, "_BARE_SETTING": bareSetting, "_SQUASHED": squashed, "_GROUND_UNIT": groundUnit,
		"_GAP_HEDGE": factGapHedge, "_GAP_NEGATED_TAIL": factGapNegatedTail, "_GAP_VAGUE": factGapVague, "_GAP_COMPARED": factGapCompared,
		"_GAP_QUESTION": factGapQuestion, "_GAP_KANJI_NUM": factGapKanjiNum, "_GAP_ASSUMPTION": factGapAssume, "_GAP_QUOTE": factGapQuote,
		"_GAP_OMISSION": factGapOmission}
	if l, ok := patterns[name]; ok {
		return lazyValue(l)
	}
	switch name {
	case "_GROUND_SAME_UNIT":
		return stringMap(groundSameUnit)
	case "_GROUND_NOT_JUDGED":
		return pyjson.Strings(groundNotJudged)
	case "_GROUND_KIND_ORDER":
		o := pyjson.NewObject()
		for _, k := range []string{"certainty", "name", "number", "word"} {
			o.Set(k, int64(groundKindOrder[k]))
		}
		return o
	case "_GROUND_MASKED":
		return pyjson.Strings(groundMasked)
	case "_CERTAINTY_WORDS":
		return pyjson.Strings(certaintyWords)
	case "_GAP_NEED":
		return stringMap(factGapNeed)
	case "_GAP_SKIP_KINDS":
		return pyjson.Strings(factGapSkipKinds)
	case "MATERIALS_FORMAT_VERSION":
		return int64(MaterialsFormatVersion)
	}
	t.Fatalf("定数 %s を Go に持っていない", name)
	return nil
}

func argStrings(t *testing.T, a any) []string {
	t.Helper()
	list, ok := a.([]any)
	if !ok {
		t.Fatalf("list でない引数 %T", a)
	}
	out := make([]string, len(list))
	for i, x := range list {
		out[i] = argString(t, x)
	}
	return out
}

func argOptStrings(t *testing.T, a any) []*string {
	t.Helper()
	if a == nil {
		return nil
	}
	list, ok := a.([]any)
	if !ok {
		t.Fatalf("list でない引数 %T", a)
	}
	out := make([]*string, len(list))
	for i, x := range list {
		out[i] = argOptString(t, x)
	}
	return out
}

// invokeGround は記録の呼び出し 1 つを Go で動かす。saved は直前に覚えた戻り値 (dumps が読む)。
func invokeGround(t *testing.T, call oracle.Call, saved *pyjson.Value, savedErr *error) (pyjson.Value, error) {
	t.Helper()
	if call.Get {
		return groundConstantValue(t, call.Function), nil
	}
	var v pyjson.Value
	var err error
	switch call.Function {
	case "ground":
		var r *pyjson.Object
		r, err = Ground(argString(t, call.Args[0]), argStrings(t, call.Args[1]), argOptString(t, call.Args[2]), argOptStrings(t, call.Args[3]))
		v = r
	case "fact_gaps":
		var r *pyjson.Object
		r, err = FactGaps(argString(t, call.Args[0]), argOptString(t, call.Args[1]))
		v = r
	case "diff":
		var r *pyjson.Object
		r, err = Diff(argString(t, call.Args[0]), argString(t, call.Args[1]))
		v = r
	case "extract_numbers":
		var r []pyjson.Value
		r, err = ExtractNumberDicts(argString(t, call.Args[0]))
		v = r
	case "dumps":
		if *savedErr != nil {
			t.Fatalf("dumps の前の呼び出しが例外になった (記録の case の組み方の誤り): %v", *savedErr)
		}
		s, derr := pyjson.Dumps(*saved, pyjson.Indent1())
		if derr != nil {
			t.Fatal(derr)
		}
		return s, nil
	default:
		t.Fatalf("関数 %s を Go に持っていない", call.Function)
	}
	if call.Save != "" {
		*saved, *savedErr = v, err
	}
	if err != nil {
		return nil, err
	}
	return replay.SortKeys(v), nil
}

func TestGroundMatchesPythonRecording(t *testing.T) {
	g, cases := loadGround(t)
	if len(cases) != len(g.Cases) {
		t.Fatalf("case %d 件に対して記録は %d 件。go run ./oracle/cmd/golden -only diff-ground で作り直す", len(cases), len(g.Cases))
	}
	calls := map[string]int{}
	raisedBy := map[string]int{}
	failed := 0
	for i, c := range cases {
		rec := g.Cases[i]
		if rec.Name != c.Name || rec.Calls != len(c.Calls) || len(rec.Digests) != rec.Calls*oracle.DigestWidth {
			t.Fatalf("case %d が記録と違う (%s %d 件 / 記録 %s %d 件)。作り直す", i, c.Name, len(c.Calls), rec.Name, rec.Calls)
		}
		kept := map[int]oracle.VisKept{}
		for _, k := range rec.Kept {
			kept[k.Index] = k
		}
		var saved pyjson.Value
		var savedErr error
		for j, call := range c.Calls {
			calls[call.Function]++
			got, exc := invokeGround(t, call, &saved, &savedErr)
			canon, digest, raised := outcomeDigest(t, got, exc)
			if raised {
				raisedBy[call.Function]++
			}
			if want := rec.Digests[j*oracle.DigestWidth : (j+1)*oracle.DigestWidth]; digest != want {
				failed++
				if failed <= 10 {
					msg := fmt.Sprintf("case %s の %d 番目 (%s): digest %s、記録 %s\n  Go: %s", c.Name, j, call.Function, digest, want, clip(canon, 600))
					if k, ok := kept[j]; ok {
						msg += "\n  記録: " + clip(string(k.Result), 600)
						if k.Exception != nil {
							msg += fmt.Sprintf(" 例外 %s: %s", k.Exception.Type, k.Exception.Message)
						}
					}
					t.Error(msg)
				}
			}
			if raised != slices.Contains(rec.Raised, j) {
				failed++
				t.Errorf("case %s の %d 番目 (%s): Go が例外を投げた %v、記録の本家が例外を投げた %v", c.Name, j, call.Function, raised, !raised)
			}
		}
	}
	if failed > 10 {
		t.Errorf("ほかにも %d 件の食い違いがあります", failed-10)
	}
	for _, f := range g.Functions {
		if calls[f.Name] != f.Calls || raisedBy[f.Name] != f.Raised {
			t.Errorf("%s の呼び出し数・例外の数が記録と違う: Go %d・%d、記録 %d・%d", f.Name, calls[f.Name], raisedBy[f.Name], f.Calls, f.Raised)
		}
	}
	total := 0
	for _, n := range calls {
		total += n
	}
	t.Logf("照合の記録 %d case・呼び出し %d 件を全部比べた", len(cases), total)
}
