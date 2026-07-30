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
type issuesScanMsg struct {
	root     string // repo root (貼り付け用の repo 相対パスに使う)
	dirs     []string
	issues   []*issues.Issue
	warnings []string
	err      error
}

// issuesView は一覧 (タブ + リスト) と本文 pager の 2 モードを持つ全画面ビュー。
type issuesView struct {
	shown    bool
	loaded   bool // 一度スキャン済みか (再表示で再スキャンしない)
	scanning bool // スキャン中 (スピナーを回す)
	err      error

	cwd      string // スキャンの起点 (再読込で使い回す)
	root     string // repo root
	dirs     []string
	all      []*issues.Issue
	warnings []string
	notice   string // 直近の操作結果 (コピー成功・読み込み失敗など)

	tabs     []issues.Tab // All は含まない (表示時に先頭へ足す)
	tabIdx   int          // 0 = All、1.. = tabs[tabIdx-1]
	showDone bool

	rows   []*issues.Issue // 現在のタブ・フィルタの表示対象
	cursor int
	offset int // 論理 = 着地点 (glide が表示位置を追いかける)
	// listGlide / bodyGlide は半ページ移動のスクロールアニメ (scroll_glide.go の共有型。
	// 一覧・diff pager と同じ手触りにする。ユーザー要望 2026-07-31)。一覧と本文は同時に
	// 動かないが、状態を混ぜると閉じ忘れた glide が他方へ漏れるので別に持つ。
	listGlide scrollGlide

	// 本文 pager (open != nil のとき本文モード)
	open      *issues.Issue
	body      *issues.Body
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

// toggle は viewer の開閉。初回だけスキャンの tea.Cmd を返す。
func (v *issuesView) toggle(cwd string) tea.Cmd {
	if v.shown {
		v.close()
		return nil
	}
	v.shown = true
	v.animStart = time.Now() // 右から左へ流し込む演出を開始 (lines が窓を変形する)
	if v.loaded || v.scanning {
		return nil
	}
	return v.scanCmd(cwd)
}

// close は viewer を閉じる (スキャン結果は保持したまま。再表示は即座)。
func (v *issuesView) close() {
	v.shown = false
	v.animStart = time.Time{}
	v.closeBody()
}

// finishAnim は開く演出を即座に着地させる。
func (v *issuesView) finishAnim() { v.animStart = time.Time{} }

// animating は開く演出の途中か (tick を高 FPS に上げ、チェーンを回し続ける判定に使う)。
func (v *issuesView) animating() bool {
	if v.listGlide.active || v.bodyGlide.active {
		return true // スクロール glide も tick で進むので「アニメ中」に含める
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
	v.cwd = cwd
	v.scanning = true
	v.err = nil
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
func (v *issuesView) receive(msg issuesScanMsg) {
	v.scanning = false
	v.loaded = true
	if msg.err != nil {
		v.err = msg.err
		return
	}
	v.root, v.dirs, v.all, v.warnings = msg.root, msg.dirs, msg.issues, msg.warnings
	v.tabs = issues.Tabs(v.all, issues.TabMinCount)
	v.tabIdx = min(v.tabIdx, len(v.tabs))
	v.refresh()
}

// refresh は現在のタブ・フィルタで表示対象を作り直す。
func (v *issuesView) refresh() {
	v.rows = issues.Filter(v.all, v.currentTab(), v.showDone)
	v.cursor = clampIdx(v.cursor, len(v.rows))
	v.offset = 0
	v.listGlide.stop()
}

// currentTab は選択中のタブ名 ("" = All)。
func (v *issuesView) currentTab() string {
	if v.tabIdx <= 0 || v.tabIdx > len(v.tabs) {
		return ""
	}
	return v.tabs[v.tabIdx-1].Name
}

// closeBody は本文モードを抜ける。
func (v *issuesView) closeBody() {
	v.open, v.body, v.bodyOff = nil, nil, 0
	v.bodyGlide.stop()
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
	v.bodyGlide.stop()
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
	rows := v.visibleRows(page)
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
	case "ctrl+u", "pgup", "b":
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
		v.showDone = !v.showDone
		v.refresh()
	case "r":
		v.loaded = false
		return v.scanCmd(v.cwd)
	case "v":
		return v.editCmd()
	case "y":
		v.copyPath()
	case "p":
		v.copyNumber()
	case "Y":
		v.copyReference()
	case "N":
		v.copyNextNumber()
	}
	return nil
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
	case "ctrl+u", "pgup", "b":
		prev := v.bodyOff
		v.bodyOff = max(v.bodyOff-rows/2, 0)
		v.bodyGlide.start(prev, v.bodyOff)
	case "g", "home":
		v.bodyOff = 0
		v.bodyGlide.stop()
	case "G", "end":
		v.bodyOff = maxOffset
		v.bodyGlide.stop()
	case "v":
		return v.editCmd()
	case "y":
		v.copyPath()
	case "p":
		v.copyNumber()
	case "Y":
		v.copyReference()
	case "N":
		v.copyNextNumber()
	}
	return nil
}

// moveCursor はカーソルを動かしてスクロール位置を追従させる。
func (v *issuesView) moveCursor(delta, rows int) {
	v.cursor = clampIdx(v.cursor+delta, len(v.rows))
	v.scrollToCursor(rows)
}

// scrollToCursor はカーソルが画面内に入るまで offset を寄せる。
func (v *issuesView) scrollToCursor(rows int) {
	if rows <= 0 {
		return
	}
	if v.cursor < v.offset {
		v.offset = v.cursor
	}
	if v.cursor >= v.offset+rows {
		v.offset = v.cursor - rows + 1
	}
	v.offset = max(min(v.offset, max(len(v.rows)-rows, 0)), 0)
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
	if iss.Number == "" {
		v.copyText(filepath.Base(iss.Rel), "番号が無いのでファイル名をコピーしました: ")
		return
	}
	v.copyText(iss.Number, "番号をコピーしました: ")
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
	var body []string
	if v.open != nil {
		body = v.bodyLines(o)
	} else {
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
		status := v.open.StatusLabel()
		if p := v.open.Progress(); p != "" {
			status += "  " + p
		}
		return []string{
			paint(clipToWidth(v.open.Rel, width), ansiBold, colored),
			paint(clipToWidth(status, width), ansiDim, colored),
			"",
		}
	}
	head := []string{v.tabLine(issuesRenderOpts{width: width, colored: colored})}
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
	head := v.headLines(o.width, o.colored)
	if msg := v.emptyMessage(o); msg != "" {
		return append(head, paint(clipToWidth(msg, o.width), ansiDim, o.colored))
	}
	rows := max(o.page-len(head), 1)
	offset := max(min(v.listGlide.offset(v.offset), max(len(v.rows)-rows, 0)), 0)
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
	case v.scanning:
		return o.spinner + " issues を探しています..."
	case v.err != nil:
		return "エラー: " + firstLine(v.err.Error())
	case len(v.dirs) == 0:
		return "issues ディレクトリが見つかりません (repo root と root/*/issues を探しました)"
	case len(v.rows) == 0 && !v.showDone:
		return "このタブに未完了の issue はありません (a: done も表示)"
	case len(v.rows) == 0:
		return "このタブに issue はありません"
	default:
		return ""
	}
}

// tabLine はタブ行 (件数つき) と、右端に有効な状態フィルタを描く。
func (v *issuesView) tabLine(o issuesRenderOpts) string {
	var b strings.Builder
	b.WriteString(v.tabChip("All", len(issues.Filter(v.all, "", v.showDone)), v.tabIdx == 0, o.colored))
	for i, t := range v.tabs {
		b.WriteString(" ")
		b.WriteString(v.tabChip(t.Name, t.Count, v.tabIdx == i+1, o.colored))
	}
	filter := issues.StatusOpen.Badge() + issues.StatusPending.Badge()
	if v.showDone {
		filter += issues.StatusDone.Badge()
	}
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
	if i != v.cursor {
		return cursorGutterBlank + text
	}
	if o.cursorPaint != nil {
		return o.cursorPaint(cursorGutterMark + text)
	}
	return clipToWidth(cursorGutterMark+paint(text, ansiBold, o.colored), width)
}

// bodyLines は本文 pager (ヘッダー + 本文) を描く。
func (v *issuesView) bodyLines(o issuesRenderOpts) []string {
	header := v.headLines(o.width, o.colored)
	rows := max(o.page-len(header), 1)
	lines := v.body.Lines(o.width-scrollbarReserve, o.colored)
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
		return "j/k/Space: スクロール  g/G: 先頭/末尾  p: 番号  v: nvim  h/q: 一覧へ"
	}
	done := "a: done"
	if v.showDone {
		done = "a: done 除外"
	}
	return "j/k: 移動  Tab: カテゴリ  Enter: 本文  p: 番号  N: 次番号  " + done + "  q: 閉じる"
}
