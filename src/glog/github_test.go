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
			{"__typename":"CheckRun","name":"build","status":"COMPLETED","conclusion":"SUCCESS","detailsUrl":"https://github.com/o/r/runs/1"},
			{"__typename":"StatusContext","context":"ci/legacy","state":"SUCCESS","targetUrl":"https://ci.example.com/42"}]}}},
		"c1": {"statusCheckRollup": null},
		"c2": null
	}}}`
	statuses, details, ghErr := FetchCIStatuses(context.Background(), fakeRunner(fixture, "", nil),
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
	// 展開表示用のジョブ一覧 (CheckRun は name/detailsUrl、StatusContext は context/targetUrl)
	want := []CheckDetail{
		{Name: "build", State: StateSuccess, URL: "https://github.com/o/r/runs/1"},
		{Name: "ci/legacy", State: StateSuccess, URL: "https://ci.example.com/42"},
	}
	if got := details[sha1]; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("details[sha1] = %+v; want %+v", got, want)
	}
	if got := details[sha2]; got == nil || len(got) != 0 {
		t.Errorf("details[sha2] (Check なし) = %+v; want 空スライス", got)
	}
}

func TestFetchCIStatusesEmpty(t *testing.T) {
	called := false
	runner := func(_ context.Context, _ string, _ ...string) ([]byte, []byte, error) {
		called = true
		return nil, nil, nil
	}
	statuses, _, ghErr := FetchCIStatuses(context.Background(), runner, Repo{}, nil)
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
	statuses, _, ghErr := FetchCIStatuses(context.Background(), fakeRunner(fixture, "", nil),
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
	_, _, ghErr := FetchCIStatuses(context.Background(), fakeRunner("not json", "", nil),
		Repo{Owner: "o", Name: "r"}, []string{strings.Repeat("a", 40)})
	if ghErr == nil || ghErr.Kind != GHOther {
		t.Errorf("壊れた JSON は GHOther になるべき: %+v", ghErr)
	}
}

// argsRunner は呼び出し引数に応じて応答を切り替える CommandRunner。
func argsRunner(t *testing.T, responses map[string]string) CommandRunner {
	t.Helper()
	return func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		joined := strings.Join(args, " ")
		for pattern, out := range responses {
			if strings.Contains(joined, pattern) {
				return []byte(out), nil, nil
			}
		}
		t.Fatalf("想定外のコマンド: gh %s", joined)
		return nil, nil, nil
	}
}

func TestFetchJobDetailPrefersAnnotations(t *testing.T) {
	run := argsRunner(t, map[string]string{
		"check-runs/123/annotations": `[
			{"path":"src/a.go","start_line":10,"annotation_level":"failure","message":"undefined: foo\ndetail"}]`,
	})
	lines, ghErr := FetchJobDetail(context.Background(), run, Repo{Owner: "o", Name: "r"},
		CheckDetail{Name: "lint", State: StateFailure, CheckID: 123})
	if ghErr != nil {
		t.Fatalf("ghErr = %v", ghErr)
	}
	want := []string{"[failure] src/a.go:10", "  undefined: foo", "  detail"}
	if len(lines) != 3 || lines[0] != want[0] || lines[1] != want[1] || lines[2] != want[2] {
		t.Errorf("annotations 行 = %v; want %v", lines, want)
	}
}

func TestFetchJobDetailFallsBackToLog(t *testing.T) {
	logOut := "job\tstep\tline one\njob\tstep\tline two\n"
	run := argsRunner(t, map[string]string{
		"annotations": `[]`,
		"run view":    logOut,
	})
	// 失敗 job は --log-failed を使う
	called := false
	wrapped := func(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
		if strings.Contains(strings.Join(args, " "), "--log-failed") {
			called = true
		}
		return run(ctx, name, args...)
	}
	lines, ghErr := FetchJobDetail(context.Background(), wrapped, Repo{Owner: "o", Name: "r"},
		CheckDetail{Name: "test", State: StateFailure, CheckID: 9})
	if ghErr != nil {
		t.Fatalf("ghErr = %v", ghErr)
	}
	if !called {
		t.Errorf("失敗 job で --log-failed が使われていない")
	}
	// job/step のタブプレフィックスは落ちる
	if len(lines) != 2 || lines[0] != "line one" || lines[1] != "line two" {
		t.Errorf("ログ行 = %v", lines)
	}
}

func TestFetchJobDetailNonActions(t *testing.T) {
	// StatusContext (CheckID=0) は取得経路が無い
	lines, ghErr := FetchJobDetail(context.Background(), nil, Repo{},
		CheckDetail{Name: "ci/legacy", State: StateSuccess, CheckID: 0})
	if ghErr != nil || len(lines) != 1 || !strings.Contains(lines[0], "取得できません") {
		t.Errorf("lines = %v, ghErr = %v", lines, ghErr)
	}
}

func TestLogTail(t *testing.T) {
	var b strings.Builder
	for i := range 100 {
		fmt.Fprintf(&b, "j\ts\tline %d\n", i)
	}
	lines := logTail(b.String(), 50)
	if len(lines) != 50 || lines[0] != "line 50" || lines[49] != "line 99" {
		t.Errorf("tail = %d 行, 先頭 %q, 末尾 %q", len(lines), lines[0], lines[len(lines)-1])
	}
	if got := logTail("", 50); len(got) != 0 {
		t.Errorf("空ログ = %v", got)
	}
}

func TestLogTailSanitizesContent(t *testing.T) {
	// メッセージ部のタブは端末のタブ展開で枠の桁計算を壊す (スクロールで視界に入ると
	// 表示崩壊する実測バグ) ため、取り込み時に無害化する
	out := "j\ts\t\ufeffok  \tglog\t0.641s\r\n"
	lines := logTail(out, 50)
	if len(lines) != 1 {
		t.Fatalf("lines = %v", lines)
	}
	if strings.ContainsAny(lines[0], "\t\r\ufeff") {
		t.Errorf("制御文字が残っている: %q", lines[0])
	}
	if !strings.Contains(lines[0], "ok      glog") {
		t.Errorf("タブが空白へ展開されていない: %q", lines[0])
	}
}

func TestLogTailStripsTimestamp(t *testing.T) {
	// 行頭の ISO タイムスタンプ (~29 桁) は幅の浪費なので落とす
	out := "j\ts\t2026-07-16T13:11:31.4381694Z ##[group]Run make test\n" +
		"j\ts\tno timestamp line\n"
	lines := logTail(out, 50)
	if lines[0] != "##[group]Run make test" {
		t.Errorf("タイムスタンプが残っている: %q", lines[0])
	}
	if lines[1] != "no timestamp line" {
		t.Errorf("タイムスタンプ無しの行が変更された: %q", lines[1])
	}
}

func TestSanitizeDetailLine(t *testing.T) {
	if got := sanitizeDetailLine("plain text"); got != "plain text" {
		t.Errorf("素の行が変更された: %q", got)
	}
	// ANSI カラーは残す (枠側の幅計算が対応済み)
	colored := "\x1b[36;1mmake test\x1b[0m"
	if got := sanitizeDetailLine(colored); got != colored {
		t.Errorf("ANSI が落ちた: %q", got)
	}
	if got := sanitizeDetailLine("a\tb\rc\ufeffd"); got != "a    bcd" {
		t.Errorf("sanitize = %q; want %q", got, "a    bcd")
	}
}

func TestFetchCommitPR(t *testing.T) {
	sha := strings.Repeat("a", 40)
	// OPEN > MERGED の優先で選ぶ
	fixture := `{"data":{"repository":{"object":{"associatedPullRequests":{"nodes":[
		{"number":10,"url":"https://github.com/o/r/pull/10","state":"MERGED"},
		{"number":12,"url":"https://github.com/o/r/pull/12","state":"OPEN"}]}}}}}`
	pr, ghErr := FetchCommitPR(context.Background(), fakeRunner(fixture, "", nil), Repo{Owner: "o", Name: "r"}, sha)
	if ghErr != nil || pr == nil || pr.Number != 12 {
		t.Errorf("pr = %+v, ghErr = %v; want OPEN の #12", pr, ghErr)
	}
	// MERGED のみならそれ
	merged := `{"data":{"repository":{"object":{"associatedPullRequests":{"nodes":[
		{"number":10,"url":"https://github.com/o/r/pull/10","state":"MERGED"}]}}}}}`
	pr, _ = FetchCommitPR(context.Background(), fakeRunner(merged, "", nil), Repo{Owner: "o", Name: "r"}, sha)
	if pr == nil || pr.Number != 10 {
		t.Errorf("pr = %+v; want MERGED の #10", pr)
	}
	// PR なし
	none := `{"data":{"repository":{"object":{"associatedPullRequests":{"nodes":[]}}}}}`
	pr, ghErr = FetchCommitPR(context.Background(), fakeRunner(none, "", nil), Repo{Owner: "o", Name: "r"}, sha)
	if pr != nil || ghErr != nil {
		t.Errorf("PR なしで pr = %+v, ghErr = %v", pr, ghErr)
	}
	// 壊れた JSON
	if _, ghErr = FetchCommitPR(context.Background(), fakeRunner("x", "", nil), Repo{Owner: "o", Name: "r"}, sha); ghErr == nil {
		t.Errorf("壊れた JSON がエラーにならない")
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
