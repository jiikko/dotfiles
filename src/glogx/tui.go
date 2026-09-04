package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"doctor/disk"
	"glogx/issues"
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
	// zoomInterval は開閉演出中の tick (~60fps)。他より速いのは、この演出だけが「短い所要
	// (appZoomDuration 220ms) を壁時計で刻む」ためで、周期がそのままフレーム数になる:
	// 12.5fps だと中間フレームが 2 枚しか出ず (4行 → 30行 → 実画面)、演出でなく点滅に見える。
	// 🚨 上げても遅くはならない: 進捗は壁時計なので、端末が追いつかなければフレームが間引かれる
	// だけで所要は変わらない (フレーム数で進める glide とはここが違う)。
	zoomInterval = 16 * time.Millisecond
	// maxPanelJobs は job パネルに一度に表示する行数。超過分はパネル内でスクロールする。
	maxPanelJobs = 10
	// usageRefreshInterval は usage オーバーレイをバックグラウンド再取得する周期 (ユーザー要望
	// 2026-07-22)。トークン課金は発生しないが「安価」ではない: 1 回 ≈ 2.0s wall / 1.8s CPU
	// (実測 2026-07-25)。この値は「表示の許容陳腐度」の単一の出典で、周期以外に 2 つの派生が
	// ぶら下がる: usageCacheTTL (起動時のキャッシュ有効期間) と usageOverlay.stale
	// (非表示中に止めたリフレッシュを U の再表示で取り戻す閾値)。どちらも定数を参照しているので
	// 値を変えれば自動で追従する — が、下記は追従しないので手で揃えること。
	// 🚨 実装で強制できない 2 つの制約 (変更時に再評価すること):
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
// 非フォーカス中に止める案の判断は spinnerActive のコメント参照 (このチェーンも同じ扱い)。
func usageRefreshTick() tea.Cmd {
	return tea.Tick(usageRefreshInterval, func(time.Time) tea.Msg { return usageRefreshMsg{} })
}

// ciResultMsg は一括取得 1 チャンク分の結果。一括取得は chunkSHAs で複数チャンクへ割って
// 並列に投げるので、この msg は 1 回の取得につき複数回届く (startCIFetch 参照)。
// shas はそのチャンクが担当した SHA — unknown 埋め / detailsLoading 解除 / toFetch からの
// 差し引きをチャンクの範囲に閉じるために要る (全 toFetch を使うと、まだ飛んでいるチャンクの
// SHA を「応答に無かった」と誤判定して unknown に落としてしまう)。
type ciResultMsg struct {
	batch CIBatch
	ghErr *GHError
	shas  []string
	epoch int // 発行時点の fetchEpoch (世代不一致の遅延チャンクを捨てるため)
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

// issuesRestoreMsg は前回終了時の issues viewer の画面を復元する合図 (起動時。issues_state.go)。
// repo の照合まで済んでいるので、受け取り側は開くだけ。
type issuesRestoreMsg struct{ screen issuesScreen }

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

// ciPollMsg は CI 追従ポーリングの周期タイマー。追従対象 (ciPollTargets) が 1 つでもある間、
// glogx を開いているあいだずっと回る — パネルの開閉・push からの経過時間・起動からの経過に
// 依存しない (「pending なら常に追う」の単一の出典)。gen は reloadAfterPull の世代で、
// リロード後に古いタイマーが二重チェーンとして復活するのを防ぐ。
type ciPollMsg struct{ gen int }

// ciPollResultMsg は ciPoll が投げた再取得の結果。一括取得 (ciResultMsg) と分けているのは、
// fetching / detailsLoading を立てず表示を「取得中」に落とさないため (チラつき防止)。
// gen は投げた時点の世代で、リロードを跨いで着弾した結果を捨てるために持つ
// (ciResultMsg の epoch と同じ役目)。
type ciPollResultMsg struct {
	gen     int
	targets []string
	batch   CIBatch
	ghErr   *GHError
}

const (
	ciPollInterval = 3 * time.Second
	// ciAwaitMaxAttempts は「push したのに CI が 1 つも現れない」ケースを諦めるまでの回数
	// (3s × 40 = 最長 2 分)。workflow を持たない repo で永久にポーリングしないための上限で、
	// pending が一度見えたら対象は awaitCI を卒業するのでこの上限は掛からない (完了まで追う)。
	ciAwaitMaxAttempts = 40
)

// rerunMsg は CI job 再実行要求 (r → y 確認後の gh run rerun --job) の結果。glogx の独自機能。
// sha は対象コミット (パネルリフレッシュの照合用)。
type rerunMsg struct {
	sha string
	err error
}

// rerunPollGrace は rerun 直後にパネル SHA を追従対象へ留める周期回数 (ciPollInterval × 10 = ~30s)。
// rerun を要求してから GraphQL に queued/in_progress が映るまでラグがあり、その間は状態が
// success/failure のままで追従対象にならない (パネルの ✗ が固まったままになる) ため、
// pending が見えるまでの間だけ空振りを許す。上限到達で諦める (反映は次の開き直しで)。
const rerunPollGrace = 10

// noPromptGitCmd は remote に触る git (push/pull) 用のコマンドを組む。GIT_TERMINAL_PROMPT=0
// で「認証情報が要るのに helper が無い」場合に /dev/tty へ対話プロンプトを出させず即エラーに
// する: bubbletea が同じ端末を raw mode で握っているため、git が tty を奪うと表示が壊れ入力
// 挙動が未定義になる (対話認証は TUI の外でやるべき作業)。タイムアウトは付けない — 正当な
// 巨大 push が遅い回線で中断される方が push 失敗として有害なため (レビュー K2)。
// pullMsg は git pull --rebase の実行結果 (u → y 確認後)。glogx の独自機能。
type pullMsg struct{ err error }

// updateBeginMsg は「早期リターン判定 (installedIsLatest) を通過した = 実際に自己更新を
// 走らせる」合図。C/X 押下 → startUpdate (判定。モーダルなし) → updateBeginMsg →
// actModal.runUpdate (spinner モーダルあり) の 2 段構え。
type updateBeginMsg struct{ target string }

// updateMsg は `claude update` の実行結果 (C キー、確認なし即実行)。glogx の独自機能。
// before/after は update 前後の CLI バージョン (取得失敗時は空)。両方取れて差があれば
// "vX → vY"、同じなら「変更なし」を notice に出す。
type updateMsg struct {
	target string // "claude" / "codex" (結果トーストの出し分け用)
	before string
	after  string
	// note は update 成功時の CLI 出力末尾行 (early / 失敗時は空)。before == after のとき
	// 「なぜ変わらなかったか」を語る唯一の材料 (runCLIUpdate の note を参照)。
	note string
	err  error // 失敗時のみ。Error() は CLI 出力の末尾行を含む
	// early は「すでに latest」の判定による早期リターン (runUpdate の実行結果ではない)。
	// 実行結果と区別できないと、C 連打で並走した判定の結果が走行中の update を降ろす。
	early bool
}

// updateNoteSuffix は CLI 出力の末尾行をトーストへ添える接尾辞にする ("" ならなし)。
// before == after のときだけ使う: 「更新しなかった理由」は CLI 自身の言葉が一次情報で、
// glogx が要約すると (「最新です」がまさにそうだった) 誤訳になる。
func updateNoteSuffix(note string) string {
	if note == "" {
		return ""
	}
	return ": " + note
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
	// toFetch は一括取得の「まだ結果が来ていない」SHA。チャンクが着弾するたびに縮む
	// (fetching() も pendingFetches==0 で下りる)。パネルの「進行中の一括取得を待つ」ガードが
	// これを見るので、既に着弾したチャンクの SHA で待たされないようにするために縮める必要がある。
	toFetch []string
	// pendingFetches は未着の一括取得チャンク数。**取得中かどうかの唯一の出典**で、
	// fetching() がここから導出する (issue 224。以前は fetching bool を並置し、4 箇所で
	// 手書き同期していた。1 箇所書き忘れるとスピナーが回りっぱなし / パネルが待たずに
	// 二重取得する形で silent に狂い、build もテストも通った)。
	// (スピナー・invalidate gate・パネルガードが fetching を見るので、最後のチャンクが
	// 着弾するまで下ろせない)。
	pendingFetches int
	// fetchEpoch は一括取得の世代。startCIFetch が張り替えるたびに進める。pull/push の
	// 再取得が toFetch/pendingFetches を新世代へ上書きした後、旧世代のチャンクが届くと
	// カウンタを誤減算し (スピナー早期消灯)、取得中 SHA を toFetch から誤って間引く
	// (同一 SHA 並行リクエストのガード無効化) ため、世代不一致の ciResultMsg は捨てる
	fetchEpoch int
	repo       Repo
	hasRepo    bool
	ghErr      *GHError
	decor      *DecorColors
	oneline    bool
	colored    bool
	// showFrame は最外周フレーム (板 + ドロップシャドウ) 描画の有効フラグ (issue 025)。起動時固定
	// (!opts.NoFrame)。🚨 下の frame (int) はスピナーのフレームカウンタで別物 (名前衝突回避のため
	// bool 側を showFrame とした)。実際に描くかは frameActive() が端末サイズ下限も見て判定する。
	showFrame    bool
	frame        int
	width        int
	height       int
	cursor       int    // コミット index
	offset       int    // ビューポート先頭の行 index (論理 = カーソル可視化の着地点)
	panelSHA     string // job パネルを表示中のコミット SHA ("" = パネルなし)
	panelCursor  int    // パネル内で選択中の job index (-1 = タイトル行にフォーカス)
	panelGrace   int    // rerun 直後、pending が見えなくてもパネル SHA を追従対象に留める残回数
	copyOnDetail string // Y で詳細未取得だった detailKey。jobDetailMsg 到着時にコピーして消す ("" = 予約なし)
	// job 詳細ポップアップ (annotations / ログ tail) の pager 状態と描画は jobDetailOverlay 型
	// (job_detail_overlay.go) に切り出す。panel-frame (panelSHA/panelCursor/panelGrace) と ETA・
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
	// actionModal 型 (action_modal.go) に切り出す。実行の orchestration (awaitCI 編成・
	// reloadAfterPull・結果整形) は CI/コミット状態と密結合なので browseModel 側に残す。
	actModal       actionModal
	pullAnimating  bool            // pull 後に先頭へ増えた新規コミット行を上から降らせる演出中 (offset が進行度)
	opts           *Options        // pull 後のコミット再読込に使う (revs / max-count)
	awaitCI        map[string]bool // push 直後で CI がまだ 1 つも見えない SHA (見えたら外れる。上限は ciAwaitMaxAttempts)
	awaitAttempts  int             // awaitCI の試行回数 (上限で諦める)
	ciPolling      bool            // ciPollMsg の自己更新チェーンが 1 本生きているか (single-flight)
	ciPollInFlight bool            // ciPoll の再取得が in-flight (同一 SHA への GraphQL 並行を避ける)
	ciPollGen      int             // ciPollMsg の世代 (reloadAfterPull で進めて残タイマーを無効化)
	lastWarning    string          // w でコピーする直近の警告/エラー文字列 (トーストが消えても保持。issue 026)
	tmuxPrefix     string          // tmux prefix の bubbletea 表記 (例 "ctrl+t")。"" = tmux 外/不明で機能オフ
	verbatim       []Line          // git log 実出力の取り込み行 (nil = 自前レンダリング)

	// usage オーバーレイ (右上に Claude Code の /usage 残量を重ねる)。ユーザー要望 2026-07-21。
	// 状態と描画は usageOverlay 型 (usage_overlay.go) に切り出し、ここは 1 フィールドだけ持つ。
	usageOv usageOverlay

	// コミット一覧のスクロール glide (表示 offset を論理 offset へ滑らせる)。実体は
	// scroll_glide.go の共有型で、diff pager / issues viewer も同じ型を持つ (手触りを揃える)。
	glide scrollGlide

	// バックグラウンド再ビルド (bin/lib/go_autobuild.zsh --async) の決着監視。zero value は
	// 「監視しない」で、shim が GO_AUTOBUILD_PENDING を立てた起動でだけ動く (autobuild.go)。
	autobuild autobuildWatch

	// 別プロセス (別ターミナルの commit / rebase・Claude Code・pull) による git log の変化の
	// 見張り。状態と判定は gitLogWatch 型 (gitlog_watch.go) に切り出し、反映は reloadLog を使う。
	logWatch gitLogWatch

	// issues viewer (i キーで開く全画面の issue ブラウザ)。状態と描画は issuesView 型
	// (issues_view.go) に切り出し、ここは 1 フィールドだけ持つ。読む規約の一次情報は
	// docs/issues-viewer-spec.md。
	issuesOv issuesView
	// ratelimit ダッシュボード (R キーで開く全画面の残量ビュー)。usage オーバーレイと同じ
	// Snapshot を枠ごとのアナログ盤にして描く (取得経路は usageOv のものを共用。
	// ratelimit_dashboard.go 冒頭)。
	rlDash ratelimitDash
	// doctorOv は D の全画面 doctor (doctor_view.go)。rlDash と同じ薄い器で、開くと走査を始める
	doctorOv doctorView
	// status viewer (s キーで開く全画面の作業ツリービュー)。stage / unstage / 変更を捨てる を
	// 行う write 側の画面で、状態と描画は statusView 型 (status_view.go) に切り出す。
	// 読み書きの規約の一次情報は docs/status-viewer-spec.md。
	statusOv statusView
	// restartPending は「裏ビルドが完成したので再起動を提案したい」= 保留中の印。
	// 🚨 これは「出したい」であって「出ている」ではない。実際に出すかは restartPromptVisible()
	// が決める (中断できない処理の最中は出さない)。表示と入力の両方が同じ述語を見る契約。
	// restartRequested は「終了後に新しいバイナリで自分を置き換える」印 (main.go が exec する)。
	restartPending   bool
	restartRequested bool
	// lastKey / lastKeyAt はキーリピート (押しっぱなし) の判定用 (swallowKeyRepeat)。
	lastKey   string
	lastKeyAt time.Time
	// zoom は画面全体の開閉演出 (zoom.go)。zero value = 演出なしなので、Init を通らない
	// 経路 (テスト・静的出力) は今までどおり実画面がそのまま出る。
	zoom appZoom

	// toast は右下に数秒だけ出す結果フィードバック (push/pull 完了)。自動消滅 (toast.go)。
	toast toast

	ticking bool // 80ms スピナー tick チェーンが 1 本生きているか (maybeTick の single-flight)
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
	// 上限超過分は問い合わせず StateUnknown 表示のまま (capFetchSHAs のポリシー)。
	// toFetch に残すと「取得中」のまま永遠に解決しない
	toFetch = capFetchSHAs(toFetch)
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
		cancel:         cancel,
		usageOv:        usageOverlay{visible: true},
		issuesOv:       newIssuesView(),
		statusOv:       newStatusView(),
		autobuild:      newAutobuildWatch(selfExePath(), os.Getenv(autobuildPendingEnv) != "", timeNow()),
	}
	// 🚨 ここは「取得中か」ではなく **「これから取得を始めるか」** の判定なので、
	// fetching() (= pendingFetches > 0) を使ってはいけない。pendingFetches はこの下の
	// チャンク分割で初めて入る (issue 224 で fetching フィールドを消したときの落とし穴)。
	if len(toFetch) > 0 {
		// 起動時の一括取得は m.cancel と ctx を共有する意図的例外 (q 中断で走行中の
		// GraphQL を止めるため)。チャンクへ割って並列に投げるので、画面に映っている先頭
		// コミットの CI が最初に埋まる (startCIFetch の分割方針)。
		//
		// 🚨 チャンク closure で cancel() を defer しないこと: 共有 ctx なので最初に終わった
		// チャンクが残りを巻き添えキャンセルしてしまう。timer の解放は m.cancel
		// (main.go の defer browse.cancel() と quit 経路) が担う。
		chunks := chunkSHAs(toFetch)
		m.pendingFetches = len(chunks)
		epoch := m.fetchEpoch // closure は別 goroutine で走るので値で捕捉 (m の直読みは race)
		cmds := make([]tea.Cmd, 0, len(chunks))
		for _, chunk := range chunks {
			cmds = append(cmds, func() tea.Msg {
				batch, ghErr := fetchCIChunk(ctx, ExecRunner, repo, chunk)
				return ciResultMsg{batch: batch, ghErr: ghErr, shas: chunk, epoch: epoch}
			})
		}
		m.fetch = tea.Batch(cmds...)
	}
	return m
}

func (m *browseModel) Init() tea.Cmd {
	// tmux prefix の取得は非同期 (fork 1 本 ≈ 6ms を初期描画のクリティカルパスに乗せない)
	prefix := func() tea.Msg { return prefixMsg{key: loadTmuxPrefix()} }
	// 起動時はディスクキャッシュ可 (連続起動のたびに claude subprocess を起こさない)
	u := m.usageOv.fetchCmd(true)
	// usage を起動時に取得するため tick を常に起動する (取得中スピナーを回す。取得完了で
	// spinnerActive が false になり tick は自然に止まる)。CI fetch の有無に依らず起動する。
	// usageRefreshTick で 1 分ごとのバックグラウンド再取得チェーンも起動する (ユーザー要望)。
	// Claude Code / codex の新バージョン検出 (claude_version.go / codex_version.go)。
	// どちらもバックグラウンド 1 回きりで、結果は *UpdateAvailableMsg (更新なし/失敗は
	// nil Msg で無音)。
	ver := tea.Batch(checkClaudeVersionCmd(), checkCodexVersionCmd())
	cliHealth := checkCLIHealthCmd()
	// doctor の起動時トースト: 前回のスキャン結果 (ファイル読みのみ、fork なし) が閾値を超えていれば
	// 文言で D へ誘導する。起動時に走査はしない (issue 148 の骨格)。
	if c, ok := loadDoctorDiskCache(); ok {
		if text := doctorStartupToast(c, ok, timeNow()); text != "" {
			m.toast.showInfo(text)
			markDoctorNotified(timeNow())
		}
	}
	// バックグラウンド再ビルドの監視 (autobuild.go)。shim が GO_AUTOBUILD_PENDING を
	// 立てていない通常起動では nil = tick が増えない。
	//
	// 「ビルド中」はここで即出す: 完成を待つと数十秒遅れ、その間ユーザーは自分が旧版を触って
	// いることを知らない (ユーザー要望 2026-07-31)。
	if res, notify, _ := m.autobuild.handle(autobuildRunning, timeNow()); notify {
		text, ok := autobuildToast(res)
		m.toast.show(text, ok)
	} else if autobuildStaleBinary(selfExePath()) {
		// ビルド中でもないのに失敗記録が残っている = 旧版に固定された状態。「ビルド中」と違って
		// 自然には解消しないので、コピー可能な警告 (w) として出す。
		//
		// ビルド中を優先する理由: 再挑戦が走っているなら、その決着 (成功/失敗) はこのセッションの
		// 監視が伝える。両方出すと 1 つの出来事に 2 枚のトーストが積まれる。
		text, _ := autobuildToast(autobuildStale)
		m.showWarning(text)
	}
	m.zoom.start(timeNow()) // 画面を中央から開く (zoom.go)。tick は下の maybeTick が回す
	ab := m.autobuild.tickCmd()
	// issues viewer を出したまま終了していたら、その画面を復元する (issues_state.go)。
	// ファイル読みだけ同期で済ませ、repo の照合 (git fork) は記憶があるときだけ非同期で行う
	// = 記憶が無い通常の起動では fork が増えない。
	var restore tea.Cmd
	if s, ok := loadIssuesScreen(timeNow()); ok {
		restore = issuesRestoreCmd(s)
	}
	// doctor を出したまま終了していたら、その画面を復元する (doctor_resume.go)。
	// 🚨 issues の復元とは**排他にしない**: 両方は同時に出ないので、後から開く doctor が
	// 前面に来る。どちらを優先するかは「最後に見ていた方」で決めたいが、記憶は別ファイルで
	// 順序を持たないため、ここでは doctor を後に開く (issues は下に残り、閉じれば出てくる)
	var doctorRestore tea.Cmd
	if tb, ok := loadDoctorScreen(timeNow()); ok {
		doctorRestore = m.doctorOv.toggle()
		m.doctorOv.tab = tb
	}
	// 🚨 起動時にも追従チェーンを張る: ディスクキャッシュに pending が残っていると初回 fetch が
	// 走らず (m.fetching() == false)、ciResultMsg 起点の開始点をどれも踏まないまま
	// 「pending なのに追わない」状態になる (キャッシュの pending TTL 内に再起動した場合)。
	poll := m.ensureCIPoll()
	// 別プロセスによる git log の変化の見張り (gitlog_watch.go)。対象ディレクトリの解決は
	// git を叩くので非同期にし、保険のポーリングだけ先に張る (イベント待ちは解決後に張る)。
	logWatch := tea.Batch(gitLogWatchDirsCmd(), m.gitLogPollCmd())
	if m.fetching() {
		return tea.Batch(m.fetch, prefix, u, ver, cliHealth, ab, restore, doctorRestore, poll, logWatch, m.maybeTick(), usageRefreshTick())
	}
	return tea.Batch(prefix, u, ver, cliHealth, ab, restore, doctorRestore, poll, logWatch, m.maybeTick(), usageRefreshTick())
}

// issuesRestoreCmd は記憶した画面が今の repo のものか確かめる (別 repo で開いた glogx に
// 前の repo の issue を出さない)。一致しなければ nil Msg = 無音で通常起動。
func issuesRestoreCmd(s issuesScreen) tea.Cmd {
	return func() tea.Msg {
		if issues.RepoRoot(currentDir()) != s.Root {
			return nil
		}
		return issuesRestoreMsg{screen: s}
	}
}

// currentDir は探索の起点にする cwd (取れなければカレント相対)。glogx は tmux popup から
// -d '#{pane_current_path}' で起動されるので、cwd は repo のサブディレクトリになりうる。
func currentDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "." // cwd が取れない環境でも repo 直下として探索を試みる
	}
	return cwd
}

// normalizeSpaceKey は Space の表記ゆれを " " へ揃える。キー switch は " " だけを見ればよい。
//
// なぜ 2 表記あるか: bubbletea v2 の KeyPressMsg.String() は Space を "space" と綴る (v1 は
// " ")。一方、複数ルーンが 1 イベントで届いたときの分解経路 (Update の KeyPressMsg 処理) は
// 生のルーン文字列を渡すので " " が来る。v2 移行時にこの綴りの変化を拾い落として、Space の
// 割当 5 箇所 (issues viewer の半ページ / diff pager / job 詳細を閉じる / パネルを開く) が
// 全て無反応になっていた (ユーザー報告 2026-07-31)。入口で正規化して 1 箇所に閉じる。
func normalizeSpaceKey(key string) string {
	if key == "space" {
		return " "
	}
	return key
}

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return tickMsg{} })
}

// maybeTick は tick を single-flight で仕込む。既にチェーンが 1 本生きていれば nil を返して
// 二重チェーンを作らない。tea.Batch(cmd, maybeTick()) は Init・各 fetch 経路など多数に散らばり、
// 非同期処理が重なるたびに独立した自己増殖チェーンが恒久追加されて (push 直後ポーリングでは
// 最長 2 分間に ~48 本まで) 再描画/アニメが N 倍化していた (レビュー C1)。この single-flight で
// 全 tick 発行を 1 本に束ねる。🚨 ciPoll の tea.Tick は別周期の独立タイマーなので
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
	return tickEvery(m.tickInterval())
}

// tickInterval は今のフレーム周期。横に動く演出 (scroll glide / toast スライド / 引き出し) の
// 最中だけ ~30fps へ上げ、それ以外は 12.5fps に落とす。アプリ全体の開閉演出と
// issues / status viewer の開閉スライドはさらに上げる (理由は zoomInterval の doc)。
//
// 🚨 高 FPS が要る演出の登録先はここだけ。spinnerActive は「tickInterval が周期を上げているか」で
// 演出の有無を導出するので、ここに足せばチェーン維持にも効く。かつては両方へ手で足す規約で、
// 足し忘れると「回るが 12.5fps」になり点滅に見える再発が実際に 2 回起きた (2026-08-06 まで)。
func (m *browseModel) tickInterval() time.Duration {
	if m.zoom.animating(timeNow()) || m.issuesOv.slideAnimating() || m.statusOv.slideAnimating() {
		return zoomInterval // 短い演出なので周期がそのままフレーム数になる (60fps)
	}
	if m.glide.active || m.diffOv.animating() || m.toast.animating() || m.issuesOv.animating() || m.statusOv.animating() {
		return scrollInterval // スライドを滑らかに (30fps)
	}
	return spinnerInterval
}

// fetchCIStatusesCmd は targets の CI 状態取得を tea.Cmd にする。ctx/timeout/defer cancel の
// ボイラープレートを 1 箇所へ集約し、wrap で結果を各 msg (ciResult/detail/basis) に包む
// (レビュー U1)。同一 SHA 並行取得を避ける注意 (ciPollMsg / fetchPanelDetails のコメント)
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

// startCIFetch は一括取得を chunkSHAs のチャンクへ割り、チャンクごとに ciResultMsg を返す Cmd を
// 束ねて返す。取得の「開始点」は 3 箇所 (起動 / reloadAfterPull / refetchAfterPush)
// あるので、状態 (toFetch / pendingFetches / ghErr) の立て方をここへ集約する。
//
// チャンク 1 が commits の表示順先頭なので、画面に映っているコミットの CI が最初に埋まる
// (ビューポート計算もカーソル追従も不要。commits が既に表示順なのを利用している)。
//
// ghErr はここでクリアする: チャンクごとに無条件代入すると、後から着弾した成功チャンクが先の
// 失敗チャンクの警告を消してしまう。「新しい取得で警告をリセットする」(レビュー C4 の
// sticky 警告防止) は開始時にやり、ハンドラ側は非 nil のときだけ立てる。
// fetching は一括取得のチャンクが 1 つでも未着か。**pendingFetches からの派生**であって
// 独立した状態ではない (issue 224)。フィールドとして並置すると、新しく「取得を始める /
// 終える」経路を足したときに代入を書き忘れ、スピナー (spinnerActive)・再描画ゲート
// (tickMsg)・パネルの「進行中の一括取得を待つ」ガード・ciPollMsg の並行抑止が
// **silent に狂う** (build もテストも通る)。導出にすればその失敗モードが構造的に消える。
func (m *browseModel) fetching() bool { return m.pendingFetches > 0 }

func (m *browseModel) startCIFetch(shas []string) tea.Cmd {
	shas = capFetchSHAs(shas) // 超過分は StateUnknown のまま (newBrowseModel と同じポリシー)
	chunks := chunkSHAs(shas)
	m.fetchEpoch++ // 旧世代の未着チャンクを無効化 (ciResultMsg ハンドラの世代ガード)
	m.toFetch = shas
	m.pendingFetches = len(chunks)
	m.ghErr = nil
	cmds := make([]tea.Cmd, 0, len(chunks))
	for _, chunk := range chunks {
		cmds = append(cmds, fetchCIChunkCmd(m.repo, chunk, m.fetchEpoch))
	}
	return tea.Batch(cmds...)
}

// fetchCIChunkCmd は 1 チャンク = GraphQL 1 リクエスト分の取得 Cmd。
//
// 🚨 FetchCIStatuses を通さないこと: あちらも内部で chunkSHAs するので、既に割ったチャンクを
// 渡すともう一段割られて同時リクエスト数が fetchConcurrency の二乗側 (最大 16 本) へ膨らむ。
// 分割はここ (startCIFetch) の 1 段だけに保つ。
func fetchCIChunkCmd(repo Repo, chunk []string, epoch int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		batch, ghErr := fetchCIChunk(ctx, ExecRunner, repo, chunk)
		return ciResultMsg{batch: batch, ghErr: ghErr, shas: chunk, epoch: epoch}
	}
}

// mergeCIBatch は CIBatch 応答を browseModel のキャッシュへ吸収する。fetched (終了時 SaveCache
// 用の負キャッシュ) と statuses (表示用) は常に同一 source を受け、details / prCache も一緒に
// 更新される — この「4 キャッシュを 1 単位で co-update する」不変条件を ciResult/detail/basis の
// 3 ハンドラから 1 箇所へ局所化する (第 5 の co-update map が増えても touch は 1 箇所)。PR は
// コミット行のバッジ表示と p キーの両方で使う。site 固有の invalidateLines / ghErr クリア /
// detailsLoading 解除 / panelCursor クランプ / awaitCI 掃除は各ハンドラに残す (吸収の関心事ではない)。
func (m *browseModel) mergeCIBatch(statuses map[string]CIState, details map[string][]CheckDetail, prs map[string]*PRRef) {
	maps.Copy(m.fetched, statuses)
	maps.Copy(m.statuses, statuses)
	maps.Copy(m.details, details)
	maps.Copy(m.prCache, prs)
}

// showWarning は失敗/警告トーストを出しつつ lastWarning に残す (w で表示が消えた後もコピー
// できるように。issue 026)。
//
// どの失敗をここへ通すかの基準 (🚨 以前は「失敗は必ずこれを経由」と書いてあったが、実際には
// 素の toast.show(…, false) が 26 箇所あり doc の方が現実より強かった。正しくは 3 分類):
//
//   - エラー詳細を含み、後で見返す価値があるもの (「pull に失敗: …」) → showWarning
//   - 操作を拒否した理由の案内 (「未 push のコミットはありません」) → 素の toast.show(…, false)。
//     コピー対象でないうえ、lastWarning を上書きすると直前のエラーがコピー不能になる
//   - クリップボード操作そのものの失敗 → 素の toast.show(…, false)。w (警告コピー) の対象を
//     潰さないため。そもそもクリップボードが壊れているなら警告のコピーも失敗する
//
// 成功トースト (toast.show(…, true)) もこれを通さない (成功文言で lastWarning が上書きされると
// 直前のエラーがコピー不能になる)。
func (m *browseModel) showWarning(text string) {
	// lastWarning は w でクリップボードへコピーされる = 端末の外へ出る値なので、
	// 表示 (toast 側で無害化) とは別にここでも無害化する。制御文字入りの文字列を
	// 貼り付け先へ持ち出さない
	m.lastWarning = sanitizePlainLine(text)
	m.toast.show(text, false)
}

// deliverNotice は viewer の操作結果 (takeNotice の戻り) をトーストへ流す。成功はトースト、
// 失敗は w でコピーできるよう lastWarning にも積む (showWarning)。戻り値は「流したか」で、
// 呼び出し側がトーストを動かす tick を束ねるかの判断に使う。
// 🚨 setNotice は打鍵経路 (handleKey 直後) だけでなく Msg 経路 (issuesScanMsg → rebindOpen)
// からも置かれる。Msg 経路の呼び出し側でもこれを通すこと。通さないと「置いたが誰も取り出さず、
// 次の打鍵まで画面に出ない」黙殺になる (issue 059: 本文が無言で畳まれた)。
func (m *browseModel) deliverNotice(text string, ok bool) bool {
	if text == "" {
		return false
	}
	if ok {
		m.toast.show(text, true)
	} else {
		m.showWarning(text)
	}
	return true
}

// showClaudeUpdate は「新バージョンあり」の通知を出す。
//
// 以前は先行トーストを潰さないよう専用タイマーで遅延再送していたが、
// トーストが積めるようになったので調停は不要になった (toast の doc 参照)。
func (m *browseModel) showClaudeUpdate(latest string) tea.Cmd {
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
		m.invalidateLines() // 幅で折り返し行数が変わる
		// resize 中の glide は破棄して即時にする (表示 offset が stale になるため)。一覧だけでなく
		// pager 側も止める: 幅で行数が変わり、glide の着地点が resize 前の行数基準で古くなる。
		m.glide.stop()
		m.diffOv.glide.stop()
		m.issuesOv.bodyGlide.stop()
		m.statusOv.pagerGlide.stop()
		m.ensureCursorVisible()
		return m, nil
	case tickMsg:
		m.ticking = false // このチェーンが 1 拍消費した。継続は下の maybeTick で単一に保つ
		// 閉じる演出が着地したらここで初めて終了する (演出の間はまだ描き続ける)
		if m.zoom.settle(timeNow()) {
			m.done = true
			return m, tea.Quit
		}
		// issues viewer の閉じる演出が着地したらここで畳む。🚨 下の spinnerActive の早期 return
		// より前に置く: animating() は closing のあいだ true を返し続け、この settleClose が
		// 下ろして初めて false になる。後ろに置くと最後の 1 拍が届かず閉じかけの姿で固まる。
		m.issuesOv.settleClose()
		m.statusOv.settleClose() // status viewer も同じ理由で spinnerActive の判定より前に畳む
		if !m.spinnerActive() {
			return m, nil // アニメ対象なし: 再アームせずチェーンを終わらせる
		}
		if m.pullAnimating {
			m.advancePullAnim() // pull で増えた新規行を 1 行/フレームで上から降らせる
		}
		if m.glide.active {
			m.glide.advance(m.offset) // 一覧のスクロールを表示 offset で滑らせる
		}
		// pager 側 (diff / issues 一覧・本文) の glide も同じ tick で進める。共有型なので
		// 手触り (カーブ・フレーム数) は自動的に一覧と揃う (scroll_glide.go)。
		m.diffOv.advanceGlide()
		m.issuesOv.advanceGlide()
		m.statusOv.advanceGlide()
		var pushRefetchCmd tea.Cmd
		if m.pushAnimating {
			pushRefetchCmd = m.advancePushAnim() // push 境界の罫線を 1 コミット/フレームで上へ
		}
		for sha, start := range m.pushSlides {
			if timeNow().Sub(start) >= pushSlideDuration {
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
		// list に毎フレーム変化する内容 (loading スピナー) が乗るのは fetch/awaitCI の 2 状態
		// だけ。他の spinnerActive 条件 (panelHasRunningJob/pullAnimating/detailsLoading/
		// jobDetailBusy/diffBusy) のスピナー・経過時間は panelLines/diffBoxLines 側 (lines() の
		// 外) で毎フレーム描かれるので、ここで list を無効化すると -p 巨大 patch を含む全行を
		// 80ms ごとに組み直すだけの無駄になる (レビュー C7)。offset を動かす pull アニメも
		// lines() は不変なので invalidate 不要 (View が窓を切り直す)
		//
		// 🚨 この 2 状態では「header 行のスピナー字形」を変えるために全行を組み直している。
		// perf 監査 2026-07-25 で指摘されたが、さらに絞る対応はしないと判断した:
		// 既定 (patch なし) の RenderLines は 7.8µs / 12.6KB で 80ms 周期に対し無視できる。
		// 効くのは -p 併用時 (実測 332µs / 733KB per frame) だけで、それも fetch 中の数秒に限る
		// = 80ms 予算の 0.4%。一方 header 行だけの差分更新にするには Line の契約 (Text に
		// 字形が焼き込まれている) を変えるか、verbatim / mediumLines の 2 経路へ header 生成を
		// 複製する必要があり、表示バグの危険が利得に見合わない。
		// -p を常用して fetch 中の描画が重いと体感したら再評価する (その時は Line にスピナー
		// 位置を持たせて View 側で差し替えるのが筋)。
		if m.fetching() || len(m.awaitCI) > 0 {
			m.invalidateLines()
		}
		return m, tea.Batch(m.maybeTick(), toastHoldCmd, pushRefetchCmd)
	case ciResultMsg:
		// 世代不一致 = pull/push の再取得 (startCIFetch) 前に飛ばした旧世代チャンク。
		// 丸ごと捨てる (データも捨てる: 同じ SHA は新世代のチャンクが取り直す)
		if msg.epoch != m.fetchEpoch {
			return m, m.maybeTick()
		}
		m.invalidateLines()
		// チャンクごとに届くので、成功チャンクで先の失敗チャンクの警告を消さない
		// (取得開始時のリセットは startCIFetch が担う)
		if msg.ghErr != nil {
			m.ghErr = msg.ghErr
		}
		// 応答に無かった SHA は unknown で埋める (fetched へ入れる = 終了時に SaveCache
		// される 30 秒の負キャッシュ)。q での中断 (fillUnknown) と違い、こちらは API の
		// 実際の返答に基づく確定。範囲はこのチャンクの SHA だけ — 全 toFetch で埋めると
		// まだ飛んでいるチャンクの SHA を「応答に無かった」と誤判定する
		filled := fillUnknownFetched(msg.batch.Statuses, msg.shas)
		m.mergeCIBatch(filled, msg.batch.Details, msg.batch.PRs)
		// 未着チャンクが残っている間は fetching() が true のまま (スピナー・invalidate gate・
		// パネルガードの出典)。toFetch は着弾ぶんを差し引き、パネルが既に結果の来た SHA で
		// 待たされないようにする
		m.pendingFetches = max(m.pendingFetches-1, 0)
		// 🚨 ここを slices.DeleteFunc で書かないこと: あれは in-place 圧縮して破棄した末尾を
		// ゼロ埋めするが、chunkSHAs は元スライスの部分スライスを返すので m.toFetch と
		// 未着チャンクの msg.shas は同じ配列を共有している。in-place に削ると「まだ飛んでいる
		// チャンクの SHA 列」が空文字へ潰れ、その結果が届いても unknown 埋め・loading 解除の
		// 対象を失う。新しいスライスへ写して共有配列に触らない。
		remaining := make([]string, 0, len(m.toFetch))
		for _, sha := range m.toFetch {
			if !slices.Contains(msg.shas, sha) {
				remaining = append(remaining, sha)
			}
		}
		m.toFetch = remaining
		// 一括取得待ちでパネルを開いていた SHA の loading を解除する (結果が来なかった
		// SHA も含めて解除。details 不在は「(CI job 情報なし)」表示に落ちる)
		for _, sha := range msg.shas {
			delete(m.detailsLoading, sha)
		}
		m.settleAwaitCI()
		// 一括取得で実行中コミットの Details が入った場合も ETA basis を補充する
		// (パネルを取得中に開いていたケース)。pending が見えたらここで追従チェーンが張られる
		// (起動直後に既に pending なコミットがある場合の開始点でもある)。
		return m, tea.Batch(m.maybeFetchETABasis(), m.ensureCIPoll())
	case detailMsg:
		m.invalidateLines()
		delete(m.detailsLoading, msg.sha)
		m.ghErr = msg.ghErr // 成功時 (nil) はクリア: ciResultMsg と揃える (sticky 警告の防止・レビュー C4)
		m.mergeCIBatch(msg.batch.Statuses, msg.batch.Details, msg.batch.PRs)
		// リフレッシュで job 数が縮んだ場合にフォーカスを範囲内へ戻す
		if msg.sha == m.panelSHA && m.panelCursor >= len(m.details[m.panelSHA]) {
			m.panelCursor = len(m.details[m.panelSHA]) - 1
		}
		// ETA basis (同名完了 job) の補充と追従チェーンの開始をここで行う (openPanel 時点では
		// details 未取得で pending かどうか判定できない)。ensureCIPoll は single-flight なので
		// 既にチェーンが生きていれば nil を返す (二重 timer で加速しない)。
		return m, tea.Batch(m.maybeFetchETABasis(), m.ensureCIPoll())
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
		// 🚨 ghErr は共有 sticky 警告なので detailOv.receive に閉じず browseModel で無条件代入する。
		// receive は busy 落とし・cache 格納・(今開いている詳細なら) 末尾スクロールを担う。currentKey は
		// live な detailKey() を渡す (snapshot 禁止: リフレッシュで panelCursor がクランプされ得るため)。
		m.detailOv.receive(msg, m.detailKey(), m.visibleDetailRows())
		// Y で詳細取得を待っていたら、到着したこの内容をコピーする (issue 020)。取得失敗
		// (ghErr) は上の sticky 警告に任せ、予約だけ静かに破棄する。
		// 🚨 予約後にフォーカスが動いていたら (detailKey() != msg.key) コピーしない: msg.lines は
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
		// 取得中にカーソルが動いていたら、結果の反映 (キャッシュ・バッジ) だけ行い
		// 自動オープン・トーストは出さない: 離れたコミットの PR がいきなりブラウザで
		// 開くのを防ぐ (jobDetailMsg / prStatusMsg と同型の stale ガード)
		wasCurrent := len(m.commits) > 0 && m.commits[m.cursor].SHA == msg.sha
		if msg.ghErr != nil {
			// 一時エラーをキャッシュすると「PR はありません」という誤答が固定される
			// (次の p で再試行させる) ため、キャッシュは成功時のみ
			if wasCurrent {
				m.showWarning("PR の取得に失敗しました: " + firstLine(msg.ghErr.Warning()))
			}
			return m, m.maybeTick()
		}
		m.prCache[msg.sha] = msg.pr
		m.invalidateLines() // コミット行の PR バッジに反映
		if !wasCurrent {
			return m, m.maybeTick()
		}
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
		// 警告は「今表示中の対象」の失敗のときだけ出す: 別 SHA へ移った後に届く遅延エラーで
		// 無関係な失敗 notice を被せない (prStatusMsg の wasCurrent と同型。receive は当該
		// sha が表示中ならエラー時に close するため、一致は receive の前に捕捉する)
		wasCurrent := msg.sha == m.diffOv.sha
		if err := m.diffOv.receive(msg); err != nil && wasCurrent {
			m.showWarning("diff の取得に失敗しました: " + firstLine(err.Error()))
		}
		return m, m.maybeTick()
	case prefixMsg:
		m.tmuxPrefix = msg.key
		return m, nil
	case usageMsg:
		m.usageOv.handle(msg)
		return m, nil
	case issuesWatchMsg:
		// viewer を開いている間だけ回る独立チェーン (issues_watch.go)。別プロセスの編集を
		// その場で反映する。🚨 maybeTick を束ねない: 反映は再スキャン (scanCmd) で、アニメは
		// 動かないため。フレーム tick を足すとこのチェーンの意図 (1s 周期) が崩れる。
		return m, m.issuesOv.handleWatch(msg)
	case issuesScanMsg:
		// 戻り値は畳まれていた取り直しの予約 (issuesView.receive の doc)。捨てると
		// 「自分がファイルを動かしたのに一覧が古いまま」が残る
		cmd := m.issuesOv.receive(msg)
		// receive → rebindOpen が置く notice (開いていた issue の消失) をこのフレームで配達する。
		// 打鍵経路の takeNotice に任せると、次のキーまで「本文が無言で畳まれた」に見え、
		// その次のキーが q だと理由が 1 度も描かれずに終了する (issue 059)。
		// maybeTick はトーストを出したときだけ束ねる (issuesWatchMsg の 1s チェーンの意図を崩さない)
		if m.deliverNotice(m.issuesOv.takeNotice()) {
			return m, tea.Batch(cmd, m.maybeTick())
		}
		return m, cmd
	case statusLoadMsg:
		// git status の結果 (status viewer)。返り値はプレビューの取り直し予約 (内容が変わった
		// ときだけ)。🚨 maybeTick も束ねる: 取得中スピナーを回していた場合、結果到着でそれを
		// 下ろすフレームが要る (下ろさないと最後のスピナー姿で固まる)。
		return m, tea.Batch(m.statusOv.receive(msg), m.maybeTick())
	case statusPollMsg:
		// viewer を開いている間だけ回る自動更新チェーン (spec 5 節)。🚨 maybeTick を束ねない:
		// 反映は読み直しだけでアニメは動かないため (issuesWatchMsg と同じ理由)。
		return m, m.statusOv.receivePoll(msg)
	case statusPreviewTickMsg:
		return m, tea.Batch(m.statusOv.receivePreviewTick(msg, m.colored), m.maybeTick())
	case statusPreviewMsg:
		m.statusOv.receivePreview(msg)
		return m, m.maybeTick()
	case issuesRestoreMsg:
		// 🚨 復元の repo 照合 (git fork) が返る前に type-ahead の s で status viewer が開いて
		// いたら復元を捨てる。restore は自分の shown しか見ないため、ここで弾かないと両 viewer
		// 同時 shown になり「見えている status」と「キーを受ける issues」が食い違う
		// (敵対レビューで再現 2026-08-06)
		// 🚨 ratelimit ダッシュボード (R) が先に開いていた場合も同じ理由で捨てる: 復元すると
		// 裏に issues viewer が開いた状態になり、ダッシュボードの i (横断) が toggle で
		// 「開く」ではなく「閉じる」に化ける。
		// 🚨 判定は activeFullScreen から導出する (issue 227): 全画面ビューアを 1 枚足した
		// ときにここへ書き足すのを忘れると、復元で 2 枚同時 shown になる。
		if id := m.activeFullScreen(); id != fullScreenNone && id != fullScreenIssues {
			return m, m.maybeTick()
		}
		return m, tea.Batch(m.issuesOv.restore(currentDir(), msg.screen), m.maybeTick())
	case claudeUpdateAvailableMsg:
		return m, m.showClaudeUpdate(msg.latest)
	case codexUpdateAvailableMsg:
		m.toast.show("codex v"+msg.latest+" が公開されています (X で更新)", true)
		return m, m.maybeTick()
	case doctorDiskMsg:
		return m, tea.Batch(m.doctorOv.receiveDisk(msg), m.maybeTick())
	case doctorDeleteMsg:
		return m, tea.Batch(m.doctorOv.receiveDelete(msg), m.maybeTick())
	case doctorSvcMsg:
		m.doctorOv.receiveSvc(msg)
		return m, m.maybeTick()
	case doctorDockerMsg:
		m.doctorOv.receiveDocker(msg)
		return m, m.maybeTick()
	case doctorBrewMsg:
		m.doctorOv.receiveBrew(msg)
		return m, m.maybeTick()
	case cliHealthMsg:
		for _, issue := range msg.issues {
			if text := cliHealthWarning(issue); text != "" {
				m.showWarning(text)
			}
		}
		return m, m.maybeTick()
	case autobuildMsg:
		// 裏のビルドが決着したらトーストで知らせる。
		//
		// 🚨 notify と keep は同時に立つ (handle は「開始を伝えた後も失敗を拾うため監視を続ける」を
		// この組み合わせで表す)。通知したときに tickCmd を張り直さないと監視チェーンが切れ、
		// その後のビルド失敗が二度と通知されない (監視は失敗を検出する唯一の経路。issue 032)。
		res, notify, keep := m.autobuild.handle(msg.result, timeNow())
		var watch tea.Cmd
		if keep {
			watch = m.autobuild.tickCmd()
		}
		if notify && res == autobuildInstalled {
			// 完成はその場で再起動できる合図なので、消えるトーストでなくダイアログで出す。
			// 🚨 ここでは「出したい」を立てるだけ。実際に出るのは中断できない処理 (claude update /
			// push / pull) が走っていないときで、判断は restartPromptVisible() が持つ
			m.restartPending = true
			return m, tea.Batch(m.maybeTick(), watch)
		}
		if notify {
			text, ok := autobuildToast(res)
			m.toast.show(text, ok)
			return m, tea.Batch(m.maybeTick(), watch) // トーストのスライドは tick で進む
		}
		return m, watch
	case autobuildSpawnMsg:
		// pull 後の裏ビルドが始まった。🚨 起動時とまったく同じ形で監視を張る (newAutobuildWatch →
		// handle で「ビルド中」を即出す → tickCmd)。完成すれば通常の autobuildMsg 経路が
		// 再起動ダイアログを出すので、ここで完成側の面倒を見る必要はない。
		if !msg.spawned || m.autobuild.active {
			return m, nil
		}
		m.autobuild = newAutobuildWatch(selfExePath(), true, timeNow())
		if res, notify, _ := m.autobuild.handle(autobuildRunning, timeNow()); notify {
			text, ok := autobuildToast(res)
			m.toast.show(text, ok)
		}
		return m, tea.Batch(m.autobuild.tickCmd(), m.maybeTick())
	case toastMsg:
		m.toast.startLeaving(msg) // 静止明け: 退場アニメへ (世代一致時のみ)。maybeTick で tick 再開
		return m, m.maybeTick()
	case usageRefreshMsg:
		// バックグラウンドで /usage を再取得し、次回リフレッシュを予約する。取得中も snap は
		// 消さないので loading() は false のままスピナーに落ちず、表示は last-good を維持する
		// (handle の不変条件)。
		//
		// 表示中だけ取得する。以前は「隠れていても最新値を用意しておく」ため表示/非表示に
		// 依らず回していたが、その判断は「/usage は ~440ms でゼロコスト」という誤った前提の
		// 上にあった (実測 2.0s wall / 1.8s CPU。440ms は JSON の duration_ms = 内部処理時間)。
		// オーバーレイは起動時グランスの後どのナビゲーションキーでも dismiss されるので、
		// 実際にはほぼ常に非表示 = 見えないもののために 60 秒ごと永続に node を起こしていた。
		// 「即座に最新が見える」という元の意図は再表示 (U) 時の stale 判定で保つ。
		if !m.wantsUsageRefresh() {
			return m, usageRefreshTick() // チェーンは維持 (再表示後に周期取得が復活する)
		}
		return m, tea.Batch(m.usageOv.fetchCmd(false), usageRefreshTick())
	case gitLogDirsMsg:
		return m, m.handleGitLogDirs(msg)
	case gitLogProbeMsg:
		return m, m.handleGitLogProbe(msg)
	case gitLogFPMsg:
		return m, m.handleGitLogFP(msg)
	case gitLogReloadMsg:
		return m, m.handleGitLogReload(msg)
	case ciPollMsg:
		if msg.gen != m.ciPollGen {
			return m, nil // reloadAfterPull で世代が進んだ後の残タイマーは破棄
		}
		targets := m.ciPollTargets()
		if len(targets) == 0 {
			// 🚨 **ここで awaitCI を空にする** (issue 223)。ciPollTargets は commits を走査して
			// `m.awaitCI[c.SHA]` を targets に入れるので、**targets が空なら残っている awaitCI の
			// 要素は定義上 commits の外**にいる = どの経路でも取り除かれないファントム。
			// ⚠️ 打ち切り (awaitAttempts) を早期 return の前へ動かしても効かない: この分岐は
			// ciPolling=false でチェーンごと止めるため、1 回数えても次の周期が来ない。
			m.awaitCI, m.awaitAttempts = nil, 0
			m.ciPolling = false // 追従対象なし: チェーンを止める (次の開始点で再アーム)
			return m, nil
		}
		if m.panelGrace > 0 {
			m.panelGrace-- // rerun 直後: 新しい実行が GraphQL に映るまで空振りを許す
		}
		// 「CI が 1 つも現れない」の打ち切りは周期の側で数える。結果着弾の側で数えると、
		// 一括取得が複数チャンクへ割れた回に awaitAttempts が余分に進み、意図した
		// ciPollInterval × ciAwaitMaxAttempts (= 2 分) より早く諦めてしまう
		if len(m.awaitCI) > 0 {
			m.awaitAttempts++
			if m.awaitAttempts >= ciAwaitMaxAttempts {
				m.awaitCI = nil // 諦める: workflow を持たない repo で永久にポーリングしない
			}
		}
		next := m.scheduleCIPoll()
		// 別経路の取得と重ねない (同一 SHA への GraphQL 並行は完了順で statuses/details が
		// 上書きされる。fetchPanelDetails と同じ注意)。タイマーだけ繋いで次の周期に回す
		if m.ciPollInFlight || m.fetching() {
			return m, next
		}
		m.ciPollInFlight = true
		gen := m.ciPollGen
		fetch := ciPollFetch(m.repo, targets, func(b CIBatch, e *GHError) tea.Msg {
			return ciPollResultMsg{gen: gen, targets: targets, batch: b, ghErr: e}
		})
		return m, tea.Batch(fetch, next, m.maybeTick())
	case ciPollResultMsg:
		// 🚨 in-flight は世代不一致でも必ず下ろす: リロード側は「飛んでいる poll の結果が
		// 着弾するまで in-flight を維持する」前提 (reloadAfterPull のコメント) なので、
		// ここで下ろさないと以降の周期が永久に fetch を見送る
		m.ciPollInFlight = false
		if msg.gen != m.ciPollGen {
			// リロード前に投げた結果。マージすると入れ替わった statuses を古い観測で
			// 巻き戻す (決着済みの SHA が pending に戻り、表示と追従が 1 周期ぶれる)
			return m, m.ensureCIPoll()
		}
		m.invalidateLines()
		m.ghErr = msg.ghErr // 成功時 (nil) はクリア: ciResultMsg と揃える (sticky 警告の防止)
		m.mergeCIBatch(msg.batch.Statuses, msg.batch.Details, msg.batch.PRs)
		m.settleAwaitCI()
		// リフレッシュで job 数が縮んだ場合にフォーカスを範囲内へ戻す
		if m.panelSHA != "" && m.panelCursor >= len(m.details[m.panelSHA]) {
			m.panelCursor = max(len(m.details[m.panelSHA])-1, -1)
		}
		return m, tea.Batch(m.maybeFetchETABasis(), m.ensureCIPoll())
	case pullMsg:
		m.actModal.pulling = false
		if msg.err != nil {
			m.showWarning("pull に失敗: " + firstLine(msg.err.Error()))
			return m, m.maybeTick()
		}
		// 成功トーストを右下にせり上げつつ全面リロード (アニメで画面が動いてもトーストは数秒残る)。
		m.toast.show("pull --rebase しました", true)
		// pull で自分のソースが更新されたなら、その場で裏ビルドを始める (autobuildAfterPull)。
		// status viewer を開いたまま p で pull した場合は、その場で読み直す: 自動更新は 1.5 秒
		// 周期なので、待たせるとヘッダーの ahead/behind が古いまま残って「効いていない」に見える。
		return m, tea.Batch(m.reloadAfterPull(), m.maybeTick(), m.autobuildAfterPull(), m.statusOv.loadCmd())
	case rerunMsg:
		m.actModal.rerunning = false
		if msg.err != nil {
			m.showWarning("再実行に失敗: " + firstLine(msg.err.Error()))
			return m, m.maybeTick()
		}
		m.toast.show("CI を再実行します", true)
		// パネルを開いたままなら猶予つきで追従対象に留める (rerun が GraphQL に映るまでのラグは
		// rerunPollGrace のコメント参照)。映れば StatePending になり、以降は通常の追従へ
		// 自然に引き継がれる。パネルが閉じられていれば何もしない (次の開き直しで最新を取る)
		var poll tea.Cmd
		if msg.sha == m.panelSHA && m.panelSHA != "" {
			m.panelGrace = rerunPollGrace
			poll = m.ensureCIPoll() // 既にチェーンが生きていれば nil (二重化しない)
		}
		return m, tea.Batch(poll, m.maybeTick())
	case updateBeginMsg:
		// C / X 連打で「同じ CLI」の update が走行中なら二重実行しない (自己更新が競合する)。
		// 🚨 別の CLI は弾かない: claude と codex は独立に走らせる (ユーザー要望 2026-08-21)。
		// ここを anyUpdating() に戻すと片方の実行中にもう片方が始められず直列に戻る。
		if m.actModal.isUpdating(msg.target) {
			return m, m.maybeTick()
		}
		// 🚨 判定 Cmd の走行中 (実測 40-80ms) は update モーダルが出ていないため、その窓で
		// b / u / r を押すと確認モーダルが立つ。そこへ update を重ねると、描かれるのは update
		// (boxLines の switch 順) なのにキーを受け取るのは確認 (handleKey の判定順) になり、
		// 「完了まで終了できません」の画面で Enter が git push を起動する (audit 2026-08-20 で
		// runGitPush 1 回を実測)。確認・git 実行中は update を譲り、理由をトーストで伝える。
		// 🚨 ここに anyUpdating() を足さないこと: 別 CLI の update とは並走させる (issue 074 の主旨)。
		// 本来の直し方は「描画とキー判定を同一の状態値から導出する」(issue 071 に残置)。
		if m.actModal.pushConfirm || m.actModal.pullConfirm || m.actModal.rerunConfirm ||
			m.actModal.pushing || m.actModal.pulling || m.actModal.rerunning {
			m.toast.show(msg.target+" update は確認/実行が終わってから実行してください", true)
			return m, m.maybeTick()
		}
		return m, tea.Batch(m.actModal.runUpdate(msg.target), m.maybeTick())
	case updateMsg:
		// 🚨 早期リターン (「すでに latest」の判定結果) で走行中の update を降ろさないこと。
		// C の判定 Cmd が並走したとき、その結果が走っている自己更新の追跡を消し、モーダルが
		// 閉じて終了ガードが解ける (自己更新が孤児化 / 二重起動する。red team 2026-08-21 が
		// npm 2 本同時と Ctrl-C 脱出を実測)。走行中なら判定結果は捨てる。
		if msg.early && m.actModal.isUpdating(msg.target) {
			return m, m.maybeTick()
		}
		// 該当 CLI だけ走行中から外す。もう片方が走っていればモーダルは残る
		// (両方終わるまで「完了まで終了できません」を維持する)
		m.actModal.finishUpdate(msg.target)
		// 結果は右下トーストで出す (旧: 何かキーで閉じるダイアログ。ユーザー要望 2026-07-25)。
		// バージョンが上がったのか latest だったのかは 1 行に畳んで一目で分かる形にする。
		// 「新バージョンあり」の通知 (showClaudeUpdate) と違って調停は挟まない:
		// こちらは C / X を押した本人への結果なので、先行トーストを上書きする後勝ちが正しい。
		// どちらの CLI の結果かが一目で分かるよう、主語として CLI 名を常に前置する
		// (ユーザー要望 2026-08-12。旧: codex のみ前置)。
		name := msg.target + " "
		switch {
		case msg.err != nil:
			m.showWarning(name + "更新に失敗: " + firstLine(msg.err.Error()))
		case msg.before != "" && msg.after != "" && msg.before != msg.after:
			m.toast.show(name+"v"+msg.before+" → v"+msg.after+" に更新しました", true)
		case msg.before != "" && msg.before == msg.after && msg.early:
			// glogx 自身が「installed >= キャッシュ済み latest」を証明した早期リターン。
			// ここだけが「最新です」と言ってよい (update は走っていない)。
			m.toast.show(name+"すでに最新版です (v"+msg.before+")", true)
		case msg.before != "" && msg.before == msg.after:
			// update を走らせたのにバージョンが動かなかった。🚨 これを「最新です」と言わないこと:
			// CLI は自前の stale なキャッシュを見て「更新不要」と判断しても exit 0 で成功する
			// (実測 2026-09-03: 起動時トーストが codex の新版を告げた直後に X を押しても
			// ~/.codex/version.json が 44 日前で止まっていて "already up to date" が返り、
			// glogx がそれを「すでに最新版です」と翻訳していた)。glogx は registry 直取りの
			// latest を握っているので、それより古いままなら警告として出す。
			if latest := cachedLatestVersion(versionCacheFileFor(msg.target)); versionLess(msg.before, latest) {
				m.showWarning(name + "update を実行しましたが v" + msg.before + " のままです (公開は v" + latest + ")" + updateNoteSuffix(msg.note))
				return m, m.maybeTick()
			}
			m.toast.show(name+"update を実行しましたが変化なし (v"+msg.before+")"+updateNoteSuffix(msg.note), true)
		case msg.after != "":
			m.toast.show(name+"現在のバージョン: v"+msg.after, true) // before 不明で比較できず
		default:
			m.toast.show(name+"update を実行しました", true) // 前後とも取得できず
		}
		return m, m.maybeTick()
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
		// status viewer 表示中は演出を出さない (対象のコミット一覧が画面に無く、演出分だけ
		// 再取得が遅れるだけ)。代わりに viewer を読み直してヘッダーの ahead を即消す
		// (pull 成功時の statusOv.loadCmd と同じ理由)
		if m.statusOv.visible() {
			return m, tea.Batch(m.refetchAfterPush(), m.maybeTick(), m.statusOv.loadCmd())
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
		// エディタを閉じて復帰。job ログは stdin 渡しなのでファイルは残らず、バッファも破棄済み。
		// 🚨 issues viewer の e (別名 v) だけは実ファイルを編集可能で開く (メモを足せるように
		// readonly にしていない) ので、復帰の境界で取り直す。取り直さないと編集結果 (H1・front matter の
		// status・チェックボックス) が一覧にも本文にも出ず、viewer が古い内容を最新として表示する。
		// 🚨 viewer 表示中でも issuesView.notice へ回さない: notice はどのヘッダーも描かず、
		// 次の打鍵で takeNotice されるまで画面に出ない (キーを押すまで失敗が黙殺される)。
		// viewLines が viewer の窓にもトーストを合成するので、ここは常にトーストでよい。
		// 🚨 起動対象は $VISUAL/$EDITOR で変わる (editorCommand) ので、文言でツール名を名指し
		// しない。job ログ・repo root だけは nvim 固定だが、失敗の主因は可変側 (typo した
		// $EDITOR・PATH に無いエディタ) なので総称で出す。
		//
		// 🚨 「起動できなかった」と「起動できたが 0 以外で終了した」を分ける。後者 (nvim の :cq 等)
		// はエディタが実際に開いてファイルを保存できているので、reload を飛ばすと上の不変条件
		// (復帰の境界で取り直す) が破れ、保存済みの編集が出ないまま古い内容を最新として表示する。
		var exitErr *exec.ExitError
		switch {
		case msg.err == nil:
		case errors.As(msg.err, &exitErr):
			m.showWarning("エディタが異常終了しました: " + firstLine(msg.err.Error()))
		default:
			// 起動失敗はファイルが変わっていないので取り直さない
			m.showWarning("エディタを開けませんでした: " + firstLine(msg.err.Error()))
			return m, m.maybeTick()
		}
		return m, tea.Batch(m.issuesOv.reloadAfterEdit(), m.maybeTick())
	// KeyMsg (v2 では KeyPressMsg/KeyReleaseMsg を束ねる interface) ではなく押下だけを取る。
	// 離鍵イベントは KeyboardEnhancements を要求していないので届かないが、interface で受けると
	// 将来 enhancement を有効にした瞬間に 1 打が 2 回処理される。
	case tea.KeyPressMsg:
		// 高速連打やパイプ入力で複数の文字キーが 1 つのキーイベント (Text 長 > 1) に
		// まとまって届いた場合の分解。まとめずに msg.String() だけ見ると "hhq" のような
		// 未知キー扱いになり、以降の操作が全て無視されたように見える (pty スモークで実測)。
		// v2 の入力デコーダは grapheme クラスタ単位で 1 イベントを返すのでまとまらない想定だが、
		// 保険として残す (v1 で実際に起きた回帰であり、削っても得られる簡素化は 10 行)。
		if runes := []rune(msg.Text); len(runes) > 1 {
			var cmds []tea.Cmd
			for _, r := range runes {
				_, cmd := m.handleKey(string(r))
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
				if m.done {
					break
				}
			}
			// 🚨 単キー経路と同じく maybeTick を束ねる: 分解したキーが出したトースト・glide は
			// 他に tick を回す理由が無ければ 1 フレームも進まず、shown=0 のまま凍って見えない。
			// この経路は「普段は通らないが通ったときだけ壊れる」ので気づきにくい (issue 032)。
			// maybeTick は single-flight なのでループ内の Cmd と重ねても二重には走らない。
			return m, tea.Batch(append(cmds, m.maybeTick())...)
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

// updateKeyReachable は C / X を update の入口として受けてよいかを返す。overlay が自分の語彙を
// 持っているとき (= ownsKeys) はそちらを優先する。今その状態を持つのは 3 つ:
// issues の絞り込み・URL ピッカー・目印確認 / status の pager・破棄確認 / **doctor の削除の確認と
// 実行中** (issue 148 ④)。status viewer では X が「変更を捨てる」(docs/status-viewer-spec.md) なので渡す。
// それ以外 (ratelimit / diff / PR status / job パネル) は入力モードも C / X の割り当ても
// 持たないので常に受ける = どの画面からでも update を始められる (README のキー表が正本)。
// 🚨 overlay に新しい入力モード (y/N 確認など) や C / X の割り当てを足すときはここも直す。
// overlay 側は「未知キーは無視」なので、忘れても build もテストも壊れず、update が確認中のキーを
// 先取りする形で静かに壊れる (doctor に削除確認を足す予定 = issue 148 ④ が最初の該当)。
func (m *browseModel) updateKeyReachable(key string) bool {
	if m.issuesOv.visible() && m.issuesOv.ownsKeys() {
		return false
	}
	// doctor の削除は y/N の確認と実行中のブロックを持つ (issue 148 ④)。ここで譲らないと
	// 確認中の X が codex update を始め、削除の確認が裏に残る
	if m.doctorOv.visible() && m.doctorOv.ownsKeys() {
		return false
	}
	if m.statusOv.visible() && (m.statusOv.ownsKeys() || key == "X") {
		return false
	}
	return true
}

func (m *browseModel) handleKey(key string) (tea.Model, tea.Cmd) {
	if m.swallowKeyRepeat(key) {
		return m, nil
	}
	// C-g は即終了: tmux の C-g popup (bind -n C-g) をトグル風に開閉するため
	// (開くキーと同じキーで閉じる)。本家 glog には無い割当。
	// 閉じる演出の途中に来たキーは即着地させる (q を押してから消えるまでの間、入力が
	// 効かない時間を作らない)
	if m.zoom.closing() {
		m.zoom.finish()
		m.done = true
		return m, tea.Quit
	}
	// issues viewer の閉じる演出中に来たキーも即着地させる。🚨 ただしキーは飲み込まず、
	// 畳んだあとの状態で通常どおり処理する: 飲むと「q で閉じた直後の q が効かない」時間が
	// できる (アプリの終了演出はキーを飲んでも終わるだけなので失うものが無いが、こちらは違う)。
	// i ならこのあと下の分岐で開き直しになる = 閉じる演出の途中で開き直せる。
	//
	// 🚨 viewer へ routing する分岐 (下の issuesOv.visible()) より前に置くこと。後ろに置くと
	// 演出中のキーが viewer 側で処理され、モードを持つキー (/ の絞り込み・n の確認) が
	// 「畳んだ後の view」に状態を残す = 次に i で開いた瞬間に蘇る。
	//
	// 🚨 例外は e (エディタ) の 1 キーだけ。ここを素通ると、板がまだ見えているのに git log 一覧側の
	// e (openEditorAtRoot = `nvim .`) が全画面で起動し、「見ている issue を開いたつもりが repo root」
	// になる。上の「飲むと q が効かない窓ができる」は q/Esc の応答性の話で、e を 1 キーだけ捨てても
	// q は素通しのまま成立するので、両立させる (issues viewer の e が案内キーになった 2026-08-13
	// 以降、踏む確率が上がった)。
	closedIssues := m.issuesOv.finishClose()
	closedStatus := m.statusOv.finishClose() // status viewer も同じ契約 (閉じ演出中のキーは viewer に届かない)
	if (closedIssues || closedStatus) && key == "e" {
		return m, m.maybeTick()
	}
	if key == "ctrl+c" || key == "ctrl+g" {
		switch {
		// 🚨 push / pull と共存したときは下の 2 段ガードへ落とす。ここで常時ブロックすると
		// stall した push の唯一の脱出口 (Ctrl-C 2 回) が消える (現状は updateBeginMsg の
		// 譲りで共存しないが、if 1 つに依存させない)。
		case m.actModal.anyUpdating() && !m.actModal.pushing && !m.actModal.pulling:
			// 自己バイナリ更新の中断は CLI を壊しうるので常にブロック (ユーザー選定 2026-07-22)。
			// 並走中はどちらか 1 つでも走っていればブロックする (片方だけ生き残った状態で
			// 終了すると、残った自己更新が孤児のまま進む)。
			// escape は updateTimeout のみ。モーダルに「完了まで終了できません」を出す。
			return m, nil
		case m.doctorOv.visible() && m.doctorOv.del.blocking():
			// doctor の削除も push / pull と同じ 2 段ガード。🚨 ここに case が無いと
			// **1 回目の Ctrl-C でプロセスごと落ちる**。それは削除の中断を ctx で伝える経路を
			// 使わないので、記録が executing のまま残り、cli: の子プロセスも孤児化する
			// (doctor_delete.go 冒頭の不変条件を、UI の配線が破る形。敵対レビュー 2026-09-03 が実測)。
			// 中断の意味づけ (1 回目は武装 / 2 回目で cancel) は doctorView が持つので、
			// 判定を 2 箇所に書かず handleDeleteKey へ渡す。**どちらの回でもここで飲む**
			// (2 回目の cancel は相を落とさない = 相を落とすのは receiveDelete なので、
			// 回数で戻り値を分けても同じ値になる)
			m.doctorOv.handleDeleteKey("ctrl+c")
			return m, m.maybeTick()
		case m.actModal.pushing || m.actModal.pulling:
			// 途中終了は不整合 (特に pull --rebase の mid-rebase 状態) を招くので 1 回目はブロック。
			// ただし stall で永久に閉じられなくならないよう、2 回目の Ctrl-C で強制終了する
			// (quit() の actModal.stop() が走行中 git を cancel。ユーザー選定 2026-07-23)。
			if m.actModal.forceQuitArmed {
				return m.quit()
			}
			m.actModal.forceQuitArmed = true
			return m, nil
		case key == "ctrl+c":
			return m.quitNow() // 緊急脱出は演出なしで最短に抜ける
		default:
			return m.quit()
		}
	}
	// git push/pull/update の確認と実行中ガードは action モーダルが捌く (通知はトースト)
	// (警告/結果の dismiss・確認 y/N・実行中のキー無視。判定順は actionModal.handleKey 側)。
	// 実行を伴う確認 y は action (実行 tea.Cmd) を載せて返すので maybeTick と束ねる。
	if consumed, action := m.actModal.handleKey(key); consumed {
		if action != nil {
			return m, tea.Batch(action, m.maybeTick())
		}
		return m, nil
	}
	// tmux prefix の誤爆フィードバック: popup 表示中は tmux がキーを処理しない
	// (display-popup はモーダル) ため、window 移動しようとした prefix はここへ素通りしてくる。
	// 無言だと「効かない」だけで理由が分からないので、prefix キー自体を飲んで案内を出す。
	//
	// 🚨 飲むのは prefix キー 1 つだけ。続くキーは通常の操作として処理する: prefix は
	// 押し間違いで押されるので、打ち直した入力まで失敗扱いにしない (ユーザー判断)。
	// 代償として popup 内の prefix+p は PR オープンを発火する。破壊的な b/u は y/N 確認が
	// 挟まるので許容した = 確認なし即実行のキーを増やすとこの判断が崩れる。
	//
	// 確認モーダル (y/N) 中はここへ来ない (上のモーダル処理が先): モーダル内では
	// 「y 以外の任意キー = キャンセル」というモーダルの語彙を優先する。
	// 対象キーはハードコードせず起動時に tmux サーバへ聞いた現在値 (prefixMsg)。
	// tmux 外や取得失敗では tmuxPrefix="" のままこの機能ごと無効になる。
	if m.tmuxPrefix != "" && key == m.tmuxPrefix {
		// 通知は右下トースト (中央ダイアログは操作を遮って重い)
		m.toast.show("tmux prefix は popup では効きません (C-g で閉じてから)", false)
		return m, m.maybeTick()
	}
	// emacs 流の水平移動エイリアス (C-n/C-p = ↓/↑ は各ビューで対応済み)。ここで
	// 正規化するので全ビュー (一覧/パネル/詳細/diff) に一括で効く。
	// 🚨 本家 glog と異なり C-b は ← の別名ではない (push を C-b → b に変えた名残で未割当)
	if key == "ctrl+f" {
		key = "right"
	}
	key = normalizeSpaceKey(key)
	// バックグラウンドビルドの完成ダイアログ。r で確認なしに再起動する (ユーザー要望 2026-08-01)。
	//
	// 🚨 出ている間はキーを 1 つ飲んで必ず閉じる (r 以外は「今はしない」)。素通しにすると、
	// r が issues viewer の再読込・job パネルの再実行にも割り当たっているので「再読込のつもりが
	// 再起動」になる。🚨 viewer の分岐より前に置く: viewer は全画面モーダルで全キーを飲むため、
	// 後ろに置くと viewer を開いている間ダイアログに答えられない。
	//
	// 🚨 描画と同じ restartPromptVisible() で判定する (フラグを直接見ない)。出ていないダイアログに
	// キーを吸わせない・出ているのに届かない、の両方をこの一致で防ぐ。
	//
	// 🚨 ここに限っては restartPending を直接見ても今は同じ結果になる: 上の actModal.handleKey が
	// ownsKeys() のとき必ず consumed で return するので、この行に来た時点で ownsKeys() は false =
	// 2 つの述語は構造的に等価。つまりこの選択を守るテストは書けない (変異させても green)。
	// それでも述語を揃えておくのは、その等価性が「actModal を先に捌く」という並び順に依存して
	// いて、並び替えた瞬間に黙って壊れるため。テストで守られていない箇所だと分かるように明記する。
	if m.restartPromptVisible() {
		m.restartPending = false // 答えた = 保留を解除する (次のビルド完成まで出ない)
		if key == "r" {
			return m.restartForNewBinary()
		}
		return m, nil
	}
	// doctor 表示中も全画面: キーはここで飲み切る (rlDash と同じ位置・同じ理由)。
	// C = claude update / X = codex update (確認なし即実行。ユーザー選定 2026-07-22)。U=usage と並ぶ
	// 大文字の「Claude Code メタ操作」で、実行中は spinner モーダルを出す (描画は finishWithGlobalChrome
	// がどの画面にも重ねる)。🚨 overlay の分岐より前に置くこと: 後ろだと各 overlay がキーを飲み、
	// git log 一覧へ戻らないと update を始められない (ユーザー要望 2026-09-02: doctor / ratelimit /
	// issues / status の上からも開始したい)。overlay 自身の語彙が先に立つ場面だけ updateKeyReachable
	// が譲る (入力・確認モード中、status viewer の X = 変更を捨てる)。
	if target, ok := updateKeyTarget(key); ok && m.updateKeyReachable(key) {
		m.usageOv.dismiss() // 一覧の他のキーと同じ語彙 (U で出し、次のキーで消える)
		return m, tea.Batch(m.actModal.startUpdateFor(target), m.maybeTick())
	}
	// 全画面ビューア (doctor / 残量ダッシュボード / issues / status) が出ている間は、キーを
	// そのビューアが飲み切る。🚨 **どれが出ているかの出典は activeFullScreen 1 箇所**
	// (issue 227)。ここで個別に visible() を並べ直さない。
	//
	// 🚨 この dispatch の位置は動かさないこと。前 (actModal の確認・実行中ガード / tmux prefix /
	// 再起動ダイアログ / C・X の update) へ動かすと viewer の語彙が外側に奪われ、後ろ (裸の
	// b / u = push / pull、U / R / D) へ動かすと viewer を見ている最中の u が
	// git pull --rebase の確認を開く footgun になる。
	switch m.activeFullScreen() {
	case fullScreenDoctor:
		return m.routeKeyToDoctor(key)
	case fullScreenRatelimit:
		return m.routeKeyToRatelimitDash(key)
	case fullScreenIssues:
		return m.routeKeyToIssues(key)
	case fullScreenStatus:
		return m.routeKeyToStatus(key)
	case fullScreenNone, fullScreenCount:
		// 一覧を見ている (fullScreenCount は番兵で activeFullScreen は返さない)。下へ落とす
	}
	// usage オーバーレイのトグル / dismiss。モーダル (push/pull 確認)・prefix・
	// 実行中ガードを素通りしないよう必ずそれらの後に置く: 先頭に置くと U が push 確認を
	// キャンセルし損ねて残った確認へ Enter で誤 push する footgun になる (レビュー指摘 2026-07-21)。
	// U は明示トグル (取得中なら spinner を回し直す)。それ以外のナビゲーションキーは「起動時
	// グランス」を引っ込める副作用だけ持たせ、キー本来の動作は下の dispatch/switch で続行する。
	if key == "U" {
		return m, m.toggleUsage()
	}
	// R = 全画面 ratelimit ダッシュボード。U と同じ判定位置に置く (モーダル・prefix・実行中
	// ガードの後) — 先頭に置くと push/pull 確認を素通りする footgun が同じ形で起きる。
	if key == "R" {
		return m, m.toggleRatelimitDash()
	}
	// D = 全画面 doctor (issue 148)。R と同じ判定位置 (モーダル・prefix・実行中ガードの後)。
	// 開いた時にスキャンが始まる (起動時には走査しない)。
	if key == "D" {
		m.usageOv.dismiss()
		return m, tea.Batch(m.doctorOv.toggle(), m.maybeTick())
	}
	m.usageOv.dismiss()
	// diff ポップアップ表示中はスクロール/閉じる操作だけを受ける (最前面のモーダル)
	if m.diffOv.visible() {
		return m.handleDiffKey(key)
	}
	// PR 状態ポップアップ表示中も同様にモーダル (o/y/閉じるだけを受ける)
	if m.prStatusOv.visible() {
		return m.handlePRStatusKey(key)
	}
	// i = issues viewer を開く (全画面。読む規約は docs/issues-viewer-spec.md)。一覧表示中
	// だけ受ける: job パネル/詳細を開いているときはそちらの語彙を優先する。初回だけ非同期で
	// スキャンし、以降は結果を保持したまま開閉する (再スキャンは viewer 内の r)。
	if key == "i" && m.panelSHA == "" {
		return m, tea.Batch(m.issuesOv.toggle(currentDir()), m.maybeTick())
	}
	// s = status viewer を開く (全画面。規約は docs/status-viewer-spec.md)。i と同じく一覧表示中
	// だけ受ける: job パネル/詳細を開いているときはそちらの語彙を優先する。
	if key == "s" && m.panelSHA == "" {
		return m, tea.Batch(m.statusOv.toggle(), m.maybeTick())
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
		m.copyWithToast(warn, "警告をコピーしました")
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
		m.glide.stop() // 端へのジャンプは即時 (距離が不定で glide の意味が薄い)
	case "G", "end":
		m.cursor = clampIdx(len(m.commits)-1, len(m.commits))
		m.ensureCursorVisible()
		m.glide.stop()
	// 半ページ移動も glide に載せる (ユーザー要望 2026-07-31)。カーソルは動かさずビューポート
	// だけが動くので、prev からの距離がそのまま glide の距離になる。
	case "ctrl+d", "pgdown":
		prev := m.offset
		m.offset = m.clampOffset(m.offset + m.pageSize()/2)
		return m, m.startScrollAnim(prev)
	case "ctrl+u", "pgup", "shift+space":
		prev := m.offset
		m.offset = m.clampOffset(m.offset - m.pageSize()/2)
		return m, m.startScrollAnim(prev)
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
	case "e":
		return m, m.openEditorAtRoot()
	case "E":
		return m, m.openFilerAtRoot()
	}
	return m, nil
}

// routeKeyToDoctor は doctor 表示中のキー処理。全画面なのでキーはここで飲み切る
// (裏の一覧をスクロールさせない)。呼ぶのは handleKey の activeFullScreen dispatch 1 箇所で、
// **dispatch の位置とその理由はそちらに書く** (ここへ再掲しない)。
func (m *browseModel) routeKeyToDoctor(key string) (tea.Model, tea.Cmd) {
	// 描画中に選択行が消えて寄せていたら、**キーを飲まずに**知らせる (issue 210)。
	// カーソルの寄せは View から起きるので、通知経路はここしか無い
	if t := m.doctorOv.takeCursorFellBack(); t != "" {
		m.toast.show(t, false)
	}
	// U (usage) は doctor の上でも効く (issues / status と同じ契約。issue 227 ③)。usage の箱は
	// finishWithGlobalChrome がどの画面にも重ねるので「取得だけ走って画面に出ない」は起きない。
	// 🚨 doctor が自分でキーを解釈し切る状態 (削除の y/N 確認・実行中) では横取りごと飛ばして
	// 委譲へ落とす: 飛ばさないと確認中の U が箱を出し、確認は裏に残ったままになる
	// (issues の URL ピッカーで実際に起きた形 = issue 113)。
	if !m.doctorOv.ownsKeys() {
		if key == "U" {
			return m, m.toggleUsage()
		}
		m.usageOv.dismiss() // 他のキーで引っ込むのは一覧と同じ語彙 (U で出し、次のキーで消える)
	}
	switch m.doctorOv.handleKey(key, m.pageSize()) {
	case doctorClosed:
		return m, m.maybeTick()
	case doctorRescan:
		// 再スキャンの理由がある場合は伝える (「前回の結果だったので取り直します」等)。
		// 🚨 黙って走らせない: ユーザーは Space / d を押したのであって r を押していない
		if t := m.doctorOv.takeToast(); t != "" {
			m.toast.show(t, false)
		}
		// close() を経由しない: 数件だけの partial を書いて完全な結果を潰さない (doctorView.saveCache の注記)
		return m, tea.Batch(m.doctorOv.rescan(), m.maybeTick())
	case doctorCopyPath:
		m.copyWithToast(m.doctorOv.copyPayload(), "パスをコピーしました")
	case doctorCopyText:
		m.copyWithToast(m.doctorOv.copyPayload(), "解説をコピーしました (LLM にそのまま貼れます)")
	case doctorCopyLog:
		m.copyWithToast(m.doctorOv.copyPayload(), "実行の記録をコピーしました (LLM にそのまま貼れます)")
	case doctorNothing:
		m.toast.show("この行にはコピーするものがありません", false)
	case doctorToast:
		m.toast.show(m.doctorOv.takeToast(), false)
	case doctorRunDelete:
		// 削除 (と、その下見) は doctorView が組んだ Cmd をそのまま走らせる
		return m, tea.Batch(m.doctorOv.takeDeleteCmd(), m.maybeTick())
	case doctorSwallow:
	}
	return m, m.maybeTick()
}

// routeKeyToRatelimitDash は残量ダッシュボード表示中のキー処理 (doctor と同じく全画面)。
//
// 🚨 U (usage) は**意図的に受けない**。ダッシュボードは同じ Snapshot を全画面で描いたもので、
// 右上に小さい方を重ねると同じ値が 2 か所に出る (toggleRatelimitDash が開くときに
// usageOv.dismiss() しているのと同じ判断)。issues / status が U を受けるのとはここが違う。
func (m *browseModel) routeKeyToRatelimitDash(key string) (tea.Model, tea.Cmd) {
	switch m.rlDash.handleKey(key) {
	case rlDashClosed:
		return m, m.maybeTick()
	case rlDashRefresh:
		return m, tea.Batch(m.usageOv.fetchCmd(false), m.maybeTick())
	// i / s = viewer へ横断 (viewer 側の R と対。ユーザー要望 2026-09-01)。handleKey が
	// 既に閉じているので、issues ↔ status の横断と同じく閉じ演出は待たずに即着地する。
	// 🚨 toggle を呼ぶので、ここへ来る時点で相手の viewer が開いていないこと (全画面は
	// 同時に 1 枚) が前提。開いていると toggle が「開く」でなく「閉じる」に化ける。起動時
	// 復元との競合はその前提を守るために issuesRestoreMsg 側で弾いている。
	//
	// 🚨 一覧の i/s が持つ `m.panelSHA == ""` ガードは、ここでは**意図的に付けない**
	// (敵対レビュー指摘 2026-09-01)。あのガードの理由は「job パネルを開いている間はそちらの
	// キー語彙を優先する」= キーの取り合いの解消で、ダッシュボードは全画面で全キーを飲む
	// ため取り合いが起きない。パネル (panelSHA / panelCursor / details) は viewer とは独立
	// した状態で、閉じれば元のパネルがそのまま描き直される。🚨 逆にガードを付けると、
	// パネルを開いたまま R → i が無音の no-op になる (issue 122 が禁じた形)。
	// パネル側の状態を viewer が読むようになったら、この判断を再評価すること。
	case rlDashIssues:
		return m, tea.Batch(m.issuesOv.toggle(currentDir()), m.maybeTick())
	case rlDashStatus:
		return m, tea.Batch(m.statusOv.toggle(), m.maybeTick())
	case rlDashSwallow:
		// 閉じる / 更新 / 横断以外は握り潰す (下の return へ落ちる)
	}
	return m, nil
}

// routeKeyToIssues は issues viewer 表示中のキー処理 (全画面なのでキーは全部 viewer が飲む)。
//
// U (usage) はここで受ける: 以前は「viewer が全画面で描かれないのに取得だけ走る」ため
// 弾いていたが、viewLines が viewer の窓へ usage を合成するようになったので開けてよい。
func (m *browseModel) routeKeyToIssues(key string) (tea.Model, tea.Cmd) {
	// U は viewer の上でも効く (ユーザー要望 2026-08-01)。viewLines が usage を viewer の窓へ
	// 合成するので「取得だけ走って画面に出ない」問題は起きない (トーストと同じ経路)。
	//
	// 🚨 viewer が自分でキーを解釈し切る状態 (URL ピッカー / 番号入力 / y/N 確認) では、この
	// 横取りを飛ばして下の委譲へ落とす。飛ばさないと URL ピッカーが宣言している
	// 「印字文字はすべて検索語に流す」が大文字 U だけ破れる (issue 113)。
	// 🚨 ガードは横取りだけに掛けること (この if 自体に付けると委譲も飛ぶ)。
	if !m.issuesOv.ownsKeys() {
		if key == "U" {
			return m, m.toggleUsage()
		}
		m.usageOv.dismiss() // 他のキーで引っ込むのは一覧と同じ語彙 (U で出し、次のキーで消える)
	}
	cmd := m.issuesOv.handleKey(key, m.issuesOpts().viewport())
	// viewer の操作結果 (コピー・URL 起動・読み込み失敗) は glogx 共通の右下トーストで出す
	// (ユーザー要望 2026-07-31)。viewer が全画面でトーストが隠れていた時代はヘッダー行に
	// 出していたが、下の viewLines でトーストを viewer の上にも合成するようにした。
	m.deliverNotice(m.issuesOv.takeNotice())
	// q/esc = glogx ごと終了 (ユーザー要望 2026-08-06: git log 一覧へは戻らない)。viewer を
	// 出したまま終了するので、次回起動は再開記憶で同じ画面から始まる (C-g と同じ経路)
	if m.issuesOv.takeWantQuit() {
		return m.quit()
	}
	// s = status viewer へ横断 (ユーザー要望 2026-08-06)。閉じる演出は待たず即着地させる:
	// 全画面 viewer は同時に 1 枚の前提で、閉じ演出と次の開き演出を重ねない
	if m.issuesOv.takeWantStatus() {
		m.issuesOv.finishClose()
		return m, tea.Batch(cmd, m.statusOv.toggle(), m.maybeTick())
	}
	// R = ratelimit ダッシュボードへ横断 (ユーザー要望 2026-09-01)。s と同じ経路で、
	// 閉じ演出を待たず即着地させる (全画面は同時に 1 枚)。取得の起こし方は一覧の R と
	// 同じ toggleRatelimitDash に委ねる (single-flight ガードを 1 か所に保つ)。
	if m.issuesOv.takeWantRatelimit() {
		m.issuesOv.finishClose()
		return m, tea.Batch(cmd, m.toggleRatelimitDash())
	}
	return m, tea.Batch(cmd, m.maybeTick())
}

// routeKeyToStatus は status viewer 表示中のキー処理 (issues と同じ形。全画面)。
// p / b / u の扱いだけが issues と違う (docs/status-viewer-spec.md 3 節)。
func (m *browseModel) routeKeyToStatus(key string) (tea.Model, tea.Cmd) {
	// 🚨 viewer が自分でキーを解釈し切る状態 (全画面 pager / 破棄確認) では、この横取りを
	// まるごと飛ばして下の委譲へ落とす。飛ばさないと viewer のキー語彙を外側が奪い、
	// `b` (半ページ戻り) が push 確認に化けて続く Enter で実 push が走る (実測 2026-08-21)。
	// 🚨 ガードは横取りだけに掛けること: この if 自体に付けると委譲も飛んで pager が
	// キーを受け取れなくなる (実装中に踏んだ)。
	if !m.statusOv.ownsKeys() {
		if key == "U" {
			return m, m.toggleUsage() // viewer の上でも usage は出せる (issues と同じ契約)
		}
		m.usageOv.dismiss()
		if key == "p" {
			// pull は viewer の中からも打てる (ユーザー要望 2026-08-05)。🚨 一覧の u とキーを
			// 分けているのは spec 3 節の判断を残すため: staging 中に「隣のキー」で remote 操作へ
			// 滑るのを防ぎつつ、明示的に p を押したときだけ通す。確認 (y/N) は actModal が出す。
			m.actModal.askPull()
			return m, m.maybeTick()
		}
		if key == "b" {
			// push も viewer の中から打てる (ユーザー要望 2026-08-07)。p (pull) と同じく
			// 確認 (y/N) は actModal が viewer の上に重ねて出す。b は一覧と同じキーで、
			// staging の語彙 (j/k/Space/a/X/d…) と衝突しない
			return m, m.confirmPush()
		}
		if key == "u" {
			// u は一覧の pull キーだが、ここでは p を使う (誤爆しやすい隣接キーで remote を
			// 叩かせない。spec 3 節)。黙って無視すると「押したのに何も起きない」になるので理由を返す
			m.toast.show("pull は p です (status viewer では u を使いません)", false)
			return m, m.maybeTick()
		}
	}
	cmd := m.statusOv.handleKey(key, m.statusOpts().viewport())
	m.deliverNotice(m.statusOv.takeNotice())
	// q/esc = glogx ごと終了 (issues 側と同じ契約。ユーザー要望 2026-08-06)
	if m.statusOv.takeWantQuit() {
		return m.quit()
	}
	// i = issues viewer へ横断 (ユーザー要望 2026-08-06)。issues 側の s と対 (即着地も同じ理由)
	if m.statusOv.takeWantIssues() {
		m.statusOv.finishClose()
		return m, tea.Batch(cmd, m.issuesOv.toggle(currentDir()), m.maybeTick())
	}
	// R = ratelimit ダッシュボードへ横断 (issues 側の R と同じ。ユーザー要望 2026-09-01)
	if m.statusOv.takeWantRatelimit() {
		m.statusOv.finishClose()
		return m, tea.Batch(cmd, m.toggleRatelimitDash())
	}
	return m, tea.Batch(cmd, m.maybeTick())
}

// confirmPush は push 確認 (y/N) に入る。未 push が 1 件も無ければ確認を出さない
// (誤爆防止と「push 済みなのに聞かれる」違和感の回避)。
func (m *browseModel) confirmPush() tea.Cmd {
	if m.unpushedCount() == 0 {
		// 「押しても何も起きない理由」の通知はキー待ちのモーダルでなく右下トーストで出す
		// (ユーザー要望 2026-07-25)。他の no-op 通知 (コピー対象なし・PR なし等) と同じ経路。
		m.toast.show("未 push のコミットはありません", false)
		return m.maybeTick()
	}
	m.actModal.pushConfirm = true
	return nil
}

// reloadAfterPull は pull --rebase 成功後の全面リロード (カーソルは新規コミットの先頭へ)。
func (m *browseModel) reloadAfterPull() tea.Cmd {
	_, cmd, _ := m.reloadLog(false, "pull 後の再読込に失敗しました: ")
	return cmd
}

// logData は git log の読み直しで取り直すもの一式 (Update の外で集め、applyLogData でモデルへ入れる)。
type logData struct {
	commits  []Commit
	raw      []string // 表示用 verbatim (oneline では nil)
	statuses map[string]CIState
	toFetch  []string
	repo     Repo
	hasRepo  bool
}

// loadLogData は git を fork して logData を集める純粋な読み込み (モデルを触らない)。
// git を 5-6 本 fork する (LoadCommits / LoadLogDisplay / planStatuses の rev-list・remote get-url・
// rev-parse) ので、Update の中では呼ばず Cmd に出す (reflectGitLogChange)。pull 後の全面リロード
// (reloadLog) は利用者の操作なので同期のまま。
func loadLogData(opts *Options, colored, oneline bool) (logData, error) {
	commits, err := LoadCommits(opts, colored)
	if err != nil {
		return logData{}, err
	}
	shas := make([]string, len(commits))
	for i, c := range commits {
		shas[i] = c.SHA
	}
	d := logData{commits: commits}
	d.statuses, d.toFetch, d.repo, d.hasRepo, _ = planStatuses(opts, shas)
	if !oneline {
		if raw, dispErr := LoadLogDisplay(opts, colored); dispErr == nil {
			d.raw = raw // 照合失敗は applyLogData 側で nil = 自前レンダリングへ
		}
	}
	return d, nil
}

// reloadLog は git log を読み直して派生キャッシュを畳む共通経路 (同期。pull 後の全面リロード用)。
// 読み込みと反映は loadLogData / applyLogData に分かれており、外部変更の追従 (gitlog_watch.go) は
// 読み込みだけを Cmd へ出して同じ applyLogData で反映する。
//
// 返り値の added は先頭へ増えた新規コミット数、ok=false は読み直しに失敗した = モデルを
// 一切触っていない (警告はここで出しているので、呼び出し側は追加の通知をしない)。
func (m *browseModel) reloadLog(keepView bool, failPrefix string) (added int, cmd tea.Cmd, ok bool) {
	d, err := loadLogData(m.opts, m.colored, m.oneline)
	if err != nil {
		m.showWarning(failPrefix + firstLine(err.Error()))
		return 0, nil, false
	}
	added, cmd = m.applyLogData(d, keepView)
	return added, cmd, true
}

// applyLogData は読み直した logData をモデルへ入れ、派生キャッシュを畳む。rebase でローカル SHA が
// 変わりうるため、コミット列・push 状態・CI・派生キャッシュ (details/PR/diff/job 詳細)
// をすべて取り直す (部分更新は旧 SHA の残骸が混ざる)。
//
// keepView == false なら先頭へ寄せて新規コミット行を上から降らせる (pull した本人は先頭を見る)。
// true なら**見えている画面を動かさない** (外部の変更を反映するとき、読んでいる行をずらさない
// = gitlog_watch.go)。錨は 2 段に取る:
//
//   - ビューポート先頭のコミット → その画面行を保つ (offset の復元)
//   - カーソルのコミット → カーソルの選択を保つ
//
// 🚨 カーソルだけを錨にしないこと: ctrl+d でページ送りした状態 (cursor == 0 のまま下を読む) では
// カーソルが指すのは**先頭コミット**で、`--amend` で最も消えやすい SHA になる。消えた瞬間に
// 「先頭へ倒す」経路へ落ちて読んでいた位置が飛ぶ (敵対レビューで実測 2026-09-01)。
// 両方消えていたら (rebase / reset) 先頭へ倒す。
//
// 錨は行集合が入れ替わる直前 (この関数の中) で測る。非同期の読み直しでは Cmd を出してから
// 結果が届くまでに利用者がスクロールしうるので、Cmd を出す時点で測った錨は使えない。
func (m *browseModel) applyLogData(d logData, keepView bool) (added int, cmd tea.Cmd) {
	commits := d.commits
	// 読み直し前の SHA 集合。先頭へ増えた新規コミット数を「既知 SHA に当たるまで」で数え、
	// アニメーションの対象行数を決める (rebase で SHA が書き換わった場合も破綻しない)。
	oldSHAs := make(map[string]struct{}, len(m.commits))
	for _, c := range m.commits {
		oldSHAs[c.SHA] = struct{}{}
	}
	cursorSHA, topSHA, topRow := "", "", 0
	if keepView {
		if m.cursor >= 0 && m.cursor < len(m.commits) {
			cursorSHA = m.commits[m.cursor].SHA
		}
		if idx := topVisibleCommitIdx(m.lines(), m.offset); idx >= 0 && idx < len(m.commits) {
			topSHA = m.commits[idx].SHA
			topRow = headerLineIndex(m.lines(), idx) - m.offset
		}
	}
	m.commits = commits
	m.statuses, m.toFetch, m.repo, m.hasRepo = d.statuses, d.toFetch, d.repo, d.hasRepo
	m.details = map[string][]CheckDetail{}
	m.detailsLoading = map[string]bool{}
	m.detailOv.reset() // job 詳細ログキャッシュも破棄 (旧 SHA の残骸を持ち越さない)
	m.prCache = map[string]*PRRef{}
	m.prBusy = map[string]bool{}
	m.prStatusOv.reset() // 旧 SHA の PR 詳細キャッシュも破棄
	m.diffOv.reset()
	m.closePanel()
	m.awaitCI, m.awaitAttempts = nil, 0
	m.ciPollGen++       // 旧世代の残タイマーを無効化する (リロードで対象そのものが入れ替わる)
	m.ciPolling = false // 次の ciResultMsg 着弾で張り直す
	// 🚨 ciPollInFlight はここで false に戻さない: pull で SHA 集合が入れ替わっても既存 SHA は
	// 残るので、飛んでいる旧 poll と新 poll が同一 SHA を並行取得し、完了順で古い結果が勝ちうる。
	// 旧 poll の結果が着弾した時点で false に戻る (数周期 fetch を見送るだけで追従は途切れない)。
	m.glide.stop() // リロードの演出はアニメ側が担うので一覧の glide は破棄
	m.verbatim = nil
	if !m.oneline && d.raw != nil {
		m.verbatim = VerbatimLines(d.raw, commits) // 照合失敗は nil = 自前レンダリングへ
	}
	m.invalidateLines()
	// 見張りの基準を作り直す (gitlog_watch.go)。自分で読み直した後の状態を「変化」として
	// もう一度反映しないため。空にすると次の測定が手元のコミット列と突き合わせて基準を作る。
	// 🚨 飛んでいる測定の札も降ろす: 読み直しの直前に測った古い指紋が届くと、新しいコミット列と
	// 突き合わせて必ず不一致になり、無駄な再読込・トースト・CI 再取得が続けて走る。
	// reloadSeq を進めるのは、飛んでいる非同期の読み直し (reflectGitLogChange) を捨てるため:
	// この後に届く古い logData を入れると、いま入れたものより古い状態へ戻る。
	m.logWatch.hasSeen, m.logWatch.measuring = false, false
	m.logWatch.reloadSeq++
	m.logWatch.reloading = false
	added = countNewCommits(commits, oldSHAs)
	newTop, newCursor := indexOfCommit(commits, topSHA), indexOfCommit(commits, cursorSHA)
	switch {
	case newTop >= 0:
		// 画面先頭のコミットが残っている: その画面行を保つ = 見えている内容が動かない。
		// 新規コミットは画面外の上に積まれるので降らせる演出はしない。
		m.offset = m.clampOffset(headerLineIndex(m.lines(), newTop) - topRow)
		if newCursor >= 0 {
			m.cursor = newCursor
		} else {
			m.cursor = clampIdx(newTop, len(commits)) // カーソルのコミットだけ消えた (amend 等)
		}
	case newCursor >= 0:
		// 画面先頭は消えたがカーソルのコミットは残っている: 選択を保って画面内へ収める
		m.cursor = newCursor
		m.ensureCursorVisible()
	default:
		m.cursor, m.offset = 0, 0 // カーソルは新規コミットの先頭へ (ユーザー要望 2026-07-20)
		// 先頭に増えた新規コミット行を上から降らせる演出。startPullAnim が offset を新規行数だけ
		// 手前 (下スクロール位置) へ置き、tick で 1 行/フレームずつ 0 へ戻すと新規行が上から入り
		// 既存行が下へずれて見える。アニメしないと決まったときだけ即カーソル可視化に落とす
		m.startPullAnim(oldSHAs)
		if !m.pullAnimating {
			m.ensureCursorVisible()
		}
	}

	if !m.hasRepo || len(m.toFetch) == 0 {
		m.pendingFetches = 0 // 取得を始めないので下ろす (fetching() もこれで下りる)
		if m.pullAnimating {
			return added, m.maybeTick() // CI 取得は無いが、アニメーションのために tick を回す
		}
		return added, nil
	}
	return added, tea.Batch(m.startCIFetch(m.toFetch), m.maybeTick())
}

// indexOfCommit は sha のコミットの index (無ければ -1)。sha == "" は常に -1 (錨なし)。
func indexOfCommit(commits []Commit, sha string) int {
	if sha == "" {
		return -1
	}
	for i, c := range commits {
		if c.SHA == sha {
			return i
		}
	}
	return -1
}

// countNewCommits は先頭へ増えた新規コミット数 (既知 SHA に当たるまで)。
func countNewCommits(commits []Commit, oldSHAs map[string]struct{}) int {
	n := 0
	for _, c := range commits {
		if _, ok := oldSHAs[c.SHA]; ok {
			break // 既知コミットに到達 = ここから下は元からある分
		}
		n++
	}
	return n
}

// pullAnimMaxLines は pull アニメで降らせる最大行数。大量 pull でも待ちが伸びすぎないよう
// 「頭だけ」流し、超過分は最初から所定位置に置く (ユーザー要望 2026-07-20)。
const pullAnimMaxLines = 8

// startPullAnim は pull 後に先頭へ増えた新規コミットの行数を求め、あれば offset を
// その分 (上限 pullAnimMaxLines) だけ下げてアニメーションを開始する。tick は
// reloadAfterPull / tickMsg 側で回す。
func (m *browseModel) startPullAnim(oldSHAs map[string]struct{}) {
	newCommits := countNewCommits(m.commits, oldSHAs)
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
	m.pushAnimNext = timeNow().Add(pushAnimStep)
	return true
}

// advancePushAnim は pushAnimStep 経過ごとに push 演出を 1 コミット分進める。
// 最も古い StateUnpushed を消すと境界が 1 コミット上がる。全部消えたら演出終了で、
// 本来の後処理 (CI 全件再取得) へ進む cmd を返す。
func (m *browseModel) advancePushAnim() tea.Cmd {
	if timeNow().Before(m.pushAnimNext) {
		return nil
	}
	m.pushAnimNext = timeNow().Add(pushAnimStep)
	for i := len(m.commits) - 1; i >= 0; i-- {
		if m.statuses[m.commits[i].SHA] == StateUnpushed {
			delete(m.statuses, m.commits[i].SHA)
			if m.pushSlides == nil {
				m.pushSlides = map[string]time.Time{}
			}
			m.pushSlides[m.commits[i].SHA] = timeNow() // 通過した区画の沈み込みを開始
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
		p := float64(timeNow().Sub(start)) / float64(pushSlideDuration)
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
	// 自分の push で origin/* が動いた分は見張りの基準を作り直して飲み込む (gitlog_watch.go)。
	// SHA は変わらないので次の測定は再読込せず基準だけ更新する = push の直後に「更新しました」
	// のトーストと CI 再取得が二重に走らない。🚨 %D の decoration (origin/master の位置) は
	// 読み直していないので古いまま残る (push 前からの挙動。全面リロードを自分の操作の直後に
	// 足さない判断)。飛んでいる測定も同じ理由で捨てる (reloadLog と同じ規律)。
	m.logWatch.hasSeen, m.logWatch.measuring = false, false
	m.awaitCI = map[string]bool{}
	m.awaitAttempts = 0
	if m.pushAnimTip != "" {
		// 🚨 **commits 所属を確かめてから入れる** (issue 223)。awaitCI の不変条件は
		// 「awaitCI ⊆ commits の SHA」で、これを破ると**どの経路でも取り除かれない**
		// 要素が残る: ciPollTargets は commits を走査して targets を組むので commits 外の
		// SHA は追従対象にならず、statuses も永久に現れないので settleAwaitCI も落とせない。
		// 結果 spinnerActive() が下りず、tickMsg の invalidateLines が 80ms ごとに
		// 全行を組み直し続ける (画面は静止しているのにアイドルへ戻らない)。
		// 踏む筋: push 演出中に u で pull → applyLogData が awaitCI を nil にするが
		// pushAnimTip は残る → その pull で履歴が書き換わり tip の SHA が消える →
		// 演出の着地でここへ来る。
		if m.hasCommitSHA(m.pushAnimTip) {
			m.awaitCI[m.pushAnimTip] = true
		}
		m.pushAnimTip = ""
	}
	all := make([]string, 0, len(m.commits))
	for _, c := range m.commits {
		if len(m.awaitCI) == 0 && m.statuses[c.SHA] == StateUnpushed {
			m.awaitCI[c.SHA] = true // commits は新しい順なので最初の unpushed = tip
		}
		all = append(all, c.SHA)
		delete(m.statuses, c.SHA)
		delete(m.details, c.SHA)
	}
	m.invalidateLines()
	return m.startCIFetch(all)
}

// startScrollAnim は一覧のビューポートが動いたとき、表示 offset を旧位置 prev から論理 offset へ
// 数フレームで滑らせる (ユーザー要望「にゅっと」)。呼び出しは j/k の 1 コミット移動と半ページ
// 移動 (ctrl+d/u・PgDn/PgUp。ユーザー要望 2026-07-31)。g/G の端ジャンプは距離が不定なので
// 即時のまま。
//
// glide はフレーム数で終わる (距離では終わらない) ので、1 コミット移動でも半ページでも所要は
// 一定 (~200ms) になり、背高コミット (長メッセージ・stat/patch) でも間延びしない。高さで
// animate/snap が変わる違和感を避けるため行数キャップは設けない (ユーザー要望 2026-07-21)。
// 連打の積み上げ (「押した分だけ遅れて動く」最悪の体感) の抑制は glide.start が持つ。
// 論理 offset は呼び出し側 (ensureCursorVisible / clampOffset) が既に動かしているので触らない。
func (m *browseModel) startScrollAnim(prev int) tea.Cmd {
	if m.offset == prev {
		return nil // ビューポートは動いていない (カーソルが画面内 / 端で clamp された)
	}
	if m.pullAnimating {
		m.glide.stop() // pull アニメ中は積まず即時 (連打の抑制は glide.start が持つ)
		return nil
	}
	if !m.glide.start(prev, m.offset) {
		return nil
	}
	return m.maybeTick()
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

// cancelAll は走行中の全非同期リソース (CI fetch の ctx / usage fetch / push・pull の git /
// issues と git log の fsnotify watcher) を止める後始末の単一ファネル。冪等。quit() (キー操作の終了) と
// main.go の defer の両方が呼ぶ: bubbletea v2 は SIGINT/SIGTERM を InterruptMsg/QuitMsg に
// 変換するが、この 2 つだけは model.Update を経由せず eventLoop が直接 return するため
// (tea.go の handleSignals/eventLoop)、シグナル終了では quit() が走らない。defer 側が無いと
// push/pull 中の SIGTERM (tmux kill-window 等) で deadline なしの git 子プロセスが孤児化する
// (issue 029 P1)。
func (m *browseModel) cancelAll() {
	m.cancel()
	m.usageOv.stop()  // 走行中の usage fetch subprocess を中断 (オーファン化防止)
	m.actModal.stop() // 走行中の push/pull git subprocess を中断 (stall 中の孤児化防止)
	// issues viewer の fsnotify watcher を閉じる。通常終了ではプロセス終了が fd を回収するが、
	// 再起動 (restartSelf の syscall.Exec) は fd テーブルを引き継ぐため明示的に閉じないと漏れる:
	// fsnotify v1.9.0 の darwin backend は監視対象 fd に O_CLOEXEC を付ける一方、kqueue fd 本体
	// (backend_kqueue.go newKqueue) には CloseOnExec を呼ばず、viewer を開いたまま r で再起動する
	// たびに kqueue fd が新プロセスへ 1 本ずつ継承され続ける。
	m.issuesOv.stopWatch()
	m.stopGitLogWatch() // git log の見張りも同じ理由で閉じる (gitlog_watch.go)
	m.doctorOv.stop()   // doctor の走査 goroutine / 子プロセス (brew / simctl) を止める (popup の開閉で残さない)
}

// quit はアプリ全体を終了する (取得中断分は unknown へ落とす)。
func (m *browseModel) quit() (tea.Model, tea.Cmd) { return m.quitWith(true) }

// quitNow は演出なしで即終了する (Ctrl-C の緊急脱出。待たされないことが価値)。
func (m *browseModel) quitNow() (tea.Model, tea.Cmd) { return m.quitWith(false) }

// quitWith は後始末をしてから終了する。animate なら「中央へ吸い込まれる」演出を挟み、
// 着地してから抜ける (tickMsg の settle が tea.Quit を出す)。
//
// 🚨 後始末 (走行中 subprocess の cancel・issues の画面記憶) は演出の前に済ませる: 演出中に
// 端末を閉じられても、止めるべきものは止まっている状態にしておく。
func (m *browseModel) quitWith(animate bool) (tea.Model, tea.Cmd) {
	m.rememberIssuesScreen()
	m.rememberDoctorScreen()
	m.cancelAll()
	if m.fetching() {
		m.fillUnknown()
	}
	if animate && m.zoom.startClose(timeNow()) {
		return m, m.maybeTick()
	}
	m.done = true
	return m, tea.Quit
}

// restartForNewBinary は「新しいバイナリで自分を置き換える」ことを予約して終了する。
//
// exec を Update の中でやらない: bubbletea が端末を raw mode + Alt Screen で握っている最中に
// プロセスを置き換えると、復元されないまま次のプロセスが同じ端末を触ることになる。tea.Quit で
// 抜けて bubbletea に端末を戻させてから、main.go が exec する。
//
// 後始末 (走行中 subprocess の cancel・issues の画面記憶) は quit と共有する。画面記憶が乗るので、
// issues viewer を開いたまま再起動すると同じ画面から再開する (issues_state.go の TTL 内)。
func (m *browseModel) restartForNewBinary() (tea.Model, tea.Cmd) {
	m.restartRequested = true
	return m.quit()
}

// autobuildAfterPull は pull で自分のソースが更新されていたら、その場で裏ビルドを起動する。
//
// 狙いは「glogx を手で起動し直す手間をなくす」(ユーザー要望 2026-08-05): 以前は pull で新しい
// glogx が降ってきても、popup を閉じて開き直すまで再ビルドが始まらなかった。
//
// 🚨 「pull が glogx のソースを含んでいたか」は自分で判定しない。glogx はどの repo でも動くので
// 「今 pull した repo が dotfiles か」の判定が要るように見えるが、shim の指紋比較は自分のソース
// ディレクトリだけを見るので、無関係な repo を pull しても stale にならない = 何も起きない。
// 判定を写経せず shim へ委ねることで、この分岐そのものが不要になる。
//
// 監視中 (起動時に spawn された分がまだ決着していない) なら何もしない: 同じビルドを二重に
// 数えないため。shim の lock があるので二重起動そのものは起きないが、トーストは重複しうる。
func (m *browseModel) autobuildAfterPull() tea.Cmd {
	if m.autobuild.active {
		return nil
	}
	return spawnAutobuildCmd()
}

// restartPromptVisible は再起動ダイアログを今出してよいか。表示 (restartPromptLines) と
// 入力 (handleKey) の両方がこの 1 つの述語を見る。
//
// 🚨 順序を 2 箇所に書かないための関数。以前は「キーは actModal が先に飲む」「描画は
// restartPrompt を後に重ねる」と別々に書いていたため、claude update 中に裏ビルドが完成すると
// 「最前面のダイアログにどのキーも届かない」状態になった (実測: r/j/q/esc/enter/ctrl+g の
// すべてが無反応。しかも更新中モーダルの『完了まで終了できません』をダイアログが覆うので、
// 効かない理由すら画面から消えていた)。
//
// actModal が出ている間は出さない (running() ではなく active())。active() は「描かれる」と
// 「キーを消費する」を兼ねる 1 つの述語で、running() は実行中だけを指すので足りない。
// 理由は 2 つあり、どちらか片方だけ見ると事故る:
//
//   - 実行中 (running): このダイアログの r は restartForNewBinary → cancelAll で走行中の
//     claude update / git を殺す。Ctrl-C をブロックしてまで防いでいる当のものなので、
//     押させてはいけない選択肢を提示しない
//   - 確認待ち (y/N): キーは actModal が持っているのに最前面はこちらになる。画面の
//     「その他のキー: 後で」に従って押した y が push を実行した (実測。無反応より危険)
//
// 完成の事実は restartPending が保持しているので、actModal が手を離せば自然に出る。
func (m *browseModel) restartPromptVisible() bool {
	// 🚨 doctor の削除中も出さない。このダイアログの r は cancelAll で走行中の処理を殺すし、
	// 出ている間はどのキーもダイアログに吸われて doctor に届かない (削除の確認が裏に残る)。
	// actModal を除外しているのと同じ理由 (敵対レビュー 2026-09-03 が実測)
	return m.restartPending && !m.actModal.active() && (!m.doctorOv.visible() || !m.doctorOv.ownsKeys())
}

// restartPromptLines は完成ダイアログの箱 (push/pull 確認と同じ見た目)。
func (m *browseModel) restartPromptLines() []string {
	if !m.restartPromptVisible() {
		return nil
	}
	return centerBox(" 新しい glogx ", []string{
		"新しいバージョンが利用可能です",
		"",
		paint("r: 今すぐ再起動   その他: 後で", ansiDim, m.colored),
	}, m.contentWidth(), m.colored)
}

// keyRepeatGuard は「押しっぱなし」を 1 回の入力として扱う判定窓。🚨 端末はキーを離した
// ことを教えてくれない (離鍵イベントは kitty のキーボード拡張が要り、glogx は要求していない。
// Update の KeyPressMsg の注記を参照) ので、離鍵の代わりに「同じキーが速く来続けたら
// 自動リピート」と時間で判定する。窓は押されるたびに更新するので、押し続けている限り 1 回に
// まとまり、指を離して窓が切れてから次の 1 回になる (ユーザー要望 2026-08-01)。
//
// 250ms の根拠 (このマシンの実測 2026-08-01/2026-08-07): 最初のリピートまで 225ms・以降 30ms
// 間隔 (defaults read -g InitialKeyRepeat=15 / KeyRepeat=2)。1 回目のリピートも窓に収める必要が
// あるので 225ms より長く取り、意識して 2 回押す間隔よりは短く保つ。当初 300ms だったが再打鍵の
// 待ちを縮めたいとの要望 (2026-08-07。200ms 希望だったが 225ms を覆えないため 225ms を超える
// 最小刻みの 250ms で合意)。🚨 InitialKeyRepeat を 16 (240ms) 以上へ変えるとこの窓を素通りして
// 長押しが 2 回 toggle に戻る。その時はここも追従させること。
const keyRepeatGuard = 250 * time.Millisecond

// repeatGuardedKeys は自動リピートを潰すキー。
//
// 🚨 移動系 (j/k/矢印/半ページ) は入れない: 押しっぱなしでスクロールし続けるのは期待される
// 動作で、潰すと「押しても動かない」壊れ方になる。潰すのは「開いて閉じる」を繰り返してしまう
// トグルと、押すたびに subprocess やファイル操作が走るキーだけ。
var repeatGuardedKeys = map[string]bool{
	"i": true, // issues viewer (押しっぱなしで高速に開閉する。ユーザー報告 2026-08-01)
	"s": true, // status viewer (i と同じ toggle。ユーザー報告 2026-08-07)
	"U": true, // usage オーバーレイ (開くたびに取り直しの判定が走る)
	"R": true, // ratelimit ダッシュボード (U と同じ toggle。開くたびに取得判定が走る)
	"D": true, // doctor (開くたびにスキャンが走る。押しっぱなしで開閉と走査を繰り返す)
	"d": true, // diff ポップアップ
	"P": true, // PR 状態ポップアップ
	"n": true, // next へ移動の確認 (開いた確認を次のリピートが閉じてしまう)
	"a": true, // 状態フィルタの巡回 (3 段なので押しっぱなしだと今どこか分からなくなる)
}

// swallowKeyRepeat は自動リピートとみなしたキーを飲む (true = 何もしない)。
func (m *browseModel) swallowKeyRepeat(key string) bool {
	if !repeatGuardedKeys[key] {
		m.lastKey, m.lastKeyAt = "", time.Time{} // 別のキーが来たら押しっぱなしは切れている
		return false
	}
	now := timeNow()
	repeat := key == m.lastKey && now.Sub(m.lastKeyAt) < keyRepeatGuard
	// 🚨 飲んだときも基準を更新する: これが「離すまで 1 回扱い」の実体で、押し続けている限り
	// 窓が伸び続ける。更新しないと keyRepeatGuard ごとに 1 回ずつ通ってしまう。
	m.lastKey, m.lastKeyAt = key, now
	return repeat
}

// wantsUsageRefresh は 1 分ごとの /usage 再取得を回すか。右上オーバーレイと全画面
// ダッシュボードのどちらかが見えていれば回す (どちらも同じ Snapshot を描くので、取得経路は
// 1 本のまま)。見えていないときに回さない理由は usageRefreshMsg ハンドラの注記。
func (m *browseModel) wantsUsageRefresh() bool {
	return m.usageOv.visible || m.rlDash.visible()
}

// rlDashLoading はダッシュボードが取得待ち (スピナーを回す) か。usageOverlay.loading() は
// 右上オーバーレイの表示状態を見るので、ダッシュボードだけが開いているときは false になる。
func (m *browseModel) rlDashLoading() bool {
	return m.rlDash.visible() && m.usageOv.snap == nil && m.usageOv.err == nil
}

// toggleRatelimitDash は全画面 ratelimit ダッシュボードの開閉 (R)。開くときは右上の usage
// オーバーレイを引っ込め (同じ値を 2 か所に出さない)、Snapshot が無い / 古ければ取得を起こす。
// 🚨 取得の判定は usageOv に委ねる (single-flight ガードと cancel を 1 か所に保つため)。
func (m *browseModel) toggleRatelimitDash() tea.Cmd {
	m.rlDash.toggle()
	if !m.rlDash.visible() {
		return m.maybeTick()
	}
	m.usageOv.dismiss()
	if m.usageOv.snap == nil {
		// 初回はディスクキャッシュを使う (fresh なら subprocess を起こさず即描ける)
		return tea.Batch(m.usageOv.fetchCmd(true), m.maybeTick())
	}
	if m.usageOv.stale() {
		return tea.Batch(m.usageOv.fetchCmd(false), m.maybeTick())
	}
	return m.maybeTick()
}

// toggleUsage は usage オーバーレイの開閉 (U)。コミット一覧と issues viewer の両方から呼ぶ
// (どちらの画面でも同じ意味にする。viewer 側の合成は viewLines)。
//
// 非表示中は定期リフレッシュを止めているので、再表示のここで陳腐なら取り直す。表示は last-good
// を出したまま静かに差し替わる (snap があれば loading() は false のままでスピナーに落ちない =
// 開いた瞬間に数字が消えない)。
func (m *browseModel) toggleUsage() tea.Cmd {
	m.usageOv.toggle()
	if !m.usageOv.visible {
		return nil
	}
	if m.usageOv.stale() {
		return tea.Batch(m.usageOv.fetchCmd(false), m.maybeTick())
	}
	return m.maybeTick()
}

// rememberIssuesScreen は「issues viewer を出したまま終了したら次の起動で復元する」ための
// 保存 (issues_state.go)。🚨 一覧から終了したときは消す — 残すと、一覧を見て閉じた次の起動で
// 2 回前の viewer が蘇る (ユーザー指定: git log 一覧のときは復元しない)。
func (m *browseModel) rememberIssuesScreen() {
	if s, ok := m.issuesOv.screen(timeNow()); ok {
		_ = saveIssuesScreen(s) // 保存できなくても終了は妨げない
		return
	}
	removeIssuesScreen()
}

// rememberDoctorScreen は「doctor を出したまま終了したら次の起動で復元する」ための保存
// (doctor_resume.go)。🚨 issues 側と同じく、**開いていないまま終了したら消す** — 残すと、
// 一覧を見て閉じた次の起動で 2 回前の doctor が蘇る。
func (m *browseModel) rememberDoctorScreen() {
	if m.doctorOv.visible() {
		_ = saveDoctorScreen(doctorScreen{Tab: int(m.doctorOv.tab), SavedAt: timeNow()}) // 失敗しても終了は妨げない
		return
	}
	removeDoctorScreen()
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
	case "P":
		// パネル内では PR 状態ポップアップを開かない仕様 (重ね順が複雑化する)。他の無効操作
		// (job 未選択の o/r 等) と同様、無反応でなく理由をトーストで返す
		m.toast.show("PR 状態はパネルを閉じてから P で表示します", false)
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
// 🚨 job.URL (= StatusContext の targetUrl 等) は外部 CI が任意に設定でき無害化を一切通って
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
	m.copyWithToast(b.String(), fmt.Sprintf("job 詳細をコピーしました (%d 行)", len(lines)))
}

// askRerun はフォーカス中 job の CI 再実行確認 (y/N) に入る (job パネル / job 詳細の r キー)。
// 再実行できないケース (StatusContext / 失敗以外) は確認を出さず notice で理由を伝える。
func (m *browseModel) askRerun() {
	job, ok := m.focusedJob()
	if !ok {
		// タイトル行フォーカス (job 未選択) で r。o/Y と揃えて選択を促す。
		m.toast.show("job を選択してから r で再実行します", false)
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
	if job.running() {
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
var timeNow = time.Now //nolint:forbidigo // シームの定義点そのもの (ここ以外の time.Now を lint が禁止する)

// jobDetailRows は詳細ポップアップに一度に表示する行数の上限。実際の行数は
// 端末の高さに合わせて visibleDetailRows が縮める。
const jobDetailRows = 15

// visibleDetailRows は詳細ポップアップが実際に使える行数 (job パネルとヒント行を
// 差し引いた残り。低い端末で詳細ボックスがビューポートに切られ、末尾スクロールが
// 見えなくなるのを防ぐ)。-4 = 詳細の枠 2 行 + パネル・詳細それぞれの下端落ち影 1 行ずつ。
func (m *browseModel) visibleDetailRows() int {
	jobBoxLines := min(max(len(m.details[m.panelSHA]), 1), maxPanelJobs) + 2
	return max(min(jobDetailRows, m.pageSize()-jobBoxLines-4), 3)
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

// copyWithToast はクリップボードへコピーし、結果をトーストで通知する共有経路 (失敗文言が
// 4 箇所で複製されていたのを一本化)。失敗トーストは lastWarning を汚さない (コピー対象の
// 警告を上書きして w の再試行を潰さないため、showWarning は通さない)。
func (m *browseModel) copyWithToast(text, successMsg string) {
	if err := copyToClipboard(text); err != nil {
		m.toast.show("コピーに失敗しました: "+firstLine(err.Error()), false)
		return
	}
	m.toast.show(successMsg, true)
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
	m.copyWithToast(url, "コピーしました: "+url)
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
	// パネルコミットの Details 取得と ETA basis の補充を仕掛ける。details 既取得なら basis 補充
	// だけ即走る (basis 判定に details が要るため)。追従チェーンはパネルとは独立して pending で
	// 回っているので、ここでは single-flight の ensureCIPoll を通すだけ (未取得なら detailMsg 側)。
	return tea.Batch(m.fetchPanelDetails(sha), m.maybeFetchETABasis(), m.ensureCIPoll())
}

// ciPollTargets は「まだ決着していないので取り直す」SHA。追従の条件はこの 1 箇所だけが決める:
//
//   - StatePending のコミット: 完了 (success/failure/neutral) まで無条件に追う。パネルを開いて
//     いるかどうか・push からどれだけ経ったかは問わない
//   - 取得済み Details に実行中 job がある SHA: ロールアップが pending にならない組み合わせ
//     (「1 件失敗 + 1 件実行中」は aggregateRollup が失敗優先で StateFailure) を拾うため。
//     これが無いと、失敗を含むコミットの残り job や rerun した job の経過時間が固まる
//   - awaitCI: push 直後で CI がまだ 1 つも見えない SHA (ciAwaitMaxAttempts で打ち切る)
//   - panelGrace 中のパネル SHA: rerun 要求が GraphQL に映るまでのラグ吸収
func (m *browseModel) ciPollTargets() []string {
	seen := make(map[string]bool, len(m.commits))
	targets := make([]string, 0, len(m.commits))
	add := func(sha string) {
		if sha == "" || seen[sha] {
			return
		}
		seen[sha] = true
		targets = append(targets, sha)
	}
	for _, c := range m.commits {
		if m.statuses[c.SHA] == StatePending || m.awaitCI[c.SHA] || m.hasRunningJob(c.SHA) {
			add(c.SHA)
		}
	}
	if m.panelGrace > 0 {
		add(m.panelSHA)
	}
	return capFetchSHAs(targets)
}

// ciPollFetch は ciPoll の再取得を張る差し替え点 (本体は fetchCIStatusesCmd)。並行取得を
// 弾くガードをテストが回数で観測するために変数にしている (runJobRerun と同じ理由)。
var ciPollFetch = fetchCIStatusesCmd

// scheduleCIPoll は現世代の ciPollMsg を ciPollInterval 後に発火させる (チェーンの「継続」用)。
// チェーンの「開始」は必ず ensureCIPoll を通す。
func (m *browseModel) scheduleCIPoll() tea.Cmd {
	gen := m.ciPollGen
	return tea.Tick(ciPollInterval, func(time.Time) tea.Msg { return ciPollMsg{gen: gen} })
}

// ensureCIPoll は ciPollMsg の自己更新チェーンを single-flight で 1 本だけ張る (maybeTick と
// 同型)。既に生きている / 追従対象が無いときは nil を返す。開始点が複数あっても
// (起動時の ciResultMsg / detailMsg / refetchAfterPush / rerunMsg / openPanel) チェーンが
// 二重化してポーリング頻度が倍にならないのはこのガードによる。
func (m *browseModel) ensureCIPoll() tea.Cmd {
	if m.ciPolling || len(m.ciPollTargets()) == 0 {
		return nil
	}
	m.ciPolling = true
	return m.scheduleCIPoll()
}

// settleAwaitCI は「push したが CI がまだ見えない」SHA の後始末。CI が見えたら awaitCI を
// 卒業させ、見えていない SHA は結果を捨てる (statuses から消してスピナーに戻し、fetched からも
// 外してファイルキャッシュへ「checks なし」を負キャッシュとして残さない)。上限に達したら諦める。
// hasCommitSHA は sha が今の commits に在るか (awaitCI の不変条件の判定に使う)。
func (m *browseModel) hasCommitSHA(sha string) bool {
	return slices.ContainsFunc(m.commits, func(c Commit) bool { return c.SHA == sha })
}

func (m *browseModel) settleAwaitCI() {
	if len(m.awaitCI) == 0 {
		return
	}
	for sha := range m.awaitCI {
		// 🚨 **commits に無い SHA は毎周期ここで落とす** (issue 223)。入口 (refetchAfterPush)
		// でも弾いているが、awaitCI を張る経路が増えたときにこちらが最後の砦になる。
		// 落とさないと statuses が永久に現れず、下の switch は default 側を回り続けるだけで
		// awaitCI から消えない = スピナーと再描画が止まらない。
		if !m.hasCommitSHA(sha) {
			delete(m.awaitCI, sha)
			continue
		}
		switch m.statuses[sha] {
		case StatePending, StateSuccess, StateFailure, StateNeutral:
			delete(m.awaitCI, sha) // CI が見えた: 以降は statuses 起点の通常の追従へ
		default:
			delete(m.statuses, sha)
			delete(m.fetched, sha)
		}
	}
	m.invalidateLines()
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
	if m.fetching() && slices.Contains(m.toFetch, sha) {
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
		if j.running() {
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
		case m.fetching() && slices.Contains(m.toFetch, c.SHA): // 進行中の一括取得を待つ
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
	// パネルを閉じても CI 追従ポーリングは止めない (pending なら追い続けるのが不変条件)。
	// 閉じるのはパネル固有の状態だけ。
	m.panelGrace = 0    // rerun 直後の猶予はパネルと一緒に終える (パネル SHA を狙う猶予なので)
	m.copyOnDetail = "" // Y のコピー予約も破棄 (閉じた後の到着で意図しないコピーをしない)
	m.detailOv.close()  // 詳細ポップアップも閉じる (panel/detail 両クラスタの choke point)
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
	//
	// 🚨 ここは $EDITOR を見ずに nvim 固定にする: 開くのが実ファイルでなく標準入力 (`-`) で、
	// scratch 化も vim 系の -c/-R に依存しているため、任意のエディタでは成立しない
	// (code - / nano - は不可)。実ファイルを開く経路の $EDITOR 対応は editorCommand の doc を参照。
	cmd := exec.Command("nvim", "-R", "-c", "setlocal buftype=nofile noswapfile nomodifiable", "-")
	cmd.Stdin = strings.NewReader(jobLogText(lines))
	return runEditorCmd(cmd)
}

// openJob はパネルで選択中の job の詳細ページをブラウザで開く。
func (m *browseModel) openJob() tea.Cmd {
	job, ok := m.focusedJob()
	if !ok {
		// タイトル行フォーカス (job 未選択) で o。Y (copyJobContext) と揃えて選択を促す
		// (job パネルの o/r/Y は job 未選択なら無反応なので理由をトーストで出す)。
		m.toast.show("job を選択してから o で開きます", false)
		return nil
	}
	if job.URL == "" {
		m.toast.show("この job には詳細ページの URL がありません", false)
		return nil
	}
	return m.openURLCmd(job.URL)
}

// prTargetSHA は PR 系操作 (p/P) の対象 SHA を返す。取得できない状態 (コミットなし /
// remote なし / 未 push) は理由をトーストで出して ok=false (openPR / openPRStatus で
// 文言まで同一だったガード 3 連の一本化)。
func (m *browseModel) prTargetSHA() (sha string, ok bool) {
	if len(m.commits) == 0 {
		return "", false
	}
	if !m.hasRepo {
		m.toast.show("GitHub の remote が無いため PR を取得できません", false)
		return "", false
	}
	sha = m.commits[m.cursor].SHA
	if m.statuses[sha] == StateUnpushed {
		m.toast.show("未 push のコミットに PR はありません", false)
		return "", false
	}
	return sha, true
}

// openPR はカーソル位置のコミットに紐づく PR をブラウザで開く (p キー)。
// commit → PR の関連は GitHub (associatedPullRequests) から取得し、結果はキャッシュする。
func (m *browseModel) openPR() tea.Cmd {
	sha, ok := m.prTargetSHA()
	if !ok {
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
	sha, ok := m.prTargetSHA()
	if !ok {
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
	// current()==nil の 2 状態 (取得中 / PR なし) はどちらもポップアップ自身が spinner・
	// 「(紐づく PR はありません)」で可視化済みなので、o/y は無言 no-op でよい (取得中に
	// 「PR がありません」トーストを出すと誤報になる)。
	case "o":
		if pr := m.prStatusOv.current(); pr != nil {
			return m, m.openURLCmd(pr.URL)
		}
	case "y":
		if pr := m.prStatusOv.current(); pr != nil {
			m.copyWithToast(pr.URL, "コピーしました: "+pr.URL)
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
	// 半ページ移動は glide (scroll_glide.go) で進むので tick を張る。張らないと advanceGlide を
	// 呼ぶ者がおらず、表示位置がスクロール前で固まったまま「キーが効かない」ように見える
	// (敵対的レビュー P1 2026-07-31: Space 後に j を何度押しても先頭行が出続ける実測)。
	// 1 行移動・閉じるは glide を使わないので tick を増やさない。
	if m.diffOv.glide.active {
		return m, m.maybeTick()
	}
	return m, nil
}

// visibleDiffRows は diff ポップアップの本文行数。diff は主役コンテンツなので
// ビューポートほぼ全面 (枠 2 行 + 下端の落ち影 1 行 + 余白 1 行 + ヒント行ぶんを差し引く) を使う。
// 端末の高さ (pageSize) 依存なのでレイアウトを知る browseModel 側に残す。
func (m *browseModel) visibleDiffRows() int {
	return max(m.pageSize()-5, 3)
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

// spinnerActive は「毎フレームの tick チェーンを回し続けるか」の単一ゲート
// (アニメ・スピナー・ライブ経過時間の出典をすべて OR で束ねる)。
//
// 🚨 ここに「フォーカスされているか」を足して非フォーカス中の tick を止める案 (bubbletea v2 の
// FocusMsg/BlurMsg。o/p で別アプリへ移った後も 80ms tick と usage の毎分リフレッシュが
// 回り続けるのを削る) は、実装可能・前提も揃っている (tmux は focus-events on) が
// 「今は不要」とのユーザー判断で見送っている (2026-07-25)。CPU が気になると言われたら再評価する。
// 経緯と他の未採用 v2 機能は docs/glogx-bubbletea-v2.md。
func (m *browseModel) spinnerActive() bool {
	// 演出 (glide / toast / 開閉スライド / zoom) は列挙しない: tickInterval が周期を上げている =
	// 何かの演出中、で導出する。演出の登録先を tickInterval の 1 箇所に保つ (同期漏れの再発防止)
	return m.tickInterval() != spinnerInterval || m.fetching() || m.actModal.running() || m.pullAnimating || m.pushAnimating || len(m.pushSlides) > 0 || len(m.awaitCI) > 0 || len(m.detailsLoading) > 0 || m.detailOv.fetching() || m.diffOv.fetching() || m.prStatusOv.fetching() || m.panelHasRunningJob() || m.usageOv.loading() || m.rlDashLoading() || m.issuesOv.loading() ||
		m.statusOv.fetching() || m.doctorOv.scanning() || m.doctorOv.deleting()
}

// issuesOpts は issues viewer へ渡す描画情報。カーソル行の強調はコミット一覧と同じ
// cursorLine (溝の矢印 + 暗青 bg) を貸すことで、見た目の語彙を 1 つに保つ。
// statusOpts は status viewer の描画情報。🚨 cursorPaint は渡さない (contentWidth まで塗る
// bgLine では 2 カラムのプレビュー側まで背景が伸びる。statusRenderOpts の doc 参照)。
func (m *browseModel) statusOpts() statusRenderOpts {
	return statusRenderOpts{
		width:   m.contentWidth(),
		page:    m.pageSize(),
		colored: m.colored,
		spinner: m.spinner(),
	}
}

// viewport は描画情報から「窓の寸法 + 色」を取り出す (キー処理へ渡す形)。
func (o statusRenderOpts) viewport() statusViewport {
	return statusViewport{width: o.width, page: o.page, colored: o.colored}
}

// ratelimitOpts は全画面 ratelimit ダッシュボードの描画情報。データは usageOv の Snapshot を
// そのまま渡す (取得経路を 1 本に保つ。ratelimit_dashboard.go 冒頭)。
func (m *browseModel) doctorOpts() doctorRenderOpts {
	return doctorRenderOpts{env: disk.RealEnv(), width: m.contentWidth(), page: m.pageSize(), colored: m.colored, spinner: m.spinner(), now: timeNow()}
}

func (m *browseModel) ratelimitOpts() ratelimitRenderOpts {
	return ratelimitRenderOpts{
		width:   m.contentWidth(),
		page:    m.pageSize(),
		colored: m.colored,
		spinner: m.spinner(),
		snap:    m.usageOv.snap,
		err:     m.usageOv.err,
		now:     timeNow(),
	}
}

func (m *browseModel) issuesOpts() issuesRenderOpts {
	return issuesRenderOpts{
		width:       m.contentWidth(),
		page:        m.pageSize(),
		colored:     m.colored,
		spinner:     m.spinner(),
		cursorPaint: m.cursorEmphasis, // 溝は viewer が持つので強調だけ渡す (二重矢印の防止)
	}
}

// panelHasRunningJob は表示中の job パネルに実行中 (経過時間が増える) job があるか。
// tick を回し続けて「N 経過 / 残り ~M」をライブ更新するため (spinnerActive が false だと
// tick が止まり、パネルを開いたまま経過秒が固まる)。
func (m *browseModel) panelHasRunningJob() bool { return m.hasRunningJob(m.panelSHA) }

// hasRunningJob は sha の取得済み Details に実行中 job があるか。
// 🚨 statuses (ロールアップ) では代用できない: aggregateRollup は失敗を最優先するので
// 「1 件失敗 + 1 件実行中」は StateFailure になり、pending 判定では実行中を取りこぼす。
func (m *browseModel) hasRunningJob(sha string) bool {
	if sha == "" {
		return false
	}
	for _, job := range m.details[sha] {
		if job.running() {
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
	m.offset = windowOffsetFor(m.offset, headerLineIndex(lines, m.cursor), len(lines), m.pageSize())
}

// topVisibleCommitIdx はビューポート先頭 (offset 行目) に見えているコミットの index。
// 区切り行 (CommitIdx == -1) の上に乗っている場合は下へ辿る。どれも無ければ -1。
func topVisibleCommitIdx(lines []Line, offset int) int {
	for i := max(offset, 0); i < len(lines); i++ {
		if lines[i].CommitIdx >= 0 {
			return lines[i].CommitIdx
		}
	}
	return -1
}

// headerLineIndex は commitIdx のヘッダー行 (カーソルが乗る行) の行 index。
// 見つからなければ 0 (行集合が空 / 対象外のとき先頭へ倒す)。
func headerLineIndex(lines []Line, commitIdx int) int {
	for i, l := range lines {
		if l.Header && l.CommitIdx == commitIdx {
			return i
		}
	}
	return 0
}

// View は画面内容に加えて端末モード (Alt Screen) も宣言する。bubbletea v2 では
// EnterAltScreen 相当の命令的 Cmd / WithAltScreen オプションが廃され、毎フレームの
// View が端末機能の唯一の出典になっている。
func (m *browseModel) View() tea.View {
	v := tea.NewView(m.viewLines())
	// Alt Screen 上でブラウズし q で抜けると表示は消える (git log の pager と同じ。
	// ユーザー要望 2026-07-17)。
	v.AltScreen = true
	return v
}

// finishWithGlobalChrome はどの画面 (コミット一覧 / issues / status viewer) でも出るべき
// オーバーレイを重ねて窓を仕上げる。全ビューの唯一の出口。
//
// 🚨 ここに書く 4 つ (action モーダル → 再起動 → usage → トースト) はビューごとに書かないこと。
// 過去に viewer が全画面だった頃、この合成を一覧側にしか書いておらず「issues を開いている間は
// 通知が画面に一切出ない」時期があった。前面順も含めてこの 1 箇所が契約の出典
// (issue 085: 以前は viewer 用と一覧用に逐語 2 コピーあった)。
//
// 各オーバーレイの理由:
//   - action モーダル (pull 確認・実行中): キーは viewer より先に actModal が捌く (handleKey の
//     判定順) ので、描かないと「見えないモーダルがキーを持つ」= y/N の行き先が画面から分からない
//     状態になる。status viewer の p (pull) で実際にこの経路へ来る。overlayCenteredBox は行を
//     塗り潰さず左右の背景を残して合成する (モーダルの左側テキストが消えるのを解消)
//   - 再起動ダイアログ: 中央。答えるまで残る (次の 1 キーで必ず閉じる)
//   - usage: 上部右端の複数行モーダル。U で再表示、任意キーで消える
//   - トースト: 右下 (hint 行の直上) に数秒。push/pull 完了や viewer の操作結果 (コピー等) を
//     glogx 共通の語彙で出す
func (m *browseModel) finishWithGlobalChrome(window []string, page int) string {
	if box := m.centerModalLines(); len(box) > 0 {
		window = overlayCenteredBox(window, box, m.contentWidth(), page, m.colored)
	}
	if box := m.restartPromptLines(); len(box) > 0 {
		window = overlayCenteredBox(window, box, m.contentWidth(), page, m.colored)
	}
	if box := m.usageOv.boxLines(m.contentWidth(), m.colored, m.spinner()); len(box) > 0 {
		window = overlayBoxTopRight(window, box, m.contentWidth(), m.colored)
	}
	if box := m.toast.boxLines(m.colored, toastDrawBudget(page)); len(box) > 0 {
		window = overlayBoxBottomRight(window, box, m.contentWidth(), m.colored)
	}
	return m.finishWindow(window, page)
}

// toastDrawBudget は、通常の半ページ予算を保ちつつ重要警告 2 枚ぶんを確保する。
// 下限がないと狭い窓で重要警告が 1 枚に減り、上限がないと 2 箱が窓を覆うため、両方が要る。
func toastDrawBudget(page int) int {
	return min(max(page/2, toastBoxLines*2), max(page-1, toastBoxLines))
}

// viewLines は画面content を組む本体 (旧 View)。テストはここではなく View().Content を見る。
func (m *browseModel) viewLines() string {
	if m.done {
		// 終了確定後は何も描かない (Alt Screen の復帰で表示は消える)
		return ""
	}
	page := m.pageSize()
	// issues viewer は全画面: コミット一覧とオーバーレイ群を描かずに窓ごと差し替える。
	// lines() がちょうど page 行返すので、枠と hint 行の経路は共通のまま (finishWindow)。
	// status viewer も全画面: 一覧とオーバーレイ群を描かず窓ごと差し替える (issues と同じ経路)。
	// 🚨 issues より前に判定する必要はない (同時には開かない: i/s の横断は閉じてから開き、
	// 起動時 restore も status が開いていれば捨てる — issuesRestoreMsg の注記) が、
	// 共通 chrome (トースト / usage 等) は finishWithGlobalChrome が同じ前面順で載せる。
	// ratelimit ダッシュボードも全画面 (issues / status と同じ経路)。同時には開かない:
	// viewer からの R も、ダッシュボードからの i/s も「閉じてから開く」ので、3 画面のうち
	// 高々 1 枚しか shown にならない (起動時 restore は開いていれば捨てる — issuesRestoreMsg
	// の注記)。開いている間の他のキーは handleKey が飲む。
	switch m.activeFullScreen() {
	case fullScreenRatelimit:
		return m.finishWithGlobalChrome(m.rlDash.lines(m.ratelimitOpts()), page)
	case fullScreenDoctor:
		return m.finishWithGlobalChrome(m.doctorOv.lines(m.doctorOpts()), page)
	case fullScreenStatus:
		return m.finishWithGlobalChrome(m.statusOv.lines(m.statusOpts()), page)
	case fullScreenIssues:
		return m.finishWithGlobalChrome(m.issuesOv.lines(m.issuesOpts()), page)
	case fullScreenNone, fullScreenCount:
		// 全画面ビューアが出ていない = 下でコミット一覧を組む
	}
	lines := m.lines()
	// glide 中は表示 offset (途中位置) で窓を切る。それ以外は論理 offset。
	renderOffset := m.glide.offset(m.offset)
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
			text = padSpaces(off) + paint(stripANSI(text), ansiDim, m.colored)
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
	// リストが 1 画面に収まらないときは右端にスクロールバー列を出す (diff/job overlay と同じ
	// 見た目)。overlay 群の合成より先に足す = ポップアップ類はバーの上に浮く。offset は
	// glide 中の表示 offset を使っているので thumb もグライドに追従する。
	window = scrollbarColumn(window, m.contentWidth(), len(lines), offset, m.colored)
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
	// ここから先 (action モーダル → 再起動 → usage → トースト) は全ビュー共通なので
	// finishWithGlobalChrome が持つ。一覧固有のオーバーレイ (スクロールバー / job パネル /
	// diff / PR 状態) より後面に来ない = 共通 chrome が常に最前面。
	return m.finishWithGlobalChrome(window, page)
}

// finishWindow は組み終わった窓をフレームで包み hint 行を足して 1 フレームの文字列にする。
// コミット一覧と issues viewer (全画面) の共通の出口 (枠・hint の扱いを 1 箇所に保つ)。
func (m *browseModel) finishWindow(window []string, page int) string {
	// フレーム有効時は最外周を余白 + 枠 + 右下ドロップシャドウで包む (issue 025)。板の高さを
	// 安定させるため、コンテンツが少なくても pageSize 行まで空行でパディングしてから包む
	// (板が常にビューポート一杯 = リサイズや行数変動で枠が踊らない)。hint は板の外・最下行。
	if m.frameActive() {
		for len(window) < page {
			window = append(window, "")
		}
		window = wrapWindowFrame(window, m.width, m.colored)
	}
	// 1 フレーム分の最終文字列は 10-30KB になる。pre-size しないと Builder の倍々成長で
	// 出力サイズと同程度のバッファを毎フレーム捨てる (alloc プロファイルで View 全体の 57%)。
	hint := m.hintLine()
	size := len(hint)
	for _, w := range window {
		size += len(w) + 1 // +1 = 行末の "\n"
	}
	// 開閉の演出中は画面全体 (枠 + hint) を中央から開く / 中央へ吸い込む姿へ変換する (zoom.go)。
	// 🚨 hint も含めて 1 枚として扱う: hint だけ最下行に残ると「板は縮んだのに文字が浮いている」
	// 見え方になる。
	if scale := m.zoom.scale(timeNow()); scale < appZoomSnap {
		all := make([]string, 0, len(window)+1)
		all = append(all, window...)
		all = append(all, hint)
		zoomed := zoomWindow(all, scale, m.width, m.colored, m.frameActive())
		window, hint = zoomed[:len(zoomed)-1], zoomed[len(zoomed)-1]
		size = len(hint)
		for _, w := range window {
			size += len(w) + 1
		}
	}
	// 画面全体の地色 (全画面 ratelimit ダッシュボードだけ)。枠・余白・影・hint まで含めて
	// 塗るので、ここ (1 フレームの出口) で 1 回だけ掛ける。
	bg := m.screenBg()
	if bg != "" {
		size += (len(window) + 1) * (len(bg) + len(ansiReset) + m.width)
	}
	var b strings.Builder
	b.Grow(size)
	for _, w := range window {
		b.WriteString(paintScreenBg(w, bg, m.width))
		b.WriteString("\n")
	}
	b.WriteString(paintScreenBg(hint, bg, m.width))
	return b.String()
}

// screenBg は画面全体に敷く地の色 ("" = 端末の地色のまま)。
//
// 🚨 面塗りは既定では**しない**。bgLine の doc にあるとおり、push 済みエリアの面塗りは
// 「環境の配色次第で視認性を落とす」としてユーザー判断で撤去した経緯があり、その判断は
// 生きている (面塗りを自発的に増やさない)。ここが例外なのは、全画面の残量表示だけは地色を
// 固定したいという明示要望があるため (2026-09-01。理由は ansiScreenBg の doc)。
func (m *browseModel) screenBg() string {
	if !m.colored || !m.rlDash.visible() {
		return ""
	}
	return ansiScreenBg
}

// paintScreenBg は 1 行を bg で端末幅まで塗る。行内の SGR リセットで地色が切れるので、
// リセット直後に張り直す (bgLine と同じ手口だが、あちらは板の内側 contentWidth 幅、
// こちらは枠や hint も含む端末幅ぶん)。
func paintScreenBg(line, bg string, width int) string {
	if bg == "" || width <= 0 {
		return line
	}
	line = clipToWidth(line, width)
	pad := max(width-dispWidth(line), 0)
	return bg + reapplyAfterReset(line, bg) + padSpaces(pad) + ansiReset
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
	box := buildShadowPanelBox(title, rows, width, m.colored, ansiDim)
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

// bgLine は行全体を指定 bg で端末幅まで塗る (行内の SGR リセットで bg が切れないよう、
// リセット直後に bg を張り直す)。色なしではそのまま返す (bg が使えない)。
// NOTE: push 済みエリアの面塗りにも使っていたが、bg の面塗りは環境の配色次第で
// 視認性を落とすためユーザー判断で撤去 (2026-07-19)。push 境界の可視化は境界線
// (insertPushBoundary) に一本化。面塗りの再提案はしない。
// cursorEmphasis はカーソル行の「強調だけ」を施す (溝の矢印は付けない)。issues viewer へ渡す
// cursorPaint はこれ。
//
// 🚨 cursorLine を渡してはいけない: あちらは溝の矢印を前置するため、溝を自分で持つ viewer
// (行の幅計算が cursorGutterWidth 前提。issues_view.go の rowLine) では矢印が二重になる
// (実測 2026-07-31: "→ → 030 ○ feat alpha")。cursorPaint の契約は issuesRenderOpts の doc
// どおり「強調」だけで、溝の所有者は viewer 側。
func (m *browseModel) cursorEmphasis(text string) string {
	if !m.colored {
		return text // 色なしでは矢印 (viewer 側の溝) だけが指標
	}
	return m.bgLine(text, ansiCursorBg)
}

func (m *browseModel) bgLine(text, bg string) string {
	if !m.colored {
		return clipToWidth(text, m.contentWidth())
	}
	text = clipToWidth(text, m.contentWidth())
	pad := max(m.contentWidth()-dispWidth(text), 0)
	return bg + reapplyAfterReset(text, bg) + padSpaces(pad) + ansiReset
}

func (m *browseModel) hintLine() string {
	// 全画面ビューアの hint は viewer 自身のものだけを出す (issue 227 で activeFullScreen から
	// 導出する形に寄せた)。🚨 **actModal の確認より前に return するのは意図**: status viewer の
	// 中から `b` / `p` で push / pull の確認を出したとき、hint 行は viewer の語彙のままになる
	// (中央の確認モーダルが y/N を案内するので、狭い hint 行を奪ってまで二重に出さない)。
	// この順序は issue 227 の前からのもので、構造が変わっただけ。🚨 CI 進捗・GH 警告の前置はしない: viewer の hint は popup の実幅
	// ぴったりに詰めてあり (issues_view.go の hint)、前置すると末尾のキー案内 = 抜ける手段が
	// 黙って切り落とされる。CI は viewer を閉じれば見える。
	switch m.activeFullScreen() {
	case fullScreenRatelimit:
		return m.hintLineText(m.rlDash.hint())
	case fullScreenDoctor:
		return m.hintLineText(m.doctorOv.hint(m.hintWidth()))
	case fullScreenStatus:
		return m.hintLineText(m.statusOv.hint(m.hintWidth()))
	case fullScreenIssues:
		return m.hintLineText(m.issuesOv.hint())
	case fullScreenNone, fullScreenCount:
		// 全画面ビューアが出ていない = 下の一覧の hint
	}
	hint := "j/k: 移動  Enter: CI job  d: diff  o: ブラウザ  p: PR  P: PR 状態  y: URL コピー  b: push  u: pull  i: issues  U: usage  R: 残量  C: update  D: doctor  w: 警告コピー  q: 終了"
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
	case m.actModal.anyUpdating():
		hint = m.spinner() + " " + strings.Join(m.actModal.updatingTargets(), " + ") + " update..."
	case m.diffOv.visible():
		hint = "j/k/Space: スクロール  g/G: 先頭/末尾  y: URL コピー  q/h: 閉じる"
	case m.prStatusOv.visible():
		hint = "o: PR をブラウザで開く  y: URL コピー  P/q/h: 閉じる"
	case m.detailOv.visible():
		hint = fitHintItems(m.hintWidth(), []hintItem{
			{"j/k: スクロール", 3},
			{"v: nvim で開く", 4},
			{"r: 再実行", 3},
			{"Enter/h/q: 戻る", 1}, // 抜ける手段は最優先
			{"o: ブラウザ", 4},
			{"y: URL", 5},
			{"Y: 詳細コピー", 5},
		})
	case m.panelSHA != "" && m.panelCursor >= 0:
		hint = fitHintItems(m.hintWidth(), []hintItem{
			{"j/k: job 移動", 3},
			{"Enter: 詳細ログ", 3},
			{"r: 再実行", 4},
			{"o: ブラウザ", 4},
			{"d: diff", 5},
			{"p: PR", 5},
			{"y: URL", 5},
			{"Y: 詳細コピー", 6},
			{"h/q: 閉じる", 1}, // 抜ける手段は最優先
		})
	case m.panelSHA != "":
		hint = "j: job を選択  d: diff  p: PR  y: commit URL  Enter/h/q: 閉じる"
	}
	if m.fetching() {
		hint = m.spinner() + " CI 状態を取得中...  " + hint
	}
	if m.ghErr != nil {
		hint = "🚨 " + firstLine(m.ghErr.Warning()) + "  " + hint
	}
	return m.hintLineText(hint)
}

// hintLineText は最下行の hint を塗って幅に収める (前置の有無で分かれる出口を 1 本にする)。
func (m *browseModel) hintLineText(hint string) string {
	painted := paint(hint, ansiDim, m.colored)
	if m.frameActive() {
		// hint は板の外 (最下行) だが、左余白 1 桁を付けて板の左端 (┌) と縦に揃える。素朴に
		// " " を前置すると、既定 hint が clip 後に m.width ちょうどになり実効幅 m.width+1 で
		// 折り返し崩壊するため、clip 幅を左右余白ぶん (2) 差し引く (板の footprint と同じ span)。
		return " " + clipToWidth(painted, m.hintWidth())
	}
	return clipToWidth(painted, m.hintWidth())
}

// hintWidth は hint 行に使える桁数。🚨 hintLineText の clip 幅と、hint を組む側 (statusView.hint)
// が使う予算はこの 1 か所から取る。2 か所に式を書くと、片方だけ余白を変えた瞬間に「収まる
// つもりで組んだ hint が黙って切られる」形でずれる (issue 155 はその状態だった)。
func (m *browseModel) hintWidth() int {
	if m.frameActive() {
		return max(m.width-2, 1)
	}
	return m.width
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
	// Alt Screen は View が宣言する (v2 では program オプションではない)。
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return m, err
	}
	if fm, ok := final.(*browseModel); ok {
		return fm, nil
	}
	return m, nil
}
