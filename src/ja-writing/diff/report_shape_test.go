package diff

import (
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/oracle"
	"ja-writing/oracle/reportshape"
)

// 子の --json の形が崩れた dict (欄を 1 つずつ消す・型を変える) を report・report_parts・detector_summary に通し、本家の記録
// (testdata/golden/report-shape.json) と比べる。本家が例外になる dict では error (check はそれを誤りとして出す)、ならない dict では本家と同じ答え。
func TestReportShapeMatchesPythonRecording(t *testing.T) {
	var g oracle.ReportShapeGolden
	if err := oracle.ReadGolden(goldenDir, oracle.ReportShapeFile, &g); err != nil {
		t.Fatal(err)
	}
	call := func(i int, d *pyjson.Object) (string, error) {
		switch i {
		case 0:
			parts, err := ReportParts(d)
			if err != nil {
				return "", err
			}
			return pyjson.Dumps(parts, pyjson.Options{})
		case 1:
			return Report(d)
		}
		s, err := DetectorSummary(d, DetectorKeys[i-2])
		if err != nil || s == nil {
			return "None", err
		}
		return "str:" + *s, nil
	}
	st, bad, err := reportshape.Check(g, "diff/", 2+len(DetectorKeys), call)
	if err != nil {
		t.Fatal(err)
	}
	for i, b := range bad {
		if i == 20 {
			t.Errorf("ほか %d 件", len(bad)-20)
			break
		}
		t.Error(b)
	}
	if st.Mutations < 5000 || st.Raised < 5000 || st.Compared < 5000 {
		t.Errorf("書き換え %d・本家の例外 %d・比べた戻り値 %d (どれも 5000 以上のはず)", st.Mutations, st.Raised, st.Compared)
	}
	t.Logf("書き換え %d・本家の例外 %d・比べた戻り値 %d", st.Mutations, st.Raised, st.Compared)
}
