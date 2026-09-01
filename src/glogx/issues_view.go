package main

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"glogx/issues"
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
type issuesScanMsg struct {
	root     string // repo root (貼り付け用の repo 相対パスに使う)
	dirs     []string
	issues   []*issues.Issue
	warnings []string
	// fp は読んだ時点のファイル群の指紋 (issues_watch.go)。見張りの基準に使う。
	fp string
}

// issuesView は一覧 (タブ + リスト) と本文 pager の 2 モードを持つ全画面ビュー。
type issuesView struct {
	shown    bool
	loaded   bool // 一度スキャンを完了したか (取り直し中に前回の結果を出してよいかの判定)
	scanning bool // スキャン中 (スピナーを回す。二重発行の防止も兼ねる)
	// rescanPending は「飛行中のスキャンが終わったら、もう 1 回取り直す」予約。
	// ⚠️ single-flight (scanCmd) は連打でゴルーチンを積まないための仕組みだが、自分がファイルを
	// 動かした後 (n の next/ 移動) の取り直しまで落とすと、実ファイルは動いたのに一覧が旧位置を
	// 出し続ける。しかも飛行中のスキャンは移動より前に始まっているので、その結果が届いても
	// 移動は映らない。要求を 1 つに畳んで receive の出口で張り直すことで、貫通の穴を増やさずに
	// 「最後の要求は必ず反映される」を満たす。
	rescanPending bool

	cwd      string // スキャンの起点 (再読込で使い回す)
	root     string // repo root
	dirs     []string
	all      []*issues.Issue
	warnings []string
	// notice は直近の操作結果 (コピー成功・読み込み失敗など)。noticeOK は成功か。
	// browseModel はこれを取り出してトーストにする (takeNotice)。⚠️ viewer 単体でも駆動できる
	// 契約 (このファイル冒頭) を保つため、ここではトーストを知らず「結果」だけを置く。
	notice   string
	noticeOK bool

	// tabsCanon は issues.Tabs が返す正規順序 (done 込みの件数降順 → 名前昇順、other は末尾)。
	// tabs は「そこから 0 件を右へ寄せた表示・巡回順」。⚠️ 並べ替えを tabs へ破壊的に適用すると
	// 順序が履歴依存になる (前回の並びを基準に再度寄せるため、全件表示に戻しても正規順序へ戻らず
	// 「12, 6, 2, 6, 4」のような不規則な並びが残る。実測 2026-07-31)。正規順序を別に保ち、
	// 表示順は (正規順序, 現在の件数) の純粋な写像として毎回作り直す。
	tabsCanon []issues.Tab
	tabs      []issues.Tab // All は含まない (表示時に先頭へ足す)
	tabIdx    int          // 0 = All、1.. = tabs[tabIdx-1]
	// filter は「どの状態まで見せるか」(a キーで巡回)。zero value = open のみが既定
	// (issues.FilterOpen)。pending / done を既定で伏せる理由は issues.StatusFilter の doc。
	filter issues.StatusFilter
	// numFilter は番号のインクリメンタルフィルタ (/)。zero value = 絞り込みなし。効いている
	// あいだはタブと filter の両方を無視した全 issue が対象になる (issuesNumberFilter の doc)。
	numFilter issuesNumberFilter
	// チップに出す件数。issues.Tab.Count は done を含む全件なので表示には使わない (理由は
	// refresh)。tabCount は tabs と同じ並び、allCount は All チップ用。
	tabCount  []int
	allCount  int
	nextCount int // 疑似カテゴリ [next] のチップに出す件数

	rows   []*issues.Issue // 現在のタブ・フィルタの表示対象
	cursor int
	// 複数選択 (shift+↑/↓)。範囲は錨 (markAt) と cursor から毎回導出する。⚠️ 選択集合を別に
	// 持たない: 行集合はフィルタ・タブ・再スキャンで入れ替わるので、集合で持つと実体を失った
	// 選択が残り「見えていない行がコピーされる」。錨だけなら行集合を作り直す refresh で畳める。
	marked bool
	markAt int
	// offset は窓の先頭行。⚠️ 一覧に glide (スクロールアニメ) は載せない: 半ページ移動は
	// cursor と窓を同時に動かし、windowOffset が「カーソルを含む最小の窓」を導出するので
	// カーソルは必ず窓の端に来る = 窓を 1 行でも遅らせるとカーソルが画面から出る。遅らせる余地が
	// 幾何的にゼロで、載せても瞬時に着地点へ張り付くだけだった (実測 issue 031)。窓が動かない
	// ケース (カーソルが窓の途中) では glide 自体が始まらないので、条件が排他になっている。
	offset int

	// 本文 pager (open != nil のとき本文モード)
	open *issues.Issue
	body *issues.Body
	// urlPick は本文中 URL のピッカー (u)。閉じているときは zero value。
	urlPick urlPicker
	// markNext は「次にやる」の目印を付ける確認 (n)。⚠️ 実ファイルを動かす唯一の操作なので、
	// 他のキーと違って必ず確認を挟む (glogx の push/pull と同じ作法)。zero value = 確認なし。
	markNext issuesMarkConfirm
	// drawer は本文を左から開く引き出しの演出状態 (issues_drawer.go)。閉じる演出のあいだは
	// open/body を生かしたまま逆再生するため、破棄は settleDrawer が担う。
	drawer    issuesDrawer
	bodyOff   int // 論理 = 着地点
	bodyGlide scrollGlide

	// 開くときのスライドイン演出の開始時刻 (ゼロ値 = 演出なし)。フレーム数ではなく壁時計で
	// 進めるのは、tick 周期が変わっても所要時間が変わらないようにするため (push 演出の
	// pushSlides と同じ方式)。演出中の tick 周期は tickInterval が上げる (slideAnimating)。
	// 閉じる演出 (closing) でも同じ時計を使う — 開く演出の逆再生なので所要も同じ。
	animStart time.Time
	// wantStatus は「s で status viewer へ切り替えたい」の一度きりの信号 (browseModel が
	// takeWantStatus で取り出す。閉じ→開きの連携は viewer 単体では完結しないため)。
	wantStatus bool
	// wantRatelimit は「R で ratelimit ダッシュボードへ切り替えたい」の一度きりの信号 (同上)。
	wantRatelimit bool
	// wantQuit は「q/esc で glogx ごと終了したい」の一度きりの信号 (同上。quit は browseModel の
	// 仕事で、viewer は tea.Quit を出さない)。
	wantQuit bool
	// closing は閉じる演出 (開く演出の逆再生) の途中か (ユーザー要望 2026-08-01)。
	//
	// ⚠️ 演出のあいだ shown を false にしない: 逆再生で画面が見えている必要がある。実際の
	// 片付け (本文の破棄・watcher の停止) は finishClose に一本化する。片付けを二箇所に書くと、
	// 演出の着地とキーによる即着地のどちらかが stopWatch を通らず watcher が二重に走る。
	closing bool
	// closeAnimOff は閉じる演出を出さない (テスト用)。⚠️ zero value = 演出あり: 本番の既定を
	// 「演出する」に置く。テストは「close したら即座に畳まれている」前提で書かれており、演出を
	// 挟むと全てが 1 拍待ちになって読めなくなる (アプリ開閉演出の appZoom.off と同じ作法)。
	// 演出そのものは issues_close_anim_test.go が明示的に on にして検査する。
	closeAnimOff bool

	// watch は開いている間だけ回す「別プロセスの編集」の見張り (issues_watch.go)。
	watch issuesWatch

	// pending は前回終了時の画面の復元予約 (issues_state.go)。スキャン結果が要るので、
	// 適用は最初の receive まで待つ (applyScreen が 1 度だけ消費する)。
	pending *issuesScreen
}

const (
	// issuesAnimDuration は開く演出の所要時間 (最後の行が着地するまで)。当初 700ms、
	// 2 倍速の 350ms へ短縮 (ユーザー要望 2026-08-13。閉じ・引き出し・status viewer も
	// 速度関係を保ったまま一律 1/2)。
	issuesAnimDuration = 350 * time.Millisecond
	// issuesCloseDuration は閉じる演出の所要時間 (板が画面外へ抜け切るまで)。開くより速いのは、
	// 開くときは中身を読み始められる一方、閉じるときは「もう用が済んだ画面」を見せ続けるため
	// (引き出しの issuesDrawerDuration と同じ値・同じ理由)。⚠️ 畳む時刻でもある: この時間が
	// 板の実際の滞在時間より長いと、抜けた後の空舞台を見せてから git log へ戻ることになる。
	issuesCloseDuration = 225 * time.Millisecond
	// issuesAnimStagger は開く演出で行ごとに開始をずらす割合。0 なら全行同時に動いて「板が 1 枚
	// 滑り込む」見え方、大きいほど「上から順に流れ込む」見え方になる。閉じる演出では使わない
	// (rowOffsetRatio の doc)。
	issuesAnimStagger = 0.35
)

func newIssuesView() issuesView { return issuesView{} }

// visible は viewer を表示中か。
func (v *issuesView) visible() bool { return v.shown }

// loading はスキャン中か (スピナー tick を回し続ける判定用)。
func (v *issuesView) loading() bool { return v.scanning }

// toggle は viewer の開閉。開くたびにスキャンし直す tea.Cmd を返す。
//
// 「初回だけ」にしていた頃は N (次に採番すべき番号) が初回スキャン時点の最大番号 + 1 を返し
// 続けていた。番号は viewer の外でも増える (別セッション・エディタ・他ブランチからの merge) ので、
// 番号の鮮度がユーザーの「r を押したかどうか」の記憶に依存し、番号の再利用を招く (他 repo で
// 実際に 37 件発生している事故)。スキャンは非同期かつ実測 2.5〜13.6ms なので、開くたびに
// 取り直しても体感は変わらない: 取得中も前回の結果を出したまま差し替わる (emptyMessage)。
func (v *issuesView) toggle(cwd string) tea.Cmd {
	if v.closing {
		v.finishClose() // 閉じる演出の途中の i は「閉じ切ってから開き直す」(watcher を二重に張らない)
	} else if v.shown {
		v.close()
		return nil
	}
	v.shown = true
	// 右から左へ流し込む演出を開始 (lines が窓を変形する)。⚠️ 時計は timeNow で取る:
	// 引き出し (drawer) と同じ差し替え点に揃えないと、この 1 つのビューの中で
	// 「止められる時計」と「止められない時計」が混在する。
	v.animStart = timeNow()
	return tea.Batch(v.scanCmd(cwd), v.watchCmd())
}

// restore は前回終了時の画面を復元しながら viewer を開く (起動時。issues_state.go)。
// 適用はスキャン結果が届く receive まで待つので、ここでは予約を置いてスキャンを始めるだけ。
//
// ⚠️ 開く演出は出さない: 復元は「閉じたところから再開」なので、起動のたびに issuesAnimDuration 待たされる
// のは筋が違う (ユーザー選定 2026-07-31)。
// ⚠️ 既に開いているなら何もしない: 復元の git fork とスキャンの間に i が押された場合で、
// 上書きするとユーザーの操作を奪う。
func (v *issuesView) restore(cwd string, s issuesScreen) tea.Cmd {
	if v.shown {
		return nil
	}
	v.shown = true
	v.animStart = time.Time{}
	v.pending = &s
	return tea.Batch(v.scanCmd(cwd), v.watchCmd())
}

// screen は今の画面を保存用に書き出す (ok=false = 覚えるものが無い)。
//
// viewer を出していないときに false を返すのが「git log 一覧から終了したら記憶しない」の実体
// (呼び出し側はそのとき記憶を消す)。スキャン前 (root 未確定) も覚えない: 照合キーが無い記憶は
// 復元時に別 repo で当たってしまう。
func (v *issuesView) screen(now time.Time) (issuesScreen, bool) {
	// ⚠️ 閉じる演出の途中 = ユーザーは既に閉じている。shown はまだ true だが覚えない
	// (覚えると「q で閉じてすぐ終了した」次の起動で、閉じたはずの viewer が蘇る)。
	if !v.shown || v.closing || v.root == "" {
		return issuesScreen{}, false
	}
	open := issuePath(v.open)
	if v.drawer.phase == drawerClosing {
		open = "" // 閉じる演出の途中 = ユーザーは既に閉じている。開いた状態で復元しない
	}
	return issuesScreen{
		Root:    v.root,
		SavedAt: now,
		Tab:     v.currentTab(),
		Filter:  v.filter.String(),
		Cursor:  issuePath(v.current()),
		Open:    open,
		BodyOff: v.bodyOff,
	}, true
}

// applyScreen は復元予約を今のスキャン結果へ当てる。
//
// 消えた issue (rename / 状態ディレクトリへ移動) には黙って別物を当てない: カーソルは
// anchorCursor が当たらなければ先頭のまま、本文はパスが見つからなければ一覧のままにする。
func (v *issuesView) applyScreen(s issuesScreen) {
	v.filter, _ = issues.ParseStatusFilter(s.Filter) // 未知の名前は既定へ (loadIssuesScreen で正規化済み)
	v.refresh()                                      // フィルタを反映してタブの並びと件数を作る (tabIdx を引くのに要る)
	v.tabIdx = tabIndexOf(v.tabs, s.Tab)
	v.refresh() // 選んだタブで行集合を作り直す
	// 窓 (offset) はここで動かさない: 描画が windowOffset でカーソルを含む位置へ収束させる
	// (offset を状態でなく導出値として扱う規律。windowOffset の doc)
	v.anchorCursor(s.Cursor)
	if s.Open == "" {
		return
	}
	for _, iss := range v.all {
		if iss.Path != s.Open {
			continue
		}
		if v.openIssue(iss) {
			v.drawer.finish()     // 演出は出さず開き切った状態から始める
			v.bodyOff = s.BodyOff // 行数を超えていれば bodyLines が収束させる
		}
		return
	}
}

// close は viewer を閉じる (スキャン結果は保持する。再表示は前回の結果を出しながら取り直す)。
//
// 閉じる演出 (開く演出の逆再生) を挟んでから片付ける (ユーザー要望 2026-08-01)。ここで画面を
// 落とさないのは、逆再生のあいだ一覧が見えている必要があるため (引き出しの startClose と同じ)。
func (v *issuesView) close() {
	if !v.shown || v.closing {
		return
	}
	v.closing = true
	v.animStart = timeNow()
	if v.closeAnimOff {
		v.finishClose() // 演出なしの設定では同じ出口を即座に通す (片付けの経路を分けない)
	}
}

// settleClose は閉じる演出が着地していれば片付ける (browseModel の tick から毎拍呼ばれる)。
func (v *issuesView) settleClose() {
	if !v.closing || timeNow().Sub(v.animStart) < issuesCloseDuration {
		return
	}
	v.finishClose()
}

// finishClose は閉じる演出を即座に着地させて片付ける。閉じていなければ何もしない。
//
// ⚠️ viewer を畳む唯一の出口にする。演出の着地 (settleClose) とキーによる即着地の両方が
// ここを通ることで、片方が stopWatch を通らずに次の watchCmd が走る = watcher が二重に居座る、
// という取りこぼしが構造的に起きない。
// 戻り値は「この呼び出しで実際に畳んだか」。⚠️ tui.go が演出中のキーを 1 つだけ捨てる判定に使う
// (素通しで別ターゲットを開いてしまう e。理由はそちらのコメント)。
func (v *issuesView) finishClose() bool {
	if !v.closing {
		return false
	}
	v.closing = false
	v.shown = false
	v.animStart = time.Time{}
	v.discardBody() // viewer ごと閉じるので引き出しの演出は持ち越さない
	v.stopWatch()   // 見張りの watcher を閉じる (fd を残さない。issues_watch.go)
	// ⚠️ 取り直しの予約も捨てる。残すと閉じた後に「非表示の viewer のための」スキャンが 1 回走り、
	// その間 loading() が true になって tick が昂進する (自己終息はするが無駄な仕事)。
	v.rescanPending = false
	// 番号の絞り込みは持ち越さない。⚠️ q / Esc は絞り込みを解くだけで閉じない (1 段戻る) が、
	// i は 1 段戻さず閉じるので、ここで捨てないと次に開いた viewer が「なぜか件数が少ない一覧」
	// から始まる。行集合も作り直す — rows は常に visibleIssues() と一致させる (残すと、開いた
	// 直後の 1 フレームだけタブ行の下に絞り込まれた行が並ぶ)。
	if v.numFilter.active {
		v.numFilter.clear()
		v.refresh()
	}
	return true
}

// finishAnim は演出を即座に着地させる。閉じる演出のときは片付けまで進める
// (ここで時計だけ止めると、閉じかけの姿のまま二度と畳まれない状態で固まる)。
func (v *issuesView) finishAnim() {
	if v.closing {
		v.finishClose()
		return
	}
	v.animStart = time.Time{}
}

// slideAnimating は viewer 全体の開閉スライド中か。tickInterval が 60fps へ上げる判定に使う
// (引き出しと pager glide は含めない: あちらは他の glide と同じ 30fps で足りる)。
func (v *issuesView) slideAnimating() bool {
	// ⚠️ 閉じる演出は「時間が過ぎたら false」にしない: tick は animating が false になった拍で
	// 止まるので、時間で降ろすと片付けの settleClose が呼ばれる前にチェーンが切れ、閉じかけの姿で
	// 固まる。settleClose が closing を下ろして初めて false になる (自分で終われる形にする)。
	if v.closing {
		return true
	}
	return v.shown && !v.animStart.IsZero() && timeNow().Sub(v.animStart) < issuesAnimDuration
}

// animating は演出の途中か (tick チェーンを回し続ける spinnerActive の判定に使う)。
func (v *issuesView) animating() bool {
	if v.bodyGlide.active {
		return true // 本文 pager の glide は tick で進むので「アニメ中」に含める
	}
	if v.drawer.animating(timeNow()) {
		return true // 本文の引き出しの開閉も tick で進む
	}
	return v.slideAnimating()
}

// takeWantStatus は「s で status viewer へ切り替えたい」を一度だけ取り出す (takeNotice と同じ語彙)。
func (v *issuesView) takeWantStatus() bool {
	want := v.wantStatus
	v.wantStatus = false
	return want
}

// takeWantRatelimit は「R で ratelimit ダッシュボードへ切り替えたい」を一度だけ取り出す。
func (v *issuesView) takeWantRatelimit() bool {
	want := v.wantRatelimit
	v.wantRatelimit = false
	return want
}

// takeWantQuit は「q/esc で glogx ごと終了したい」を一度だけ取り出す (takeNotice と同じ語彙)。
func (v *issuesView) takeWantQuit() bool {
	want := v.wantQuit
	v.wantQuit = false
	return want
}

// advanceGlide はスクロール glide を 1 フレーム進める (browseModel の tick から呼ばれる)。
func (v *issuesView) advanceGlide() {
	if v.bodyGlide.active {
		v.bodyGlide.advance(v.bodyOff)
	}
}

// scanCmd は探索・メタデータ読み込みを 1 つのゴルーチンでまとめて行う。
//
// なぜ 1 発でメタデータまで読むか: Issue はポインタで保持するので、後追いのゴルーチンから
// 埋めると View 側の読み取りと競合する。探索 (readdir) と本文読み (H1 / front matter /
// チェックボックス) を同じゴルーチンで終わらせて完成品を渡せば競合が構造的に起きない。
// コストは実測 2.5〜13.6ms (229 ファイルの repo) で、glogx の起動パスではなく i を
// 押したときだけ通る (仕様書「本文を起動時に全件読まない」の担保)。
func (v *issuesView) scanCmd(cwd string) tea.Cmd {
	if v.scanning {
		return nil // single-flight: i / r の連打で同じ探索のゴルーチンを積まない
	}
	v.cwd = cwd
	v.scanning = true
	return func() tea.Msg { return scanIssues(cwd) }
}

// scanAfterChangeCmd は「自分が (または開いていたエディタが) ファイルを変えた後」の取り直し。
// 飛行中なら予約して receive の出口で張り直すので、single-flight に落とされない。
//
// ⚠️ これが必要なのは、飛行中のスキャンが**変更より前に始まっている**ため。その結果が届いても
// 変更は映らないので、単に落とすと「実ファイルは動いたのに一覧は旧位置を出し続ける」状態が
// fsnotify の次周期まで残る (fsnotify が無音な FS では保険のポーリングまで)。
//
// ⚠️ 読み直しだけの経路 (toggle / restore / r) には使わない。あちらは連打を畳むのが正しく、
// 予約すると 1 打ごとに追加のスキャンが後から積まれる (single-flight の目的そのものを損なう)。
func (v *issuesView) scanAfterChangeCmd() tea.Cmd {
	if v.scanning {
		v.rescanPending = true
		return nil
	}
	return v.scanCmd(v.cwd)
}

// scanIssues は探索・メタデータ読み込み・見張りの基準づくりを 1 回で行う (scanCmd の本体)。
//
// 指紋をここで取るのが要点: 「読んだ内容」と「基準」を同じ時点に揃えないと、その差の間に入った
// 外部編集が基準に焼き込まれ、次の編集が来るまで永久に取りこぼす (issues_watch.go)。
func scanIssues(cwd string) issuesScanMsg {
	root := issues.RepoRoot(cwd)
	dirs := issues.FindDirs(root)
	found, warnings := issues.Scan(dirs)
	for _, iss := range found {
		// メタデータの読み取り失敗は無視する (タイトルがスラッグ表示に落ちるだけ)
		_ = iss.LoadMeta()
	}
	return issuesScanMsg{
		root: root, dirs: dirs, issues: found, warnings: warnings,
		fp: issuesFingerprint(dirs, issuesWatchPaths(found)),
	}
}

// receive はスキャン結果を反映する。
//
// Scan は毎回新しい *Issue を作るので、見ている場所は安定キーで引き直す (仕様が定める同一性キーは
// パス。番号も basename も一意でない)。引き直さないと、再スキャンのたびに (a) カーソルが別の
// issue へ滑り、(b) タブが別カテゴリを指し (tabs は件数降順なので件数が変わると並びが変わる)、
// (c) 本文モードが v.all から外れた古いポインタを掴んで状態・進捗が編集前のまま固まる。
// 戻り値は「この結果を受けて追加で走らせる Cmd」(予約されていた取り直し。無ければ nil)。
// restore / reloadAfterEdit と同じく Cmd を返す作法に揃えている。
func (v *issuesView) receive(msg issuesScanMsg) tea.Cmd {
	v.scanning, v.loaded = false, true
	// 見張りの基準は「このスキャンが読んだ時点の指紋」に揃える (issues_watch.go)。最初の観測で
	// 取ると、読んだ時刻と基準を取る時刻が最大 1 周期ずれ、その間の外部編集が基準へ焼き込まれて
	// 次の編集が来るまで永久に取りこぼす。自分の取り直しを「外部の変化」と誤検出しないのも同じ式で
	// 満たせる (スキャンは内容を変えないので指紋は動かない)。
	v.watch.seen, v.watch.pending = msg.fp, ""
	tab, cursorPath, openPath, markPath := v.currentTab(), issuePath(v.current()), issuePath(v.open), v.markPath()
	v.root, v.dirs, v.all, v.warnings = msg.root, msg.dirs, msg.issues, msg.warnings
	v.tabsCanon = issues.Tabs(v.all, issues.TabMinCount)
	v.tabs = v.tabsCanon // refresh が件数を数えて表示順 (0 件を右) へ並べ替える
	v.tabIdx = tabIndexOf(v.tabs, tab)
	v.refresh()
	v.anchorCursor(cursorPath)
	v.anchorMark(markPath)
	v.rebindOpen(openPath)
	v.startWatch() // 監視対象のディレクトリはスキャン結果で決まる (issues_watch.go)
	if v.pending != nil {
		// 起動時の復元予約は最初のスキャン結果へ 1 度だけ当てる (以降の r / 編集後の取り直しは
		// 通常どおり「今見ている場所」を引き継ぐ)
		s := *v.pending
		v.pending = nil
		v.applyScreen(s)
	}
	if v.rescanPending {
		v.rescanPending = false
		return v.scanCmd(v.cwd) // 畳んでおいた要求を 1 回だけ張り直す (ここでは scanning=false)
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

// anchorCursor は再スキャン前と同じ issue にカーソルを戻す (消えていれば位置を維持)。
func (v *issuesView) anchorCursor(path string) {
	if path == "" {
		return
	}
	for i, iss := range v.rows {
		if iss.Path == path {
			v.cursor = i
			return
		}
	}
}

// markPath は選択の錨が指す issue のパス ("" = 選択していない / 錨が範囲外)。
func (v *issuesView) markPath() string {
	if !v.marked || v.markAt < 0 || v.markAt >= len(v.rows) {
		return ""
	}
	return v.rows[v.markAt].Path
}

// anchorMark は再スキャン前と同じ issue へ選択の錨を張り替える (消えていれば選択したままにしない)。
//
// このビューは「位置で持つものは安定キーで張り替える」で揃えている (カーソル=パス・タブ=名前・
// 本文=パス)。選択の錨だけがその規律から外れており、行集合を作り直す refresh が畳んでいた。
// 外部編集の即時反映 (issues_watch.go) が入ってからは、選択している最中に取り直しが走るのが
// 普通になった (Claude Code が issue を書くたび) ため、畳むと選択が実用にならない。
//
// ⚠️ 張り替えるのは再スキャン (同じ集合の読み直し) だけ。タブ・フィルタの切り替えでは refresh が
// 畳んだままにする — あちらは行集合の意味そのものが変わるので、範囲を持ち越すと別の対象を指す。
func (v *issuesView) anchorMark(path string) {
	if path == "" {
		return
	}
	for i, iss := range v.rows {
		if iss.Path == path {
			v.marked, v.markAt = true, i
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
//  3. どこにも無ければ本文モードを畳んで理由を通知する。⚠️ 消えた本文を出し続けると、viewer が
//     「もう無いファイルの内容」を最新として見せ続ける (このモードでは実体が無いので編集も
//     取り直しもできず、読み続ける対象がそもそも無い)
//
// ⚠️ 畳むのは 3 の「どこにも無い」ときだけ。移動を 2 で吸収してから判定するので、done/ へ移された
// だけで一覧へ引き戻すことはない (カーソル・選択の錨が「消えていれば現状維持」なのと同じ精神で、
// ユーザーの居場所を理由なく奪わない)。
func (v *issuesView) rebindOpen(path string) {
	if path == "" {
		return
	}
	for _, iss := range v.all {
		if iss.Path == path {
			v.open = iss
			return
		}
	}
	base := filepath.Base(path)
	moved, ambiguous := v.matchByBase(base)
	switch {
	case moved != nil:
		v.open = moved
	case ambiguous:
		// 実体はあるが本人を決められない。畳むのは「どこにも無い」ときだけなので現状維持
	default:
		v.discardBody() // 演出は挟まない (抜けていく板に映す中身がもう無い)
		v.setNotice("開いていた issue が見つかりません (一覧へ戻ります): "+base, false)
	}
}

// matchByBase は同じファイル名の issue を探す。ちょうど 1 件なら (それ, false)、複数なら
// (nil, true)、無ければ (nil, false)。⚠️ 「複数」と「無い」を呼び出し側で分ける必要がある
// (複数は実体があるので畳んではいけない)。
func (v *issuesView) matchByBase(base string) (found *issues.Issue, ambiguous bool) {
	for _, iss := range v.all {
		if filepath.Base(iss.Path) != base {
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
func (v *issuesView) refresh() {
	// ⚠️ 行集合が変わるので選択は畳む。錨は位置で持つため、残すと別の issue を指す
	v.clearMark()
	v.rows = v.visibleIssues()
	v.cursor = clampIdx(v.cursor, len(v.rows))
	// チップの件数は「そのタブを選んだときに実際に並ぶ行数」と同じ Filter から出す。
	// issues.Tab.Count は done を含む全件なので、そのまま出すと done を伏せた既定表示で
	// 「カテゴリの合計 ≠ All ≠ 一覧の行数」になる。
	//
	// ⚠️ タブ集合そのものは v.all から作る (receive)。Filter 後の集合から作り直すと done だけの
	// カテゴリが消え、位置で持つ tabIdx が別カテゴリを指す。ここで数えるのは件数だけ。
	v.allCount = len(issues.Filter(v.all, "", v.filter))
	v.nextCount = len(v.rowsForTab(tabNextName))
	sel := v.currentTab() // 並べ替えを跨いで選択を保つため名前で覚える (tabIdx は位置で持つ)
	counts := make([]int, len(v.tabsCanon))
	for i, t := range v.tabsCanon {
		counts[i] = len(issues.Filter(v.all, t.Name, v.filter))
	}
	// 0 件のカテゴリは右へ寄せる (ユーザー要望 2026-07-31)。タブ集合の順序は done 込みの全件数で
	// 決まる (issues.Tabs) ため、状態を伏せた表示では「0 件なのに左端」が構造的に起きていた。
	v.tabs, v.tabCount = reorderTabsByCount(v.tabsCanon, counts)
	v.tabIdx = tabIndexOf(v.tabs, sel)
}

// visibleIssues は今の条件で一覧に並べる行集合。
//
// ⚠️ 行集合を作るのはここ 1 箇所にする。番号フィルタは再スキャン (r / 見張り) や a を跨いで
// 残るので、/ の処理側で v.rows を差し替える形にすると、refresh を通る経路 (receive /
// applyScreen / a) が絞り込みヘッダーを出したままタブの行へ黙って戻してしまう。
func (v *issuesView) visibleIssues() []*issues.Issue {
	if v.numFilter.active {
		return v.numFilter.rows(v.all)
	}
	return v.rowsForTab(v.currentTab())
}

// rowsForTab はタブ名に対応する行集合。⚠️ 疑似カテゴリ [next] はファイル名のカテゴリではなく
// 状態 (next/ に居るか) で選ぶので、issues.Filter へ名前として渡さない (渡すと「@next という
// カテゴリの issue」を探して常に 0 件になる)。
//
// [next] が状態フィルタ (a) を見ないのは、next が段階に関係なく常に見える状態だから
// (issues.StatusFilter.shows)。目印を付けたものが「今の段階では見えない」のは逆の結果になる。
func (v *issuesView) rowsForTab(tab string) []*issues.Issue {
	if tab != tabNextName {
		return issues.Filter(v.all, tab, v.filter)
	}
	out := make([]*issues.Issue, 0, 8)
	for _, iss := range v.all {
		if iss.Status == issues.StatusNext {
			out = append(out, iss)
		}
	}
	return out
}

// reorderTabsByCount は正規順序 tabs を「件数 0 を右へ寄せた」並びへ写す純関数 (件数も同じ並びで
// 返す)。件数 > 0 / 0 の 2 群に分け、各群の中は正規順序を保つ。入力は破壊しない。
//
// ⚠️ human タブは 0 件でも右へ寄せない (All の直後に固定する)。人間待ちのタスクは件数が
// 少ないときこそ見落とすので、席を動かさないことが目的 ([next] を左端に固定するのと同じ規律)。
//
// ⚠️ 表示順だけ変えて巡回順を据え置くと、Tab キーの移動が画面の並びと食い違う (右端に見えるタブへ
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

// setNotice は操作結果を置く (ok=false は失敗)。
// ⚠️ ここで無害化する: 通知文は issue のファイル名・本文由来の URL を素で埋め込む呼び出しが
// 多く、呼び出しごとに包むと必ずどこかが漏れる (status_view.go の setNotice と同じ規律)。
func (v *issuesView) setNotice(text string, ok bool) {
	v.notice, v.noticeOK = sanitizePlainLine(text), ok
}

// takeNotice は未表示の操作結果を取り出して消す。browseModel がトーストへ流すための口で、
// 取り出された通知はヘッダーに出さない (トーストとヘッダーで二重に出さない)。
//
// なぜヘッダー行でなくトーストにするか: コピーや URL 起動の結果は glogx 全体で右下トーストに
// 出す語彙で統一されている (ユーザー要望 2026-07-31)。viewer が全画面でトーストが隠れていた
// 時代の名残でヘッダーに出していたが、トーストを viewer の上にも合成するようにしたので不要。
func (v *issuesView) takeNotice() (string, bool) {
	text, ok := v.notice, v.noticeOK
	v.notice, v.noticeOK = "", false
	return text, ok
}

// tabNextName は疑似カテゴリ [next] の識別子 (保存・復元でもこの名前で持つ)。
//
// ⚠️ ファイル名のカテゴリトークンとして現れない綴りにする: 実在するカテゴリ語と同じ綴りだと
// 同名のタブが 2 つ並び、位置で持つ選択 (tabIdx) の指す先が曖昧になる (issues.Tabs が other で
// 同じ問題を合算で回避しているのと同型)。トークンは英数と - からしか作られないので @ を使う。
const tabNextName = "@next"

// tabIdx の規約: -1 = 疑似カテゴリ [next]、0 = All、1.. = v.tabs[tabIdx-1]。
//
// ⚠️ next を -1 にして All を 0 のままにするのは zero value のため。0 を next にすると、
// 作りたてのビュー (newIssuesView / テストの zero value) が既定で [next] を選ぶことになり、
// 「開いたら空の一覧が出る」挙動に変わる。
const tabIdxNext = -1

// currentTab は選択中のタブ名 ("" = All、tabNextName = 疑似カテゴリ [next])。
func (v *issuesView) currentTab() string {
	if v.tabIdx == tabIdxNext {
		return tabNextName
	}
	if v.tabIdx <= 0 || v.tabIdx > len(v.tabs) {
		return ""
	}
	return v.tabs[v.tabIdx-1].Name
}

// closeBody は本文モードを抜ける。
// closeBody は本文を閉じる。⚠️ ここでは逆再生を始めるだけで中身は消さない — 消すと閉じる
// 演出に何も映らない。実際の破棄は演出が着地したとき (settleDrawer)。
func (v *issuesView) closeBody() {
	// 後始末は本文の有無に依らず行う (呼ばれた時点で「本文モードではない」を満たすべき)。
	v.bodyGlide.stop()
	v.urlPick.close()
	if v.open == nil {
		return
	}
	v.drawer.startClose(timeNow())
}

// discardBody は本文の状態を実際に捨てる (演出の着地後・viewer を閉じるとき)。
func (v *issuesView) discardBody() {
	v.open, v.body, v.bodyOff = nil, nil, 0
	v.bodyGlide.stop()
	v.urlPick.close()
	v.drawer = issuesDrawer{}
}

// settleDrawer は引き出しの演出が終わっていれば静止状態へ進め、閉じ切っていれば本文を捨てる。
// 描画とキー処理の両方から呼ぶ (どちらが先に来ても状態が進む)。
func (v *issuesView) settleDrawer() {
	if v.drawer.settle(timeNow()) {
		v.discardBody()
	}
}

// openBody はカーソル位置の issue の本文を読む。
//
// 読み込みは同期で行う: 1 ファイルの読み込みは sub-ms で、非同期にすると「読み込み中に
// カーソルが動いた」等の状態を増やすだけで得がない (スキャンと違い件数に比例しない)。
func (v *issuesView) openBody() {
	if iss := v.current(); iss != nil {
		v.openIssue(iss)
	}
}

// openIssue は指定した issue の本文を開く (読めたら true)。カーソル行を開く openBody と、
// 前回終了時の画面をパスから開き直す applyScreen が共有する。
func (v *issuesView) openIssue(iss *issues.Issue) bool {
	v.clearMark() // 本文は 1 件の操作。選択を残すと y が一覧の範囲へ効いて対象が食い違う
	body, err := iss.ReadBody()
	if err != nil {
		v.setNotice("本文を読めませんでした: "+firstLine(err.Error()), false)
		return false
	}
	v.open, v.body, v.bodyOff = iss, body, 0
	v.urlPick.close() // 別の issue を開いたら前の URL 一覧を持ち越さない
	v.drawer.open(timeNow())
	v.bodyGlide.stop()
	return true
}

// reloadAfterEdit は nvim で編集して戻ってきたときの取り直し (呼び出し側の editorClosedMsg)。
//
// 一覧のメタデータは Issue.LoadMeta が一度読んだら二度読まないので再スキャンで作り直し、開いて
// いる本文は Body が読み込み時の内容を握っているので読み直す。どちらもしないと、編集した当人に
// 対して viewer が編集前の内容を出し続ける (仕様が最も嫌う「viewer が確信を持って嘘をつく」型)。
func (v *issuesView) reloadAfterEdit() tea.Cmd {
	if !v.shown {
		return nil
	}
	if v.open != nil {
		if body, err := v.open.ReadBody(); err == nil {
			v.body = body // bodyOff は保つ (描画側が新しい行数へ収束させる)
		}
	}
	return v.scanAfterChangeCmd()
}

// current はカーソル位置の issue (無ければ nil)。
func (v *issuesView) current() *issues.Issue {
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		return nil
	}
	return v.rows[v.cursor]
}

// ownsKeys は viewer 自身がキーを解釈し切る状態か (URL ピッカー入力中 / 番号の絞り込み入力中 /
// 「次にやる」の y/N 確認中)。statusView.ownsKeys と対。
//
// ⚠️ browseModel 側の U 横取り (tui.go の issuesOv.visible() ブロック) は、この状態では止める。
// 止めないと viewer が持つキー語彙を外側が奪う: URL ピッカーは
// 「印字文字はすべて検索語に流す (個別のキーを先に横取りすると、その文字を含む URL を
// 検索できなくなる)」と宣言しているのに、大文字 `U` だけが残量モーダルに化けていた
// (issue 113。`github.com/Ueno/...` のような URL を絞り込めない)。
//
// ⚠️ ガードは横取りだけに掛けること: 委譲ごと飛ばすと viewer がキーを受け取れなくなる
// (status 側が実装中に踏んだ罠。status_view.go:ownsKeys の注記と同じ)。
//
// numFilter は active でなく **typing** を見る: 絞り込みが効いているだけの状態
// (数字を打ち終わった後) は通常のナビゲーションなので、U は外側で受けてよい。
// active を見ると「絞り込みを解くまで U が恒久的に死ぬ」。
//
// ⚠️ numFilter を入れている理由は URL ピッカーとは別で、**今日の実利ではない** (敵対的レビューの
// 指摘。numFilter は数字しか受けないので、U が検索語になることは今は無い。むしろ入力中の U は
// 無言の no-op になる = 機能が 1 つ減る)。それでも入れているのは、
// docs/issues-viewer-spec.md が「タイトル検索を足すときは『数字以外を無視する』を外して
// 検索語へ流す形になる」と予告しているため。実装した日に同じ穴を開け直さないよう、
// 「印字入力を食うモード」としてここで一括して扱う。
// なお status 側の pager も U を無言で飲む (割当が無い) ので、この repo の既存の作法とは一貫する。
func (v *issuesView) ownsKeys() bool {
	return v.urlPick.active || v.numFilter.typing || v.markNext.active
}

// handleKey は viewer 表示中のキーを処理する。
//
// 全画面モーダルなのでキーは全部ここで飲む (呼び出し側へ素通りさせない)。素通りさせると
// 一覧を見ている最中の u が git pull --rebase の確認を開く類の誤爆になる: 呼び出し側の
// dispatch は裸の b / u を push / pull に割り当てているため。
//
// page は画面に使える行数 (hint 行を除く)。リスト/本文に実際に使える行数はヘッダーを
// 差し引いた visibleRows で、描画と同じ値を使う (ずれると G が末尾に届かなくなる)。
func (v *issuesView) handleKey(key string, vp issuesViewport) tea.Cmd {
	// このビューは browseModel なしでも駆動できる契約なので、Space の正規化も自分で通す
	// (呼び出し側の normalizeSpaceKey と同じ関数。理由はそちらのコメント)
	key = normalizeSpaceKey(key)
	v.finishAnim() // 演出中のキーは即着地させる (q が効かない時間を作らないため)
	if v.drawer.finish() {
		v.discardBody() // 引き出しの逆再生中にキーが来たら即座に閉じ切る
	}
	// 通知は takeNotice で取り出された時点で消えるので、ここでのクリアは不要 (取り出されない
	// まま次のキーが来た場合だけ古い結果が残るが、browseModel は毎キーで取り出す)
	rows := v.visibleRows(vp)
	// ⚠️ 確認モーダルは最優先で飲む (実ファイルを動かす操作なので、裏のキーを効かせない)
	if v.markNext.active {
		return v.markNextKey(key)
	}
	// ⚠️ URL ピッカーは他のどの割当よりも先に飲む: インクリメンタルサーチでは印字文字がすべて
	// 検索語なので、e/v (エディタ) や y (コピー) を先に処理すると "e" や "y" を含む URL を
	// 検索できない (e は example / .dev / developer に頻出するので実害が大きい)。
	if v.urlPick.active {
		return v.urlPickerKey(key)
	}
	// 番号入力中も同じ理由で先に飲む: 打った数字が検索語なので、一覧のキーへ素通りさせると
	// 数字キーに割当を足した瞬間に「検索語を打つと画面が動く」ことになる
	if v.numFilter.typing {
		return v.numberFilterKey(key, rows)
	}
	// モードに依らないアクションキーは先に飲む (対象は target() が一覧/本文で切り替える)
	if cmd, ok := v.actionKey(key); ok {
		return cmd
	}
	if v.open != nil {
		return v.handleBodyKey(key, rows)
	}
	switch key {
	case "q", "esc":
		// 選択中は解除を優先する (tig 流に 1 段戻る)。選択したまま終了すると、再開記憶で
		// 次に開いたとき解除の手段が無い状態から始まる
		if v.marked {
			v.clearMark()
			return nil
		}
		// 絞り込みも同じく 1 段戻る (絞り込んだまま終了すると、次に開いたとき「なぜか件数が
		// 少ない一覧」から始まる)
		if v.numFilter.active {
			v.numFilter.clear()
			v.refresh()
			return nil
		}
		// 戻る段が無ければ glogx ごと終了する (ユーザー要望 2026-08-06: git log 一覧へは
		// 戻らない。一覧へ戻りたいときは i の toggle)。quit は browseModel が行う (takeWantQuit)
		v.wantQuit = true
	case "i":
		// i は toggle で一覧へ戻る (閉じる意図が明確なので 1 段戻りはしない)
		v.close()
	case "s":
		// status viewer への横断 (ユーザー要望 2026-08-06)。q/esc と違い選択・絞り込みの
		// 1 段戻りはしない (「status を見たい」意図が明確なため)。確認モーダル・URL ピッカー・
		// 番号入力中はここまで届かないので誤爆しない。閉じ→開きは browseModel (takeWantStatus)
		v.close()
		v.wantStatus = true
	case "R":
		// ratelimit ダッシュボードへの横断 (ユーザー要望 2026-09-01)。s と同じ扱い
		// (全画面どうしの入れ替えなので閉じてから開く)。ダッシュボード側の i と対で往復できる。
		// ⚠️ hint には入れない (1 行が popup 実幅に詰まっている。i と同じ理由で --help と
		// README が正本。issues_view.go:hint の注記)
		v.close()
		v.wantRatelimit = true
	case "u":
		// ⚠️ 黙って無視しない。u は git log 一覧では pull、本文では URL ピッカー、status viewer では
		// 「pull は p です」を返す — 一覧だけ無音だと「押したのに何も起きない」= 壊れて見える
		// (status viewer 側で明文化されている規律。issue 122)。
		// 効かせない理由は openURLPicker の doc: 一覧で押せるようにするとキー 1 打ごとにファイルを
		// 読むことになり、しかも「その issue に URL があるか」は一覧に出ていない。
		v.setNotice("URL 一覧は本文を開いてから u で出します", false)
	case "/":
		v.numFilter.start() // 絞り込み中なら続きから打てる (行集合は変わらないので refresh 不要)
	case "j", "down", "ctrl+n":
		v.moveCursor(1, rows)
	case "k", "up", "ctrl+p":
		v.moveCursor(-1, rows)
	// 範囲選択 (ユーザー要望 2026-08-01)。y / p / Y が選択範囲へ効く。
	// 移動が矢印と j/k の 2 系統あるので、伸張も両方に付ける (K = shift+k、J = shift+j)。
	// ⚠️ 矢印だけにしない: shift+矢印は端末・多重化 (tmux) の設定次第でアプリまで届かないことが
	// あり、そのとき機能ごと沈黙する。素の大文字は必ず届くので、確実に動く経路を必ず 1 本持たせる。
	case "shift+up", "K":
		v.extendMark(-1, rows)
	case "shift+down", "J":
		v.extendMark(1, rows)
	case "ctrl+d", "pgdown", " ", "f":
		v.moveCursor(max(rows/2, 1), rows)
	case "ctrl+u", "pgup", "b", "shift+space":
		v.moveCursor(-max(rows/2, 1), rows)
	case "g", "home":
		v.clearMark()
		v.cursor, v.offset = 0, 0
	case "G", "end":
		v.clearMark()
		v.cursor = max(len(v.rows)-1, 0)
		v.scrollToCursor(rows)
	case "tab", "l", "right":
		if v.numFilter.active {
			break // 絞り込み中はカテゴリの概念が無い (ヘッダーもタブ行を出していない)
		}
		v.moveTab(1)
	// ctrl+b で左へ (ユーザー要望 2026-07-31)。右は tui.go が ctrl+f を "right" へ正規化するので
	// 既に効いており、ここで足すのは対になる左だけ。C-b を「←の別名」として全ビューに広げないのは
	// 一覧・パネル側の left に別の意味を与えないため (tui.go の C-f 正規化の注記を参照)。
	case "shift+tab", "h", "left", "ctrl+b":
		if v.numFilter.active {
			break
		}
		v.moveTab(-1)
	case "enter", "o":
		v.openBody()
	case "n":
		v.askMarkNext()
	case "a":
		if v.numFilter.active {
			break // 番号検索は状態を無視するので、押しても画面が変わらない (裏で状態だけ変える方が悪い)
		}
		v.filter = v.filter.Next()
		v.refresh()
	case "r":
		return v.scanCmd(v.cwd) // loaded は落とさない (取り直し中も前回の結果を出したままにする)
	}
	return nil
}

// numberFilterKey は番号を入力しているあいだのキー。
//
// ⚠️ 数字と編集キー以外の印字文字は捨てる (無視して入力を続ける)。一覧のキーとして実行しない
// のは issuesNumberFilter の doc の理由による。
func (v *issuesView) numberFilterKey(key string, rows int) tea.Cmd {
	switch key {
	case "esc", "ctrl+g":
		v.numFilter.clear()
		v.refresh()
	case "enter":
		v.numFilter.confirm()
		v.refresh() // 空入力の確定では絞り込みが消えるので、行集合を作り直す
	case "down", "ctrl+n":
		v.moveCursor(1, rows)
	case "up", "ctrl+p":
		v.moveCursor(-1, rows)
	default:
		if v.numFilter.edit(key) {
			v.cursor = 0 // 絞り込み直後は先頭を見せる (urlPicker と同じ)
			v.refresh()
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
func (v *issuesView) actionKey(key string) (tea.Cmd, bool) {
	switch key {
	case "v", "e":
		// e は git log 一覧の e (nvim を repo root で開く) と語彙を揃えたもの。v は先にあった
		// 割当で、打ち慣れを壊さないため残す (本文モードの hint が案内するのは e だけ)。
		//
		// ⚠️ 閉じる演出中 (issuesCloseDuration) の e はここへ来ない: tui.go が
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
// ⚠️ 描画側 (listLines / bodyLines) と同じ式・同じ幅で数えること。片方だけ幅が違うと、幅で
// 折り返すヘッダーを足した瞬間にキー側と描画側の page 分割が食い違い、半ページ移動の距離や
// カーソルと窓の関係が静かにずれる (描画側には収束処理があるので症状から原因へ辿り着けない)。
// 一致は TestIssuesLayoutAgreesBetweenKeysAndRender が固定する。
func (v *issuesView) visibleRows(vp issuesViewport) int {
	return max(vp.page-len(v.headLines(v.headWidth(vp.width), false)), 1)
}

// headWidth はヘッダーを組む幅。本文は引き出しの内側に描くので、そちらの幅で数える。
func (v *issuesView) headWidth(total int) int {
	if v.open != nil {
		return v.bodyWidth(total)
	}
	return total
}

// bodyWidth は本文を組む幅 (引き出しの内側)。⚠️ 演出中の途中幅ではなく着地後の幅で組む:
// 途中幅で整形し直すと毎フレーム折り返しが変わって文字が踊る (composeDrawer の doc)。
func (v *issuesView) bodyWidth(total int) int { return max(v.drawer.targetWidth(total)-1, 1) }

// handleBodyKey は本文 pager のキー操作 (diffOverlay と同じ語彙)。
func (v *issuesView) handleBodyKey(key string, rows int) tea.Cmd {
	switch key {
	// Enter は「TUI 内の開閉 toggle」(ユーザー要望 2026-08-01)。一覧の Enter で開き、本文の
	// Enter で閉じる。glogx 本体の job パネル (tui.go の handlePanelKey) が既にこの語彙なので、
	// viewer だけ Enter が行送りだと同じキーの意味が画面ごとに変わる。
	// ⚠️ pagerScrollKey へ渡す前に捌くこと: あちらは enter を 1 行送りに写す。
	case "q", "esc", "h", "left", "enter":
		v.closeBody()
	case "i":
		// i は本文からも効く (一覧の i と同じ toggle。**s と同じ理由**: --help と README が
		// 「viewer 内のキー」として i を案内しており、本文だけ沈黙すると案内が嘘になる。issue 122)。
		// ⚠️ 本文だけ畳む 1 段戻りにはしない: それは Enter / q / h が既に持っている語彙で、
		//   README の「i で閉じて一覧へ戻る」とも食い違う。
		v.close()
	case "s":
		// status viewer への横断は本文からも効く (一覧の s と同じ。--help が「viewer 内のキー」
		// として案内しており、本文だけ沈黙すると案内が嘘になる)
		v.close()
		v.wantStatus = true
	case "R":
		// ratelimit ダッシュボードへの横断も本文から効く (s と同じ理由)
		v.close()
		v.wantRatelimit = true
	case "u":
		v.openURLPicker()
	default:
		// スクロールの語彙 (1 行 / 半ページ + glide / 端ジャンプ) は diffOverlay・status viewer の
		// 全画面 diff と共有する (scroll_glide.go の pagerScrollKey)。手触りを 1 箇所に集約するため。
		v.bodyOff = pagerScrollKey(key, v.bodyOff, rows, v.body.Len(), &v.bodyGlide)
	}
	return nil
}

// moveCursor はカーソルを動かしてスクロール位置を追従させる。素の移動は選択を解除する
// (選択したまま離れた場所へ動くと「見えていない範囲がコピー対象」になる)。
func (v *issuesView) moveCursor(delta, rows int) {
	v.clearMark()
	v.setCursor(v.cursor+delta, rows)
}

// setCursor はカーソルを位置 i へ置いて窓を追従させる (選択には触れない)。
func (v *issuesView) setCursor(i, rows int) {
	v.cursor = clampIdx(i, len(v.rows))
	v.scrollToCursor(rows)
}

// extendMark は shift+↑/↓ の伸張。初回は今の行を錨にしてから動くので、1 回押すと
// 「元の行 + 隣の行」の 2 行が選択される (エディタ・Finder と同じ)。
func (v *issuesView) extendMark(delta, rows int) {
	if len(v.rows) == 0 {
		return
	}
	if !v.marked {
		v.marked, v.markAt = true, v.cursor
	}
	v.setCursor(v.cursor+delta, rows)
}

// clearMark は選択を解除する。
func (v *issuesView) clearMark() { v.marked, v.markAt = false, 0 }

// selection は選択範囲 [lo, hi] (両端を含む)。選択していなければ ok=false。
// ⚠️ 錨は行集合の入れ替えで無効になりうるので、範囲は必ず今の rows へ収めてから返す。
func (v *issuesView) selection() (lo, hi int, ok bool) {
	if !v.marked || len(v.rows) == 0 {
		return 0, 0, false
	}
	lo, hi = min(v.markAt, v.cursor), max(v.markAt, v.cursor)
	return max(lo, 0), min(hi, len(v.rows)-1), true
}

// selectedRows は操作の対象 (選択中ならその範囲、選択していなければ対象 1 件)。
func (v *issuesView) selectedRows() []*issues.Issue {
	if lo, hi, ok := v.selection(); ok {
		return v.rows[lo : hi+1]
	}
	if iss := v.target(); iss != nil {
		return []*issues.Issue{iss}
	}
	return nil
}

// scrollToCursor はカーソルが画面内に入るまで offset を寄せる。
func (v *issuesView) scrollToCursor(rows int) { v.offset = v.windowOffset(rows) }

// windowOffset は一覧の描画開始行 (論理 offset)。カーソルを必ず含み、末尾では余白を作らない
// 位置へ寄せる。
//
// ⚠️ offset は独立した状態ではなく (cursor, 行数, 表示行数) からの導出値として扱う。表示行数は
// キー処理時と描画時でずれる: 通知行が出てヘッダーが 1 行増える / リサイズで page が変わる /
// タブ・フィルタ切替で行数が変わる。offset を状態として持ち回ると、そのずれが「カーソル行が
// 1 本も描かれず、見えない行が Enter・v・y の対象になる」窓として残る。導出を
// scrollToCursor (キー) と listLines (描画) の両方が通すことで食い違いを構造的に消す。
func (v *issuesView) windowOffset(rows int) int {
	if rows <= 0 {
		return 0
	}
	return windowOffsetFor(v.offset, v.cursor, len(v.rows), rows)
}

// moveTab はタブを切り替える (端で止まらず巡回する)。
func (v *issuesView) moveTab(delta int) {
	// [next] (-1) と All (0) を含めた巡回。-1 起点なので +1 して 0 起点へ寄せてから回す
	n := len(v.tabs) + 2
	v.tabIdx = ((v.tabIdx+1+delta)%n+n)%n - 1
	v.refresh()
}

// target は操作対象の issue (本文モードなら開いているもの、一覧ならカーソル行)。
func (v *issuesView) target() *issues.Issue {
	if v.open != nil {
		return v.open
	}
	return v.current()
}

// editCmd は対象の issue を $VISUAL / $EDITOR (未設定なら nvim) で開く。job ログ (scratch
// バッファ) と違い実ファイルなので readonly にしない: viewer から直接メモを足したくなるため。
//
// エディタの解決を editorCommand に寄せているのは、これが glogx で唯一「実ファイルを 1 つ開く」
// 経路で、任意の $EDITOR で成立するため (据え置いた 2 箇所の理由は editorCommand の doc)。
// ⚠️ 起動前に実体を確かめる。一覧が握る Issue.Path は n (next/ へ移動) や別プロセスの
// rename/削除で stale になり、そのパスを渡すとエディタは黙って**新規バッファ**として開く
// (nvim はエラーにしない)。そこで保存すると旧位置にファイルが復活し、issues/move.go が
// 「同じ basename を 2 箇所に作らない」と宣言している不変条件を viewer 自身が破る。
// 開かずに取り直す方に倒す (古い一覧のまま編集させない)。
func (v *issuesView) editCmd() tea.Cmd {
	iss := v.target()
	if iss == nil {
		return nil
	}
	if _, err := os.Stat(iss.Path); err != nil {
		v.setNotice("実体が見つかりません (一覧を取り直します): "+iss.Rel, false)
		return v.scanAfterChangeCmd()
	}
	return runEditorCmd(editorCommand(iss.Path))
}

// openURLPicker は本文中の URL のピッカーを開く (u キー)。URL が無ければ開かずに通知する。
//
// 一覧モードでは効かない (本文を読んでいないため)。一覧で押せるようにするとキー 1 打ごとに
// ファイルを読むことになり、しかも「その issue に URL があるか」は一覧に出ていない。
func (v *issuesView) openURLPicker() {
	if v.body == nil {
		return
	}
	if !v.urlPick.open(v.body.URLs()) {
		v.setNotice("この issue に URL はありません", false)
	}
}

// urlPickerKey はピッカー表示中のキーを捌く。確定 (Enter) でブラウザを開く Cmd を返す。
func (v *issuesView) urlPickerKey(key string) tea.Cmd {
	url := v.urlPick.selected() // 確定前に読む (close で状態が消えるため)
	open, _ := v.urlPick.handleKey(key)
	if !open || url == "" {
		return nil
	}
	v.urlPick.close()
	// 「開きました」と断定しない: 失敗は openURLMsg 経由でトースト警告になる (browseModel 側)。
	v.setNotice("URL を開きます: "+url, true)
	return func() tea.Msg { return openURLMsg{err: openInBrowser(url)} }
}

// copyPath は対象の issue のパスをクリップボードへ入れる (選択中は範囲ぶん)。
func (v *issuesView) copyPath() {
	v.copyEach("パス", func(iss *issues.Issue) string { return iss.Path })
}

// copyNumber は issue 番号をコピーする (p)。番号は rename も move も生き残る唯一安定した
// 参照形式で、実測でも repo 内 59 箇所・commit message 25 件がこの形。
//
// 番号を持たない issue (素スラッグ。実測で SnapTrim に 4 件) では黙って空をコピーせず、
// ファイル名に落として「番号が無い」ことを通知する。
func (v *issuesView) copyNumber() {
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
func (v *issuesView) copyReference() {
	v.copyEach("参照", func(iss *issues.Issue) string { return iss.Reference(v.root) })
}

// copyEach は対象 (選択中なら範囲、なければ 1 件) から text() を作ってコピーする。
func (v *issuesView) copyEach(label string, text func(*issues.Issue) string) {
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
// ⚠️ 通知には全文を載せない: トーストは 1 行で、改行を含む文字列を渡すと枠が壊れる。
// クリップボードには全件を改行区切りで入れ、通知は件数 + 先頭だけにする。
func (v *issuesView) copyLines(lines []string, label string) {
	if len(lines) == 1 {
		v.copyText(lines[0], label+"をコピーしました: ")
		return
	}
	if err := copyToClipboard(strings.Join(lines, "\n")); err != nil {
		v.setNotice("コピーに失敗しました: "+firstLine(err.Error()), false)
		return
	}
	v.setNotice(strconv.Itoa(len(lines))+" 件の"+label+"をコピーしました: "+lines[0]+" ほか", true)
}

// copyNextNumber は次に採番すべき番号をコピーする (N)。走査済みの全ディレクトリから計算する
// ので、状態ディレクトリを見落として番号を再利用する事故が起きない (issues.NextNumber)。
func (v *issuesView) copyNextNumber() {
	if len(v.all) == 0 {
		return
	}
	v.copyText(issues.NextNumber(v.all), "次の番号をコピーしました: ")
}

// copyText はクリップボードへ入れて結果を通知する (コピー系アクションの共通処理)。
func (v *issuesView) copyText(text, okPrefix string) {
	if err := copyToClipboard(text); err != nil {
		v.setNotice("コピーに失敗しました: "+firstLine(err.Error()), false)
		return
	}
	v.setNotice(okPrefix+text, true)
}

// カテゴリの色。意味が広く共有されている語には固定色を割り、表に無い語は語のハッシュで
// 安定に (起動ごとに変わらないように) 割る。カテゴリ語彙は repo ごとに違い、実測で
// 「変更種別 19 語 / サブシステム名体系 / トークンなし」が併存するので表だけでは足りない。
//
// 色番号は 256 色 (docs/theme-colors.md の 256 色主環境・gruvbox 基調に合わせた bright 系)。
const (
	catRed    = "\x1b[38;5;167m" // bug / fix / security — 失敗系の赤 (本体の ansiRed と同じ意味)
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
// ⚠️ 赤を入れない: 意味を持たない語 (サブシステム名など) が「失敗」の色で出ると誤読される。
// 赤は bug / fix / security に予約する。
var catHashPalette = []string{catGreen, catTeal, catYellow, catBlue, catPurple, catGold, catGray}

// categoryColor は語に対する色を返す (同じ語なら常に同じ色)。カテゴリ無しは dim。
func categoryColor(name string) string {
	if name == "" {
		return ansiDim
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
// 別の型にしてある。⚠️ 幅を落とさないこと: 幅を知らずに page を分割していた頃は、ヘッダーを
// 幅 0 で数えるしかなく「ヘッダーは折り返してはいけない」という暗黙の前提を抱えていた。
type issuesViewport struct {
	width int
	page  int
}

type issuesRenderOpts struct {
	width   int
	page    int
	colored bool
	spinner string
	// cursorPaint はカーソル行の強調 (browseModel の bgLine を渡す)。nil なら太字だけで示す
	// = テストと NO_COLOR 用。
	cursorPaint func(string) string
}

// viewport は描画情報から窓の寸法だけを取り出す (キー処理へ渡す形)。
func (o issuesRenderOpts) viewport() issuesViewport {
	return issuesViewport{width: o.width, page: o.page}
}

// lines は全画面ビューの page 行を返す (常にちょうど page 行。呼び出し側の枠・hint 経路を
// 変えずに差し替えられるようにするため)。
func (v *issuesView) lines(o issuesRenderOpts) []string {
	v.settleDrawer() // 閉じ切っていたら本文を捨ててから組む (描画とキーのどちらが先でも進む)
	var body []string
	switch {
	case v.urlPick.active:
		body = v.urlPick.lines(o)
	case v.open != nil:
		// 本文は「一覧の上に左から開く引き出し」として重ねる。全画面で置き換えると、どの一覧の
		// どこから開いたかが画面から消える (ユーザー要望 2026-07-31: Notion の peek のように)。
		w := v.drawer.width(o.width, timeNow())
		inner := o
		// 整形は最終幅で行い、演出中は切るだけにする (途中幅で整形し直すと毎フレーム折り返しが
		// 変わって文字が踊る。詳細は composeDrawer の doc)
		inner.width = v.bodyWidth(o.width)
		body = composeDrawer(padTo(v.listLines(o), o.page), padTo(v.bodyLines(inner), o.page),
			w, o.width, o.colored)
	default:
		body = v.listLines(o)
	}
	body = padTo(body, o.page)
	// 確認モーダルは最前面 (実ファイルを動かす操作なので、裏の一覧より確実に目に入る位置)
	if box := v.markNextBox(o.width, o.colored); len(box) > 0 {
		body = overlayCenteredBox(body, box, o.width, o.page, o.colored)
	}
	if p := v.animProgress(); p < 1 {
		body = slideInWindow(body, p, o.width, v.closing)
	}
	return body
}

// animProgress は開く演出の進み (0..1)。演出していないときは 1 (= 変形しない)。
//
// 閉じるときは進捗を 1 → 0 へ落とす (所要も別で、issuesCloseDuration の方が短い)。⚠️ 反転する
// のは進捗の向きだけで、見え方 (緩急・行ごとのずらし) まで開く演出の逆再生にはしない
// (理由は rowOffsetRatio の doc)。
func (v *issuesView) animProgress() float64 {
	if v.closing {
		return max(1-float64(timeNow().Sub(v.animStart))/float64(issuesCloseDuration), 0)
	}
	if !v.animating() {
		return 1
	}
	return float64(timeNow().Sub(v.animStart)) / float64(issuesAnimDuration)
}

// padTo は行数を n へ揃える (足りなければ空行、多ければ切る)。引き出しの合成では一覧と本文の
// 行数が違う (ヘッダーの行数が異なる) ため、重ねる前に必ず揃える。窓を「ちょうど page 行」で
// 返す契約 (lines の doc) もこれで満たす。
func padTo(lines []string, n int) []string {
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines[:n]
}

// slideInWindow は窓の各行を「右から左へ流し込む」途中の姿にする (closing なら右へ抜ける途中)。
// 画面の外に居る行は空にする = 右端から現れ、右端へ消える。
func slideInWindow(window []string, progress float64, width int, closing bool) []string {
	out := make([]string, 0, len(window))
	last := max(len(window)-1, 1)
	for i, ln := range window {
		ratio := rowOffsetRatio(progress, i, last, closing)
		switch {
		case ratio <= 0:
			out = append(out, ln) // 着地済み
			continue
		case ratio >= 1 || ln == "":
			out = append(out, "") // まだ画面外 (または元から空行)
			continue
		}
		off := int(math.Round(ratio * float64(width)))
		if off >= width {
			out = append(out, "") // 端数で幅ぴったりまで押し出された (空白だけの行を残さない)
			continue
		}
		out = append(out, padSpaces(off)+clipToWidth(ln, width-off))
	}
	return out
}

// rowOffsetRatio は行 i の横ずれを幅に対する割合で返す (0 = 着地、1 = 画面外)。
//
// 入ってくる向きだけ easeOutCubic で終端を減速させ、行ごとに開始をずらす (stagger)。着地する
// ものは減速しないと「カクッ」と止まって見え、ずらしがあると上から順に流れ込んで見える。
//
// ⚠️ 出ていく向きにこの作法を流用しないこと。板には着地点が無いので、終端の減速は「もう画面から
// 消えているのに畳まれない時間」に化ける (easeOutCubic だと 280 桁端末で残り 100ms が真っ白)。
// ずらしも視線のある最上行を最後に回して動き出しを遅らせる。出ていくときは全行同時・等速。
func rowOffsetRatio(progress float64, i, last int, closing bool) float64 {
	if closing {
		return 1 - progress // 板が 1 枚まるごと等速で右へ抜ける
	}
	delay := issuesAnimStagger * float64(i) / float64(last)
	return 1 - easeOutCubicFloat((progress-delay)/(1-issuesAnimStagger))
}

// easeOutCubicFloat は 0..1 の進みを easeOutCubic で写す (終点付近で減速)。
// toast.go の easedShown はフレーム数ベースの同種の計算だが、あちらは箱幅への写像まで
// 含むため共有していない (こちらは壁時計ベースの進みだけを扱う)。
func easeOutCubicFloat(p float64) float64 {
	q := 1 - p
	return 1 - q*q*q
}

// headLines は現在のモードのヘッダー行 (一覧ならタブ行 + 通知、本文ならパス + 状態)。
// キー操作側 (visibleRows) と描画側で同じ関数を通すことで行数のずれを防ぐ。width=0 は
// 行数だけが必要な呼び出し (中身は使われない)。
// headLines はモードに応じたヘッダー行。
//
// ⚠️ 返す行数は width に依らないこと (幅で折り返さず clipToWidth で切る)。キー処理は描画幅を
// 知らないまま幅 0 で数えている (visibleRows) ため、幅で行数が変わるヘッダーを足すと page の
// 分割がキー側と描画側で食い違う。折り返したいヘッダーが要るなら、まず visibleRows へ幅を
// 通す経路を作ること (handleKey のシグネチャまで届く変更になる)。
func (v *issuesView) headLines(width int, colored bool) []string {
	if v.open != nil {
		return v.bodyHeadLines(width, colored)
	}
	return v.listHeadLines(width, colored)
}

// bodyHeadLines は本文モードのヘッダー (ファイル名 + 状態)。
//
// 操作結果 (コピー・URL 起動) の行はここに持たない: browseModel が takeNotice で取り出して
// 右下トーストに出す (ユーザー要望 2026-07-31)。以前は「トーストは全画面差し替えの下に隠れる」
// ためここが唯一の受け皿だったが、viewer の上にもトーストを合成するようにして解消した。
func (v *issuesView) bodyHeadLines(width int, colored bool) []string {
	status := v.open.StatusLabel()
	// 進捗は開いている本文から数える (Issue 側で持つと一覧を出すたびに全 issue の全文を
	// 読むことになる。issues/body.go の Body.Progress の doc)。
	//
	// ⚠️ 鮮度は v.body と同じで、それ以上ではない: 取り直しが失敗したとき
	// (reloadAfterEdit は err == nil のときだけ v.body を差し替える) は本文も進捗も
	// 古いまま残る。本文テキストの stale は以前からある性質で、進捗をここへ移したことで
	// **本文と進捗の鮮度が揃った** (以前は Issue 側の進捗だけ別経路で更新されていた)
	if v.body != nil {
		if p := v.body.Progress(); p != "" {
			status += "  " + p
		}
	}
	return []string{
		// Rel はファイル名 = 外部由来。ファイルを開く同一性は v.open.Path 側が持つので、
		// 画面に出すこちらだけ無害化する (worktreeRow.dispPath と同じ分け方)。
		paint(clipToWidth(sanitizePlainLine(v.open.Rel), width), ansiBold, colored),
		paint(clipToWidth(status, width), ansiDim, colored),
		"",
	}
}

// listHeadLines は一覧のヘッダー (タブ + スキャン警告)。
//
// ⚠️ headLines と分けているのは引き出しのため: 本文を開いている間も下地の一覧はタブを出す
// 必要があり、headLines をそのまま使うと下地にまで本文のヘッダーが出る (実測で「一覧の上に
// 本文のファイル名が乗る」表示になった)。
//
// スキャン警告 (同名ファイルの二重化 = 静かな内容喪失) はここに残す: 操作結果と違って
// 「今この repo が抱えている状態」なので、消えるトーストでなく画面に出続ける必要がある。
// 操作結果はトーストへ移した (takeNotice)。
func (v *issuesView) listHeadLines(width int, colored bool) []string {
	// 絞り込み中はタブ行を検索行に差し替える (両方出さない: タブは無視される概念なので、
	// 並べると「どのタブの中を検索しているのか」という無い関係を読ませてしまう)
	head := make([]string, 0, 3)
	if v.numFilter.active {
		head = append(head, v.numberFilterLine(width, colored))
	} else {
		head = append(head, v.tabLine(issuesRenderOpts{width: width, colored: colored}))
	}
	if len(v.warnings) > 0 {
		head = append(head, paint(clipToWidth("⚠ "+v.warnings[0], width), ansiYellow, colored))
	}
	return append(head, "")
}

// listLines はタブ + 警告 + リストを描く。
func (v *issuesView) listLines(o issuesRenderOpts) []string {
	head := v.listHeadLines(o.width, o.colored) // 引き出しの下地でも一覧のタブを出す
	if msg := v.emptyMessage(o); msg != "" {
		return append(head, paint(clipToWidth(msg, o.width), ansiDim, o.colored))
	}
	rows := max(o.page-len(head), 1)
	// 論理 offset を「描画時の行数でカーソルを含む窓」へ収束させる (キー処理時の行数と
	// ずれていても窓とカーソルが食い違わない)。窓はこの導出値そのままなので、カーソルは
	// 定義上必ず含まれる。
	v.offset = v.windowOffset(rows)
	offset := v.offset
	end := min(offset+rows, len(v.rows))
	out := make([]string, 0, rows)
	for i := offset; i < end; i++ {
		// バー列ぶんを先に引く: 幅ぴったりに組むと scrollbarColumn のクリップで末尾 1 文字が
		// "…" に化ける (box.go の scrollbarColumnWidth)
		out = append(out, v.rowLine(i, o, o.width-scrollbarColumnWidth))
	}
	out = scrollbarColumn(out, o.width, len(v.rows), offset, o.colored)
	return append(head, out...)
}

// emptyMessage は「出すものが無い」状態の案内 ("" = リストを描く)。
func (v *issuesView) emptyMessage(o issuesRenderOpts) string {
	switch {
	case v.scanning && !v.loaded:
		// スピナーは初回だけ。取り直しの間は前回の結果を出したままにする (last-good)。開くたびに
		// 再スキャンするので、ここでスピナーに落とすと開く瞬間に毎回一覧が消えて瞬く
		// (usage overlay の「取得中も last-good を維持する」と同じ規律)。
		return o.spinner + " issues を探しています..."
	case len(v.dirs) == 0:
		return "issues ディレクトリが見つかりません (repo root と root/*/issues を探しました)"
	case len(v.rows) == 0 && v.numFilter.active && v.numFilter.query != "":
		// ⚠️ 状態フィルタの案内 (a: pending も表示) を出さない。番号検索は状態を無視しているので、
		// a を押しても結果は 1 件も増えない
		return "番号に「" + v.numFilter.query + "」を含む issue はありません (Esc: 解除)"
	case len(v.rows) == 0 && v.filter == issues.FilterOpen:
		return "このタブに open の issue はありません (a: pending も表示)"
	case len(v.rows) == 0 && v.filter == issues.FilterPending:
		return "このタブに open / pending の issue はありません (a: done も表示)"
	case len(v.rows) == 0:
		return "このタブに issue はありません"
	default:
		return ""
	}
}

// numberFilterLine は番号で絞り込み中のヘッダー (タブ行の代わり)。
//
// 「全カテゴリ・全状態」を必ず出す: 番号検索はタブと状態フィルタの両方を無視するので、書かないと
// 「今 open だけを見ているはず」という直前までの文脈のまま done の issue が並ぶことになる。
// 入力中だけ末尾に "_" を出して、打鍵を待っているのか確定済みなのかを区別できるようにする。
func (v *issuesView) numberFilterLine(width int, colored bool) string {
	caret := ""
	if v.numFilter.typing {
		caret = "_"
	}
	line := "番号: " + v.numFilter.query + caret +
		"  " + strconv.Itoa(len(v.rows)) + " 件 (全カテゴリ・全状態)"
	return paint(clipToWidth(line, width), ansiBold, colored)
}

// tabLine はタブ行 (件数つき) と、右端に有効な状態フィルタを描く。
func (v *issuesView) tabLine(o issuesRenderOpts) string {
	// 件数は refresh が数えた値を読む (毎フレーム・毎打鍵の Filter は全件分の slice を捨てる。
	// visibleRows も行数を得るためにこの関数を通る)
	chips := make([]string, 0, len(v.tabs)+2)
	// [next] は All の左に固定 (ユーザー要望 2026-08-01)。カテゴリの 0 件寄せ (reorderTabsByCount)
	// の対象にしない — 目印を付ける場所として常に同じ位置に居てほしいため、0 件でも左端に出す。
	chips = append(chips, v.tabChip(tabNextName, v.nextCount, v.tabIdx == tabIdxNext, o.colored))
	chips = append(chips, v.tabChip("All", v.allCount, v.tabIdx == 0, o.colored))
	for i, t := range v.tabs {
		count := 0
		if i < len(v.tabCount) {
			count = v.tabCount[i]
		}
		chips = append(chips, v.tabChip(t.Name, count, v.tabIdx == i+1, o.colored))
	}
	filter := v.filter.Badges()
	avail := max(o.width-dispWidth(filter)-1, 1)
	left := scrollTabs(chips, v.tabIdx+1, avail, o.colored) // チップ配列は [next] が 0 番
	pad := max(o.width-dispWidth(left)-dispWidth(filter), 0)
	// ⚠️ 組んだ後に必ず切る (scrollTabs 末尾と同じ規律): avail には下限 1 があるので、
	// バッジ + 印すら入らない極小幅 (o.width ≤ dispWidth(filter)) では合成が幅を超える
	// (issue 053: 幅 1 で「…○」= 2 セルが出ていた。収まる幅では clip は素通りで無 alloc)
	return clipToWidth(left+padSpaces(pad)+paint(filter, ansiDim, o.colored), o.width)
}

// tabScrollMark は「この向きにまだタブがある」ことを示す印 (幅 1 の bare 記号に限る。
// 絵文字は層ごとに幅解釈が割れる。width.go の VS16 の議論と同じ理由)。
const (
	tabScrollLeft  = "‹"
	tabScrollRight = "›"
)

// scrollTabs はタブ行を「選択中のチップが必ず見える窓」へ切り出す (横スクロール)。
// 隠れている側には ‹ / › を出して、その先にタブがあることを示す。
//
// ⚠️ 窓を状態として持たない: (選択位置, チップ幅, 使える幅) からの導出値として毎回作り直す
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
	sel = clampIdx(sel, len(chips))
	mark := func(s string) string { return paint(s, ansiDim, colored) }
	// 印のぶんを先に差し引いてから詰める (印を後付けすると幅を超える)
	reserve := func(start, end int) int {
		w := 0
		if start > 0 {
			w += dispWidth(tabScrollLeft) + 1 // "‹ "
		}
		if end < len(chips) {
			w += 1 + dispWidth(tabScrollRight) // " ›"
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
	// ⚠️ 最後に必ず切る: 1 チップだけで avail を超える極端な狭さでは上のループが縮めきれない。
	return clipToWidth(b.String(), avail)
}

// joinedWidth は空白 1 つで連結したときの表示幅。
func joinedWidth(chips []string) int {
	w := 0
	for i, c := range chips {
		if i > 0 {
			w++
		}
		w += dispWidth(c)
	}
	return w
}

// tabChip は 1 個のタブ表示。カテゴリ色を使い、選択中は太字・非選択は dim で落とす
// (一覧のカテゴリ列と同じ色にすることで「タブの色 = その行の色」が対応する)。
// All は特定のカテゴリではないのでシアン (本体の見出し色) を使う。
func (v *issuesView) tabChip(name string, count int, active bool, colored bool) string {
	label, color := name, categoryColor(name)
	switch name {
	case "All":
		color = ansiCyan // 特定のカテゴリではないので本体の見出し色
	case tabNextName:
		// 疑似カテゴリは識別子 (@next) でなく見出しの綴りで出す。色は状態バッジ ▶ と同じ意味の
		// 「これからやる」を示す固定色にする (語のハッシュだと repo ごとに色が変わる)
		label, color = "next", catYellow
	}
	text := "[" + label + " " + strconv.Itoa(count) + "]"
	if active {
		return paint(text, ansiBold+color, colored)
	}
	return paint(text, ansiDim+color, colored)
}

// issuesSelGutter は選択範囲の行に出す溝。⚠️ 幅は cursorGutterWidth と同じ 2 桁にすること
// (カーソル行の "→ " と混在するので、違う幅だと選択行だけ 1 桁ずれる)。
const issuesSelGutter = "▌ "

// rowLine は一覧の 1 行 (番号・状態バッジ・カテゴリ・タイトル)。width は行が使える
// 表示幅 (スクロールバー列を差し引いた後)。
func (v *issuesView) rowLine(i int, o issuesRenderOpts, width int) string {
	iss := v.rows[i]
	num := fillRight(iss.Number, 3)
	badge := iss.Status.Badge()
	cat := fillRight(clipToWidth(iss.Category, 9), 9)
	catPainted := paint(cat, categoryColor(iss.Category), o.colored)
	// ⚠️ 一覧に進捗 (チェックボックスの n/N) は出さない。数えるには本文を最後まで読む必要が
	// あり、一覧を出すたびに全 issue の全文を読んでいた (起動と外部編集後の再スキャンで毎回)。
	// 進捗は「あると便利」程度で、そのために全件の全文を読むのは釣り合わないと判断した。
	// 詳細を開いたときは Body が全文を持っているので、そこでは追加の I/O なしに出せる
	// (bodyHeadLines)。空いた幅はタイトルへ回る。
	// 一次情報: issues/done/050-perf-glogx-issue-list-reads-full-body.md
	// 溝 + "NNN " + バッジ + " " + カテゴリ + " " + タイトル
	fixed := cursorGutterWidth + dispWidth(num) + 1 + dispWidth(badge) + 1 + dispWidth(cat) + 1
	titleW := max(width-fixed, 4)
	title := clipToWidth(iss.Display(), titleW)
	text := num + " " + badge + " " + catPainted + " " + title
	// ⚠️ どの経路も同じ幅に切る。titleW には下限 (4) があるので、極端に狭い幅では固定部分だけで
	// width を超える。カーソル行だけ切っていたため、そこ以外の行が枠を突き破っていた。
	if i != v.cursor {
		// 選択範囲の行は溝で示す (カーソル行は → が優先。範囲は必ずカーソルを含むので競合しない)
		gutter := cursorGutterBlank
		if lo, hi, ok := v.selection(); ok && i >= lo && i <= hi {
			gutter = paint(issuesSelGutter, ansiCyan, o.colored)
		}
		return clipToWidth(gutter+text, width)
	}
	if o.cursorPaint != nil {
		return o.cursorPaint(clipToWidth(cursorGutterMark+text, width))
	}
	return clipToWidth(cursorGutterMark+paint(text, ansiBold, o.colored), width)
}

// bodyLines は本文 pager (ヘッダー + 本文) を描く。
func (v *issuesView) bodyLines(o issuesRenderOpts) []string {
	header := v.bodyHeadLines(o.width, o.colored)
	rows := max(o.page-len(header), 1)
	// 左の行番号の溝ぶんも整形前に引く (溝を後付けすると幅を超える)。桁数はソース行数から
	// 決める: 整形しないと行番号が分からず、行番号が分からないと溝幅が決まらない循環を切る
	gutter := srcGutterWidth(v.body.SrcLineCount())
	lines := v.body.Lines(o.width-scrollbarColumnWidth-gutter, o.colored) // バー列ぶんも引く
	// 行数は幅で変わる (Body は幅ごとに整形し直す)。幅が広がって行数が減ると論理 bodyOff が
	// 上限を超えたまま残り、k / ctrl+u は max(bodyOff-n, 0) しか見ないので「何度押しても
	// 画面が動かない」打鍵数が生まれる。描画で確定した行数で論理 offset を収束させて防ぐ。
	v.bodyOff = clampScrollOffset(v.bodyOff, len(lines), rows)
	offset := clampScrollOffset(v.bodyGlide.offset(v.bodyOff), len(lines), rows)
	end := min(offset+rows, len(lines))
	nums := v.body.SrcLines()
	out := make([]string, 0, rows)
	for i := offset; i < end; i++ {
		src := 0
		if i < len(nums) {
			src = nums[i]
		}
		out = append(out, srcGutter(src, gutter, o.colored)+lines[i])
	}
	out = scrollbarColumn(out, o.width, len(lines), offset, o.colored)
	return append(header, out...)
}

// srcGutterWidth は行番号の溝の桁数 (番号 + 空白 1)。行数が増えても揃うよう桁数で決める。
func srcGutterWidth(srcLines int) int {
	digits := len(strconv.Itoa(max(srcLines, 1)))
	return max(digits, 2) + 1 // 2 桁未満でも詰まって見えないよう最低 2 桁は取る
}

// srcGutter は 1 行ぶんの行番号の溝。src=0 (折り返しの続き行・畳まれた 2 行目以降) は空白。
//
// ⚠️ 番号は「ソース (.md) の行番号」であって表示行の連番ではない。同じ番号を続き行にも並べると
// 「その行がそこにある」と読めてしまい、外 (nvim / Claude Code) へ持ち出したとき指す先がずれる。
func srcGutter(src, width int, colored bool) string {
	if width <= 0 {
		return ""
	}
	if src <= 0 {
		return padSpaces(width)
	}
	num := strconv.Itoa(src)
	return paint(fillLeft(num, width-1), ansiDim, colored) + " "
}

// hint は viewer 表示中の操作案内 (最下行)。
//
// ⚠️ モードの数は lines() の分岐と揃える (ピッカー / 本文 / 一覧の 3 つ)。揃っていないと、URL
// ピッカー表示中に本文 pager の案内 (j/k/g/G/p/u/e/h/q) が出る — それらは全部 urlPicker が検索語
// として飲むので、案内したキーが 1 つも案内どおりに動かない。
func (v *issuesView) hint() string {
	// ⚠️ hint は 1 行で、幅を超えた分は末尾から黙って切られる。上限は tmux popup の実幅で、
	// 数値は testPopupWidth (テスト側の代表値) に置き TestIssuesViewHintFitsPopupWidth が固定する
	// — production はこの値を持たない (幅は端末から決まる) ので、ここに数字を書くと乖離する。
	// 収まる範囲へ絞り、絞られたキー (y / Y / r / 一覧の p) は --help と README を正本にする。
	// nvim を開くキーは e と v の 2 本あるが、案内するのは e だけ (v は打ち慣れのための別名で、
	// 幅で絞ったのではなく意図的に出さない)。一覧モードは幅の都合でどちらも案内しない。
	if v.urlPick.active {
		// 件数と 1 字消し (ctrl+h) はピッカー自身のヘッダーが出すので繰り返さない。ここは
		// 「打った文字がそのまま絞り込みになる」= 本文 pager のキーが効かないことだけを伝える。
		return "文字入力で絞り込み  ctrl+n/p: 移動  Enter: 開く  Esc: 戻る"
	}
	if v.numFilter.typing {
		// 打鍵がすべて検索語になる = 一覧のキーが効かないことを伝える (urlPick と同じ理由)
		return "数字で絞り込み  ctrl+n/p: 移動  Enter: 確定  Esc: 解除"
	}
	if v.open != nil {
		// エディタ名を書かないのは editCmd が $VISUAL/$EDITOR を見るため ($EDITOR=code の人に
		// "nvim" と案内しない)。幅は TestIssuesViewHintFitsPopupWidth が固定する。
		return "j/k/Space: スクロール  g/G: 先頭/末尾  p: 番号  u: URL  e: 編集  Enter/h/q: 一覧へ"
	}
	// a は 3 段の巡回なので「次に押すと何が増えるか」を出す (現在どこまで見えているかはタブ行
	// 右端のバッジ ○/○⏸/○⏸✓ が示すので、ここで二重に説明しない)。
	// ⚠️ 語でなくバッジで書くのは幅のため: hint は 1 行で popup 実幅に詰まっており、
	// "a: pending も" (14 桁) では末尾の "q: 閉じる" が黙って切れる (実測)。
	if lo, hi, ok := v.selection(); ok {
		// 選択中は効くキーだけを出す (移動と Enter は選択を畳むので、並べると誤解を招く)
		return strconv.Itoa(hi-lo+1) + " 件選択  J/K・shift+↑↓: 増減  y: パス  p: 番号  Y: 参照  Esc: 解除"
	}
	if v.numFilter.active {
		// Tab と a は絞り込み中の no-op なので案内しない (押しても何も起きないキーを出さない)
		return "j/k: 移動  Enter: 本文  p: 番号  n: next  /: 絞り込み直す  Esc: 解除"
	}
	next := "a: +" + issues.StatusPending.Badge()
	switch v.filter {
	case issues.FilterPending:
		next = "a: +" + issues.StatusDone.Badge()
	case issues.FilterAll:
		next = "a: " + issues.StatusOpen.Badge() + "のみ"
	case issues.FilterOpen:
	}
	// ⚠️ "q: 終了" であって "閉じる" ではない。q/esc は **glogx ごと終了**する
	// (ユーザー要望 2026-08-06)。一覧へ戻るのは i (toggle)。README も 2 語を使い分けており、
	// git log 一覧の hint も同じ動作を "q: 終了" と書いている。issue 121
	//
	// ⚠️ "i: 一覧へ" は入れられない: 足すと最長モード (filter=2) で 85 桁になり
	//   TestIssuesViewHintFitsPopupWidth が落ちる (実測)。戻り方は --help と README が正本。
	return "j/k: 移動  Tab: カテゴリ  /: 検索  Enter: 本文  n: next  " + next + "  q: 終了"
}

// 「次にやる」の目印 (n)。選択中の issue を <issue ディレクトリ>/next/ へ移す。
//
// ⚠️ viewer で唯一、実ファイルを動かす操作なので必ず確認を挟む (glogx の push/pull と同じ作法)。
// 移動そのものは issues.MoveToSubdir で、git index には触れない (理由はそちらの doc)。
//
// 一覧モードだけで受ける: 本文を開いたまま動かすと、開いている Body のパスが実体から外れて
// 「読んでいるファイルがもう無い」状態になる。目印は一覧を見ながら付けるものなので実害もない。
type issuesMarkConfirm struct {
	active  bool
	targets []*issues.Issue
	// unmark は「目印を外す」向きか (next/ から issue ディレクトリ直下へ戻す)。
	unmark bool
}

// askMarkNext は確認を開く (対象が無ければ何もしない)。n は目印の toggle で、既に next の
// issue に対しては「外す」向きになる (ユーザー要望 2026-08-01)。
//
// ⚠️ 向きはカーソル行で決めて選択範囲全体を揃える。1 件ずつ toggle にしない: 目印つきと無しが
// 混ざった選択で「何が起きるか」を確認ダイアログの 1 文で言えなくなる (「3 件のうち 2 件を付けて
// 1 件を外します」は読めない)。
func (v *issuesView) askMarkNext() {
	rows := v.selectedRows()
	if len(rows) == 0 {
		return
	}
	unmark := false
	if cur := v.current(); cur != nil {
		unmark = cur.Status == issues.StatusNext
	}
	v.markNext = issuesMarkConfirm{active: true, targets: rows, unmark: unmark}
}

// markNextKey は確認中のキーを捌く。y/Enter で実行、それ以外は取り消し。
//
// ⚠️ 取り消しを n/Esc に限定しない: 実ファイルを動かす確認で「知らないキーを押したら実行された」
// が起きてはいけないので、明示的な y/Enter 以外はすべて取り消しに倒す。
//
// ⚠️ ここだけ厳密 (大文字 `Y` も取り消し) なのは意図的で、discardKey / actionModal の
//
//	ToLower 判定へ揃えない (理由は status_view.go:discardKey の注記。issue 071 / 123)。
func (v *issuesView) markNextKey(key string) tea.Cmd {
	targets, unmark := v.markNext.targets, v.markNext.unmark
	v.markNext = issuesMarkConfirm{}
	if key != "y" && key != "enter" {
		return nil
	}
	// 外すときは issue ディレクトリ直下へ戻す (= open)。⚠️ 元居た場所 (done/ 等) は覚えていない:
	// 目印は「次にやる」ものに付けるので戻り先は open が自然で、履歴を持つと「どこへ戻るか
	// 分からない」方が困る。
	dest, verb := issues.NextDirName, "next へ移しました"
	if unmark {
		dest, verb = "", "の next を外しました"
	}
	moved, skipped := 0, 0
	for _, iss := range targets {
		if (iss.Status == issues.StatusNext) != unmark {
			skipped++ // 既に目的の向き (同じ場所への移動を「変化」と数えない)
			continue
		}
		if _, err := issues.MoveToSubdir(iss, dest); err != nil {
			v.setNotice("移動できませんでした: "+firstLine(err.Error()), false)
			return v.scanAfterChangeCmd() // 途中まで動いた分を一覧へ反映する
		}
		moved++
	}
	v.clearMark()
	if moved == 0 {
		v.setNotice("対象がありません (すべて既にその状態)", true)
		return nil
	}
	notice := strconv.Itoa(moved) + " 件" + verb
	if skipped > 0 {
		notice += " (" + strconv.Itoa(skipped) + " 件は対象外)"
	}
	v.setNotice(notice, true)
	return v.scanAfterChangeCmd() // 置き場所が変わったので一覧を取り直す
}

// markNextBox は確認モーダルの箱 (glogx の push/pull 確認と同じ見た目)。
func (v *issuesView) markNextBox(width int, colored bool) []string {
	if !v.markNext.active {
		return nil
	}
	what := strconv.Itoa(len(v.markNext.targets)) + " 件"
	if len(v.markNext.targets) == 1 {
		// Rel は同一性のため無害化しない実物 (issues/parse.go newIssue の doc)。画面へ出す
		// ここで sanitize する (制御文字入りのファイル名で確認モーダルを細工させない)
		what = sanitizePlainLine(filepath.Base(v.markNext.targets[0].Rel))
	}
	title, line := " next へ移動 ", what+" を next/ へ移します"
	if v.markNext.unmark {
		title, line = " next を外す ", what+" を next/ から issues 直下へ戻します"
	}
	return centerBox(title, []string{
		line,
		paint("(次にやる目印。ファイルを移動します。commit はしません)", ansiDim, colored),
		"",
		paint("y/Enter: 実行   その他: キャンセル", ansiDim, colored),
	}, width, colored)
}
