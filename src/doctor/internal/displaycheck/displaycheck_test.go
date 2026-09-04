package displaycheck

import (
	"strings"
	"testing"
)

// 🚨 **検査器そのものを検査する。** 各 package の呼び出し (disk / svc / docker) は
// 「今の実装に違反が無い」ことしか示さず、**検査器が違反を見逃すようになった**ことは
// 検出しない。実測 2026-09-04: 未使用 exempt の検出を外す変異が、3 package すべて緑で通った。
//
// fixture は testdata/ に置く (Go のツールチェインが無視するディレクトリなので、
// わざと壊した package を置いても本体のビルドに混ざらない)。

func goodSpec() Spec {
	return Spec{
		Dir: "testdata/good", Package: "good",
		Gates: map[string]Gate{
			"Report": {Func: "SanitizeForDisplay", Recv: "out"},
			"Item":   {Func: "SanitizeForDisplay", Recv: "it"},
		},
		Exempt: map[string]string{
			"Report.Ident": "同一性を持つ値 (fixture)",
			"Item.Name":    "同一性を持つ値 (fixture)",
			"Item.Kind":    "内部生成の enum (fixture)",
		},
		WantChecked:         3, // Report.Free / Report.Lines / Item.Detail
		MinNamedStringTypes: 1,
	}
}

func problemsOf(t *testing.T, s Spec) []string {
	t.Helper()
	problems, fatal := check(s)
	if fatal != "" {
		t.Fatalf("検査できなかった: %s", fatal)
	}
	return problems
}

func wantOneProblem(t *testing.T, s Spec, substr string) {
	t.Helper()
	problems := problemsOf(t, s)
	for _, p := range problems {
		if strings.Contains(p, substr) {
			return
		}
	}
	t.Fatalf("%q を含む指摘が出ていない: %v", substr, problems)
}

func TestCleanPackageHasNoProblems(t *testing.T) {
	if problems := problemsOf(t, goodSpec()); len(problems) != 0 {
		t.Fatalf("違反の無い fixture で指摘が出た: %v", problems)
	}
}

// 関門を通っていないフィールドを見つける (この検査の本題)。
func TestUnsanitizedFieldIsReported(t *testing.T) {
	s := Spec{
		Dir: "testdata/bad", Package: "bad",
		Gates:       map[string]Gate{"Report": {Func: "SanitizeForDisplay", Recv: "out"}},
		Exempt:      map[string]string{"Report.Kind": "内部生成の enum (fixture)"},
		WantChecked: 2, MinNamedStringTypes: 1,
	}
	wantOneProblem(t, s, "Report.Missed")
}

// 🚨 **未使用の exempt を違反にする**。exempt は「検査対象に在るが免除する」宣言なので、
// 使われない = そのフィールドが検査から外れた印。抽出の射程が縮んだときの検出手段。
func TestUnusedExemptIsReported(t *testing.T) {
	s := goodSpec()
	s.Exempt["Report.NoSuchField"] = "存在しないフィールド"
	wantOneProblem(t, s, "Report.NoSuchField")
}

// 理由の書かれていない exempt を違反にする (「なぜ免除してよいか」が残らない)。
func TestExemptWithoutReasonIsReported(t *testing.T) {
	s := goodSpec()
	s.Exempt["Report.Free"] = ""
	s.WantChecked = 2 // Free が exempt に移るので 1 件減る
	wantOneProblem(t, s, "理由が書かれていない")
}

// 射程が変わったら赤にする (フィールドを足した / 抽出が壊れた、のどちらでも)。
func TestWantCheckedMismatchIsReported(t *testing.T) {
	s := goodSpec()
	s.WantChecked = 99
	wantOneProblem(t, s, "射程が変わっている")
}

// rename に追従していない表 (型 / 関門が見つからない) を赤にする。
func TestMissingTypeOrGateIsReported(t *testing.T) {
	s := goodSpec()
	s.Gates["NoSuchType"] = Gate{Func: "SanitizeForDisplay", Recv: "out"}
	wantOneProblem(t, s, "構造体が見つからない")

	s2 := goodSpec()
	s2.Gates["Report"] = Gate{Func: "NoSuchGate", Recv: "out"}
	wantOneProblem(t, s2, "関門 NoSuchGate が見つからない")
}

// 🚨 **抽出できなかったものを緑にしない** (fail-closed)。
func TestExtractionCanaryIsFatal(t *testing.T) {
	s := goodSpec()
	s.MinNamedStringTypes = 99
	if _, fatal := check(s); fatal == "" {
		t.Fatal("射程が縮んでも fatal にならない")
	}
	s2 := goodSpec()
	s2.Package = "nosuchpackage"
	if _, fatal := check(s2); fatal == "" {
		t.Fatal("package が見つからなくても fatal にならない")
	}
}
