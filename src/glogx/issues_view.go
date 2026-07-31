package main

import (
	"math"
	"os/exec"
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
}

// issuesView は一覧 (タブ + リスト) と本文 pager の 2 モードを持つ全画面ビュー。
type issuesView struct {
	shown    bool
	loaded   bool // 一度スキャンを完了したか (取り直し中に前回の結果を出してよいかの判定)
	scanning bool // スキャン中 (スピナーを回す。二重発行の防止も兼ねる)

	cwd      string // スキャンの起点 (再読込で使い回す)
	root     string // repo root
	dirs     []string
	all      []*issues.Issue
	warnings []string
	notice   string // 直近の操作結果 (コピー成功・読み込み失敗など)

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
	// チップに出す件数。issues.Tab.Count は done を含む全件なので表示には使わない (理由は
	// refresh)。tabCount は tabs と同じ並び、allCount は All チップ用。
	tabCount []int
	allCount int

	rows   []*issues.Issue // 現在のタブ・フィルタの表示対象
	cursor int
	offset int // 論理 = 着地点 (glide が表示位置を追いかける)
	// listGlide / bodyGlide は半ページ移動のスクロールアニメ (scroll_glide.go の共有型。
	// 一覧・diff pager と同じ手触りにする。ユーザー要望 2026-07-31)。一覧と本文は同時に
	// 動かないが、状態を混ぜると閉じ忘れた glide が他方へ漏れるので別に持つ。
	listGlide scrollGlide

	// 本文 pager (open != nil のとき本文モード)
	open *issues.Issue
	body *issues.Body
	// urlPick は本文中 URL のピッカー (u)。閉じているときは zero value。
	urlPick urlPicker
	// drawer は本文を左から開く引き出しの演出状態 (issues_drawer.go)。閉じる演出のあいだは
	// open/body を生かしたまま逆再生するため、破棄は settleDrawer が担う。
	drawer    issuesDrawer
	bodyOff   int // 論理 = 着地点
	bodyGlide scrollGlide

	// 開くときのスライドイン演出の開始時刻 (ゼロ値 = 演出なし)。フレーム数ではなく壁時計で
	// 進めるのは、tick 周期が変わっても所要時間が変わらないようにするため (push 演出の
	// pushSlides と同じ方式)。演出中は tick を scrollInterval (~30fps) に上げる。
	animStart time.Time
}

const (
	// issuesAnimDuration は開く演出の所要時間 (最後の行が着地するまで)。
	issuesAnimDuration = 700 * time.Millisecond
	// issuesAnimStagger は行ごとに開始をずらす割合。0 なら全行同時に動いて「板が 1 枚
	// 滑り込む」見え方、大きいほど「上から順に流れ込む」見え方になる。
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
	if v.shown {
		v.close()
		return nil
	}
	v.shown = true
	v.animStart = time.Now() // 右から左へ流し込む演出を開始 (lines が窓を変形する)
	return v.scanCmd(cwd)
}

// close は viewer を閉じる (スキャン結果は保持する。再表示は前回の結果を出しながら取り直す)。
func (v *issuesView) close() {
	v.shown = false
	v.animStart = time.Time{}
	v.listGlide.stop() // 閉じた後も glide が残ると、再表示の一瞬だけ古い位置から滑る
	v.discardBody()    // viewer ごと閉じるので引き出しの演出は持ち越さない
}

// finishAnim は開く演出を即座に着地させる。
func (v *issuesView) finishAnim() { v.animStart = time.Time{} }

// animating は開く演出の途中か (tick を高 FPS に上げ、チェーンを回し続ける判定に使う)。
func (v *issuesView) animating() bool {
	if v.listGlide.active || v.bodyGlide.active {
		return true // スクロール glide も tick で進むので「アニメ中」に含める
	}
	if v.drawer.animating(timeNow()) {
		return true // 本文の引き出しの開閉も tick で進む
	}
	return v.shown && !v.animStart.IsZero() && time.Since(v.animStart) < issuesAnimDuration
}

// advanceGlide はスクロール glide を 1 フレーム進める (browseModel の tick から呼ばれる)。
func (v *issuesView) advanceGlide() {
	if v.listGlide.active {
		v.listGlide.advance(v.offset)
	}
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
	return func() tea.Msg {
		root := issues.RepoRoot(cwd)
		dirs := issues.FindDirs(root)
		found, warnings := issues.Scan(dirs)
		for _, iss := range found {
			// メタデータの読み取り失敗は無視する (タイトルがスラッグ表示に落ちるだけ)
			_ = iss.LoadMeta()
		}
		return issuesScanMsg{root: root, dirs: dirs, issues: found, warnings: warnings}
	}
}

// receive はスキャン結果を反映する。
//
// Scan は毎回新しい *Issue を作るので、見ている場所は安定キーで引き直す (仕様が定める同一性キーは
// パス。番号も basename も一意でない)。引き直さないと、再スキャンのたびに (a) カーソルが別の
// issue へ滑り、(b) タブが別カテゴリを指し (tabs は件数降順なので件数が変わると並びが変わる)、
// (c) 本文モードが v.all から外れた古いポインタを掴んで状態・進捗が編集前のまま固まる。
func (v *issuesView) receive(msg issuesScanMsg) {
	v.scanning, v.loaded = false, true
	tab, cursorPath, openPath := v.currentTab(), issuePath(v.current()), issuePath(v.open)
	v.root, v.dirs, v.all, v.warnings = msg.root, msg.dirs, msg.issues, msg.warnings
	v.tabsCanon = issues.Tabs(v.all, issues.TabMinCount)
	v.tabs = v.tabsCanon // refresh が件数を数えて表示順 (0 件を右) へ並べ替える
	v.tabIdx = tabIndexOf(v.tabs, tab)
	v.refresh()
	v.anchorCursor(cursorPath)
	v.rebindOpen(openPath)
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

// rebindOpen は本文モードで開いている issue を新しいスキャン結果へ繋ぎ直す。パスが消えた
// (rename / 状態ディレクトリへ移動) 場合は読み終えている本文をそのまま出し続ける。
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
}

// refresh は現在のタブ・フィルタで表示対象を作り直す。
//
// offset を 0 に戻さないのは、cursor を温存したまま窓だけ先頭へ飛ばすと「カーソル行が
// どの行にも描かれない」状態が残るため (a / Tab / 再スキャンで実際に起きていた)。窓は
// windowOffset が cursor から導出するので、ここは行集合の作り直しだけを担う。
func (v *issuesView) refresh() {
	v.rows = issues.Filter(v.all, v.currentTab(), v.filter)
	v.cursor = clampIdx(v.cursor, len(v.rows))
	v.listGlide.stop() // 行集合が変わったので、旧着地点へ向かう glide は捨てる
	// チップの件数は「そのタブを選んだときに実際に並ぶ行数」と同じ Filter から出す。
	// issues.Tab.Count は done を含む全件なので、そのまま出すと done を伏せた既定表示で
	// 「カテゴリの合計 ≠ All ≠ 一覧の行数」になる。
	//
	// ⚠️ タブ集合そのものは v.all から作る (receive)。Filter 後の集合から作り直すと done だけの
	// カテゴリが消え、位置で持つ tabIdx が別カテゴリを指す。ここで数えるのは件数だけ。
	v.allCount = len(issues.Filter(v.all, "", v.filter))
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

// reorderTabsByCount は正規順序 tabs を「件数 0 を右へ寄せた」並びへ写す純関数 (件数も同じ並びで
// 返す)。件数 > 0 / 0 の 2 群に分け、各群の中は正規順序を保つ。入力は破壊しない。
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
	for _, nonZero := range []bool{true, false} {
		for i, c := range counts {
			if (c > 0) == nonZero {
				outTabs = append(outTabs, tabs[i])
				outCounts = append(outCounts, c)
			}
		}
	}
	return outTabs, outCounts
}

// currentTab は選択中のタブ名 ("" = All)。
func (v *issuesView) currentTab() string {
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
	iss := v.current()
	if iss == nil {
		return
	}
	body, err := iss.ReadBody()
	if err != nil {
		v.notice = "本文を読めませんでした: " + firstLine(err.Error())
		return
	}
	v.open, v.body, v.bodyOff = iss, body, 0
	v.urlPick.close() // 別の issue を開いたら前の URL 一覧を持ち越さない
	v.drawer.open(timeNow())
	v.bodyGlide.stop()
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
	return v.scanCmd(v.cwd)
}

// current はカーソル位置の issue (無ければ nil)。
func (v *issuesView) current() *issues.Issue {
	if v.cursor < 0 || v.cursor >= len(v.rows) {
		return nil
	}
	return v.rows[v.cursor]
}

// handleKey は viewer 表示中のキーを処理する。
//
// 全画面モーダルなのでキーは全部ここで飲む (呼び出し側へ素通りさせない)。素通りさせると
// 一覧を見ている最中の u が git pull --rebase の確認を開く類の誤爆になる: 呼び出し側の
// dispatch は裸の b / u を push / pull に割り当てているため。
//
// page は画面に使える行数 (hint 行を除く)。リスト/本文に実際に使える行数はヘッダーを
// 差し引いた visibleRows で、描画と同じ値を使う (ずれると G が末尾に届かなくなる)。
func (v *issuesView) handleKey(key string, page int) tea.Cmd {
	// このビューは browseModel なしでも駆動できる契約なので、Space の正規化も自分で通す
	// (呼び出し側の normalizeSpaceKey と同じ関数。理由はそちらのコメント)
	key = normalizeSpaceKey(key)
	v.finishAnim() // 演出中のキーは即着地させる (q が効かない時間を作らないため)
	if v.drawer.finish() {
		v.discardBody() // 引き出しの逆再生中にキーが来たら即座に閉じ切る
	}
	v.notice = "" // 通知は直前の操作の結果なので、次のキーで消す (寿命の理由は headLines)
	rows := v.visibleRows(page)
	// ⚠️ URL ピッカーは他のどの割当よりも先に飲む: インクリメンタルサーチでは印字文字がすべて
	// 検索語なので、v (nvim) や y (コピー) を先に処理すると "v" や "y" を含む URL を検索できない。
	if v.urlPick.active {
		return v.urlPickerKey(key)
	}
	// モードに依らないアクションキーは先に飲む (対象は target() が一覧/本文で切り替える)
	if cmd, ok := v.actionKey(key); ok {
		return cmd
	}
	if v.open != nil {
		return v.handleBodyKey(key, rows)
	}
	switch key {
	case "q", "esc", "i":
		v.close()
	case "j", "down", "ctrl+n":
		v.moveCursor(1, rows)
	case "k", "up", "ctrl+p":
		v.moveCursor(-1, rows)
	// 半ページ移動は glide に載せる (Space / ctrl+d。ユーザー要望 2026-07-31)。1 行移動は
	// 距離 1 行で滑らせる意味がなく、端ジャンプ (g/G) は距離が不定なので即時のまま。
	case "ctrl+d", "pgdown", " ", "f":
		prev := v.offset
		v.moveCursor(max(rows/2, 1), rows)
		v.listGlide.start(prev, v.offset)
	case "ctrl+u", "pgup", "b", "shift+space":
		prev := v.offset
		v.moveCursor(-max(rows/2, 1), rows)
		v.listGlide.start(prev, v.offset)
	case "g", "home":
		v.cursor, v.offset = 0, 0
		v.listGlide.stop()
	case "G", "end":
		v.cursor = max(len(v.rows)-1, 0)
		v.scrollToCursor(rows)
	case "tab", "l", "right":
		v.moveTab(1)
	case "shift+tab", "h", "left":
		v.moveTab(-1)
	case "enter", "o":
		v.openBody()
	case "a":
		v.filter = v.filter.Next()
		v.refresh()
	case "r":
		return v.scanCmd(v.cwd) // loaded は落とさない (取り直し中も前回の結果を出したままにする)
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
	case "v":
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

// visibleRows は page 行のうちリスト/本文に使える行数 (ヘッダーを差し引く)。
func (v *issuesView) visibleRows(page int) int { return max(page-len(v.headLines(0, false)), 1) }

// handleBodyKey は本文 pager のキー操作 (diffOverlay と同じ語彙)。
func (v *issuesView) handleBodyKey(key string, rows int) tea.Cmd {
	maxOffset := max(v.body.Len()-rows, 0)
	switch key {
	case "q", "esc", "h", "left":
		v.closeBody()
	case "j", "down", "ctrl+n", "enter":
		v.bodyOff = min(v.bodyOff+1, maxOffset)
	case "k", "up", "ctrl+p":
		v.bodyOff = max(v.bodyOff-1, 0)
	case "ctrl+d", "pgdown", " ", "f":
		prev := v.bodyOff
		v.bodyOff = min(v.bodyOff+rows/2, maxOffset)
		v.bodyGlide.start(prev, v.bodyOff)
	case "ctrl+u", "pgup", "b", "shift+space":
		prev := v.bodyOff
		v.bodyOff = max(v.bodyOff-rows/2, 0)
		v.bodyGlide.start(prev, v.bodyOff)
	case "u":
		v.openURLPicker()
	case "g", "home":
		v.bodyOff = 0
		v.bodyGlide.stop()
	case "G", "end":
		v.bodyOff = maxOffset
		v.bodyGlide.stop()
	}
	return nil
}

// moveCursor はカーソルを動かしてスクロール位置を追従させる。
func (v *issuesView) moveCursor(delta, rows int) {
	v.cursor = clampIdx(v.cursor+delta, len(v.rows))
	v.scrollToCursor(rows)
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
	offset := min(v.offset, v.cursor)     // カーソルが窓より上に出ない
	offset = max(offset, v.cursor-rows+1) // カーソルが窓より下に出ない
	return max(min(offset, max(len(v.rows)-rows, 0)), 0)
}

// moveTab はタブを切り替える (端で止まらず巡回する)。
func (v *issuesView) moveTab(delta int) {
	n := len(v.tabs) + 1 // All を含む
	v.tabIdx = ((v.tabIdx+delta)%n + n) % n
	v.refresh()
}

// target は操作対象の issue (本文モードなら開いているもの、一覧ならカーソル行)。
func (v *issuesView) target() *issues.Issue {
	if v.open != nil {
		return v.open
	}
	return v.current()
}

// editCmd は対象の issue を nvim で開く。job ログ (scratch バッファ) と違い実ファイルなので
// readonly にしない: viewer から直接メモを足したくなるため。
func (v *issuesView) editCmd() tea.Cmd {
	iss := v.target()
	if iss == nil {
		return nil
	}
	return runEditorCmd(exec.Command("nvim", iss.Path))
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
		v.notice = "この issue に URL はありません"
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
	v.notice = "URL を開きます: " + url
	return func() tea.Msg { return openURLMsg{err: openInBrowser(url)} }
}

// copyPath は対象の issue のパスをクリップボードへ入れる。
func (v *issuesView) copyPath() {
	iss := v.target()
	if iss == nil {
		return
	}
	v.copyText(iss.Path, "パスをコピーしました: ")
}

// copyNumber は issue 番号をコピーする (p)。番号は rename も move も生き残る唯一安定した
// 参照形式で、実測でも repo 内 59 箇所・commit message 25 件がこの形。
//
// 番号を持たない issue (素スラッグ。実測で SnapTrim に 4 件) では黙って空をコピーせず、
// ファイル名に落として「番号が無い」ことを通知する。
func (v *issuesView) copyNumber() {
	iss := v.target()
	if iss == nil {
		return
	}
	id := iss.Ident() // CATEGORY-NNN 形式は接頭辞まで含む ("UI-005"。理由は Ident)
	if id == "" {
		v.copyText(filepath.Base(iss.Rel), "番号が無いのでファイル名をコピーしました: ")
		return
	}
	v.copyText(id, "番号をコピーしました: ")
}

// copyReference は貼り付け用の 1 行参照をコピーする (Y)。番号 + タイトル + repo 相対パス。
func (v *issuesView) copyReference() {
	iss := v.target()
	if iss == nil {
		return
	}
	v.copyText(iss.Reference(v.root), "参照をコピーしました: ")
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
		v.notice = "コピーに失敗しました: " + firstLine(err.Error())
		return
	}
	v.notice = okPrefix + text
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
	"research": catPurple, "design": catPurple,
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
type issuesRenderOpts struct {
	width   int
	page    int
	colored bool
	spinner string
	// cursorPaint はカーソル行の強調 (browseModel の bgLine を渡す)。nil なら太字だけで示す
	// = テストと NO_COLOR 用。
	cursorPaint func(string) string
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
		inner.width = max(v.drawer.targetWidth(o.width)-1, 1)
		body = composeDrawer(v.padTo(v.listLines(o), o.page), v.padTo(v.bodyLines(inner), o.page),
			w, o.width, o.colored)
	default:
		body = v.listLines(o)
	}
	for len(body) < o.page {
		body = append(body, "")
	}
	body = body[:o.page]
	if p := v.animProgress(); p < 1 {
		body = slideInWindow(body, p, o.width)
	}
	return body
}

// animProgress は開く演出の進み (0..1)。演出していないときは 1 (= 変形しない)。
func (v *issuesView) animProgress() float64 {
	if !v.animating() {
		return 1
	}
	return float64(time.Since(v.animStart)) / float64(issuesAnimDuration)
}

// padTo は行数を n へ揃える (足りなければ空行、多ければ切る)。引き出しの合成では一覧と本文の
// 行数が違う (ヘッダーの行数が異なる) ため、重ねる前に必ず揃える。
func (v *issuesView) padTo(lines []string, n int) []string {
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines[:n]
}

// slideInWindow は窓の各行を「右から左へ流し込む」途中の姿にする。
//
// 行ごとに開始をずらす (stagger) ので、板が 1 枚滑るのではなく上から順に流れ込んで見える。
// 進みは easeOutCubic で終点付近で減速させる (線形だと着地が「カクッ」と止まる。toast の
// easedShown と同じ理由)。まだ入ってきていない行は空にする = 右端から現れる。
func slideInWindow(window []string, progress float64, width int) []string {
	out := make([]string, 0, len(window))
	last := max(len(window)-1, 1)
	for i, ln := range window {
		delay := issuesAnimStagger * float64(i) / float64(last)
		local := (progress - delay) / (1 - issuesAnimStagger)
		switch {
		case local >= 1:
			out = append(out, ln) // 着地済み
			continue
		case local <= 0 || ln == "":
			out = append(out, "") // まだ画面外 (または元から空行)
			continue
		}
		off := int(math.Round((1 - easeOutCubicFloat(local)) * float64(width)))
		if off >= width {
			out = append(out, "")
			continue
		}
		out = append(out, padSpaces(off)+clipToWidth(ln, width-off))
	}
	return out
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
func (v *issuesView) headLines(width int, colored bool) []string {
	if v.open != nil {
		return v.bodyHeadLines(width, colored)
	}
	return v.listHeadLines(width, colored)
}

// bodyHeadLines は本文モードのヘッダー (ファイル名 + 状態 + 通知)。
func (v *issuesView) bodyHeadLines(width int, colored bool) []string {
	{
		status := v.open.StatusLabel()
		if p := v.open.Progress(); p != "" {
			status += "  " + p
		}
		head := []string{
			paint(clipToWidth(v.open.Rel, width), ansiBold, colored),
			paint(clipToWidth(status, width), ansiDim, colored),
		}
		// 本文モードでも y / p / Y / N は効く。ここに通知の行が無いと、コピーの成功も失敗も
		// 画面に一切出ない (トーストは全画面差し替えの下に隠れるので受け皿がここしかない)
		if v.notice != "" {
			head = append(head, paint(clipToWidth(v.notice, width), ansiDim, colored))
		}
		return append(head, "")
	}
}

// listHeadLines は一覧のヘッダー (タブ + 通知/警告)。
//
// ⚠️ headLines と分けているのは引き出しのため: 本文を開いている間も下地の一覧はタブを出す
// 必要があり、headLines をそのまま使うと下地にまで本文のヘッダーが出る (実測で「一覧の上に
// 本文のファイル名が乗る」表示になった)。
func (v *issuesView) listHeadLines(width int, colored bool) []string {
	head := []string{v.tabLine(issuesRenderOpts{width: width, colored: colored})}
	// notice はキー 1 打分の寿命 (handleKey が入口で消す) なので、警告より優先しても恒久的に
	// 隠すことはない。⚠️ 寿命を外すとスキャン警告 (同名ファイルの二重化 = 静かな内容喪失) が
	// 二度と出なくなる。
	switch {
	case v.notice != "":
		head = append(head, paint(clipToWidth(v.notice, width), ansiDim, colored))
	case len(v.warnings) > 0:
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
	// 論理 offset を「描画時の行数でカーソルを含む窓」へ収束させてから、その着地点に対して
	// glide の途中位置を出す (キー処理時の行数とずれていても窓とカーソルが食い違わない)
	v.offset = v.windowOffset(rows)
	// ⚠️ glide の途中位置もカーソルを含む範囲へ寄せる。半ページ移動は cursor と offset を同時に
	// 動かすので、素の途中位置 (旧窓) で切るとアニメ中だけカーソル行が 1 本も描かれず、
	// 「見えない行が Enter・v・y の対象になる」窓が復活する (windowOffset を導出値にした狙いを
	// glide が一時的に壊す。敵対的レビュー P2 2026-07-31)。滑らかさは残したまま、窓の端が
	// カーソルに追いつくところで止める。
	shown := v.listGlide.offset(v.offset)
	shown = max(min(shown, v.cursor), v.cursor-rows+1)
	offset := max(min(shown, max(len(v.rows)-rows, 0)), 0)
	end := min(offset+rows, len(v.rows))
	out := make([]string, 0, rows)
	for i := offset; i < end; i++ {
		out = append(out, v.rowLine(i, o, o.width-scrollbarReserve))
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

// tabLine はタブ行 (件数つき) と、右端に有効な状態フィルタを描く。
func (v *issuesView) tabLine(o issuesRenderOpts) string {
	var b strings.Builder
	// 件数は refresh が数えた値を読む (毎フレーム・毎打鍵の Filter は全件分の slice を捨てる。
	// visibleRows も行数を得るためにこの関数を通る)
	b.WriteString(v.tabChip("All", v.allCount, v.tabIdx == 0, o.colored))
	for i, t := range v.tabs {
		b.WriteString(" ")
		count := 0
		if i < len(v.tabCount) {
			count = v.tabCount[i]
		}
		b.WriteString(v.tabChip(t.Name, count, v.tabIdx == i+1, o.colored))
	}
	filter := v.filter.Badges()
	left := clipToWidth(b.String(), max(o.width-dispWidth(filter)-1, 1))
	pad := max(o.width-dispWidth(left)-dispWidth(filter), 0)
	return left + padSpaces(pad) + paint(filter, ansiDim, o.colored)
}

// tabChip は 1 個のタブ表示。カテゴリ色を使い、選択中は太字・非選択は dim で落とす
// (一覧のカテゴリ列と同じ色にすることで「タブの色 = その行の色」が対応する)。
// All は特定のカテゴリではないのでシアン (本体の見出し色) を使う。
func (v *issuesView) tabChip(name string, count int, active bool, colored bool) string {
	text := "[" + name + " " + strconv.Itoa(count) + "]"
	color := ansiCyan
	if name != "All" {
		color = categoryColor(name)
	}
	if active {
		return paint(text, ansiBold+color, colored)
	}
	return paint(text, ansiDim+color, colored)
}

// scrollbarReserve は scrollbarColumn がバー列 + 手前の空きに使う桁数。行の組み立てで
// 先に差し引いておく: 幅ぴったりに組んでから scrollbarColumn に渡すと、あちらのクリップで
// 末尾 1 文字が省略記号に化ける (本文の 1 文字が消える)。
const scrollbarReserve = 2

// rowLine は一覧の 1 行 (番号・状態バッジ・カテゴリ・タイトル・進捗)。width は行が使える
// 表示幅 (スクロールバー列を差し引いた後)。
func (v *issuesView) rowLine(i int, o issuesRenderOpts, width int) string {
	iss := v.rows[i]
	num := fillRight(iss.Number, 3)
	badge := iss.Status.Badge()
	cat := fillRight(clipToWidth(iss.Category, 9), 9)
	catPainted := paint(cat, categoryColor(iss.Category), o.colored)
	progress := iss.Progress()
	// 溝 + "NNN " + バッジ + " " + カテゴリ + " " + タイトル (+ 右端に進捗)
	fixed := cursorGutterWidth + dispWidth(num) + 1 + dispWidth(badge) + 1 + dispWidth(cat) + 1
	titleW := max(width-fixed-dispWidth(progress)-1, 4)
	title := clipToWidth(iss.Display(), titleW)
	text := num + " " + badge + " " + catPainted + " " + title
	if progress != "" {
		pad := max(width-cursorGutterWidth-dispWidth(text)-dispWidth(progress), 1)
		text += padSpaces(pad) + paint(progress, ansiDim, o.colored)
	}
	// ⚠️ どの経路も同じ幅に切る。titleW には下限 (4) があるので、極端に狭い幅では固定部分だけで
	// width を超える。カーソル行だけ切っていたため、そこ以外の行が枠を突き破っていた。
	if i != v.cursor {
		return clipToWidth(cursorGutterBlank+text, width)
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
	lines := v.body.Lines(o.width-scrollbarReserve, o.colored)
	// 行数は幅で変わる (Body は幅ごとに整形し直す)。幅が広がって行数が減ると論理 bodyOff が
	// 上限を超えたまま残り、k / ctrl+u は max(bodyOff-n, 0) しか見ないので「何度押しても
	// 画面が動かない」打鍵数が生まれる。描画で確定した行数で論理 offset を収束させて防ぐ。
	v.bodyOff = max(min(v.bodyOff, max(len(lines)-rows, 0)), 0)
	offset := max(min(v.bodyGlide.offset(v.bodyOff), max(len(lines)-rows, 0)), 0)
	end := min(offset+rows, len(lines))
	out := make([]string, 0, rows)
	for i := offset; i < end; i++ {
		out = append(out, lines[i])
	}
	out = scrollbarColumn(out, o.width, len(lines), offset, o.colored)
	return append(header, out...)
}

// hint は viewer 表示中の操作案内 (最下行)。
func (v *issuesView) hint() string {
	// ⚠️ hint は 1 行で、幅を超えた分は末尾から黙って切られる。popup の実幅 (84 桁) に
	// 収まる範囲へ絞り、絞られたキー (y / Y / v / r) は --help と README を正本にする。
	if v.open != nil {
		return "j/k/Space: スクロール  g/G: 先頭/末尾  p: 番号  u: URL  v: nvim  h/q: 一覧へ"
	}
	// a は 3 段の巡回なので「次に押すと何が増えるか」を出す (現在どこまで見えているかはタブ行
	// 右端のバッジ ○/○⏸/○⏸✓ が示すので、ここで二重に説明しない)。
	// ⚠️ 語でなくバッジで書くのは幅のため: hint は 1 行で popup 実幅に詰まっており、
	// "a: pending も" (14 桁) では末尾の "q: 閉じる" が黙って切れる (実測)。
	next := "a: +" + issues.StatusPending.Badge()
	switch v.filter {
	case issues.FilterPending:
		next = "a: +" + issues.StatusDone.Badge()
	case issues.FilterAll:
		next = "a: " + issues.StatusOpen.Badge() + "のみ"
	case issues.FilterOpen:
	}
	return "j/k: 移動  Tab: カテゴリ  Enter: 本文  p: 番号  N: 次番号  " + next + "  q: 閉じる"
}
