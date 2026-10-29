package main

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/jiikko/dotfiles/src/tuikit/anim"
	"github.com/jiikko/dotfiles/src/tuikit/termcaps"
	"github.com/jiikko/dotfiles/src/tuikit/termcolor"
	"github.com/jiikko/dotfiles/src/tuikit/toast"

	"subproc"
	"treefiler/termsync"

	"glogx/internal/commitview"
	"glogx/internal/extcmd"
	"glogx/internal/filerview"
	"glogx/internal/gitdata"
	"glogx/internal/issuesview"
	"glogx/internal/lanview"
	"glogx/internal/ratelimitview"
	"glogx/internal/statusview"
	"glogx/internal/term"
	"glogx/internal/usageview"

	"glogx/internal/app"
	"glogx/internal/doctorview"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
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
	spinnerInterval = 80 * time.Millisecond // スピナー等の通常 tick (12.5fps。CPU 節約)
	scrollInterval  = 16 * time.Millisecond // scroll glide 中の高 FPS tick (~60fps。滑らかさ優先)
	// zoomInterval は開閉演出中の tick (~125fps)。他より速いのは、この演出だけが「短い所要
	// (appZoomDuration 160ms) を壁時計で刻む」ためで、周期がそのままフレーム数になる:
	// 12.5fps だと中間フレームが 2 枚しか出ず (4行 → 30行 → 実画面)、演出でなく点滅に見える。
	// 🚨 上げても遅くはならない: 進捗は壁時計なので、端末が追いつかなければフレームが間引かれる
	// だけで所要は変わらない (フレーム数で進める glide とはここが違う)。
	zoomInterval = 8 * time.Millisecond
	// maxPanelJobs は job パネルに一度に表示する行数。超過分はパネル内でスクロールする。
	maxPanelJobs = 10
)

// browseModel の状態 (フィールド) はこの型 1 つに置き、メソッドは責務ごとの browse_*.go に分けてある
// (update は「どのメッセージを誰が受けるか」の表だけにし、3 文以上の処理は on<Msg> として受け手のファイルに置く)。
// 状態を責務ごとの型へ割らないのは、CI の取得とパネルが panelSHA / mergeCIBatch / settleAwaitCI で同じ状態を同じ順に
// 触るため (割ると同じ順序の保証が 2 つの型にまたがる。CI の取得とパネルが同じ browse_ci.go にあるのも同じ理由)。
// 分けた効果の見積もり (2026-10-29、直近 90 日に tui.go を触った 181 commit の git log -p -U0 の hunk の関数見出しで数えた):
// tui.go に触らずに済むのは 39% (update の hunk を tui.go に残ると数えた下限) 〜 67% (on<Msg> へ出た case の中の変更も外へ出ると
// 数えた上限)。残る変更の多くはフィールドの追加・Init・newBrowseModel。update の case を出さずにメソッドだけを分けると 2〜3 割に留まる。
type browseModel struct {
	commits        []gitdata.Commit
	statuses       map[string]gitdata.CIState // 表示用 (キャッシュ + 取得結果のマージ)
	fetched        map[string]gitdata.CIState // API から取得した分 (終了後のキャッシュ保存用)
	details        map[string][]gitdata.CheckDetail
	detailsLoading map[string]bool
	// detailsWaiting は detailsLoading のうち「**Cmd を出さずに**一括取得の結果を待っている」
	// SHA。fetchPanelDetails の待機分岐だけがここへ入る。
	//
	// 🚨 不変条件は「detailsLoading ⊆ toFetch」ではなく「**札には必ず、その SHA を名指して
	// 届く live Cmd がある**」。実取得分岐の札は自前の detailMsg が降ろすので、世代交代で
	// 落としてはいけない (落とすと取得中なのに「(CI job 情報なし)」を出し、開き直すと
	// 同一 SHA へ 2 本目の GraphQL が飛ぶ)。区別せずキー集合だけを見ると、この 2 種類を
	// 取り違える。
	detailsWaiting map[string]bool
	// toFetch は一括取得の「まだ結果が来ていない」SHA。チャンクが着弾するたびに縮む
	// (fetching() も pendingFetches==0 で下りる)。パネルの「進行中の一括取得を待つ」ガードが
	// これを見るので、既に着弾したチャンクの SHA で待たされないようにするために縮める必要がある。
	toFetch []string
	// pendingFetches は未着の一括取得チャンク数。**取得中かどうかの唯一の出典**で、
	// fetching() がここから導出する (fetching bool を並置して 4 箇所で手書き同期すると、
	// 1 箇所書き忘れるとスピナーが回りっぱなし / パネルが待たずに
	// 二重取得する形で silent に狂い、build もテストも通る)。
	// (スピナー・invalidate gate・パネルガードが fetching を見るので、最後のチャンクが
	// 着弾するまで下ろせない)。
	pendingFetches int
	// fetchEpoch は一括取得の世代。startCIFetch が張り替えるたびに進める。pull/push の
	// 再取得が toFetch/pendingFetches を新世代へ上書きした後、旧世代のチャンクが届くと
	// カウンタを誤減算し (スピナー早期消灯)、取得中 SHA を toFetch から誤って間引く
	// (同一 SHA 並行リクエストのガード無効化) ため、世代不一致の ciResultMsg は捨てる
	fetchEpoch int
	repo       gitdata.Repo
	hasRepo    bool
	ghErr      *gitdata.GHError
	decor      *gitdata.DecorColors
	oneline    bool
	colored    bool
	// showFrame は最外周フレーム (板 + ドロップシャドウ) 描画の有効フラグ。起動時固定
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
	// job 詳細ポップアップ (annotations / ログ tail) の pager 状態と描画は commitview.JobDetailOverlay
	// (internal/commitview/job_detail_overlay.go) に切り出す。panel-frame (panelSHA/panelCursor/panelGrace) と ETA・
	// CI 取得は details/statuses/commits と構造的に結合するため browseModel に残す (詳細は同ファイル)。
	// cache キー (detailKey) はパネルのカーソル座標から借りる (identity 非所有) ので呼び出し側で注入。
	detailOv commitview.JobDetailOverlay
	prCache  map[string]*gitdata.PRRef // sha → 紐づく PR (nil 格納 = 確認済みで PR なし)
	prBusy   map[string]bool           // PR 取得中の sha
	// PR 状態ポップアップ (P キー) の状態と描画は commitview.PrStatusOverlay (internal/commitview/pr_status_overlay.go) に
	// 切り出し、ここは 1 フィールドだけ持つ。CI 行の整形はコミット状態を知る browseModel 側。
	prStatusOv commitview.PrStatusOverlay
	// diff ポップアップ (d キー) の状態と描画は commitview.DiffOverlay (internal/commitview/diff_overlay.go) に切り出し、
	// ここは 1 フィールドだけ持つ。ターゲット選定・非同期取得・URL コピーは境界をまたぐため
	// openDiff / handleDiffKey に薄く残す。
	diffOv commitview.DiffOverlay
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
	// 状態と描画は usageview.UsageOverlay (internal/usageview/usage_overlay.go) に切り出し、ここは 1 フィールドだけ持つ。
	usageOv usageview.UsageOverlay

	// コミット一覧のスクロール glide (表示 offset を論理 offset へ滑らせる)。実体は
	// internal/term/scroll_glide.go の共有型で、diff pager / issues viewer も同じ型を持つ (手触りを揃える)。
	glide anim.ScrollGlide

	// バックグラウンド再ビルド (bin/lib/go_autobuild.zsh --async) の決着監視。zero value は
	// 「監視しない」で、shim が GO_AUTOBUILD_PENDING を立てた起動でだけ動く (autobuild.go)。
	autobuild autobuildWatch

	// 別プロセス (別ターミナルの commit / rebase・Claude Code・pull) による git log の変化の
	// 見張り。状態と判定は gitLogWatch 型 (gitlog_watch.go) に切り出し、反映は reloadLog を使う。
	logWatch gitLogWatch

	// issues viewer (i キーで開く全画面の issue ブラウザ)。状態と描画は issuesview.IssuesView
	// (internal/issuesview/issues_view.go) に切り出し、ここは 1 フィールドだけ持つ。読む規約の一次情報は
	// docs/issues-viewer-spec.md。
	issuesOv issuesview.IssuesView
	// ratelimit ダッシュボード (R キーで開く全画面の残量ビュー)。usage オーバーレイと同じ
	// Snapshot を枠ごとのアナログ盤にして描く (取得経路は usageOv のものを共用。
	// ratelimit_dashboard.go 冒頭)。
	rlDash ratelimitview.RatelimitDash
	// filerV は F の全画面 treefiler (filer_view.go)。画面の部品は src/treefiler の filer パッケージ
	filerV filerview.FilerView
	// lanV は L の全画面 mac-lan-manager (lan_view.go)。画面の部品は src/mac-lan-manager の lanmgr パッケージ
	lanV lanview.LanView
	// doctorOv は D の全画面 doctor (doctor_view.go)。rlDash と同じ薄い器で、開くと走査を始める
	doctorOv doctorview.DoctorView
	// status viewer (s キーで開く全画面の作業ツリービュー)。stage / unstage / 変更を捨てる を
	// 行う write 側の画面で、状態と描画は statusview.StatusView (internal/statusview/status_view.go) に切り出す。
	// 読み書きの規約の一次情報は docs/status-viewer-spec.md。
	statusOv statusview.StatusView
	// restartPending は「裏ビルドが完成したので再起動を提案したい」= 保留中の印。
	// 🚨 これは「出したい」であって「出ている」ではない。実際に出すかは restartPromptVisible()
	// が決める (中断できない処理の最中は出さない)。表示と入力の両方が同じ述語を見る契約。
	// restartRequested は「終了後に新しいバイナリで自分を置き換える」印 (main.go が exec する)。
	restartPending   bool
	restartRequested bool
	// lastKey / lastKeyAt はキーリピート (押しっぱなし) の判定用 (swallowKeyRepeat)。
	lastKey   string
	lastKeyAt time.Time
	// zoom は画面全体の開閉演出 (zoom.go)。実画面から始まる (newAppZoom)。Init を通らない
	// 経路 (テスト・静的出力) も実画面がそのまま出る (開く演出は Init からも呼んでいない)。
	zoom appZoom

	// toast は右下に数秒だけ出す結果フィードバック (push/pull 完了)。自動消滅 (github.com/jiikko/dotfiles/src/tuikit/toast)。
	toast toast.Stack

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
	// deferredStartup は起動時の CI 取得が終わるまで預ける検査 (usage・バージョン・ログイン)。
	// どれも node の claude / codex を起こすので、CI 取得と開いた直後の描画から CPU を奪わないよう後ろへ回す。
	// 放すのは Update の 1 か所だけ (releaseStartupChecks)
	deferredStartup tea.Cmd
	// keyed は起動の後にキーが押されたか。照合を待っている issues の復元 (issuesRestoreMsg) は、着いた時点で押されていたら捨てる:
	// ユーザーが操作を始めた後に画面を割り込ませない (F を開いて閉じた後の一覧に、遅れて issues が開く形)
	keyed bool

	// lines() のメモ化。行リストの再構築は O(出力全行数) で、-p の巨大 patch では
	// キー 1 打ごとに数万行を組み直すことになるためキャッシュする。行内容を変えうる
	// 更新 (statuses/details のマージ・スピナーフレーム・幅変更) だけが無効化する。
	// カーソル移動・パネル開閉は View の窓側で重ねるだけなので無効化不要
	linesCache []Line
	linesValid bool
}

func newBrowseModel(commits []gitdata.Commit, statuses map[string]gitdata.CIState, toFetch []string, repo gitdata.Repo, hasRepo bool, opts *Options, colored bool, width, height int) *browseModel {
	// 上限超過分は問い合わせず StateUnknown 表示のまま (capFetchSHAs のポリシー)。
	// toFetch に残すと「取得中」のまま永遠に解決しない
	toFetch, chunks := gitdata.PlanCIChunks(toFetch)
	ctx, cancel := context.WithTimeout(context.Background(), app.FetchTimeout)
	m := &browseModel{
		toast:          toast.Stack{Shadow: term.AnsiShadowFg}, // 落ち影は他の板と同じ近黒 (term.BuildPanelBoxImpl と同じ)
		commits:        commits,
		statuses:       statuses,
		fetched:        map[string]gitdata.CIState{},
		details:        map[string][]gitdata.CheckDetail{},
		detailsLoading: map[string]bool{},
		detailsWaiting: map[string]bool{},
		detailOv:       commitview.NewJobDetailOverlay(),
		prCache:        map[string]*gitdata.PRRef{},
		prBusy:         map[string]bool{},
		prStatusOv:     commitview.NewPRStatusOverlay(),
		diffOv:         commitview.NewDiffOverlay(),
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
		usageOv:        usageview.NewUsageOverlay(),
		issuesOv:       issuesview.NewIssuesView(),
		zoom:           newAppZoom(),
		statusOv:       statusview.NewStatusView(),
		autobuild:      newAutobuildWatch(selfExePath(), os.Getenv(autobuildPendingEnv) != "", app.TimeNow()),
	}
	// 🚨 ここは「取得中か」ではなく **「これから取得を始めるか」** の判定なので、
	// fetching() (= pendingFetches > 0) を使ってはいけない。pendingFetches はこの下の
	// チャンク分割で初めて入る (fetching フィールドを消したときの落とし穴)。
	if len(toFetch) > 0 {
		// 起動時の一括取得は m.cancel と ctx を共有する意図的例外 (q 中断で走行中の
		// GraphQL を止めるため)。チャンクへ割って並列に投げるので、画面に映っている先頭
		// コミットの CI が最初に埋まる (startCIFetch の分割方針)。
		//
		// 🚨 チャンク closure で cancel() を defer しないこと: 共有 ctx なので最初に終わった
		// チャンクが残りを巻き添えキャンセルしてしまう。timer の解放は m.cancel
		// (main.go の defer browse.cancel() と quit 経路) が担う。
		m.pendingFetches = len(chunks)
		epoch := m.fetchEpoch // closure は別 goroutine で走るので値で捕捉 (m の直読みは race)
		cmds := make([]tea.Cmd, 0, len(chunks))
		for _, chunk := range chunks {
			cmds = append(cmds, func() tea.Msg {
				batch, ghErr := gitdata.FetchCIChunk(ctx, gitdata.ExecRunner, repo, chunk)
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
	// CI の取得中は usage の取得を預ける (下の deferredStartup) が、ディスクキャッシュを読むだけなら fork しないので
	// 先に済ませて数字を出す。預けると CI の取得の間ずっと右上がスピナーになる
	var u tea.Cmd
	if !m.fetching() || !m.usageOv.ShowCached(app.TimeNow()) {
		// 🚨 fetchCmd は呼んだ時点で inFlight を立てるので、預けている間の usageRefreshMsg は重ねて取得しない
		u = m.usageOv.FetchCmd(true)
	}
	// usage を起動時に取得するため tick を常に起動する (取得中スピナーを回す。取得完了で
	// spinnerActive が false になり tick は自然に止まる)。CI fetch の有無に依らず起動する。
	// usageRefreshTick で 1 分ごとのバックグラウンド再取得チェーンも起動する (ユーザー要望)。
	// Claude Code / codex の新バージョン検出 (claude_version.go / codex_version.go)。
	// どちらもバックグラウンド 1 回きりで、結果は *UpdateAvailableMsg (更新なし/失敗は
	// nil Msg で無音)。
	ver := tea.Batch(checkClaudeVersionCmd(), checkCodexVersionCmd())
	cliHealth := checkCLIHealthCmd()
	// doctor の起動時トースト: 前回のスキャン結果 (ファイル読みのみ、fork なし) が閾値を超えていれば
	// 文言で D へ誘導する。起動時に走査はしない。
	if text := doctorview.StartupToast(app.TimeNow()); text != "" {
		m.toast.ShowInfo(text)
	}
	// バックグラウンド再ビルドの監視 (autobuild.go)。shim が GO_AUTOBUILD_PENDING を
	// 立てていない通常起動では nil = tick が増えない。
	//
	// 「ビルド中」はここで即出す: 完成を待つと数十秒遅れ、その間ユーザーは自分が旧版を触って
	// いることを知らない (ユーザー要望 2026-07-31)。
	if res, notify, _ := m.autobuild.handle(autobuildRunning, app.TimeNow()); notify {
		text, ok := autobuildToast(res)
		m.toast.Show(text, ok)
	} else if autobuildStaleBinary(selfExePath()) {
		// ビルド中でもないのに失敗記録が残っている = 旧版に固定された状態。「ビルド中」と違って
		// 自然には解消しないので、コピー可能な警告 (w) として出す。
		//
		// ビルド中を優先する理由: 再挑戦が走っているなら、その決着 (成功/失敗) はこのセッションの
		// 監視が伝える。両方出すと 1 つの出来事に 2 枚のトーストが積まれる。
		text, _ := autobuildToast(autobuildStale)
		m.showWarning(text)
	}
	// 🚨 起動時の開く演出は出さない (ユーザー判断 2026-09-05)。popup で開くたびに
	// appZoomDuration 待たされる方が体感を損ねる、という判断。**終了の演出は残す**
	// (閉じる方は「押した結果が見える」ので待ちに感じにくい)。
	// 開く演出そのものの実装は zoom.go に残してある (戻すならこの行を復活させるだけ)。
	// m.zoom.start(app.TimeNow()) // 画面を中央から開く (zoom.go)
	ab := m.autobuild.tickCmd()
	restore := m.restoreScreensCmd(app.TimeNow())
	// 🚨 起動時にも追従チェーンを張る: ディスクキャッシュに pending が残っていると初回 fetch が
	// 走らず (m.fetching() == false)、ciResultMsg 起点の開始点をどれも踏まないまま
	// 「pending なのに追わない」状態になる (キャッシュの pending TTL 内に再起動した場合)。
	poll := m.ensureCIPoll()
	// 別プロセスによる git log の変化の見張り (gitlog_watch.go)。対象ディレクトリの解決は
	// git を叩くので非同期にし、保険のポーリングだけ先に張る (イベント待ちは解決後に張る)。
	logWatch := tea.Batch(gitLogWatchDirsCmd(), m.gitLogPollCmd())
	if m.fetching() {
		m.deferredStartup = tea.Batch(u, ver, cliHealth)
		return tea.Batch(m.fetch, prefix, ab, restore, poll, logWatch, m.maybeTick(), usageRefreshTick())
	}
	return tea.Batch(u, ver, cliHealth, prefix, ab, restore, poll, logWatch, m.maybeTick(), usageRefreshTick())
}

func (m *browseModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.update(msg)
	return model, tea.Batch(cmd, m.releaseStartupChecks())
}

func (m *browseModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.done {
		// 終了確定後に届く残メッセージは無視する (q での取得中断が
		// 「context canceled」警告として出るのを防ぐ)
		return m, nil
	}
	// doctor の非同期の結果 (走査・削除)。doctor が受け取って次の Cmd と知らせを返す
	if cmd, toast, ok := m.doctorOv.Receive(msg); ok {
		if toast != "" {
			m.toast.Show(toast, false)
		}
		return m, tea.Batch(cmd, m.maybeTick())
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.onWindowSize(msg)
	case filerview.FilerChangedMsg:
		return m.onFilerChanged(msg)
	case lanview.LanChangedMsg:
		return m.onLanChanged(msg)
	case tickMsg:
		return m.onTick(msg)
	case ciResultMsg:
		return m.onCIResult(msg)
	case detailMsg:
		return m.onDetail(msg)
	case basisMsg:
		return m.onBasis(msg)
	case commitview.JobDetailMsg:
		return m.onJobDetail(msg)
	case prMsg:
		return m.onPR(msg)
	case prStatusMsg:
		return m.onPRStatus(msg)
	case commitview.DiffMsg:
		return m.onDiff(msg)
	case prefixMsg:
		m.tmuxPrefix = msg.key
		return m, nil
	case usageview.UsageMsg:
		return m, m.usageOv.Handle(msg)
	case issuesview.IssuesWatchMsg:
		// viewer を開いている間だけ回る独立チェーン (issues_watch.go)。別プロセスの編集を
		// その場で反映する。🚨 maybeTick を束ねない: 反映は再スキャン (scanCmd) で、アニメは
		// 動かないため。フレーム tick を足すとこのチェーンの意図 (1s 周期) が崩れる。
		return m, m.issuesOv.HandleWatch(msg)
	case issuesview.IssuesScanMsg:
		return m.onIssuesScan(msg)
	case statusview.StatusLoadMsg:
		// git status の結果 (status viewer)。返り値はプレビューの取り直し予約 (内容が変わった
		// ときだけ)。🚨 maybeTick も束ねる: 取得中スピナーを回していた場合、結果到着でそれを
		// 下ろすフレームが要る (下ろさないと最後のスピナー姿で固まる)。
		return m, tea.Batch(m.statusOv.Receive(msg), m.maybeTick())
	case statusview.StatusPollMsg:
		// viewer を開いている間だけ回る自動更新チェーン (spec 5 節)。🚨 maybeTick を束ねない:
		// 反映は読み直しだけでアニメは動かないため (issuesWatchMsg と同じ理由)。
		return m, m.statusOv.ReceivePoll(msg)
	case statusview.StatusPreviewTickMsg:
		return m, tea.Batch(m.statusOv.ReceivePreviewTick(msg, m.colored), m.maybeTick())
	case statusview.StatusPreviewMsg:
		return m, tea.Batch(m.statusOv.ReceivePreview(msg), m.maybeTick())
	case issuesRestoreMsg:
		// 🚨 復元の repo 照合 (git fork) が返る前に type-ahead の s で status viewer が開いて
		// いたら復元を捨てる。restore は自分の shown しか見ないため、ここで弾かないと両 viewer
		// 同時 shown になり「見えている status」と「キーを受ける issues」が食い違う
		// (2026-08-06 に再現)
		// 🚨 ratelimit ダッシュボード (R) が先に開いていた場合も同じ理由で捨てる: 復元すると
		// 裏に issues viewer が開いた状態になり、ダッシュボードの i (横断) が toggle で
		// 「開く」ではなく「閉じる」に化ける。
		// 🚨 判定は activeFullScreen から導出する: 全画面ビューアを 1 枚足した
		// ときにここへ書き足すのを忘れると、復元で 2 枚同時 shown になる。
		if id := m.activeFullScreen(); m.keyed || (id != FullScreenNone && id != FullScreenIssues) {
			return m, m.maybeTick()
		}
		return m, tea.Batch(m.issuesOv.Restore(app.CurrentDir(), msg.screen), m.maybeTick())
	case claudeUpdateAvailableMsg:
		return m, m.showClaudeUpdate(msg.latest)
	case codexUpdateAvailableMsg:
		m.toast.Show("codex v"+msg.latest+" が公開されています (X で更新)", true)
		return m, m.maybeTick()
	case cliHealthMsg:
		for _, issue := range msg.issues {
			if text := cliHealthWarning(issue); text != "" {
				m.showWarning(text)
			}
		}
		return m, m.maybeTick()
	case autobuildMsg:
		return m.onAutobuild(msg)
	case autobuildSpawnMsg:
		return m.onAutobuildSpawn(msg)
	case toast.Msg:
		m.toast.StartLeaving(msg) // 静止明け: 退場アニメへ (世代一致時のみ)。maybeTick で tick 再開
		return m, m.maybeTick()
	case usageRefreshMsg:
		// バックグラウンドで /usage を再取得し、次回リフレッシュを予約する。取得中も snap は
		// 消さないので loading() は false のままスピナーに落ちず、表示は last-good を維持する
		// (handle の不変条件)。
		//
		// 表示中だけ取得する。「隠れていても最新値を用意しておく」ために表示/非表示に
		// 依らず回さない。それは「/usage は ~440ms でゼロコスト」という誤った前提の
		// 上にある (実測 2.0s wall / 1.8s CPU。440ms は JSON の duration_ms = 内部処理時間)。
		// オーバーレイは起動時グランスの後どのナビゲーションキーでも dismiss されるので、
		// 実際にはほぼ常に非表示 = 見えないもののために 60 秒ごと永続に node を起こすことになる。
		// 「即座に最新が見える」という元の意図は再表示 (U) 時の stale 判定で保つ。
		if !m.wantsUsageRefresh() {
			return m, usageRefreshTick() // チェーンは維持 (再表示後に周期取得が復活する)
		}
		return m, tea.Batch(m.usageOv.FetchCmd(false), usageRefreshTick())
	case gitLogDirsMsg:
		return m, m.handleGitLogDirs(msg)
	case gitLogProbeMsg:
		return m, m.handleGitLogProbe(msg)
	case gitLogFPMsg:
		return m, m.handleGitLogFP(msg)
	case gitLogReloadMsg:
		return m, m.handleGitLogReload(msg)
	case ciPollMsg:
		return m.onCIPoll(msg)
	case ciPollResultMsg:
		return m.onCIPollResult(msg)
	case pullMsg:
		return m.onPull(msg)
	case rerunMsg:
		return m.onRerun(msg)
	case updateBeginMsg:
		return m.onUpdateBegin(msg)
	case updateMsg:
		return m.onUpdate(msg)
	case pushMsg:
		return m.onPush(msg)
	case extcmd.OpenURLMsg:
		if msg.Err != nil {
			m.showWarning("ブラウザを開けませんでした: " + app.FirstLine(msg.Err.Error()))
		}
		return m, m.maybeTick()
	case extcmd.EditorClosedMsg:
		return m.onEditorClosed(msg)
	case tea.PasteMsg:
		return m.onPaste(msg)
	// KeyMsg (v2 では KeyPressMsg/KeyReleaseMsg を束ねる interface) ではなく押下だけを取る。
	// 離鍵イベントは KeyboardEnhancements を要求していないので届かないが、interface で受けると
	// 将来 enhancement を有効にした瞬間に 1 打が 2 回処理される。
	case tea.KeyPressMsg:
		return m.onKeyPress(msg)
	}
	return m, nil
}

func (m *browseModel) onWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.invalidateLines() // 幅で折り返し行数が変わる
	// resize 中の glide は破棄して即時にする (表示 offset が stale になるため)。一覧だけでなく
	// pager 側も止める: 幅で行数が変わり、glide の着地点が resize 前の行数基準で古くなる。
	m.glide.Stop()
	m.diffOv.StopGlides()
	m.issuesOv.StopGlides()
	m.statusOv.StopGlides()
	m.ensureCursorVisible()
	m.filerV.Resize(m.contentWidth(), m.pageSize())
	if m.filerV.Animating() {
		return m, m.maybeTick()
	}
	return m, nil
}

// RunBrowse は TUI を実行し、最終状態のモデルを返す。Alt Screen を使うため、
// 終了時に表示は消える (git log の pager と同じ。ユーザー要望 2026-07-17)。
func RunBrowse(m *browseModel) (*browseModel, error) {
	// Alt Screen は View が宣言する (v2 では program オプションではない)。
	var opts []tea.ProgramOption
	// tmux の中でも、外側の端末が対応していれば 24bit の色をそのまま書く (treefiler の F と mac-lan-manager の L の板が 24bit の色を描く。
	// 判定しないと 256 色に落ちて面の色が紺になる。理由は tuikit/termcolor の doc)。glogx 自身の色は 256 色・基本色なので変わらない
	// termcolor と termcaps が同じ問い合わせ (tmux の client_termfeatures) をするので、1 回の答えを使い回す
	// (tmux が固まっていると 1 回ごとに上限の 1 秒を待つ)
	type answer struct {
		out string
		err error
	}
	asked := map[string]answer{}
	run := func(ctx context.Context, name string, args ...string) (string, error) {
		key := strings.Join(append([]string{name}, args...), "\x00")
		if a, ok := asked[key]; ok {
			return a.out, a.err
		}
		out, err := subproc.CommandContext(ctx, name, args...).Output()
		asked[key] = answer{string(out), err}
		return string(out), err
	}
	if termcolor.TrueColorThroughTmux(os.Getenv("TMUX"), run) {
		opts = append(opts, tea.WithColorProfile(colorprofile.TrueColor))
	}
	// 外側の端末が同期出力を反映しないなら、同期出力を使わず、F の木の縦の動きを流さずに寄せ切る (tuikit/termcaps の doc)。
	// tmux の中で使うと tmux が外側へ書く量が約 5 倍になる (理由は treefiler/termsync)
	m.filerV.Unsynced = !termcaps.SyncedOutput(os.Getenv, run)
	opts = append(opts, termsync.ProgramOptions(m.filerV.Unsynced)...)
	p := tea.NewProgram(m, opts...)
	final, err := p.Run()
	if err != nil {
		return m, err
	}
	if fm, ok := final.(*browseModel); ok {
		return fm, nil
	}
	return m, nil
}
