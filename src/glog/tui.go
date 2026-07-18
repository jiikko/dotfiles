package main

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
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
	spinnerInterval = 80 * time.Millisecond
	// maxPanelJobs は job パネルに一度に表示する行数。超過分はパネル内でスクロールする。
	maxPanelJobs = 10
)

type ciResultMsg struct {
	batch CIBatch
	ghErr *GHError
}

// detailMsg はパネル表示時のオンデマンド取得 (キャッシュヒットで詳細が無い SHA) の結果。
type detailMsg struct {
	sha   string
	batch CIBatch
	ghErr *GHError
}

type tickMsg struct{}

// openURLMsg は job 詳細ページをブラウザで開いた結果。
type openURLMsg struct{ err error }

// jobDetailMsg は job 詳細 (annotations / ログ tail) のオンデマンド取得の結果。
type jobDetailMsg struct {
	key   string // sha/jobIdx (取得中表示とキャッシュのキー)
	lines []string
	ghErr *GHError
}

// prMsg は commit に紐づく PR のオンデマンド取得の結果 (p キー)。
type prMsg struct {
	sha   string
	pr    *PRRef // nil = PR なし
	ghErr *GHError
}

// diffMsg はコミット diff のオンデマンド取得の結果 (d キー)。
type diffMsg struct {
	sha   string
	lines []string
	err   error
}

// loadCommitDiff はテストで実 git を叩かないための差し替え点。
var loadCommitDiff = LoadCommitDiff

// openInBrowser はテストで実ブラウザを開かないための差し替え点。
var openInBrowser = func(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Run()
	default:
		return exec.Command("xdg-open", url).Run()
	}
}

// copyToClipboard はテストで実クリップボードを触らないための差し替え点。
// tmux 内なら load-buffer -w (tmux バッファ + OSC52 でシステム側にも届く) を優先し、
// 失敗時や tmux 外は OS のクリップボードコマンドへ。
var copyToClipboard = func(text string) error {
	if os.Getenv("TMUX") != "" {
		cmd := exec.Command("tmux", "load-buffer", "-w", "-")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	default:
		cmd = exec.Command("xclip", "-selection", "clipboard")
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

type browseModel struct {
	commits        []Commit
	statuses       map[string]CIState // 表示用 (キャッシュ + 取得結果のマージ)
	fetched        map[string]CIState // API から取得した分 (終了後のキャッシュ保存用)
	details        map[string][]CheckDetail
	detailsLoading map[string]bool
	toFetch        []string
	repo           Repo
	hasRepo        bool
	ghErr          *GHError
	decor          *DecorColors
	oneline        bool
	colored        bool
	frame          int
	width          int
	height         int
	cursor         int    // コミット index
	offset         int    // ビューポート先頭の行 index
	panelSHA       string // job パネルを表示中のコミット SHA ("" = パネルなし)
	panelCursor    int    // パネル内で選択中の job index (-1 = タイトル行にフォーカス)
	detailOpen     bool   // job 詳細 (annotations / ログ tail) ポップアップを表示中か
	detailOffset   int    // 詳細ポップアップのスクロール位置
	jobDetail      map[string][]string // key (sha/jobIdx) → 詳細行 (メモリ内キャッシュ)
	jobDetailBusy  map[string]bool     // 取得中の key
	prCache        map[string]*PRRef   // sha → 紐づく PR (nil 格納 = 確認済みで PR なし)
	prBusy         map[string]bool     // PR 取得中の sha
	diffSHA        string              // diff ポップアップ表示中の SHA ("" = 非表示)
	diffOffset     int                 // diff ポップアップのスクロール位置
	diffCache      map[string][]string // sha → 整形済み diff 行 (メモリ内キャッシュ)
	diffBusy       map[string]bool     // diff 取得中の sha
	notice         string // hint 行に出す一時メッセージ (次のキーで消える)
	fetching       bool
	done           bool
	fetch          tea.Cmd
	cancel         context.CancelFunc

	// lines() のメモ化。行リストの再構築は O(出力全行数) で、-p の巨大 patch では
	// キー 1 打ごとに数万行を組み直すことになるためキャッシュする。行内容を変えうる
	// 更新 (statuses/details のマージ・スピナーフレーム・幅変更) だけが無効化する。
	// カーソル移動・パネル開閉は View の窓側で重ねるだけなので無効化不要
	linesCache []Line
	linesValid bool
}

func newBrowseModel(commits []Commit, statuses map[string]CIState, toFetch []string, repo Repo, hasRepo bool, opts *Options, colored bool, width, height int) *browseModel {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	m := &browseModel{
		commits:        commits,
		statuses:       statuses,
		fetched:        map[string]CIState{},
		details:        map[string][]CheckDetail{},
		detailsLoading: map[string]bool{},
		jobDetail:      map[string][]string{},
		jobDetailBusy:  map[string]bool{},
		prCache:        map[string]*PRRef{},
		prBusy:         map[string]bool{},
		diffCache:      map[string][]string{},
		diffBusy:       map[string]bool{},
		toFetch:        toFetch,
		repo:           repo,
		hasRepo:        hasRepo,
		panelCursor:    -1,
		oneline:        opts.Oneline,
		colored:        colored,
		width:          width,
		height:         height,
		fetching:       len(toFetch) > 0,
		cancel:         cancel,
	}
	if m.fetching {
		m.fetch = func() tea.Msg {
			defer cancel()
			batch, ghErr := FetchCIStatuses(ctx, ExecRunner, repo, toFetch)
			return ciResultMsg{batch: batch, ghErr: ghErr}
		}
	}
	return m
}

func (m *browseModel) Init() tea.Cmd {
	if m.fetching {
		return tea.Batch(m.fetch, tick())
	}
	return nil
}

func tick() tea.Cmd {
	return tea.Tick(spinnerInterval, func(time.Time) tea.Msg { return tickMsg{} })
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
		m.ensureCursorVisible()
		return m, nil
	case tickMsg:
		if !m.spinnerActive() {
			return m, nil
		}
		m.frame++
		m.invalidateLines() // 取得中コミットのスピナーフレームが進む
		return m, tick()
	case ciResultMsg:
		m.invalidateLines()
		m.ghErr = msg.ghErr
		// 応答に無かった SHA は unknown で埋める (fetched へ入れる = 終了時に SaveCache
		// される 30 秒の負キャッシュ)。q での中断 (fillUnknown) と違い、こちらは API の
		// 実際の返答に基づく確定
		filled := fillUnknownFetched(msg.batch.Statuses, m.toFetch)
		maps.Copy(m.fetched, filled)
		maps.Copy(m.statuses, filled)
		maps.Copy(m.details, msg.batch.Details)
		// PR はバッジ表示と p キーの両方で使う (一括取得分で p が即開きになる)
		maps.Copy(m.prCache, msg.batch.PRs)
		m.fetching = false
		// 一括取得待ちでパネルを開いていた SHA の loading を解除する (結果が来なかった
		// SHA も含めて解除。details 不在は「(CI job 情報なし)」表示に落ちる)
		for _, sha := range m.toFetch {
			delete(m.detailsLoading, sha)
		}
		return m, nil
	case detailMsg:
		m.invalidateLines()
		delete(m.detailsLoading, msg.sha)
		if msg.ghErr != nil {
			m.ghErr = msg.ghErr
		}
		maps.Copy(m.fetched, msg.batch.Statuses)
		maps.Copy(m.statuses, msg.batch.Statuses)
		maps.Copy(m.details, msg.batch.Details)
		maps.Copy(m.prCache, msg.batch.PRs)
		return m, nil
	case jobDetailMsg:
		delete(m.jobDetailBusy, msg.key)
		if msg.ghErr != nil {
			m.ghErr = msg.ghErr
		}
		if msg.lines != nil {
			m.jobDetail[msg.key] = msg.lines
			if m.detailOpen && m.detailKey() == msg.key {
				// ログは末尾 (直近の出力) が本題なので下端から表示する
				m.detailOffset = max(len(msg.lines)-m.visibleDetailRows(), 0)
			}
		}
		return m, nil
	case prMsg:
		delete(m.prBusy, msg.sha)
		if msg.ghErr != nil {
			// 一時エラーをキャッシュすると「PR はありません」という誤答が固定される
			// (次の p で再試行させる) ため、キャッシュは成功時のみ
			m.notice = "PR の取得に失敗しました: " + firstLine(msg.ghErr.Warning())
			return m, nil
		}
		m.prCache[msg.sha] = msg.pr
		m.invalidateLines() // コミット行の PR バッジに反映
		if msg.pr == nil {
			m.notice = "このコミットに紐づく PR はありません"
			return m, nil
		}
		m.notice = fmt.Sprintf("PR #%d を開きます", msg.pr.Number)
		return m, m.openURLCmd(msg.pr.URL)
	case diffMsg:
		delete(m.diffBusy, msg.sha)
		if msg.err != nil {
			m.notice = "diff の取得に失敗しました: " + firstLine(msg.err.Error())
			if m.diffSHA == msg.sha {
				m.diffSHA = ""
			}
			return m, nil
		}
		m.diffCache[msg.sha] = msg.lines
		return m, nil
	case openURLMsg:
		if msg.err != nil {
			m.notice = "ブラウザを開けませんでした: " + firstLine(msg.err.Error())
		}
		return m, nil
	case tea.KeyMsg:
		// 高速連打やパイプ入力では複数の文字キーが 1 つの KeyMsg (Runes 長 > 1) に
		// まとまって届く。分解せず msg.String() だけ見ると "hhq" のような未知キー扱いに
		// なり、以降の操作が全て無視されたように見える (pty スモークで実測) ため、
		// 1 文字ずつのキー入力として順に処理する
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 {
			var cmds []tea.Cmd
			for _, r := range msg.Runes {
				_, cmd := m.handleKey(string(r))
				if cmd != nil {
					cmds = append(cmds, cmd)
				}
				if m.done {
					break
				}
			}
			return m, tea.Batch(cmds...)
		}
		return m.handleKey(msg.String())
	}
	return m, nil
}

func (m *browseModel) handleKey(key string) (tea.Model, tea.Cmd) {
	m.notice = ""
	if key == "ctrl+c" {
		return m.quit()
	}
	// diff ポップアップ表示中はスクロール/閉じる操作だけを受ける (最前面のモーダル)
	if m.diffSHA != "" {
		return m.handleDiffKey(key)
	}
	// q はビューのスタックを 1 段戻る (tig 流。ユーザー要望): 詳細 → job 一覧 →
	// コミット一覧、と閉じていき、最上位でだけ終了。即終了したいときは Ctrl-C
	if key == "q" {
		switch {
		case m.detailOpen:
			m.detailOpen = false
			m.detailOffset = 0
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
		m.cursor = clampIdx(m.cursor+1, len(m.commits))
		m.ensureCursorVisible()
	case "k", "up", "ctrl+p":
		m.cursor = clampIdx(m.cursor-1, len(m.commits))
		m.ensureCursorVisible()
	case "g", "home":
		m.cursor = 0
		m.offset = 0
	case "G", "end":
		m.cursor = clampIdx(len(m.commits)-1, len(m.commits))
		m.ensureCursorVisible()
	case "ctrl+d", "pgdown":
		m.offset = m.clampOffset(m.offset + m.pageSize()/2)
	case "ctrl+u", "pgup":
		m.offset = m.clampOffset(m.offset - m.pageSize()/2)
	case "enter", " ", "l", "right", "tab":
		return m, m.openPanel()
	case "y":
		m.copyFocusURL()
	case "p":
		return m, m.openPR()
	case "d":
		return m, m.openDiff()
	}
	return m, nil
}

// quit はアプリ全体を終了する (取得中断分は unknown へ落とす)。
func (m *browseModel) quit() (tea.Model, tea.Cmd) {
	m.cancel()
	if m.fetching {
		m.fillUnknown()
	}
	m.done = true
	return m, tea.Quit
}

// handlePanelKey は job パネル表示中のキー操作。j/k はパネル内のフォーカス移動になる。
// Enter は一貫して「TUI 内で開閉 (toggle)」: タイトル行 = パネルを閉じる (Enter 連打で
// 開閉 toggle)、job 行 = 詳細 (annotations / ログ tail) ポップアップを開く。
// ブラウザで開くのは o (ユーザー要望)。
func (m *browseModel) handlePanelKey(key string) (tea.Model, tea.Cmd) {
	if m.detailOpen {
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
	case "p":
		return m, m.openPR()
	case "d":
		return m, m.openDiff()
	}
	return m, nil
}

// handleDetailKey は job 詳細ポップアップ表示中のキー操作。j/k は詳細のスクロール。
// Enter は toggle の閉じる側、o はブラウザ。
func (m *browseModel) handleDetailKey(key string) (tea.Model, tea.Cmd) {
	rows := m.visibleDetailRows()
	maxOffset := max(len(m.jobDetail[m.detailKey()])-rows, 0)
	switch key {
	case "enter", " ", "esc", "h", "left":
		m.detailOpen = false
		m.detailOffset = 0
	case "j", "down", "ctrl+n":
		m.detailOffset = min(m.detailOffset+1, maxOffset)
	case "k", "up", "ctrl+p":
		m.detailOffset = max(m.detailOffset-1, 0)
	case "ctrl+d", "pgdown":
		m.detailOffset = min(m.detailOffset+rows/2, maxOffset)
	case "ctrl+u", "pgup":
		m.detailOffset = max(m.detailOffset-rows/2, 0)
	case "g", "home":
		m.detailOffset = 0
	case "G", "end":
		m.detailOffset = maxOffset
	case "o":
		return m, m.openJob()
	case "y":
		m.copyFocusURL()
	}
	return m, nil
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

// jobDetailRows は詳細ポップアップに一度に表示する行数の上限。実際の行数は
// 端末の高さに合わせて visibleDetailRows が縮める。
const jobDetailRows = 15

// visibleDetailRows は詳細ポップアップが実際に使える行数 (job パネルとヒント行を
// 差し引いた残り。低い端末で詳細ボックスがビューポートに切られ、末尾スクロールが
// 見えなくなるのを防ぐ)。
func (m *browseModel) visibleDetailRows() int {
	jobBoxLines := min(max(len(m.details[m.panelSHA]), 1), maxPanelJobs) + 2
	return max(min(jobDetailRows, m.pageSize()-jobBoxLines-2), 3)
}

// openJobDetail はフォーカス中 job の annotations / ログ tail のポップアップを開く。
func (m *browseModel) openJobDetail() tea.Cmd {
	check, ok := m.focusedJob()
	if !ok {
		return nil
	}
	m.detailOpen = true
	m.detailOffset = 0
	key := m.detailKey()
	if lines, ok := m.jobDetail[key]; ok {
		m.detailOffset = max(len(lines)-m.visibleDetailRows(), 0)
		return nil
	}
	if m.jobDetailBusy[key] {
		return nil
	}
	m.jobDetailBusy[key] = true
	repo := m.repo
	cmd := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		lines, ghErr := FetchJobDetail(ctx, ExecRunner, repo, check)
		return jobDetailMsg{key: key, lines: lines, ghErr: ghErr}
	}
	return tea.Batch(cmd, tick())
}

// copyFocusURL はフォーカス位置の URL (job 選択中はその job、それ以外はコミット) を
// クリップボードへコピーする。LLM に貼る用途 (ユーザー要望)。
func (m *browseModel) copyFocusURL() {
	url := ""
	if job, ok := m.focusedJob(); ok {
		url = job.URL
	} else if m.hasRepo && len(m.commits) > 0 {
		url = fmt.Sprintf("https://github.com/%s/%s/commit/%s", m.repo.Owner, m.repo.Name, m.commits[m.cursor].SHA)
	}
	if url == "" {
		m.notice = "コピーできる URL がありません"
		return
	}
	if err := copyToClipboard(url); err != nil {
		m.notice = "コピーに失敗しました: " + firstLine(err.Error())
		return
	}
	m.notice = "コピーしました: " + url
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
	if m.fetching && slices.Contains(m.toFetch, sha) {
		m.detailsLoading[sha] = true
		return nil
	}
	m.detailsLoading[sha] = true
	repo := m.repo
	cmd := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		batch, ghErr := FetchCIStatuses(ctx, ExecRunner, repo, []string{sha})
		return detailMsg{sha: sha, batch: batch, ghErr: ghErr}
	}
	return tea.Batch(cmd, tick())
}

func (m *browseModel) closePanel() {
	m.panelSHA = ""
	m.panelCursor = -1
	m.detailOpen = false
	m.detailOffset = 0
}

// openURLCmd は URL をブラウザで開く Cmd。StatusContext の targetUrl 等、外部が任意に
// 設定できる値を通すため、file:// 等でローカルのハンドラを起動させないよう
// http(s) だけを開く。
func (m *browseModel) openURLCmd(url string) tea.Cmd {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		m.notice = "http(s) 以外の URL は開きません"
		return nil
	}
	return func() tea.Msg {
		return openURLMsg{err: openInBrowser(url)}
	}
}

// openJob はパネルで選択中の job の詳細ページをブラウザで開く。
func (m *browseModel) openJob() tea.Cmd {
	job, ok := m.focusedJob()
	if !ok {
		return nil
	}
	if job.URL == "" {
		m.notice = "この job には詳細ページの URL がありません"
		return nil
	}
	return m.openURLCmd(job.URL)
}

// openPR はカーソル位置のコミットに紐づく PR をブラウザで開く (p キー)。
// commit → PR の関連は GitHub (associatedPullRequests) から取得し、結果はキャッシュする。
func (m *browseModel) openPR() tea.Cmd {
	if len(m.commits) == 0 {
		return nil
	}
	if !m.hasRepo {
		m.notice = "GitHub の remote が無いため PR を取得できません"
		return nil
	}
	sha := m.commits[m.cursor].SHA
	if m.statuses[sha] == StateUnpushed {
		m.notice = "未 push のコミットに PR はありません"
		return nil
	}
	if pr, ok := m.prCache[sha]; ok {
		if pr == nil {
			m.notice = "このコミットに紐づく PR はありません"
			return nil
		}
		return m.openURLCmd(pr.URL)
	}
	if m.prBusy[sha] {
		return nil
	}
	m.prBusy[sha] = true
	m.notice = "PR を検索中..."
	repo := m.repo
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		pr, ghErr := FetchCommitPR(ctx, ExecRunner, repo, sha)
		return prMsg{sha: sha, pr: pr, ghErr: ghErr}
	}
}

// openDiff はカーソル位置 (パネル表示中はそのコミット) の diff ポップアップを開く (d キー)。
// 同じコミットで再度 d を押すと閉じる (toggle)。job パネルは閉じてから開く (重ね順の単純化)。
func (m *browseModel) openDiff() tea.Cmd {
	if len(m.commits) == 0 {
		return nil
	}
	sha := m.commits[m.cursor].SHA
	if m.panelSHA != "" {
		sha = m.panelSHA
	}
	m.closePanel()
	if m.diffSHA == sha {
		m.diffSHA = ""
		return nil
	}
	m.diffSHA = sha
	m.diffOffset = 0
	if _, ok := m.diffCache[sha]; ok {
		return nil
	}
	if m.diffBusy[sha] {
		return nil
	}
	m.diffBusy[sha] = true
	colored := m.colored
	cmd := func() tea.Msg {
		lines, err := loadCommitDiff(sha, colored)
		return diffMsg{sha: sha, lines: lines, err: err}
	}
	return tea.Batch(cmd, tick())
}

// handleDiffKey は diff ポップアップ表示中のキー操作。j/k はスクロール。
func (m *browseModel) handleDiffKey(key string) (tea.Model, tea.Cmd) {
	rows := m.visibleDiffRows()
	maxOffset := max(len(m.diffCache[m.diffSHA])-rows, 0)
	switch key {
	case "q", "esc", "enter", " ", "h", "left", "d":
		m.diffSHA = ""
		m.diffOffset = 0
	case "j", "down", "ctrl+n":
		m.diffOffset = min(m.diffOffset+1, maxOffset)
	case "k", "up", "ctrl+p":
		m.diffOffset = max(m.diffOffset-1, 0)
	case "ctrl+d", "pgdown":
		m.diffOffset = min(m.diffOffset+rows/2, maxOffset)
	case "ctrl+u", "pgup":
		m.diffOffset = max(m.diffOffset-rows/2, 0)
	case "g", "home":
		m.diffOffset = 0
	case "G", "end":
		m.diffOffset = maxOffset
	case "y":
		m.copyFocusURL()
	}
	return m, nil
}

// visibleDiffRows は diff ポップアップの本文行数。diff は主役コンテンツなので
// ビューポートほぼ全面 (枠 2 行 + 余白 1 行 + ヒント行ぶんを差し引く) を使う。
func (m *browseModel) visibleDiffRows() int {
	return max(m.pageSize()-4, 3)
}

// diffBoxLines は diff ポップアップの描画行 (枠付き)。非表示なら nil。
func (m *browseModel) diffBoxLines() []string {
	if m.diffSHA == "" {
		return nil
	}
	width := m.width
	if width <= 0 {
		width = 80
	}
	var commit *Commit
	for i := range m.commits {
		if m.commits[i].SHA == m.diffSHA {
			commit = &m.commits[i]
			break
		}
	}
	if commit == nil {
		return nil
	}
	var rows []string
	title := fmt.Sprintf(" diff: %s %s ", commit.ShortSHA, commit.Subject)
	switch {
	case m.diffBusy[m.diffSHA]:
		rows = []string{paint(m.spinner()+" diff を取得中...", ansiDim, m.colored)}
	default:
		lines := m.diffCache[m.diffSHA]
		if len(lines) == 0 {
			rows = []string{paint("(diff はありません)", ansiDim, m.colored)}
			break
		}
		start := min(m.diffOffset, max(len(lines)-1, 0))
		end := min(start+m.visibleDiffRows(), len(lines))
		rows = append(rows, lines[start:end]...)
		title = fmt.Sprintf(" diff: %s [%d-%d/%d] %s ", commit.ShortSHA, start+1, end, len(lines), commit.Subject)
	}
	return buildPanelBox(title, rows, width, m.colored)
}

// fillUnknown は結果が得られなかった SHA を「取得中」のまま残さず unknown へ落とす。
func (m *browseModel) fillUnknown() {
	for _, sha := range m.toFetch {
		if _, ok := m.statuses[sha]; !ok {
			m.statuses[sha] = StateUnknown
		}
	}
}

func (m *browseModel) spinnerActive() bool {
	return m.fetching || len(m.detailsLoading) > 0 || len(m.jobDetailBusy) > 0 || len(m.diffBusy) > 0
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
		// View は全行にカーソル溝 2 桁 ("❯ " / "  ") を足すため、折り返し幅は
		// その分を差し引く (差し引かないと全幅の折り返し行が clip され末尾が欠ける)
		Width: max(m.width-2, 0),
		Decor: m.decor,
		PRs:   m.prCache,
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
func (m *browseModel) pageSize() int {
	return max(m.height-1, 1)
}

func (m *browseModel) clampOffset(offset int) int {
	maxOffset := max(len(m.lines())-m.pageSize(), 0)
	return min(max(offset, 0), maxOffset)
}

// ensureCursorVisible はカーソル対象コミットのヘッダー行がビューポート内に入るよう
// offset を調整する。
func (m *browseModel) ensureCursorVisible() {
	lines := m.lines()
	header := 0
	for i, l := range lines {
		if l.Header && l.CommitIdx == m.cursor {
			header = i
			break
		}
	}
	page := m.pageSize()
	if header < m.offset {
		m.offset = header
	}
	if header >= m.offset+page {
		m.offset = header - page + 1
	}
	m.offset = min(max(m.offset, 0), max(len(lines)-page, 0))
}

func (m *browseModel) View() string {
	if m.done {
		// 終了確定後は何も描かない (Alt Screen の復帰で表示は消える)
		return ""
	}
	lines := m.lines()
	page := m.pageSize()
	offset := min(m.offset, max(len(lines)-page, 0))
	end := min(offset+page, len(lines))
	window := make([]string, 0, page)
	for i := offset; i < end; i++ {
		text := lines[i].Text
		if lines[i].Header && lines[i].CommitIdx == m.cursor {
			text = cursorMark(m.colored) + text
		} else {
			text = "  " + text
		}
		window = append(window, clipToWidth(text, m.width))
	}
	// job パネルは対象コミットのヘッダー行直下へ「重ねる」(リスト行を置き換える)。
	// リストの行構成自体は変えないので、開閉で後続行がずれない。
	// 下に収まらない場合はビューポート内へ収まる位置まで引き上げる
	if panel := m.panelLines(); len(panel) > 0 {
		window = overlayBox(window, panel, m.boxAnchor(lines, offset, m.panelSHA)+1, page)
	}
	// diff ポップアップは job パネルよりさらに前面 (openDiff がパネルを閉じるため
	// 実際に同時表示になることはないが、重ね順の契約としてパネルの後に描く)
	if diffBox := m.diffBoxLines(); len(diffBox) > 0 {
		window = overlayBox(window, diffBox, m.boxAnchor(lines, offset, m.diffSHA)+1, page)
	}
	var b strings.Builder
	for _, w := range window {
		b.WriteString(w)
		b.WriteString("\n")
	}
	b.WriteString(m.hintLine())
	return b.String()
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

// overlayBox は box をウィンドウの anchor 位置へ重ねる (リスト行を置き換える)。
// 下に収まらない場合はビューポート内へ収まる位置まで引き上げる。
func overlayBox(window, box []string, anchor, page int) []string {
	start := min(anchor, max(page-len(box), 0))
	start = max(start, 0)
	for i, p := range box {
		pos := start + i
		if pos < len(window) {
			window[pos] = p
		} else if len(window) < page {
			window = append(window, p)
		}
	}
	return window
}

// panelLines は job パネルの描画行 (枠付き)。パネル非表示なら nil。
func (m *browseModel) panelLines() []string {
	if m.panelSHA == "" {
		return nil
	}
	width := m.width
	if width <= 0 {
		width = 80
	}
	var commit *Commit
	for i := range m.commits {
		if m.commits[i].SHA == m.panelSHA {
			commit = &m.commits[i]
			break
		}
	}
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
			if d := formatDuration(jobs[i].Duration); d != "" {
				row += paint(" ("+d+")", ansiDim, m.colored)
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
	box := buildPanelBox(title, rows, width, m.colored)
	if m.detailOpen {
		// 詳細ボックスは job パネルの「子」であることが分かるよう段差を付ける (ユーザー要望)
		for _, line := range m.detailBoxLines(width - len(detailIndent)) {
			box = append(box, detailIndent+line)
		}
	}
	return box
}

// detailIndent は job 詳細ボックスのツリー段差 (job パネルの子であることの視覚表現)。
const detailIndent = "  "

// detailBoxLines は job 詳細 (annotations / ログ tail) の第 2 ポップアップ。
// job パネルの直下へ続けて重ねる。
func (m *browseModel) detailBoxLines(width int) []string {
	name := ""
	if job, ok := m.focusedJob(); ok {
		name = job.Name
	}
	key := m.detailKey()
	var rows []string
	title := " " + name + " "
	switch {
	case m.jobDetailBusy[key]:
		rows = []string{paint(m.spinner()+" 詳細を取得中...", ansiDim, m.colored)}
	default:
		lines := m.jobDetail[key]
		if len(lines) == 0 {
			rows = []string{paint("(詳細なし)", ansiDim, m.colored)}
			break
		}
		start := min(m.detailOffset, max(len(lines)-1, 0))
		end := min(start+m.visibleDetailRows(), len(lines))
		rows = make([]string, 0, end-start)
		for _, l := range lines[start:end] {
			rows = append(rows, decorateDetailLine(l, m.colored))
		}
		title = fmt.Sprintf(" %s [%d-%d/%d] ", name, start+1, end, len(lines))
	}
	return buildPanelBox(title, rows, width, m.colored)
}

// buildPanelBox は枠線付きのパネルを組み立てる。行の実効幅は ANSI を除いて計算する。
func buildPanelBox(title string, rows []string, width int, colored bool) []string {
	if width < 10 {
		width = 10
	}
	inner := width - 4 // "│ " + " │"
	lines := make([]string, 0, len(rows)+2)
	// タイトルは SGR 入りの job 名や commit subject がそのまま載る。ANSI を残すと
	// 幅計算 (Truncate/StringWidth) がずれて罫線が崩れ、タイトル全体の dim 塗りも
	// 途中でリセットされるため、タイトルに限っては ANSI を落とす
	title = runewidth.Truncate(stripANSI(title), width-2, "…")
	top := "┌" + title + strings.Repeat("─", max(width-2-runewidth.StringWidth(title), 0)) + "┐"
	lines = append(lines, paint(top, ansiDim, colored))
	for _, row := range rows {
		content := clipToWidth(row, inner)
		pad := max(inner-runewidth.StringWidth(stripANSI(content)), 0)
		lines = append(lines, paint("│ ", ansiDim, colored)+content+strings.Repeat(" ", pad)+paint(" │", ansiDim, colored))
	}
	lines = append(lines, paint("└"+strings.Repeat("─", width-2)+"┘", ansiDim, colored))
	return lines
}

func cursorMark(colored bool) string {
	return paint("❯ ", ansiBold, colored)
}

func (m *browseModel) hintLine() string {
	hint := "j/k: 移動  Enter: CI job  d: diff  p: PR  y: URL コピー  q: 終了"
	switch {
	case m.diffSHA != "":
		hint = "j/k: スクロール  C-d/C-u: 半頁  g/G: 先頭/末尾  Enter/h/q: 閉じる"
	case m.detailOpen:
		hint = "j/k: スクロール  Enter/h/q: 戻る  o: ブラウザ  y: URL コピー"
	case m.panelSHA != "" && m.panelCursor >= 0:
		hint = "j/k: job 移動  Enter: 詳細ログ  o: ブラウザ  y: URL コピー  h/q: 閉じる"
	case m.panelSHA != "":
		hint = "j: job を選択  y: commit URL  Enter/h/q: 閉じる"
	}
	if m.fetching {
		hint = m.spinner() + " CI 状態を取得中...  " + hint
	}
	if m.notice != "" {
		hint = "⚠ " + m.notice + "  " + hint
	}
	if m.ghErr != nil {
		hint = "⚠ " + firstLine(m.ghErr.Warning()) + "  " + hint
	}
	return clipToWidth(paint(hint, ansiDim, m.colored), m.width)
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
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return m, err
	}
	if fm, ok := final.(*browseModel); ok {
		return fm, nil
	}
	return m, nil
}
