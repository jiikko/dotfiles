package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"glogx/internal/commitview"
	"glogx/internal/extcmd"
	"glogx/internal/gitdata"
	"glogx/internal/term"

	"glogx/internal/app"

	tea "charm.land/bubbletea/v2"
)

// prMsg は commit に紐づく PR のオンデマンド取得の結果 (p キー)。
type prMsg struct {
	sha   string
	pr    *gitdata.PRRef // nil = PR なし
	ghErr *gitdata.GHError
}

// prStatusMsg は PR 詳細状態のオンデマンド取得の結果 (P キー)。
type prStatusMsg struct {
	sha    string
	status *gitdata.PRStatus // nil = PR なし
	ghErr  *gitdata.GHError
}

// loadCommitDiff はテストで実 git を叩かないための差し替え点。
var loadCommitDiff = gitdata.LoadCommitDiff

func (m *browseModel) onPR(msg prMsg) (tea.Model, tea.Cmd) {
	delete(m.prBusy, msg.sha)
	// 取得中にカーソルが動いていたら、結果の反映 (キャッシュ・バッジ) だけ行い
	// 自動オープン・トーストは出さない: 離れたコミットの PR がいきなりブラウザで
	// 開くのを防ぐ (jobDetailMsg / prStatusMsg と同型の stale ガード)
	wasCurrent := len(m.commits) > 0 && m.commits[m.cursor].SHA == msg.sha
	if msg.ghErr != nil {
		// 一時エラーをキャッシュすると「PR はありません」という誤答が固定される
		// (次の p で再試行させる) ため、キャッシュは成功時のみ
		if wasCurrent {
			m.showWarning("PR の取得に失敗しました: " + app.FirstLine(msg.ghErr.Warning()))
		}
		return m, m.maybeTick()
	}
	m.prCache[msg.sha] = msg.pr
	m.invalidateLines() // コミット行の PR バッジに反映
	if !wasCurrent {
		return m, m.maybeTick()
	}
	if msg.pr == nil {
		m.toast.Show("このコミットに紐づく PR はありません", false)
		return m, m.maybeTick()
	}
	m.toast.Show(fmt.Sprintf("PR #%d を開きます", msg.pr.Number), true)
	return m, tea.Batch(m.openURLCmd(msg.pr.URL), m.maybeTick())
}

func (m *browseModel) onPRStatus(msg prStatusMsg) (tea.Model, tea.Cmd) {
	// receive は当該 sha が表示中ならエラー時に close するため、sha 一致は receive の前に捕捉する。
	// notice は「今表示中の対象」の失敗のときだけ出す: 別 sha へ移った後に届く遅延エラーで
	// 無関係な失敗 notice を被せない (レビュー確定 low)。
	wasCurrent := msg.sha == m.prStatusOv.SHA()
	m.prStatusOv.Receive(msg.sha, msg.status, msg.ghErr)
	if msg.ghErr != nil && wasCurrent {
		// 一時エラーはキャッシュしない (receive 側)。理由はトーストで伝える
		m.showWarning("PR の取得に失敗しました: " + app.FirstLine(msg.ghErr.Warning()))
	}
	return m, m.maybeTick()
}

func (m *browseModel) onDiff(msg commitview.DiffMsg) (tea.Model, tea.Cmd) {
	// 警告は「今表示中の対象」の失敗のときだけ出す: 別 SHA へ移った後に届く遅延エラーで
	// 無関係な失敗 notice を被せない (prStatusMsg の wasCurrent と同型。receive は当該
	// sha が表示中ならエラー時に close するため、一致は receive の前に捕捉する)
	wasCurrent := msg.SHA() == m.diffOv.SHA()
	if err := m.diffOv.Receive(msg); err != nil && wasCurrent {
		m.showWarning("diff の取得に失敗しました: " + app.FirstLine(err.Error()))
	}
	return m, m.maybeTick()
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
		m.toast.Show("GitHub の remote が無いため開けません", false)
		return nil
	}
	return m.openURLCmd(url)
}

// copyWithToast はクリップボードへコピーし、結果をトーストで通知する共有経路 (失敗文言が
// 4 箇所で複製されていたのを一本化)。失敗トーストは lastWarning を汚さない (コピー対象の
// 警告を上書きして w の再試行を潰さないため、showWarning は通さない)。
func (m *browseModel) copyWithToast(text, successMsg string) {
	if err := extcmd.CopyToClipboard(text); err != nil {
		m.toast.Show(app.ClipboardFailText(err), false)
		return
	}
	m.toast.Show(successMsg, true)
}

func (m *browseModel) copyFocusURL() {
	var url string
	if job, ok := m.focusedJob(); ok {
		url = job.URL
	} else {
		url = m.commitURL()
	}
	if url == "" {
		m.toast.Show("コピーできる URL がありません", false)
		return
	}
	// クリップボードへ入れる直前にも平文にする (copyJobContextLines と同じ理由。job.URL は外部 CI が
	// 設定できる)
	url = term.SanitizePlainLine(url)
	m.copyWithToast(url, "コピーしました: "+url)
}

// openURLCmd は URL をブラウザで開く Cmd。StatusContext の targetUrl 等、外部が任意に
// 設定できる値を通すため、file:// 等でローカルのハンドラを起動させないよう
// http(s) だけを開く。
func (m *browseModel) openURLCmd(url string) tea.Cmd {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		m.toast.Show("http(s) 以外の URL は開きません", false)
		return nil
	}
	return func() tea.Msg {
		return extcmd.OpenURLMsg{Err: extcmd.OpenInBrowser(url)}
	}
}

// prTargetSHA は PR 系操作 (p/P) の対象 SHA を返す。取得できない状態 (コミットなし /
// remote なし / 未 push) は理由をトーストで出して ok=false (openPR / openPRStatus で
// 文言まで同一だったガード 3 連の一本化)。
func (m *browseModel) prTargetSHA() (sha string, ok bool) {
	if len(m.commits) == 0 {
		return "", false
	}
	if !m.hasRepo {
		m.toast.Show("GitHub の remote が無いため PR を取得できません", false)
		return "", false
	}
	sha = m.commits[m.cursor].SHA
	if m.statuses[sha] == gitdata.StateUnpushed {
		m.toast.Show("未 push のコミットに PR はありません", false)
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
			m.toast.Show("このコミットに紐づく PR はありません", false)
			return nil
		}
		return m.openURLCmd(pr.URL)
	}
	if m.prBusy[sha] {
		return nil
	}
	m.prBusy[sha] = true
	// 進行中トースト (印なしのシアン) は直後に届く prMsg の結果トーストで上書きされる。tick は呼び出し側
	// (handleListKey/handlePanelKey の maybeTick) が回す。
	m.toast.ShowInfo("PR を検索中...")
	repo := m.repo
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), app.FetchTimeout)
		defer cancel()
		pr, ghErr := gitdata.FetchCommitPR(ctx, gitdata.ExecRunner, repo, sha)
		return prMsg{sha: sha, pr: pr, ghErr: ghErr}
	}
}

// openPRStatus はカーソル位置コミットの PR 状態ポップアップを開く (P キー)。
// 同じコミットで再度 P を押すと閉じる (toggle)。取得はオンデマンド単発 GraphQL
// (一括クエリと prCache は number/url/state のまま変えない。理由は PRStatus のコメント)。
func (m *browseModel) openPRStatus() tea.Cmd {
	sha, ok := m.prTargetSHA()
	if !ok {
		return nil
	}
	if !m.prStatusOv.Open(sha) {
		return nil // toggle 閉 / キャッシュヒット
	}
	repo := m.repo
	cmd := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), app.FetchTimeout)
		defer cancel()
		status, ghErr := gitdata.FetchPRStatus(ctx, gitdata.ExecRunner, repo, sha)
		return prStatusMsg{sha: sha, status: status, ghErr: ghErr}
	}
	return tea.Batch(cmd, m.maybeTick())
}

// handlePRStatusKey は PR 状態ポップアップ表示中のキー操作。o (ブラウザ) / y (URL コピー) と
// 閉じるだけの小さなモーダル (スクロールする本文は無い)。
func (m *browseModel) handlePRStatusKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "h", "left", "esc", "P", "enter":
		m.prStatusOv.Close()
	// current()==nil の 2 状態 (取得中 / PR なし) はどちらもポップアップ自身が spinner・
	// 「(紐づく PR はありません)」で可視化済みなので、o/y は無言 no-op でよい (取得中に
	// 「PR がありません」トーストを出すと誤報になる)。
	case "o":
		if pr := m.prStatusOv.Current(); pr != nil {
			return m, m.openURLCmd(pr.URL)
		}
	case "y":
		if pr := m.prStatusOv.Current(); pr != nil {
			// PR の url は取得の段階で無害化していない (表示する欄だけを無害化している)。
			// クリップボードは最後の関門なので、入れる直前に平文にする
			url := term.SanitizePlainLine(pr.URL)
			m.copyWithToast(url, "コピーしました: "+url)
		}
	}
	return m, nil
}

// prStatusCILine は PR ポップアップに出すコミット CI 状態の 1 行 (出典はコミット行と同じ
// statuses/details)。失敗時は取得済み details から失敗 job 数を添える。
func (m *browseModel) prStatusCILine() string {
	sha := m.prStatusOv.SHA()
	if sha == "" {
		return ""
	}
	st, ok := m.statuses[sha]
	if !ok {
		return ""
	}
	line := StatusGlyph(st, m.colored, m.spinner()) + " " + string(st)
	if st == gitdata.StateFailure {
		n := 0
		for _, d := range m.details[sha] {
			if d.State == gitdata.StateFailure {
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
	if !m.diffOv.Open(sha) {
		return nil // toggle 閉 / キャッシュヒット / 取得中: 追加取得は不要
	}
	return m.fetchDiffCmd(sha)
}

// fetchDiffCmd は sha の diff を非同期で取りに行く (結果は diffMsg)。
func (m *browseModel) fetchDiffCmd(sha string) tea.Cmd {
	colored := m.colored
	cmd := func() tea.Msg {
		lines, err := loadCommitDiff(sha, colored)
		return commitview.NewDiffMsg(sha, lines, err)
	}
	return tea.Batch(cmd, m.maybeTick())
}

// openNeighborDiff は diff を開いたまま隣 (delta = ±1) のコミットへ差し替える (J/K。
// docs/glogx-ui-guide.md §6: 小文字 j/k が「行」なら大文字は「項目」)。
//
// 起点は一覧のカーソルではなく**今開いている SHA**にする。job パネルから開いた diff は
// panelSHA を優先しており、一覧のカーソルと一致しない場合があるため (カーソル起点だと
// 「隣」が見ている diff の隣にならない)。移った先へはカーソルも追従させる。
// 端では止めて案内する (巻かない。issues viewer の openNeighbor と同じ)。
func (m *browseModel) openNeighborDiff(delta int) tea.Cmd {
	idx := slices.IndexFunc(m.commits, func(c gitdata.Commit) bool { return c.SHA == m.diffOv.SHA() })
	if idx < 0 {
		idx = m.cursor
	}
	ni := idx + delta
	if ni < 0 || ni >= len(m.commits) {
		if delta > 0 {
			m.toast.Show("これが最後のコミットです", false)
		} else {
			m.toast.Show("これが最初のコミットです", false)
		}
		return nil
	}
	m.cursor = ni
	m.ensureCursorVisible()
	// 🚨 open は同じ SHA だと toggle で閉じるが、隣は必ず別 SHA なのでここでは閉じない
	if !m.diffOv.Open(m.commits[ni].SHA) {
		return nil // キャッシュヒット / 取得中
	}
	return m.fetchDiffCmd(m.commits[ni].SHA)
}

// handleDiffKey は diff ポップアップ表示中のキー操作。y (URL コピー) は境界をまたぐのでここで
// 処理し、スクロール/閉じるは diffOv.Scroll へ委譲する (pager 流儀の詳細は commitview.DiffOverlay 側)。
func (m *browseModel) handleDiffKey(key string) (tea.Model, tea.Cmd) {
	if key == "y" {
		m.copyFocusURL()
		return m, nil
	}
	switch key {
	case "J", "shift+down":
		return m, m.openNeighborDiff(1)
	case "K", "shift+up":
		return m, m.openNeighborDiff(-1)
	}
	m.diffOv.Scroll(key, m.visibleDiffRows())
	// 半ページ移動は glide (internal/term/scroll_glide.go) で進むので tick を張る。張らないと advanceGlide を
	// 呼ぶ者がおらず、表示位置がスクロール前で固まったまま「キーが効かない」ように見える
	// (2026-07-31 に実測: Space 後に j を何度押しても先頭行が出続ける実測)。
	// 1 行移動・閉じるは glide を使わないので tick を増やさない。
	if m.diffOv.Animating() {
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
	return m.diffOv.BoxLines(m.contentWidth(), m.colored, m.spinner(), m.commitBySHA(m.diffOv.SHA()), m.visibleDiffRows())
}
