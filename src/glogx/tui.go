package main

import (
	"context"
	"fmt"
	"maps"
	"math"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Bubble Tea による less 風の対話ブラウズ (カーソル移動 + CI job 表示)。
// 元 issue では「対話 UI は非目標」「Alt Screen 不使用で最終表示を履歴に残す」だったが、
// どちらもユーザー指示で上書き済み (対話 UI 解禁 2026-07-16 / Alt Screen 化 2026-07-17)。
// 現在は git log の pager と同じ挙動: Alt Screen 上でブラウズし、q で抜けると表示は
// 消えて何も残らない。残したいものは y (URL コピー)・o (ブラウザ)・--no-pager で。
// goroutine (fetch Cmd) は stdout へ直接書かず、結果を必ず tea.Msg として返す。
//
// CI job 一覧はリストへ行を差し込まず、対象コミット直下へ重ねるパネル (ポップアップ)
// で表示する。展開方式だと開閉のたびに後続行がずれて高さがガタつくため (ユーザー要望)。

const (
	fetchTimeout    = 10 * time.Second
	spinnerInterval = 80 * time.Millisecond // スピナー等の通常 tick (12.5fps。CPU 節約)
	scrollInterval  = 33 * time.Millisecond // scroll glide 中の高 FPS tick (~30fps。滑らかさ優先)
	// maxPanelJobs は job パネルに一度に表示する行数。超過分はパネル内でスクロールする。
	maxPanelJobs = 10
	// usageRefreshInterval は usage オーバーレイをバックグラウンド再取得する周期 (ユーザー要望
	// 2026-07-22)。/usage は LLM を呼ばないゼロコストなローカルコマンドなので毎分でも安価。
	// ⚠️ 実装で強制できない 2 つの制約 (変更時に再評価すること):
	//  1. fetchTimeout より必ず大きく保つ。小さくすると fetch が overlap し、fetchCmd の
	//     o.cancel 上書きで前回 fetch の cancel を取りこぼす (現状 10s < 60s で overlap しない)。
	//  2. usage_overlay.go boxLines のフッター文言「1分ごとに更新」がこの値に結合している。
	//     周期を変えるならフッター文言も揃えること (dim 表示・値は静かに差し替わる旨の明示)。
	usageRefreshInterval = time.Minute
)

// usageRefreshMsg は usage オーバーレイの定期リフレッシュ発火 (usageRefreshInterval ごと)。
type usageRefreshMsg struct{}

// usageRefreshTick は次回の usage リフレッシュを usageRefreshInterval 後に予約する tea.Cmd。
// Init で 1 本起動し、usageRefreshMsg ハンドラが毎回 1 本張り直すことで cron 型の単一チェーンに
// なる (発火ごとに +1 予約なので二重化しない)。
func usageRefreshTick() tea.Cmd {
	return tea.Tick(usageRefreshInterval, func(time.Time) tea.Msg { return usageRefreshMsg{} })
}

type ciResultMsg struct {
	batch CIBatch
	ghErr *GHError
}

// detailMsg はパネル表示時のオンデマンド取得 (キャッシュヒットで詳細が無い SHA) の結果。
type detailMsg struct {
	sha   string
	batch CIBatch
	ghErr *GHError
}

// basisMsg は実行中 job の ETA 用に「同名完了 job の Duration」を補うための、表示中
// コミット (Details 未取得のもの) の一括取得結果。targets は取得を要求した SHA 群
// (レスポンスに現れなかったものも loading 解除する)。詳細は maybeFetchETABasis。
type basisMsg struct {
	targets []string
	batch   CIBatch
	ghErr   *GHError
}

type tickMsg struct{}

// openURLMsg は job 詳細ページをブラウザで開いた結果。
type openURLMsg struct{ err error }

// editorClosedMsg は job ログを開いた nvim を閉じた結果 (e キー)。
type editorClosedMsg struct{ err error }

// runEditorCmd はテストで実 nvim を起動しないための差し替え点。tea.ExecProcess は
// bubbletea の描画を一旦止め、端末を nvim へ明け渡し、終了後に復帰する (エディタ起動用途の
// 標準経路)。
var runEditorCmd = func(cmd *exec.Cmd) tea.Cmd {
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return editorClosedMsg{err} })
}

// jobDetailMsg は job 詳細 (annotations / ログ tail) のオンデマンド取得の結果。
type jobDetailMsg struct {
	key   string // sha/jobIdx (取得中表示とキャッシュのキー)
	lines []string
	ghErr *GHError
}

// pushMsg は git push の実行結果 (b → y 確認後)。glogx の独自機能で、
// 本家 glog (read-only) には無い。
type pushMsg struct{ err error }

// pushPollMsg は push 直後ポーリングの周期タイマー。push した新規コミットは CI job が
// 走り出すまでタイムラグがあり、即 fetch すると「checks なし (StateNone, TTL 5分)」を
// 拾ってネガティブキャッシュ化するため、CI が見えるまで一定間隔で取り直す (ユーザー要望)。
type pushPollMsg struct{}

const (
	pushPollInterval    = 5 * time.Second
	pushPollMaxAttempts = 24 // 5s × 24 = 最長 2 分で諦める (その回の結果は保存しない)
)

// panelPollMsg は job パネル表示中の定期リフレッシュ (経過時間をライブで見ている
// ユーザー向けに job の状態・所要時間も追従させる。ユーザー要望 2026-07-20)。
// seq はパネルの開閉世代: 開き直しで古いタイマーが二重ポーリングにならないよう、
// 世代が一致するときだけ有効。
type panelPollMsg struct{ seq int }

const panelPollInterval = 3 * time.Second

// rerunMsg は CI job 再実行要求 (r → y 確認後の gh run rerun --job) の結果。glogx の独自機能。
// sha は対象コミット (パネルリフレッシュの照合用)。
type rerunMsg struct {
	sha string
	err error
}

// rerunPollGrace は rerun 直後にパネルへ与える猶予ポーリング回数 (panelPollInterval × 10 = ~30s)。
// rerun を要求してから GraphQL に queued/in_progress が映るまでラグがあり、その間は
// panelHasRunningJob が false でポーリングが止まってしまう (パネルの ✗ が固まったままになる)
// ため、実行中 job が見えるまでの間だけ空振りを許す。上限到達で諦める (反映は次の開き直しで)。
const rerunPollGrace = 10

// noPromptGitCmd は remote に触る git (push/pull) 用のコマンドを組む。GIT_TERMINAL_PROMPT=0
// で「認証情報が要るのに helper が無い」場合に /dev/tty へ対話プロンプトを出させず即エラーに
// する: bubbletea が同じ端末を raw mode で握っているため、git が tty を奪うと表示が壊れ入力
// 挙動が未定義になる (対話認証は TUI の外でやるべき作業)。タイムアウトは付けない — 正当な
// 巨大 push が遅い回線で中断される方が push 失敗として有害なため (レビュー K2)。
// pullMsg は git pull --rebase の実行結果 (u → y 確認後)。glogx の独自機能。
type pullMsg struct{ err error }

// updateMsg は `claude update` の実行結果 (C キー、確認なし即実行)。glogx の独自機能。
// before/after は update 前後の CLI バージョン (取得失敗時は空)。両方取れて差があれば
// "vX → vY"、同じなら「変更なし」を notice に出す。
type updateMsg struct {
	before string
	after  string
	err    error // 失敗時のみ。Error() は claude 出力の末尾行を含む
}

// prefixMsg は tmux prefix の取得結果 (起動時に 1 回、非同期)。
type prefixMsg struct{ key string }

// prMsg は commit に紐づく PR のオンデマンド取得の結果 (p キー)。
type prMsg struct {
	sha   string
	pr    *PRRef // nil = PR なし
	ghErr *GHError
}

// prStatusMsg は PR 詳細状態のオンデマンド取得の結果 (P キー, issue 021)。
type prStatusMsg struct {
	sha    string
	status *PRStatus // nil = PR なし
	ghErr  *GHError
}

// diffMsg はコミット diff のオンデマンド取得の結果 (d キー)。
type diffMsg struct {
	sha   string
	lines []string
	err   error
}

// loadCommitDiff はテストで実 git を叩かないための差し替え点。
var loadCommitDiff = LoadCommitDiff

type browseModel struct {
	commits        []Commit
	statuses       map[string]CIState // 表示用 (キャッシュ + 取得結果のマージ)
	fetched        map[string]CIState // API から取得した分 (終了後のキャッシュ保存用)
	details        map[string][]CheckDetail
	detailsLoading map[string]bool
	toFetch        []string
	repo           Repo
	hasRepo        bool
	ghErr          *GHError
	decor          *DecorColors
	oneline        bool
	colored        bool
	// showFrame は最外周フレーム (板 + ドロップシャドウ) 描画の有効フラグ (issue 025)。起動時固定
	// (!opts.NoFrame)。⚠️ 下の frame (int) はスピナーのフレームカウンタで別物 (名前衝突回避のため
	// bool 側を showFrame とした)。実際に描くかは frameActive() が端末サイズ下限も見て判定する。
	showFrame      bool
	frame          int
	width          int
	height         int
	cursor         int    // コミット index
	offset         int    // ビューポート先頭の行 index (論理 = カーソル可視化の着地点)
	offsetShown    int    // 描画に使う表示 offset (scrollAnim 中だけ offset と乖離し glide)
	scrollAnim     bool   // j/k のコミット単位スクロールを表示 offset で滑らせている最中か
	scrollFrom     int    // scroll glide 開始時の表示 offset (ease-in の進捗基点)
	scrollFrame    int    // scroll glide の経過フレーム数 (ease-in の進捗)
	panelSHA       string // job パネルを表示中のコミット SHA ("" = パネルなし)
	panelCursor    int    // パネル内で選択中の job index (-1 = タイトル行にフォーカス)
	panelPollSeq   int    // パネル開閉の世代 (panelPollMsg の有効性判定)
	panelRefresh   bool   // パネルの定期リフレッシュ実行中 (detailsLoading と別: 表示を「取得中」に落とさない)
	panelPollGrace int    // 実行中 job が見えなくてもポーリングを続ける残回数 (rerun 直後の反映ラグ吸収)
	panelPolling   bool   // panelPollMsg の自己更新チェーンが 1 本生きているか (maybeTick と同型の single-flight)
	copyOnDetail   string // Y で詳細未取得だった detailKey。jobDetailMsg 到着時にコピーして消す ("" = 予約なし)
	// job 詳細ポップアップ (annotations / ログ tail) の pager 状態と描画は jobDetailOverlay 型
	// (job_detail_overlay.go) に切り出す。panel-frame (panelSHA/panelCursor/poll/refresh) と ETA・
	// CI 取得は details/statuses/commits と構造的に結合するため browseModel に残す (詳細は同ファイル)。
	// cache キー (detailKey) はパネルのカーソル座標から借りる (identity 非所有) ので呼び出し側で注入。
	detailOv jobDetailOverlay
	prCache  map[string]*PRRef // sha → 紐づく PR (nil 格納 = 確認済みで PR なし)
	prBusy   map[string]bool   // PR 取得中の sha
	// PR 状態ポップアップ (P キー) の状態と描画は prStatusOverlay 型 (pr_status_overlay.go) に
	// 切り出し、ここは 1 フィールドだけ持つ。CI 行の整形はコミット状態を知る browseModel 側。
	prStatusOv prStatusOverlay
	// diff ポップアップ (d キー) の状態と描画は diffOverlay 型 (diff_overlay.go) に切り出し、
	// ここは 1 フィールドだけ持つ。ターゲット選定・非同期取得・URL コピーは境界をまたぐため
	// openDiff / handleDiffKey に薄く残す。
	diffOv diffOverlay
	// git push / pull --rebase / claude update の確認〜実行〜結果モーダルの状態機械は
	// actionModal 型 (action_modal.go) に切り出す。実行の orchestration (pushPoll 編成・
	// reloadAfterPull・結果整形) は CI/コミット状態と密結合なので browseModel 側に残す。
	actModal      actionModal
	pullAnimating bool            // pull 後に先頭へ増えた新規コミット行を上から降らせる演出中 (offset が進行度)
	opts          *Options        // pull 後のコミット再読込に使う (revs / max-count)
	pushPoll      map[string]bool // push 直後ポーリング対象の SHA (CI が見えたら外れる)
	pollAttempts  int             // push 直後ポーリングの試行回数 (上限で諦める)
	lastWarning   string          // w でコピーする直近の警告/エラー文字列 (トーストが消えても保持。issue 026)
	tmuxPrefix    string          // tmux prefix の bubbletea 表記 (例 "ctrl+t")。"" = tmux 外/不明で機能オフ
	prefixPending bool            // 直前のキーが tmux prefix。次の 1 キーを飲み込む
	verbatim      []Line          // git log 実出力の取り込み行 (nil = 自前レンダリング)

	// usage オーバーレイ (右上に Claude Code の /usage 残量を重ねる)。ユーザー要望 2026-07-21。
	// 状態と描画は usageOverlay 型 (usage_overlay.go) に切り出し、ここは 1 フィールドだけ持つ。
	usageOv usageOverlay

	// toast は右下に数秒だけ出す結果フィードバック (push/pull 完了)。自動消滅 (toast.go)。
	toast toast

	fetching bool
	ticking  bool // 80ms スピナー tick チェーンが 1 本生きているか (maybeTick の single-flight)
	// push 成功の演出 (startPushAnim)。演出が statuses の StateUnpushed を先に消していく
	// ため、演出後の CI ポーリング対象 (push 時点の tip) は pushAnimTip に捕捉しておく
	pushAnimating bool
	pushAnimTip   string
	pushAnimNext  time.Time // 次に境界を 1 段進める時刻 (tick 周期は 80/33ms で揺れるため時刻で刻む)
	// pushSlides は境界が通過したコミットの「右へ沈み込む」演出の開始時刻 (SHA → 開始)。
	// View 段の変換 (slideColumns) が参照し、pushSlideDuration 経過で tick が破棄する
	pushSlides map[string]time.Time
	done       bool
	fetch      tea.Cmd
	cancel     context.CancelFunc

	// lines() のメモ化。行リストの再構築は O(出力全行数) で、-p の巨大 patch では
	// キー 1 打ごとに数万行を組み直すことになるためキャッシュする。行内容を変えうる
	// 更新 (statuses/details のマージ・スピナーフレーム・幅変更) だけが無効化する。
	// カーソル移動・パネル開閉は View の窓側で重ねるだけなので無効化不要
	linesCache []Line
	linesValid bool
}

func newBrowseModel(commits []Commit, statuses map[string]CIState, toFetch []string, repo Repo, hasRepo bool, opts *Options, colored bool, width, height int) *browseModel {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	m := &browseModel{
		commits:        commits,
		statuses:       statuses,
		fetched:        map[string]CIState{},
		details:        map[string][]CheckDetail{},
		detailsLoading: map[string]bool{},
		detailOv:       newJobDetailOverlay(),
		prCache:        map[string]*PRRef{},
		prBusy:         map[string]bool{},
		prStatusOv:     newPRStatusOverlay(),
		diffOv:         newDiffOverlay(),
		toFetch:        toFetch,
		repo:           repo,
		hasRepo:        hasRepo,
		panelCursor:    -1,
		opts:           opts,
		oneline:        opts.Oneline,
		colored:        colored,
		showFrame:      !opts.NoFrame,
		width:          width,
		height:         height,
		fetching:       len(toFetch) > 0,
		cancel:         cancel,
		usageOv:        usageOverlay{visible: true},
	}
	if m.fetching {
		m.fetch = func() tea.Msg {
			defer cancel()
			batch, ghErr := FetchCIStatuses(ctx, ExecRunner, repo, toFetch)
			return ciResultMsg{batch: batch, ghErr: ghErr}
		}
	}
	return m
}

func (m *browseModel) Init() tea.Cmd {
	// tmux prefix の取得は非同期 (fork 1 本 ≈ 6ms を初期描画のクリティカルパスに乗せない)
	prefix := func() tea.Msg { return prefixMsg{key: loadTmuxPrefix()} }
	u := m.usageOv.fetchCmd()
	// IME 自動切替 (ime.go) に使う macism が未導入なら、起動時に error トーストで brew 導入を
	// 案内する (数秒で自動消滅)。ime.go 側は未導入でも no-op なので機能自体は壊れないが、IME が
	// 英数へ切り替わらない事実に気づけるよう能動的に案内する (ユーザー要望 2026-07-23)。
	if !macismInstalled() {
		m.showWarning("macism 未導入: brew tap laishulu/homebrew && brew install macism")
	}
	// usage を起動時に取得するため tick を常に起動する (取得中スピナーを回す。取得完了で
	// spinnerActive が false になり tick は自然に止まる)。CI fetch の有無に依らず起動する。
	// usageRefreshTick で 1 分ごとのバックグラウンド再取得チェーンも起動する (ユーザー要望)。
	// Claude Code の新バージョン検出 (claude_version.go)。バックグラウンド 1 回きりで、
	// 結果は claudeUpdateAvailableMsg (更新なし/失敗は nil Msg で無音)。
	ver := checkClaudeVersionCmd()
	if m.fetching {
		return tea.Batch(m.fetch, prefix, u, ver, m.maybeTick(), usageRefreshTick())
	}
	return tea.Batch(prefix, u, ver, m.maybeTick(), usageRefreshTick())
}

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return tickMsg{} })
}

// maybeTick は tick を single-flight で仕込む。既にチェーンが 1 本生きていれば nil を返して
// 二重チェーンを作らない。tea.Batch(cmd, maybeTick()) は Init・各 fetch 経路など多数に散らばり、
// 非同期処理が重なるたびに独立した自己増殖チェーンが恒久追加されて (push 直後ポーリングでは
// 最長 2 分間に ~48 本まで) 再描画/アニメが N 倍化していた (レビュー C1)。この single-flight で
// 全 tick 発行を 1 本に束ねる。⚠️ pushPoll/panelPoll の tea.Tick は別周期の独立タイマーなので
// maybeTick を通さない (それぞれ seq/guard で管理)。
//
// 周期は scroll glide 中だけ scrollInterval (~30fps) に上げて滑らかにし、それ以外は
// spinnerInterval (12.5fps) に落として CPU を節約する。チェーンは毎 tickMsg で maybeTick
// から張り直されるので、glide 開始/終了で周期が自動的に切り替わる。
func (m *browseModel) maybeTick() tea.Cmd {
	if m.ticking {
		return nil
	}
	m.ticking = true
	if m.scrollAnim || m.toast.animating() {
		return tickEvery(scrollInterval) // スライドを滑らかに (30fps)
	}
	return tickEvery(spinnerInterval)
}

// fetchCIStatusesCmd は targets の CI 状態取得を tea.Cmd にする。ctx/timeout/defer cancel の
// ボイラープレートを 1 箇所へ集約し、wrap で結果を各 msg (ciResult/detail/basis) に包む
// (レビュー U1)。同一 SHA 並行取得を避ける注意 (panelPollMsg / fetchPanelDetails のコメント)
// は呼び出し側のガードが担う。newBrowseModel の初期 fetch だけは m.cancel と ctx を共有して
// q 中断に使う意図的例外なので、この helper を通さず据え置く。
func fetchCIStatusesCmd(repo Repo, targets []string, wrap func(CIBatch, *GHError) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		batch, ghErr := FetchCIStatuses(ctx, ExecRunner, repo, targets)
		return wrap(batch, ghErr)
	}
}

// mergeCIBatch は CIBatch 応答を browseModel のキャッシュへ吸収する。fetched (終了時 SaveCache
// 用の負キャッシュ) と statuses (表示用) は常に同一 source を受け、details / prCache も一緒に
// 更新される — この「4 キャッシュを 1 単位で co-update する」不変条件を ciResult/detail/basis の
// 3 ハンドラから 1 箇所へ局所化する (第 5 の co-update map が増えても touch は 1 箇所)。PR は
// コミット行のバッジ表示と p キーの両方で使う。site 固有の invalidateLines / ghErr クリア /
// detailsLoading 解除 / panelCursor クランプ / pushPoll 掃除は各ハンドラに残す (吸収の関心事ではない)。
func (m *browseModel) mergeCIBatch(statuses map[string]CIState, details map[string][]CheckDetail, prs map[string]*PRRef) {
	maps.Copy(m.fetched, statuses)
	maps.Copy(m.statuses, statuses)
	maps.Copy(m.details, details)
	maps.Copy(m.prCache, prs)
}

// claudeUpdateToastDefer は起動時の先行トースト (macism 未導入 error 警告など) が消えるのを
// 待ってから version 通知を再送する間隔。toastHold (3s) + 出入りスライドより長めに取る。
const claudeUpdateToastDefer = 4 * time.Second

// claudeUpdateRetryMsg は先行トースト表示中に version 通知を 1 度だけ遅延再送する合図。
type claudeUpdateRetryMsg struct{ latest string }

// showOrDeferClaudeUpdate は「新バージョンあり」の info トーストを出す。ただし他のトースト
// (起動時の macism error 警告など) が表示中なら上書きせず、消えた頃に 1 度だけ遅延再送する。
// 単一スロット・後勝ちの toast 設計を歪めずに「重要度 error > info」を守るための調停:
// info 側が visible な先行トーストへ道を譲る。retry=true はその 1 回きりの再送で、まだ塞がって
// いれば諦める (macism 警告は毎起動出るので次回起動で version 通知を読める)。
// showWarning は失敗/警告トーストを出しつつ lastWarning に残す (w で表示が消えた後もコピー
// できるように。issue 026)。成功トースト (toast.show(…, true)) はこれを通さない — 成功文言で
// lastWarning が上書きされると直前のエラーがコピー不能になるため。失敗トーストの発行は必ず
// これを経由し、コピー対象を漏れなく捕捉する。
func (m *browseModel) showWarning(text string) {
	m.lastWarning = text
	m.toast.show(text, false)
}

func (m *browseModel) showOrDeferClaudeUpdate(latest string, retry bool) tea.Cmd {
	if m.toast.visible() {
		if retry {
			return nil // 再送してもまだ塞がっている: 今回は諦める
		}
		return tea.Tick(claudeUpdateToastDefer, func(time.Time) tea.Msg {
			return claudeUpdateRetryMsg{latest: latest}
		})
	}
	m.toast.show("Claude Code v"+latest+" が公開されています (C で更新)", true)
	return m.maybeTick()
}

func (m *browseModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.done {
		// 終了確定後に届く残メッセージは無視する (q での取得中断が
		// 「context canceled」警告として出るのを防ぐ)
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.invalidateLines()  // 幅で折り返し行数が変わる
		m.scrollAnim = false // resize 中の glide は破棄して即時 (表示 offset が stale になるため)
		m.ensureCursorVisible()
		return m, nil
	case tickMsg:
		m.ticking = false // このチェーンが 1 拍消費した。継続は下の maybeTick で単一に保つ
		if !m.spinnerActive() {
			return m, nil // アニメ対象なし: 再アームせずチェーンを終わらせる
		}
		if m.pullAnimating {
			m.advancePullAnim() // pull で増えた新規行を 1 行/フレームで上から降らせる
		}
		if m.scrollAnim {
			m.advanceScroll() // j/k のコミット単位スクロールを表示 offset で滑らせる
		}
		var pushRefetchCmd tea.Cmd
		if m.pushAnimating {
			pushRefetchCmd = m.advancePushAnim() // push 境界の罫線を 1 コミット/フレームで上へ
		}
		for sha, start := range m.pushSlides {
			if time.Since(start) >= pushSlideDuration {
				delete(m.pushSlides, sha) // 沈み込みが終わった区画は通常表示へ戻す
			}
		}
		var toastHoldCmd tea.Cmd
		if m.toast.animating() {
			// トーストの横スライド (右画面外との出入り) をカラム単位で 1 フレーム進める。
			// 入場完了時は holding へ移り toastHold 後の退場タイマーを返す。
			toastHoldCmd = m.toast.advance(m.colored)
		}
		m.frame++
		// list に毎フレーム変化する内容 (loading スピナー) が乗るのは fetch/pushPoll の 2 状態
		// だけ。他の spinnerActive 条件 (panelHasRunningJob/pullAnimating/detailsLoading/
		// jobDetailBusy/diffBusy) のスピナー・経過時間は panelLines/diffBoxLines 側 (lines() の
		// 外) で毎フレーム描かれるので、ここで list を無効化すると -p 巨大 patch を含む全行を
		// 80ms ごとに組み直すだけの無駄になる (レビュー C7)。offset を動かす pull アニメも
		// lines() は不変なので invalidate 不要 (View が窓を切り直す)
		if m.fetching || len(m.pushPoll) > 0 {
			m.invalidateLines()
		}
		return m, tea.Batch(m.maybeTick(), toastHoldCmd, pushRefetchCmd)
	case ciResultMsg:
		m.invalidateLines()
		m.ghErr = msg.ghErr
		// 応答に無かった SHA は unknown で埋める (fetched へ入れる = 終了時に SaveCache
		// される 30 秒の負キャッシュ)。q での中断 (fillUnknown) と違い、こちらは API の
		// 実際の返答に基づく確定
		filled := fillUnknownFetched(msg.batch.Statuses, m.toFetch)
		m.mergeCIBatch(filled, msg.batch.Details, msg.batch.PRs)
		m.fetching = false
		// 一括取得待ちでパネルを開いていた SHA の loading を解除する (結果が来なかった
		// SHA も含めて解除。details 不在は「(CI job 情報なし)」表示に落ちる)
		for _, sha := range m.toFetch {
			delete(m.detailsLoading, sha)
		}
		// push 直後ポーリング: CI がまだ見えない (none/unknown) SHA は結果を捨てて
		// (statuses から消してスピナーに戻し、fetched からも外してファイルキャッシュへの
		// ネガティブキャッシュ保存を防ぐ) 次の周期で取り直す。CI が見えたら対象から外す
		if len(m.pushPoll) > 0 {
			for sha := range m.pushPoll {
				switch m.statuses[sha] {
				case StatePending, StateSuccess, StateFailure, StateNeutral:
					delete(m.pushPoll, sha) // CI が見えた: 以降は通常のキャッシュ運用
				default:
					delete(m.statuses, sha)
					delete(m.fetched, sha)
				}
			}
			m.invalidateLines()
			if len(m.pushPoll) > 0 && m.pollAttempts < pushPollMaxAttempts {
				return m, tea.Batch(
					tea.Tick(pushPollInterval, func(time.Time) tea.Msg { return pushPollMsg{} }),
					m.maybeTick())
			}
			m.pushPoll = nil // 上限到達: スピナーは spinnerActive から外れて止まる
		}
		// 一括取得で実行中コミットの Details が入った場合も ETA basis を補充する
		// (パネルを取得中に開いていたケース)。
		return m, m.maybeFetchETABasis()
	case detailMsg:
		m.invalidateLines()
		delete(m.detailsLoading, msg.sha)
		wasRefresh := msg.sha == m.panelSHA && m.panelRefresh
		if msg.sha == m.panelSHA {
			m.panelRefresh = false
		}
		m.ghErr = msg.ghErr // 成功時 (nil) はクリア: ciResultMsg と揃える (sticky 警告の防止・レビュー C4)
		m.mergeCIBatch(msg.batch.Statuses, msg.batch.Details, msg.batch.PRs)
		// リフレッシュで job 数が縮んだ場合にフォーカスを範囲内へ戻す
		if msg.sha == m.panelSHA && m.panelCursor >= len(m.details[m.panelSHA]) {
			m.panelCursor = len(m.details[m.panelSHA]) - 1
		}
		// パネルを開いたコミットの Details が今届いた場合、実行中 job があれば ETA basis
		// (同名完了 job) の補充と定期リフレッシュの開始をここで行う (openPanel 時点では
		// details 未取得で判定できない)。リフレッシュ結果の到着では開始しない
		// (次回は panelPollMsg ハンドラ側が予約済み。二重 timer で加速するのを防ぐ)
		var poll tea.Cmd
		if msg.sha == m.panelSHA && !wasRefresh && m.panelHasRunningJob() {
			poll = m.ensurePanelPoll()
		}
		return m, tea.Batch(m.maybeFetchETABasis(), poll)
	case basisMsg:
		m.invalidateLines()
		m.ghErr = msg.ghErr // 成功時 (nil) はクリア: ciResultMsg と揃える (レビュー C4)
		m.mergeCIBatch(msg.batch.Statuses, msg.batch.Details, msg.batch.PRs)
		// レスポンスに現れなかった target も含めて loading 解除し、Details エントリを
		// 空スライスで確定させる (未設定のままだと同じ target を無限に取り直してしまう)。
		for _, sha := range msg.targets {
			delete(m.detailsLoading, sha)
			if _, ok := m.details[sha]; !ok {
				m.details[sha] = []CheckDetail{}
			}
		}
		return m, nil
	case jobDetailMsg:
		m.ghErr = msg.ghErr // 成功時 (nil) はクリア: ciResultMsg と揃える (レビュー C4)。
		// ⚠️ ghErr は共有 sticky 警告なので detailOv.receive に閉じず browseModel で無条件代入する。
		// receive は busy 落とし・cache 格納・(今開いている詳細なら) 末尾スクロールを担う。currentKey は
		// live な detailKey() を渡す (snapshot 禁止: リフレッシュで panelCursor がクランプされ得るため)。
		m.detailOv.receive(msg, m.detailKey(), m.visibleDetailRows())
		// Y で詳細取得を待っていたら、到着したこの内容をコピーする (issue 020)。取得失敗
		// (ghErr) は上の sticky 警告に任せ、予約だけ静かに破棄する。
		// ⚠️ 予約後にフォーカスが動いていたら (detailKey() != msg.key) コピーしない: msg.lines は
		// 予約時の job のログだが、focusedJob() は現フォーカスを返すため、貼ると「別 job のヘッダに
		// 旧 job の本文」という silent 誤コピーになる。詳細ポップアップを閉じただけ (closePanel を
		// 経ない) でカーソル移動できる経路があり、copyOnDetail が残るため起きる (レビュー確定 high)。
		if m.copyOnDetail == msg.key {
			m.copyOnDetail = ""
			if job, ok := m.focusedJob(); ok && m.detailKey() == msg.key && msg.ghErr == nil && len(msg.lines) > 0 {
				m.copyJobContextLines(job, msg.lines)
			}
		}
		return m, nil
	case prMsg:
		delete(m.prBusy, msg.sha)
		if msg.ghErr != nil {
			// 一時エラーをキャッシュすると「PR はありません」という誤答が固定される
			// (次の p で再試行させる) ため、キャッシュは成功時のみ
			m.showWarning("PR の取得に失敗しました: " + firstLine(msg.ghErr.Warning()))
			return m, m.maybeTick()
		}
		m.prCache[msg.sha] = msg.pr
		m.invalidateLines() // コミット行の PR バッジに反映
		if msg.pr == nil {
			m.toast.show("このコミットに紐づく PR はありません", false)
			return m, m.maybeTick()
		}
		m.toast.show(fmt.Sprintf("PR #%d を開きます", msg.pr.Number), true)
		return m, tea.Batch(m.openURLCmd(msg.pr.URL), m.maybeTick())
	case prStatusMsg:
		// receive は当該 sha が表示中ならエラー時に close するため、sha 一致は receive の前に捕捉する。
		// notice は「今表示中の対象」の失敗のときだけ出す: 別 sha へ移った後に届く遅延エラーで
		// 無関係な失敗 notice を被せない (レビュー確定 low)。
		wasCurrent := msg.sha == m.prStatusOv.sha
		m.prStatusOv.receive(msg.sha, msg.status, msg.ghErr)
		if msg.ghErr != nil && wasCurrent {
			// 一時エラーはキャッシュしない (receive 側)。理由はトーストで伝える
			m.showWarning("PR の取得に失敗しました: " + firstLine(msg.ghErr.Warning()))
		}
		return m, m.maybeTick()
	case diffMsg:
		if err := m.diffOv.receive(msg); err != nil {
			m.showWarning("diff の取得に失敗しました: " + firstLine(err.Error()))
		}
		return m, m.maybeTick()
	case prefixMsg:
		m.tmuxPrefix = msg.key
		return m, nil
	case usageMsg:
		m.usageOv.handle(msg)
		return m, nil
	case claudeUpdateAvailableMsg:
		return m, m.showOrDeferClaudeUpdate(msg.latest, false)
	case claudeUpdateRetryMsg:
		return m, m.showOrDeferClaudeUpdate(msg.latest, true)
	case toastMsg:
		m.toast.startLeaving(msg) // 静止明け: 退場アニメへ (世代一致時のみ)。maybeTick で tick 再開
		return m, m.maybeTick()
	case usageRefreshMsg:
		// バックグラウンドで /usage を再取得し、次回リフレッシュを予約する。取得中も snap は
		// 消さないので loading() は false のままスピナーに落ちず、表示は last-good を維持する
		// (handle の不変条件)。表示/非表示に依らず回し、隠れていても最新値を用意しておく。
		return m, tea.Batch(m.usageOv.fetchCmd(), usageRefreshTick())
	case panelPollMsg:
		if msg.seq != m.panelPollSeq || m.panelSHA == "" {
			return m, nil // パネルが閉じた/開き直された後の残タイマーは破棄
		}
		if m.panelHasRunningJob() {
			m.panelPollGrace = 0 // 実行中 job が見えた: 猶予は役目を終え、以降は通常の追従
		} else {
			if m.panelPollGrace <= 0 {
				m.panelPolling = false // このチェーンは停止する (開き直し/次の開始点で再アーム)
				return m, nil          // 全 job 完了: 追従の必要が無いのでポーリング終了
			}
			m.panelPollGrace-- // rerun 直後: 実行中 job が GraphQL に映るまで空振りを許す
		}
		next := m.schedulePanelPoll()
		// 実行中の一括取得/リフレッシュと重ねない (同一 SHA への GraphQL 並行は
		// 完了順で statuses/details が上書きされる。fetchPanelDetails と同じ注意)
		if m.panelRefresh || m.detailsLoading[m.panelSHA] || (m.fetching && slices.Contains(m.toFetch, m.panelSHA)) {
			return m, next
		}
		m.panelRefresh = true // detailsLoading と違い表示は「取得中」に落とさない (チラつき防止)
		sha := m.panelSHA
		refresh := fetchCIStatusesCmd(m.repo, []string{sha}, func(b CIBatch, e *GHError) tea.Msg {
			return detailMsg{sha: sha, batch: b, ghErr: e}
		})
		return m, tea.Batch(refresh, next)
	case pushPollMsg:
		if len(m.pushPoll) == 0 || m.fetching {
			return m, nil // fetching 中 (別経路の取得が進行) は次の ciResultMsg 側で判定する
		}
		m.pollAttempts++
		targets := slices.Collect(maps.Keys(m.pushPoll))
		m.toFetch = targets
		m.fetching = true
		fetch := fetchCIStatusesCmd(m.repo, targets, func(b CIBatch, e *GHError) tea.Msg {
			return ciResultMsg{batch: b, ghErr: e}
		})
		return m, tea.Batch(fetch, m.maybeTick())
	case pullMsg:
		m.actModal.pulling = false
		if msg.err != nil {
			m.showWarning("pull に失敗: " + firstLine(msg.err.Error()))
			return m, m.maybeTick()
		}
		// 成功トーストを右下にせり上げつつ全面リロード (アニメで画面が動いてもトーストは数秒残る)。
		m.toast.show("pull --rebase しました", true)
		return m, tea.Batch(m.reloadAfterPull(), m.maybeTick())
	case rerunMsg:
		m.actModal.rerunning = false
		if msg.err != nil {
			m.showWarning("再実行に失敗: " + firstLine(msg.err.Error()))
			return m, m.maybeTick()
		}
		m.toast.show("CI を再実行します", true)
		// パネルを開いたままなら猶予ポーリングで追従する (rerun が GraphQL に映るまでのラグは
		// rerunPollGrace のコメント参照)。映れば panelHasRunningJob → 既存の定期リフレッシュへ
		// 自然に引き継がれる。パネルが閉じられていれば何もしない (次の開き直しで最新を取る)
		var poll tea.Cmd
		if msg.sha == m.panelSHA && m.panelSHA != "" {
			m.panelPollGrace = rerunPollGrace
			poll = m.ensurePanelPoll() // 既にチェーンが生きていれば nil (二重化しない)
		}
		return m, tea.Batch(poll, m.maybeTick())
	case updateMsg:
		m.actModal.updating = false
		// 結果はダイアログで出す (何かキーで閉じる。ユーザー要望 2026-07-22)。バージョンが
		// 上がったのか latest だったのかを一目で分かるようにする。
		switch {
		case msg.err != nil:
			m.actModal.updateResult = "更新に失敗しました\n" + firstLine(msg.err.Error())
		case msg.before != "" && msg.after != "" && msg.before != msg.after:
			m.actModal.updateResult = "バージョンが上がりました\nv" + msg.before + " → v" + msg.after
		case msg.before != "" && msg.before == msg.after:
			m.actModal.updateResult = "すでに最新版です (v" + msg.before + ")"
		case msg.after != "":
			m.actModal.updateResult = "現在のバージョン: v" + msg.after // before 不明で比較できず
		default:
			m.actModal.updateResult = "update を実行しました" // 前後とも取得できず
		}
		return m, nil
	case pushMsg:
		m.actModal.pushing = false
		if msg.err != nil {
			m.showWarning("push に失敗: " + firstLine(msg.err.Error()))
			return m, m.maybeTick()
		}
		m.toast.show("push しました", true)
		if !m.hasRepo || len(m.commits) == 0 {
			return m, m.maybeTick() // 再取得先が無くてもトーストは出す (アニメ tick を回す)
		}
		if m.startPushAnim() {
			return m, m.maybeTick() // 演出完了後 (advancePushAnim) に refetchAfterPush へ進む
		}
		return m, tea.Batch(m.refetchAfterPush(), m.maybeTick())
	case openURLMsg:
		if msg.err != nil {
			m.showWarning("ブラウザを開けませんでした: " + firstLine(msg.err.Error()))
		}
		return m, m.maybeTick()
	case editorClosedMsg:
		// nvim を閉じて復帰。stdin 渡しなのでファイルは残らず、バッファも破棄済み
		if msg.err != nil {
			m.showWarning("nvim を開けませんでした: " + firstLine(msg.err.Error()))
		}
		return m, m.maybeTick()
	case tea.KeyMsg:
		// 高速連打やパイプ入力では複数の文字キーが 1 つの KeyMsg (Runes 長 > 1) に
		// まとまって届く。分解せず msg.String() だけ見ると "hhq" のような未知キー扱いに
		// なり、以降の操作が全て無視されたように見える (pty スモークで実測) ため、
		// 1 文字ずつのキー入力として順に処理する
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 {
			var cmds []tea.Cmd
			for _, r := range msg.Runes {
				_, cmd := m.handleKey(string(r))
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
				if m.done {
					break
				}
			}
			return m, tea.Batch(cmds...)
		}
		// handleKey はキー経路の唯一の入口。ここで maybeTick を必ず束ねることで、ハンドラや
		// openX()/copyX() の内部で出したトースト (バリデーション失敗・コピー結果など) が
		// 呼び出し側で return m, nil されてもアニメ tick が確実に回る (トーストが shown=0 のまま
		// 凍って見えない事故を防ぐ)。maybeTick は single-flight で冪等なので二重には走らない。
		model, cmd := m.handleKey(msg.String())
		return model, tea.Batch(cmd, m.maybeTick())
	}
	return m, nil
}

func (m *browseModel) handleKey(key string) (tea.Model, tea.Cmd) {
	// C-g は即終了: tmux の C-g popup (bind -n C-g) をトグル風に開閉するため
	// (開くキーと同じキーで閉じる)。本家 glog には無い割当。
	if key == "ctrl+c" || key == "ctrl+g" {
		switch {
		case m.actModal.updating:
			// 自己バイナリ更新の中断は CLI を壊しうるので常にブロック (ユーザー選定 2026-07-22)。
			// escape は updateTimeout のみ。モーダルに「完了まで終了できません」を出す。
			return m, nil
		case m.actModal.pushing || m.actModal.pulling:
			// 途中終了は不整合 (特に pull --rebase の mid-rebase 状態) を招くので 1 回目はブロック。
			// ただし stall で永久に閉じられなくならないよう、2 回目の Ctrl-C で強制終了する
			// (quit() の actModal.stop() が走行中 git を cancel。ユーザー選定 2026-07-23)。
			if m.actModal.forceQuitArmed {
				return m.quit()
			}
			m.actModal.forceQuitArmed = true
			return m, nil
		default:
			return m.quit()
		}
	}
	// git push/pull/update の確認・実行中・警告・結果ダイアログは action モーダルが捌く
	// (警告/結果の dismiss・確認 y/N・実行中のキー無視。判定順は actionModal.handleKey 側)。
	// 実行を伴う確認 y は action (実行 tea.Cmd) を載せて返すので maybeTick と束ねる。
	if consumed, action := m.actModal.handleKey(key); consumed {
		if action != nil {
			return m, tea.Batch(action, m.maybeTick())
		}
		return m, nil
	}
	// tmux prefix の誤爆フィードバック: popup 表示中は tmux がキーを処理しない
	// (display-popup はモーダル) ため、window 移動しようとした prefix+n/p はここへ
	// 素通りしてくる。無言だと「効かない」だけで理由が分からないので案内を出し、
	// prefix に続く 1 キーは飲み込む (C-t p が PR オープン等に化ける誤爆の防止)。
	// prefix 連打 (tmux のリテラル送信の癖) は pending を張り直して同じ案内を出す。
	// 確認モーダル (y/N) 中はここへ来ない (上のモーダル処理が先): モーダル内では
	// 「y 以外の任意キー = キャンセル」というモーダルの語彙を優先する (セルフレビュー指摘)。
	// 対象キーはハードコードせず起動時に tmux サーバへ聞いた現在値 (prefixMsg)。
	// tmux 外や取得失敗では tmuxPrefix="" のままこの機能ごと無効になる。ユーザー要望。
	if m.prefixPending && key != m.tmuxPrefix {
		m.prefixPending = false
		// 右下トーストで通知する (中央ダイアログだと操作を遮って重い、とのユーザー要望 2026-07-24)
		m.toast.show("window 操作は C-g で popup を閉じてから prefix+"+key, false)
		return m, m.maybeTick()
	}
	if m.tmuxPrefix != "" && key == m.tmuxPrefix {
		m.prefixPending = true
		m.toast.show("tmux prefix は popup では効きません (C-g で閉じてから)", false)
		return m, m.maybeTick()
	}
	// usage オーバーレイのトグル / dismiss。モーダル (push/pull 確認・pushWarn)・prefix・
	// 実行中ガードを素通りしないよう必ずそれらの後に置く: 先頭に置くと U が push 確認を
	// キャンセルし損ねて残った確認へ Enter で誤 push する footgun になる (レビュー指摘 2026-07-21)。
	// U は明示トグル (取得中なら spinner を回し直す)。それ以外のナビゲーションキーは「起動時
	// グランス」を引っ込める副作用だけ持たせ、キー本来の動作は下の dispatch/switch で続行する。
	if key == "U" {
		m.usageOv.toggle()
		if m.usageOv.visible {
			return m, m.maybeTick()
		}
		return m, nil
	}
	m.usageOv.dismiss()
	// emacs 流の水平移動エイリアス (C-n/C-p = ↓/↑ は各ビューで対応済み)。ここで
	// 正規化するので全ビュー (一覧/パネル/詳細/diff) に一括で効く。
	// ⚠️ 本家 glog と異なり C-b は ← の別名ではない (push を C-b → b に変えた名残で未割当)
	if key == "ctrl+f" {
		key = "right"
	}
	// diff ポップアップ表示中はスクロール/閉じる操作だけを受ける (最前面のモーダル)
	if m.diffOv.visible() {
		return m.handleDiffKey(key)
	}
	// PR 状態ポップアップ表示中も同様にモーダル (o/y/閉じるだけを受ける)
	if m.prStatusOv.visible() {
		return m.handlePRStatusKey(key)
	}
	// b = push / u = pull --rebase (どちらも y/N 確認へ)。glogx の独自機能。
	// diff 表示中は b = 半ページ戻るなので、diff のディスパッチより後で拾う
	// (一覧/パネル/詳細では b/u は未使用。C-u の半ページ上とは別キー)
	if key == "b" {
		return m, m.confirmPush()
	}
	if key == "u" {
		m.actModal.askPull()
		return m, nil
	}
	// C = claude update (確認なし即実行。ユーザー選定 2026-07-22)。glogx の独自機能で
	// U=usage と並ぶ大文字の「Claude Code メタ操作」。実行中は spinner モーダルを出す。
	if key == "C" {
		return m, tea.Batch(m.actModal.startUpdate(), m.maybeTick())
	}
	// w = 直近の警告/エラーをクリップボードへコピー (issue 026)。トーストは数秒で消えるが
	// lastWarning は保持しているので消えた後でもコピーできる。ghErr (CI 取得失敗の sticky
	// 警告) は lastWarning に無くても hint に出続けているので fallback で拾う。tmux popup 内では
	// copy-mode に入れないため、pbcopy 直書きの copyToClipboard が唯一の取り出し口 (y/Y と同じ)。
	if key == "w" {
		warn := m.lastWarning
		if warn == "" && m.ghErr != nil {
			warn = m.ghErr.Warning()
		}
		if warn == "" {
			// コピーできる警告が無い旨は error トーストで出す (ユーザー要望 2026-07-23)。lastWarning は
			// 汚さない (この文言自体はコピー対象でないため showWarning は通さない)。
			m.toast.show("コピーできる警告はありません", false)
			return m, m.maybeTick()
		}
		if err := copyToClipboard(warn); err != nil {
			// コピー失敗は error トーストで出すが lastWarning は汚さない (コピー対象の警告を
			// 上書きして w の再試行を潰さないため。848 と同じ理由で showWarning は通さない)。
			m.toast.show("コピーに失敗しました: "+firstLine(err.Error()), false)
			return m, m.maybeTick()
		}
		m.toast.show("警告をコピーしました", true)
		return m, m.maybeTick()
	}
	// q はビューのスタックを 1 段戻る (tig 流。ユーザー要望): 詳細 → job 一覧 →
	// コミット一覧、と閉じていき、最上位でだけ終了。即終了したいときは Ctrl-C
	if key == "q" {
		switch {
		case m.detailOv.visible():
			m.detailOv.close()
		case m.panelSHA != "":
			m.closePanel()
		default:
			return m.quit()
		}
		return m, nil
	}
	if m.panelSHA != "" {
		return m.handlePanelKey(key)
	}
	switch key {
	case "esc":
		return m.quit()
	case "j", "down", "ctrl+n":
		prev := m.offset
		m.cursor = clampIdx(m.cursor+1, len(m.commits))
		m.ensureCursorVisible()
		return m, m.startScrollAnim(prev)
	case "k", "up", "ctrl+p":
		prev := m.offset
		m.cursor = clampIdx(m.cursor-1, len(m.commits))
		m.ensureCursorVisible()
		return m, m.startScrollAnim(prev)
	case "g", "home":
		m.cursor = 0
		m.offset = 0
		m.scrollAnim = false // ジャンプは即時 (glide 対象外)
	case "G", "end":
		m.cursor = clampIdx(len(m.commits)-1, len(m.commits))
		m.ensureCursorVisible()
		m.scrollAnim = false
	case "ctrl+d", "pgdown":
		m.offset = m.clampOffset(m.offset + m.pageSize()/2)
		m.scrollAnim = false
	case "ctrl+u", "pgup":
		m.offset = m.clampOffset(m.offset - m.pageSize()/2)
		m.scrollAnim = false
	case "enter", " ", "l", "right", "tab":
		return m, m.openPanel()
	case "y":
		m.copyFocusURL()
	case "p":
		return m, m.openPR()
	case "P":
		return m, m.openPRStatus()
	case "d":
		return m, m.openDiff()
	case "o":
		return m, m.openCommitURL()
	}
	return m, nil
}

// confirmPush は push 確認 (y/N) に入る。未 push が 1 件も無ければ確認を出さない
// (誤爆防止と「push 済みなのに聞かれる」違和感の回避)。
func (m *browseModel) confirmPush() tea.Cmd {
	if m.unpushedCount() == 0 {
		m.actModal.pushWarn = "未 push のコミットはありません" // hint 行でなくモーダルで (ユーザー要望)
		return nil
	}
	m.actModal.pushConfirm = true
	return nil
}

// reloadAfterPull は pull --rebase 成功後の全面リロード。rebase でローカル SHA が
// 変わりうるため、コミット列・push 状態・CI・派生キャッシュ (details/PR/diff/job 詳細)
// をすべて取り直す (部分更新は旧 SHA の残骸が混ざる)。
func (m *browseModel) reloadAfterPull() tea.Cmd {
	commits, err := LoadCommits(m.opts, m.colored)
	if err != nil {
		m.showWarning("pull 後の再読込に失敗しました: " + firstLine(err.Error()))
		return nil
	}
	// pull 前の SHA 集合。pull 後に先頭へ増えた新規コミット数を「既知 SHA に当たるまで」で
	// 数え、アニメーションの対象行数を決める (rebase で SHA が書き換わった場合も破綻しない)。
	oldSHAs := make(map[string]struct{}, len(m.commits))
	for _, c := range m.commits {
		oldSHAs[c.SHA] = struct{}{}
	}
	m.commits = commits
	shas := make([]string, len(commits))
	for i, c := range commits {
		shas[i] = c.SHA
	}
	m.statuses, m.toFetch, m.repo, m.hasRepo, _ = planStatuses(m.opts, shas)
	m.details = map[string][]CheckDetail{}
	m.detailsLoading = map[string]bool{}
	m.detailOv.reset() // job 詳細ログキャッシュも破棄 (旧 SHA の残骸を持ち越さない)
	m.prCache = map[string]*PRRef{}
	m.prBusy = map[string]bool{}
	m.prStatusOv.reset() // 旧 SHA の PR 詳細キャッシュも破棄
	m.diffOv.reset()
	m.closePanel()
	m.pushPoll = nil
	m.scrollAnim = false      // pull リロードは pull アニメ側が担うので j/k glide は破棄
	m.cursor, m.offset = 0, 0 // カーソルは新規コミットの先頭へ (ユーザー要望 2026-07-20)
	if !m.oneline {
		m.verbatim = nil
		if raw, dispErr := LoadLogDisplay(m.opts, m.colored); dispErr == nil {
			m.verbatim = VerbatimLines(raw, commits) // 照合失敗は nil = 自前レンダリングへ
		}
	}
	m.invalidateLines()
	// 先頭に増えた新規コミット行を上から降らせる演出。startPullAnim が offset を新規行数だけ
	// 手前 (下スクロール位置) へ置き、tick で 1 行/フレームずつ 0 へ戻すと新規行が上から入り
	// 既存行が下へずれて見える。アニメしないと決まったときだけ即カーソル可視化に落とす
	m.startPullAnim(oldSHAs)
	if !m.pullAnimating {
		m.ensureCursorVisible()
	}
	if !m.hasRepo || len(m.toFetch) == 0 {
		m.fetching = false
		if m.pullAnimating {
			return m.maybeTick() // CI 取得は無いが、アニメーションのために tick を回す
		}
		return nil
	}
	m.fetching = true
	fetch := fetchCIStatusesCmd(m.repo, m.toFetch, func(b CIBatch, e *GHError) tea.Msg {
		return ciResultMsg{batch: b, ghErr: e}
	})
	return tea.Batch(fetch, m.maybeTick())
}

// pullAnimMaxLines は pull アニメで降らせる最大行数。大量 pull でも待ちが伸びすぎないよう
// 「頭だけ」流し、超過分は最初から所定位置に置く (ユーザー要望 2026-07-20)。
const pullAnimMaxLines = 8

// startPullAnim は pull 後に先頭へ増えた新規コミットの行数を求め、あれば offset を
// その分 (上限 pullAnimMaxLines) だけ下げてアニメーションを開始する。tick は
// reloadAfterPull / tickMsg 側で回す。
func (m *browseModel) startPullAnim(oldSHAs map[string]struct{}) {
	newCommits := 0
	for _, c := range m.commits {
		if _, ok := oldSHAs[c.SHA]; ok {
			break // 既知コミットに到達 = ここから下は元からある分
		}
		newCommits++
	}
	if newCommits == 0 {
		return
	}
	// 新規コミットが占める行数 (medium 表示では 1 コミット複数行)。最初の「既存コミット」の
	// 行 index が、そのまま先頭からの新規行数になる。全行が新規で既存が下に無いなら
	// 押し下げる相手がいないのでアニメしない (newLines が 0 のまま)
	lines := m.lines()
	newLines := 0
	for i, l := range lines {
		if l.CommitIdx >= newCommits {
			newLines = i
			break
		}
	}
	// offset スクロールで新規行を画面外上部に隠す方式のため、リスト全体が画面に収まる
	// (スクロール不能な) 短いリストではアニメできない → 即表示にフォールバック
	if newLines == 0 || len(lines) <= m.pageSize() {
		return
	}
	m.offset = min(newLines, pullAnimMaxLines)
	m.pullAnimating = true
}

// advancePullAnim は pull アニメーションを 1 行分進める (1 フレーム = tick 1 回)。
// offset を 0 に向けて減らすと、新規行が上から降りて既存行が下へずれる。
func (m *browseModel) advancePullAnim() {
	m.offset--
	if m.offset <= 0 {
		m.offset = 0
		m.pullAnimating = false
		m.ensureCursorVisible()
	}
}

// pushAnimMaxSteps は push 演出で 1 段ずつ流す最大コミット数。大量 push でも
// 待ちが伸びないよう頭打ちにし、超過分は開始時に即切り替える (pullAnimMaxLines と同型)。
const pushAnimMaxSteps = 8

// pushAnimStep は境界が 1 コミット上がる間隔。80ms/段では目で追えない
// (ユーザーフィードバック 2026-07-23) ため、1 段ずつ確実に視認できる速さにする。
const pushAnimStep = 600 * time.Millisecond

// pushSlideDuration は境界通過したコミット区画が右へ沈んで戻ってくるまでの時間。
// pushAnimStep より長いので複数コミットの push では沈み込みが波状に重なる。
const pushSlideDuration = time.Second

// startPushAnim は push 成功の演出を開始する。未 push だったコミットを古い順に
// 1 コミット/フレームで取得中 (spinner) 表示へ切り替えていくと、insertPushBoundary の
// 境界罫線が 1 段ずつ上へスライドし、先頭サマリの ↑N が減っていき、最後に
// (all pushed ✓) へ着地する (描画側は無変更で、statuses の遷移だけで演出が成立する)。
// 未 push が無ければ何もせず false (呼び出し側が即 refetchAfterPush へ)。
func (m *browseModel) startPushAnim() bool {
	var unpushed []string // 新しい順
	for _, c := range m.commits {
		if m.statuses[c.SHA] == StateUnpushed {
			unpushed = append(unpushed, c.SHA)
		}
	}
	if len(unpushed) == 0 {
		return false
	}
	m.pushAnimTip = unpushed[0]
	for len(unpushed) > pushAnimMaxSteps {
		last := len(unpushed) - 1
		delete(m.statuses, unpushed[last])
		unpushed = unpushed[:last]
	}
	m.invalidateLines()
	m.pushAnimating = true
	m.pushAnimNext = time.Now().Add(pushAnimStep)
	return true
}

// advancePushAnim は pushAnimStep 経過ごとに push 演出を 1 コミット分進める。
// 最も古い StateUnpushed を消すと境界が 1 コミット上がる。全部消えたら演出終了で、
// 本来の後処理 (CI 全件再取得) へ進む cmd を返す。
func (m *browseModel) advancePushAnim() tea.Cmd {
	if time.Now().Before(m.pushAnimNext) {
		return nil
	}
	m.pushAnimNext = time.Now().Add(pushAnimStep)
	for i := len(m.commits) - 1; i >= 0; i-- {
		if m.statuses[m.commits[i].SHA] == StateUnpushed {
			delete(m.statuses, m.commits[i].SHA)
			if m.pushSlides == nil {
				m.pushSlides = map[string]time.Time{}
			}
			m.pushSlides[m.commits[i].SHA] = time.Now() // 通過した区画の沈み込みを開始
			m.invalidateLines()
			return nil
		}
	}
	m.pushAnimating = false
	return m.refetchAfterPush()
}

// slideColumns は push 演出の「origin に吸い込まれる」沈み込み: 境界が通過したコミットの
// 区画 (ヘッダー行から次のコミットまで) を、画面幅の半分まで右へ滑らせて戻す (ユーザー要望
// 2026-07-23「50%くらい右に埋まる」)。返り値は行 index → 右オフセット列数 (演出なしは nil)。
// sin カーブで 0 → 半幅 → 0 と往復し、区画の判定はヘッダー行起点で行う (罫線行は CommitIdx
// を持つがヘッダー行に後続しないため巻き込まない)。
func (m *browseModel) slideColumns(lines []Line) map[int]int {
	if len(m.pushSlides) == 0 {
		return nil
	}
	depth := m.contentWidth() / 2
	byCommit := map[int]int{}
	for i, c := range m.commits {
		start, ok := m.pushSlides[c.SHA]
		if !ok {
			continue
		}
		p := float64(time.Since(start)) / float64(pushSlideDuration)
		if p < 0 || p >= 1 {
			continue
		}
		if off := int(float64(depth) * math.Sin(math.Pi*p)); off > 0 {
			byCommit[i] = off
		}
	}
	if len(byCommit) == 0 {
		return nil
	}
	cols := map[int]int{}
	cur, active := -1, 0
	for i, l := range lines {
		if l.Header {
			cur = l.CommitIdx
			active = byCommit[cur]
		} else if l.CommitIdx != cur {
			active = 0 // 区画を抜けた (罫線行や次コミットへの切り替わり)
		}
		if active > 0 {
			cols[i] = active
		}
	}
	return cols
}

// refetchAfterPush は push 後の CI 再取得。表示中リスト全体の CI 状態を破棄して取り直す
// (ユーザー要望 2026-07-19: push で CI が走り出すため、起動時キャッシュ由来の表示は丸ごと
// 古くなる)。statuses から消す → スピナー表示に戻り、toFetch 差し替えで一括取得と同じ経路
// (ciResultMsg) に乗せる。取得結果は fetched 経由で終了時に SaveCache へマージされ、
// ファイルキャッシュ側も新しい観測で上書きされる。
// ポーリング対象は push の先頭 (tip = 最新の unpushed) だけ (ユーザー要望 2026-07-19)。
// CI は push イベントの head commit にしか走らないのが普通で、途中のコミットまで対象に
// すると CI が永遠に見えず上限までスピナーが回り続ける。途中コミットの「checks なし (–)」
// は本物なので通常どおり取得・キャッシュする。tip は演出が statuses を先に消すため
// pushAnimTip (startPushAnim が捕捉) を優先する。
func (m *browseModel) refetchAfterPush() tea.Cmd {
	m.pushPoll = map[string]bool{}
	m.pollAttempts = 0
	if m.pushAnimTip != "" {
		m.pushPoll[m.pushAnimTip] = true
		m.pushAnimTip = ""
	}
	var all []string
	for _, c := range m.commits {
		if len(m.pushPoll) == 0 && m.statuses[c.SHA] == StateUnpushed {
			m.pushPoll[c.SHA] = true // commits は新しい順なので最初の unpushed = tip
		}
		all = append(all, c.SHA)
		delete(m.statuses, c.SHA)
		delete(m.details, c.SHA)
	}
	m.invalidateLines()
	m.toFetch = all
	m.fetching = true
	return fetchCIStatusesCmd(m.repo, all, func(b CIBatch, e *GHError) tea.Msg {
		return ciResultMsg{batch: b, ghErr: e}
	})
}

// startScrollAnim は j/k でビューポートがコミット単位に動いたとき、表示 offset (offsetShown)
// を旧位置 prev から論理 offset へ数フレームで滑らせる (ユーザー要望「にゅっと」)。
// アニメの積み上げは「押した分だけ遅れて動く」最悪の体感を生むので、連打 (既に scrollAnim)・
// pull アニメ中はアニメせず即時にする (render は m.offset に戻る)。呼び出しは j/k の 1 コミット
// 移動だけ (g/G・PgDn は元々 snap 経路) なので offset ジャンプはコミット 1 個ぶんに収まり、
// ease-out が距離でなく時間 (~2 フレーム) を抑えるため、背高コミット (長メッセージ・stat/patch)
// でも即着地する。高さで animate/snap が変わる違和感を避けるため行数キャップは設けない
// (ユーザー要望 2026-07-21)。論理 offset は ensureCursorVisible が既に動かしているので触らない。
func (m *browseModel) startScrollAnim(prev int) tea.Cmd {
	if m.offset == prev {
		return nil // カーソルが画面内: ビューポートは動いていない
	}
	if m.scrollAnim || m.pullAnimating {
		m.scrollAnim = false // 連打/pull アニメ中は積まず即時
		return nil
	}
	m.offsetShown = prev
	m.scrollFrom = prev
	m.scrollFrame = 0
	m.scrollAnim = true
	return m.maybeTick()
}

// scrollAnimFrames は scroll glide の総フレーム数 (× scrollInterval 33ms ≒ 200ms)。
// 少ないほど速い。30fps 化 (12.5→30fps) に合わせて 3→6 に増やし、同程度の duration で
// ease-in カーブの刻みを細かく = 滑らかにした。
const scrollAnimFrames = 6

// advanceScroll は scroll glide を 1 フレーム進める。ease-in (二次 t^2) で「最初ゆっくり →
// 終盤に加速」する (ユーザー要望 2026-07-21)。進捗は開始位置 scrollFrom からの経過フレーム
// 割合 t=frame/scrollAnimFrames で測り、表示 offset = scrollFrom + dist*t^2。最終フレームで
// 論理 offset へスナップして scrollAnim を下ろす。
// カーブを変えるならここ: t*(2-t) にすると ease-out (最初速く減速)、t で等速。
func (m *browseModel) advanceScroll() {
	dist := m.offset - m.scrollFrom
	m.scrollFrame++
	if dist == 0 || m.scrollFrame >= scrollAnimFrames {
		m.offsetShown = m.offset
		m.scrollAnim = false
		return
	}
	// prog = round(|dist| * frame^2 / scrollAnimFrames^2)。符号は dist に合わせる (上下対称)
	mag := dist
	if mag < 0 {
		mag = -mag
	}
	f, total := m.scrollFrame, scrollAnimFrames
	prog := (mag*f*f*2 + total*total) / (2 * total * total) // round-half-up
	if dist < 0 {
		prog = -prog
	}
	m.offsetShown = m.scrollFrom + prog
}

// unpushedCount は未 push コミット数 (push 確認モーダルと confirmPush が共用)。
func (m *browseModel) unpushedCount() int {
	n := 0
	for _, st := range m.statuses {
		if st == StateUnpushed {
			n++
		}
	}
	return n
}

// centerModalLines は中央モーダルの描画行 (action モーダル: push/pull/update)。非表示なら nil。
// tmux prefix 誤爆のフィードバックは中央ダイアログをやめて右下 toast へ移した (2026-07-24)。
func (m *browseModel) centerModalLines() []string {
	return m.actModal.boxLines(m.contentWidth(), m.colored, m.spinner(), m.unpushedCount())
}

// quit はアプリ全体を終了する (取得中断分は unknown へ落とす)。
func (m *browseModel) quit() (tea.Model, tea.Cmd) {
	m.cancel()
	m.usageOv.stop()  // 走行中の usage fetch subprocess を中断 (オーファン化防止)
	m.actModal.stop() // 走行中の push/pull git subprocess を中断 (stall 中の Ctrl-C 孤児化防止)
	if m.fetching {
		m.fillUnknown()
	}
	m.done = true
	return m, tea.Quit
}

// handlePanelKey は job パネル表示中のキー操作。j/k はパネル内のフォーカス移動になる。
// Enter は一貫して「TUI 内で開閉 (toggle)」: タイトル行 = パネルを閉じる (Enter 連打で
// 開閉 toggle)、job 行 = 詳細 (annotations / ログ tail) ポップアップを開く。
// ブラウザで開くのは o (ユーザー要望)。
func (m *browseModel) handlePanelKey(key string) (tea.Model, tea.Cmd) {
	if m.detailOv.visible() {
		return m.handleDetailKey(key)
	}
	jobs := m.details[m.panelSHA]
	switch key {
	case "esc", "h", "left":
		m.closePanel()
	case "enter", " ":
		if m.panelCursor < 0 {
			m.closePanel()
			return m, nil
		}
		return m, m.openJobDetail()
	case "j", "down", "ctrl+n":
		if m.panelCursor+1 < len(jobs) {
			m.panelCursor++
		}
	case "k", "up", "ctrl+p":
		m.panelCursor = max(m.panelCursor-1, -1)
	case "g", "home":
		// job 0 件でフォーカスを 0 にすると、存在しない job にフォーカスが移って
		// タイトル行 (-1) へ戻れなくなる (Enter で閉じられない) ため空ではしない
		if len(jobs) > 0 {
			m.panelCursor = 0
		}
	case "G", "end":
		if len(jobs) > 0 {
			m.panelCursor = len(jobs) - 1
		}
	case "l", "right", "tab":
		if m.panelCursor < 0 {
			if len(jobs) > 0 {
				m.panelCursor = 0
			}
			return m, nil
		}
		return m, m.openJobDetail()
	case "o":
		return m, m.openJob()
	case "y":
		m.copyFocusURL()
	case "Y":
		return m, m.copyJobContext()
	case "p":
		return m, m.openPR()
	case "d":
		return m, m.openDiff()
	case "r":
		m.askRerun()
	}
	return m, nil
}

// handleDetailKey は job 詳細ポップアップ表示中のキー操作。o(ブラウザ)/v(nvim)/y(コピー) の
// 越境キーはここで処理し、スクロール/閉じ (enter/space/esc/h/left/j/k/g/G/pg) は detailOv.scroll へ
// 委譲する (handleDiffKey が y を残して scroll を委譲するのと同型)。cache キー/表示行数はレイアウト・
// パネル状態依存なので detailKey()/visibleDetailRows() を引数で注入する。
func (m *browseModel) handleDetailKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "o":
		return m, m.openJob()
	case "v":
		return m, m.openJobLogInEditor()
	case "y":
		m.copyFocusURL()
		return m, nil
	case "Y":
		return m, m.copyJobContext()
	case "r":
		m.askRerun()
		return m, nil
	}
	m.detailOv.scroll(key, m.detailKey(), m.visibleDetailRows())
	return m, nil
}

// copyJobContext はフォーカス中 job の「何が起きたか」(step 一覧 + annotations / ログ末尾) を
// Markdown 整形でクリップボードへコピーする (job パネル / job 詳細の Y キー。LLM に貼る用。
// issue 020)。詳細が未取得なら詳細ポップアップを開いて取得し、到着 (jobDetailMsg) 時にコピーする。
func (m *browseModel) copyJobContext() tea.Cmd {
	job, ok := m.focusedJob()
	if !ok {
		m.toast.show("job を選択してから Y で詳細をコピーします", false)
		return nil
	}
	if lines := m.detailOv.lines(m.detailKey()); len(lines) > 0 {
		m.copyJobContextLines(job, lines)
		return nil
	}
	m.copyOnDetail = m.detailKey()
	return m.openJobDetail()
}

// copyJobContextLines は job 詳細行をヘッダ (job 名 / commit / URL) 付きの Markdown にして
// クリップボードへ入れる。ヘッダ・本文とも制御コードを除去したプレーンテキストにする。
// ⚠️ job.URL (= StatusContext の targetUrl 等) は外部 CI が任意に設定でき無害化を一切通って
// いない。生のままシステムクリップボードへ流すと、ペースト先の端末で OSC52 (クリップボード
// 書き換え)/カーソル操作等が発火しうる (レビュー確定)。stripANSI 単体は OSC を落とせない
// (英字終端判定のため OSC の途中で誤終了し BEL が残る・実測) ので、OSC/DCS を確実に落とす
// sanitizeDetailLine を先に通し、残る SGR (色) を stripANSI で除去して完全な平文にする。
// c.Subject は %q が制御文字を Go エスケープするため安全。本文 lines は取得時に
// sanitizeDetailLine 済みなので jobLogText の stripANSI だけで足りる。
func (m *browseModel) copyJobContextLines(job CheckDetail, lines []string) {
	plain := func(s string) string { return stripANSI(sanitizeDetailLine(s)) }
	var b strings.Builder
	b.WriteString("## CI job: ")
	b.WriteString(plain(job.Name))
	if c := m.commitBySHA(m.panelSHA); c != nil && m.hasRepo {
		fmt.Fprintf(&b, " — %s/%s@%s %q", m.repo.Owner, m.repo.Name, c.ShortSHA, c.Subject)
	}
	b.WriteString("\n")
	if job.URL != "" {
		b.WriteString(plain(job.URL))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(jobLogText(lines))
	if err := copyToClipboard(b.String()); err != nil {
		m.toast.show("コピーに失敗しました: "+firstLine(err.Error()), false)
		return
	}
	m.toast.show(fmt.Sprintf("job 詳細をコピーしました (%d 行)", len(lines)), true)
}

// askRerun はフォーカス中 job の CI 再実行確認 (y/N) に入る (job パネル / job 詳細の r キー)。
// 再実行できないケース (StatusContext / 失敗以外) は確認を出さず notice で理由を伝える。
func (m *browseModel) askRerun() {
	job, ok := m.focusedJob()
	if !ok {
		return
	}
	if job.CheckID == 0 {
		m.toast.show("GitHub Actions の job ではないため再実行できません", false)
		return
	}
	if job.State != StateFailure {
		m.toast.show("再実行できるのは失敗した job だけです", false)
		return
	}
	repo, sha, id := m.repo, m.panelSHA, job.CheckID
	m.actModal.askRerun(job.Name, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		return rerunMsg{sha: sha, err: runJobRerun(ctx, repo, id)}
	})
}

// detailKey は job 詳細キャッシュのキー。詳細表示中は panelCursor が動かないため安定する。
func (m *browseModel) detailKey() string {
	return fmt.Sprintf("%s/%d", m.panelSHA, m.panelCursor)
}

// focusedJob はパネルでフォーカス中の job を返す (タイトル行フォーカス・範囲外・
// パネル非表示は ok=false)。境界条件の実装をここ 1 箇所に集約する。
func (m *browseModel) focusedJob() (CheckDetail, bool) {
	jobs := m.details[m.panelSHA]
	if m.panelCursor < 0 || m.panelCursor >= len(jobs) {
		return CheckDetail{}, false
	}
	return jobs[m.panelCursor], true
}

// etaBasis は実行中 job (name) の終了予定を概算するための「直近の同名完了 job 1 件」の
// 所要時間を返す。excludeSHA (実行中の当該コミット) を除き、表示中コミットを
// excludeSHA に近い順 (まず古い側、次に新しい側) に走査して最初に見つかった同名 job を
// 採用する。追加 fetch はしない (画面に取得済みの Details だけで概算する)。
// 見つからなければ ok=false (履歴が画面に無い / 初回実行)。
//
// StateNeutral (cancelled / skipped) は除外する: 途中 cancel された run も StartedAt/
// CompletedAt を持ち Duration>0 になるが、数秒で切られた時間を basis にすると ETA が
// 極端に短く出て即「予定超過」になり概算が誤る。完了まで走った失敗 (StateFailure) は
// 所要時間として妥当なので basis に残す。
func (m *browseModel) etaBasis(name, excludeSHA string) (time.Duration, bool) {
	idx := -1
	for i := range m.commits {
		if m.commits[i].SHA == excludeSHA {
			idx = i
			break
		}
	}
	if idx < 0 {
		return 0, false
	}
	for dist := 1; dist < len(m.commits); dist++ {
		for _, j := range [2]int{idx + dist, idx - dist} { // 古い側 (下) を先に見る
			if j < 0 || j >= len(m.commits) {
				continue
			}
			for _, det := range m.details[m.commits[j].SHA] {
				if det.Name == name && det.Duration > 0 && det.State != StateNeutral {
					return det.Duration, true
				}
			}
		}
	}
	return 0, false
}

// jobTimeSuffix は job 行末尾の時間表示 ("(...)" の中身。空 = 何も出さない)。
//   - 実行中 (StatePending) で開始時刻あり: "2m10s 経過" + ETA basis があれば "残り ~50s" / "予定超過"
//   - 完了: 所要時間 (従来どおり)
//
// 実行中判定は State で行う (Duration==0 は StatusContext / 未取得も含むため出典にしない)。
func (m *browseModel) jobTimeSuffix(job CheckDetail) string {
	if job.State == StatePending && !job.StartedAt.IsZero() {
		elapsed := timeNow().Sub(job.StartedAt)
		el := formatDuration(elapsed)
		if el == "" { // 開始直後 (<1s) / わずかな時計ずれ
			el = "0s"
		}
		suffix := el + " 経過"
		if basis, ok := m.etaBasis(job.Name, m.panelSHA); ok {
			if remain := basis - elapsed; remain > 0 {
				suffix += ", 残り ~" + formatDuration(remain)
			} else {
				suffix += ", 予定超過"
			}
		}
		return suffix
	}
	return formatDuration(job.Duration)
}

// timeNow は経過時間・ETA 算出の現在時刻。テストで固定するため差し替え可能にしている。
var timeNow = time.Now

// jobDetailRows は詳細ポップアップに一度に表示する行数の上限。実際の行数は
// 端末の高さに合わせて visibleDetailRows が縮める。
const jobDetailRows = 15

// visibleDetailRows は詳細ポップアップが実際に使える行数 (job パネルとヒント行を
// 差し引いた残り。低い端末で詳細ボックスがビューポートに切られ、末尾スクロールが
// 見えなくなるのを防ぐ)。
func (m *browseModel) visibleDetailRows() int {
	jobBoxLines := min(max(len(m.details[m.panelSHA]), 1), maxPanelJobs) + 2
	return max(min(jobDetailRows, m.pageSize()-jobBoxLines-2), 3)
}

// openJobDetail はフォーカス中 job の annotations / ログ tail のポップアップを開く。
func (m *browseModel) openJobDetail() tea.Cmd {
	check, ok := m.focusedJob()
	if !ok {
		return nil
	}
	key := m.detailKey()
	if !m.detailOv.startOpen(key, m.visibleDetailRows()) {
		return nil // cache ヒット (offset は末尾へ) / 取得中: 追加取得は不要
	}
	repo := m.repo
	cmd := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		lines, ghErr := FetchJobDetail(ctx, ExecRunner, repo, check)
		return jobDetailMsg{key: key, lines: lines, ghErr: ghErr}
	}
	return tea.Batch(cmd, m.maybeTick())
}

// copyFocusURL はフォーカス位置の URL (job 選択中はその job、それ以外はコミット) を
// クリップボードへコピーする。LLM に貼る用途 (ユーザー要望)。
// commitURL はカーソル位置コミットの GitHub commit ページ URL ("" = repo なし/コミットなし)。
func (m *browseModel) commitURL() string {
	if !m.hasRepo || len(m.commits) == 0 {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/%s/commit/%s", m.repo.Owner, m.repo.Name, m.commits[m.cursor].SHA)
}

// openCommitURL はカーソル位置コミットの GitHub commit ページをブラウザで開く (一覧の o キー)。
func (m *browseModel) openCommitURL() tea.Cmd {
	url := m.commitURL()
	if url == "" {
		m.toast.show("GitHub の remote が無いため開けません", false)
		return nil
	}
	return m.openURLCmd(url)
}

func (m *browseModel) copyFocusURL() {
	var url string
	if job, ok := m.focusedJob(); ok {
		url = job.URL
	} else {
		url = m.commitURL()
	}
	if url == "" {
		m.toast.show("コピーできる URL がありません", false)
		return
	}
	if err := copyToClipboard(url); err != nil {
		m.toast.show("コピーに失敗しました: "+firstLine(err.Error()), false)
		return
	}
	m.toast.show("コピーしました: "+url, true)
}

// openPanel はカーソル位置のコミットの CI job パネルを開く。詳細が未取得
// (キャッシュヒットで一括取得に含まれなかった SHA) なら、その SHA だけ追加取得する。
func (m *browseModel) openPanel() tea.Cmd {
	if len(m.commits) == 0 {
		return nil
	}
	sha := m.commits[m.cursor].SHA
	m.panelSHA = sha
	m.panelCursor = -1 // タイトル行フォーカスから開始 (この状態の Enter = 閉じる)
	m.panelPollSeq++   // 前のパネルの残タイマーを世代で無効化する
	// パネルコミットの Details 取得と、実行中 job があれば ETA basis の補充を両方仕掛ける。
	// details 既取得なら basis 補充だけ即走る (basis 判定に details が要るため)。
	// 定期リフレッシュは「実行中 job がある」と分かっているときだけ開始する
	// (details 未取得ならその到着時 = detailMsg 側で開始する。常時 timer を返すと
	// 「fetch 不要なら Cmd は nil」というパネル系テストの契約も壊れる)。
	var poll tea.Cmd
	if m.panelHasRunningJob() {
		poll = m.ensurePanelPoll()
	}
	return tea.Batch(m.fetchPanelDetails(sha), m.maybeFetchETABasis(), poll)
}

// schedulePanelPoll は現世代の panelPollMsg を panelPollInterval 後に発火させる。
// ⚠️ これはチェーンの「継続」用 (panelPollMsg ハンドラ内の再アーム)。チェーンの「開始」は
// 必ず ensurePanelPoll を通し、複数の開始点 (openPanel / detailMsg / rerunMsg) が独立した
// チェーンを二重に張らないようにする。
func (m *browseModel) schedulePanelPoll() tea.Cmd {
	seq := m.panelPollSeq
	return tea.Tick(panelPollInterval, func(time.Time) tea.Msg { return panelPollMsg{seq: seq} })
}

// ensurePanelPoll は panelPollMsg の自己更新チェーンを single-flight で 1 本だけ張る
// (maybeTick と同型)。既に生きていれば nil を返して二重チェーンを作らない。全ての「開始点」
// (openPanel / detailMsg 初到着 / rerunMsg) がこれを通ることで、例えば実行中 job があるパネルで
// rerun したときに openPanel が張った chain と rerunMsg が張る chain が二重化して GraphQL
// ポーリング頻度が倍になる不具合を防ぐ (レビュー確定)。チェーンの停止点 (grace 尽き / closePanel)
// で panelPolling=false に戻す。
func (m *browseModel) ensurePanelPoll() tea.Cmd {
	if m.panelPolling {
		return nil
	}
	m.panelPolling = true
	return m.schedulePanelPoll()
}

// fetchPanelDetails はパネルコミット sha の Details をオンデマンド取得する Cmd
// (取得不要 / 取得先なしは nil)。openPanel から切り出した本体。
func (m *browseModel) fetchPanelDetails(sha string) tea.Cmd {
	if _, ok := m.details[sha]; ok || m.detailsLoading[sha] {
		return nil
	}
	if !m.hasRepo || m.statuses[sha] == StateUnpushed {
		// remote が GitHub でない / 未 push の SHA は取得先が無い
		m.details[sha] = []CheckDetail{}
		return nil
	}
	// 進行中の一括取得に含まれる SHA は、その結果 (details 込み) を待つ。
	// ここで別リクエストを打つと同一 SHA への GraphQL が並行し、完了順で
	// statuses/details が上書きされる (codex レビュー指摘)
	if m.fetching && slices.Contains(m.toFetch, sha) {
		m.detailsLoading[sha] = true
		return nil
	}
	m.detailsLoading[sha] = true
	cmd := fetchCIStatusesCmd(m.repo, []string{sha}, func(b CIBatch, e *GHError) tea.Msg {
		return detailMsg{sha: sha, batch: b, ghErr: e}
	})
	return tea.Batch(cmd, m.maybeTick())
}

// maybeFetchETABasis は、パネルを開いた実行中コミットに ETA basis (同名完了 job の
// Duration) がセッション内に無いとき、表示中の他コミットのうち Details 未取得のものを
// 1 回の GraphQL でまとめて取得する Cmd を返す (nil = 取得不要 / 取得先なし)。
//
// 完了コミットは State だけがキャッシュされ Details は保存されないため (cache.go)、glogx を
// 開き直すと完了状態は cache ヒットで toFetch から外れ、その job Duration が欠けて ETA が
// 出なくなる。パネルを開いた時点でこの穴を能動的に埋める。取得は 1 リクエストに束ね、
// 対象は表示中コミットに限る (無制限に履歴を遡らない)。
func (m *browseModel) maybeFetchETABasis() tea.Cmd {
	if m.panelSHA == "" || !m.hasRepo {
		return nil
	}
	jobs, ok := m.details[m.panelSHA]
	if !ok {
		return nil // パネルコミットの Details 未取得。到着後 (detailMsg) に再評価される
	}
	// basis を必要とする実行中 job があり、かつ現状 basis が取れないときだけ補充する
	need := false
	for _, j := range jobs {
		if j.State == StatePending && !j.StartedAt.IsZero() {
			if _, ok := m.etaBasis(j.Name, m.panelSHA); !ok {
				need = true
				break
			}
		}
	}
	if !need {
		return nil
	}
	var targets []string
	for _, c := range m.commits {
		switch {
		case c.SHA == m.panelSHA:
		case m.detailsLoading[c.SHA]:
		case m.statuses[c.SHA] == StateUnpushed:
		case m.fetching && slices.Contains(m.toFetch, c.SHA): // 進行中の一括取得を待つ
		default:
			if _, ok := m.details[c.SHA]; !ok { // Details 未取得のものだけ
				targets = append(targets, c.SHA)
			}
		}
		if len(targets) >= fetchMaxSHAs {
			break
		}
	}
	if len(targets) == 0 {
		return nil
	}
	for _, sha := range targets {
		m.detailsLoading[sha] = true
	}
	cmd := fetchCIStatusesCmd(m.repo, targets, func(b CIBatch, e *GHError) tea.Msg {
		return basisMsg{targets: targets, batch: b, ghErr: e}
	})
	return tea.Batch(cmd, m.maybeTick())
}

func (m *browseModel) closePanel() {
	m.panelSHA = ""
	m.panelCursor = -1
	m.panelPollSeq++ // 定期リフレッシュの残タイマーを世代で無効化する
	// panelRefresh も必ず下ろす: 不変条件は「現在開いているパネル向けの refresh が
	// in-flight」なので、パネルが無ければ false でなければならない。これを怠ると、in-flight
	// refresh 中にパネルを閉じたとき、遅延到着する detailMsg{旧SHA} が
	// `msg.sha == m.panelSHA("")` に一致せず panelRefresh を戻せず true に固着し、以降
	// panelPollMsg が毎回 refresh をスキップしてパネルのライブ更新が恒久停止する
	// (かつ実行中 job のパネルでは spinnerActive が真のまま tick/poll が空回りし続ける。
	// レビュー C2/C3/K1)。closePanel は全パネル退出経路 (q/h/esc/left・reloadAfterPull)
	// の choke point なのでここ 1 箇所で覆える。閉じた後に届く旧 refresh の detailMsg は
	// panelSHA と不一致で無害 (maps.Copy はキャッシュ更新のみ・poll は再開しない)。
	m.panelRefresh = false
	m.panelPollGrace = 0   // rerun 直後の猶予ポーリングもパネルと一緒に終える
	m.panelPolling = false // ポーリングチェーンの single-flight フラグを戻す (次の開き直しで再アーム可能に)
	m.copyOnDetail = ""    // Y のコピー予約も破棄 (閉じた後の到着で意図しないコピーをしない)
	m.detailOv.close()     // 詳細ポップアップも閉じる (panel/detail 両クラスタの choke point)
}

// openURLCmd は URL をブラウザで開く Cmd。StatusContext の targetUrl 等、外部が任意に
// 設定できる値を通すため、file:// 等でローカルのハンドラを起動させないよう
// http(s) だけを開く。
func (m *browseModel) openURLCmd(url string) tea.Cmd {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		m.toast.show("http(s) 以外の URL は開きません", false)
		return nil
	}
	return func() tea.Msg {
		return openURLMsg{err: openInBrowser(url)}
	}
}

// jobLogText は job 詳細行を nvim へ渡すプレーンテキストにする。ANSI 色 (SGR) を除去して、
// nvim で yank したときに制御コードが混ざらないようにする。
func jobLogText(lines []string) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(stripANSI(l))
		b.WriteByte('\n')
	}
	return b.String()
}

// openJobLogInEditor は表示中の job 詳細ログを nvim で開く (v キー・コピー用。less の v=エディタ
// で開く慣習に合わせた「view」)。ログは stdin
// (`nvim -`) で渡すのでディスクにファイルを残さず、nvim を閉じればバッファごと破棄される
// (ユーザー要望 2026-07-21: ログのテキストをコピーしたいが後に残したくない)。
func (m *browseModel) openJobLogInEditor() tea.Cmd {
	lines := m.detailOv.lines(m.detailKey())
	if len(lines) == 0 {
		m.toast.show("開けるログがありません", false)
		return nil
	}
	// -R (readonly) + nomodifiable + buftype=nofile で「閲覧してコピーするだけ」の scratch に
	// する: 誤編集できず :q が常にクリーンに閉じる (素の nvim - だと変更扱い等で :q がエラーに
	// なる・ユーザー報告 2026-07-21)。yank は nomodifiable でも可能。noswapfile で swap も残さない。
	cmd := exec.Command("nvim", "-R", "-c", "setlocal buftype=nofile noswapfile nomodifiable", "-")
	cmd.Stdin = strings.NewReader(jobLogText(lines))
	return runEditorCmd(cmd)
}

// openJob はパネルで選択中の job の詳細ページをブラウザで開く。
func (m *browseModel) openJob() tea.Cmd {
	job, ok := m.focusedJob()
	if !ok {
		return nil
	}
	if job.URL == "" {
		m.toast.show("この job には詳細ページの URL がありません", false)
		return nil
	}
	return m.openURLCmd(job.URL)
}

// openPR はカーソル位置のコミットに紐づく PR をブラウザで開く (p キー)。
// commit → PR の関連は GitHub (associatedPullRequests) から取得し、結果はキャッシュする。
func (m *browseModel) openPR() tea.Cmd {
	if len(m.commits) == 0 {
		return nil
	}
	if !m.hasRepo {
		m.toast.show("GitHub の remote が無いため PR を取得できません", false)
		return nil
	}
	sha := m.commits[m.cursor].SHA
	if m.statuses[sha] == StateUnpushed {
		m.toast.show("未 push のコミットに PR はありません", false)
		return nil
	}
	if pr, ok := m.prCache[sha]; ok {
		if pr == nil {
			m.toast.show("このコミットに紐づく PR はありません", false)
			return nil
		}
		return m.openURLCmd(pr.URL)
	}
	if m.prBusy[sha] {
		return nil
	}
	m.prBusy[sha] = true
	// 進行中トースト (…) は直後に届く prMsg の結果トーストで上書きされる。tick は呼び出し側
	// (handleListKey/handlePanelKey の maybeTick) が回す。
	m.toast.showInfo("PR を検索中...")
	repo := m.repo
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		pr, ghErr := FetchCommitPR(ctx, ExecRunner, repo, sha)
		return prMsg{sha: sha, pr: pr, ghErr: ghErr}
	}
}

// openPRStatus はカーソル位置コミットの PR 状態ポップアップを開く (P キー, issue 021)。
// 同じコミットで再度 P を押すと閉じる (toggle)。取得はオンデマンド単発 GraphQL
// (一括クエリと prCache は number/url/state のまま変えない。理由は PRStatus のコメント)。
func (m *browseModel) openPRStatus() tea.Cmd {
	if len(m.commits) == 0 {
		return nil
	}
	if !m.hasRepo {
		m.toast.show("GitHub の remote が無いため PR を取得できません", false)
		return nil
	}
	sha := m.commits[m.cursor].SHA
	if m.statuses[sha] == StateUnpushed {
		m.toast.show("未 push のコミットに PR はありません", false)
		return nil
	}
	if !m.prStatusOv.open(sha) {
		return nil // toggle 閉 / キャッシュヒット
	}
	repo := m.repo
	cmd := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		status, ghErr := FetchPRStatus(ctx, ExecRunner, repo, sha)
		return prStatusMsg{sha: sha, status: status, ghErr: ghErr}
	}
	return tea.Batch(cmd, m.maybeTick())
}

// handlePRStatusKey は PR 状態ポップアップ表示中のキー操作。o (ブラウザ) / y (URL コピー) と
// 閉じるだけの小さなモーダル (スクロールする本文は無い)。
func (m *browseModel) handlePRStatusKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "h", "left", "esc", "P", "enter":
		m.prStatusOv.close()
	case "o":
		if pr := m.prStatusOv.current(); pr != nil {
			return m, m.openURLCmd(pr.URL)
		}
	case "y":
		if pr := m.prStatusOv.current(); pr != nil {
			if err := copyToClipboard(pr.URL); err != nil {
				m.toast.show("コピーに失敗しました: "+firstLine(err.Error()), false)
			} else {
				m.toast.show("コピーしました: "+pr.URL, true)
			}
		}
	}
	return m, nil
}

// prStatusCILine は PR ポップアップに出すコミット CI 状態の 1 行 (出典はコミット行と同じ
// statuses/details)。失敗時は取得済み details から失敗 job 数を添える。
func (m *browseModel) prStatusCILine() string {
	sha := m.prStatusOv.sha
	if sha == "" {
		return ""
	}
	st, ok := m.statuses[sha]
	if !ok {
		return ""
	}
	line := StatusGlyph(st, m.colored, m.spinner()) + " " + string(st)
	if st == StateFailure {
		n := 0
		for _, d := range m.details[sha] {
			if d.State == StateFailure {
				n++
			}
		}
		if n > 0 {
			line += fmt.Sprintf(" (%d job 失敗)", n)
		}
	}
	return line
}

// openDiff はカーソル位置 (パネル表示中はそのコミット) の diff ポップアップを開く (d キー)。
// 同じコミットで再度 d を押すと閉じる (toggle)。job パネルは閉じてから開く (重ね順の単純化)。
// ターゲット選定・パネル閉じ・非同期取得の境界だけをここで持ち、pager の状態は diffOv が持つ。
func (m *browseModel) openDiff() tea.Cmd {
	if len(m.commits) == 0 {
		return nil
	}
	sha := m.commits[m.cursor].SHA
	if m.panelSHA != "" {
		sha = m.panelSHA
	}
	m.closePanel()
	if !m.diffOv.open(sha) {
		return nil // toggle 閉 / キャッシュヒット / 取得中: 追加取得は不要
	}
	colored := m.colored
	cmd := func() tea.Msg {
		lines, err := loadCommitDiff(sha, colored)
		return diffMsg{sha: sha, lines: lines, err: err}
	}
	return tea.Batch(cmd, m.maybeTick())
}

// handleDiffKey は diff ポップアップ表示中のキー操作。y (URL コピー) は境界をまたぐのでここで
// 処理し、スクロール/閉じるは diffOv.scroll へ委譲する (pager 流儀の詳細は diffOverlay 側)。
func (m *browseModel) handleDiffKey(key string) (tea.Model, tea.Cmd) {
	if key == "y" {
		m.copyFocusURL()
		return m, nil
	}
	m.diffOv.scroll(key, m.visibleDiffRows())
	return m, nil
}

// visibleDiffRows は diff ポップアップの本文行数。diff は主役コンテンツなので
// ビューポートほぼ全面 (枠 2 行 + 余白 1 行 + ヒント行ぶんを差し引く) を使う。
// 端末の高さ (pageSize) 依存なのでレイアウトを知る browseModel 側に残す。
func (m *browseModel) visibleDiffRows() int {
	return max(m.pageSize()-4, 3)
}

// diffBoxLines は diff ポップアップの描画行 (枠付き)。SHA からコミットを解決して diffOv へ渡す。
func (m *browseModel) diffBoxLines() []string {
	return m.diffOv.boxLines(m.contentWidth(), m.colored, m.spinner(), m.commitBySHA(m.diffOv.sha), m.visibleDiffRows())
}

// fillUnknown は結果が得られなかった SHA を「取得中」のまま残さず unknown へ落とす。
func (m *browseModel) fillUnknown() {
	for _, sha := range m.toFetch {
		if _, ok := m.statuses[sha]; !ok {
			m.statuses[sha] = StateUnknown
		}
	}
}

func (m *browseModel) spinnerActive() bool {
	return m.fetching || m.actModal.running() || m.pullAnimating || m.pushAnimating || len(m.pushSlides) > 0 || m.scrollAnim || m.toast.animating() || len(m.pushPoll) > 0 || len(m.detailsLoading) > 0 || m.detailOv.fetching() || m.diffOv.fetching() || m.prStatusOv.fetching() || m.panelHasRunningJob() || m.usageOv.loading()
}

// panelHasRunningJob は表示中の job パネルに実行中 (経過時間が増える) job があるか。
// tick を回し続けて「N 経過 / 残り ~M」をライブ更新するため (spinnerActive が false だと
// tick が止まり、パネルを開いたまま経過秒が固まる)。
func (m *browseModel) panelHasRunningJob() bool {
	if m.panelSHA == "" {
		return false
	}
	for _, job := range m.details[m.panelSHA] {
		if job.State == StatePending && !job.StartedAt.IsZero() {
			return true
		}
	}
	return false
}

func (m *browseModel) spinner() string {
	return spinnerFrames[m.frame%len(spinnerFrames)]
}

// renderOpts はリスト部分の描画パラメータ。job 一覧はパネルで重ねるため、
// インライン展開 (Expanded) は使わない (行構成を不変に保つ)。
func (m *browseModel) renderOpts() RenderOpts {
	return RenderOpts{
		Oneline: m.oneline,
		Colored: m.colored,
		Spinner: m.spinner(),
		// 全行に 2 桁のカーソル溝 (cursorGutter*) を確保するため、折り返し幅は
		// その分狭い (bg 塗りだけでは視認しにくいという 2026-07-21 のユーザー要望で、
		// 2026-07-19 の「溝なし・全幅」から再反転した)
		Width:    max(m.contentWidth()-cursorGutterWidth, 0),
		Decor:    m.decor,
		PRs:      m.prCache,
		Verbatim: m.verbatim,
		HasRepo:  m.hasRepo,
		PushAnim: m.pushAnimating,
	}
}

func (m *browseModel) lines() []Line {
	if !m.linesValid {
		m.linesCache = RenderLines(m.commits, m.statuses, m.renderOpts())
		m.linesValid = true
	}
	return m.linesCache
}

func (m *browseModel) invalidateLines() {
	m.linesValid = false
}

// pageSize はビューポートの行数 (最下段のヒント行を除く)。
// フレーム有効時の寸法オーバーヘッド (issue 025)。
//
//	横 frameHOverhead = 左余白1 + "│ "2 + " │"2 + 影1 + 右余白1 = 7
//	縦 frameVOverhead = 上余白1 + 上辺1 + 下辺1 + 下影1 + hint1 = 5
//
// frameMinWidth/Height 未満の端末ではフレームを自動 OFF し従来描画へフォールバックする
// (tmux の小ペイン/popup でも安全。極小端末で影の見切れを避ける)。
const (
	frameHOverhead = 7
	frameVOverhead = 5
	frameMinWidth  = 60
	frameMinHeight = 15
)

// frameActive は今このフレームで最外周フレームを描くか。起動時固定の showFrame に加え、端末が
// 下限サイズ以上のときだけ true。入力 (showFrame + width/height) は WindowSizeMsg でのみ変化し、
// そこで invalidateLines 済みなので linesCache の無効化点は増やさない (issue の不変条件)。
func (m *browseModel) frameActive() bool {
	return m.showFrame && m.width >= frameMinWidth && m.height >= frameMinHeight
}

// contentWidth はコンテンツ (リスト行・overlay・モーダル) が使える横幅。フレーム有効時は枠 + 余白 +
// 影のぶんを引く。m.width/m.height を直読みしてよいのは frameActive/contentWidth/pageSize と
// View の wrapWindowFrame 呼び出しだけ、という規約で幅の単一ファネルにする。
func (m *browseModel) contentWidth() int {
	if !m.frameActive() {
		return m.width
	}
	return m.width - frameHOverhead
}

// pageSize はビューポートの行数 (最下段のヒント行を除く)。フレーム有効時は枠・余白・影・hint ぶんを
// 引く。scroll 半ページ / pull アニメ / detail・diff 行数 / clampOffset / ensureCursorVisible /
// View が全てここを経由するので、フレーム分が全消費者へ自動伝播する。
func (m *browseModel) pageSize() int {
	if !m.frameActive() {
		return max(m.height-1, 1)
	}
	return max(m.height-frameVOverhead, 1)
}

func (m *browseModel) clampOffset(offset int) int {
	maxOffset := max(len(m.lines())-m.pageSize(), 0)
	return min(max(offset, 0), maxOffset)
}

// ensureCursorVisible はカーソル対象コミットのヘッダー行がビューポート内に入るよう
// offset を調整する。
func (m *browseModel) ensureCursorVisible() {
	lines := m.lines()
	header := 0
	for i, l := range lines {
		if l.Header && l.CommitIdx == m.cursor {
			header = i
			break
		}
	}
	page := m.pageSize()
	if header < m.offset {
		m.offset = header
	}
	if header >= m.offset+page {
		m.offset = header - page + 1
	}
	m.offset = min(max(m.offset, 0), max(len(lines)-page, 0))
}

func (m *browseModel) View() string {
	if m.done {
		// 終了確定後は何も描かない (Alt Screen の復帰で表示は消える)
		return ""
	}
	lines := m.lines()
	page := m.pageSize()
	// scrollAnim 中は表示 offset (glide 途中) で窓を切る。それ以外は論理 offset。
	renderOffset := m.offset
	if m.scrollAnim {
		renderOffset = m.offsetShown
	}
	offset := min(max(renderOffset, 0), max(len(lines)-page, 0))
	end := min(offset+page, len(lines))
	window := make([]string, 0, page)
	slides := m.slideColumns(lines)
	for i := offset; i < end; i++ {
		text := lines[i].Text
		// push 演出の沈み込み中の区画: 元の色を剥がして dim 一色に落とし、右オフセットを
		// 付けて幅でクリップする (「非活性になって origin へ吸い込まれる」見た目)。
		// カーソル強調より優先する (演出中の bg 塗りは動きを汚す)
		if off := slides[i]; off > 0 {
			text = strings.Repeat(" ", off) + paint(stripANSI(text), ansiDim, m.colored)
			window = append(window, cursorGutterBlank+clipToWidth(text, max(m.contentWidth()-cursorGutterWidth, 0)))
			continue
		}
		// カーソルは全行に確保した 2 桁の溝の「→ 」+ ヘッダー行全体の bg 塗りで示す。
		// 溝は一度「git log と左マージンがずれる」で廃止したが、bg 塗りだけでは
		// 視認しにくいため全行マージン込みで復活 (ユーザー要望 2026-07-21)
		if lines[i].Header && lines[i].CommitIdx == m.cursor {
			window = append(window, m.cursorLine(text))
			continue
		}
		window = append(window, cursorGutterBlank+clipToWidth(text, max(m.contentWidth()-cursorGutterWidth, 0)))
	}
	// job パネルは対象コミットのヘッダー行直下へ「重ねる」(リスト行を置き換える)。
	// リストの行構成自体は変えないので、開閉で後続行がずれない。
	// 下に収まらない場合はビューポート内へ収まる位置まで引き上げる
	if panel := m.panelLines(); len(panel) > 0 {
		window = overlayBox(window, panel, m.boxAnchor(lines, offset, m.panelSHA)+1, page)
	}
	// diff ポップアップは job パネルよりさらに前面 (openDiff がパネルを閉じるため
	// 実際に同時表示になることはないが、重ね順の契約としてパネルの後に描く)
	if diffBox := m.diffBoxLines(); len(diffBox) > 0 {
		window = overlayBox(window, diffBox, m.boxAnchor(lines, offset, m.diffOv.sha)+1, page)
	}
	// PR 状態ポップアップも対象コミット直下へ重ねる (job パネルとは同時表示にならない:
	// P は一覧のみで受け、表示中は handlePRStatusKey がモーダルに捌く)
	if prBox := m.prStatusOv.boxLines(m.contentWidth(), m.colored, m.spinner(), m.prStatusCILine()); len(prBox) > 0 {
		window = overlayBox(window, prBox, m.boxAnchor(lines, offset, m.prStatusOv.sha)+1, page)
	}
	// push 確認/実行中は画面中央のモーダルを最前面に重ねる (ユーザー要望 2026-07-19:
	// hint 行の [y/N] だけでは気づきにくい)。overlayCenteredBox は行を塗り潰さず左右の背景
	// リストを残して合成する (モーダルの左側テキストが消えるのを解消・ユーザー要望 2026-07-22)。
	if box := m.centerModalLines(); len(box) > 0 {
		window = overlayCenteredBox(window, box, m.contentWidth(), page, m.colored)
	}
	// usage オーバーレイは最前面 (上部右端の複数行モーダル)。U で再表示、任意キーで消える。
	if box := m.usageOv.boxLines(m.contentWidth(), m.colored, m.spinner()); len(box) > 0 {
		window = overlayBoxTopRight(window, box, m.contentWidth(), m.colored)
	}
	// トーストは右下 (hint 行の直上) に数秒だけ。push/pull 完了の結果フィードバック。
	if box := m.toast.boxLines(m.colored); len(box) > 0 {
		window = overlayBoxBottomRight(window, box, m.contentWidth(), m.colored)
	}
	// フレーム有効時は最外周を余白 + 枠 + 右下ドロップシャドウで包む (issue 025)。板の高さを
	// 安定させるため、コンテンツが少なくても pageSize 行まで空行でパディングしてから包む
	// (板が常にビューポート一杯 = リサイズや行数変動で枠が踊らない)。hint は板の外・最下行。
	if m.frameActive() {
		for len(window) < page {
			window = append(window, "")
		}
		window = wrapWindowFrame(window, m.width, m.colored)
	}
	var b strings.Builder
	for _, w := range window {
		b.WriteString(w)
		b.WriteString("\n")
	}
	b.WriteString(m.hintLine())
	return b.String()
}

// boxAnchor は sha のコミットヘッダー行のウィンドウ内位置を返す
// (ウィンドウ外へスクロールしている場合は先頭 -1 = ボックスは最上部に出る)。
func (m *browseModel) boxAnchor(lines []Line, offset int, sha string) int {
	for i, l := range lines {
		if l.Header && l.CommitIdx < len(m.commits) && m.commits[l.CommitIdx].SHA == sha {
			return i - offset
		}
	}
	return -1
}

// commitBySHA は SHA に一致するコミットを線形探索で返す (無ければ nil)。パネル/diff の
// 描画で同一ループが重複していたのを 1 本化 (レビュー C5)。表示件数は既定 20 で O(n) は無害。
func (m *browseModel) commitBySHA(sha string) *Commit {
	for i := range m.commits {
		if m.commits[i].SHA == sha {
			return &m.commits[i]
		}
	}
	return nil
}

// panelLines は job パネルの描画行 (枠付き)。パネル非表示なら nil。
func (m *browseModel) panelLines() []string {
	if m.panelSHA == "" {
		return nil
	}
	width := m.contentWidth()
	if width <= 0 {
		width = 80
	}
	commit := m.commitBySHA(m.panelSHA)
	if commit == nil {
		return nil
	}
	jobs, haveDetails := m.details[m.panelSHA]
	var rows []string
	switch {
	case m.detailsLoading[m.panelSHA]:
		rows = []string{paint(m.spinner()+" CI job を取得中...", ansiDim, m.colored)}
	case !haveDetails:
		rows = []string{paint("(CI job 情報なし)", ansiDim, m.colored)}
	case len(jobs) == 0:
		rows = []string{paint("(Check はありません)", ansiDim, m.colored)}
	default:
		// panelCursor が見える範囲の job を切り出す (maxPanelJobs でスクロール)
		start := 0
		if m.panelCursor >= maxPanelJobs {
			start = m.panelCursor - maxPanelJobs + 1
		}
		endJob := min(start+maxPanelJobs, len(jobs))
		for i := start; i < endJob; i++ {
			mark := "  "
			if i == m.panelCursor {
				mark = cursorMark(m.colored)
			}
			row := mark + StatusGlyph(jobs[i].State, m.colored, "") + " " + jobs[i].Name
			if suffix := m.jobTimeSuffix(jobs[i]); suffix != "" {
				row += paint(" ("+suffix+")", ansiDim, m.colored)
			}
			rows = append(rows, row)
		}
	}
	title := fmt.Sprintf(" CI jobs: %s %s ", commit.ShortSHA, commit.Subject)
	switch {
	case len(jobs) > 0 && m.panelCursor >= 0:
		title = fmt.Sprintf(" CI jobs: %s (%d/%d) %s ", commit.ShortSHA, m.panelCursor+1, len(jobs), commit.Subject)
	case len(jobs) > 0:
		title = fmt.Sprintf(" CI jobs: %s (%d 件) %s ", commit.ShortSHA, len(jobs), commit.Subject)
	}
	box := buildPanelBox(title, rows, width, m.colored)
	if m.detailOv.visible() {
		// 詳細ボックスは job パネルの「子」であることが分かるよう段差を付ける (ユーザー要望)
		for _, line := range m.detailBoxLines(width - len(detailIndent)) {
			box = append(box, detailIndent+line)
		}
	}
	return box
}

// detailIndent は job 詳細ボックスのツリー段差 (job パネルの子であることの視覚表現)。
const detailIndent = "  "

// detailBoxLines は job 詳細ポップアップの描画行。job 名/cache キー/表示行数を解決して detailOv へ
// 渡す (diffBoxLines が commit を解決して diffOv へ渡すのと同型)。job パネルの直下へ重ねる。
func (m *browseModel) detailBoxLines(width int) []string {
	name := ""
	if job, ok := m.focusedJob(); ok {
		name = job.Name
	}
	return m.detailOv.boxLines(width, m.colored, m.spinner(), name, m.detailKey(), m.visibleDetailRows())
}

// cursorLine はカーソル位置のコミットヘッダー行を強調する。溝の「→ 」に加え、色ありでは
// 行全体 (溝込み) を暗青 bg で塗る。色なし (NO_COLOR) では矢印のみ。
func (m *browseModel) cursorLine(text string) string {
	if !m.colored {
		return clipToWidth(cursorGutterMark+text, m.contentWidth())
	}
	return m.bgLine(cursorGutterMark+text, ansiCursorBg)
}

// ansiResetRe は SGR リセット。git log --color は "\x1b[0m" でなく短縮形 "\x1b[m" を
// 多用するため、literal 一致だと色付き行の bg 再適用が途切れる (行末まで塗れない実測
// 2026-07-19)。両形を 1 つの正規表現で拾う。
var ansiResetRe = regexp.MustCompile("\x1b\\[0?m")

// bgLine は行全体を指定 bg で端末幅まで塗る (行内の SGR リセットで bg が切れないよう、
// リセット直後に bg を張り直す)。色なしではそのまま返す (bg が使えない)。
// NOTE: push 済みエリアの面塗りにも使っていたが、bg の面塗りは環境の配色次第で
// 視認性を落とすためユーザー判断で撤去 (2026-07-19)。push 境界の可視化は境界線
// (insertPushBoundary) に一本化。面塗りの再提案はしない。
func (m *browseModel) bgLine(text, bg string) string {
	if !m.colored {
		return clipToWidth(text, m.contentWidth())
	}
	text = clipToWidth(text, m.contentWidth())
	pad := max(m.contentWidth()-dispWidth(text), 0)
	return bg + ansiResetRe.ReplaceAllString(text, "$0"+bg) +
		strings.Repeat(" ", pad) + ansiReset
}

func (m *browseModel) hintLine() string {
	hint := "j/k: 移動  Enter: CI job  d: diff  o: ブラウザ  p: PR  y: URL コピー  b: push  u: pull  U: usage  C: update  w: 警告コピー  q: 終了"
	switch {
	case m.actModal.pushConfirm:
		hint = "push しますか? [Y/n] (Enter=y)"
	case m.actModal.pullConfirm:
		hint = "pull --rebase しますか? [Y/n] (Enter=y)"
	case m.actModal.pushing:
		hint = m.spinner() + " pushing..."
	case m.actModal.pulling:
		hint = m.spinner() + " pulling..."
	case m.actModal.rerunConfirm:
		hint = "job を再実行しますか? [Y/n] (Enter=y)"
	case m.actModal.rerunning:
		hint = m.spinner() + " rerunning..."
	case m.actModal.updating:
		hint = m.spinner() + " claude update..."
	case m.diffOv.visible():
		hint = "j/k/Space: スクロール  g/G: 先頭/末尾  q/h: 閉じる"
	case m.prStatusOv.visible():
		hint = "o: PR をブラウザで開く  y: URL コピー  P/q/h: 閉じる"
	case m.detailOv.visible():
		hint = "j/k: スクロール  v: nvim で開く  r: 再実行  Enter/h/q: 戻る  o: ブラウザ  y: URL  Y: 詳細コピー"
	case m.panelSHA != "" && m.panelCursor >= 0:
		hint = "j/k: job 移動  Enter: 詳細ログ  r: 再実行  o: ブラウザ  y: URL  Y: 詳細コピー  h/q: 閉じる"
	case m.panelSHA != "":
		hint = "j: job を選択  y: commit URL  Enter/h/q: 閉じる"
	}
	if m.fetching {
		hint = m.spinner() + " CI 状態を取得中...  " + hint
	}
	if m.ghErr != nil {
		hint = "⚠ " + firstLine(m.ghErr.Warning()) + "  " + hint
	}
	painted := paint(hint, ansiDim, m.colored)
	if m.frameActive() {
		// hint は板の外 (最下行) だが、左余白 1 桁を付けて板の左端 (┌) と縦に揃える。素朴に
		// " " を前置すると、既定 hint が clip 後に m.width ちょうどになり実効幅 m.width+1 で
		// 折り返し崩壊するため、clip 幅を左右余白ぶん (2) 差し引く (板の footprint と同じ span)。
		return " " + clipToWidth(painted, max(m.width-2, 1))
	}
	return clipToWidth(painted, m.width)
}

func clampIdx(i, total int) int {
	if total <= 0 {
		return 0
	}
	return min(max(i, 0), total-1)
}

// RunBrowse は TUI を実行し、最終状態のモデルを返す。Alt Screen を使うため、
// 終了時に表示は消える (git log の pager と同じ。ユーザー要望 2026-07-17)。
func RunBrowse(m *browseModel) (*browseModel, error) {
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return m, err
	}
	if fm, ok := final.(*browseModel); ok {
		return fm, nil
	}
	return m, nil
}
