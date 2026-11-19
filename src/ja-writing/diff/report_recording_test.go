package diff

import (
	"strconv"
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
	summaryOnly := 0
	for _, c := range g.Cases {
		v, err := pyjson.Loads(string(c.D))
		if err != nil {
			t.Fatalf("%s: dict を読めません: %v", c.Name, err)
		}
		d := v.(*pyjson.Object)
		checkSummaries(t, c, d)
		if c.SummaryOnly {
			summaryOnly++
			continue
		}
		checkParts(t, c, d)
		risks, _ := d.Get("meaning_risks")
		if got := failOnLine(fieldInt(d, "meaning_risk_count"), fieldInt(d, "fail_on_meaning_risk_count"), risks.([]pyjson.Value)); got != c.FailOnLine {
			t.Errorf("%s: 終了コード 1 の行が本家と違う: %q (本家 %q)", c.Name, got, c.FailOnLine)
		}
		got, err := report(d)
		if err != nil {
			t.Errorf("%s: %v", c.Name, err)
			continue
		}
		if got != c.Report {
			t.Errorf("%s: 本家と違う\n--- Go\n%s\n--- 本家\n%s", c.Name, clipLines(got), clipLines(c.Report))
		}
	}
	if summaryOnly < 10 {
		t.Errorf("detector_summary だけの case が %d 件しかない", summaryOnly)
	}
	if len(g.Cases)-summaryOnly < 100 {
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

// checkSummaries は detector_summary を key ごとに本家と比べる (None は nil)。
func checkSummaries(t *testing.T, c oracle.DiffReportCase, d *pyjson.Object) {
	t.Helper()
	if len(c.Summaries) != len(DetectorKeys) {
		t.Fatalf("%s: detector_summary の記録が %d 個 (key は %d 個)", c.Name, len(c.Summaries), len(DetectorKeys))
	}
	for _, key := range DetectorKeys {
		want, ok := c.Summaries[key]
		if !ok {
			t.Fatalf("%s: detector_summary の %s の記録が無い", c.Name, key)
		}
		got := DetectorSummary(d, key)
		if (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Errorf("%s: detector_summary(%s) が本家と違う: %s (本家 %s)", c.Name, key, showPtr(got), showPtr(want))
		}
	}
}

// checkParts は report_parts を本家と比べる (鍵の順も比べる)。
func checkParts(t *testing.T, c oracle.DiffReportCase, d *pyjson.Object) {
	t.Helper()
	got, err := pyjson.Dumps(ReportParts(d), pyjson.Compact())
	if err != nil {
		t.Fatal(err)
	}
	want, err := pyjson.Loads(string(c.Parts))
	if err != nil {
		t.Fatalf("%s: report_parts の記録を読めません: %v", c.Name, err)
	}
	wantText, err := pyjson.Dumps(want, pyjson.Compact())
	if err != nil {
		t.Fatal(err)
	}
	if got != wantText {
		t.Errorf("%s: report_parts が本家と違う\n--- Go\n%s\n--- 本家\n%s", c.Name, clipLines(got), clipLines(wantText))
	}
}

func showPtr(s *string) string {
	if s == nil {
		return "None"
	}
	return strconv.Quote(*s)
}
