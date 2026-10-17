package issuesview

import (
	tea "charm.land/bubbletea/v2"
	"github.com/jiikko/dotfiles/src/tuikit/anim"
	"github.com/jiikko/dotfiles/src/tuikit/listnav"

	"glogx/internal/app"
	"glogx/issues"
)

// Fixture は main のテスト (browseModel と組み合わせる配線・入力欄の caret・横断の語彙・1 フレームの予算のテスト) が
// issues viewer を特定の状態に置く・読むための口 (doctorview の Fixture と同じ作法)。本番のコードは使わない
// (structure_ratchet_test.go の TestNoFixtureCallsInProduction が止める)。browseModel が要らないテストは
// package issuesview の中に書き、この口を使わない。ここに足すのは「main のテストが本当に要る状態」だけにする (issue 817)。
type Fixture struct{ v *IssuesView }

// Fixture はテスト用の口を返す (上の型の注記)。
func (v *IssuesView) Fixture() Fixture { return Fixture{v} }

// FixtureScanMsg は走査の結果の Msg を組む (browseModel.Update へ流して受け取りの配線を見るテスト用)。
func FixtureScanMsg(root string, dirs []string, list []*issues.Issue, warnings ...string) IssuesScanMsg {
	return IssuesScanMsg{root: root, dirs: dirs, issues: list, warnings: warnings}
}

// FixtureScanDir は root を本物の走査にかけた結果の Msg (実ファイルを置くテスト用)。
func FixtureScanDir(root string) IssuesScanMsg { return scanIssues(root) }

// FixtureScanDirs は走査の結果の Msg が見つけた issues ディレクトリ。
func FixtureScanDirs(msg IssuesScanMsg) []string { return msg.dirs }

// SetShown は開いているかを置く (走査や演出を起こさない)。
func (f Fixture) SetShown(on bool) { f.v.shown = on }

// SetCloseAnim は閉じる演出の有無を置く (テストの既定は演出なし。演出そのものを見るテストだけが入れる)。
func (f Fixture) SetCloseAnim(on bool) { f.v.closeAnimOff = !on }

// FinishAnim は開く演出を着地させる。
func (f Fixture) FinishAnim() { f.v.finishAnim() }

// FinishDrawer は本文の引き出しの演出を着地させる。
func (f Fixture) FinishDrawer() { f.v.drawer.finish() }

// DrawerTargetWidth は幅 w のときの本文の引き出しの幅。
func (f Fixture) DrawerTargetWidth(w int) int { return f.v.drawer.targetWidth(w) }

// RestartAnim は開く演出の起点を今にする (演出の途中の描画を見るテスト用)。
func (f Fixture) RestartAnim() { f.v.animStart = app.TimeNow() }

// Close は閉じる (演出ありなら閉じる演出に入る)。
func (f Fixture) Close() { f.v.close() }

// Closing は閉じる演出の途中か。
func (f Fixture) Closing() bool { return f.v.closing }

// SetCwd / SetRoot は走査の起点と repo root を置く。
func (f Fixture) SetCwd(dir string)  { f.v.cwd = dir }
func (f Fixture) SetRoot(dir string) { f.v.root = dir }

// StartScan は今の起点で走査を始める Cmd (Cmd を返さなければ nil)。
func (f Fixture) StartScan() tea.Cmd { return f.v.scanCmd(f.v.cwd) }

// RescanPending は編集の後の取り直しが予約されているか。
func (f Fixture) RescanPending() bool { return f.v.rescanPending }

// Scanning / SetScanning は走査中の印。
func (f Fixture) Scanning() bool                     { return f.v.scanning }
func (f Fixture) SetScanning(on bool)                { f.v.scanning = on }
func (f Fixture) Notice() string                     { return f.v.notice }
func (f Fixture) BodyOpen() bool                     { return f.v.open != nil }
func (f Fixture) Cursor() int                        { return f.v.cursor }
func (f Fixture) RowCount() int                      { return len(f.v.rows) }
func (f Fixture) VisibleRows(o IssuesRenderOpts) int { return f.v.visibleRows(o.Viewport()) }

// BodyPager は本文のスクロール (glide の途中を作る・読むテスト用)。
func (f Fixture) BodyPager() *listnav.Pager { return &f.v.bodyPager }

// CursorGlide は一覧のカーソルの滑走。
func (f Fixture) CursorGlide() *anim.CursorGlide { return &f.v.curGlide }

// OpenURLPicker は本文の URL ピッカーを urls で開く (開けたら true)。
func (f Fixture) OpenURLPicker(urls []string) bool { return f.v.urlPick.open(urls) }

// URLPickerQuery / URLPickerCursor / URLPickerMatches は URL ピッカーの検索語・選択・絞り込みの件数。
func (f Fixture) URLPickerQuery() string { return f.v.urlPick.query() }
func (f Fixture) URLPickerCursor() int   { return f.v.urlPick.cursor }
func (f Fixture) URLPickerMatches() int  { return len(f.v.urlPick.match) }

// NumberFilterQuery / NumberFilterActive は番号の絞り込みの入力と、確定後に効いているか。
func (f Fixture) NumberFilterQuery() string { return f.v.numFilter.query() }
func (f Fixture) NumberFilterActive() bool  { return f.v.numFilter.active }

// SetNumberFilterTyping は番号の入力中にする (入力欄が開いている状態)。
func (f Fixture) SetNumberFilterTyping() { f.v.numFilter.start() }

// MarkNextActive / SetMarkNextActive は「next の目印を付ける」確認が出ているか。
func (f Fixture) MarkNextActive() bool { return f.v.markNext.active }
func (f Fixture) SetMarkNextActive()   { f.v.markNext = issuesMarkConfirm{active: true} }

// Watcher は見張りの watcher (nil = 張っていない)。PollArmed は保険のポーリングを張っているか。
func (f Fixture) Watcher() app.DirWatcher { return f.v.watch.W }
func (f Fixture) PollArmed() bool         { return f.v.watch.PollArmed }

// Observe は見張りと同じ指紋を同期で計算した Msg (tea.Tick を待たずに見張りの配線を駆動するテスト用)。
func (f Fixture) Observe() IssuesWatchMsg {
	return IssuesWatchMsg{fp: issuesFingerprint(f.v.watchTargets())}
}

// OpenIssue は iss の本文を開く (開けたら true)。
func (f Fixture) OpenIssue(iss *issues.Issue) bool { return f.v.openIssue(iss) }

// Select は一覧の cursor 行から markAt 行までを選んだ状態にする (選択が成り立ったら true)。
func (f Fixture) Select(cursor, markAt int) bool {
	f.v.ensureDisplayRows()
	f.v.cursor, f.v.marked, f.v.markAt = cursor, true, markAt
	_, _, ok := f.v.selection()
	return ok
}

// EditNumberFilter / ConfirmNumberFilter は番号の入力欄へ s を打つ (受け付けたら true) / 確定する。
func (f Fixture) EditNumberFilter(s string) bool { return f.v.numFilter.edit(s) }
func (f Fixture) ConfirmNumberFilter()           { f.v.numFilter.confirm() }
