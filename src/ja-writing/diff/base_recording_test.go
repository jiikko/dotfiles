//go:build !race

// 記録との突き合わせの重い検査。1 つの goroutine で順に呼ぶだけで、-race を付けても見つけられるデータ競争は無く、時間だけが数倍になる
// (CI の race の run で lint のパッケージが go test の時間の上限 10 分を超えた)。race の run からは外し、Makefile の test が race なしで別に回す。

package diff

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/oracle"
)

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
		var build []any
		saved := map[string]string{}
		for j, call := range c.Calls {
			calls[call.Function]++
			var got pyjson.Value
			var exc error
			switch {
			case call.Quiet:
				// 覚えるだけの呼び出し (結果の digest は 8 つの 0)。段の呼び出しが同じ引数から作り直す
				switch call.Function {
				case "_build_full_diff":
					build = call.Args
				case "normalize":
					saved[call.Save] = Normalize(argString(t, call.Args[0]))
				case "_certainty_word_spans":
					certaintySaved[call.Save] = argString(t, call.Args[0])
				}
				if want := rec.Digests[j*oracle.DigestWidth : (j+1)*oracle.DigestWidth]; want != "00000000" {
					t.Errorf("case %s の %d 番目: 覚えるだけの呼び出しの digest が %s", c.Name, j, want)
				}
				continue
			case call.Get && slices.Contains(oracle.DiffCertaintyConstantNames(), call.Function):
				got = certaintyConstantValue(t, call.Function)
			case slices.Contains(oracle.DiffCertaintyFunctions, call.Function):
				got = invokeCertainty(t, call, build)
			case call.Get && slices.Contains(oracle.DiffContrastConstantNames(), call.Function):
				got = contrastConstantValue(t, call.Function)
			case slices.Contains(oracle.DiffContrastFunctions, call.Function):
				got = invokeContrast(t, call, build)
			case call.Get && slices.Contains(oracle.DiffQualifierConstantNames(), call.Function):
				got = qualifierConstantValue(t, call.Function)
			case slices.Contains(oracle.DiffQualifierFunctions, call.Function):
				got = invokeQualifier(t, call, build)
			case slices.Contains(oracle.DiffFeelingFunctions, call.Function):
				got = invokeFeeling(t, call, build)
			case call.Get && slices.Contains(oracle.DiffQuestionConstantNames(), call.Function):
				got = questionConstantValue(t, call.Function)
			case slices.Contains(oracle.DiffQuestionFunctions, call.Function):
				got = invokeQuestion(t, call, build)
			case call.Get && slices.Contains(oracle.DiffNegationConstantNames(), call.Function):
				got = negationConstantValue(t, call.Function)
			case call.Get && slices.Contains(oracle.DiffSwapConstantNames(), call.Function):
				got = swapConstantValue(t, call.Function)
			case slices.Contains(oracle.DiffNegationFunctions, call.Function) || slices.Contains(oracle.DiffSwapFunctions, call.Function):
				got = invokeNegation(t, call, build)
			case call.Get && slices.Contains(oracle.DiffNumberConstantNames(), call.Function):
				got = numberConstantValue(t, call.Function)
			case slices.Contains(oracle.DiffNumberFunctions, call.Function):
				got = invokeNumber(t, call, build)
			case slices.Contains(oracle.DiffListFunctions, call.Function):
				got, exc = invokeList(t, call)
			case call.Get && slices.Contains(oracle.DiffToneConstantNames(), call.Function):
				got = toneConstantValue(t, call.Function)
			case slices.Contains(oracle.DiffToneFunctions, call.Function):
				got, exc = invokeTone(t, call, build)
			case call.Get && slices.Contains(oracle.DiffStiffConstantNames(), call.Function):
				got = stiffConstantValue(t, call.Function)
			case slices.Contains(oracle.DiffStiffFunctions, call.Function):
				got = invokeStiff(t, call)
			case call.Get && slices.Contains(oracle.DiffModalConstantNames(), call.Function):
				got = modalConstantValue(t, call.Function)
			case call.Get:
				got = typeConstantValue(t, call.Function)
			case call.Function == "_modality_changes" || call.Function == "_logic_changes":
				got = invokeModal(t, call, saved)
			case call.Function == "_rule_candidates":
				got = invokeRuleCandidates(t, call)
			case call.Function == "_type_changes":
				got = invokeTypeChanges(t, call, saved)
			case strings.HasPrefix(call.Function, "_"):
				got = invokeStage(t, call, build)
			default:
				got = invoke(t, call)
			}
			canon, digest, raised := outcomeDigest(t, got, exc)
			if want := rec.Digests[j*oracle.DigestWidth : (j+1)*oracle.DigestWidth]; digest != want {
				failed++
				if failed <= 15 {
					msg := fmt.Sprintf("case %s の %d 番目 (%s %.60q): digest %s、記録 %s\n  Go: %s", c.Name, j, call.Function, fmt.Sprint(call.Args), digest, want, clip(canon, 400))
					if k, ok := kept[j]; ok {
						msg += "\n  記録: " + clip(string(k.Result), 400)
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
	if failed > 15 {
		t.Errorf("ほかにも %d 件の食い違いがあります", failed-15)
	}
	for _, f := range g.Functions {
		if calls[f.Name] != f.Calls {
			t.Errorf("%s の呼び出し数が記録と違う: Go %d、記録 %d", f.Name, calls[f.Name], f.Calls)
		}
	}
	checkStiffHits(t)
	checkToneHits(t)
	checkNumberHits(t)
	checkNegationSwapHits(t)
	checkQualifierHits(t)
	checkQuestionHits(t)
	checkFeelingHits(t)
	checkContrastHits(t)
	checkCertaintyHits(t)
	total := 0
	for _, n := range calls {
		total += n
	}
	t.Logf("関数の記録 %d case・呼び出し %d 件を全部比べた", len(cases), total)
}
