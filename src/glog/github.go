package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// CIState はコミット単位に集約した CI 状態。
type CIState string

const (
	StateSuccess CIState = "success" // 対象 Check がすべて成功 (skipped 混在は許容)
	StateFailure CIState = "failure" // 1 つ以上失敗
	StatePending CIState = "pending" // queued / in_progress / pending あり
	StateNeutral CIState = "neutral" // cancelled / skipped / neutral のみ
	StateNone    CIState = "none"    // Check が存在しない (未 push の SHA も含む)
	StateUnknown CIState = "unknown" // 未取得・取得不能
)

// fetchMaxSHAs は 1 回の GraphQL で問い合わせる SHA 数の上限。alias 100 × contexts 100 で
// ノード数を抑える。超過分は StateUnknown のまま表示する (表示件数の既定 20 では届かない)。
const fetchMaxSHAs = 100

// GHErrorKind は gh 呼び出し失敗の分類。表示メッセージの出し分けに使う。
type GHErrorKind int

const (
	GHNotInstalled GHErrorKind = iota
	GHNotAuthenticated
	GHRateLimited
	GHOther
)

// GHError は GitHub 連携の失敗。Git 履歴表示は成立させたまま警告 1 行に落とす。
type GHError struct {
	Kind   GHErrorKind
	Detail string
}

func (e *GHError) Error() string { return e.Detail }

// Warning はユーザー向けの 1 行警告文。
func (e *GHError) Warning() string {
	switch e.Kind {
	case GHNotInstalled:
		return "glog: gh が見つからないため CI 状態を取得できません (brew install gh)"
	case GHNotAuthenticated:
		return "glog: gh が未認証のため CI 状態を取得できません (gh auth login)"
	case GHRateLimited:
		return "glog: GitHub API の rate limit に達しています: " + firstLine(e.Detail)
	default:
		return "glog: CI 状態の取得に失敗しました: " + firstLine(e.Detail)
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// Repo は remote から解決した GitHub リポジトリ。
type Repo struct {
	Owner string
	Name  string
}

// ResolveRepo はカレントリポジトリの remote から owner/name を解決する。
// 優先順: 現在ブランチの upstream remote → origin。GitHub 以外 / remote なしは ok=false。
func ResolveRepo() (Repo, bool) {
	remotes := []string{}
	if out, err := runGit("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err == nil {
		// 形式: "origin/main" → remote 名部分
		if name, _, found := strings.Cut(strings.TrimSpace(out), "/"); found && name != "" {
			remotes = append(remotes, name)
		}
	}
	remotes = append(remotes, "origin")
	seen := map[string]bool{}
	for _, remote := range remotes {
		if seen[remote] {
			continue
		}
		seen[remote] = true
		out, err := runGit("remote", "get-url", remote)
		if err != nil {
			continue
		}
		if repo, ok := ParseGitHubURL(strings.TrimSpace(out)); ok {
			return repo, true
		}
	}
	return Repo{}, false
}

var githubURLPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+?)(?:\.git)?/?$`),
	regexp.MustCompile(`^git@github\.com:([^/]+)/([^/]+?)(?:\.git)?$`),
	regexp.MustCompile(`^ssh://git@github\.com(?::\d+)?/([^/]+)/([^/]+?)(?:\.git)?$`),
}

// ParseGitHubURL は remote URL から owner/name を取り出す。GitHub 以外は ok=false。
func ParseGitHubURL(url string) (Repo, bool) {
	for _, re := range githubURLPatterns {
		if m := re.FindStringSubmatch(url); m != nil {
			return Repo{Owner: m[1], Name: m[2]}, true
		}
	}
	return Repo{}, false
}

// CommandRunner は外部コマンド実行の差し替え点。テストでは fixture を返す fake に置き換え、
// 通常テストで外部通信させない (issue のテスト方針)。
type CommandRunner func(ctx context.Context, name string, args ...string) (stdout []byte, stderr []byte, err error)

// ExecRunner は実際にコマンドを実行する CommandRunner。
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errBuf strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return []byte(out.String()), []byte(errBuf.String()), err
}

// GraphQL レスポンスの必要部分。
type rollupContext struct {
	Typename   string `json:"__typename"`
	Name       string `json:"name"`       // CheckRun のジョブ名
	Context    string `json:"context"`    // StatusContext のコンテキスト名
	Status     string `json:"status"`     // CheckRun: QUEUED / IN_PROGRESS / COMPLETED / ...
	Conclusion string `json:"conclusion"` // CheckRun: SUCCESS / FAILURE / NEUTRAL / CANCELLED / SKIPPED / ...
	State      string `json:"state"`      // StatusContext: SUCCESS / FAILURE / ERROR / PENDING / EXPECTED
}

// CheckDetail は展開表示用の Check 1 件分 (ジョブ名 + 状態)。
type CheckDetail struct {
	Name  string
	State CIState
}

type rollupPayload struct {
	State    string `json:"state"`
	Contexts struct {
		Nodes []rollupContext `json:"nodes"`
	} `json:"contexts"`
}

type commitPayload struct {
	StatusCheckRollup *rollupPayload `json:"statusCheckRollup"`
}

// FetchCIStatuses は表示対象 SHA を 1 リクエストの GraphQL へまとめて問い合わせる
// (コミットごとの REST 逐次呼び出しはしない: issue の設計)。認証は gh へ委譲する。
// 返り値の statuses に無い SHA は取得できなかったもの (呼び出し側で StateUnknown 扱い)。
// details は展開表示用のジョブ一覧 (Check が無い SHA は空スライス)。
// 部分成功 (data と errors の同時返却) では取れた分と GHError の両方を返す。
func FetchCIStatuses(ctx context.Context, run CommandRunner, repo Repo, shas []string) (map[string]CIState, map[string][]CheckDetail, *GHError) {
	if len(shas) == 0 {
		return map[string]CIState{}, map[string][]CheckDetail{}, nil
	}
	if len(shas) > fetchMaxSHAs {
		shas = shas[:fetchMaxSHAs]
	}
	query := buildStatusQuery(shas)
	stdout, stderr, err := run(ctx, "gh", "api", "graphql",
		"-F", "owner="+repo.Owner, "-F", "name="+repo.Name, "-f", "query="+query)
	if err != nil {
		return nil, nil, classifyGHError(err, string(stderr))
	}
	return parseStatusResponse(stdout, shas)
}

// buildStatusQuery は SHA ごとの alias で 1 クエリに束ねる。SHA は git が返した 40 桁 hex
// なのでクエリ文字列へのリテラル埋め込みで injection の余地はない。
func buildStatusQuery(shas []string) string {
	var b strings.Builder
	b.WriteString("query($owner: String!, $name: String!) { repository(owner: $owner, name: $name) {\n")
	for i, sha := range shas {
		fmt.Fprintf(&b, "c%d: object(oid: %q) { ...ciStatus }\n", i, sha)
	}
	b.WriteString(`} }
fragment ciStatus on Commit {
  statusCheckRollup {
    state
    contexts(first: 100) {
      nodes {
        __typename
        ... on CheckRun { name status conclusion }
        ... on StatusContext { context state }
      }
    }
  }
}`)
	return b.String()
}

func parseStatusResponse(stdout []byte, shas []string) (map[string]CIState, map[string][]CheckDetail, *GHError) {
	var resp struct {
		Data struct {
			Repository map[string]json.RawMessage `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(stdout, &resp); err != nil {
		return nil, nil, &GHError{Kind: GHOther, Detail: "GraphQL レスポンスを解析できません: " + err.Error()}
	}
	// GraphQL は data と errors を同時に返しうる (部分成功)。取れた分は表示に使いつつ、
	// 失敗があった事実は警告として通知する (欠落 SHA が黙って ? になるのを防ぐ)。
	var ghErr *GHError
	if len(resp.Errors) > 0 {
		ghErr = &GHError{Kind: GHOther, Detail: "GraphQL エラー: " + resp.Errors[0].Message}
	}
	if resp.Data.Repository == nil {
		if ghErr != nil {
			return nil, nil, ghErr
		}
		return nil, nil, &GHError{Kind: GHOther, Detail: "GraphQL レスポンスに repository がありません"}
	}
	statuses := make(map[string]CIState, len(shas))
	details := make(map[string][]CheckDetail, len(shas))
	for i, sha := range shas {
		raw, ok := resp.Data.Repository[fmt.Sprintf("c%d", i)]
		if !ok {
			continue
		}
		var commit *commitPayload
		if err := json.Unmarshal(raw, &commit); err != nil {
			continue
		}
		if commit == nil {
			// SHA が GitHub 上に存在しない (未 push など) → Check なし扱い
			statuses[sha] = StateNone
			details[sha] = []CheckDetail{}
			continue
		}
		statuses[sha] = aggregateRollup(commit.StatusCheckRollup)
		details[sha] = detailsOf(commit.StatusCheckRollup)
	}
	return statuses, details, ghErr
}

// nodeState は Check 1 件分の状態を CIState へ写す。集約と展開表示の両方が使う。
func nodeState(node rollupContext) CIState {
	switch node.Typename {
	case "CheckRun":
		if node.Status != "COMPLETED" {
			return StatePending
		}
		switch node.Conclusion {
		case "SUCCESS":
			return StateSuccess
		case "FAILURE", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE":
			return StateFailure
		default: // NEUTRAL / CANCELLED / SKIPPED / STALE
			return StateNeutral
		}
	case "StatusContext":
		switch node.State {
		case "SUCCESS":
			return StateSuccess
		case "FAILURE", "ERROR":
			return StateFailure
		case "PENDING", "EXPECTED":
			return StatePending
		default:
			return StateNeutral
		}
	default:
		return StateUnknown
	}
}

// aggregateRollup は issue の集約ルールを適用する:
// 1. 失敗あり → failure  2. 実行中あり → pending  3. 成功あり → success
// 4. cancelled/skipped/neutral のみ → neutral  5. Check なし → none
func aggregateRollup(rollup *rollupPayload) CIState {
	if rollup == nil || len(rollup.Contexts.Nodes) == 0 {
		return StateNone
	}
	var anyFailure, anyPending, anySuccess, anyNeutral bool
	for _, node := range rollup.Contexts.Nodes {
		switch nodeState(node) {
		case StateFailure:
			anyFailure = true
		case StatePending:
			anyPending = true
		case StateSuccess:
			anySuccess = true
		case StateNeutral:
			anyNeutral = true
		}
	}
	switch {
	case anyFailure:
		return StateFailure
	case anyPending:
		return StatePending
	case anySuccess:
		return StateSuccess
	case anyNeutral:
		return StateNeutral
	default:
		return StateNone
	}
}

// detailsOf は展開表示用のジョブ一覧を組み立てる。
func detailsOf(rollup *rollupPayload) []CheckDetail {
	if rollup == nil {
		return []CheckDetail{}
	}
	details := make([]CheckDetail, 0, len(rollup.Contexts.Nodes))
	for _, node := range rollup.Contexts.Nodes {
		name := node.Name
		if name == "" {
			name = node.Context
		}
		if name == "" {
			name = "(unnamed)"
		}
		details = append(details, CheckDetail{Name: name, State: nodeState(node)})
	}
	return details
}

func classifyGHError(err error, stderr string) *GHError {
	if errors.Is(err, exec.ErrNotFound) {
		return &GHError{Kind: GHNotInstalled, Detail: err.Error()}
	}
	lower := strings.ToLower(stderr)
	switch {
	// gh のバージョンで文言が揺れる: "gh auth login" / "not logged into any GitHub hosts" /
	// "authentication required" などをまとめて未認証扱いにする
	case strings.Contains(lower, "gh auth login") ||
		strings.Contains(lower, "not logged in") ||
		strings.Contains(lower, "authentication"):
		return &GHError{Kind: GHNotAuthenticated, Detail: stderr}
	case strings.Contains(lower, "rate limit") || strings.Contains(lower, "rate_limited"):
		return &GHError{Kind: GHRateLimited, Detail: stderr}
	default:
		detail := stderr
		if strings.TrimSpace(detail) == "" {
			detail = err.Error()
		}
		return &GHError{Kind: GHOther, Detail: detail}
	}
}
