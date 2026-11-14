package diff

import (
	"strings"
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/oracle"
)

// 人が読む出力 (本家の report) を、本家が返した dict と書き換えた dict の記録 (testdata/golden/diff-report.json) と突き合わせる。
// 記録の dict は pyjson.Loads で読み、diffDocument が組む値と同じ型 (*pyjson.Object・[]pyjson.Value・int64・float64) で渡す。
func TestReportMatchesPythonRecording(t *testing.T) {
	var g oracle.DiffReportGolden
	if err := oracle.ReadGolden(goldenDir, oracle.DiffReportFile, &g); err != nil {
		t.Fatal(err)
	}
	for _, c := range g.Cases {
		v, err := pyjson.Loads(string(c.D))
		if err != nil {
			t.Fatalf("%s: dict を読めません: %v", c.Name, err)
		}
		got, err := report(v.(*pyjson.Object))
		if err != nil {
			t.Errorf("%s: %v", c.Name, err)
			continue
		}
		if got != c.Report {
			t.Errorf("%s: 本家と違う\n--- Go\n%s\n--- 本家\n%s", c.Name, clipLines(got), clipLines(c.Report))
		}
	}
	if len(g.Cases) < 100 {
		t.Errorf("case が %d 件しかない", len(g.Cases))
	}
	t.Logf("report の記録 %d case を比べた", len(g.Cases))
}

func clipLines(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) > 40 {
		lines = append(lines[:40], "…")
	}
	return strings.Join(lines, "\n")
}
