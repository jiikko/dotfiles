package diff

import (
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/oracle"
	"ja-writing/oracle/replay"
)

// 群 T の段の記録の突き合わせ。

var feelingHits = map[string]int{}

// checkFeelingHits は群 T の段の記録が、気持ちの語が出る組と何も出ない組のどちらにも十分届いていることを確かめる。
func checkFeelingHits(t *testing.T) {
	t.Helper()
	for _, want := range []string{"feeling:feeling", "feeling:(none)"} {
		if feelingHits[want] < 20 {
			t.Errorf("群 T の記録が %s に届いた回数は %d (20 回以上が要る)", want, feelingHits[want])
		}
	}
	t.Logf("群 T の記録の届き方: %v", feelingHits)
}

// invokeFeeling は群 T の呼び出し 1 つの結果。build は、直前の _build_full_diff の呼び出しの引数。
func invokeFeeling(t *testing.T, c oracle.Call, build []any) pyjson.Value {
	t.Helper()
	if c.Function != "_feeling_risks" {
		t.Fatalf("関数 %s を Go に持っていない", c.Function)
	}
	v, err := FeelingRisks(argString(t, build[0]), argString(t, build[1]))
	if err != nil {
		t.Fatalf("_feeling_risks: %v", err)
	}
	countRiskKinds(v, "feeling:", feelingHits)
	return replay.SortKeys(v)
}
