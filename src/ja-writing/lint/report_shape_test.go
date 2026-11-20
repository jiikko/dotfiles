package lint

import (
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/oracle"
	"ja-writing/oracle/reportshape"
)

// 子の --json の形が崩れた dict を render_report・render_findings に通し、本家の記録 (testdata/golden/report-shape.json) と比べる。
// 本家が例外になる dict では error、ならない dict では本家と同じ答え。
func TestRenderShapeMatchesPythonRecording(t *testing.T) {
	var g oracle.ReportShapeGolden
	if err := oracle.ReadGolden(goldenDir, oracle.ReportShapeFile, &g); err != nil {
		t.Fatal(err)
	}
	call := func(i int, d *pyjson.Object) (string, error) {
		if i == 0 {
			return RenderReport(d)
		}
		return RenderFindings(d)
	}
	st, bad, err := reportshape.Check(g, "lint/", 2, call)
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
	if st.Mutations < 1800 || st.Raised < 500 || st.Compared < 3000 {
		t.Errorf("書き換え %d・本家の例外 %d・比べた戻り値 %d (少なすぎる)", st.Mutations, st.Raised, st.Compared)
	}
	t.Logf("書き換え %d・本家の例外 %d・比べた戻り値 %d", st.Mutations, st.Raised, st.Compared)
}
