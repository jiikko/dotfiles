package issuesview

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jiikko/dotfiles/src/tuikit/anim"
	"github.com/jiikko/dotfiles/src/tuikit/confirm"
	"github.com/jiikko/dotfiles/src/tuikit/gutter"
	"github.com/jiikko/dotfiles/src/tuikit/layout"
	"github.com/jiikko/dotfiles/src/tuikit/listnav"

	"glogx/issues"

	"glogx/internal/app"
	"glogx/internal/extcmd"
	"glogx/internal/term"

	tea "charm.land/bubbletea/v2"
)

// issues viewer (i キー) の全画面ビュー。
//
// 設計: browseModel を一切参照しない自己完結の状態機械にする。browseModel 側の結合は
// 「フィールド 1 つ + キー経路 1 行 + View 1 分岐 + hint 1 case」で済み、このビューだけを
// 単体テストで駆動できる (diffOverlay / usageOverlay と同じ方針)。
//
// 仕様 (探索範囲・状態の決め方・カテゴリタブ) の一次情報は docs/issues-viewer-spec.md。

// issuesScanMsg は issues の探索結果。
//
// error を持たないのは探索層が部分成功を返す設計だから: 読めなかったディレクトリは空として
// 扱い、viewer が表示すべき異常 (同名ファイルの二重化など) は warnings に載る。全体が失敗する
// ケースが無いので、失敗を表す経路も持たない。
type IssuesScanMsg struct {
	Root     string // repo root (貼り付け用の repo 相対パスに使う)
	Dirs     []string
	Issues   []*issues.Issue
	Warnings []string
	// fp は読んだ時点のファイル群の指紋 (issues_watch.go)。見張りの基準に使う。
	fp string
}

// displayRow は一覧の表示単位。Issue の集合 (rows) に親行を混ぜると、カーソル位置を
// issue の添字だと仮定する copy/move/open が親を誤って操作するため、表示専用の層を分ける。
type displayRowKind string

const (
	displayRowIssue displayRowKind = "issue"
	displayRowGroup displayRowKind = "group"
)

type displayRow struct {
	kind       displayRowKind
	issue      *issues.Issue
	groupKey   string
	groupName  string
	childCount int
	// childDone は子のうち done の件数 (親行の進捗表示 `(5 ✓2)` の右側)。0 なら出さないので、
	// done が 1 件も無い group の見た目は従来の `(N)` のまま (issue 291)。
	childDone int
	// inGroup は展開した group 親行の直下に並ぶ子 issue。一覧では groupChildIndent ぶん右へ寄せて
	// 親との所属を見せる (2026-09-05 のユーザー要望: 親と同じ桁に並ぶと開いたときに所属が読めない)。
	inGroup bool
	// groupHead は epic 自身の親 issue (形は issues.IsGroupParent) を group の親行として描く行。合成の
	// group 行 (displayRowGroup) と違い実体は issue なので、開く・コピー・n はそのまま効く
	// (親 issue が group 行に吸われて触れなくなるのを避ける。2026-09-05 ユーザー要望)。
	groupHead bool
}

// groupChildIndent は group 配下の子 issue 行を右へ寄せる幅。半角空白だけで組む
// (全角と混ぜると桁が揃って見えない。no-mixed-width-columns-in-terminal-ui.md)。
const groupChildIndent = "  "

type displayUnit struct {
	row       displayRow
	maxNumber int
	hasNumber bool
	key       string
}

// issuesView は一覧 (タブ + リスト) と本文 pager の 2 モードを持つ全画面ビュー。
type IssuesView struct {
	Shown    bool
	Loaded   bool // 一度スキャンを完了したか (取り直し中に前回の結果を出してよいかの判定)
	Scanning bool // スキャン中 (スピナーを回す。二重発行の防止も兼ねる)
	// rescanPending は「飛行中のスキャンが終わったら、もう 1 回取り直す」予約。
	// 🚨 single-flight (scanCmd) は連打でゴルーチンを積まないための仕組みだが、自分がファイルを
	// 動かした後 (n の next/ 移動) の取り直しまで落とすと、実ファイルは動いたのに一覧が旧位置を
	// 出し続ける。しかも飛行中のスキャンは移動より前に始まっているので、その結果が届いても
	// 移動は映らない。要求を 1 つに畳んで receive の出口で張り直すことで、貫通の穴を増やさずに
	// 「最後の要求は必ず反映される」を満たす。
	RescanPending bool

	Cwd      string // スキャンの起点 (再読込で使い回す)
	Root     string // repo root
	Dirs     []string
	All      []*issues.Issue
	warnings []string
	// notice は直近の操作結果 (コピー成功・読み込み失敗など)。app.NoticeOK は成功か。
	// browseModel はこれを取り出してトーストにする (takeNotice)。🚨 viewer 単体でも駆動できる
	// 契約 (このファイル冒頭) を保つため、ここではトーストを知らず「結果」だけを置く。
	Notice     string
	noticeKind app.NoticeKind

	// tabsCanon は issues.Tabs が返す正規順序 (done 込みの件数降順 → 名前昇順、other は末尾)。
	// tabs は「そこから 0 件を右へ寄せた表示・巡回順」。🚨 並べ替えを tabs へ破壊的に適用すると
	// 順序が履歴依存になる (前回の並びを基準に再度寄せるため、全件表示に戻しても正規順序へ戻らず
	// 「12, 6, 2, 6, 4」のような不規則な並びが残る。実測 2026-07-31)。正規順序を別に保ち、
	// 表示順は (正規順序, 現在の件数) の純粋な写像として毎回作り直す。
	tabsCanon []issues.Tab
	tabs      []issues.Tab // All は含まない (表示時に先頭へ足す)
	tabIdx    int          // 0 = All、1.. = tabs[tabIdx-1]
	// filter は「どの状態まで見せるか」(a キーで巡回)。zero value = open のみが既定
	// (issues.FilterOpen)。pending / done を既定で伏せる理由は issues.StatusFilter の doc。
	Filter issues.StatusFilter
	// numFilter は番号のインクリメンタルフィルタ (/)。zero value = 絞り込みなし。効いている
	// あいだはタブと filter の両方を無視した全 issue が対象になる (issuesNumberFilter の doc)。
	NumFilter issuesNumberFilter
	// チップに出す件数。issues.Tab.Count は done を含む全件なので表示には使わない (理由は
	// refresh)。tabCount は tabs と同じ並び、allCount は All チップ用。
	tabCount  []int
	allCount  int
	nextCount int // 疑似カテゴリ [next] のチップに出す件数

	Rows []*issues.Issue // 現在のタブ・フィルタの表示対象 (親行を含めない子 issue 集合)
	// displayRows は rows から導出する表示単位。親行はここにだけ存在し、rows / tabCount /
	// allCount は従来どおり子 issue 数を持つ。
	displayRows []displayRow
	// rowsGen / displayRowsGen は rows と displayRows の同期を O(1) で判定するための世代。
	// 🚨 rows を書き換えるのは setRows だけ (issue 268)。以前は displayRows の元になった
	// rows のコピーを持って**毎回全件突き合わせ**ており、rowLine / selection /
	// isIssueDisplayRow が可視行ごとにそれを呼ぶため 1 フレームが
	// 「可視行数 × 全 issue 件数」で走っていた (2000 件で 62.4µs / 選択 20 行なら 439µs)。
	// 直接代入を禁じる規律は issues_rows_setter_test.go がソース走査で強制する。
	rowsGen        int
	displayRowsGen int
	// groups は epic group の展開状態 (手動 / 番号フィルタの一時展開 / フィルタ中の明示的な畳み)。
	// 3 つの整合は groupExpansion が持つ (issues_group_expansion.go)
	groups groupExpansion
	Cursor int
	// 複数選択 (shift+↑/↓)。範囲は錨 (markAt) と cursor から毎回導出する。🚨 選択集合を別に
	// 持たない: 行集合はフィルタ・タブ・再スキャンで入れ替わるので、集合で持つと実体を失った
	// 選択が残り「見えていない行がコピーされる」。錨だけなら行集合を作り直す refresh で畳める。
	Marked bool
	MarkAt int
	// offset は窓の先頭行。🚨 一覧に glide (スクロールアニメ) は載せない: 半ページ移動は
	// cursor と窓を同時に動かし、windowOffset が「カーソルを含む最小の窓」を導出するので
	// カーソルは必ず窓の端に来る = 窓を 1 行でも遅らせるとカーソルが画面から出る。遅らせる余地が
	// 幾何的にゼロで、載せても瞬時に着地点へ張り付くだけだった (実測 issue 031)。窓が動かない
	// ケース (カーソルが窓の途中) では glide 自体が始まらないので、条件が排他になっている。
	offset int

	// 本文 pager (open != nil のとき本文モード)
	Open *issues.Issue
	Body *issues.Body
	// urlPick は本文中 URL のピッカー (u)。閉じているときは zero value。
	UrlPick urlPicker
	// linkJump は本文中のファイルパスのジャンプモード (Tab。issues_linkjump.go)。zero value = モード外。
	LinkJump linkJump
	// docStack はジャンプ先の .md を本文 pager で開いたときに下へ積んだ本文 (issue → doc → doc)。
	// 空 = issue の本文だけ。🚨 空でないとき v.open は issue ではなく doc (一覧に無いパス) なので、
	// 一覧と照合する処理 (rebindOpen・画面の記憶) は底の issue (rootOpen) を使う。
	docStack []bodyFrame
	// docLine は開いた doc で最初に見せたいソース行 (`x.md#L12`)。描画で行が決まってから 1 度だけ使う。
	docLine int
	// linkRepos は linkReposRoot (= その時の root) と同じ repo の全 checkout (jumpRepos が遅延で引く)
	linkRepos     []string
	linkReposRoot string
	// markNext は「次にやる」の目印を付ける確認 (n)。🚨 実ファイルを動かす唯一の操作なので、
	// 他のキーと違って必ず確認を挟む (glogx の push/pull と同じ作法)。zero value = 確認なし。
	MarkNext IssuesMarkConfirm
	// drawer は本文を左から開く引き出しの演出状態 (issues_drawer.go)。閉じる演出のあいだは
	// open/body を生かしたまま逆再生するため、破棄は settleDrawer が担う。
	Drawer    issuesDrawer
	BodyPager listnav.Pager // 本文のスクロール (論理 offset = 着地点 + 半ページの glide)

	// curGlide は一覧の半ページ移動でカーソルを滑らせる演出 (internal/term/scroll_glide.go)。論理カーソルは
	// 即着地するので、描画だけが遅れる。
	CurGlide anim.CursorGlide

	// 開くときのスライドイン演出の開始時刻 (ゼロ値 = 演出なし)。フレーム数ではなく壁時計で
	// 進めるのは、tick 周期が変わっても所要時間が変わらないようにするため (push 演出の
	// pushSlides と同じ方式)。演出中の tick 周期は tickInterval が上げる (slideAnimating)。
	// 閉じる演出 (closing) でも同じ時計を使う — 開く演出の逆再生なので所要も同じ。
	AnimStart time.Time
	// wantCross は横断キー (app.CrossTarget) で「別の全画面へ切り替えたい」の一度きりの信号 (browseModel が
	// takeWantCross で取り出す。閉じ→開きの連携は viewer 単体では完結しないため)。app.FullScreenNone = 無し
	wantCross app.FullScreenID
	// wantQuit は「q/esc で glogx ごと終了したい」の一度きりの信号 (同上。quit は browseModel の
	// 仕事で、viewer は tea.Quit を出さない)。
	wantQuit bool
	// closing は閉じる演出 (開く演出の逆再生) の途中か (ユーザー要望 2026-08-01)。
	//
	// 🚨 演出のあいだ shown を false にしない: 逆再生で画面が見えている必要がある。実際の
	// 片付け (本文の破棄・watcher の停止) は finishClose に一本化する。片付けを二箇所に書くと、
	// 演出の着地とキーによる即着地のどちらかが stopWatch を通らず watcher が二重に走る。
	Closing bool
	// closeAnimOff は閉じる演出を出さない (テスト用)。🚨 zero value = 演出あり: 本番の既定を
	// 「演出する」に置く。テストは「close したら即座に畳まれている」前提で書かれており、演出を
	// 挟むと全てが 1 拍待ちになって読めなくなる (アプリ開閉演出の appZoom.off と同じ作法)。
	// 演出そのものは issues_close_anim_test.go が明示的に on にして検査する。
	CloseAnimOff bool

	// watch は開いている間だけ回す「別プロセスの編集」の見張り (issues_watch.go)。
	Watch issuesWatch

	// pending は前回終了時の画面の復元予約 (issues_state.go)。スキャン結果が要るので、
	// 適用は最初の receive まで待つ (applyScreen が 1 度だけ消費する)。
	pending *IssuesScreen
	// move の直後に再スキャン結果へ張り替えるパス。移動前の stale な Path を receive が
	// そのまま再利用すると、カーソル/選択が別行へ滑る。
	pendingCursorPath string
	pendingMarkPath   string
	// pendingMoveStale は、移動時点ですでにスキャンが飛行中だったため、次の receive が
	// 移動前の stale 結果になりうる印。1 回だけ stale を許し、その次の receive では宛先が
	// 消えていても pending を捨てる。
	pendingMoveStale bool
}

const (
	// issuesAnimDuration は開く演出の所要時間 (最後の行が着地するまで)。当初 700ms →
	// 350ms (2026-08-13) → 175ms (2026-09-05)。いずれもユーザー要望で一律 2 倍速にしたもので、
	// 閉じ・引き出し・status viewer・zoom・toast・glide も速度関係を保ったまま同じ倍率で縮めている。
	issuesAnimDuration = 175 * time.Millisecond
	// issuesCloseDuration は閉じる演出の所要時間 (板が画面外へ抜け切るまで)。開くより速いのは、
	// 開くときは中身を読み始められる一方、閉じるときは「もう用が済んだ画面」を見せ続けるため
	// (引き出しの issuesDrawerDuration と同じ値・同じ理由)。🚨 畳む時刻でもある: この時間が
	// 板の実際の滞在時間より長いと、抜けた後の空舞台を見せてから git log へ戻ることになる。
	IssuesCloseDuration = 112 * time.Millisecond
	// issuesAnimStagger は開く演出で行ごとに開始をずらす割合。0 なら全行同時に動いて「板が 1 枚
	// 滑り込む」見え方、大きいほど「上から順に流れ込む」見え方になる。閉じる演出では使わない
	// (tuikit layout.RowOffsetRatio の doc)。
	IssuesAnimStagger = 0.35
)

func NewIssuesView() IssuesView { return IssuesView{} }

// visible は viewer を表示中か。
func (v *IssuesView) Visible() bool { return v.Shown }

// loading はスキャン中か (スピナー tick を回し続ける判定用)。
func (v *IssuesView) Loading() bool { return v.Scanning }

// toggle は viewer の開閉。開くたびにスキャンし直す tea.Cmd を返す。
//
// 「初回だけ」にしていた頃は N (次に採番すべき番号) が初回スキャン時点の最大番号 + 1 を返し
// 続けていた。番号は viewer の外でも増える (別セッション・エディタ・他ブランチからの merge) ので、
// 番号の鮮度がユーザーの「r を押したかどうか」の記憶に依存し、番号の再利用を招く (他 repo で
// 実際に 37 件発生している事故)。スキャンは非同期かつ実測 2.5〜13.6ms なので、開くたびに
// 取り直しても体感は変わらない: 取得中も前回の結果を出したまま差し替わる (emptyMessage)。
func (v *IssuesView) Toggle(cwd string) tea.Cmd {
	if v.Closing {
		v.FinishClose() // 閉じる演出の途中の i は「閉じ切ってから開き直す」(watcher を二重に張らない)
	} else if v.Shown {
		v.Close()
		return nil
	}
	v.Shown = true
	// 右から左へ流し込む演出を開始 (lines が窓を変形する)。🚨 時計は app.TimeNow で取る:
	// 引き出し (drawer) と同じ差し替え点に揃えないと、この 1 つのビューの中で
	// 「止められる時計」と「止められない時計」が混在する。
	v.AnimStart = app.TimeNow()
	return tea.Batch(v.ScanCmd(cwd), v.watchCmd())
}

// restore は前回終了時の画面を復元しながら viewer を開く (起動時。issues_state.go)。
// 適用はスキャン結果が届く receive まで待つので、ここでは予約を置いてスキャンを始めるだけ。
//
// 🚨 開く演出は出さない: 復元は「閉じたところから再開」なので、起動のたびに issuesAnimDuration 待たされる
// のは筋が違う (ユーザー選定 2026-07-31)。
// 🚨 既に開いているなら何もしない: 復元の git fork とスキャンの間に i が押された場合で、
// 上書きするとユーザーの操作を奪う。
func (v *IssuesView) Restore(cwd string, s IssuesScreen) tea.Cmd {
	if v.Shown {
		return nil
	}
	v.Shown = true
	v.AnimStart = time.Time{}
	v.pending = &s
	return tea.Batch(v.ScanCmd(cwd), v.watchCmd())
}

// screen は今の画面を保存用に書き出す (ok=false = 覚えるものが無い)。
//
// viewer を出していないときに false を返すのが「git log 一覧から終了したら記憶しない」の実体
// (呼び出し側はそのとき記憶を消す)。スキャン前 (root 未確定) も覚えない: 照合キーが無い記憶は
// 復元時に別 repo で当たってしまう。
func (v *IssuesView) Screen(now time.Time) (IssuesScreen, bool) {
	// 🚨 閉じる演出の途中 = ユーザーは既に閉じている。shown はまだ true だが覚えない
	// (覚えると「q で閉じてすぐ終了した」次の起動で、閉じたはずの viewer が蘇る)。
	if !v.Shown || v.Closing || v.Root == "" {
		return IssuesScreen{}, false
	}
	// doc を開いていたら底の issue とその位置を覚える (doc は一覧に無いので、復元の照合で外れる)
	open, bodyOff := issuePath(v.rootOpen()), v.BodyPager.Offset
	if len(v.docStack) > 0 {
		bodyOff = v.docStack[0].offset
	}
	if v.Drawer.Phase() == anim.Closing {
		open = "" // 閉じる演出の途中 = ユーザーは既に閉じている。開いた状態で復元しない
	}
	return IssuesScreen{
		Root:        v.Root,
		SavedAt:     now,
		Tab:         v.currentTab(),
		Filter:      v.Filter.String(),
		Cursor:      issuePath(v.current()),
		Open:        open,
		BodyOff:     bodyOff,
		Groups:      v.groups.saved(),
		CursorGroup: v.currentGroupKey(),
	}, true
}

// applyScreen は復元予約を今のスキャン結果へ当てる。
//
// 消えた issue (rename / 状態ディレクトリへ移動) には黙って別物を当てない: カーソルは
// anchorCursorInternal が当たらなければ先頭のまま、本文はパスが見つからなければ一覧のままにする。
func (v *IssuesView) applyScreen(s IssuesScreen) {
	v.groups.restore(s.Groups)
	for _, iss := range v.All {
		key, ok := iss.EpicGroupKey()
		if (iss.Path != s.Cursor && iss.Path != s.Open) || !ok {
			continue
		}
		v.groups.reveal(key) // カーソル・本文の居場所が畳んだ group の中に隠れないように
	}
	v.Filter, _ = issues.ParseStatusFilter(s.Filter) // 未知の名前は既定へ (loadIssuesScreen で正規化済み)
	v.Refresh()                                      // フィルタを反映してタブの並びと件数を作る (tabIdx を引くのに要る)
	v.tabIdx = tabIndexOf(v.tabs, s.Tab)
	v.Refresh() // 選んだタブで行集合を作り直す
	v.pruneExpandedGroups()
	// 窓 (offset) はここで動かさない: 描画が windowOffset でカーソルを含む位置へ収束させる
	// (offset を状態でなく導出値として扱う規律。windowOffset の doc)
	if s.CursorGroup != "" {
		v.anchorGroupInternal(s.CursorGroup)
	} else {
		v.anchorCursorInternal(s.Cursor)
	}
	if s.Open == "" {
		return
	}
	for _, iss := range v.All {
		if iss.Path != s.Open {
			continue
		}
		if v.OpenIssue(iss) {
			v.Drawer.Finish()              // 演出は出さず開き切った状態から始める
			v.BodyPager.Offset = s.BodyOff // 行数を超えていれば bodyLines が収束させる
		}
		return
	}
}

// close は viewer を閉じる (スキャン結果は保持する。再表示は前回の結果を出しながら取り直す)。
//
// 閉じる演出 (開く演出の逆再生) を挟んでから片付ける (ユーザー要望 2026-08-01)。ここで画面を
// 落とさないのは、逆再生のあいだ一覧が見えている必要があるため (引き出しの startClose と同じ)。
func (v *IssuesView) Close() {
	if !v.Shown || v.Closing {
		return
	}
	v.Closing = true
	// 閉じたらカーソルの滑走を残さない (本文 pager の glide と同じ理由: 再表示の一瞬だけ古い位置から滑るのを
	// 防ぐ)。close は toggle 経由だと handleKey を通らないので finishAnim が効かない。
	v.CurGlide.Stop()
	v.AnimStart = app.TimeNow()
	if v.CloseAnimOff {
		v.FinishClose() // 演出なしの設定では同じ出口を即座に通す (片付けの経路を分けない)
	}
}

// settleClose は閉じる演出が着地していれば片付ける (browseModel の tick から毎拍呼ばれる)。
func (v *IssuesView) SettleClose() {
	if !v.Closing || anim.Elapsed(v.AnimStart, app.TimeNow(), IssuesCloseDuration) < 1 {
		return
	}
	v.FinishClose()
}

// finishClose は閉じる演出を即座に着地させて片付ける。閉じていなければ何もしない。
//
// 🚨 viewer を畳む唯一の出口にする。演出の着地 (settleClose) とキーによる即着地の両方が
// ここを通ることで、片方が stopWatch を通らずに次の watchCmd が走る = watcher が二重に居座る、
// という取りこぼしが構造的に起きない。
// 戻り値は「この呼び出しで実際に畳んだか」。🚨 tui.go が演出中のキーを 1 つだけ捨てる判定に使う
// (素通しで別ターゲットを開いてしまう e。理由はそちらのコメント)。
func (v *IssuesView) FinishClose() bool { //nolint:unparam // 戻り値は main (tui.go の演出中のキーの判定) が使う。unparam はパッケージの外の呼び出しを見ない
	if !v.Closing {
		return false
	}
	v.Closing = false
	v.Shown = false
	v.AnimStart = time.Time{}
	v.discardBody() // viewer ごと閉じるので引き出しの演出は持ち越さない
	v.StopWatch()   // 見張りの watcher を閉じる (fd を残さない。issues_watch.go)
	// 🚨 取り直しの予約も捨てる。残すと閉じた後に「非表示の viewer のための」スキャンが 1 回走り、
	// その間 loading() が true になって tick が昂進する (自己終息はするが無駄な仕事)。
	v.RescanPending = false
	// 番号の絞り込みは持ち越さない。🚨 q / Esc は絞り込みを解くだけで閉じない (1 段戻る) が、
	// i は 1 段戻さず閉じるので、ここで捨てないと次に開いた viewer が「なぜか件数が少ない一覧」
	// から始まる。行集合も作り直す — rows は常に visibleIssues() と一致させる (残すと、開いた
	// 直後の 1 フレームだけタブ行の下に絞り込まれた行が並ぶ)。
	if v.NumFilter.Active {
		v.endNumberFilter("")
	}
	return true
}

// finishAnim は演出を即座に着地させる。閉じる演出のときは片付けまで進める
// (ここで時計だけ止めると、閉じかけの姿のまま二度と畳まれない状態で固まる)。
func (v *IssuesView) FinishAnim() {
	// 半ページ移動のカーソル滑走も即着地させる: 次のキーは必ず論理カーソル (着地点) に効くので、
	// 描画だけが遅れて「見えている行と操作対象が違う」時間を残さない。
	v.CurGlide.Stop()
	if v.Closing {
		v.FinishClose()
		return
	}
	v.AnimStart = time.Time{}
}

// slideAnimating は viewer 全体の開閉スライド中か。tickInterval が 125fps へ上げる判定に使う
// (引き出しと pager glide は含めない: あちらは他の glide と同じ 60fps で足りる)。
func (v *IssuesView) SlideAnimating() bool {
	// 🚨 閉じる演出は「時間が過ぎたら false」にしない: tick は animating が false になった拍で
	// 止まるので、時間で降ろすと片付けの settleClose が呼ばれる前にチェーンが切れ、閉じかけの姿で
	// 固まる。settleClose が closing を下ろして初めて false になる (自分で終われる形にする)。
	if v.Closing {
		return true
	}
	return v.Shown && anim.Elapsed(v.AnimStart, app.TimeNow(), issuesAnimDuration) < 1
}

// animating は演出の途中か (tick チェーンを回し続ける spinnerActive の判定に使う)。
func (v *IssuesView) Animating() bool {
	if v.BodyPager.Animating() {
		return true // 本文 pager の glide は tick で進むので「アニメ中」に含める
	}
	if v.CurGlide.Active() {
		return true // 一覧のカーソル滑走も tick で進む
	}
	if v.Drawer.animating(app.TimeNow()) {
		return true // 本文の引き出しの開閉も tick で進む
	}
	return v.SlideAnimating()
}

// takeWantCross は横断先 (無ければ app.FullScreenNone) を一度だけ取り出す (takeNotice と同じ語彙)。
func (v *IssuesView) TakeWantCross() app.FullScreenID {
	want := v.wantCross
	v.wantCross = app.FullScreenNone
	return want
}

// takeWantQuit は「q/esc で glogx ごと終了したい」を一度だけ取り出す (takeNotice と同じ語彙)。
func (v *IssuesView) TakeWantQuit() bool {
	want := v.wantQuit
	v.wantQuit = false
	return want
}

// advanceGlide はスクロール glide を 1 フレーム進める (browseModel の tick から呼ばれる)。
func (v *IssuesView) AdvanceGlide() {
	v.BodyPager.Advance()
	if v.CurGlide.Active() {
		v.CurGlide.Advance(v.Cursor)
	}
}

// stopGlides は進行中の滑走を全部止める (advanceGlide と対)。resize では幅で行数が変わり、着地点と
// 窓の起点が resize 前の行数基準で古くなる。🚨 滑走を足したらここにも足す: 呼び出し側 (tui.go の
// resize) が viewer の中のフィールドを直接止めていた頃は、足した滑走の止め忘れが tui.go 側に出た (issue 666)
func (v *IssuesView) StopGlides() {
	v.BodyPager.Stop()
	v.CurGlide.Stop()
}

// scanCmd は探索・メタデータ読み込みを 1 つのゴルーチンでまとめて行う。
//
// なぜ 1 発でメタデータまで読むか: Issue はポインタで保持するので、後追いのゴルーチンから
// 埋めると View 側の読み取りと競合する。探索 (readdir) と本文読み (H1 / front matter /
// チェックボックス) を同じゴルーチンで終わらせて完成品を渡せば競合が構造的に起きない。
// コストは実測 2.5〜13.6ms (229 ファイルの repo) で、glogx の起動パスではなく i を
// 押したときだけ通る (仕様書「本文を起動時に全件読まない」の担保)。
func (v *IssuesView) ScanCmd(cwd string) tea.Cmd {
	if v.Scanning {
		return nil // single-flight: i / r の連打で同じ探索のゴルーチンを積まない
	}
	v.Cwd = cwd
	v.Scanning = true
	return func() tea.Msg { return ScanIssues(cwd) }
}

// scanAfterChangeCmd は「自分が (または開いていたエディタが) ファイルを変えた後」の取り直し。
// 飛行中なら予約して receive の出口で張り直すので、single-flight に落とされない。
//
// 🚨 これが必要なのは、飛行中のスキャンが**変更より前に始まっている**ため。その結果が届いても
// 変更は映らないので、単に落とすと「実ファイルは動いたのに一覧は旧位置を出し続ける」状態が
// fsnotify の次周期まで残る (fsnotify が無音な FS では保険のポーリングまで)。
//
// 🚨 読み直しだけの経路 (toggle / restore / r) には使わない。あちらは連打を畳むのが正しく、
// 予約すると 1 打ごとに追加のスキャンが後から積まれる (single-flight の目的そのものを損なう)。
func (v *IssuesView) scanAfterChangeCmd() tea.Cmd {
	if v.Scanning {
		v.RescanPending = true
		return nil
	}
	return v.ScanCmd(v.Cwd)
}

// scanIssues は探索・メタデータ読み込み・見張りの基準づくりを 1 回で行う (scanCmd の本体)。
//
// 🚨 基準 (指紋) は「読んだ内容より古い」側に寄せる。新しい側に寄ると、その差の間に入った外部編集が
// 基準に焼き込まれ、次の編集が来るまで一覧が古いまま残る (issue 749: 以前は全件の LoadMeta の後に
// 指紋を取っていた)。古い側に寄れば、次の観測で差が出て取り直すだけで済む。
//   - ファイルの編集: 指紋を LoadMeta の前に取る (後から入った編集は LoadMeta が読むか、次の観測で差になる)
//   - ファイルの追加・削除: 見張る集合は Scan の結果から作るので、Scan の前に取ることはできない。
//     代わりに構造のディレクトリの指紋を Scan の前と指紋を取った後で比べ、違えば基準を
//     issuesStaleBaseline にして次の観測で必ず取り直させる
func ScanIssues(cwd string) IssuesScanMsg {
	root := issues.RepoRoot(cwd)
	dirs := issues.FindDirs(root)
	before := IssuesFingerprint(issuesWatchDirs(dirs, nil), nil)
	found, warnings := issuesScanDirs(dirs)
	fp := IssuesFingerprint(issuesWatchDirs(dirs, found), issuesWatchPaths(found))
	if IssuesFingerprint(issuesWatchDirs(dirs, nil), nil) != before {
		fp = issuesStaleBaseline
	}
	loadAllMeta(found)
	return IssuesScanMsg{Root: root, Dirs: dirs, Issues: found, Warnings: warnings, fp: fp}
}

// loadAllMeta は全件のメタデータを読む。読み終えた時点の差し込み口を同じ関数に持つのは、読む位置を動かす
// 変更 (指紋の後ろ → 前) で差し込み口も一緒に動き、テストが「読んだ後から指紋までの窓」を見続けるため。
func loadAllMeta(found []*issues.Issue) {
	for _, iss := range found {
		// メタデータの読み取り失敗は無視する (タイトルがスラッグ表示に落ちるだけ)
		_ = iss.LoadMeta()
	}
	scanIssuesStage("read")
}

// issuesScanDirs はテストの差し込み口 (走査の最中に入った追加・移動を作る)。
var issuesScanDirs = issues.Scan

// issuesStaleBaseline は「走査の途中でディレクトリが変わった」ときの基準。観測した指紋とは
// 一致しないので、次の観測で取り直しになる ("" は「次の観測を基準にする」の意味なので使えない)。
const issuesStaleBaseline = "\x00stale"

// scanIssuesStage はテストの差し込み口 (走査の途中で外部の編集を起こす)。
var scanIssuesStage = func(string) {}

// receive はスキャン結果を反映する。
//
// Scan は毎回新しい *Issue を作るので、見ている場所は安定キーで引き直す (仕様が定める同一性キーは
// パス。番号も basename も一意でない)。引き直さないと、再スキャンのたびに (a) カーソルが別の
// issue へ滑り、(b) タブが別カテゴリを指し (tabs は件数降順なので件数が変わると並びが変わる)、
// (c) 本文モードが v.all から外れた古いポインタを掴んで状態・進捗が編集前のまま固まる。
// 戻り値は「この結果を受けて追加で走らせる Cmd」(予約されていた取り直し。無ければ nil)。
// restore / reloadAfterEdit と同じく Cmd を返す作法に揃えている。
func (v *IssuesView) Receive(msg IssuesScanMsg) tea.Cmd {
	v.Scanning, v.Loaded = false, true
	// 見張りの基準は「このスキャンが読んだ時点の指紋」に揃える (issues_watch.go)。最初の観測で
	// 取ると、読んだ時刻と基準を取る時刻が最大 1 周期ずれ、その間の外部編集が基準へ焼き込まれて
	// 次の編集が来るまで永久に取りこぼす。自分の取り直しを「外部の変化」と誤検出しないのも同じ式で
	// 満たせる (スキャンは内容を変えないので指紋は動かない)。
	v.Watch.seen, v.Watch.pending = msg.fp, ""
	tab, cursorPath, openPath, markPath := v.currentTab(), issuePath(v.current()), issuePath(v.rootOpen()), v.markPath()
	cursorGroupKey := v.currentGroupKey()
	pendingCursorPath, pendingMarkPath := v.pendingCursorPath, v.pendingMarkPath
	pendingMoveStale := v.pendingMoveStale
	if pendingCursorPath != "" {
		cursorPath = pendingCursorPath
	}
	if pendingMarkPath != "" {
		markPath = pendingMarkPath
	}
	v.Root, v.Dirs, v.All, v.warnings = msg.Root, msg.Dirs, msg.Issues, msg.Warnings
	v.tabsCanon = issues.Tabs(v.All, issues.TabMinCount)
	v.tabs = v.tabsCanon // refresh が件数を数えて表示順 (0 件を右) へ並べ替える
	v.tabIdx = tabIndexOf(v.tabs, tab)
	v.Refresh()
	v.pruneExpandedGroups() // 開いたまま group を消して作り直しても、死にキーで勝手に展開しない
	// 親行にカーソルがあっても、move の再アンカー予約 (pendingCursorPath) があるならそちらを
	// 優先する。予約は必ず issue の path なので、親行アンカーで飛ばすと消費だけされて効かない
	// (今は親行では n を受けないので到達しないが、受けた瞬間に黙って壊れる形なので先に塞ぐ)。
	if cursorGroupKey != "" && pendingCursorPath == "" {
		v.anchorGroupInternal(cursorGroupKey)
	} else {
		v.anchorCursorInternal(cursorPath)
	}
	v.anchorMark(markPath)
	// 移動直後に到着した結果が move 前に始まった stale scan なら、新 path はまだ msg.all に
	// 無い。stale 印の次の結果まで pending anchor を残し、そこで宛先を消費する。
	if pendingCursorPath != "" && (hasIssuePath(v.All, pendingCursorPath) || !pendingMoveStale) {
		v.pendingCursorPath = ""
	}
	if pendingMarkPath != "" && (hasIssuePath(v.All, pendingMarkPath) || !pendingMoveStale) {
		v.pendingMarkPath = ""
	}
	if pendingMoveStale {
		v.pendingMoveStale = false
	}
	v.rebindOpen(openPath)
	v.startWatch() // 監視対象のディレクトリはスキャン結果で決まる (issues_watch.go)
	if v.pending != nil {
		// 起動時の復元予約は最初のスキャン結果へ 1 度だけ当てる (以降の r / 編集後の取り直しは
		// 通常どおり「今見ている場所」を引き継ぐ)
		s := *v.pending
		v.pending = nil
		v.applyScreen(s)
	}
	if v.RescanPending {
		v.RescanPending = false
		return v.ScanCmd(v.Cwd) // 畳んでおいた要求を 1 回だけ張り直す (ここでは scanning=false)
	}
	return nil
}

// issuePath は nil 安全なパス取得 (再スキャンをまたいで位置を引き継ぐキー)。
func issuePath(iss *issues.Issue) string {
	if iss == nil {
		return ""
	}
	return iss.Path
}

func hasIssuePath(all []*issues.Issue, path string) bool {
	if path == "" {
		return false
	}
	for _, iss := range all {
		if iss.Path == path {
			return true
		}
	}
	return false
}

// tabIndexOf は名前からタブ位置 (0 = All) を引く。消えたカテゴリは All に落ちる。
func tabIndexOf(tabs []issues.Tab, name string) int {
	if name == tabNextName {
		return tabIdxNext
	}
	if name == "" {
		return 0
	}
	for i, t := range tabs {
		if t.Name == name {
			return i + 1
		}
	}
	return 0
}

// anchorCursorInternal は再スキャン後と復元時の内部再アンカー。ここでは pending move を残し、
// 受信側が stale / authoritative の判定を終える。
func (v *IssuesView) anchorCursorInternal(path string) {
	if path == "" {
		return
	}
	v.EnsureDisplayRows()
	if v.cursorToPath(path) {
		return
	}
	// 🚨 rows には居るのに displayRows に居ない = 畳んだ group の中に隠れている。その group を
	// 開いて置き直す。移動で issue が group の中へ入る経路があり (予約外ディレクトリの迷子に n を
	// 押すと `epic/<name>/next/` = group の子になる)、開かないと「移動しました」と言った直後に
	// 対象が画面から消え、カーソルは親行に着地する (2026-09-06 の敵対的レビュー 2 周目)。
	//
	// 🚨 これは**ユーザーが明示的に畳んだ状態 (groups.collapsed) を上書きし、しかも
	// groups.manual は screen に保存される**ので、再起動を跨いで開いたままになる。
	// groups.auto は refresh のたびに捨てられるので、再スキャンを跨ぐ移動の
	// 再アンカーには使えない (置き場が groups.manual しかない)。カーソルの居場所を
	// 見せる方を優先した意図的な選択 (同 3 周目 P3-5)。
	for _, iss := range v.Rows {
		if iss.Path != path || iss.GroupKey == "" {
			continue
		}
		v.groups.reveal(iss.GroupKey)
		v.rebuildDisplayRows()
		v.cursorToPath(path)
		return
	}
}

// cursorToPath は displayRows の中で path の issue 行へカーソルを置く (見つからなければ false)。
func (v *IssuesView) cursorToPath(path string) bool {
	for i, row := range v.displayRows {
		if row.kind == displayRowIssue && row.issue.Path == path {
			v.Cursor = i
			return true
		}
	}
	return false
}

// pruneExpandedGroups は今の走査結果に無い GroupKey を展開状態から落とす。GroupKey は絶対
// パスなので、group の rename / 削除 / checkout の移動で死にキーが state に溜まり、同名 group を
// 作り直したとき「畳んだつもり」を無視して勝手に展開する (敵対レビュー round 2)。
func (v *IssuesView) pruneExpandedGroups() {
	if len(v.groups.manual) == 0 {
		return
	}
	alive := make(map[string]bool, len(v.groups.manual))
	for _, iss := range v.All {
		if key, ok := iss.EpicGroupKey(); ok {
			alive[key] = true
		}
	}
	v.groups.prune(alive)
}

// isGroupParent は展開を持つ親行 (合成の group 行と、統合した親 issue 行の両方)。
func (r displayRow) isGroupParent() bool {
	return r.kind == displayRowGroup || r.groupHead
}

func (v *IssuesView) currentGroupKey() string {
	row, ok := v.currentDisplayRow()
	if !ok || row.kind != displayRowGroup {
		return ""
	}
	return row.groupKey
}

// markPath は選択の錨が指す issue のパス ("" = 選択していない / 錨が範囲外)。
func (v *IssuesView) markPath() string {
	v.EnsureDisplayRows()
	if !v.Marked || v.MarkAt < 0 || v.MarkAt >= len(v.displayRows) ||
		v.displayRows[v.MarkAt].kind != displayRowIssue {
		return ""
	}
	return v.displayRows[v.MarkAt].issue.Path
}

// anchorMark は再スキャン前と同じ issue へ選択の錨を張り替える (消えていれば選択したままにしない)。
//
// このビューは「位置で持つものは安定キーで張り替える」で揃えている (カーソル=パス・タブ=名前・
// 本文=パス)。選択の錨だけがその規律から外れており、行集合を作り直す refresh が畳んでいた。
// 外部編集の即時反映 (issues_watch.go) が入ってからは、選択している最中に取り直しが走るのが
// 普通になった (Claude Code が issue を書くたび) ため、畳むと選択が実用にならない。
//
// 🚨 張り替えるのは再スキャン (同じ集合の読み直し) だけ。タブ・フィルタの切り替えでは refresh が
// 畳んだままにする — あちらは行集合の意味そのものが変わるので、範囲を持ち越すと別の対象を指す。
func (v *IssuesView) anchorMark(path string) {
	if path == "" {
		return
	}
	v.EnsureDisplayRows()
	for i, row := range v.displayRows {
		if row.kind == displayRowIssue && row.issue.Path == path {
			v.Marked, v.MarkAt = true, i
			return
		}
	}
}

// rebindOpen は本文モードで開いている issue を新しいスキャン結果へ繋ぎ直す。
//
// 同一性キーはパス (spec 2 節: 番号は一意でない) だが、パスは `n` の next/ 移動や別プロセスの
// 状態ディレクトリ移動で変わる。そこで 3 段で解決する:
//
//  1. 同じパスがあれば繋ぎ直す (通常の再スキャン)
//  2. 無ければ **同じ basename が 1 件だけ**ある場所へ繋ぎ直す (= 移動を追う)。追わないと、読んでいる
//     最中に done/ へ移された issue が「実体から外れた本文」になり、以降の y が消えたパスをコピーし、
//     e は実体確認 (editCmd) で弾かれて編集もできない。複数一致は spec 3 節が警告する異常
//     (同名が複数の状態ディレクトリにある) で、どれが本人か決められないので繋ぎ直さない
//  3. どこにも無ければ本文モードを畳んで理由を通知する。🚨 消えた本文を出し続けると、viewer が
//     「もう無いファイルの内容」を最新として見せ続ける (このモードでは実体が無いので編集も
//     取り直しもできず、読み続ける対象がそもそも無い)
//
// 🚨 畳むのは 3 の「どこにも無い」ときだけ。移動を 2 で吸収してから判定するので、done/ へ移された
// だけで一覧へ引き戻すことはない (カーソル・選択の錨が「消えていれば現状維持」なのと同じ精神で、
// ユーザーの居場所を理由なく奪わない)。
func (v *IssuesView) rebindOpen(path string) {
	if path == "" {
		return
	}
	if len(v.docStack) > 0 {
		// doc を開いている: 一覧と照合するのは底の issue (doc は一覧に無いので、照合すると「見つからない」で畳まれる)
		doc := v.Open
		v.Open = v.docStack[0].open
		v.rebindOpenIssue(path)
		if v.Open == nil {
			return // 底の issue が消えた = 本文ごと畳んだ (discardBody が積んだものも捨てている)
		}
		v.docStack[0].open, v.Open = v.Open, doc
		return
	}
	v.rebindOpenIssue(path)
}

// rebindOpenIssue は rebindOpen の本体 (v.open が issue のとき)。
func (v *IssuesView) rebindOpenIssue(path string) {
	for _, iss := range v.All {
		if iss.Path == path {
			v.Open = iss
			return
		}
	}
	base := filepath.Base(path)
	dir := ""
	if v.Open != nil {
		dir = v.Open.Dir
	}
	// 🚨 ここの通知は app.NoticeError (lastWarning に積む)。Msg 経路 (外部の変更) で置かれるので、次の打鍵が q だと
	// トーストを読む前に終了し、理由が w でしか辿れない (issue 059)
	moved, ambiguous := v.matchByBase(dir, base)
	switch {
	case moved != nil:
		// 🚨 basename 一致だけで本人と決めない (issue 277)。basename は「番号 + スラッグ」なので
		// 番号の一意性 (tests/issues/test_issue_numbers_unique.sh) が守られている限り本人だが、
		// それが壊れた repo や `issues/epic/<name>/` が増えた状況では**別 issue** でありうる。
		// 症状は「読んでいた本文が静かに別の issue に差し替わる」で、利用者からは気づけない。
		//
		// 🚨 **見出しが違っても本文は捨てないこと** (敵対的レビュー 2026-09-06)。見出しの不一致は
		// 「別 issue」と「同じ issue を移動と同時に改題した」を区別できない (basename が同じなら
		// 番号も同じなので、番号でも切れない)。後者は `git pull` / `git checkout` で
		// 「done/ へ移しつつ H1 も直したコミット」を受けるだけで起き、issuesWatchDebounce の
		// 200ms が両イベントを 1 回の再スキャンへ畳むので**日常操作で成立する**。
		// 捨てると、自分で直した見出しのせいで起きていない事象を告げられ、読んでいた位置を失う。
		// 黙って入れ替わるのを防ぐ目的は notice で足りる (利用者が見て判断できる)。
		if v.Open != nil && v.Open.Title != "" {
			_ = moved.LoadMeta() // 見出しは遅延読み。読めなければ突き合わせを諦める (下の条件が偽)
			if moved.Title != "" && moved.Title != v.Open.Title {
				v.setNotice("見出しが変わりました (別 issue の可能性): "+base, app.NoticeError)
			}
		}
		v.Open = moved
	case ambiguous:
		// 実体はあるが本人を決められない。畳むのは「どこにも無い」ときだけなので現状維持
	default:
		v.discardBody() // 演出は挟まない (抜けていく板に映す中身がもう無い)
		v.setNotice("開いていた issue が見つかりません (一覧へ戻ります): "+base, app.NoticeError)
	}
}

// matchByBase は同じ issue dir (dir、"" なら全 dir) の中で同じファイル名の issue を探す。
// ちょうど 1 件なら (それ, false)、複数なら (nil, true)、無ければ (nil, false)。🚨 「複数」と
// 「無い」を呼び出し側で分ける必要がある (複数は実体があるので畳んではいけない)。
//
// 🚨 issue dir を跨いで照合しない。移動 (`n` / done へ送る / git mv) はどれも同じ issue dir の
// 中で起き、同一性の範囲も issues.conflicts と同じ (Dir, basename)。跨ぐと、root の issues/ と
// macOS/issues/ を両方持つ repo で、見出しまで同じ別 dir の issue へ黙って繋ぎ直す (issue 665)。
func (v *IssuesView) matchByBase(dir, base string) (found *issues.Issue, ambiguous bool) {
	for _, iss := range v.All {
		if filepath.Base(iss.Path) != base || (dir != "" && iss.Dir != dir) {
			continue
		}
		if found != nil {
			return nil, true // 同名が複数 = どれが本人か決められない
		}
		found = iss
	}
	return found, false
}

// refresh は現在のタブ・フィルタで表示対象を作り直す。
//
// offset を 0 に戻さないのは、cursor を温存したまま窓だけ先頭へ飛ばすと「カーソル行が
// どの行にも描かれない」状態が残るため (a / Tab / 再スキャンで実際に起きていた)。窓は
// windowOffset が cursor から導出するので、ここは行集合の作り直しだけを担う。
func (v *IssuesView) Refresh() {
	// 🚨 行集合が変わるので選択は畳む。錨は位置で持つため、残すと別の issue を指す
	v.ClearMark()
	v.CurGlide.Stop() // 同じ理由で滑走も畳む (起点の行番号が別の issue を指す)
	v.SetRows(v.visibleIssues())
	if v.NumFilter.Active {
		v.groups.setAuto(v.NumFilter.groupKeys(v.Rows))
	} else {
		v.groups.setAuto(nil)
	}
	v.rebuildDisplayRows()
	v.Cursor = app.ClampIdx(v.Cursor, len(v.displayRows))
	// チップの件数は「そのタブを選んだときに実際に並ぶ行数」と同じ Filter から出す。
	// issues.Tab.Count は done を含む全件なので、そのまま出すと done を伏せた既定表示で
	// 「カテゴリの合計 ≠ All ≠ 一覧の行数」になる。
	//
	// 🚨 タブ集合そのものは v.all から作る (receive)。Filter 後の集合から作り直すと done だけの
	// カテゴリが消え、位置で持つ tabIdx が別カテゴリを指す。ここで数えるのは件数だけ。
	v.allCount = len(issues.Filter(v.All, "", v.Filter))
	v.nextCount = len(v.rowsForTab(tabNextName))
	sel := v.currentTab() // 並べ替えを跨いで選択を保つため名前で覚える (tabIdx は位置で持つ)
	counts := make([]int, len(v.tabsCanon))
	for i, t := range v.tabsCanon {
		counts[i] = len(issues.Filter(v.All, t.Name, v.Filter))
	}
	// 0 件のカテゴリは右へ寄せる (ユーザー要望 2026-07-31)。タブ集合の順序は done 込みの全件数で
	// 決まる (issues.Tabs) ため、状態を伏せた表示では「0 件なのに左端」が構造的に起きていた。
	v.tabs, v.tabCount = reorderTabsByCount(v.tabsCanon, counts)
	v.tabIdx = tabIndexOf(v.tabs, sel)
}

// rebuildDisplayRows は現在見えている子 issue を親行 + 子行へ写像する。rows は Scan/Filter の
// 並びを保つが、親行の順序だけは group の最大番号で決める (同値は GroupKey 昇順)。
func (v *IssuesView) rebuildDisplayRows() {
	type group struct {
		key       string
		name      string
		parent    *issues.Issue
		children  []*issues.Issue
		maxNumber int
		hasNumber bool
	}
	groups := make(map[string]*group)
	groupList := make([]*group, 0, 4)
	units := make([]displayUnit, 0, len(v.Rows))
	for _, iss := range v.Rows {
		key, epic := iss.EpicGroupKey()
		if !epic {
			n, ok := issues.Number(iss)
			units = append(units, displayUnit{
				row:       displayRow{kind: displayRowIssue, issue: iss},
				maxNumber: n, hasNumber: ok,
				key: iss.Rel,
			})
			continue
		}
		g, ok := groups[key]
		if !ok {
			g = &group{key: key, name: iss.Group}
			groups[key] = g
			groupList = append(groupList, g)
		}
		switch {
		case issues.IsGroupParent(iss) && g.parent == nil:
			g.parent = iss // その epic 自身の親 issue (形は issues.IsGroupParent)
		case issues.IsGroupNumberParent(iss) && !issues.IsGroupNumberParent(g.parent):
			// 番号の形を優先する (v.rows の並び = 番号の降順に任せると、後から足した
			// `NNN-epic-<番号>` が旧来の親を子へ落とす)。先に親にしたものは子へ戻す
			g.children = append(g.children, g.parent)
			g.parent = iss
		default:
			g.children = append(g.children, iss)
		}
		n, ok := issues.Number(iss)
		if ok && (!g.hasNumber || n > g.maxNumber) {
			g.maxNumber, g.hasNumber = n, true
		}
	}
	for _, g := range groupList {
		if g.parent == nil && len(g.children) == 0 {
			continue
		}
		row := displayRow{
			kind: displayRowGroup, groupKey: g.key, groupName: g.name,
			childCount: len(g.children), childDone: countDoneIssues(g.children),
		}
		if g.parent != nil {
			row.kind, row.issue, row.groupHead = displayRowIssue, g.parent, true
		}
		units = append(units, displayUnit{
			row:       row,
			maxNumber: g.maxNumber, hasNumber: g.hasNumber, key: g.key,
		})
	}
	if len(groupList) > 0 {
		// group が無い従来の一覧は、caller が渡した rows の順序をそのまま保つ。
		// Scan の通常順序は既に番号降順で、テスト/復元の手組みの順序も壊さない。
		sortDisplayUnits(units)
	}
	out := make([]displayRow, 0, len(v.Rows)+len(groupList))
	for _, u := range units {
		out = append(out, u.row)
		if !u.row.isGroupParent() || !v.groupExpanded(u.row.groupKey) {
			continue
		}
		g := groups[u.row.groupKey]
		for _, iss := range g.children {
			out = append(out, displayRow{kind: displayRowIssue, issue: iss, inGroup: true})
		}
	}
	v.displayRows = out
	v.displayRowsGen = v.rowsGen
}

// setRows は表示対象の子 issue 集合を差し替える (displayRows の再構築を予約する)。
//
// 🚨 `v.rows = ...` と直接書かないこと。世代が進まないと ensureDisplayRows が
// 「同期済み」と判断し、displayRows が古いまま描かれる (issue 268)。
// production の書き込み口は refresh の 1 箇所だが、テストが行集合を手で作る経路もある
// (scroll_glide_test)。規律は issues_rows_setter_test.go が走査で強制する。
func (v *IssuesView) SetRows(rows []*issues.Issue) {
	v.Rows = rows
	v.rowsGen++
}

// sortDisplayUnits は番号付きの単独 issue と group 親を同じ番号順に並べる。group の最大番号
// が、その group の表示位置を決める。
func sortDisplayUnits(units []displayUnit) {
	sort.SliceStable(units, func(i, j int) bool {
		a, b := units[i], units[j]
		return issues.OrderLess(a.maxNumber, a.hasNumber, a.key, b.maxNumber, b.hasNumber, b.key)
	})
}

func (v *IssuesView) groupExpanded(key string) bool {
	return v.groups.expanded(key)
}

// ensureDisplayRows は setRows で予約された再構築を、実際に displayRows が要るところで消化する。
//
// 🚨 判定は世代の比較だけ (O(1))。以前は rows の全件突き合わせで、これを可視行ごとに呼ぶ
// 描画経路が「可視行数 × 全件」で走っていた (issue 268)。
func (v *IssuesView) EnsureDisplayRows() {
	// 🚨 世代が同じでも「rows が非空なのに displayRows が空」なら作り直す。
	// 世代だけを見ると、ゼロ値から `&issuesView{rows: list}` のように**複合リテラルで
	// 直接埋めた**場合に rowsGen も displayRowsGen も 0 のまま = 同期済みと誤判定して
	// 恒久的に stale になる (敵対レビュー 2026-09-06)。全件比較だった旧実装は
	// `len(displayRowsSource) != len(rows)` で偶然この形を自己回復していたので、
	// 世代化でその回復を落とさないよう明示する。rows が非空なら rebuildDisplayRows は
	// 必ず 1 行以上を作る (issue 行か group 親行) ので、この条件は O(1) で足りる。
	if v.displayRowsGen == v.rowsGen && (len(v.displayRows) > 0 || len(v.Rows) == 0) {
		return
	}
	v.rebuildDisplayRows()
}

func (v *IssuesView) currentDisplayRow() (displayRow, bool) {
	v.EnsureDisplayRows()
	if v.Cursor < 0 || v.Cursor >= len(v.displayRows) {
		return displayRow{}, false
	}
	return v.displayRows[v.Cursor], true
}

func (v *IssuesView) isIssueDisplayRow(i int) bool {
	v.EnsureDisplayRows()
	return i >= 0 && i < len(v.displayRows) && v.displayRows[i].kind == displayRowIssue
}

func copyExpandedGroups(src map[string]bool) map[string]bool {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]bool, len(src))
	for key, expanded := range src {
		if expanded {
			dst[key] = true
		}
	}
	if len(dst) == 0 {
		return nil
	}
	return dst
}

// visibleIssues は今の条件で一覧に並べる行集合。
//
// 🚨 行集合を作るのはここ 1 箇所にする。番号フィルタは再スキャン (r / 見張り) や a を跨いで
// 残るので、/ の処理側で v.rows を差し替える形にすると、refresh を通る経路 (receive /
// applyScreen / a) が絞り込みヘッダーを出したままタブの行へ黙って戻してしまう。
func (v *IssuesView) visibleIssues() []*issues.Issue {
	if v.NumFilter.Active {
		return v.NumFilter.rows(v.All)
	}
	return v.rowsForTab(v.currentTab())
}

// rowsForTab はタブ名に対応する行集合。🚨 疑似カテゴリ [next] はファイル名のカテゴリではなく
// 状態 (next/ に居るか) で選ぶので、issues.Filter へ名前として渡さない (渡すと「@next という
// カテゴリの issue」を探して常に 0 件になる)。
//
// [next] が状態フィルタ (a) を見ないのは、next が段階に関係なく常に見える状態だから
// (issues.StatusFilter.shows)。目印を付けたものが「今の段階では見えない」のは逆の結果になる。
//
// [next] は global の目印だけ。epic の子の目印 (`epic/<name>/next/`) は group の中で ▶ として
// 見えるので載せない (載せると同じ issue が group と [next] の 2 箇所に並ぶ。ユーザー要望 2026-09-24)。
func (v *IssuesView) rowsForTab(tab string) []*issues.Issue {
	if tab != tabNextName {
		return issues.Filter(v.All, tab, v.Filter)
	}
	out := make([]*issues.Issue, 0, 8)
	for _, iss := range v.All {
		if _, epic := iss.EpicGroupKey(); iss.Status == issues.StatusNext && !epic {
			out = append(out, iss)
		}
	}
	return out
}

// reorderTabsByCount は正規順序 tabs を「件数 0 を右へ寄せた」並びへ写す純関数 (件数も同じ並びで
// 返す)。件数 > 0 / 0 の 2 群に分け、各群の中は正規順序を保つ。入力は破壊しない。
//
// 🚨 human タブは 0 件でも右へ寄せない (All の直後に固定する)。人間待ちのタスクは件数が
// 少ないときこそ見落とすので、席を動かさないことが目的 ([next] を左端に固定するのと同じ規律)。
//
// 🚨 表示順だけ変えて巡回順を据え置くと、Tab キーの移動が画面の並びと食い違う (右端に見えるタブへ
// 順番に辿り着けない)。呼び出し側は tabs (表示・巡回順) をこれで作り直し、位置で持つ選択 (tabIdx)
// は名前から張り替える (tabIndexOf)。張り替えないと a で件数が変わった瞬間に選択が別カテゴリへ滑る。
func reorderTabsByCount(tabs []issues.Tab, counts []int) ([]issues.Tab, []int) {
	if len(tabs) != len(counts) {
		return tabs, counts // 数え漏れ (呼び出し側のバグ) では並べ替えない
	}
	outTabs := make([]issues.Tab, 0, len(tabs))
	outCounts := make([]int, 0, len(counts))
	for i, t := range tabs {
		if t.Name == issues.HumanTab {
			outTabs = append(outTabs, t)
			outCounts = append(outCounts, counts[i])
		}
	}
	for _, nonZero := range []bool{true, false} {
		for i, c := range counts {
			if tabs[i].Name == issues.HumanTab {
				continue // 上で固定済み
			}
			if (c > 0) == nonZero {
				outTabs = append(outTabs, tabs[i])
				outCounts = append(outCounts, c)
			}
		}
	}
	return outTabs, outCounts
}

// setNotice は操作結果を置く (kind は deliverNotice が lastWarning に積むかを決める。app.NoticeKind の doc)。
// 🚨 ここで無害化する: 通知文は issue のファイル名・本文由来の URL を素で埋め込む呼び出しが
// 多く、呼び出しごとに包むと必ずどこかが漏れる (status_view.go の setNotice と同じ規律)。
func (v *IssuesView) setNotice(text string, kind app.NoticeKind) {
	v.Notice, v.noticeKind = term.SanitizePlainLine(text), kind
}

// takeNotice は未表示の操作結果を取り出して消す。browseModel がトーストへ流すための口で、
// 取り出された通知はヘッダーに出さない (トーストとヘッダーで二重に出さない)。
//
// なぜヘッダー行でなくトーストにするか: コピーや URL 起動の結果は glogx 全体で右下トーストに
// 出す語彙で統一されている (ユーザー要望 2026-07-31)。viewer が全画面でトーストが隠れていた
// 時代の名残でヘッダーに出していたが、トーストを viewer の上にも合成するようにしたので不要。
func (v *IssuesView) TakeNotice() (string, app.NoticeKind) {
	text, kind := v.Notice, v.noticeKind
	v.Notice, v.noticeKind = "", app.NoticeOK
	return text, kind
}

// tabNextName は疑似カテゴリ [next] の識別子 (保存・復元でもこの名前で持つ)。
//
// 🚨 ファイル名のカテゴリトークンとして現れない綴りにする: 実在するカテゴリ語と同じ綴りだと
// 同名のタブが 2 つ並び、位置で持つ選択 (tabIdx) の指す先が曖昧になる (issues.Tabs が other で
// 同じ問題を合算で回避しているのと同型)。トークンは英数と - からしか作られないので @ を使う。
const tabNextName = "@next"

// tabIdx の規約: -1 = 疑似カテゴリ [next]、0 = All、1.. = v.tabs[tabIdx-1]。
//
// 🚨 next を -1 にして All を 0 のままにするのは zero value のため。0 を next にすると、
// 作りたてのビュー (newIssuesView / テストの zero value) が既定で [next] を選ぶことになり、
// 「開いたら空の一覧が出る」挙動に変わる。
const tabIdxNext = -1

// currentTab は選択中のタブ名 ("" = All、tabNextName = 疑似カテゴリ [next])。
func (v *IssuesView) currentTab() string {
	if v.tabIdx == tabIdxNext {
		return tabNextName
	}
	if v.tabIdx <= 0 || v.tabIdx > len(v.tabs) {
		return ""
	}
	return v.tabs[v.tabIdx-1].Name
}

// closeBody は本文モードを抜ける。
// closeBody は本文を閉じる。🚨 ここでは逆再生を始めるだけで中身は消さない — 消すと閉じる
// 演出に何も映らない。実際の破棄は演出が着地したとき (settleDrawer)。
func (v *IssuesView) CloseBody() {
	// 後始末は本文の有無に依らず行う (呼ばれた時点で「本文モードではない」を満たすべき)。
	v.BodyPager.Stop()
	v.UrlPick.Close()
	if v.Open == nil {
		return
	}
	v.Drawer.startClose(app.TimeNow())
}

// discardBody は本文の状態を実際に捨てる (演出の着地後・viewer を閉じるとき)。
func (v *IssuesView) discardBody() {
	v.Open, v.Body = nil, nil
	v.BodyPager.Reset()
	v.UrlPick.Close()
	v.LinkJump = linkJump{}
	v.docStack, v.docLine = nil, 0
	v.Drawer = issuesDrawer{}
}

// settleDrawer は引き出しの演出が終わっていれば静止状態へ進め、閉じ切っていれば本文を捨てる。
// 描画とキー処理の両方から呼ぶ (どちらが先に来ても状態が進む)。
func (v *IssuesView) settleDrawer() {
	if v.Drawer.settle(app.TimeNow()) {
		v.discardBody()
	}
}

// openBody はカーソル位置の issue の本文を読む。
//
// 読み込みは同期で行う: 1 ファイルの読み込みは sub-ms で、非同期にすると「読み込み中に
// カーソルが動いた」等の状態を増やすだけで得がない (スキャンと違い件数に比例しない)。
func (v *IssuesView) openBody() {
	if row, ok := v.currentDisplayRow(); ok && row.kind == displayRowGroup {
		v.setNotice("親行では issue を開けません", app.NoticeRefused)
		return
	}
	if iss := v.current(); iss != nil {
		v.OpenIssue(iss)
	}
}

// openIssue は指定した issue の本文を開く (読めたら true)。カーソル行を開く openBody と、
// 前回終了時の画面をパスから開き直す applyScreen が共有する。
func (v *IssuesView) OpenIssue(iss *issues.Issue) bool {
	v.ClearMark() // 本文は 1 件の操作。選択を残すと y が一覧の範囲へ効いて対象が食い違う
	body, err := iss.ReadBody()
	if err != nil {
		v.setNotice("本文を読めませんでした: "+app.FirstLine(err.Error()), app.NoticeError)
		return false
	}
	v.Open, v.Body = iss, body
	v.BodyPager.Reset()
	v.UrlPick.Close() // 別の issue を開いたら前の URL 一覧を持ち越さない
	v.LinkJump = linkJump{}
	v.docStack, v.docLine = nil, 0 // 別の issue を開いたら積んだ doc は捨てる (戻り先が別の issue になる)
	v.Drawer.open(app.TimeNow())
	return true
}

// openNeighbor は本文を開いたまま、一覧の隣 (delta = ±1) の issue へ差し替える。
//
// 親行 (本文が無い) は飛ばし、端では止めて案内する (巻かない: 「次」を押し続けて先頭へ戻ると、
// 読み終えたのか一周したのか分からなくなる)。開閉の演出は挟まない — 引き出しは開いたままで
// 中身だけ替わる (板の位置が動かないので、左に覗いている一覧のカーソルが追従して見える)。
// スクロール位置は先頭へ戻す (openIssue が bodyPager を先頭へ戻す。前の位置を引き継ぐと短い本文で
// 空白だけが見える)。
func (v *IssuesView) openNeighbor(delta, rows int) {
	if v.Open == nil {
		return
	}
	v.EnsureDisplayRows()
	i := v.Cursor + delta
	for ; i >= 0 && i < len(v.displayRows); i += delta {
		if v.displayRows[i].kind == displayRowIssue {
			break
		}
	}
	if i < 0 || i >= len(v.displayRows) {
		if delta > 0 {
			v.setNotice("これが最後の issue です", app.NoticeRefused)
		} else {
			v.setNotice("これが最初の issue です", app.NoticeRefused)
		}
		return
	}
	if !v.OpenIssue(v.displayRows[i].issue) {
		return // 読めなかった issue へはカーソルも動かさない (見えているものと開いているものを揃える)
	}
	v.Drawer.Finish() // openIssue が開く演出を始め直すので即着地させる (引き出しは既に開いている)
	v.setCursor(i, rows)
}

// reloadAfterEdit は nvim で編集して戻ってきたときの取り直し (呼び出し側の extcmd.EditorClosedMsg)。
//
// 一覧のメタデータは Issue.LoadMeta が一度読んだら二度読まないので再スキャンで作り直し、開いて
// いる本文は Body が読み込み時の内容を握っているので読み直す。どちらもしないと、編集した当人に
// 対して viewer が編集前の内容を出し続ける (仕様が最も嫌う「viewer が確信を持って嘘をつく」型)。
func (v *IssuesView) ReloadAfterEdit() tea.Cmd {
	if !v.Shown {
		return nil
	}
	if v.Open != nil {
		if body, err := v.Open.ReadBody(); err == nil {
			v.Body = body // bodyPager の位置は保つ (描画側が新しい行数へ収束させる)
		}
		// 積んだ本文も読み直す: doc から底の issue へジャンプして e で編集すると、編集したのは積んだ側で、
		// h で戻ったときに編集前の本文を出すことになる (「確信を持って嘘をつく」形)
		for i := range v.docStack {
			if body, err := v.docStack[i].open.ReadBody(); err == nil {
				v.docStack[i].body = body
			}
		}
	}
	return v.scanAfterChangeCmd()
}

// current はカーソル位置の issue (無ければ nil)。
func (v *IssuesView) current() *issues.Issue {
	row, ok := v.currentDisplayRow()
	if !ok || row.kind != displayRowIssue {
		return nil
	}
	return row.issue
}

func (v *IssuesView) currentIsGroup() bool {
	row, ok := v.currentDisplayRow()
	return ok && row.kind == displayRowGroup
}

// toggleGroupAtCursor は親行の展開状態を切り替える。親行以外では false を返し、呼び出し側が
// 通常の Enter/Space の意味を続けて処理できるようにする。
func (v *IssuesView) toggleGroupAtCursor() bool {
	row, ok := v.currentDisplayRow()
	if !ok || !row.isGroupParent() {
		return false
	}
	key := row.groupKey
	v.groups.toggle(key)
	v.Refresh()
	v.anchorGroup(key)
	return true
}

func (v *IssuesView) anchorGroup(key string) {
	v.clearPendingMoveAnchors()
	v.anchorGroupInternal(key)
}

func (v *IssuesView) anchorGroupInternal(key string) {
	v.EnsureDisplayRows()
	for i, row := range v.displayRows {
		if row.isGroupParent() && row.groupKey == key {
			v.Cursor = i
			return
		}
	}
}

// endNumberFilter は番号フィルタを終わらせる唯一の経路 (検索語・group の一時的な展開・行集合を 1 組で戻す)。
// keep は解除前の現在行の group (その group の展開を手動へ引き継ぐ。"" なら引き継がない)。
// 🚨 numFilter.clear と groups.endFilter を別々に呼ばない: 片方だけだと解除後も畳んだ状態が残り、
// 展開しても開かない (issue 666 の E / 752)。
func (v *IssuesView) endNumberFilter(keep string) {
	v.groups.endFilter(keep)
	v.NumFilter.clear()
	v.Refresh()
}

// confirmNumberFilter は Enter で入力を終える。検索語が空なら絞り込みごとやめる (issuesNumberFilter.confirm の doc)。
func (v *IssuesView) confirmNumberFilter() {
	if v.NumFilter.Query() == "" {
		v.clearNumberFilter()
		return
	}
	v.NumFilter.Confirm()
	v.Refresh()
}

// clearNumberFilter は一時 autoExpanded を捨て、解除前に見ていた子 issue のパスへカーソルを
// 張り直す。番号フィルタ中は親行を含む displayRows の添字が解除後に変わるため、添字を残しては
// ならない。現在行が auto 展開された group の子なら、解除後もその子へ着地できるよう group の
// 展開だけは手動の展開 (groups.manual) へ引き継ぐ。autoExpanded 自体は捨てるので、次の refresh で
// 番号フィルタ由来の展開は残らない (カーソルの可視性を保つための 1 group だけが例外)。
func (v *IssuesView) clearNumberFilter() {
	iss := v.current()
	path := issuePath(iss)
	groupKey := v.currentGroupKey() // 親行で Esc したときは親行へ戻す (添字を残さない)
	keep := ""
	if iss != nil {
		keep, _ = iss.EpicGroupKey()
	}
	v.endNumberFilter(keep)
	// Esc は「1 段戻る」であってカーソル移動の意思表示ではないので、直前の move の再アンカー
	// 予約 (pendingCursorPath) は捨てない。捨てるのは**カーソルを置く側**で、その責務は
	// `setCursor` / `g`・`home` / `moveTab` が直接 `clearPendingMoveAnchors()` を呼ぶ形で
	// 配線されている (issue 316 で `anchorCursor` wrapper を削除。鏡像の `anchorGroup` は
	// production 到達可能なので残っている)。
	switch {
	case groupKey != "":
		v.anchorGroupInternal(groupKey)
	case path != "":
		v.anchorCursorInternal(path)
	default:
		v.Cursor = 0 // filter 中の添字を持ち越さない
	}
}

// ownsKeys は viewer 自身がキーを解釈し切る状態か (URL ピッカー入力中 / 番号の絞り込み入力中 /
// 「次にやる」の y/N 確認中)。statusView.ownsKeys と対。
//
// 🚨 browseModel 側の U 横取り (tui.go の issuesOv.visible() ブロック) は、この状態では止める。
// 止めないと viewer が持つキー語彙を外側が奪う: URL ピッカーは
// 「印字文字はすべて検索語に流す (個別のキーを先に横取りすると、その文字を含む URL を
// 検索できなくなる)」と宣言しているのに、大文字 `U` だけが残量モーダルに化けていた
// (issue 113。`github.com/Ueno/...` のような URL を絞り込めない)。
//
// 🚨 ガードは横取りだけに掛けること: 委譲ごと飛ばすと viewer がキーを受け取れなくなる
// (status 側が実装中に踏んだ罠。status_view.go:ownsKeys の注記と同じ)。
//
// numFilter は active でなく **typing** を見る: 絞り込みが効いているだけの状態
// (数字を打ち終わった後) は通常のナビゲーションなので、U は外側で受けてよい。
// active を見ると「絞り込みを解くまで U が恒久的に死ぬ」。
//
// 🚨 numFilter を入れている理由は URL ピッカーとは別で、**今日の実利ではない** (敵対的レビューの
// 指摘。numFilter は数字しか受けないので、U が検索語になることは今は無い。むしろ入力中の U は
// 無言の no-op になる = 機能が 1 つ減る)。それでも入れているのは、
// docs/issues-viewer-spec.md が「タイトル検索を足すときは『数字以外を無視する』を外して
// 検索語へ流す形になる」と予告しているため。実装した日に同じ穴を開け直さないよう、
// 「印字入力を食うモード」としてここで一括して扱う。
// なお status 側の pager も U を無言で飲む (割当が無い) ので、この repo の既存の作法とは一貫する。
func (v *IssuesView) OwnsKeys() bool {
	return v.UrlPick.Active || v.NumFilter.Typing || v.MarkNext.Active
}

// handleKey は viewer 表示中のキーを処理する。
//
// 全画面モーダルなのでキーは全部ここで飲む (呼び出し側へ素通りさせない)。素通りさせると
// 一覧を見ている最中の u が git pull --rebase の確認を開く類の誤爆になる: 呼び出し側の
// dispatch は裸の b / u を push / pull に割り当てているため。
//
// page は画面に使える行数 (hint 行を除く)。リスト/本文に実際に使える行数はヘッダーを
// 差し引いた visibleRows で、描画と同じ値を使う (ずれると G が末尾に届かなくなる)。
func (v *IssuesView) HandleKey(key string, vp IssuesViewport) tea.Cmd {
	// このビューは browseModel なしでも駆動できる契約なので、Space の正規化も自分で通す
	// (呼び出し側の app.NormalizeSpaceKey と同じ関数。理由はそちらのコメント)
	key = app.NormalizeSpaceKey(key)
	v.FinishAnim() // 演出中のキーは即着地させる (q が効かない時間を作らないため)
	if v.Drawer.Finish() {
		v.discardBody() // 引き出しの逆再生中にキーが来たら即座に閉じ切る
	}
	// 通知は takeNotice で取り出された時点で消えるので、ここでのクリアは不要 (取り出されない
	// まま次のキーが来た場合だけ古い結果が残るが、browseModel は毎キーで取り出す)
	rows := v.VisibleRows(vp)
	// 🚨 確認モーダルは最優先で飲む (実ファイルを動かす操作なので、裏のキーを効かせない)
	if v.MarkNext.Active {
		return v.markNextKey(key)
	}
	// 🚨 URL ピッカーは他のどの割当よりも先に飲む: インクリメンタルサーチでは印字文字がすべて
	// 検索語なので、e/v (エディタ) や y (コピー) を先に処理すると "e" や "y" を含む URL を
	// 検索できない (e は example / .dev / developer に頻出するので実害が大きい)。
	if v.UrlPick.Active {
		return v.urlPickerKey(key)
	}
	// 番号入力中も同じ理由で先に飲む: 打った数字が検索語なので、一覧のキーへ素通りさせると
	// 数字キーに割当を足した瞬間に「検索語を打つと画面が動く」ことになる
	if v.NumFilter.Typing {
		return v.numberFilterKey(key, rows)
	}
	// 🚨 ジャンプモードはアクションキーより先に飲む: e / y は選択中のリンクへ効かせる (後回しにすると
	// パスが光ったまま issue 自体を開く・コピーする)。捌かないキーはモードを抜けて下へ流れる
	if v.Open != nil && v.LinkJump.Active {
		if cmd, ok := v.linkJumpKey(key, vp, rows); ok {
			return cmd
		}
	}
	// 横断キー (s / R / D) は一覧・本文のどちらからも効く (表は app.CrossTarget)。選択・絞り込みの 1 段戻りは
	// しない (別の画面を見たい意図が明確なため)。i は自分自身なので下の toggle (閉じる) に任せる。
	// 確認モーダル・URL ピッカー・番号入力中はここまで届かないので誤爆しない
	if target, ok := app.CrossTarget(key); ok && target != app.FullScreenIssues {
		v.Close()
		v.wantCross = target
		return nil
	}
	// モードに依らないアクションキーは先に飲む (対象は target() が一覧/本文で切り替える)
	if cmd, ok := v.actionKey(key); ok {
		return cmd
	}
	if v.Open != nil {
		return v.HandleBodyKey(key, vp, rows)
	}
	switch key {
	case "q", "esc":
		// 選択中は解除を優先する (tig 流に 1 段戻る)。選択したまま終了すると、再開記憶で
		// 次に開いたとき解除の手段が無い状態から始まる
		if v.Marked {
			v.ClearMark()
			return nil
		}
		// 絞り込みも同じく 1 段戻る (絞り込んだまま終了すると、次に開いたとき「なぜか件数が
		// 少ない一覧」から始まる)
		if v.NumFilter.Active {
			v.clearNumberFilter()
			return nil
		}
		// 戻る段が無ければ glogx ごと終了する (ユーザー要望 2026-08-06: git log 一覧へは
		// 戻らない。一覧へ戻りたいときは i の toggle)。quit は browseModel が行う (takeWantQuit)
		v.wantQuit = true
	case "i":
		// i は toggle で一覧へ戻る (閉じる意図が明確なので 1 段戻りはしない)
		v.Close()
	case "u":
		// 🚨 黙って無視しない。u は git log 一覧では pull、本文では URL ピッカー、status viewer では
		// 「pull は p です」を返す — 一覧だけ無音だと「押したのに何も起きない」= 壊れて見える
		// (status viewer 側で明文化されている規律。issue 122)。
		// 効かせない理由は openURLPicker の doc: 一覧で押せるようにするとキー 1 打ごとにファイルを
		// 読むことになり、しかも「その issue に URL があるか」は一覧に出ていない。
		v.setNotice("URL 一覧は本文を開いてから u で出します", app.NoticeRefused)
	case "/":
		v.NumFilter.Start() // 絞り込み中なら続きから打てる (行集合は変わらないので refresh 不要)
	// 範囲選択 (ユーザー要望 2026-08-01)。y / p / Y が選択範囲へ効く。
	// 移動が矢印と j/k の 2 系統あるので、伸張も両方に付ける (K = shift+k、J = shift+j)。
	// 🚨 矢印だけにしない: shift+矢印は端末・多重化 (tmux) の設定次第でアプリまで届かないことが
	// あり、そのとき機能ごと沈黙する。素の大文字は必ず届くので、確実に動く経路を必ず 1 本持たせる。
	case "shift+up", "K":
		v.extendMark(-1, rows)
	case "shift+down", "J":
		v.extendMark(1, rows)
	case " ":
		// group 親行の Space は開閉 (それ以外の行では移動の語彙どおり半ページ)
		if !v.toggleGroupAtCursor() {
			v.applyMotion(listnav.HalfDown, rows)
		}
	case "tab", "l", "right":
		if v.NumFilter.Active {
			break // 絞り込み中はカテゴリの概念が無い (ヘッダーもタブ行を出していない)
		}
		v.moveTab(1)
	// ctrl+b で左へ (ユーザー要望 2026-07-31)。右は tui.go が ctrl+f を "right" へ正規化するので
	// 既に効いており、ここで足すのは対になる左だけ。C-b を「←の別名」として全ビューに広げないのは
	// 一覧・パネル側の left に別の意味を与えないため (tui.go の C-f 正規化の注記を参照)。
	case "shift+tab", "h", "left", "ctrl+b":
		if v.NumFilter.Active {
			break
		}
		v.moveTab(-1)
	case "enter":
		if v.toggleGroupAtCursor() {
			break
		}
		v.openBody()
	case "o":
		v.openBody()
	case "n":
		v.askMarkNext()
	case "a":
		if v.NumFilter.Active {
			break // 番号検索は状態を無視するので、押しても画面が変わらない (裏で状態だけ変える方が悪い)
		}
		v.Filter = v.Filter.Next()
		v.Refresh()
	case "r":
		return v.ScanCmd(v.Cwd) // loaded は落とさない (取り直し中も前回の結果を出したままにする)
	default:
		v.applyMotion(listnav.MotionOf(key), rows) // 移動の語彙 (j/k/g/G/半ページ) は listnav が持つ
	}
	return nil
}

// applyMotion は一覧のカーソル移動を適用する。半ページはカーソルの描画位置だけを滑らせる
// (movePage)。端へのジャンプは、範囲選択と移動待ちのアンカーを捨ててから飛ぶ。
func (v *IssuesView) applyMotion(m listnav.Motion, rows int) {
	switch m {
	case listnav.Down:
		v.moveCursor(1, rows)
	case listnav.Up:
		v.moveCursor(-1, rows)
	case listnav.HalfDown:
		v.movePage(listnav.Half(rows), rows)
	case listnav.HalfUp:
		v.movePage(-listnav.Half(rows), rows)
	case listnav.Top:
		v.clearPendingMoveAnchors()
		v.ClearMark()
		v.Cursor, v.offset = 0, 0
	case listnav.Bottom:
		v.clearPendingMoveAnchors()
		v.ClearMark()
		v.EnsureDisplayRows()
		v.Cursor = max(len(v.displayRows)-1, 0)
		v.scrollToCursor(rows)
	case listnav.None:
	}
}

// numberFilterKey は番号を入力しているあいだのキー。
//
// 🚨 数字と編集キー以外の印字文字は捨てる (無視して入力を続ける)。一覧のキーとして実行しない
// のは issuesNumberFilter の doc の理由による。
func (v *IssuesView) numberFilterKey(key string, rows int) tea.Cmd {
	switch key {
	case "esc", "ctrl+g":
		v.clearNumberFilter()
	case "enter":
		v.confirmNumberFilter()
	case "down", "ctrl+n":
		v.moveCursor(1, rows)
	case "up", "ctrl+p":
		v.moveCursor(-1, rows)
	default:
		if v.NumFilter.Edit(key) {
			v.Cursor = 0 // 絞り込み直後は先頭を見せる (urlPicker と同じ)
			v.Refresh()
		}
	}
	return nil
}

// actionKey は一覧・本文の両モードで同じ意味を持つキー (対象は target() が決める)。
// 処理したら handled=true。
//
// モードごとの switch に写すのをやめて 1 箇所に寄せている: 二重に持つとキーを 1 本足すたびに
// 2 箇所を編集する必要があり、片方に入れ忘れても「そのモードでだけ効かない」だけなので
// テストは緑のまま通る (実際に本文モードのコピーが無通知だったのもこの二重化の側で起きた)。
func (v *IssuesView) actionKey(key string) (tea.Cmd, bool) {
	if v.Open == nil && v.currentIsGroup() {
		switch key {
		case "y":
			// 合成の親行に issue は無いが、**group 名という identity はある**ので y だけは
			// 意味を持たせる (ユーザー要望 2026-09-08)。no-op のままだと、epic のパスを組む
			// ときや grep するときに画面の名前を目で読んで打ち直すことになる。
			// 統合した親 issue 行 (groupHead) はここへ来ない — あちらは実体が issue なので
			// y は従来どおりパスをコピーする。
			v.copyGroupName()
			return nil, true
		case "v", "e", "p", "Y", "N":
			v.setNotice("親行では issue 操作はできません", app.NoticeRefused)
			return nil, true
		}
	}
	switch key {
	case "v", "e":
		// e は git log 一覧の e (nvim を repo root で開く) と語彙を揃えたもの。v は先にあった
		// 割当で、打ち慣れを壊さないため残す (本文モードの hint が案内するのは e だけ)。
		//
		// 🚨 閉じる演出中 (issuesCloseDuration) の e はここへ来ない: tui.go が
		// finishClose で viewer を畳んでから通常のキー処理へ素通しするため、git log 一覧側の
		// e (openEditorAtRoot = `nvim .`) に着弾する。板がまだ見えているのに repo root が
		// 全画面で開くので誤爆の体感は軽くない。それでも素通しから e を外していないのは、
		// tui.go が「演出中のキーを飲まない」を明記した設計判断として持っているため
		// (飲むと q で閉じた直後の q が効かない窓ができる)。窓を閉じたくなったら、
		// この case ではなく tui.go の素通し側の判断を変えること。
		return v.editCmd(), true
	case "y":
		v.copyPath()
	case "p":
		v.copyNumber()
	case "Y":
		v.copyReference()
	case "N":
		v.copyNextNumber()
	default:
		return nil, false
	}
	return nil, true
}

// visibleRows は窓のうちリスト/本文に使える行数 (ヘッダーを差し引く)。
//
// 🚨 描画側 (listLines / bodyLines) と同じ式・同じ幅で数えること。片方だけ幅が違うと、幅で
// 折り返すヘッダーを足した瞬間にキー側と描画側の page 分割が食い違い、半ページ移動の距離や
// カーソルと窓の関係が静かにずれる (描画側には収束処理があるので症状から原因へ辿り着けない)。
// 一致は TestIssuesLayoutAgreesBetweenKeysAndRender が固定する。
func (v *IssuesView) VisibleRows(vp IssuesViewport) int {
	return max(vp.Page-len(v.headLines(v.headWidth(vp.Width), false)), 1)
}

// headWidth はヘッダーを組む幅。本文は引き出しの内側に描くので、そちらの幅で数える。
// 本文のヘッダーの行数は今は幅によらない (bodyHeadLines は 3 行固定) が、linkJump の状態文は幅で
// リンクを組み直すので、描画と同じ幅を渡す (行数がずれる形が足されたときに備える意味もある。issue 751)
func (v *IssuesView) headWidth(total int) int {
	if v.Open != nil {
		return v.bodyWidth(total)
	}
	return total
}

// bodyWidth は本文を組む幅 (引き出しの内側)。🚨 演出中の途中幅ではなく着地後の幅で組む:
// 途中幅で整形し直すと毎フレーム折り返しが変わって文字が踊る (composeDrawer の doc)。
func (v *IssuesView) bodyWidth(total int) int { return max(v.Drawer.TargetWidth(total)-1, 1) }

// handleBodyKey は本文 pager のキー操作 (diffOverlay と同じ語彙)。
func (v *IssuesView) HandleBodyKey(key string, vp IssuesViewport, rows int) tea.Cmd {
	switch key {
	// Enter は「TUI 内の開閉 toggle」(ユーザー要望 2026-08-01)。一覧の Enter で開き、本文の
	// Enter で閉じる。glogx 本体の job パネル (tui.go の handlePanelKey) が既にこの語彙なので、
	// viewer だけ Enter が行送りだと同じキーの意味が画面ごとに変わる。
	// 🚨 term.PagerScrollKey へ渡す前に捌くこと: あちらは enter を 1 行送りに写す。
	case "q", "esc", "h", "left", "enter":
		if v.popDoc() {
			break // doc を開いていたら 1 段戻る (元の本文へ)
		}
		v.CloseBody()
	case "i":
		// i は本文からも効く (一覧の i と同じ toggle。**s と同じ理由**: --help と README が
		// 「viewer 内のキー」として i を案内しており、本文だけ沈黙すると案内が嘘になる。issue 122)。
		// 🚨 本文だけ畳む 1 段戻りにはしない: それは Enter / q / h が既に持っている語彙で、
		//   README の「i で閉じて一覧へ戻る」とも食い違う。
		v.Close()
	case "u":
		v.openURLPicker()
	case "tab":
		// 本文中のファイルパスのジャンプモード (ユーザー要望 2026-09-28)。w3m / lynx / ブラウザの
		// 「Tab = 次のリンク」。一覧の Tab はカテゴリ移動だが、本文ではカテゴリが無いので衝突しない
		v.startLinkJump(vp, rows)
	// 本文を開いたまま隣の issue へ (ユーザー要望 2026-09-05)。小文字 j/k が「行」なら大文字は
	// 「項目」(docs/glogx-ui-guide.md §6)。一覧の J/K は範囲選択の伸張だが本文に選択は無いので
	// 衝突しない。shift+矢印も受けるのは一覧側と同じ理由 (tmux 越しで届かない端末があるので、
	// 確実に届く大文字を必ず 1 本持たせる。逆に矢印しか押さない人にも同じ操作を用意する)。
	case "J", "shift+down":
		v.openNeighbor(1, rows)
	case "K", "shift+up":
		v.openNeighbor(-1, rows)
	default:
		// スクロールの語彙 (1 行 / 半ページ + glide / 端ジャンプ) は diffOverlay・status viewer の
		// 全画面 diff と共有する (internal/term/scroll_glide.go の term.PagerScrollKey)。手触りを 1 箇所に集約するため。
		term.PagerScrollKey(key, &v.BodyPager, rows, v.Body.Len())
	}
	return nil
}

// movePage は半ページ移動 (Space / ctrl+d / b / ctrl+u / pgup / pgdown)。移動そのものは
// moveCursor と同じで、描画カーソルだけを数フレーム滑らせる (cursorGlide)。
//
// 🚨 1 行移動 (j/k) と端ジャンプ (g/G) には載せない。前者は距離 1 で滑らせる意味が無く連打の
// 体感を損ね、後者は「飛ぶ」ことがそのまま操作の意味 (本文 pager の term.PagerScrollKey が
// 半ページだけ glide に載せているのと同じ線引き)。
func (v *IssuesView) movePage(delta, rows int) {
	v.EnsureDisplayRows()
	from := v.Cursor
	v.moveCursor(delta, rows)
	v.CurGlide.Start(from, v.Cursor, term.CursorAnimFrames)
}

// moveCursor はカーソルを動かしてスクロール位置を追従させる。素の移動は選択を解除する
// (選択したまま離れた場所へ動くと「見えていない範囲がコピー対象」になる)。
func (v *IssuesView) moveCursor(delta, rows int) {
	v.ClearMark()
	v.setCursor(v.Cursor+delta, rows)
}

// setCursor はカーソルを位置 i へ置いて窓を追従させる (選択には触れない)。
func (v *IssuesView) setCursor(i, rows int) {
	v.clearPendingMoveAnchors()
	v.EnsureDisplayRows()
	v.Cursor = app.ClampIdx(i, len(v.displayRows))
	v.scrollToCursor(rows)
}

// extendMark は shift+↑/↓ の伸張。初回は今の行を錨にしてから動くので、1 回押すと
// 「元の行 + 隣の行」の 2 行が選択される (エディタ・Finder と同じ)。
func (v *IssuesView) extendMark(delta, rows int) {
	v.EnsureDisplayRows()
	if len(v.displayRows) == 0 {
		return
	}
	if !v.isIssueDisplayRow(v.Cursor) {
		v.setNotice("親行では issue を選択できません", app.NoticeRefused)
		return
	}
	next := app.ClampIdx(v.Cursor+delta, len(v.displayRows))
	if !v.isIssueDisplayRow(next) {
		v.setNotice("親行は選択できません", app.NoticeRefused)
		return
	}
	if !v.Marked {
		v.Marked, v.MarkAt = true, v.Cursor
	}
	v.setCursor(next, rows)
}

// clearMark は選択を解除する。
func (v *IssuesView) ClearMark() { v.Marked, v.MarkAt = false, 0 }

// selection は選択範囲 [lo, hi] (両端を含む)。選択していなければ ok=false。
// 🚨 錨は行集合の入れ替えで無効になりうるので、範囲は必ず今の rows へ収めてから返す。
func (v *IssuesView) Selection() (lo, hi int, ok bool) {
	v.EnsureDisplayRows()
	if !v.Marked || len(v.displayRows) == 0 {
		return 0, 0, false
	}
	lo, hi = min(v.MarkAt, v.Cursor), max(v.MarkAt, v.Cursor)
	lo, hi = max(lo, 0), min(hi, len(v.displayRows)-1)
	for i := lo; i <= hi; i++ {
		if !v.isIssueDisplayRow(i) {
			return 0, 0, false
		}
	}
	return lo, hi, true
}

// selectedRows は操作の対象 (選択中ならその範囲、選択していなければ対象 1 件)。
func (v *IssuesView) selectedRows() []*issues.Issue {
	if lo, hi, ok := v.Selection(); ok {
		rows := make([]*issues.Issue, 0, hi-lo+1)
		for i := lo; i <= hi; i++ {
			rows = append(rows, v.displayRows[i].issue)
		}
		return rows
	}
	if iss := v.target(); iss != nil {
		return []*issues.Issue{iss}
	}
	return nil
}

// scrollToCursor はカーソルが画面内に入るまで offset を寄せる。
func (v *IssuesView) scrollToCursor(rows int) { v.offset = v.windowOffset(rows) }

// windowOffset は一覧の描画開始行 (論理 offset)。カーソルを必ず含み、末尾では余白を作らない
// 位置へ寄せる。
//
// 🚨 offset は独立した状態ではなく (cursor, 行数, 表示行数) からの導出値として扱う。表示行数は
// キー処理時と描画時でずれる: 通知行が出てヘッダーが 1 行増える / リサイズで page が変わる /
// タブ・フィルタ切替で行数が変わる。offset を状態として持ち回ると、そのずれが「カーソル行が
// 1 本も描かれず、見えない行が Enter・v・y の対象になる」窓として残る。導出を
// scrollToCursor (キー) と listLines (描画) の両方が通すことで食い違いを構造的に消す。
// dispCursor は描画に使うカーソル行 (滑走中は途中位置、それ以外は論理カーソル)。
//
// 🚨 窓 (offset) は滑走に載せない。窓は論理カーソルを含む最小の窓の導出値のままで、動かすのは
// 表示カーソルだけ。だから「窓は論理カーソルを必ず含む」(issue 031 の不変条件) が滑走中も生き、
// 見えていない行が Enter/y/v の対象になることが構造的に起きない。代わりに表示カーソルの方を窓へ
// 収める — 半ページ移動では起点も着地点も着地後の窓に入るので通常は素通りし、行き過ぎ
// (ease-out-back) が窓の端を越えたときだけ端で止まる。
func (v *IssuesView) dispCursor(offset, rows int) int {
	v.EnsureDisplayRows()
	cur := app.ClampIdx(v.CurGlide.Cursor(v.Cursor), len(v.displayRows))
	if !v.CurGlide.Active() || rows <= 0 {
		return cur
	}
	return min(max(cur, offset), offset+rows-1)
}

func (v *IssuesView) windowOffset(rows int) int {
	if rows <= 0 {
		return 0
	}
	v.EnsureDisplayRows()
	return listnav.WindowOffset(v.offset, v.Cursor, len(v.displayRows), rows)
}

// moveTab はタブを切り替える (端で止まらず巡回する)。
func (v *IssuesView) moveTab(delta int) {
	v.clearPendingMoveAnchors()
	// [next] (-1) と All (0) を含めた巡回。-1 起点なので +1 して 0 起点へ寄せてから回す
	n := len(v.tabs) + 2
	v.tabIdx = listnav.Cycle(v.tabIdx+1, delta, n) - 1
	v.Refresh()
}

// target は操作対象の issue (本文モードなら開いているもの、一覧ならカーソル行)。
func (v *IssuesView) target() *issues.Issue {
	if v.Open != nil {
		return v.Open
	}
	return v.current()
}

// editCmd は対象の issue を $VISUAL / $EDITOR (未設定なら nvim) で開く。job ログ (scratch
// バッファ) と違い実ファイルなので readonly にしない: viewer から直接メモを足したくなるため。
//
// エディタの解決を extcmd.EditorCommand に寄せているのは、これが glogx で唯一「実ファイルを 1 つ開く」
// 経路で、任意の $EDITOR で成立するため (据え置いた 2 箇所の理由は extcmd.EditorCommand の doc)。
// 🚨 起動前に実体を確かめる。一覧が握る Issue.Path は n (next/ へ移動) や別プロセスの
// rename/削除で stale になり、そのパスを渡すとエディタは黙って**新規バッファ**として開く
// (nvim はエラーにしない)。そこで保存すると旧位置にファイルが復活し、issues/move.go が
// 「同じ basename を 2 箇所に作らない」と宣言している不変条件を viewer 自身が破る。
// 開かずに取り直す方に倒す (古い一覧のまま編集させない)。
func (v *IssuesView) editCmd() tea.Cmd {
	iss := v.target()
	if iss == nil {
		return nil
	}
	if _, err := os.Stat(iss.Path); err != nil {
		v.setNotice("実体が見つかりません (一覧を取り直します): "+iss.Rel, app.NoticeRefused)
		return v.scanAfterChangeCmd()
	}
	return extcmd.RunEditorCmd(extcmd.EditorCommand(iss.Path))
}

// openURLPicker は本文中の URL のピッカーを開く (u キー)。URL が無ければ開かずに通知する。
//
// 一覧モードでは効かない (本文を読んでいないため)。一覧で押せるようにするとキー 1 打ごとに
// ファイルを読むことになり、しかも「その issue に URL があるか」は一覧に出ていない。
func (v *IssuesView) openURLPicker() {
	if v.Body == nil {
		return
	}
	if !v.UrlPick.Open(v.Body.URLs()) {
		v.setNotice("この本文に URL はありません", app.NoticeRefused)
	}
}

// urlPickerKey はピッカー表示中のキーを捌く。確定 (Enter) でブラウザを開く Cmd を返す。
func (v *IssuesView) urlPickerKey(key string) tea.Cmd {
	url := v.UrlPick.selected() // 確定前に読む (close で状態が消えるため)
	open, _ := v.UrlPick.handleKey(key)
	if !open || url == "" {
		return nil
	}
	v.UrlPick.Close()
	// 「開きました」と断定しない: 失敗は extcmd.OpenURLMsg 経由でトースト警告になる (browseModel 側)。
	v.setNotice("URL を開きます: "+url, app.NoticeOK)
	return func() tea.Msg { return extcmd.OpenURLMsg{Err: extcmd.OpenInBrowser(url)} }
}

// copyPath は対象の issue のパスをクリップボードへ入れる (選択中は範囲ぶん)。
func (v *IssuesView) copyPath() {
	v.copyEach("パス", func(iss *issues.Issue) string { return iss.Path })
}

// copyNumber は issue 番号をコピーする (p)。番号は rename も move も生き残る唯一安定した
// 参照形式で、実測でも repo 内 59 箇所・commit message 25 件がこの形。
//
// 番号を持たない issue (素スラッグ。実測で SnapTrim に 4 件) では黙って空をコピーせず、
// ファイル名に落として「番号が無い」ことを通知する。
func (v *IssuesView) copyNumber() {
	rows := v.selectedRows()
	if len(rows) == 0 {
		return
	}
	lines := make([]string, 0, len(rows))
	fellBack := false
	for _, iss := range rows {
		id := iss.Ident() // CATEGORY-NNN 形式は接頭辞まで含む ("UI-005"。理由は Ident)
		if id == "" {
			fellBack = true
			id = filepath.Base(iss.Rel)
		}
		lines = append(lines, id)
	}
	label := "番号"
	switch {
	case fellBack && len(lines) == 1:
		label = "番号が無いのでファイル名"
	case fellBack:
		label = "番号 (番号なしはファイル名)"
	}
	v.copyLines(lines, label)
}

// copyReference は貼り付け用の 1 行参照をコピーする (Y)。番号 + タイトル + repo 相対パス。
func (v *IssuesView) copyReference() {
	v.copyEach("参照", func(iss *issues.Issue) string { return iss.Reference(v.Root) })
}

// copyEach は対象 (選択中なら範囲、なければ 1 件) から text() を作ってコピーする。
func (v *IssuesView) copyEach(label string, text func(*issues.Issue) string) {
	rows := v.selectedRows()
	if len(rows) == 0 {
		return
	}
	lines := make([]string, 0, len(rows))
	for _, iss := range rows {
		lines = append(lines, text(iss))
	}
	v.copyLines(lines, label)
}

// copyLines は複数行をまとめてコピーする。1 件のときの文言は単数のまま変えない
// (複数選択を足したせいで、いつもの操作の見た目が変わらないように)。
//
// 🚨 通知には全文を載せない: トーストは 1 行で、改行を含む文字列を渡すと枠が壊れる。
// クリップボードには全件を改行区切りで入れ、通知は件数 + 先頭だけにする。
func (v *IssuesView) copyLines(lines []string, label string) {
	if len(lines) == 1 {
		v.copyText(lines[0], label+"をコピーしました: ")
		return
	}
	for _, l := range lines { // 1 件は copyText が断る。まとめて入れる経路も同じく断る (issue 753)
		if !term.IsCopySafe(l) {
			v.refuseUnsafeCopy(label)
			return
		}
	}
	if err := extcmd.CopyToClipboard(strings.Join(lines, "\n")); err != nil {
		v.setNotice(app.ClipboardFailText(err), app.NoticeRefused)
		return
	}
	v.setNotice(strconv.Itoa(len(lines))+" 件の"+label+"をコピーしました: "+lines[0]+" ほか", app.NoticeOK)
}

// copyGroupName は合成の group 親行の名称をコピーする (y)。
//
// 🚨 画面用に無害化した名前 (groupLine の term.SanitizePlainLine) ではなく **生の GroupName** を
// クリップボードへ入れる。貼り先は実在するディレクトリ名でなければならない
// (`issues/epic/<name>/` を組む・grep する) ので、書き換えると別物になるため
// (termsafe.IsPlain の doc「同一性を持つ値は書き換えず落とす」と同じ理由)。
//
// 🚨 制御文字を含む名前は書き換えて入れるのではなく、コピー自体を断る (copyText。issue 753)。
// 一覧の表示 (無害化) を生表示へ直さないこと (termsafe が防いでいる端末破壊の再導入になる)。
func (v *IssuesView) copyGroupName() {
	row, ok := v.currentDisplayRow()
	if !ok || row.kind != displayRowGroup {
		return
	}
	v.copyText(row.groupName, "group 名をコピーしました: ")
}

// copyNextNumber は次に採番すべき番号をコピーする (N)。走査済みの全ディレクトリから計算する
// ので、状態ディレクトリを見落として番号を再利用する事故が起きない (issues.NextNumber)。
func (v *IssuesView) copyNextNumber() {
	if len(v.All) == 0 {
		return
	}
	v.copyText(issues.NextNumber(v.All), "次の番号をコピーしました: ")
}

// copyText はクリップボードへ入れて結果を通知する (コピー系アクションの共通処理)。
//
// 🚨 制御文字を含む値はコピーしない (書き換えもしない)。ファイル名や group 名は PR で入りうる外部由来の値で、
// ESC や bracketed paste の終わり (ESC[201~) をクリップボードへ入れると、貼った先の端末がそれを解釈する。
// 画面側は無害化されているので、利用者には見えない (issue 753)。書き換えると実在の名前と食い違うので、
// 落として断る側に倒す (termsafe.IsPlain の doc。判定は term.IsCopySafe でタブだけは通す)。
func (v *IssuesView) copyText(text, okPrefix string) {
	if !term.IsCopySafe(text) {
		v.refuseUnsafeCopy(strings.TrimSuffix(okPrefix, "をコピーしました: "))
		return
	}
	if err := extcmd.CopyToClipboard(text); err != nil {
		v.setNotice(app.ClipboardFailText(err), app.NoticeRefused)
		return
	}
	v.setNotice(okPrefix+text, app.NoticeOK)
}

// refuseUnsafeCopy は制御文字を含む値のコピーを断ったことを知らせる (copyText の doc)。
func (v *IssuesView) refuseUnsafeCopy(label string) {
	v.setNotice(label+"に制御文字が含まれるのでコピーしません (貼り付け先の端末が解釈するため)", app.NoticeRefused)
}

// カテゴリの色。意味が広く共有されている語には固定色を割り、表に無い語は語のハッシュで
// 安定に (起動ごとに変わらないように) 割る。カテゴリ語彙は repo ごとに違い、実測で
// 「変更種別 19 語 / サブシステム名体系 / トークンなし」が併存するので表だけでは足りない。
//
// 色番号は 256 色 (docs/theme-colors.md の 256 色主環境・gruvbox 基調に合わせた bright 系)。
const (
	catRed    = "\x1b[38;5;167m" // bug / fix / security — 失敗系の赤 (本体の term.AnsiRed と同じ意味)
	catGreen  = "\x1b[38;5;142m" // feat
	catTeal   = "\x1b[38;5;73m"  // refactor / cleanup / chore (feat の緑と混ざらない青緑)
	catYellow = "\x1b[38;5;214m" // perf
	catBlue   = "\x1b[38;5;109m" // test / ci / lint / build
	catPurple = "\x1b[38;5;175m" // research / design
	catGold   = "\x1b[38;5;179m" // ux / ui (perf の橙と混ざらない金)
	catGray   = "\x1b[38;5;245m" // docs / other
)

var categoryColors = map[string]string{
	"bug": catRed, "fix": catRed, "security": catRed, "hotfix": catRed,
	"feat": catGreen, "feature": catGreen,
	"refactor": catTeal, "cleanup": catTeal, "chore": catTeal,
	"perf": catYellow,
	"test": catBlue, "ci": catBlue, "lint": catBlue, "build": catBlue, "e2e": catBlue,
	"research": catPurple, "design": catPurple, "retro": catPurple,
	"ux": catGold, "ui": catGold,
	"docs": catGray, "doc": catGray, issues.OtherTab: catGray,
}

// catHashPalette は表に無いカテゴリ語へ割る色。
//
// 🚨 赤を入れない: 意味を持たない語 (サブシステム名など) が「失敗」の色で出ると誤読される。
// 赤は bug / fix / security に予約する。
var catHashPalette = []string{catGreen, catTeal, catYellow, catBlue, catPurple, catGold, catGray}

// categoryColor は語に対する色を返す (同じ語なら常に同じ色)。カテゴリ無しは dim。
func categoryColor(name string) string {
	if name == "" {
		return term.AnsiDim
	}
	if c, ok := categoryColors[name]; ok {
		return c
	}
	// FNV-1a (32bit)。hash/fnv を持ち込まずに済む短さで、語 → 色を決定的に割るだけの用途
	h := uint32(2166136261)
	for i := range len(name) {
		h = (h ^ uint32(name[i])) * 16777619
	}
	return catHashPalette[h%uint32(len(catHashPalette))]
}

// issuesRenderOpts は描画に必要な外側の情報。
// issuesViewport は「今この窓は何桁 × 何行か」。キー処理と描画が同じ値から page を分割するための型。
//
// キー処理に描画の都合 (色・カーソル強調・スピナー) まで渡さないよう、issuesRenderOpts とは
// 別の型にしてある。🚨 幅を落とさないこと: 幅を知らずに page を分割していた頃は、ヘッダーを
// 幅 0 で数えるしかなく「ヘッダーは折り返してはいけない」という暗黙の前提を抱えていた。
type IssuesViewport struct {
	Width int
	Page  int
}

type IssuesRenderOpts struct {
	Width   int
	Page    int
	Colored bool
	Spinner string
	// cursorPaint はカーソル行の強調 (browseModel の bgLine を渡す)。nil なら太字だけで示す
	// = テストと NO_COLOR 用。
	CursorPaint func(string) string
}

// viewport は描画情報から窓の寸法だけを取り出す (キー処理へ渡す形)。
func (o IssuesRenderOpts) Viewport() IssuesViewport {
	return IssuesViewport{Width: o.Width, Page: o.Page}
}

// lines は全画面ビューの page 行を返す (常にちょうど page 行。呼び出し側の枠・hint 経路を
// 変えずに差し替えられるようにするため)。
func (v *IssuesView) Lines(o IssuesRenderOpts) []string {
	v.settleDrawer() // 閉じ切っていたら本文を捨ててから組む (描画とキーのどちらが先でも進む)
	var body []string
	switch {
	case v.UrlPick.Active:
		body = v.UrlPick.lines(o)
	case v.Open != nil:
		// 本文は「一覧の上に左から開く引き出し」として重ねる。全画面で置き換えると、どの一覧の
		// どこから開いたかが画面から消える (ユーザー要望 2026-07-31: Notion の peek のように)。
		w := v.Drawer.width(o.Width, app.TimeNow())
		inner := o
		// 整形は最終幅で行い、演出中は切るだけにする (途中幅で整形し直すと毎フレーム折り返しが
		// 変わって文字が踊る。詳細は composeDrawer の doc)
		inner.Width = v.bodyWidth(o.Width)
		body = layout.ComposeDrawer(layout.PadTo(v.ListLines(o), o.Page), layout.PadTo(v.bodyLines(inner), o.Page),
			w, o.Width, o.Colored)
	default:
		body = v.ListLines(o)
	}
	body = layout.PadTo(body, o.Page)
	// 確認モーダルは最前面 (実ファイルを動かす操作なので、裏の一覧より確実に目に入る位置)
	if box := v.markNextBox(o.Width, o.Colored); len(box) > 0 {
		body = term.OverlayCenteredBox(body, box, o.Width, o.Page, o.Colored)
	}
	if p := v.animProgress(); p < 1 {
		body = layout.SlideIn(body, p, o.Width, v.Closing, IssuesAnimStagger)
	}
	return body
}

// animProgress は開く演出の進み (0..1)。演出していないときは 1 (= 変形しない)。
//
// 閉じるときは進捗を 1 → 0 へ落とす (所要も別で、issuesCloseDuration の方が短い)。🚨 反転する
// のは進捗の向きだけで、見え方 (緩急・行ごとのずらし) まで開く演出の逆再生にはしない
// (理由は tuikit layout.RowOffsetRatio の doc)。
func (v *IssuesView) animProgress() float64 {
	if v.Closing {
		return 1 - anim.Elapsed(v.AnimStart, app.TimeNow(), IssuesCloseDuration)
	}
	return anim.Elapsed(v.AnimStart, app.TimeNow(), issuesAnimDuration)
}

// headLines は現在のモードのヘッダー行 (一覧ならタブ行 + 通知、本文ならパス + 状態)。
// キー操作側 (visibleRows) と描画側で同じ関数を通すことで行数のずれを防ぐ。width=0 は
// 行数だけが必要な呼び出し (中身は使われない)。
// headLines はモードに応じたヘッダー行。
//
// 🚨 返す行数は width に依らないこと (幅で折り返さず term.ClipToWidth で切る)。キー処理は描画幅を
// 知らないまま幅 0 で数えている (visibleRows) ため、幅で行数が変わるヘッダーを足すと page の
// 分割がキー側と描画側で食い違う。折り返したいヘッダーが要るなら、まず visibleRows へ幅を
// 通す経路を作ること (handleKey のシグネチャまで届く変更になる)。
func (v *IssuesView) headLines(width int, colored bool) []string {
	if v.Open != nil {
		return v.bodyHeadLines(width, colored)
	}
	return v.listHeadLines(width, colored)
}

// bodyHeadLines は本文モードのヘッダー (ファイル名 + 状態)。
//
// 操作結果 (コピー・URL 起動) の行はここに持たない: browseModel が takeNotice で取り出して
// 右下トーストに出す (ユーザー要望 2026-07-31)。以前は「トーストは全画面差し替えの下に隠れる」
// ためここが唯一の受け皿だったが、viewer の上にもトーストを合成するようにして解消した。
func (v *IssuesView) bodyHeadLines(width int, colored bool) []string {
	status := v.Open.StatusLabel()
	// 進捗は開いている本文から数える (Issue 側で持つと一覧を出すたびに全 issue の全文を
	// 読むことになる。issues/body.go の Body.Progress の doc)。
	//
	// 🚨 鮮度は v.body と同じで、それ以上ではない: 取り直しが失敗したとき
	// (reloadAfterEdit は err == nil のときだけ v.body を差し替える) は本文も進捗も
	// 古いまま残る。本文テキストの stale は以前からある性質で、進捗をここへ移したことで
	// **本文と進捗の鮮度が揃った** (以前は Issue 側の進捗だけ別経路で更新されていた)
	if v.Body != nil {
		if p := v.Body.Progress(); p != "" {
			status += "  " + p
		}
	}
	if len(v.docStack) > 0 {
		// doc を開いている: issue の状態の代わりに戻り先を出す (状態・進捗は doc には無い)
		status = "doc " + strconv.Itoa(len(v.docStack)) + " 段目  h/Esc: " + term.SanitizePlainLine(v.docStack[len(v.docStack)-1].open.Rel) + " へ戻る"
	}
	if v.LinkJump.Active && v.Body != nil {
		// ジャンプ中は状態の行を「どこを開くか」に差し替える (行を足すと本文の位置が 1 行ずれる)
		fl := v.Body.FileLinks(v.bodyTextWidth(width), v.jumpRepos())
		if cur := v.LinkJump.reanchor(fl); cur >= 0 {
			status = v.linkJumpStatus(fl, cur)
		}
	}
	return []string{
		// Rel はファイル名 = 外部由来。ファイルを開く同一性は v.open.Path 側が持つので、
		// 画面に出すこちらだけ無害化する (worktreeRow.dispPath と同じ分け方)。
		term.Paint(term.ClipToWidth(term.SanitizePlainLine(v.Open.Rel), width), term.AnsiBold, colored),
		term.Paint(term.ClipToWidth(status, width), term.AnsiDim, colored),
		"",
	}
}

// listHeadLines は一覧のヘッダー (タブ + スキャン警告)。
//
// 🚨 headLines と分けているのは引き出しのため: 本文を開いている間も下地の一覧はタブを出す
// 必要があり、headLines をそのまま使うと下地にまで本文のヘッダーが出る (実測で「一覧の上に
// 本文のファイル名が乗る」表示になった)。
//
// スキャン警告 (同名ファイルの二重化 = 静かな内容喪失) はここに残す: 操作結果と違って
// 「今この repo が抱えている状態」なので、消えるトーストでなく画面に出続ける必要がある。
// 操作結果はトーストへ移した (takeNotice)。
func (v *IssuesView) listHeadLines(width int, colored bool) []string {
	// 絞り込み中はタブ行を検索行に差し替える (両方出さない: タブは無視される概念なので、
	// 並べると「どのタブの中を検索しているのか」という無い関係を読ませてしまう)
	head := make([]string, 0, 3)
	if v.NumFilter.Active {
		head = append(head, v.numberFilterLine(width, colored))
	} else {
		head = append(head, v.tabLine(IssuesRenderOpts{Width: width, Colored: colored}))
	}
	if len(v.warnings) > 0 {
		w := "🚨 " + v.warnings[0] // 先頭がいちばん重い (issues.Scan が並べる)
		if n := len(v.warnings) - 1; n > 0 {
			w = "🚨 (ほか " + strconv.Itoa(n) + " 件) " + v.warnings[0] // 件数を先に置く (幅で切られても残る)
		}
		head = append(head, term.Paint(term.ClipToWidth(w, width), term.AnsiYellow, colored))
	}
	return append(head, "")
}

// listLines はタブ + 警告 + リストを描く。
func (v *IssuesView) ListLines(o IssuesRenderOpts) []string {
	v.EnsureDisplayRows()
	head := v.listHeadLines(o.Width, o.Colored) // 引き出しの下地でも一覧のタブを出す
	if msg := v.emptyMessage(o); msg != "" {
		return append(head, term.Paint(term.ClipToWidth(msg, o.Width), term.AnsiDim, o.Colored))
	}
	rows := max(o.Page-len(head), 1)
	// 論理 offset を「描画時の行数でカーソルを含む窓」へ収束させる (キー処理時の行数と
	// ずれていても窓とカーソルが食い違わない)。窓はこの導出値そのままなので、カーソルは
	// 定義上必ず含まれる。
	v.offset = v.windowOffset(rows)
	offset := v.offset
	end := min(offset+rows, len(v.displayRows))
	dispCur := v.dispCursor(offset, rows)
	out := make([]string, 0, rows)
	for i := offset; i < end; i++ {
		// バー列ぶんを先に引く: 幅ぴったりに組むと scrollbarColumn のクリップで末尾 1 文字が
		// "…" に化ける (tuikit layout.ScrollbarWidth)
		out = append(out, v.rowLine(i, dispCur, o, o.Width-layout.ScrollbarWidth))
	}
	out = layout.Scrollbar(out, o.Width, len(v.displayRows), offset, o.Colored)
	return append(head, out...)
}

// emptyMessage は「出すものが無い」状態の案内 ("" = リストを描く)。
func (v *IssuesView) emptyMessage(o IssuesRenderOpts) string {
	switch {
	case v.Scanning && !v.Loaded:
		// スピナーは初回だけ。取り直しの間は前回の結果を出したままにする (last-good)。開くたびに
		// 再スキャンするので、ここでスピナーに落とすと開く瞬間に毎回一覧が消えて瞬く
		// (usage overlay の「取得中も last-good を維持する」と同じ規律)。
		return o.Spinner + " issues を探しています..."
	case len(v.Dirs) == 0:
		return "issues ディレクトリが見つかりません (repo root と root/*/issues を探しました)"
	case len(v.Rows) == 0 && v.NumFilter.Active && v.NumFilter.Query() != "":
		// 🚨 状態フィルタの案内 (a: pending も表示) を出さない。番号検索は状態を無視しているので、
		// a を押しても結果は 1 件も増えない
		return "番号に「" + v.NumFilter.Query() + "」を含む issue はありません (Esc: 解除)"
	case len(v.Rows) == 0 && v.Filter == issues.FilterOpen:
		return "このタブに open の issue はありません (a: pending も表示)"
	case len(v.Rows) == 0 && v.Filter == issues.FilterPending:
		return "このタブに open / pending の issue はありません (a: done も表示)"
	case len(v.Rows) == 0:
		return "このタブに issue はありません"
	default:
		return ""
	}
}

// numberFilterLine は番号で絞り込み中のヘッダー (タブ行の代わり)。
//
// 「全カテゴリ・全状態」を必ず出す: 番号検索はタブと状態フィルタの両方を無視するので、書かないと
// 「今 open だけを見ているはず」という直前までの文脈のまま done の issue が並ぶことになる。
// 入力中は端末のカーソルを検索語の中に置く (caretCol)。確定した後はカーソルを出さないので、
// 打鍵を待っているのか確定済みなのかはカーソルの有無で分かる。
func (v *IssuesView) numberFilterLine(width int, colored bool) string {
	text, _ := promptWindow(NumberFilterPrompt, &v.NumFilter.line, width)
	line := NumberFilterPrompt + text +
		"  " + strconv.Itoa(len(v.Rows)) + " 件 (全カテゴリ・全状態)"
	return term.Paint(term.ClipToWidth(line, width), term.AnsiBold, colored)
}

// numberFilterPrompt は番号の絞り込みの行の頭。キャレットの桁はこの幅から数える。
const NumberFilterPrompt = "番号: "

// paste は入力欄に貼り付けを入れる。入力欄が無いときは何もしない (貼り付けをキー操作として解釈しない。
// docs/glogx-ui-guide.md §7)。
func (v *IssuesView) Paste(s string) {
	switch {
	case v.UrlPick.Active:
		v.UrlPick.paste(s)
	case v.NumFilter.Typing:
		if v.NumFilter.paste(s) {
			v.Cursor = 0 // 絞り込み直後は先頭を見せる (numberFilterKey と同じ)
			v.Refresh()
		}
	}
}

// typingInput は入力欄 (URL ピッカー・番号の入力中) にキーを打っているか。打っている間は印字キーがすべて
// 検索語なので、browseModel のキーリピートの抑止 (swallowKeyRepeat) を外す。
func (v *IssuesView) TypingInput() bool { return v.UrlPick.Active || v.NumFilter.Typing }

// caretCol は入力欄のキャレットを置く桁 (lines が返す窓の中の桁)。入力欄が無い・隠れているときは ok=false。
// 行は常に窓の 0 行目 (どちらの入力欄も先頭の行にある。下の並びの注記)。
//
// 🚨 端末のカーソルは IME が変換中の文字を出す位置 (docs/glogx-ui-guide.md §7・tuikit/caret)。lines の並び
// (URL ピッカーは 0 行目が検索行 / 一覧は listHeadLines の 0 行目が番号の行) に合わせて数える。lines の並びを
// 変えたらここも直す (input_caret_test.go が描画結果の上で固定している)。
// 演出中 (開く・閉じる) と確認の板が出ている間は置かない (行がずれる・板の下に隠れる)。
func (v *IssuesView) CaretCol(o IssuesRenderOpts) (x int, ok bool) {
	if v.MarkNext.Active || v.animProgress() < 1 {
		return 0, false
	}
	switch {
	case v.UrlPick.Active:
		return v.UrlPick.caretCol(o.Width), true
	case v.NumFilter.Typing && v.Open == nil:
		_, caret := promptWindow(NumberFilterPrompt, &v.NumFilter.line, o.Width)
		return caret, true
	}
	return 0, false
}

// tabLine はタブ行 (件数つき) と、右端に有効な状態フィルタを描く。
func (v *IssuesView) tabLine(o IssuesRenderOpts) string {
	// 件数は refresh が数えた値を読む (毎フレーム・毎打鍵の Filter は全件分の slice を捨てる。
	// visibleRows も行数を得るためにこの関数を通る)
	chips := make([]string, 0, len(v.tabs)+2)
	// [next] は All の左に固定 (ユーザー要望 2026-08-01)。カテゴリの 0 件寄せ (reorderTabsByCount)
	// の対象にしない — 目印を付ける場所として常に同じ位置に居てほしいため、0 件でも左端に出す。
	chips = append(chips, v.tabChip(tabNextName, v.nextCount, v.tabIdx == tabIdxNext, o.Colored))
	chips = append(chips, v.tabChip("All", v.allCount, v.tabIdx == 0, o.Colored))
	for i, t := range v.tabs {
		count := 0
		if i < len(v.tabCount) {
			count = v.tabCount[i]
		}
		chips = append(chips, v.tabChip(t.Name, count, v.tabIdx == i+1, o.Colored))
	}
	// 🚨 段階 (v.filter) ではなく「実際に見えている状態」を出す。epic group の子は状態フィルタの
	// 対象外、番号フィルタは状態を問わず拾うので、段階だけを出すとバッジが嘘になる (issue 296)
	filter := issues.VisibleBadges(v.Filter, v.Rows)
	avail := max(o.Width-term.DispWidth(filter)-1, 1)
	left := scrollTabs(chips, v.tabIdx+1, avail, o.Colored) // チップ配列は [next] が 0 番
	pad := max(o.Width-term.DispWidth(left)-term.DispWidth(filter), 0)
	// 🚨 組んだ後に必ず切る (scrollTabs 末尾と同じ規律): avail には下限 1 があるので、
	// バッジ + 印すら入らない極小幅 (o.width ≤ term.DispWidth(filter)) では合成が幅を超える
	// (issue 053: 幅 1 で「…○」= 2 セルが出ていた。収まる幅では clip は素通りで無 alloc)
	return term.ClipToWidth(left+term.PadSpaces(pad)+term.Paint(filter, term.AnsiDim, o.Colored), o.Width)
}

// tabScrollMark は「この向きにまだタブがある」ことを示す印 (幅 1 の bare 記号に限る。
// 絵文字は層ごとに幅解釈が割れる。internal/term/width.go の VS16 の議論と同じ理由)。
const (
	tabScrollLeft  = "‹"
	tabScrollRight = "›"
)

// scrollTabs はタブ行を「選択中のチップが必ず見える窓」へ切り出す (横スクロール)。
// 隠れている側には ‹ / › を出して、その先にタブがあることを示す。
//
// 🚨 窓を状態として持たない: (選択位置, チップ幅, 使える幅) からの導出値として毎回作り直す
// (一覧の windowOffset と同じ規律。理由もそちら)。フィルタ切替やタブの並べ替えで幅も選択位置も
// 変わるため、状態で持つと「選択中のタブが画面外なのに窓は動かない」ずれが残る。
//
// 窓の始点は「選択中のチップが収まる範囲でいちばん左」を選ぶ。こうすると選択が左寄りのうちは
// 先頭 (All) から見え、右へ進んだぶんだけ最小限スクロールする = 1 タブずつ動かしたときに
// 画面が飛ばない。
func scrollTabs(chips []string, sel, avail int, colored bool) string {
	if len(chips) == 0 || avail <= 0 {
		return ""
	}
	sel = app.ClampIdx(sel, len(chips))
	mark := func(s string) string { return term.Paint(s, term.AnsiDim, colored) }
	// 印のぶんを先に差し引いてから詰める (印を後付けすると幅を超える)
	reserve := func(start, end int) int {
		w := 0
		if start > 0 {
			w += term.DispWidth(tabScrollLeft) + 1 // "‹ "
		}
		if end < len(chips) {
			w += 1 + term.DispWidth(tabScrollRight) // " ›"
		}
		return w
	}
	// 始点: 選択中のチップが収まる最左。末尾まで出せるとは限らないので、右の印は
	// 「選択より後ろがある = 出うる」として保守的に見込む
	start := 0
	for ; start < sel; start++ {
		if joinedWidth(chips[start:sel+1])+reserve(start, sel+1) <= avail {
			break
		}
	}
	// 終点: 入るところまで右へ伸ばす
	end := sel + 1
	for end < len(chips) && joinedWidth(chips[start:end+1])+reserve(start, end+1) <= avail {
		end++
	}
	var b strings.Builder
	if start > 0 {
		b.WriteString(mark(tabScrollLeft))
		b.WriteString(" ")
	}
	b.WriteString(strings.Join(chips[start:end], " "))
	if end < len(chips) {
		b.WriteString(" ")
		b.WriteString(mark(tabScrollRight))
	}
	// 🚨 最後に必ず切る: 1 チップだけで avail を超える極端な狭さでは上のループが縮めきれない。
	return term.ClipToWidth(b.String(), avail)
}

// joinedWidth は空白 1 つで連結したときの表示幅。
func joinedWidth(chips []string) int {
	w := 0
	for i, c := range chips {
		if i > 0 {
			w++
		}
		w += term.DispWidth(c)
	}
	return w
}

// tabChip は 1 個のタブ表示。カテゴリ色を使い、選択中は太字・非選択は dim で落とす
// (一覧のカテゴリ列と同じ色にすることで「タブの色 = その行の色」が対応する)。
// All は特定のカテゴリではないのでシアン (本体の見出し色) を使う。
func (v *IssuesView) tabChip(name string, count int, active bool, colored bool) string {
	label, color := name, categoryColor(name)
	switch name {
	case "All":
		color = term.AnsiCyan // 特定のカテゴリではないので本体の見出し色
	case tabNextName:
		// 疑似カテゴリは識別子 (@next) でなく見出しの綴りで出す。色は状態バッジ ▶ と同じ意味の
		// 「これからやる」を示す固定色にする (語のハッシュだと repo ごとに色が変わる)
		label, color = "next", catYellow
	}
	text := "[" + label + " " + strconv.Itoa(count) + "]"
	if active {
		return term.Paint(text, term.AnsiBold+color, colored)
	}
	return term.Paint(text, term.AnsiDim+color, colored)
}

// issuesSelGutter は選択範囲の行に出す溝。🚨 幅は term.CursorGutterWidth と同じ 2 桁にすること
// (カーソル行の "→ " と混在するので、違う幅だと選択行だけ 1 桁ずれる)。
const IssuesSelGutter = "▌ "

// countDoneIssues は done の件数。epic の子は状態フィルタを通らない (issues.StatusFilter.showsIssue)
// ので、done も必ずここに数えられる。
func countDoneIssues(list []*issues.Issue) int {
	n := 0
	for _, iss := range list {
		if iss.Status == issues.StatusDone {
			n++
		}
	}
	return n
}

// groupProgress は group 親行の括弧の中身。子が 5 件でうち 2 件 done なら "5 ✓2"。
//
// 🚨 done を出す記号は一覧の done バッジ (issues.StatusDone.Badge) と同じ出典にする。
// 「5 件中 2 件」を `2/5` のような分数で書かない: 左の数が完了なのか残りなのかが読み手に
// 決められず、同じ画面の中で 2 通りに読める (2026-09-06 にユーザーが選択した書式)。
// done が 0 件なら `✓0` を出さずに従来の `(N)` のままにする (進捗の無い group で幅を食わない)。
func groupProgress(row displayRow) string {
	out := strconv.Itoa(row.childCount)
	if row.childDone > 0 {
		out += " " + issues.StatusDone.Badge() + strconv.Itoa(row.childDone)
	}
	return out
}

// rowLine は一覧の 1 行 (番号・状態バッジ・カテゴリ・タイトル)。width は行が使える
// 表示幅 (スクロールバー列を差し引いた後)。
// rowLine は 1 行を描く。cur は「カーソル記号を出す行」= 表示カーソル (滑走中は論理カーソルと
// 違いうる。dispCursor の doc)。
func (v *IssuesView) rowLine(i, cur int, o IssuesRenderOpts, width int) string {
	v.EnsureDisplayRows()
	row := v.displayRows[i]
	if row.kind == displayRowGroup {
		return v.groupLine(i, cur, row, o, width)
	}
	iss := row.issue
	indent := ""
	switch {
	case row.inGroup:
		indent = groupChildIndent
	case row.groupHead:
		arrow := "▸"
		if v.groupExpanded(row.groupKey) {
			arrow = "▾"
		}
		indent = arrow + " (" + groupProgress(row) + ") "
	}
	num := indent + term.FillRight(iss.Number, 3)
	badge := iss.Status.Badge()
	cat := term.FillRight(term.ClipToWidth(iss.Category, 9), 9)
	catPainted := term.Paint(cat, categoryColor(iss.Category), o.Colored)
	// 🚨 一覧に進捗 (チェックボックスの n/N) は出さない。数えるには本文を最後まで読む必要が
	// あり、一覧を出すたびに全 issue の全文を読んでいた (起動と外部編集後の再スキャンで毎回)。
	// 進捗は「あると便利」程度で、そのために全件の全文を読むのは釣り合わないと判断した。
	// 詳細を開いたときは Body が全文を持っているので、そこでは追加の I/O なしに出せる
	// (bodyHeadLines)。空いた幅はタイトルへ回る。
	// 一次情報: issue 050
	// 溝 + "NNN " + バッジ + " " + カテゴリ + " " + タイトル
	fixed := term.CursorGutterWidth + term.DispWidth(num) + 1 + term.DispWidth(badge) + 1 + term.DispWidth(cat) + 1
	titleW := max(width-fixed, 4)
	title := term.ClipToWidth(iss.Display(), titleW)
	text := num + " " + badge + " " + catPainted + " " + title
	// 🚨 どの経路も同じ幅に切る。titleW には下限 (4) があるので、極端に狭い幅では固定部分だけで
	// width を超える。カーソル行だけ切っていたため、そこ以外の行が枠を突き破っていた。
	if i != cur {
		// 選択範囲の行は溝で示す (カーソル行は → が優先。範囲は必ずカーソルを含むので競合しない)
		gutter := term.CursorGutterBlank
		if lo, hi, ok := v.Selection(); ok && i >= lo && i <= hi {
			gutter = term.Paint(IssuesSelGutter, term.AnsiCyan, o.Colored)
		}
		return term.ClipToWidth(gutter+text, width)
	}
	return o.paintCursorRow(text, width, term.AnsiBold)
}

// paintCursorRow はカーソル行を描く: 溝の矢印 (→) を付け、cursorPaint があればその強調、無ければ emph の
// 色で塗る (一覧の行 / group の親行 / URL ピッカーで共有する語彙)。
func (o IssuesRenderOpts) paintCursorRow(text string, width int, emph string) string {
	if o.CursorPaint != nil {
		return o.CursorPaint(term.ClipToWidth(term.CursorGutterMark+text, width))
	}
	return term.ClipToWidth(term.CursorGutterMark+term.Paint(text, emph, o.Colored), width)
}

func (v *IssuesView) groupLine(i, cur int, row displayRow, o IssuesRenderOpts, width int) string {
	arrow := "▸"
	if v.groupExpanded(row.groupKey) {
		arrow = "▾"
	}
	text := arrow + " " + term.SanitizePlainLine(row.groupName) + " (" + groupProgress(row) + ")"
	text = term.ClipToWidth(text, max(width-term.CursorGutterWidth, 0))
	if i != cur {
		return term.ClipToWidth(term.CursorGutterBlank+text, width)
	}
	return o.paintCursorRow(text, width, term.AnsiBold+term.AnsiCyan)
}

// bodyLines は本文 pager (ヘッダー + 本文) を描く。
func (v *IssuesView) bodyLines(o IssuesRenderOpts) []string {
	header := v.bodyHeadLines(o.Width, o.Colored)
	rows := max(o.Page-len(header), 1)
	// 左の行番号の溝ぶんも整形前に引く (溝を後付けすると幅を超える)。桁数はソース行数から
	// 決める: 整形しないと行番号が分からず、行番号が分からないと溝幅が決まらない循環を切る
	gutter := srcGutterWidth(v.Body.SrcLineCount())
	textW := v.bodyTextWidth(o.Width) // バー列と溝のぶんも引く
	lines := v.Body.Lines(textW, o.Colored)
	if v.LinkJump.Active {
		// 強調は整形し直して塗る (行数・桁は Lines と同じ。markdown.RenderLinks の doc)
		repos := v.jumpRepos()
		if cur := v.LinkJump.reanchor(v.Body.FileLinks(textW, repos)); cur >= 0 {
			lines = v.Body.JumpLines(textW, o.Colored, repos, cur)
		} else {
			v.LinkJump = linkJump{} // 読み直しで選択していたリンクが消えた
		}
	}
	// 行数は幅で変わる (Body は幅ごとに整形し直す)。描画で確定した行数で論理 offset を収束させる
	// (理由は listnav.Pager.Clamp の doc)
	if v.docLine > 0 {
		v.scrollToSrcLine(v.docLine, rows)
		v.docLine = 0
	}
	v.BodyPager.Clamp(len(lines), rows)
	offset := v.BodyPager.DrawOffset(len(lines), rows)
	end := min(offset+rows, len(lines))
	nums := v.Body.SrcLines()
	out := make([]string, 0, rows)
	for i := offset; i < end; i++ {
		src := 0
		if i < len(nums) {
			src = nums[i]
		}
		out = append(out, srcGutter(src, gutter, o.Colored)+lines[i])
	}
	out = layout.Scrollbar(out, o.Width, len(lines), offset, o.Colored)
	return append(header, out...)
}

// srcGutterWidth は行番号の溝の桁数 (番号 + 空白 1)。
func srcGutterWidth(srcLines int) int { return gutter.Width(srcLines) }

// srcGutter は 1 行ぶんの行番号の溝。src=0 (折り返しの続き行・畳まれた 2 行目以降) は空白 (理由は tuikit/gutter の doc)。
func srcGutter(src, width int, colored bool) string {
	if src <= 0 || width <= 0 {
		return gutter.Format(src, width)
	}
	return term.Paint(gutter.Number(src, width), term.AnsiDim, colored) + gutter.Separator // 番号だけを薄くする (区切りは塗らない)
}

// hint は viewer 表示中の操作案内 (最下行)。
//
// 🚨 モードの数は lines() の分岐と揃える (ピッカー / 本文 / 一覧の 3 つ)。揃っていないと、URL
// ピッカー表示中に本文 pager の案内 (j/k/g/G/p/u/e/h/q) が出る — それらは全部 urlPicker が検索語
// として飲むので、案内したキーが 1 つも案内どおりに動かない。
func (v *IssuesView) Hint(width int) string {
	// 🚨 hint は 1 行。全モードを term.FitHintItems で組み、幅に入らない項目は**末尾を切るのではなく
	// 優先度で落とす** (issue 264。それ以前は固定文字列を予算ぎりぎりに詰めていて、代表幅より
	// 狭い端末では並びの最後 = 抜ける手段が黙って消えた)。🚨 優先度 1 は「どう抜けるか」**だけ**に
	// する (同じ優先度は左から採るので、ラベルを 1 に置くと極端な幅で出口の方が落ちる)。
	// 落とした項目は --help と README が正本 (status viewer と同じ作法)。
	// nvim を開くキーは e と v の 2 本あるが、案内するのは e だけ (v は打ち慣れのための別名で、
	// 幅で絞ったのではなく意図的に出さない)。一覧モードはどちらも案内しない。
	if v.MarkNext.Active {
		// n の確認中は handleKey が全キーを先に飲む (Enter も実行になる)。一覧の案内 (Enter: 本文 /
		// q: 終了) を残すと、効かない・逆の意味のキーを案内することになる (issue 750。status viewer の
		// 確認中と同じ扱い)。語彙は板の中の confirm.HintYesOther と揃える
		return term.FitHintItems(width, []term.HintItem{
			{Text: "y/Enter: 実行", Prio: 2},
			{Text: "その他: キャンセル", Prio: 1},
		})
	}
	if v.UrlPick.Active {
		// 件数と 1 字消し (ctrl+h) はピッカー自身のヘッダーが出すので繰り返さない。ここは
		// 「打った文字がそのまま絞り込みになる」= 本文 pager のキーが効かないことだけを伝える。
		return term.FitHintItems(width, []term.HintItem{
			{Text: "文字入力で絞り込み", Prio: 2},
			{Text: "ctrl+n/p: 移動", Prio: 2},
			{Text: "Enter: 開く", Prio: 2},
			{Text: "Esc: 戻る", Prio: 1},
		})
	}
	if v.Open != nil && v.LinkJump.Active {
		// ジャンプ中は j/k がスクロールでなくリンクの移動になる (本文 pager の案内を残すと嘘になる)
		return term.FitHintItems(width, []term.HintItem{
			{Text: "j/k: 選ぶ", Prio: 2},
			{Text: "Enter: 開く (md は中で)", Prio: 2},
			{Text: "e: 編集", Prio: 4},
			{Text: "y: パス", Prio: 4},
			{Text: "Esc: 戻る", Prio: 1},
		})
	}
	if v.NumFilter.Typing {
		// 打鍵がすべて検索語になる = 一覧のキーが効かないことを伝える (urlPick と同じ理由)
		return term.FitHintItems(width, []term.HintItem{
			{Text: "数字で絞り込み", Prio: 2},
			{Text: "ctrl+n/p: 移動", Prio: 2},
			{Text: "Enter: 確定", Prio: 2},
			{Text: "Esc: 解除", Prio: 1},
		})
	}
	if v.Open != nil {
		// エディタ名を書かないのは editCmd が $VISUAL/$EDITOR を見るため ($EDITOR=code の人に
		// "nvim" と案内しない)。案内した全キーが効くことは TestIssuesViewBodyHintKeysAllRespond が
		// 予算幅で固定する。Enter/h/q は job パネルと同じ「戻る」。
		return term.FitHintItems(width, []term.HintItem{
			// 「スクロール」でなく「移動」: Tab を足して予算幅 (popup) に入らなくなったため縮めた
			// (diff の pager の hint_surfaces.go は据え置き。あちらには Tab が無い)
			{Text: "j/k/Space: 移動", Prio: 2},
			{Text: "J/K: 隣へ", Prio: 3},
			{Text: "g/G: 端", Prio: 5},
			{Text: "p: 番号", Prio: 4},
			{Text: "u: URL", Prio: 5},
			{Text: "Tab: パス", Prio: 5},
			{Text: "e: 編集", Prio: 4},
			{Text: "Enter/h/q: 戻る", Prio: 1},
		})
	}
	if lo, hi, ok := v.Selection(); ok {
		// 選択中は効くキーだけを出す (移動と Enter は選択を畳むので、並べると誤解を招く)
		return term.FitHintItems(width, []term.HintItem{
			{Text: strconv.Itoa(hi-lo+1) + " 件選択", Prio: 2},
			{Text: "J/K・shift+↑↓: 増減", Prio: 2},
			{Text: "y: パス", Prio: 3},
			{Text: "p: 番号", Prio: 3},
			{Text: "Y: 参照", Prio: 4},
			{Text: "Esc: 解除", Prio: 1},
		})
	}
	if v.NumFilter.Active {
		// Tab と a は絞り込み中の no-op なので案内しない (押しても何も起きないキーを出さない)
		return term.FitHintItems(width, []term.HintItem{
			{Text: "j/k: 移動", Prio: 2},
			{Text: "Enter: 本文", Prio: 2},
			{Text: "p: 番号", Prio: 3},
			{Text: "n: next", Prio: 3},
			{Text: "/: 絞り込み直す", Prio: 4},
			{Text: "Esc: 解除", Prio: 1},
		})
	}
	// a は 3 段の巡回なので「次に押すと何が増えるか」を出す (今どこまで見えているかはタブ行
	// 右端のバッジが示すので、ここで二重に説明しない)。語でなくバッジで書くのは
	// 幅のため ("a: pending も" は 14 桁)。
	//
	// 🚨 ここが出すのは**段階**の話で、epic group の子には効かない (子は状態フィルタの対象外。
	// issue 291)。バッジ側は迂回して見えているものを括弧で足して区別する (issues.VisibleBadges)
	// が、hint は 1 行しかないので段階だけを言う (issue 296)。
	// 増える状態は issues.StatusFilter.AddedBadges が段階の正本 (shows) から導出する。ここで
	// 段階ごとに書き分けると waiting を足したときに置き去りになる (issue 665)
	next := "a: +" + v.Filter.AddedBadges()
	if v.Filter.Next() == issues.FilterOpen {
		next = "a: " + issues.FilterOpen.Badges() + "のみ"
	}
	// 🚨 "q: 終了" であって "閉じる" ではない。q/esc は **glogx ごと終了**する
	// (ユーザー要望 2026-08-06)。一覧へ戻るのは i (toggle)。README も 2 語を使い分けており、
	// git log 一覧の hint も同じ動作を "q: 終了" と書いている。issue 121。
	// "i: 一覧へ" は出さない (戻り方は --help と README が正本。足すなら優先度を付けて並べる)。
	return term.FitHintItems(width, []term.HintItem{
		{Text: "j/k: 移動", Prio: 2},
		// 🚨 上方向を `b` で案内するのは、shift+space が端末を選ぶから (spec の
		// 「半ページ移動はカーソルが滑る」末尾)。Apple Terminal では shift が落ちて Space と
		// 同じ下方向になるので、案内を shift+space にすると「押しても下へ行く」案内になる。
		// 🚨 語が「半ページ」でなく「送り」なのは幅のため: 予算幅 89 に対し現行の hint が 71 桁で、
		// "Space/b: 半ページ" は区切り込み 19 桁と 1 桁だけ入らず、a の案内 (状態フィルタ) を
		// 押しのける (TestIssuesViewHintFitsPopupWidth が予算幅で a が残ることを要求している)。
		{Text: "Space/b: 送り", Prio: 5},
		{Text: "Tab: カテゴリ", Prio: 3},
		{Text: "/: 検索", Prio: 4},
		{Text: "Enter: 本文", Prio: 2},
		{Text: "n: next", Prio: 4},
		{Text: next, Prio: 5},
		{Text: "q: 終了", Prio: 1},
	})
}

// 「次にやる」の目印 (n)。選択中の issue を <issue ディレクトリ>/next/ へ移す。
//
// 🚨 viewer で唯一、実ファイルを動かす操作なので必ず確認を挟む (glogx の push/pull と同じ作法)。
// 移動そのものは issues.MoveToSubdir で、git index には触れない (理由はそちらの doc)。
//
// 一覧モードだけで受ける: 本文を開いたまま動かすと、開いている Body のパスが実体から外れて
// 「読んでいるファイルがもう無い」状態になる。目印は一覧を見ながら付けるものなので実害もない。
type IssuesMarkConfirm struct {
	Active  bool
	targets []*issues.Issue
	// unmark は「目印を外す」向きか (next/ から issue ディレクトリ直下へ戻す)。
	unmark bool
}

// askMarkNext は確認を開く (対象が無ければ何もしない)。n は目印の toggle で、既に next の
// issue に対しては「外す」向きになる (ユーザー要望 2026-08-01)。
//
// 🚨 向きはカーソル行で決めて選択範囲全体を揃える。1 件ずつ toggle にしない: 目印つきと無しが
// 混ざった選択で「何が起きるか」を確認ダイアログの 1 文で言えなくなる (「3 件のうち 2 件を付けて
// 1 件を外します」は読めない)。
func (v *IssuesView) askMarkNext() {
	if v.currentIsGroup() {
		v.setNotice("親行では issue を next へ移せません", app.NoticeRefused)
		return
	}
	rows := v.selectedRows()
	if len(rows) == 0 {
		return
	}
	unmark := false
	if cur := v.current(); cur != nil {
		unmark = cur.Status == issues.StatusNext
	}
	v.MarkNext = IssuesMarkConfirm{Active: true, targets: rows, unmark: unmark}
}

// markNextKey は確認中のキーを捌く。y/Enter で実行、それ以外は取り消し。
//
// 🚨 取り消しを n/Esc に限定しない: 実ファイルを動かす確認で「知らないキーを押したら実行された」
// が起きてはいけないので、明示的な y/Enter 以外はすべて取り消しに倒す。
//
// 🚨 ここだけ厳密 (大文字 `Y` も取り消し) なのは意図的で、discardKey / actionModal の
//
//	ToLower 判定へ揃えない (理由は status_view.go:discardKey の注記。issue 071 / 123)。
func (v *IssuesView) markNextKey(key string) tea.Cmd {
	targets, unmark := v.MarkNext.targets, v.MarkNext.unmark
	v.MarkNext = IssuesMarkConfirm{}
	if !confirm.IsYesStrict(key) {
		return nil
	}
	// 外すときは issue ディレクトリ直下へ戻す (= open)。🚨 元居た場所 (done/ 等) は覚えていない:
	// 目印は「次にやる」ものに付けるので戻り先は open が自然で、履歴を持つと「どこへ戻るか
	// 分からない」方が困る。
	dest, verb := issues.NextDirName, "に next の目印を付けました"
	if unmark {
		dest, verb = "", "の next を外しました"
	}
	cursorBefore, markBefore := issuePath(v.current()), v.markPath()
	movedPaths := make(map[string]string, len(targets))
	moved, skipped := 0, 0
	for _, iss := range targets {
		if (iss.Status == issues.StatusNext) != unmark {
			skipped++ // 既に目的の向き (同じ場所への移動を「変化」と数えない)
			continue
		}
		newPath, err := issues.MoveToSubdir(iss, dest)
		if err != nil {
			v.setPendingMoveAnchors(cursorBefore, markBefore, movedPaths)
			v.setNotice("移動できませんでした: "+app.FirstLine(err.Error()), app.NoticeError)
			return v.scanAfterChangeCmd() // 途中まで動いた分を一覧へ反映する
		}
		movedPaths[iss.Path] = newPath
		moved++
	}
	v.ClearMark()
	if moved == 0 {
		v.setNotice("対象がありません (すべて既にその状態)", app.NoticeOK)
		return nil
	}
	v.setPendingMoveAnchors(cursorBefore, markBefore, movedPaths)
	notice := strconv.Itoa(moved) + " 件" + verb
	if skipped > 0 {
		notice += " (" + strconv.Itoa(skipped) + " 件は対象外)"
	}
	v.setNotice(notice, app.NoticeOK)
	return v.scanAfterChangeCmd() // 置き場所が変わったので一覧を取り直す
}

func (v *IssuesView) setPendingMoveAnchors(cursorPath, markPath string, moved map[string]string) {
	v.clearPendingMoveAnchors()
	if next, ok := moved[cursorPath]; ok {
		v.pendingCursorPath = next
	}
	if next, ok := moved[markPath]; ok {
		v.pendingMarkPath = next
	}
	if v.pendingCursorPath != "" || v.pendingMarkPath != "" {
		v.pendingMoveStale = v.Scanning
	}
}

func (v *IssuesView) clearPendingMoveAnchors() {
	v.pendingCursorPath, v.pendingMarkPath = "", ""
	v.pendingMoveStale = false
}

// unmarkDestLabel は「next を外す」確認モーダルに出す戻り先の呼び名。
//
// 🚨 戻り先は issue の居場所で決まる。group issue は epic の外へ出さない (issues.MoveToSubdir)
// ので、文面を「issues 直下」に固定すると、ファイルを動かす前の唯一の確認画面が嘘をつく
// (2026-09-06 の敵対的レビュー 2 周目。文面自体は 2026-08-01 から嘘だった)。
//
// 🚨 先頭 1 件で決めない。選択には global と group が混在しうる (epic 名が数字の group は
// 親 issue が issue 行として並ぶので、group 行を挟まずに範囲選択できる)。先頭だけを見ると
// 「global が先頭・後続が group」の並びで嘘に戻る (同 3 周目で実測)。
func unmarkDestLabel(targets []*issues.Issue) string {
	hasGroup, hasGlobal := false, false
	for _, iss := range targets {
		if iss.ContainerDir() != iss.Dir {
			hasGroup = true
		} else {
			hasGlobal = true
		}
	}
	// 🚨 skip される対象 (既にその向きに居るもの。markNextKey の (Status==Next) != unmark) も
	// ここでは数える。「group へ戻る」と言いながら実際に動くのが 0 件になりうる = 誤警告どまりで、
	// 逆向き (issues 直下と言って group へ入る) は作れない (MoveToSubdir と同じ 1 本の条件で
	// 決めているため。2026-09-06 の敵対的レビュー 4 周目 P3-5 で確認済み)。件数表示 (`N 件`) も
	// 同じ集合を数えているので、ここだけ絞ると画面の中で数と宛先の母集合が食い違う
	switch {
	case hasGroup && hasGlobal:
		return "元の group / issues 直下"
	case hasGroup:
		return "group 直下"
	}
	return "issues 直下"
}

// markNextBox は確認モーダルの箱 (glogx の push/pull 確認と同じ見た目)。
func (v *IssuesView) markNextBox(width int, colored bool) []string {
	if !v.MarkNext.Active {
		return nil
	}
	what := strconv.Itoa(len(v.MarkNext.targets)) + " 件"
	if len(v.MarkNext.targets) == 1 {
		// Rel は同一性のため無害化しない実物 (issues/parse.go newIssue の doc)。画面へ出す
		// ここで sanitize する (制御文字入りのファイル名で確認モーダルを細工させない)
		what = term.SanitizePlainLine(filepath.Base(v.MarkNext.targets[0].Rel))
	}
	title, lines := " next の目印 ", []string{what + " に next の目印を付けます"}
	if v.MarkNext.unmark {
		// 🚨 宛先は独立した行にする。箱は 44 桁で頭打ち (confirm.MaxWidth) なので、1 行に繋ぐと
		// 実在する長さのファイル名 (`292-feat-audit-forge-should-hand-each-agent-its-own-worktree.md`)
		// で宛先が丸ごと切り落とされ、「ファイルを動かす前の唯一の確認画面」に何も出ない
		// (2026-09-06 の敵対的レビュー 4 周目で実測。3 周目までのテストは短い名前の
		// fixture を使っていたので、この truncation を検出できていなかった)
		title = " next を外す "
		lines = []string{
			what + " を next/ から",
			"→ " + unmarkDestLabel(v.MarkNext.targets) + " へ戻します",
		}
	}
	return confirm.Dialog(title, append(lines,
		term.Paint("(次にやる目印。ファイルを移動します。commit はしません)", term.AnsiDim, colored),
	), confirm.HintYesOther, width, colored)
}
