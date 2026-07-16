package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// CIState はコミット単位に集約した CI 状態。
type CIState string

const (
	StateSuccess CIState = "success" // 対象 Check がすべて成功 (skipped 混在は許容)
	StateFailure CIState = "failure" // 1 つ以上失敗
	StatePending CIState = "pending" // queued / in_progress / pending あり
	StateNeutral CIState = "neutral" // cancelled / skipped / neutral のみ
	StateNone     CIState = "none"     // push 済みだが Check が存在しない
	StateUnknown  CIState = "unknown"  // 未取得・取得不能
	StateUnpushed CIState = "unpushed" // まだ push されていない (GitHub 上に SHA が無い)。ローカル判定のみで API には問い合わせない
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
	DetailsURL string `json:"detailsUrl"` // CheckRun のジョブ詳細ページ
	TargetURL  string `json:"targetUrl"`  // StatusContext のリンク先
	DatabaseID int64  `json:"databaseId"` // CheckRun の REST id (= GitHub Actions の job id)
}

// CheckDetail は展開表示用の Check 1 件分 (ジョブ名 + 状態 + 詳細ページ URL)。
type CheckDetail struct {
	Name    string
	State   CIState
	URL     string // 無い場合は空
	CheckID int64  // CheckRun の REST id。annotations / ログ取得に使う (StatusContext は 0)
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
//
// contexts は先頭 100 件しか見ない。Check が 100 件を超えるコミットでは 101 件目以降の
// 失敗を取りこぼして ✓ と誤報しうるが、「100 超の Check を持つ repo は現実に扱わない」
// とのユーザー判断 (2026-07-16) で totalCount ガード / pagination は見送り。
// そうした repo を扱うようになったら再評価する (totalCount を見て安全側 ? に倒すのが最小対応)。
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
        ... on CheckRun { name status conclusion detailsUrl databaseId }
        ... on StatusContext { context state targetUrl }
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
		url := node.DetailsURL
		if url == "" {
			url = node.TargetURL
		}
		details = append(details, CheckDetail{Name: name, State: nodeState(node), URL: url, CheckID: node.DatabaseID})
	}
	return details
}

// jobLogTailLines はログ表示の行数 (末尾から)。
const jobLogTailLines = 50

// FetchJobDetail は job の「何が起きたか」を取得する。annotations (CI が報告した
// file:line + メッセージの構造化データ) があればそれを優先し、無ければログの末尾を返す
// (失敗 job は --log-failed で失敗ステップのみ)。GitHub Actions の CheckRun 限定
// (StatusContext = 外部 CI はログの取得経路が無い)。
func FetchJobDetail(ctx context.Context, run CommandRunner, repo Repo, check CheckDetail) ([]string, *GHError) {
	if check.CheckID == 0 {
		return []string{"(GitHub Actions の job ではないため詳細を取得できません)"}, nil
	}
	id := strconv.FormatInt(check.CheckID, 10)
	stdout, stderr, err := run(ctx, "gh", "api",
		fmt.Sprintf("repos/%s/%s/check-runs/%s/annotations", repo.Owner, repo.Name, id))
	if err != nil {
		return nil, classifyGHError(err, string(stderr))
	}
	if lines := annotationLines(stdout); len(lines) > 0 {
		return lines, nil
	}
	args := []string{"run", "view", "--job", id, "-R", repo.Owner + "/" + repo.Name}
	if check.State == StateFailure {
		args = append(args, "--log-failed")
	} else {
		args = append(args, "--log")
	}
	stdout, stderr, err = run(ctx, "gh", args...)
	if err != nil {
		return nil, classifyGHError(err, string(stderr))
	}
	lines := logTail(string(stdout), jobLogTailLines)
	if len(lines) == 0 {
		return []string{"(ログが空です)"}, nil
	}
	return lines, nil
}

// annotationLines は check-runs annotations API のレスポンスを表示行へ変換する。
func annotationLines(stdout []byte) []string {
	var annotations []struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		Level     string `json:"annotation_level"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal(stdout, &annotations); err != nil {
		return nil
	}
	var lines []string
	for _, a := range annotations {
		head := fmt.Sprintf("[%s] %s:%d", a.Level, a.Path, a.StartLine)
		lines = append(lines, head)
		for msg := range strings.SplitSeq(strings.TrimRight(a.Message, "\n"), "\n") {
			lines = append(lines, "  "+sanitizeDetailLine(msg))
		}
	}
	return lines
}

// sanitizeDetailLine は詳細ポップアップの枠描画を壊す制御文字を無害化する。
// タブが根本原因の実測バグ: runewidth は \t を幅 0 と数えるが端末は 8 桁タブストップへ
// 展開するため、右枠の桁計算がずれて行が折り返し、インライン再描画の行対応が崩壊する
// (go test の "ok \tglog\t0.5s" 等、ログのメッセージ部には普通にタブが混ざる)。
// ANSI カラー (ESC) は枠側の幅計算が対応済みなので残す。BOM (GitHub のログ先頭に付く)
// と \r 等の他の制御文字は落とす。
func sanitizeDetailLine(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return (r < 0x20 && r != '\x1b') || r == 0x7f || r == '\ufeff' }) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteString("    ")
		case r == '\x1b':
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f || r == '\ufeff':
			// drop
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// logTimestampRe は GitHub Actions ログの各行頭に付く ISO タイムスタンプ。
// 幅を ~29 桁食う (長い行が枠幅を超えて色落ち・切り詰めされる主因) 上に情報量が薄いので
// 表示からは落とす。
var logTimestampRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z ?`)

// logTail は gh run view --log の出力から末尾 n 行を取り出す。各行の
// "job名<TAB>step名<TAB>" プレフィックスとタイムスタンプは表示幅の邪魔なので落とし、
// 残りは sanitizeDetailLine で枠描画を壊す制御文字を無害化する。
func logTail(out string, n int) []string {
	var lines []string
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		if parts := strings.SplitN(line, "\t", 3); len(parts) == 3 {
			line = parts[2]
		}
		line = sanitizeDetailLine(line)
		line = logTimestampRe.ReplaceAllString(line, "")
		lines = append(lines, line)
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
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
