package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
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
	batch, ghErr := FetchCIStatuses(context.Background(), fakeRunner(fixture, "", nil),
		Repo{Owner: "o", Name: "r"}, []string{sha1, sha2, sha3})
	statuses, details := batch.Statuses, batch.Details
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
	batch, ghErr := FetchCIStatuses(context.Background(), runner, Repo{}, nil)
	if ghErr != nil || len(batch.Statuses) != 0 || called {
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
	batch, ghErr := FetchCIStatuses(context.Background(), fakeRunner(fixture, "", nil),
		Repo{Owner: "o", Name: "r"}, []string{sha1, sha2})
	statuses := batch.Statuses
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

// chunkSHAs: 少件数は 1 本のまま / 多件数は fetchConcurrency で打ち止め / 全 SHA を
// 重複なく被覆し / 端数を配って最大チャンク (= wall time) を最小に保つ。
func TestChunkSHAs(t *testing.T) {
	shas := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = strconv.Itoa(i)
		}
		return out
	}
	for _, n := range []int{1, 2, minChunkSHAs} {
		if got := chunkSHAs(shas(n)); len(got) != 1 {
			t.Errorf("n=%d のチャンク数 = %d, want 1 (往復を増やすに値しない粒)", n, len(got))
		}
	}
	for _, n := range []int{9, 20, 50, 100} {
		got := chunkSHAs(shas(n))
		if len(got) > fetchConcurrency {
			t.Errorf("n=%d のチャンク数 = %d > 並列度上限 %d", n, len(got), fetchConcurrency)
		}
		// 被覆: 連結すると元の列に一致する (欠落も重複も順序入れ替えもない)
		var flat []string
		lo, hi := len(got[0]), 0
		for _, c := range got {
			flat = append(flat, c...)
			lo, hi = min(lo, len(c)), max(hi, len(c))
		}
		if !slices.Equal(flat, shas(n)) {
			t.Errorf("n=%d で SHA の被覆が壊れた: %v", n, flat)
		}
		if hi-lo > 1 {
			t.Errorf("n=%d のチャンクサイズが偏っている (最小 %d / 最大 %d)", n, lo, hi)
		}
	}
}

// 件数が多いときは並列チャンクへ割って投げ、結果をマージして 1 つの CIBatch にする。
func TestFetchCIStatusesChunksInParallel(t *testing.T) {
	const n = 20
	shas := make([]string, n)
	for i := range shas {
		shas[i] = fmt.Sprintf("%040d", i)
	}
	var (
		mu       sync.Mutex
		inFlight int
		maxSeen  int
		queries  int
	)
	run := func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		mu.Lock()
		inFlight++
		maxSeen = max(maxSeen, inFlight)
		queries++
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		// このチャンクに含まれる alias の数だけ SUCCESS を返す
		joined := strings.Join(args, " ")
		var b strings.Builder
		b.WriteString(`{"data":{"repository":{`)
		for i := 0; strings.Contains(joined, fmt.Sprintf("c%d: object", i)); i++ {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `"c%d": {"statusCheckRollup": {"state":"SUCCESS","contexts":{"nodes":[`+
				`{"__typename":"CheckRun","name":"build","status":"COMPLETED","conclusion":"SUCCESS"}]}}}`, i)
		}
		b.WriteString(`}}}`)
		return []byte(b.String()), nil, nil
	}
	batch, ghErr := FetchCIStatuses(context.Background(), run, Repo{Owner: "o", Name: "r"}, shas)
	if ghErr != nil {
		t.Fatalf("ghErr = %v", ghErr)
	}
	if queries < 2 {
		t.Fatalf("リクエスト数 = %d; 20 件は分割されるべき", queries)
	}
	if maxSeen < 2 {
		t.Errorf("同時実行数の最大 = %d; チャンクが直列に走っている", maxSeen)
	}
	// 全 SHA がマージ後の Statuses に揃っている
	if len(batch.Statuses) != n {
		t.Errorf("マージ後の Statuses = %d 件, want %d", len(batch.Statuses), n)
	}
	for i, sha := range shas {
		if batch.Statuses[sha] != StateSuccess {
			t.Errorf("SHA[%d] が欠けている / 状態違い: %q", i, batch.Statuses[sha])
		}
	}
}

// 1 チャンクが失敗しても、成功したチャンクの結果は捨てない (取れた分 + GHError を返す)。
// 呼び出し側の fillUnknownFetched が欠けた SHA を unknown に埋めるので、失敗チャンクの
// SHA だけが `?` に落ちる。
func TestFetchCIStatusesChunkPartialFailure(t *testing.T) {
	const n = 20
	shas := make([]string, n)
	for i := range shas {
		shas[i] = fmt.Sprintf("%040d", i)
	}
	// 先頭 SHA を含むチャンクだけ失敗させる
	first := shas[0]
	run := func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, first) {
			return nil, []byte("gh: api error"), errors.New("exit status 1")
		}
		var b strings.Builder
		b.WriteString(`{"data":{"repository":{`)
		for i := 0; strings.Contains(joined, fmt.Sprintf("c%d: object", i)); i++ {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `"c%d": {"statusCheckRollup": {"state":"SUCCESS","contexts":{"nodes":[`+
				`{"__typename":"CheckRun","name":"build","status":"COMPLETED","conclusion":"SUCCESS"}]}}}`, i)
		}
		b.WriteString(`}}}`)
		return []byte(b.String()), nil, nil
	}
	batch, ghErr := FetchCIStatuses(context.Background(), run, Repo{Owner: "o", Name: "r"}, shas)
	if ghErr == nil {
		t.Fatal("失敗チャンクの GHError が返っていない")
	}
	if len(batch.Statuses) == 0 {
		t.Fatal("成功チャンクの結果まで捨てている")
	}
	if _, ok := batch.Statuses[first]; ok {
		t.Errorf("失敗チャンクの SHA が Statuses に入っている (unknown 埋めの対象から漏れる)")
	}
	if len(batch.Statuses) >= n {
		t.Errorf("失敗チャンクぶんが欠けていない: %d 件", len(batch.Statuses))
	}
}

// argsRunner は呼び出し引数に応じて応答を切り替える CommandRunner。
//
// ⚠️ この runner の中で t.Fatal* を呼んではいけない (Errorf を使う)。CommandRunner は
// FetchJobDetail / FetchCIStatuses が goroutine から呼ぶので、テスト goroutine 以外での
// FailNow は runtime.Goexit でその goroutine だけを終わらせ、「即座に停止」の契約が黙って
// 破棄される — テストは FetchJobDetail から先へ進み、欠けたデータで後続 assert が落ちて
// 本当の失敗理由から遠い 2 つ目の失敗 (や nil 参照 panic) を生む。
//
// 実測 (2026-07-25): 現状は Goexit でも defer 経由の wg.Done が走るためハングはしない
// (以前「ハングする」と書いたのは誤り)。ただしそれは偶然で、deferred Done を持たない形
// (channel の送受信で待つ等) に変えた瞬間に deadlock になる。潜在的な足場として塞いでおく。
//
// govet の testinggoroutine はこれを検出できない (production 関数に渡したクロージャの先の
// goroutine までは追えない。この違反が入った状態で lint が 0 issues だったことで確認済み)
// ので、実装では強制できない制約としてここに書いておく。
//
// 想定外コマンドは Errorf で失敗を記録しつつ error も返す: nil,nil,nil を返すと呼び出し側は
// 空応答を正常として進み、原因から遠い場所で崩れる。
func argsRunner(t *testing.T, responses map[string]string) CommandRunner {
	t.Helper()
	return func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		joined := strings.Join(args, " ")
		if pattern, ok := longestMatch(joined, slices.Collect(maps.Keys(responses))); ok {
			return []byte(responses[pattern]), nil, nil
		}
		t.Errorf("想定外のコマンド: gh %s", joined)
		return nil, nil, fmt.Errorf("想定外のコマンド: gh %s", joined)
	}
}

func TestFetchJobDetailPrefersAnnotations(t *testing.T) {
	run := argsRunner(t, map[string]string{
		"actions/jobs/123": `{"steps":[
			{"name":"Set up job","status":"completed","conclusion":"success","started_at":"2026-07-17T00:00:00Z","completed_at":"2026-07-17T00:00:02Z"},
			{"name":"Run lint","status":"completed","conclusion":"failure","started_at":"2026-07-17T00:00:02Z","completed_at":"2026-07-17T00:02:41Z"}]}`,
		"check-runs/123/annotations": `[
			{"path":"src/a.go","start_line":10,"annotation_level":"failure","message":"undefined: foo\ndetail"}]`,
	})
	lines, ghErr := FetchJobDetail(context.Background(), run, Repo{Owner: "o", Name: "r"},
		CheckDetail{Name: "lint", State: StateFailure, CheckID: 123})
	if ghErr != nil {
		t.Fatalf("ghErr = %v", ghErr)
	}
	// 構成: step 一覧 (結論 + 所要時間) → 空行 → annotations
	want := []string{
		"✓ Set up job (2s)",
		"✗ Run lint (2m39s)",
		"",
		"[failure] src/a.go:10",
		"  undefined: foo",
		"  detail",
	}
	if len(lines) != len(want) {
		t.Fatalf("行数 = %d; want %d: %v", len(lines), len(want), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("lines[%d] = %q; want %q", i, lines[i], want[i])
		}
	}
}

func TestFetchJobDetailStepsBestEffort(t *testing.T) {
	// steps の取得失敗は annotations/ログの表示を妨げない
	run := func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "actions/jobs/") {
			return nil, []byte("boom"), errors.New("exit status 1")
		}
		if strings.Contains(joined, "annotations") {
			return []byte(`[{"path":"a.go","start_line":1,"annotation_level":"failure","message":"m"}]`), nil, nil
		}
		// goroutine から呼ばれるので Fatal* は使えない (argsRunner の注記参照)
		t.Errorf("想定外: %s", joined)
		return nil, nil, fmt.Errorf("想定外: %s", joined)
	}
	lines, ghErr := FetchJobDetail(context.Background(), run, Repo{Owner: "o", Name: "r"},
		CheckDetail{Name: "lint", State: StateFailure, CheckID: 5})
	if ghErr != nil || len(lines) != 2 || lines[0] != "[failure] a.go:1" {
		t.Errorf("lines = %v, ghErr = %v; want annotations のみ", lines, ghErr)
	}
}

func TestFetchJobDetailFallsBackToLog(t *testing.T) {
	logOut := "job\tstep\tline one\njob\tstep\tline two\n"
	run := argsRunner(t, map[string]string{
		"actions/jobs/9": `{"steps":[]}`,
		"annotations":    `[]`,
		"run view":       logOut,
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

// recordingRunner は呼ばれたコマンド引数を並行安全に記録する CommandRunner。
// FetchJobDetail は複数 goroutine から run を呼ぶので、argsRunner のように runner の中で
// t.Fatalf しない (テスト goroutine 以外からの Fatalf は不正)。判定は Wait 後にまとめて行う。
func recordingRunner(responses map[string]string, failures map[string]error) (CommandRunner, func() []string) {
	var (
		mu    sync.Mutex
		calls []string
	)
	run := func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		joined := strings.Join(args, " ")
		mu.Lock()
		calls = append(calls, joined)
		mu.Unlock()
		// ⚠️ 長いパターンから照合する: map の反復順は非決定的で、"actions/jobs/7" と
		// "actions/jobs/7/logs" のように前方一致で競合する組を登録すると、実行ごとに
		// 別の応答が返って flake する
		if pattern, ok := longestMatch(joined, slices.Collect(maps.Keys(failures))); ok {
			return nil, []byte("boom"), failures[pattern]
		}
		if pattern, ok := longestMatch(joined, slices.Collect(maps.Keys(responses))); ok {
			return []byte(responses[pattern]), nil, nil
		}
		return nil, nil, nil
	}
	return run, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(calls)
	}
}

// longestMatch は s に含まれる最長のパターンを返す (照合順を決定的にするため)。
func longestMatch(s string, patterns []string) (string, bool) {
	best, found := "", false
	for _, p := range patterns {
		if strings.Contains(s, p) && len(p) > len(best) {
			best, found = p, true
		}
	}
	return best, found
}

func calledWith(calls []string, pattern string) bool {
	return slices.ContainsFunc(calls, func(c string) bool { return strings.Contains(c, pattern) })
}

// 非失敗 job は job 単体ログの REST エンドポイントを使い、遅い gh run view は使わない。
// (gh run view --log は run 全体のログ zip を落とすため固定 ~1.0s 余分にかかる)
func TestFetchJobDetailUsesDirectLogEndpoint(t *testing.T) {
	run, calls := recordingRunner(map[string]string{
		"actions/jobs/7/logs": "2026-07-25T00:00:00.1234567Z line one\n2026-07-25T00:00:01.1234567Z line two\n",
		"actions/jobs/7":      `{"steps":[]}`,
		"annotations":         `[]`,
	}, nil)
	lines, ghErr := FetchJobDetail(context.Background(), run, Repo{Owner: "o", Name: "r"},
		CheckDetail{Name: "test", State: StateSuccess, CheckID: 7})
	if ghErr != nil {
		t.Fatalf("ghErr = %v", ghErr)
	}
	if !calledWith(calls(), "actions/jobs/7/logs") {
		t.Errorf("job 単体ログのエンドポイントを叩いていない: %v", calls())
	}
	if calledWith(calls(), "run view") {
		t.Errorf("遅い gh run view が使われている: %v", calls())
	}
	// タイムスタンプは logTail が落とす
	if len(lines) != 2 || lines[0] != "line one" || lines[1] != "line two" {
		t.Errorf("lines = %q; want [line one, line two]", lines)
	}
}

// 非失敗 job では annotations を待たずにログ取得を始める (投機)。annotations が空でも
// ログ取得のために 2 往復目を直列で足さない = 3 本すべてが並行に走っていること。
func TestFetchJobDetailSpeculatesLogInParallel(t *testing.T) {
	var (
		mu       sync.Mutex
		inFlight int
		maxSeen  int
	)
	run := func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		mu.Lock()
		inFlight++
		maxSeen = max(maxSeen, inFlight)
		mu.Unlock()
		time.Sleep(20 * time.Millisecond) // 重なりを観測できるだけの滞留
		mu.Lock()
		inFlight--
		mu.Unlock()
		switch joined := strings.Join(args, " "); {
		case strings.Contains(joined, "/logs"):
			return []byte("tail line\n"), nil, nil
		case strings.Contains(joined, "annotations"):
			return []byte(`[]`), nil, nil
		default:
			return []byte(`{"steps":[]}`), nil, nil
		}
	}
	if _, ghErr := FetchJobDetail(context.Background(), run, Repo{Owner: "o", Name: "r"},
		CheckDetail{Name: "test", State: StateSuccess, CheckID: 7}); ghErr != nil {
		t.Fatalf("ghErr = %v", ghErr)
	}
	if maxSeen != 3 {
		t.Errorf("同時実行数の最大 = %d, want 3 (steps / annotations / log が並行)", maxSeen)
	}
}

// 投機したログが失敗しても、annotations が取れているならそちらを見せてエラーにしない
// (実行中 job でログがまだ無いケース。投機の失敗を表示できる内容より優先しない)
func TestFetchJobDetailDiscardsSpeculativeLogError(t *testing.T) {
	run, calls := recordingRunner(
		map[string]string{
			"actions/jobs/7": `{"steps":[]}`,
			"annotations":    `[{"path":"a.go","start_line":1,"annotation_level":"failure","message":"boom"}]`,
		},
		map[string]error{"actions/jobs/7/logs": errors.New("exit status 1")},
	)
	lines, ghErr := FetchJobDetail(context.Background(), run, Repo{Owner: "o", Name: "r"},
		CheckDetail{Name: "test", State: StatePending, CheckID: 7})
	if ghErr != nil {
		t.Fatalf("投機ログの失敗がエラーとして表に出た: %v", ghErr)
	}
	if len(lines) == 0 || !strings.Contains(lines[0], "a.go:1") {
		t.Errorf("lines = %q; want annotations (先頭が file:line)", lines)
	}
	if !calledWith(calls(), "actions/jobs/7/logs") {
		t.Errorf("投機自体が走っていない: %v", calls())
	}
}

// 失敗 job は投機しない (annotations が出るのでログは不要)。annotations があるときに
// ログ取得が 1 本も飛ばないことを確認する = 無駄なダウンロードをしない。
func TestFetchJobDetailFailureDoesNotSpeculate(t *testing.T) {
	run, calls := recordingRunner(map[string]string{
		"actions/jobs/9": `{"steps":[]}`,
		"annotations":    `[{"path":"a.go","start_line":1,"annotation_level":"failure","message":"boom"}]`,
	}, nil)
	if _, ghErr := FetchJobDetail(context.Background(), run, Repo{Owner: "o", Name: "r"},
		CheckDetail{Name: "lint", State: StateFailure, CheckID: 9}); ghErr != nil {
		t.Fatalf("ghErr = %v", ghErr)
	}
	if calledWith(calls(), "/logs") || calledWith(calls(), "run view") {
		t.Errorf("失敗 job でログを取りに行った (annotations があるので不要): %v", calls())
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

func TestSanitizeDetailLineDropsNonSGREscapes(t *testing.T) {
	// SGR 以外の ESC シーケンスは端末制御注入の経路 (CI 側の第三者が混入可能) なので
	// シーケンスごと落とす
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"CSI 画面消去", "a\x1b[2Jb", "ab"},
		{"CSI カーソル移動", "a\x1b[10;20Hb", "ab"},
		{"CSI プライベートモード", "a\x1b[?25lb", "ab"},
		{"OSC52 クリップボード (ST 終端)", "a\x1b]52;c;ZXZpbA==\x1b\\b", "ab"},
		{"OSC タイトル変更 (BEL 終端)", "a\x1b]0;evil\ab", "ab"},
		{"DCS", "a\x1bPq#0\x1b\\b", "ab"},
		{"2 文字エスケープ", "a\x1b7b", "ab"},
		{"末尾の裸 ESC", "ab\x1b", "ab"},
		{"途切れた CSI", "ab\x1b[12", "ab"},
		{"途切れた OSC", "ab\x1b]0;title", "ab"},
		{"SGR は残る (混在)", "\x1b[31mred\x1b[0m \x1b[2Jx", "\x1b[31mred\x1b[0m x"},
	}
	for _, tt := range tests {
		if got := sanitizeDetailLine(tt.in); got != tt.want {
			t.Errorf("%s: sanitize(%q) = %q; want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

func TestDetailsOfSanitizesNames(t *testing.T) {
	// job 名 (StatusContext.context は外部が任意に設定可能) はパネルと終了後の静的出力に
	// そのまま載るため、取り込み時に無害化される
	rollup := rollupOf(rollupContext{Typename: "StatusContext", Context: "ci\x1b]0;evil\a/legacy", State: "SUCCESS"})
	details := detailsOf(rollup)
	if len(details) != 1 || details[0].Name != "ci/legacy" {
		t.Errorf("details = %+v; want name=ci/legacy (ESC 除去)", details)
	}
}

func TestAnnotationLinesSanitizesHead(t *testing.T) {
	fixture := `[{"path":"src/a\u001b[2J.go","start_line":1,"annotation_level":"failure","message":"m"}]`
	lines := annotationLines([]byte(fixture))
	if len(lines) == 0 || strings.Contains(lines[0], "\x1b") {
		t.Errorf("annotations 見出しに ESC が残っている: %q", lines)
	}
}

func TestLogTail(t *testing.T) {
	var b strings.Builder
	for i := range 100 {
		fmt.Fprintf(&b, "j\ts\tline %d\n", i)
	}
	lines := logTail([]byte(b.String()), 50)
	if len(lines) != 50 || lines[0] != "line 50" || lines[49] != "line 99" {
		t.Errorf("tail = %d 行, 先頭 %q, 末尾 %q", len(lines), lines[0], lines[len(lines)-1])
	}
	if got := logTail(nil, 50); len(got) != 0 {
		t.Errorf("空ログ = %v", got)
	}
	// 空行は非空行のカウントに含めず、末尾 n 非空行を表示順で返す ([]byte 化 + 末尾優先
	// 処理へのリファクタ後も「全行整形 → 末尾 n」と同結果になることの回帰)
	got := logTail([]byte("a\n\nb\n\nc\n"), 2)
	if len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Errorf("空行混じりの末尾 2 = %v; want [b c]", got)
	}
}

func TestLogTailSanitizesContent(t *testing.T) {
	// メッセージ部のタブは端末のタブ展開で枠の桁計算を壊す (スクロールで視界に入ると
	// 表示崩壊する実測バグ) ため、取り込み時に無害化する
	out := "j\ts\t\ufeffok  \tglog\t0.641s\r\n"
	lines := logTail([]byte(out), 50)
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
	lines := logTail([]byte(out), 50)
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
	// SGR (色/装飾) は残す (枠側の幅計算が対応済み)
	colored := "\x1b[36;1mmake test\x1b[0m"
	if got := sanitizeDetailLine(colored); got != colored {
		t.Errorf("SGR が落ちた: %q", got)
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

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, ""},
		{-time.Second, ""},
		{42 * time.Second, "42s"},
		{2*time.Minute + 39*time.Second, "2m39s"},
		{time.Hour + 2*time.Minute, "1h2m"},
	}
	for _, tt := range tests {
		if got := formatDuration(tt.d); got != tt.want {
			t.Errorf("formatDuration(%v) = %q; want %q", tt.d, got, tt.want)
		}
	}
}

func TestParseStatusResponsePRsAndDuration(t *testing.T) {
	sha := strings.Repeat("a", 40)
	fixture := `{"data":{"repository":{
		"c0": {"statusCheckRollup": {"state":"SUCCESS","contexts":{"nodes":[
			{"__typename":"CheckRun","name":"build","status":"COMPLETED","conclusion":"SUCCESS",
			 "startedAt":"2026-07-17T00:00:00Z","completedAt":"2026-07-17T00:02:39Z"}]}},
		 "associatedPullRequests":{"nodes":[
			{"number":10,"url":"https://github.com/o/r/pull/10","state":"MERGED"},
			{"number":12,"url":"https://github.com/o/r/pull/12","state":"OPEN"}]}}
	}}}`
	batch, ghErr := FetchCIStatuses(context.Background(), fakeRunner(fixture, "", nil),
		Repo{Owner: "o", Name: "r"}, []string{sha})
	if ghErr != nil {
		t.Fatalf("ghErr = %v", ghErr)
	}
	if d := batch.Details[sha][0].Duration; d != 2*time.Minute+39*time.Second {
		t.Errorf("Duration = %v; want 2m39s", d)
	}
	if pr := batch.PRs[sha]; pr == nil || pr.Number != 12 {
		t.Errorf("PRs = %+v; want OPEN の #12 (優先選択)", pr)
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

// pickBestPR の CLOSED-only fallback: OPEN/MERGED が無ければ先頭を返す (既存テストは OPEN>MERGED
// と PR なししか見ておらず、CLOSED のみを nil に落とす回帰を捕まえられなかった)。
func TestPickBestPRClosedOnlyFallback(t *testing.T) {
	got := pickBestPR([]PRRef{{Number: 5, State: "CLOSED"}, {Number: 8, State: "CLOSED"}})
	if got == nil || got.Number != 5 {
		t.Errorf("CLOSED のみのとき先頭 (#5) を返さない: %#v", got)
	}
}

// jobRerun は gh run rerun --job を正しい引数で叩き、失敗時は stderr 末尾行をエラーにする。
func TestJobRerun(t *testing.T) {
	var gotName string
	var gotArgs []string
	run := func(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
		gotName, gotArgs = name, args
		return nil, nil, nil
	}
	if err := jobRerun(context.Background(), run, Repo{Owner: "o", Name: "r"}, 42); err != nil {
		t.Fatalf("jobRerun() = %v", err)
	}
	want := []string{"run", "rerun", "--job", "42", "-R", "o/r"}
	if gotName != "gh" || !slices.Equal(gotArgs, want) {
		t.Fatalf("gh 呼び出しが違う: %s %v; want gh %v", gotName, gotArgs, want)
	}
	// 失敗: stderr の末尾行がエラーメッセージになる
	err := jobRerun(context.Background(),
		fakeRunner("", "some detail\nrun 123 cannot be rerun\n", errors.New("exit status 1")),
		Repo{Owner: "o", Name: "r"}, 42)
	if err == nil || err.Error() != "run 123 cannot be rerun" {
		t.Fatalf("stderr 末尾行がエラーにならない: %v", err)
	}
	// stderr が空なら元のエラーを使う
	err = jobRerun(context.Background(), fakeRunner("", "", errors.New("exit status 1")),
		Repo{Owner: "o", Name: "r"}, 42)
	if err == nil || err.Error() != "exit status 1" {
		t.Fatalf("stderr 空で元エラーが使われない: %v", err)
	}
}

// FetchPRStatus は PR 詳細フィールドを取り、複数 PR は OPEN 優先で 1 件選ぶ (issue 021)。
func TestFetchPRStatus(t *testing.T) {
	sha := strings.Repeat("a", 40)
	fixture := `{"data":{"repository":{"object":{"associatedPullRequests":{"nodes":[
		{"number":9,"url":"https://github.com/o/r/pull/9","state":"MERGED","title":"old","isDraft":false,
		 "reviewDecision":"APPROVED","mergeable":"MERGEABLE","baseRefName":"master","headRefName":"f/old"},
		{"number":12,"url":"https://github.com/o/r/pull/12","state":"OPEN","title":"new feature","isDraft":true,
		 "reviewDecision":"REVIEW_REQUIRED","mergeable":"CONFLICTING","baseRefName":"master","headRefName":"f/new"}
	]}}}}}`
	pr, ghErr := FetchPRStatus(context.Background(), fakeRunner(fixture, "", nil), Repo{Owner: "o", Name: "r"}, sha)
	if ghErr != nil {
		t.Fatalf("FetchPRStatus() error = %v", ghErr)
	}
	if pr == nil || pr.Number != 12 || pr.State != "OPEN" {
		t.Fatalf("OPEN 優先で選ばれない: %+v", pr)
	}
	if pr.Title != "new feature" || !pr.IsDraft || pr.ReviewDecision != "REVIEW_REQUIRED" ||
		pr.Mergeable != "CONFLICTING" || pr.BaseRefName != "master" || pr.HeadRefName != "f/new" {
		t.Fatalf("詳細フィールドが取れない: %+v", pr)
	}
	// PR なし (object はあるが nodes が空) は nil
	empty := `{"data":{"repository":{"object":{"associatedPullRequests":{"nodes":[]}}}}}`
	pr, ghErr = FetchPRStatus(context.Background(), fakeRunner(empty, "", nil), Repo{Owner: "o", Name: "r"}, sha)
	if ghErr != nil || pr != nil {
		t.Fatalf("PR なしで nil にならない: pr=%+v err=%v", pr, ghErr)
	}
}

// job 名は workflow YAML 由来でユーザーが自由に付けられる (絵文字が入りうる)。panelLines は
// job 名をそのまま枠の中へ置くので、VS16 付き絵文字が残ると枠と本文の幅が食い違う。
// git 由来テキストは gitlog.go の 2 入口、CI ログ由来は sanitizeDetailLine で正規化済みだが、
// job 名だけこの経路が抜けていた (実測 2026-07-25)。
func TestDetailsOfNormalizesVS16InJobName(t *testing.T) {
	const vs16 = "️"
	rollup := &rollupPayload{State: "FAILURE"}
	rollup.Contexts.Nodes = []rollupContext{
		{Typename: "CheckRun", Name: "build ⚠" + vs16 + " flaky", Status: "COMPLETED", Conclusion: "FAILURE"},
		{Typename: "StatusContext", Context: "ci/legacy ✔" + vs16, State: "SUCCESS"},
	}
	for _, d := range detailsOf(rollup) {
		if strings.Contains(d.Name, vs16) {
			t.Errorf("job 名に VS16 が残っている: %q", d.Name)
		}
	}
	// bare 記号自体は消さない (情報を落とさず幅だけ揃える)
	got := detailsOf(rollup)
	if len(got) != 2 || !strings.Contains(got[0].Name, "⚠") || !strings.Contains(got[1].Name, "✔") {
		t.Errorf("bare 記号まで落ちている: %+v", got)
	}
}
