package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CIState はコミット単位に集約した CI 状態。
type CIState string

const (
	StateSuccess  CIState = "success"  // 対象 Check がすべて成功 (skipped 混在は許容)
	StateFailure  CIState = "failure"  // 1 つ以上失敗
	StatePending  CIState = "pending"  // queued / in_progress / pending あり
	StateNeutral  CIState = "neutral"  // cancelled / skipped / neutral のみ
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

// lastLine は最後の非空行を返す (末尾に要約が来るコマンド出力の notice 用)。全行空なら "".
func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}

// Repo は remote から解決した GitHub リポジトリ。
type Repo struct {
	Owner string
	Name  string
}

// ResolveRepo はカレントリポジトリの remote から owner/name を解決する。
// 優先順: 現在ブランチの upstream remote → origin。GitHub 以外 / remote なしは ok=false。
//
// 起動律速を縮めるため origin の URL を投機的に並列取得する: 素朴に書くと
// rev-parse @{upstream} の出力を remote get-url の引数に使う 2-fork 直列になり、これが
// 起動の唯一の複数 fork 直列チェーンだった。共通ケース (upstream remote が origin か未設定)
// では投機取得した origin URL をそのまま使い 2 本目を消す。upstream が非 origin remote の
// 稀なケースだけ従来どおり get-url を直列で払う (どのケースでも遅くならない厳密改善)。
// insteadOf 意味論を保つため git config 直読でなく remote get-url を使い続ける。
func ResolveRepo() (Repo, bool) {
	originCh := make(chan string, 1)
	go func() {
		out, err := runGit("remote", "get-url", "origin")
		if err != nil {
			originCh <- ""
			return
		}
		originCh <- strings.TrimSpace(out)
	}()

	var upstreamRemote string
	if out, err := runGit("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err == nil {
		// 形式: "origin/main" → remote 名部分
		if name, _, found := strings.Cut(strings.TrimSpace(out), "/"); found && name != "" {
			upstreamRemote = name
		}
	}
	originURL := <-originCh

	// upstream remote (非 origin) を最優先で解決。origin と同じ/未設定なら投機取得を使う。
	if upstreamRemote != "" && upstreamRemote != "origin" {
		if out, err := runGit("remote", "get-url", upstreamRemote); err == nil {
			if repo, ok := ParseGitHubURL(strings.TrimSpace(out)); ok {
				return repo, true
			}
		}
	}
	// upstream が無い/非 GitHub/get-url 失敗 → origin へ fallback (元コードと同じ優先順)。
	if repo, ok := ParseGitHubURL(originURL); ok {
		return repo, true
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
// bytes.Buffer 直返しで string 経由の再コピーを避ける (gh run view のログは MB 級になりうる)。
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return out.Bytes(), errBuf.Bytes(), err
}

// GraphQL レスポンスの必要部分。
type rollupContext struct {
	Typename    string    `json:"__typename"`
	Name        string    `json:"name"`        // CheckRun のジョブ名
	Context     string    `json:"context"`     // StatusContext のコンテキスト名
	Status      string    `json:"status"`      // CheckRun: QUEUED / IN_PROGRESS / COMPLETED / ...
	Conclusion  string    `json:"conclusion"`  // CheckRun: SUCCESS / FAILURE / NEUTRAL / CANCELLED / SKIPPED / ...
	State       string    `json:"state"`       // StatusContext: SUCCESS / FAILURE / ERROR / PENDING / EXPECTED
	DetailsURL  string    `json:"detailsUrl"`  // CheckRun のジョブ詳細ページ
	TargetURL   string    `json:"targetUrl"`   // StatusContext のリンク先
	DatabaseID  int64     `json:"databaseId"`  // CheckRun の REST id (= GitHub Actions の job id)
	StartedAt   time.Time `json:"startedAt"`   // CheckRun の開始時刻 (所要時間表示用)
	CompletedAt time.Time `json:"completedAt"` // CheckRun の完了時刻
}

// CheckDetail は展開表示用の Check 1 件分 (ジョブ名 + 状態 + 詳細ページ URL)。
type CheckDetail struct {
	Name     string
	State    CIState
	URL      string        // 無い場合は空
	CheckID  int64         // CheckRun の REST id。annotations / ログ取得に使う (StatusContext は 0)
	Duration time.Duration // job の所要時間 (0 = 不明。StatusContext / 実行中)
	// StartedAt は CheckRun の開始時刻 (未開始 / StatusContext は zero)。実行中 job の
	// 「経過時間 = now - StartedAt」表示に使う。完了 job では Duration が所要時間の出典で
	// あり StartedAt は使わない (完了判定は Duration>0 でなく State で行う)。
	StartedAt time.Time
}

type rollupPayload struct {
	State    string `json:"state"`
	Contexts struct {
		Nodes []rollupContext `json:"nodes"`
	} `json:"contexts"`
}

type commitPayload struct {
	StatusCheckRollup      *rollupPayload `json:"statusCheckRollup"`
	AssociatedPullRequests struct {
		Nodes []PRRef `json:"nodes"`
	} `json:"associatedPullRequests"`
}

// CIBatch は一括 GraphQL の取得結果。
type CIBatch struct {
	Statuses map[string]CIState
	Details  map[string][]CheckDetail
	// PRs は commit に紐づく PR (複数あれば OPEN > MERGED 優先で 1 件)。
	// 「確認したが無い」も nil で格納する (再問い合わせ抑止)
	PRs map[string]*PRRef
}

// FetchCIStatuses は表示対象 SHA を 1 リクエストの GraphQL へまとめて問い合わせる
// (コミットごとの REST 逐次呼び出しはしない: issue の設計)。認証は gh へ委譲する。
// Statuses に無い SHA は取得できなかったもの (呼び出し側で StateUnknown 扱い)。
// Details は展開表示用のジョブ一覧 (Check が無い SHA は空スライス)。PRs はコミット行の
// バッジと p キーのキャッシュに使う。部分成功 (data と errors の同時返却) では
// 取れた分と GHError の両方を返す。
func FetchCIStatuses(ctx context.Context, run CommandRunner, repo Repo, shas []string) (CIBatch, *GHError) {
	if len(shas) == 0 {
		return emptyBatch(), nil
	}
	if len(shas) > fetchMaxSHAs {
		shas = shas[:fetchMaxSHAs]
	}
	query := buildStatusQuery(shas)
	stdout, stderr, err := run(ctx, "gh", "api", "graphql",
		"-F", "owner="+repo.Owner, "-F", "name="+repo.Name, "-f", "query="+query)
	if err != nil {
		return emptyBatch(), classifyGHError(err, string(stderr))
	}
	return parseStatusResponse(stdout, shas)
}

func emptyBatch() CIBatch {
	return CIBatch{
		Statuses: map[string]CIState{},
		Details:  map[string][]CheckDetail{},
		PRs:      map[string]*PRRef{},
	}
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
        ... on CheckRun { name status conclusion detailsUrl databaseId startedAt completedAt }
        ... on StatusContext { context state targetUrl }
      }
    }
  }
  associatedPullRequests(first: 3) { nodes { number url state } }
}`)
	return b.String()
}

func parseStatusResponse(stdout []byte, shas []string) (CIBatch, *GHError) {
	batch := emptyBatch()
	var resp struct {
		Data struct {
			Repository map[string]json.RawMessage `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(stdout, &resp); err != nil {
		return batch, &GHError{Kind: GHOther, Detail: "GraphQL レスポンスを解析できません: " + err.Error()}
	}
	// GraphQL は data と errors を同時に返しうる (部分成功)。取れた分は表示に使いつつ、
	// 失敗があった事実は警告として通知する (欠落 SHA が黙って ? になるのを防ぐ)。
	var ghErr *GHError
	if len(resp.Errors) > 0 {
		ghErr = &GHError{Kind: GHOther, Detail: "GraphQL エラー: " + resp.Errors[0].Message}
	}
	if resp.Data.Repository == nil {
		if ghErr != nil {
			return batch, ghErr
		}
		return batch, &GHError{Kind: GHOther, Detail: "GraphQL レスポンスに repository がありません"}
	}
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
			batch.Statuses[sha] = StateNone
			batch.Details[sha] = []CheckDetail{}
			continue
		}
		batch.Statuses[sha] = aggregateRollup(commit.StatusCheckRollup)
		batch.Details[sha] = detailsOf(commit.StatusCheckRollup)
		batch.PRs[sha] = pickBestPR(commit.AssociatedPullRequests.Nodes)
	}
	return batch, ghErr
}

// pickBestPR は複数 PR (cherry-pick 等) から OPEN > MERGED > その他 の優先で 1 件選ぶ。
// 無ければ nil。
func pickBestPR(nodes []PRRef) *PRRef {
	if len(nodes) == 0 {
		return nil
	}
	for _, state := range []string{"OPEN", "MERGED"} {
		for _, n := range nodes {
			if n.State == state {
				return &n
			}
		}
	}
	return &nodes[0]
}

// fillUnknownFetched は一括取得の応答に無かった SHA を unknown で埋めて返す
// (表示と 30 秒の負キャッシュの両方に載る)。静的経路 (fetchStatic) と TUI 経路
// (ciResultMsg) で共通の仕様をここに一本化する。
func fillUnknownFetched(fetched map[string]CIState, toFetch []string) map[string]CIState {
	if fetched == nil {
		fetched = map[string]CIState{}
	}
	for _, sha := range toFetch {
		if _, ok := fetched[sha]; !ok {
			fetched[sha] = StateUnknown
		}
	}
	return fetched
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
		var duration time.Duration
		if !node.StartedAt.IsZero() && !node.CompletedAt.IsZero() {
			duration = node.CompletedAt.Sub(node.StartedAt)
		}
		// name は外部 (StatusContext を作る任意のインテグレーション) が制御できる表示文字列。
		// パネルと終了後の静的出力にそのまま載るため、ここで無害化する
		details = append(details, CheckDetail{
			Name:      sanitizeDetailLine(name),
			State:     nodeState(node),
			URL:       url,
			CheckID:   node.DatabaseID,
			Duration:  duration,
			StartedAt: node.StartedAt,
		})
	}
	return details
}

// jobLogTailLines はログ表示の行数 (末尾から)。
const jobLogTailLines = 50

// FetchJobDetail は job の「何が起きたか」を取得する。構成は上から
// ① step 一覧 (結論 + 所要時間。どの step で落ちた/遅いかの一覧)
// ② annotations (CI が報告した file:line + メッセージ) があればそれ、無ければ
// ③ ログの末尾 (失敗 job は --log-failed で失敗ステップのみ)。
// GitHub Actions の CheckRun 限定 (StatusContext = 外部 CI は取得経路が無い)。
func FetchJobDetail(ctx context.Context, run CommandRunner, repo Repo, check CheckDetail) ([]string, *GHError) {
	if check.CheckID == 0 {
		return []string{"(GitHub Actions の job ではないため詳細を取得できません)"}, nil
	}
	id := strconv.FormatInt(check.CheckID, 10)
	// step 一覧 (best-effort) と annotations は入力が jobID だけで互いに独立、かつ常に
	// 両方叩くので並列化する (gh 起動 + REST 往復を 2 本直列にすると待ちがほぼ倍。
	// job 詳細パネルを開く対話操作のレイテンシに直結)。3 本目のログ取得は annotations が
	// 空のときだけなので後段の直列のまま。CommandRunner は read-only 実行で状態を持たず
	// 並行安全 (ExecRunner は exec.CommandContext + ローカル buffer)。
	var (
		lines     []string
		annStdout []byte
		annStderr []byte
		annErr    error
		wg        sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		lines = fetchJobSteps(ctx, run, repo, id)
	}()
	go func() {
		defer wg.Done()
		// per_page 既定の 30 件では大量 annotation の lint job で取りこぼすため 100 に広げる。
		// 100 超の pagination は contexts(first:100) と同じ判断で追わない
		annStdout, annStderr, annErr = run(ctx, "gh", "api",
			fmt.Sprintf("repos/%s/%s/check-runs/%s/annotations?per_page=100", repo.Owner, repo.Name, id))
	}()
	wg.Wait()
	if annErr != nil {
		return nil, classifyGHError(annErr, string(annStderr))
	}
	if annotations := annotationLines(annStdout); len(annotations) > 0 {
		return appendSection(lines, annotations), nil
	}
	args := []string{"run", "view", "--job", id, "-R", repo.Owner + "/" + repo.Name}
	if check.State == StateFailure {
		args = append(args, "--log-failed")
	} else {
		args = append(args, "--log")
	}
	stdout, stderr, err := run(ctx, "gh", args...)
	if err != nil {
		return nil, classifyGHError(err, string(stderr))
	}
	tail := logTail(stdout, jobLogTailLines)
	if len(tail) == 0 {
		tail = []string{"(ログが空です)"}
	}
	return appendSection(lines, tail), nil
}

// appendSection は空行を挟んでセクションを連結する (先頭セクションが空なら区切りなし)。
func appendSection(head, tail []string) []string {
	if len(head) == 0 {
		return tail
	}
	return append(append(head, ""), tail...)
}

// fetchJobSteps は job の step 一覧を「記号 step名 (所要時間)」の行列で返す。
// 失敗時は空 (詳細本文の取得を妨げない best-effort)。
func fetchJobSteps(ctx context.Context, run CommandRunner, repo Repo, jobID string) []string {
	stdout, _, err := run(ctx, "gh", "api",
		fmt.Sprintf("repos/%s/%s/actions/jobs/%s", repo.Owner, repo.Name, jobID))
	if err != nil {
		return nil
	}
	var job struct {
		Steps []struct {
			Name        string    `json:"name"`
			Status      string    `json:"status"`
			Conclusion  string    `json:"conclusion"`
			StartedAt   time.Time `json:"started_at"`
			CompletedAt time.Time `json:"completed_at"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(stdout, &job); err != nil || len(job.Steps) == 0 {
		return nil
	}
	lines := make([]string, 0, len(job.Steps))
	for _, s := range job.Steps {
		glyph := stepGlyph(s.Status, s.Conclusion)
		var duration time.Duration
		if !s.StartedAt.IsZero() && !s.CompletedAt.IsZero() {
			duration = s.CompletedAt.Sub(s.StartedAt)
		}
		line := glyph + " " + sanitizeDetailLine(s.Name)
		if d := formatDuration(duration); d != "" {
			line += " (" + d + ")"
		}
		lines = append(lines, line)
	}
	return lines
}

// stepGlyph は step の状態記号 (コミット/job と同じ語彙)。
func stepGlyph(status, conclusion string) string {
	if status != "completed" {
		return "●"
	}
	switch conclusion {
	case "success":
		return "✓"
	case "failure", "timed_out", "action_required", "startup_failure":
		return "✗"
	default: // skipped / cancelled / neutral
		return "⊘"
	}
}

// formatDuration は所要時間の短い表記 ("42s" / "2m39s" / "1h2m")。0 以下は空。
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
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
		// Level / Path も CI 側が制御する表示文字列なので無害化を通す
		head := sanitizeDetailLine(fmt.Sprintf("[%s] %s:%d", a.Level, a.Path, a.StartLine))
		lines = append(lines, head)
		for msg := range strings.SplitSeq(strings.TrimRight(a.Message, "\n"), "\n") {
			lines = append(lines, "  "+sanitizeDetailLine(msg))
		}
	}
	return lines
}

// PRRef は commit に紐づく Pull Request。
type PRRef struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	State  string `json:"state"` // OPEN / MERGED / CLOSED
}

// FetchCommitPR は commit に紐づく PR を返す (無ければ nil)。ブランチの特定は不要で、
// GitHub が commit → PR の関連 (associatedPullRequests) を保持している。
// 複数ある場合 (cherry-pick 等) は OPEN > MERGED > その他 の優先で 1 件選ぶ。
func FetchCommitPR(ctx context.Context, run CommandRunner, repo Repo, sha string) (*PRRef, *GHError) {
	query := fmt.Sprintf(`query($owner: String!, $name: String!) { repository(owner: $owner, name: $name) {
  object(oid: %q) { ... on Commit { associatedPullRequests(first: 5) { nodes { number url state } } } }
} }`, sha)
	stdout, stderr, err := run(ctx, "gh", "api", "graphql",
		"-F", "owner="+repo.Owner, "-F", "name="+repo.Name, "-f", "query="+query)
	if err != nil {
		return nil, classifyGHError(err, string(stderr))
	}
	var resp struct {
		Data struct {
			Repository struct {
				Object *struct {
					AssociatedPullRequests struct {
						Nodes []PRRef `json:"nodes"`
					} `json:"associatedPullRequests"`
				} `json:"object"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout, &resp); err != nil {
		return nil, &GHError{Kind: GHOther, Detail: "GraphQL レスポンスを解析できません: " + err.Error()}
	}
	obj := resp.Data.Repository.Object
	if obj == nil {
		return nil, nil
	}
	return pickBestPR(obj.AssociatedPullRequests.Nodes), nil
}

// sanitizeDetailLine は CI 由来の表示文字列 (ログ・annotations・job 名) を端末描画に
// 対して無害化する。
//
//   - タブ → スペース 4: runewidth は \t を幅 0 と数えるが端末は 8 桁タブストップへ展開
//     するため、右枠の桁計算がずれて行が折り返し、インライン再描画が崩壊する (実測バグ)
//   - ANSI は SGR (ESC[…m = 色/装飾) だけを通す allowlist。それ以外の CSI (画面消去・
//     カーソル移動) や OSC/DCS 等 (OSC52 のクリップボード書き込み・タイトル変更) は、
//     CI 側の第三者 (任意の status インテグレーション等) が混入させられる端末制御
//     シーケンス注入の経路になるため、シーケンスごと落とす
//   - BOM (GitHub のログ先頭に付く U+FEFF) と \r 等の残る制御文字は落とす
func sanitizeDetailLine(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f || r == '\ufeff' }) {
		return s
	}
	rs := []rune(s)
	var b strings.Builder
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '\t':
			b.WriteString("    ")
		case r == '\x1b':
			i = keepOnlySGR(&b, rs, i)
		case r < 0x20 || r == 0x7f || r == '\ufeff':
			// drop
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// keepOnlySGR は rs[i] の ESC から始まるシーケンスを解釈し、SGR (色/装飾) だけを b へ
// 書き出してそれ以外は捨てる。戻り値は消費したシーケンスの最終 index。
func keepOnlySGR(b *strings.Builder, rs []rune, i int) int {
	if i+1 >= len(rs) {
		return i // 末尾の裸 ESC は捨てる
	}
	switch rs[i+1] {
	case '[': // CSI: ESC [ <param/intermediate 0x20-0x3f>* <final 0x40-0x7e>
		j := i + 2
		for j < len(rs) && rs[j] >= 0x20 && rs[j] <= 0x3f {
			j++
		}
		if j >= len(rs) {
			return len(rs) - 1 // 途切れた CSI は捨てる
		}
		if rs[j] == 'm' && runesOnly(rs[i+2:j], "0123456789;:") {
			b.WriteString(string(rs[i : j+1])) // SGR のみ通す
		}
		return j
	case ']', 'P', '_', '^', 'X': // OSC / DCS / APC / PM / SOS: ST (ESC \) か BEL まで捨てる
		for j := i + 2; j < len(rs); j++ {
			if rs[j] == '\a' {
				return j
			}
			if rs[j] == '\x1b' && j+1 < len(rs) && rs[j+1] == '\\' {
				return j + 1
			}
		}
		return len(rs) - 1
	default:
		return i + 1 // その他の 2 文字エスケープ (ESC 7 等) は捨てる
	}
}

func runesOnly(rs []rune, allowed string) bool {
	for _, r := range rs {
		if !strings.ContainsRune(allowed, r) {
			return false
		}
	}
	return true
}

// logTimestampRe は GitHub Actions ログの各行頭に付く ISO タイムスタンプ。
// 幅を ~29 桁食う (長い行が枠幅を超えて色落ち・切り詰めされる主因) 上に情報量が薄いので
// 表示からは落とす。
var logTimestampRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z ?`)

// logTail は gh run view --log の出力から末尾 n 行を取り出す。各行の
// "job名<TAB>step名<TAB>" プレフィックスとタイムスタンプは表示幅の邪魔なので落とし、
// 残りは sanitizeDetailLine で枠描画を壊す制御文字を無害化する。
//
// 入力は []byte のまま受ける (ExecRunner が MB 級ログの string 再コピーを避けて []byte を
// 返す最適化を尊重する)。整形 (tab 剥がし + sanitize + regex) は末尾 n 非空行にだけ掛ける:
// 非失敗 job では --log で全ログ (数千〜数万行) が来るのに表示は 50 行だけなので、全行を
// 整形してから捨てるのは無駄。空行判定・整形はどちらも行単位で 1:1 なので「全行整形 →
// 末尾 n」と「末尾 n の非空行を整形」は同じ結果になる。
func logTail(out []byte, n int) []string {
	raw := bytes.Split(bytes.TrimRight(out, "\n"), []byte{'\n'})
	// 末尾から非空行を n 本拾う (逆順に集める)
	kept := make([][]byte, 0, n)
	for i := len(raw) - 1; i >= 0 && len(kept) < n; i-- {
		if len(raw[i]) > 0 {
			kept = append(kept, raw[i])
		}
	}
	// 表示順 (先頭→末尾) に戻しつつ、拾った ~n 行だけ整形する
	lines := make([]string, 0, len(kept))
	for i := len(kept) - 1; i >= 0; i-- {
		line := string(kept[i])
		if parts := strings.SplitN(line, "\t", 3); len(parts) == 3 {
			line = parts[2]
		}
		line = sanitizeDetailLine(line)
		line = logTimestampRe.ReplaceAllString(line, "")
		lines = append(lines, line)
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
