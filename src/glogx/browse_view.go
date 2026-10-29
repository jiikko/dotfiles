package main

import (
	"strings"

	"github.com/jiikko/dotfiles/src/tuikit/caret"
	"github.com/jiikko/dotfiles/src/tuikit/layout"
	"github.com/jiikko/dotfiles/src/tuikit/listnav"

	"glogx/internal/gitdata"
	"glogx/internal/term"
	"glogx/internal/usageview"

	"glogx/internal/app"

	tea "charm.land/bubbletea/v2"
)

// unpushedCount は未 push コミット数 (push 確認モーダルと confirmPush が共用)。
// centerModalLines は中央モーダルの描画行 (action モーダル: push/pull/update)。非表示なら nil。
// tmux prefix 誤爆のフィードバックは中央ダイアログをやめて右下 toast へ移した (2026-07-24)。
func (m *browseModel) centerModalLines() []string {
	return m.actModal.boxLines(m.contentWidth(), m.colored, m.spinner())
}

// fillUnknown は結果が得られなかった SHA を「取得中」のまま残さず unknown へ落とす。
func (m *browseModel) fillUnknown() {
	for _, sha := range m.toFetch {
		if _, ok := m.statuses[sha]; !ok {
			m.statuses[sha] = gitdata.StateUnknown
		}
	}
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
		Width:    max(m.contentWidth()-term.CursorGutterWidth, 0),
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
// フレーム有効時の寸法オーバーヘッド。
//
//	横 frameHOverhead = 左余白1 + "│ "2 + " │"2 + 影1 + 右余白1 = 7
//	縦 frameVOverhead = 上余白1 + 上辺1 + 下辺1 + 下影1 + hint1 = 5
//
// frameMinWidth/Height 未満の端末ではフレームを自動 OFF し従来描画へフォールバックする
// (tmux の小ペイン/popup でも安全。極小端末で影の見切れを避ける)。
const (
	frameHOverhead = 7
	frameVOverhead = 5
	// frameContentLeft / frameContentTop はフレーム有効時に中身の左上が来る画面の桁と行
	// (左余白1 + "│ "2 / 上余白1 + 上辺1)。端末のカーソルを中身の座標から画面の座標へ直すのに使う (caret)
	frameContentLeft = 3
	frameContentTop  = 2
	frameMinWidth    = 60
	frameMinHeight   = 15
)

// contentWidth はコンテンツ (リスト行・overlay・モーダル) が使える横幅。フレーム有効時は枠 + 余白 +
// 影のぶんを引く。m.width/m.height を直読みしてよいのは frameActive/contentWidth/pageSize と
// View の term.WrapWindowFrame 呼び出しだけ、という規約で幅の単一ファネルにする。
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
	m.offset = listnav.WindowOffset(m.offset, headerLineIndex(lines, m.cursor), len(lines), m.pageSize())
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
	v.Cursor = m.caret()
	return v
}

// caret は入力欄 (issues の番号の絞り込み・URL ピッカー) のキャレットに置く端末のカーソル。入力欄が無ければ nil (隠す)。
//
// 🚨 IME は変換中の文字を端末のカーソルの位置に出す (tuikit/caret・docs/glogx-ui-guide.md §7)。位置は IssuesView.CaretCol
// (窓の 0 行目の桁) にフレームの原点を足したもの。窓を変形する演出 (開閉の zoom) の最中と、窓の上に板が重なっている間
// (action モーダル・再起動・usage は左上の入力欄に重なりうる) は置かない。
func (m *browseModel) caret() *tea.Cursor {
	active := m.activeFullScreen()
	if m.done || (active != FullScreenIssues && active != FullScreenFiler && active != FullScreenLan) || m.zoom.scale(app.TimeNow()) < appZoomSnap {
		return nil
	}
	// toast は見ない: 右下に積む板で、入力欄のある 0 行目に届くのは窓が 4 行以下 (端末の高さ 5 以下) のときだけ
	// (toastDrawBudget の下限)。その高さでは入力欄そのものがほぼ使えないので、判定を足して毎フレーム箱を組み直すより安い
	if len(m.centerModalLines()) > 0 || len(m.restartPromptLines()) > 0 || len(m.usageOv.BoxLines(m.contentWidth(), m.colored, m.spinner())) > 0 {
		return nil
	}
	var x, y int
	var ok bool
	switch active {
	case FullScreenFiler:
		x, y, ok = m.filerV.CaretPos() // treefiler の検索欄 (最下行)
	case FullScreenLan:
		x, y, ok = m.lanV.CaretPos() // mac-lan-manager の絞り込み・別名・帯の入力欄
	case FullScreenIssues:
		x, ok = m.issuesOv.CaretCol(m.issuesOpts())
	case FullScreenNone, FullScreenRatelimit, FullScreenDoctor, FullScreenStatus, FullScreenCount:
		return nil // 入力欄が無い (上の判定で弾いている)
	}
	if !ok {
		return nil
	}
	if m.frameActive() {
		x, y = x+frameContentLeft, y+frameContentTop
	}
	return caret.At(x, y, m.width, m.height)
}

// finishWithGlobalChrome はどの画面 (コミット一覧 / issues / status viewer) でも出るべき
// オーバーレイを重ねて窓を仕上げる。全ビューの唯一の出口。
//
// 🚨 ここに書く 4 つ (action モーダル → 再起動 → usage → トースト) はビューごとに書かないこと。
// この合成を一覧側にしか書かないと「issues を開いている間は
// 通知が画面に一切出ない」状態になる。前面順も含めてこの 1 箇所が契約の出典
// (viewer 用と一覧用に逐語 2 コピーを置かない)。
//
// 各オーバーレイの理由:
//   - action モーダル (pull 確認・実行中): キーは viewer より先に actModal が捌く (handleKey の
//     判定順) ので、描かないと「見えないモーダルがキーを持つ」= y/N の行き先が画面から分からない
//     状態になる。status viewer の p (pull) で実際にこの経路へ来る。term.OverlayCenteredBox は行を
//     塗り潰さず左右の背景を残して合成する (モーダルの左側テキストが消えるのを解消)
//   - 再起動ダイアログ: 中央。答えるまで残る (次の 1 キーで必ず閉じる)
//   - usage: 上部右端の複数行モーダル。U で再表示、任意キーで消える
//   - トースト: 右下 (hint 行の直上) に数秒。push/pull 完了や viewer の操作結果 (コピー等) を
//     glogx 共通の語彙で出す
//
// 🚨 窓の上に板を足すときは、caret() の「板が出ている間は端末のカーソルを置かない」判定にも足す
// (入力欄の上に板が重なると、IME の変換中の文字が板の上に出る)。
func (m *browseModel) finishWithGlobalChrome(window []string, page int) string {
	if box := m.centerModalLines(); len(box) > 0 {
		window = term.OverlayCenteredBox(window, box, m.contentWidth(), page, m.colored)
	}
	if box := m.restartPromptLines(); len(box) > 0 {
		window = term.OverlayCenteredBox(window, box, m.contentWidth(), page, m.colored)
	}
	if box := m.usageOv.BoxLines(m.contentWidth(), m.colored, m.spinner()); len(box) > 0 {
		window = usageview.OverlayBoxTopRight(window, box, m.contentWidth(), m.colored)
	}
	if box := m.toast.BoxLines(m.colored, toastDrawBudget(page, m.toast.ReservedHeight(2, m.contentWidth())), m.contentWidth()); len(box) > 0 {
		window = usageview.OverlayBoxBottomRight(window, box, m.contentWidth(), m.colored)
	}
	return m.finishWindow(window, page)
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
	case FullScreenRatelimit:
		return m.finishWithGlobalChrome(m.rlDash.Lines(m.ratelimitOpts()), page)
	case FullScreenDoctor:
		return m.finishWithGlobalChrome(m.doctorOv.Lines(m.doctorOpts()), page)
	case FullScreenStatus:
		return m.finishWithGlobalChrome(m.statusOv.Lines(m.statusOpts()), page)
	case FullScreenIssues:
		return m.finishWithGlobalChrome(m.issuesOv.Lines(m.issuesOpts()), page)
	case FullScreenFiler:
		return m.finishWithGlobalChrome(m.filerV.Lines(m.contentWidth(), page), page)
	case FullScreenLan:
		return m.finishWithGlobalChrome(m.lanV.Lines(m.contentWidth(), page), page)
	case FullScreenNone, FullScreenCount:
		// 全画面ビューアが出ていない = 下でコミット一覧を組む
	}
	lines := m.lines()
	// glide 中は表示 offset (途中位置) で窓を切る。それ以外は論理 offset。
	renderOffset := m.glide.Offset(m.offset)
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
			text = term.PadSpaces(off) + term.Paint(term.StripANSI(text), term.AnsiDim, m.colored)
			window = append(window, term.CursorGutterBlank+term.ClipToWidth(text, max(m.contentWidth()-term.CursorGutterWidth, 0)))
			continue
		}
		// カーソルは全行に確保した 2 桁の溝の「→ 」+ ヘッダー行全体の bg 塗りで示す。
		// 溝は一度「git log と左マージンがずれる」で廃止したが、bg 塗りだけでは
		// 視認しにくいため全行マージン込みで復活 (ユーザー要望 2026-07-21)
		if lines[i].Header && lines[i].CommitIdx == m.cursor {
			window = append(window, m.cursorLine(text))
			continue
		}
		window = append(window, term.CursorGutterBlank+term.ClipToWidth(text, max(m.contentWidth()-term.CursorGutterWidth, 0)))
	}
	// リストが 1 画面に収まらないときは右端にスクロールバー列を出す (diff/job overlay と同じ
	// 見た目)。overlay 群の合成より先に足す = ポップアップ類はバーの上に浮く。offset は
	// glide 中の表示 offset を使っているので thumb もグライドに追従する。
	window = layout.Scrollbar(window, m.contentWidth(), len(lines), offset, m.colored)
	// job パネルは対象コミットのヘッダー行直下へ「重ねる」(リスト行を置き換える)。
	// リストの行構成自体は変えないので、開閉で後続行がずれない。
	// 下に収まらない場合はビューポート内へ収まる位置まで引き上げる
	if panel := m.panelLines(); len(panel) > 0 {
		window = term.OverlayBox(window, panel, m.boxAnchor(lines, offset, m.panelSHA)+1, page)
	}
	// diff ポップアップは job パネルよりさらに前面 (openDiff がパネルを閉じるため
	// 実際に同時表示になることはないが、重ね順の契約としてパネルの後に描く)
	if diffBox := m.diffBoxLines(); len(diffBox) > 0 {
		window = term.OverlayBox(window, diffBox, m.boxAnchor(lines, offset, m.diffOv.SHA())+1, page)
	}
	// PR 状態ポップアップも対象コミット直下へ重ねる (job パネルとは同時表示にならない:
	// P は一覧のみで受け、表示中は handlePRStatusKey がモーダルに捌く)
	if prBox := m.prStatusOv.BoxLines(m.contentWidth(), m.colored, m.spinner(), m.prStatusCILine()); len(prBox) > 0 {
		window = term.OverlayBox(window, prBox, m.boxAnchor(lines, offset, m.prStatusOv.SHA())+1, page)
	}
	// ここから先 (action モーダル → 再起動 → usage → トースト) は全ビュー共通なので
	// finishWithGlobalChrome が持つ。一覧固有のオーバーレイ (スクロールバー / job パネル /
	// diff / PR 状態) より後面に来ない = 共通 chrome が常に最前面。
	return m.finishWithGlobalChrome(window, page)
}

// finishWindow は組み終わった窓をフレームで包み hint 行を足して 1 フレームの文字列にする。
// コミット一覧と issues viewer (全画面) の共通の出口 (枠・hint の扱いを 1 箇所に保つ)。
func (m *browseModel) finishWindow(window []string, page int) string {
	// フレーム有効時は最外周を余白 + 枠 + 右下ドロップシャドウで包む。板の高さを
	// 安定させるため、コンテンツが少なくても pageSize 行まで空行でパディングしてから包む
	// (板が常にビューポート一杯 = リサイズや行数変動で枠が踊らない)。hint は板の外・最下行。
	// 地色 (全画面 ratelimit ダッシュボードだけ) は枠の内側だけに敷く。枠の外 (余白・影・hint) まで塗ると、
	// 近黒の影が黒地に溶けて見えない。枠を描かないときは窓の行だけを塗る
	bg := m.screenBg()
	if m.frameActive() {
		for len(window) < page {
			window = append(window, "")
		}
		window = term.WrapWindowFrame(window, m.width, m.colored, bg)
		bg = ""
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
	if scale := m.zoom.scale(app.TimeNow()); scale < appZoomSnap {
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
	if bg != "" {
		size += len(window) * (len(bg) + len(term.AnsiReset) + m.width)
	}
	var b strings.Builder
	b.Grow(size)
	for _, w := range window {
		b.WriteString(paintScreenBg(w, bg, m.width))
		b.WriteString("\n")
	}
	b.WriteString(hint)
	return b.String()
}

// screenBg は板の内側に敷く地の色 ("" = 端末の地色のまま)。
//
// 🚨 面塗りは既定では**しない**。bgLine の doc にあるとおり、push 済みエリアの面塗りは
// 「環境の配色次第で視認性を落とす」としてユーザー判断で撤去した経緯があり、その判断は
// 生きている (面塗りを自発的に増やさない)。ここが例外なのは、全画面の残量表示だけは地色を
// 固定したいという明示要望があるため (2026-09-01。理由は term.AnsiScreenBg の doc)。
func (m *browseModel) screenBg() string {
	if !m.colored || !m.rlDash.Visible() {
		return ""
	}
	return term.AnsiScreenBg
}

// paintScreenBg は 1 行を bg で端末幅まで塗る。行内の SGR リセットで地色が切れるので、
// リセット直後に張り直す (bgLine と同じ手口だが、あちらは板の内側 contentWidth 幅、
// こちらは枠を描かないときの窓の行の端末幅ぶん)。
func paintScreenBg(line, bg string, width int) string {
	if bg == "" || width <= 0 {
		return line
	}
	line = term.ClipToWidth(line, width)
	pad := max(width-term.DispWidth(line), 0)
	return bg + term.ReapplyAfterReset(line, bg) + term.PadSpaces(pad) + term.AnsiReset
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
func (m *browseModel) commitBySHA(sha string) *gitdata.Commit {
	for i := range m.commits {
		if m.commits[i].SHA == sha {
			return &m.commits[i]
		}
	}
	return nil
}

// cursorLine はカーソル位置のコミットヘッダー行を強調する。溝の「→ 」に加え、色ありでは
// 行全体 (溝込み) を暗青 bg で塗る。色なし (NO_COLOR) では矢印のみ。
func (m *browseModel) cursorLine(text string) string {
	if !m.colored {
		return term.ClipToWidth(term.CursorGutterMark+text, m.contentWidth())
	}
	return m.bgLine(term.CursorGutterMark+text, term.AnsiCursorBg)
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
// (行の幅計算が term.CursorGutterWidth 前提。issues_view.go の rowLine) では矢印が二重になる
// (実測 2026-07-31: "→ → 030 ○ feat alpha")。cursorPaint の契約は issuesRenderOpts の doc
// どおり「強調」だけで、溝の所有者は viewer 側。
func (m *browseModel) cursorEmphasis(text string) string {
	if !m.colored {
		return text // 色なしでは矢印 (viewer 側の溝) だけが指標
	}
	return m.bgLine(text, term.AnsiCursorBg)
}

func (m *browseModel) bgLine(text, bg string) string {
	if !m.colored {
		return term.ClipToWidth(text, m.contentWidth())
	}
	text = term.ClipToWidth(text, m.contentWidth())
	pad := max(m.contentWidth()-term.DispWidth(text), 0)
	return bg + term.ReapplyAfterReset(text, bg) + term.PadSpaces(pad) + term.AnsiReset
}

func (m *browseModel) hintLine() string {
	// 全画面ビューアの hint は viewer 自身のものだけを出す (activeFullScreen から導出する)。🚨 **actModal の確認より前に return するのは意図**: status viewer の
	// 中から `b` / `p` で push / pull の確認を出したとき、hint 行は viewer の語彙のままになる
	// (中央の確認モーダルが y/N を案内するので、狭い hint 行を奪ってまで二重に出さない)。
	// 🚨 CI 進捗・GH 警告の前置はしない: viewer の hint は popup の実幅
	// ぴったりに詰めてあり (issues_view.go の hint)、前置すると末尾のキー案内 = 抜ける手段が
	// 黙って切り落とされる。CI は viewer を閉じれば見える。
	switch m.activeFullScreen() {
	case FullScreenRatelimit:
		return m.hintLineText(m.rlDash.Hint(m.hintWidth()))
	case FullScreenDoctor:
		return m.hintLineText(m.doctorOv.Hint(m.hintWidth()))
	case FullScreenStatus:
		return m.hintLineText(m.statusOv.Hint(m.hintWidth()))
	case FullScreenIssues:
		return m.hintLineText(m.issuesOv.Hint(m.hintWidth()))
	case FullScreenFiler:
		return m.hintLineText(m.filerV.Hint(m.hintWidth()))
	case FullScreenLan:
		return m.hintLineText(m.lanV.Hint(m.hintWidth()))
	case FullScreenNone, FullScreenCount:
		// 全画面ビューアが出ていない = 下の一覧の hint
	}
	// 🚨 前置 (取得中 / gh の警告) は hint と**同じ予算**に載せる。
	// term.FitHintItems が幅ぴったりに収めた後で前置を積むと、hintLineText の term.ClipToWidth が
	// 末尾 = **出口**を落とす。m.ghErr は次の取得開始までクリアされないので、
	// gh 未導入 / 未認証 / 非 GitHub repo では**常時**その状態になり、幅 60〜200 の全帯で
	// 出口が消える (2026-09-06 に実測)。
	//
	// 前置は「今なぜ待っているか」の説明で、抜ける手段より優先度が低い。予算の半分を
	// 超えたら前置の方を切る。
	hw := m.hintWidth()
	// 前置は「今なぜ待っているか」の説明で、抜ける手段より優先度が低い。予算の半分を
	// 超えたら前置の方を切る (頭打ちは hintPrefix の中でやる — ここで prefix に触る形を
	// 残さないため)。
	prefix := m.hintPrefix(hw / 2)
	hw = max(hw-term.DispWidth(prefix), 0)

	// 面ごとの hint 項目は hint_surfaces.go のレジストリが唯一の出典。
	// ここで直接文字列を組まないこと — 予算計算 (term.FitHintItems) と幅ゲートの両方を
	// 迂回でき、末尾 = 出口が黙って切れる形が再生産される (279 / 281 の P1 2 本がその形)。
	// 迂回は TestHintLineHasNoInlineHintText が字句で止める。
	hint := term.FitHintItems(hw, hintBuilders[m.activeHintSurface()](m))
	return m.hintLineText(prefix + hint)
}

// hintLineText は最下行の hint を塗って幅に収める (前置の有無で分かれる出口を 1 本にする)。
func (m *browseModel) hintLineText(hint string) string {
	painted := term.Paint(hint, term.AnsiDim, m.colored)
	if m.frameActive() {
		// hint は板の外 (最下行) だが、左余白 1 桁を付けて板の左端 (┌) と縦に揃える。素朴に
		// " " を前置すると、既定 hint が clip 後に m.width ちょうどになり実効幅 m.width+1 で
		// 折り返し崩壊するため、clip 幅を左右余白ぶん (2) 差し引く (板の footprint と同じ span)。
		return " " + term.ClipToWidth(painted, m.hintWidth())
	}
	return term.ClipToWidth(painted, m.hintWidth())
}

// hintWidth は hint 行に使える桁数。🚨 hintLineText の clip 幅と、hint を組む側 (StatusView.Hint)
// が使う予算はこの 1 か所から取る。2 か所に式を書くと、片方だけ余白を変えた瞬間に「収まる
// つもりで組んだ hint が黙って切られる」形でずれる。
func (m *browseModel) hintWidth() int {
	if m.frameActive() {
		return max(m.width-2, 1)
	}
	return m.width
}
