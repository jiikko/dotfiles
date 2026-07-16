package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func TestParseGitHubURL(t *testing.T) {
	tests := []struct {
		url   string
		owner string
		name  string
		ok    bool
	}{
		{"https://github.com/owner/repo.git", "owner", "repo", true},
		{"https://github.com/owner/repo", "owner", "repo", true},
		{"https://github.com/owner/repo/", "owner", "repo", true},
		{"git@github.com:owner/repo.git", "owner", "repo", true},
		{"git@github.com:owner/repo", "owner", "repo", true},
		{"ssh://git@github.com/owner/repo.git", "owner", "repo", true},
		{"ssh://git@github.com:22/owner/repo.git", "owner", "repo", true},
		{"https://gitlab.com/owner/repo.git", "", "", false},
		{"git@bitbucket.org:owner/repo.git", "", "", false},
		{"", "", "", false},
	}
	for _, tt := range tests {
		repo, ok := ParseGitHubURL(tt.url)
		if ok != tt.ok || repo.Owner != tt.owner || repo.Name != tt.name {
			t.Errorf("ParseGitHubURL(%q) = %+v, %v; want {%s %s}, %v", tt.url, repo, ok, tt.owner, tt.name, tt.ok)
		}
	}
}

func checkRun(status, conclusion string) rollupContext {
	return rollupContext{Typename: "CheckRun", Status: status, Conclusion: conclusion}
}

func statusCtx(state string) rollupContext {
	return rollupContext{Typename: "StatusContext", State: state}
}

func rollupOf(nodes ...rollupContext) *rollupPayload {
	r := &rollupPayload{State: "SUCCESS"}
	r.Contexts.Nodes = nodes
	return r
}

func TestAggregateRollup(t *testing.T) {
	tests := []struct {
		name   string
		rollup *rollupPayload
		want   CIState
	}{
		// issue の集約ルール 1〜5
		{"失敗が1つでもあれば failure", rollupOf(checkRun("COMPLETED", "SUCCESS"), checkRun("COMPLETED", "FAILURE")), StateFailure},
		{"実行中があれば pending", rollupOf(checkRun("COMPLETED", "SUCCESS"), checkRun("IN_PROGRESS", "")), StatePending},
		{"queued も pending", rollupOf(checkRun("QUEUED", "")), StatePending},
		{"全成功なら success", rollupOf(checkRun("COMPLETED", "SUCCESS"), statusCtx("SUCCESS")), StateSuccess},
		{"成功 + skipped 混在は success", rollupOf(checkRun("COMPLETED", "SUCCESS"), checkRun("COMPLETED", "SKIPPED")), StateSuccess},
		{"cancelled/skipped/neutral のみは neutral", rollupOf(checkRun("COMPLETED", "CANCELLED"), checkRun("COMPLETED", "SKIPPED")), StateNeutral},
		{"Check なしは none", nil, StateNone},
		{"contexts 空も none", rollupOf(), StateNone},
		// commit status (StatusContext) 系
		{"StatusContext の failure", rollupOf(statusCtx("FAILURE")), StateFailure},
		{"StatusContext の error も failure", rollupOf(statusCtx("ERROR")), StateFailure},
		{"StatusContext の pending", rollupOf(statusCtx("PENDING")), StatePending},
		// 優先順: failure > pending > success
		{"failure は pending より優先", rollupOf(checkRun("IN_PROGRESS", ""), checkRun("COMPLETED", "FAILURE")), StateFailure},
	}
	for _, tt := range tests {
		if got := aggregateRollup(tt.rollup); got != tt.want {
			t.Errorf("%s: aggregateRollup = %v; want %v", tt.name, got, tt.want)
		}
	}
}

// fakeRunner は fixture を返す CommandRunner。通常テストで外部通信しない (issue のテスト方針)。
func fakeRunner(stdout, stderr string, err error) CommandRunner {
	return func(_ context.Context, _ string, _ ...string) ([]byte, []byte, error) {
		return []byte(stdout), []byte(stderr), err
	}
}

func TestFetchCIStatuses(t *testing.T) {
	sha1 := strings.Repeat("a", 40)
	sha2 := strings.Repeat("b", 40)
	sha3 := strings.Repeat("c", 40)
	fixture := `{"data":{"repository":{
		"c0": {"statusCheckRollup": {"state":"SUCCESS","contexts":{"nodes":[
			{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"}]}}},
		"c1": {"statusCheckRollup": null},
		"c2": null
	}}}`
	statuses, ghErr := FetchCIStatuses(context.Background(), fakeRunner(fixture, "", nil),
		Repo{Owner: "o", Name: "r"}, []string{sha1, sha2, sha3})
	if ghErr != nil {
		t.Fatalf("ghErr = %v", ghErr)
	}
	if statuses[sha1] != StateSuccess {
		t.Errorf("sha1 = %v; want success", statuses[sha1])
	}
	if statuses[sha2] != StateNone {
		t.Errorf("sha2 (rollup null) = %v; want none", statuses[sha2])
	}
	if statuses[sha3] != StateNone {
		t.Errorf("sha3 (GitHub 上に存在しない) = %v; want none", statuses[sha3])
	}
}

func TestFetchCIStatusesEmpty(t *testing.T) {
	called := false
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, []byte, error) {
		called = true
		return nil, nil, nil
	}
	statuses, ghErr := FetchCIStatuses(context.Background(), runner, Repo{}, nil)
	if ghErr != nil || len(statuses) != 0 || called {
		t.Errorf("空 SHA 列で API を呼んではいけない: called=%v", called)
	}
}

func TestFetchCIStatusesPartialErrors(t *testing.T) {
	// GraphQL は data と errors を同時に返しうる。取れた分は使い、失敗は警告として返す
	sha1 := strings.Repeat("a", 40)
	sha2 := strings.Repeat("b", 40)
	fixture := `{"data":{"repository":{
		"c0": {"statusCheckRollup": {"state":"SUCCESS","contexts":{"nodes":[
			{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"}]}}}
	}},"errors":[{"message":"Something went wrong while executing your query."}]}`
	statuses, ghErr := FetchCIStatuses(context.Background(), fakeRunner(fixture, "", nil),
		Repo{Owner: "o", Name: "r"}, []string{sha1, sha2})
	if statuses[sha1] != StateSuccess {
		t.Errorf("部分成功で取れた sha1 = %v; want success", statuses[sha1])
	}
	if _, ok := statuses[sha2]; ok {
		t.Errorf("欠落 SHA に状態が入っている: %v", statuses[sha2])
	}
	if ghErr == nil || !strings.Contains(ghErr.Detail, "Something went wrong") {
		t.Errorf("partial errors が警告になっていない: %+v", ghErr)
	}
}

func TestFetchCIStatusesBrokenJSON(t *testing.T) {
	_, ghErr := FetchCIStatuses(context.Background(), fakeRunner("not json", "", nil),
		Repo{Owner: "o", Name: "r"}, []string{strings.Repeat("a", 40)})
	if ghErr == nil || ghErr.Kind != GHOther {
		t.Errorf("壊れた JSON は GHOther になるべき: %+v", ghErr)
	}
}

func TestClassifyGHError(t *testing.T) {
	exitErr := errors.New("exit status 1")
	tests := []struct {
		name   string
		err    error
		stderr string
		want   GHErrorKind
	}{
		{"gh 未インストール", &exec.Error{Name: "gh", Err: exec.ErrNotFound}, "", GHNotInstalled},
		{"未認証", exitErr, "To get started with GitHub CLI, please run:  gh auth login", GHNotAuthenticated},
		{"未認証 (not logged in 系文言)", exitErr, "You are not logged into any GitHub hosts.", GHNotAuthenticated},
		{"rate limit", exitErr, "API rate limit exceeded for user", GHRateLimited},
		{"その他", exitErr, "something went wrong", GHOther},
		{"stderr 空はエラー文字列を使う", exitErr, "", GHOther},
	}
	for _, tt := range tests {
		got := classifyGHError(tt.err, tt.stderr)
		if got.Kind != tt.want {
			t.Errorf("%s: Kind = %v; want %v", tt.name, got.Kind, tt.want)
		}
		if got.Warning() == "" {
			t.Errorf("%s: Warning が空", tt.name)
		}
	}
}

func TestBuildStatusQueryCapsAndAliases(t *testing.T) {
	shas := make([]string, 3)
	for i := range shas {
		shas[i] = strings.Repeat(strconv.Itoa(i), 40)
	}
	q := buildStatusQuery(shas)
	for i, sha := range shas {
		if !strings.Contains(q, fmt.Sprintf("c%d: object(oid: %q)", i, sha)) {
			t.Errorf("query に alias c%d がありません:\n%s", i, q)
		}
	}
	if !strings.Contains(q, "statusCheckRollup") {
		t.Errorf("query に statusCheckRollup がありません")
	}
}
