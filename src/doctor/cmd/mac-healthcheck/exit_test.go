package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"doctor/exitcode"
	"doctor/health"
)

func TestHealthExitCode(t *testing.T) {
	rep := func(ss ...health.Status) health.Report {
		var r health.Report
		for _, s := range ss {
			r.Checks = append(r.Checks, health.Check{Category: health.CatNet, ID: "x", Status: s})
		}
		return r
	}
	for _, tc := range []struct {
		name string
		rep  health.Report
		want int
	}{
		{"ok と情報だけ", rep(health.StatusOK, health.StatusInfo), exitcode.NoFindings},
		{"注意がある", rep(health.StatusOK, health.StatusWarn), exitcode.Findings},
		{"異常がある", rep(health.StatusBad), exitcode.Findings},
		{"判定不能は注意・異常より優先", rep(health.StatusBad, health.StatusUnknown), exitcode.Undiagnosed},
	} {
		if got := healthExitCode(tc.rep); got != tc.want {
			t.Errorf("%s: %d; want %d", tc.name, got, tc.want)
		}
	}
}

// text と -json で同じ終了コードを返し、どちらも無害化の後の値を出す。
func TestEmitSameExitCodeForBothFormats(t *testing.T) {
	rep := health.Report{Checks: []health.Check{{Category: health.CatNet, ID: "x", Label: "l", Value: "v\x1b[31m", Status: health.StatusWarn}}}
	var text, js, errb bytes.Buffer
	rcText := emit(rep, false, &text, &errb)
	rcJSON := emit(rep, true, &js, &errb)
	if rcText != exitcode.Findings || rcJSON != rcText {
		t.Errorf("rc text=%d json=%d", rcText, rcJSON)
	}
	var back health.Report
	if err := json.Unmarshal(js.Bytes(), &back); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text.String(), "\x1b") || strings.Contains(back.Checks[0].Value, "\x1b") {
		t.Errorf("制御文字が残った")
	}
}
