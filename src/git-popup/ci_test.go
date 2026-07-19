package main

import (
	"strings"
	"testing"
)

func TestBuildCIQuery(t *testing.T) {
	query := buildCIQuery("owner", "repo", []string{"a" + strings.Repeat("0", 39), "b" + strings.Repeat("1", 39)})
	for i, sha := range []string{"a" + strings.Repeat("0", 39), "b" + strings.Repeat("1", 39)} {
		if !strings.Contains(query, "c"+string(rune('0'+i))+":object(oid:\""+sha+"\")") {
			t.Fatalf("query missing alias/sha %d: %s", i, query)
		}
	}
	if !strings.HasPrefix(query, "query($owner:String!,$name:String!){repository(owner:$owner,name:$name){") || !strings.HasSuffix(query, "}}") {
		t.Fatalf("query envelope = %s", query)
	}
	// alias ブロック間は空白必須 (無いと `...}}}c1:` で GraphQL が malformed になる)。
	// 部分文字列一致だけだと空白を消しても通るため、境界そのものを検証する。
	if !strings.Contains(query, "}}} c1:object(oid:") {
		t.Fatalf("alias 境界に空白が無い: %s", query)
	}
}

func TestParseCIResponse(t *testing.T) {
	data := []byte(`{"data":{"repository":{"c0":{"statusCheckRollup":{"state":"SUCCESS"}},"c1":{"statusCheckRollup":{"state":"ERROR"}},"c2":{"statusCheckRollup":{"state":"EXPECTED"}},"c3":{"statusCheckRollup":null},"c4":{}}}}`)
	shas := []string{"s0", "s1", "s2", "s3", "s4"}
	got, err := parseCIResponse(data, shas)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]CIState{"s0": CISuccess, "s1": CIFailure, "s2": CIPending, "s3": CINone, "s4": CINone}
	for sha, state := range want {
		if got[sha] != state {
			t.Errorf("%s = %v, want %v", sha, got[sha], state)
		}
	}
}

func TestParseCIResponseErrors(t *testing.T) {
	// GraphQL は HTTP 成功でも errors を返しうる → エラー扱い (失敗をキャッシュしないため)
	if _, err := parseCIResponse([]byte(`{"errors":[{"message":"Bad credentials"}]}`), []string{"s0"}); err == nil {
		t.Error("errors 入り応答をエラーにしていない")
	}
	// data.repository 欠落もエラー
	if _, err := parseCIResponse([]byte(`{"data":{"repository":null}}`), []string{"s0"}); err == nil {
		t.Error("repository 欠落をエラーにしていない")
	}
}

func TestClassifyCIState(t *testing.T) {
	tests := map[string]CIState{"SUCCESS": CISuccess, "FAILURE": CIFailure, "ERROR": CIFailure, "PENDING": CIPending, "EXPECTED": CIPending, "": CINone, "UNKNOWN": CINone}
	for input, want := range tests {
		if got := classifyCIState(input); got != want {
			t.Errorf("%q = %v, want %v", input, got, want)
		}
	}
}

func TestClassifyCIJob(t *testing.T) {
	tests := map[string]rune{
		"success": ciJobSuccess, "failure": ciJobFailure, "cancelled": ciJobFailure,
		"timed_out": ciJobFailure, "action_required": ciJobFailure, "startup_failure": ciJobFailure,
		"skipped": ciJobSkipped, "neutral": ciJobSkipped, "in_progress": ciJobRunning,
		"queued": ciJobRunning, "pending": ciJobRunning, "unknown": ciJobRunning,
	}
	for input, want := range tests {
		if got := classifyCIJob(input); got != want {
			t.Errorf("%q = %c, want %c", input, got, want)
		}
	}
}

func TestParseCIJobs(t *testing.T) {
	jobs := parseCIJobs("success\tbuild\thttps://x/1\nfailure\ttest\t\nin_progress\tdeploy\thttps://x/3\n")
	if len(jobs) != 3 || jobs[0] != (CIJob{State: "success", Name: "build", URL: "https://x/1"}) {
		t.Fatalf("parseCIJobs = %#v", jobs)
	}
	if jobs[1].URL != "" { // URL 空も許容
		t.Errorf("empty URL not preserved: %#v", jobs[1])
	}
	// 旧 2 フィールド形式の cache も defensive に URL 空で読める
	old := parseCIJobs("success\tbuild\n")
	if len(old) != 1 || old[0].URL != "" {
		t.Errorf("2-field fallback failed: %#v", old)
	}
	if parseCIJobs("") != nil || parseCIJobs("\n") != nil {
		t.Errorf("empty input should be nil")
	}
}

func TestRenderCIJobs(t *testing.T) {
	jobs := []CIJob{{State: "success", Name: "build"}, {State: "failure", Name: "test"}, {State: "in_progress", Name: "deploy"}, {State: "skipped", Name: "lint"}}
	lines := renderCIJobs(jobs, -1)
	joined := stripANSI(strings.Join(lines, "\n"))
	for _, want := range []string{"── CI ──", "✓ build", "✗ test", "● deploy", "○ lint", "──────────"} {
		if !strings.Contains(joined, want) {
			t.Errorf("rendered jobs missing %q: %q", want, joined)
		}
	}
	// selected=1 でその行にカーソル ▌
	sel := renderCIJobs(jobs, 1)
	if !strings.Contains(sel[2], "▌") { // [0]=ヘッダ [1]=build [2]=test
		t.Errorf("selected job not highlighted: %q", sel[2])
	}
	if renderCIJobs(nil, -1) != nil {
		t.Errorf("empty jobs should render nil")
	}
}
