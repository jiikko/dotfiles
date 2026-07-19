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
