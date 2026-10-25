package main

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const unrelated = `struct U {
    func total(_ xs: [Int]) -> Int {
        var s = 0
        for x in xs where x > 0 { s += x * 2 }
        return s
    }
}
`

// 本体が局所の名前を参照しない 2 つ。値の位置 (リテラルと静的な値) だけが違い、L3 でだけ一致する。
const foldedA = `struct FA {
    func make() -> Config {
        Config(name: "a", retries: 3, timeout: Duration.seconds(10), verbose: true)
    }
}
`

const foldedB = `struct FB {
    func build() -> Config {
        Config(name: Names.b, retries: Limits.max, timeout: Duration.seconds(Limits.wait), verbose: Flags.on)
    }
}
`

func near(t *testing.T, _ string, args ...string) (int, string, string) {
	t.Helper()
	return runReport(t, append([]string{"near"}, args...)...)
}

func TestNearListsTheCopyButNotTheTarget(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA, "Sources/B.swift": helperB, "Sources/U.swift": unrelated})
	rc, out, errs := near(t, root, filepath.Join(root, "Sources/A.swift")+":location")
	if rc != 0 || !strings.Contains(out, "近いもの 1") || !strings.Contains(out, "L1 Sources/B.swift:2 B.makeLocation") || strings.Count(out, "Sources/A.swift") != 1 {
		t.Errorf("B の写しだけを L1 で期待 (指した A は 1 行目だけ): rc=%d out=%q err=%q", rc, out, errs)
	}
	if rc, out, _ := near(t, root, filepath.Join(root, "Sources/U.swift")+":total"); rc != 0 || !strings.Contains(out, "近いものはない") {
		t.Errorf("無関係な関数は近いものはない: rc=%d out=%q", rc, out)
	}
}

// near と report は同じ比べ方 (Eligible と段のハッシュ) を通るので、report の組のメンバーは互いに near に出る。
func TestNearAgreesWithReportGroups(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA, "Sources/B.swift": helperB, "Sources/C.swift": helperC, "Sources/U.swift": unrelated})
	_, out, _ := runReport(t, "-format", "json", root)
	var s struct {
		Groups []struct {
			Level   string
			Members []struct {
				File string
				Line int
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil || len(s.Groups) == 0 {
		t.Fatalf("report の組が無い: %v %s", err, out)
	}
	for _, g := range s.Groups {
		for _, m := range g.Members {
			_, nout, _ := near(t, root, "-format", "json", filepath.Join(root, m.File)+":"+strconv.Itoa(m.Line))
			var n struct {
				Matches []struct {
					Level string
					Unit  struct {
						File string
						Line int
					}
				}
			}
			if err := json.Unmarshal([]byte(nout), &n); err != nil {
				t.Fatal(err)
			}
			got := map[string]string{}
			for _, x := range n.Matches {
				got[x.Unit.File+":"+strconv.Itoa(x.Unit.Line)] = x.Level
			}
			for _, o := range g.Members {
				k := o.File + ":" + strconv.Itoa(o.Line)
				if o == m {
					if _, ok := got[k]; ok {
						t.Errorf("指した単位 %s が near に出た", k)
					}
					continue
				}
				if lv, ok := got[k]; !ok || lv > g.Level {
					t.Errorf("report の %s の組の %s が、%s の near に %s 以下で出ない (%q)", g.Level, k, m.File, g.Level, nout)
				}
			}
		}
	}
}

func TestNearSeparatesFoldedMatches(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/FA.swift": foldedA, "Sources/FB.swift": foldedB})
	rc, out, errs := near(t, root, "-levels", "L1,L2,L3", filepath.Join(root, "Sources/FA.swift")+":make")
	if rc != 0 || !strings.Contains(out, "L3 (局所の名前を参照しない単位どうしの一致) Sources/FB.swift") || !strings.Contains(out, "L3 は比べていない") {
		t.Errorf("畳まれた一致を中身を見た一致と分けて出すことを期待: rc=%d out=%q err=%q", rc, out, errs)
	}
	if _, out, _ := runReport(t, "-levels", "L1,L2,L3", root); !strings.Contains(out, "重複なし") {
		t.Errorf("畳まれた一致は report の組にはしない: %q", out)
	}
}

func TestNearAppliesGroupExclusion(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA, "Sources/B.swift": helperB})
	writeConfig(t, root, groupRule(t, root))
	if rc, out, _ := near(t, root, filepath.Join(root, "Sources/A.swift")+":location"); rc != 0 || !strings.Contains(out, "近いものはない・除外で外した 1") {
		t.Errorf("組の除外で外した一致は数だけ出す: rc=%d out=%q", rc, out)
	}
	if _, out, _ := near(t, root, "-no-exclude", filepath.Join(root, "Sources/A.swift")+":location"); !strings.Contains(out, "近いもの 1") {
		t.Errorf("-no-exclude で一致が出ない: %q", out)
	}
}

func TestPointSelection(t *testing.T) {
	src := `struct P {
    func f(_ a: Int) -> Int { a + 1 }
    func f(_ a: String) -> String { a }
    func g() {
        run { x in other { y in use(x, y) } }
    }
}
`
	root := repoWith(t, map[string]string{"Sources/P.swift": src})
	file := filepath.Join(root, "Sources/P.swift")
	if rc, _, errs := near(t, root, file+":f"); rc != 2 || !strings.Contains(errs, "2 つある") || !strings.Contains(errs, "Sources/P.swift:3 f(_ a: String)") {
		t.Errorf("同名の 2 つは候補を並べて rc=2: rc=%d err=%q", rc, errs)
	}
	if _, out, _ := runReport(t, "explain", file+":3"); !strings.Contains(out, "P.f(_ a: String) -> String") {
		t.Errorf("行で指すとその行の単位: %q", out)
	}
	if _, out, _ := runReport(t, "explain", "-format", "json", file+":5"); !strings.Contains(out, `"col": 13`) {
		t.Errorf("同じ行で始まるクロージャは左 (外側) を選ぶ: %q", out)
	}
	if rc, _, errs := near(t, root, file+":nothing"); rc != 2 || !strings.Contains(errs, "nothing という単位が無い") {
		t.Errorf("無い名前は rc=2: rc=%d err=%q", rc, errs)
	}
}

func TestExplainShowsFormsAndSkipReason(t *testing.T) {
	root := repoWith(t, map[string]string{"Sources/A.swift": helperA, "Sources/FA.swift": foldedA})
	_, out, errs := runReport(t, "explain", filepath.Join(root, "Sources/A.swift")+":location")
	if !strings.Contains(out, "L1 ") || !strings.Contains(out, "(ret \"StorageLocation\")") || strings.Contains(out, "比べない") {
		t.Errorf("段ごとの形を出し、比べる単位に理由は出さない: %q err=%q", out, errs)
	}
	if _, out, _ := runReport(t, "explain", "-min-nodes", "500", filepath.Join(root, "Sources/A.swift")+":location"); !strings.Contains(out, "下限 500 より少ない") {
		t.Errorf("下限で比べない理由が出ない: %q", out)
	}
	if _, out, _ := runReport(t, "explain", filepath.Join(root, "Sources/FA.swift")+":make"); !strings.Contains(out, "(比べない: 本体が局所の名前を参照しないので") {
		t.Errorf("L3 で畳まれる理由が出ない: %q", out)
	}
}

// L3 は値の位置を畳むので、節の数の違う単位どうしも同じハッシュになる。下限より小さい単位を指したら、相手が大きくても比べない
// (report・gate も比べないので、near だけが組にしない一致を出さない)。
func TestNearOnSmallUnitComparesNothing(t *testing.T) {
	root := repoWith(t, map[string]string{
		"Sources/A.swift": "struct A {\n    func a(_ x: Int) -> Int { x + 1 }\n}\n",
		"Sources/B.swift": "struct B {\n    func b(_ y: Int) -> Int { y + Limits.config.maximum.value }\n}\n",
	})
	rc, out, errs := near(t, root, "-levels", "L1,L2,L3", "-min-nodes", "10", filepath.Join(root, "Sources/A.swift")+":a")
	if rc != 0 || !strings.Contains(out, "近いものはない") || !strings.Contains(out, "L3 は比べていない: 節が 5 で下限 10 より少ない") {
		t.Errorf("下限より小さい単位を指すと比べず、理由を出す: rc=%d out=%q err=%q", rc, out, errs)
	}
	if _, out, _ := near(t, root, "-levels", "L1,L2,L3", "-min-nodes", "5", filepath.Join(root, "Sources/A.swift")+":a"); !strings.Contains(out, "L3 Sources/B.swift:2") {
		t.Errorf("下限を下げると L3 で一致する (入力の前提): %q", out)
	}
}
