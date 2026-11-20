package diff

import (
	"slices"
	"testing"

	"ja-writing/oracle"
)

func loadGround(t *testing.T) (oracle.DiffGroundGolden, []oracle.VisCase) {
	t.Helper()
	var g oracle.DiffGroundGolden
	if err := oracle.ReadGolden(goldenDir, oracle.DiffGroundFile, &g); err != nil {
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
	cases, err := oracle.DiffGroundCases(corpus, diffDir, inputs)
	if err != nil {
		t.Fatal(err)
	}
	return g, cases
}

// 空振りを止める: 記録の対象の関数と定数が全部記録にあり、例外の経路 (照合・事実の穴・数値の取り出し) も記録にある。
func TestGroundRecordingCoversEveryFunction(t *testing.T) {
	g, _ := loadGround(t)
	byName := map[string]oracle.VisFunctionRecord{}
	for _, f := range g.Functions {
		byName[f.Name] = f
	}
	for _, name := range append(slices.Clone(oracle.DiffGroundFunctions), oracle.DiffGroundConstants...) {
		if byName[name].Calls == 0 {
			t.Errorf("%s の記録が 0 件 (空振り)", name)
		}
	}
	for _, name := range []string{"ground", "fact_gaps", "extract_numbers"} {
		if byName[name].Raised == 0 {
			t.Errorf("%s の例外の記録が無い", name)
		}
	}
	for _, f := range g.Functions {
		if !slices.Contains(oracle.DiffGroundFunctions, f.Name) && !slices.Contains(oracle.DiffGroundConstants, f.Name) {
			t.Errorf("記録に一覧にない名前 %s がある", f.Name)
		}
	}
}
