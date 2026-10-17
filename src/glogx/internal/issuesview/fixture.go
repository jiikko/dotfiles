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
	return IssuesScanMsg{Root: root, Dirs: dirs, Issues: list, Warnings: warnings}
}

// FixtureScanDir は root を本物の走査にかけた結果の Msg (実ファイルを置くテスト用)。
func FixtureScanDir(root string) IssuesScanMsg { return ScanIssues(root) }

// FixtureScanDirs は走査の結果の Msg が見つけた issues ディレクトリ。
func FixtureScanDirs(msg IssuesScanMsg) []string { return msg.Dirs }

// Load は開いた状態で走査の結果を受け取らせる (閉じる演出は切る)。
func (f Fixture) Load(dirs []string, list []*issues.Issue) {
	f.v.CloseAnimOff = true
	f.v.Shown = true
	f.v.Receive(IssuesScanMsg{Dirs: dirs, Issues: list})
}

// SetShown は開いているかを置く (走査や演出を起こさない)。
func (f Fixture) SetShown(on bool) { f.v.Shown = on }

// SetCloseAnim は閉じる演出の有無を置く (テストの既定は演出なし。演出そのものを見るテストだけが入れる)。
func (f Fixture) SetCloseAnim(on bool) { f.v.CloseAnimOff = !on }

// FinishAnim は開く演出を着地させる。
func (f Fixture) FinishAnim() { f.v.FinishAnim() }

// FinishDrawer は本文の引き出しの演出を着地させる。
func (f Fixture) FinishDrawer() { f.v.Drawer.Finish() }

// DrawerTargetWidth は幅 w のときの本文の引き出しの幅。
func (f Fixture) DrawerTargetWidth(w int) int { return f.v.Drawer.TargetWidth(w) }

// RestartAnim は開く演出の起点を今にする (演出の途中の描画を見るテスト用)。
func (f Fixture) RestartAnim() { f.v.AnimStart = app.TimeNow() }

// Close は閉じる (演出ありなら閉じる演出に入る)。
func (f Fixture) Close() { f.v.Close() }

// Closing は閉じる演出の途中か。
func (f Fixture) Closing() bool { return f.v.Closing }

// SetCwd / SetRoot は走査の起点と repo root を置く。
func (f Fixture) SetCwd(dir string)  { f.v.Cwd = dir }
func (f Fixture) SetRoot(dir string) { f.v.Root = dir }

// StartScan は今の起点で走査を始める Cmd (Cmd を返さなければ nil)。
func (f Fixture) StartScan() tea.Cmd { return f.v.ScanCmd(f.v.Cwd) }

// RescanPending は編集の後の取り直しが予約されているか。
func (f Fixture) RescanPending() bool { return f.v.RescanPending }

// Scanning / SetScanning は走査中の印。
func (f Fixture) Scanning() bool                     { return f.v.Scanning }
func (f Fixture) SetScanning(on bool)                { f.v.Scanning = on }
func (f Fixture) Notice() string                     { return f.v.Notice }
func (f Fixture) BodyOpen() bool                     { return f.v.Open != nil }
func (f Fixture) Cursor() int                        { return f.v.Cursor }
func (f Fixture) SetCursor(i int)                    { f.v.Cursor = i }
func (f Fixture) RowCount() int                      { return len(f.v.Rows) }
func (f Fixture) VisibleRows(o IssuesRenderOpts) int { return f.v.VisibleRows(o.Viewport()) }

// BodyPager は本文のスクロール (glide の途中を作る・読むテスト用)。
func (f Fixture) BodyPager() *listnav.Pager { return &f.v.BodyPager }

// CursorGlide は一覧のカーソルの滑走。
func (f Fixture) CursorGlide() *anim.CursorGlide { return &f.v.CurGlide }

// OpenURLPicker は本文の URL ピッカーを urls で開く (開けたら true)。
func (f Fixture) OpenURLPicker(urls []string) bool { return f.v.UrlPick.Open(urls) }

// URLPickerQuery / URLPickerCursor / URLPickerMatches は URL ピッカーの検索語・選択・絞り込みの件数。
func (f Fixture) URLPickerQuery() string { return f.v.UrlPick.Query() }
func (f Fixture) URLPickerCursor() int   { return f.v.UrlPick.Cursor }
func (f Fixture) URLPickerMatches() int  { return len(f.v.UrlPick.Match) }

// NumberFilterQuery / NumberFilterActive は番号の絞り込みの入力と、確定後に効いているか。
func (f Fixture) NumberFilterQuery() string { return f.v.NumFilter.Query() }
func (f Fixture) NumberFilterActive() bool  { return f.v.NumFilter.Active }

// SetNumberFilterTyping は番号の入力中にする (入力欄が開いている状態)。
func (f Fixture) SetNumberFilterTyping() { f.v.NumFilter.Start() }

// MarkNextActive / SetMarkNextActive は「next の目印を付ける」確認が出ているか。
func (f Fixture) MarkNextActive() bool { return f.v.MarkNext.Active }
func (f Fixture) SetMarkNextActive()   { f.v.MarkNext = IssuesMarkConfirm{Active: true} }

// Watcher は見張りの watcher (nil = 張っていない)。PollArmed は保険のポーリングを張っているか。
func (f Fixture) Watcher() app.DirWatcher { return f.v.Watch.W }
func (f Fixture) PollArmed() bool         { return f.v.Watch.PollArmed }

// Observe は見張りと同じ指紋を同期で計算した Msg (tea.Tick を待たずに見張りの配線を駆動するテスト用)。
func (f Fixture) Observe() IssuesWatchMsg {
	return IssuesWatchMsg{Fp: IssuesFingerprint(f.v.WatchTargets())}
}
