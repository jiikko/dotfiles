package diff

import (
	"reflect"
	"testing"
)

// 公開の入口は本家の normalize_doc_type を通す: 大文字・前後の空白は article と同じ答え、使えない値は ValueError。
func TestEntryPointsNormalizeDocType(t *testing.T) {
	const old, new = "原因は場合がある。\n", "原因は必ずある。\n"
	for _, name := range []string{"MeaningRisks", "AdditionRisks", "HedgesSection"} {
		call := func(docType string) (any, error) {
			switch name {
			case "MeaningRisks":
				return MeaningRisks(old, new, false, docType)
			case "AdditionRisks":
				return AdditionRisks(old, new, docType)
			}
			return HedgesSection(old, new, docType)
		}
		want, err := call("article")
		if err != nil {
			t.Fatalf("%s(article): %v", name, err)
		}
		for _, spelling := range []string{"ARTICLE", " article ", "Ａrticle"} {
			got, err := call(spelling)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("%s(%q) は article と同じ答えのはず: err=%v", name, spelling, err)
			}
		}
		if _, err := call("bogus"); err == nil {
			t.Errorf("%s(bogus) は ValueError のはず", name)
		}
	}
	none, _ := MeaningRisks(old, new, false, "")
	article, _ := MeaningRisks(old, new, false, "article")
	if reflect.DeepEqual(none, article) {
		t.Error("指定なしと article で答えが違わない入力では、上の比べは空振りする")
	}
}
