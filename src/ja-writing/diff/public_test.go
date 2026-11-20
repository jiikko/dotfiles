package diff

import (
	"testing"

	"ja-writing/internal/pyjson"
	"ja-writing/oracle"
)

func loadsObject(t *testing.T, s string) *pyjson.Object {
	t.Helper()
	v, err := pyjson.Loads(s)
	if err != nil {
		t.Fatal(err)
	}
	return v.(*pyjson.Object)
}

// FailOnCount は本家の diff() が書いた fail_on_meaning_risk_count と同じ数を返す (diff-report.json の dict 全部)。
func TestFailOnCountMatchesRecordedCount(t *testing.T) {
	var g oracle.DiffReportGolden
	if err := oracle.ReadGolden(goldenDir, oracle.DiffReportFile, &g); err != nil {
		t.Fatal(err)
	}
	positive := 0
	for _, c := range g.Cases {
		d := loadsObject(t, string(c.D))
		risks, _ := d.Field("meaning_risks").([]pyjson.Value)
		got, err := FailOnCount(risks)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if want := d.Field("fail_on_meaning_risk_count").(int64); int64(got) != want {
			t.Errorf("%s: FailOnCount = %d (本家 %d)", c.Name, got, want)
		}
		if got > 0 {
			positive++
		}
	}
	if positive < 10 {
		t.Errorf("数えた恐れのある dict が %d 個しかない", positive)
	}
}

// 本家の fail_on_count・_shows_rewritten_only・_hunk_heading の答え (Python 3.15.0 で確かめた値。E は例外)。
func TestPublicHelpersMatchPython(t *testing.T) {
	for _, c := range []struct{ risks, want string }{
		{`[]`, "0"}, {`["x"]`, "E"}, {`[null]`, "E"}, {`[[]]`, "E"}, {`[{}]`, "E"}, {`[{"kind": "number"}]`, "1"},
		{`[{"kind": "unsupported", "subkind": "number"}]`, "1"}, {`[{"kind": "unsupported"}]`, "0"}, {`[{"kind": 3}]`, "0"},
		{`[{"kind": "feeling"}, {"kind": "negation"}]`, "1"}, {`[{"kind": "number"}, 5]`, "E"},
	} {
		v, err := pyjson.Loads(c.risks)
		if err != nil {
			t.Fatal(err)
		}
		n, err := FailOnCount(v.([]pyjson.Value))
		if got := map[bool]string{true: "E", false: string(rune('0' + n))}[err != nil]; got != c.want {
			t.Errorf("FailOnCount(%s) = %s (本家 %s)", c.risks, got, c.want)
		}
	}
	for _, c := range []struct{ d, want string }{
		{`{}`, "E"}, {`{"status": "no_change"}`, "false"}, {`{"status": "changed", "full_diff": {"hunks": [1]}}`, "true"},
		{`{"full_diff": {"hunks": []}}`, "false"}, {`{"full_diff": {}}`, "E"},
	} {
		ok, err := ShowsRewrittenOnly(loadsObject(t, c.d))
		got := map[bool]string{true: "true", false: "false"}[ok]
		if err != nil {
			got = "E"
		}
		if got != c.want {
			t.Errorf("ShowsRewrittenOnly(%s) = %s (本家 %s)", c.d, got, c.want)
		}
	}
	for _, c := range []struct{ h, want string }{
		{`{}`, "E"},
		{`{"old_blocks": [], "new_blocks": [], "tag": "insert", "new_start": 2, "new_end": 3, "notes": []}`, "@ 後2-3行目を追加（）"},
		{`{"old_blocks": ["code"], "new_blocks": ["list"], "tag": "replace", "old_start": 1, "old_end": 1, "new_start": 1, "new_end": 1, "notes": [{"kind": "moved_to", "line": 4}]}`,
			"@ 元1行目 → 後1行目（コード → 箇条書き、移した行。後4行目へ）"},
		{`{"old_blocks": [], "new_blocks": [], "tag": "insert", "new_start": 2, "new_end": 3}`, "E"},
	} {
		got, err := HunkHeading(loadsObject(t, c.h))
		if err != nil {
			got = "E"
		}
		if got != c.want {
			t.Errorf("HunkHeading(%s) = %q (本家 %q)", c.h, got, c.want)
		}
	}
}

// Stances は写しを返す (呼び出し元が書き換えても、次に呼んだときの値と diff の表は変わらない)。
func TestStancesReturnsCopy(t *testing.T) {
	first := Stances()
	want := len(first)
	for k := range first {
		delete(first, k)
	}
	first["x"] = "y"
	again := Stances()
	if len(again) != want || again["x"] != "" {
		t.Errorf("Stances() を書き換えると次の値が変わる: %v", again)
	}
	if want == 0 {
		t.Error("Stances() が空")
	}
}
