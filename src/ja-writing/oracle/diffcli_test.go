package oracle

import (
	"slices"
	"testing"
)

// 検出の種類ごとに、記録の --json の実行に「出る入力」と「出ない入力」がある (空振りの検出も、出しすぎる検出も、CLI の記録が緑のままにしない)。
func TestDiffRecordingHasHitAndMissForEveryKind(t *testing.T) {
	var g LintCLIGolden
	if err := ReadGolden(goldenDir, DiffCLIFile, &g); err != nil {
		t.Fatal(err)
	}
	hit, miss := map[string]int{}, map[string]int{}
	json := 0
	for _, c := range g.Cases {
		if c.Kinds == nil {
			continue
		}
		json++
		for _, k := range DiffDetectionKinds {
			if slices.Contains(c.Kinds, k) {
				hit[k]++
			} else {
				miss[k]++
			}
		}
	}
	if json < 1000 {
		t.Fatalf("--json の記録が %d 件しかない", json)
	}
	for _, k := range DiffDetectionKinds {
		if hit[k] == 0 || miss[k] == 0 {
			t.Errorf("検出の種類 %s の記録: 出る入力 %d 件・出ない入力 %d 件 (どちらかが 0)", k, hit[k], miss[k])
		}
	}
}

// 手で作った入力 (testdata/diff/) は、全部が CLI の記録の case になっている (置いただけで使われない入力を作らない)。
func TestDiffInputsAreAllUsedByCases(t *testing.T) {
	c, err := LoadCorpus(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := LoadDiffInputs("../testdata/diff")
	if err != nil {
		t.Fatal(err)
	}
	used := map[string]bool{}
	for _, cs := range DiffCLICases(c, inputs, []string{"mail"}, []string{"expert"}) {
		used[cs.Name] = true
	}
	for _, in := range inputs {
		if !used["input/"+in.Name+"|json"] {
			t.Errorf("入力 %s が CLI の記録の case に無い", in.Name)
		}
	}
	if len(DiffCorpusPairs(c)) < 70 {
		t.Errorf("入力の集まりから作る直す前と後の組が %d 組 (70 組以上のはず)", len(DiffCorpusPairs(c)))
	}
}
