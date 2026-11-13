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
	"ja-writing/oracle/replay"
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
			switch {
			case call.Quiet:
				// 覚えるだけの呼び出し (結果の digest は 8 つの 0)。段の呼び出しが同じ引数から作り直す
				switch call.Function {
				case "_build_full_diff":
					build = call.Args
				case "normalize":
					saved[call.Save] = Normalize(argString(t, call.Args[0]))
				}
				if want := rec.Digests[j*oracle.DigestWidth : (j+1)*oracle.DigestWidth]; want != "00000000" {
					t.Errorf("case %s の %d 番目: 覚えるだけの呼び出しの digest が %s", c.Name, j, want)
				}
				continue
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
			canon, digest, err := replay.ResultDigest(got)
			if err != nil {
				t.Fatal(err)
			}
			if want := rec.Digests[j*oracle.DigestWidth : (j+1)*oracle.DigestWidth]; digest != want {
				failed++
				if failed <= 15 {
					msg := fmt.Sprintf("case %s の %d 番目 (%s %.60q): digest %s、記録 %s\n  Go: %s", c.Name, j, call.Function, fmt.Sprint(call.Args), digest, want, clip(canon, 400))
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
