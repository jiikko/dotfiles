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

// pushMsg は git push の実行結果 (b → y 確認後)。glogx の独自機能で、
// 本家 glog (read-only) には無い。
type pushMsg struct{ err error }

// pushPollMsg は push 直後ポーリングの周期タイマー。push した新規コミットは CI job が
// 走り出すまでタイムラグがあり、即 fetch すると「checks なし (StateNone, TTL 5分)」を
// 拾ってネガティブキャッシュ化するため、CI が見えるまで一定間隔で取り直す (ユーザー要望)。
type pushPollMsg struct{}

const (
	pushPollInterval    = 5 * time.Second
	pushPollMaxAttempts = 24 // 5s × 24 = 最長 2 分で諦める (その回の結果は保存しない)
)

// runGitPush はテストで実 push しないための差し替え点。
var runGitPush = func() error {
	out, err := exec.Command("git", "push").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
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
// OS のクリップボードコマンド (pbcopy/xclip) を真実とし、tmux 内では tmux バッファへも
// 積む (tmux paste 用のおまけ・best effort)。本家 glog は load-buffer -w の成功 (exit 0)
// を「システム側にも届いた」とみなすが、-w の実体は OSC52 転送で、外側端末が OSC52 を
// 解釈しなければ exit 0 のままクリップボードに入らない (glogx で実測 2026-07-19)。
var copyToClipboard = func(text string) error {
	if os.Getenv("TMUX") != "" {
		cmd := exec.Command("tmux", "load-buffer", "-w", "-")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run() // 失敗しても OS クリップボードが本命なので無視
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
	cursor         int                 // コミット index
	offset         int                 // ビューポート先頭の行 index
	panelSHA       string              // job パネルを表示中のコミット SHA ("" = パネルなし)
	panelCursor    int                 // パネル内で選択中の job index (-1 = タイトル行にフォーカス)
	detailOpen     bool                // job 詳細 (annotations / ログ tail) ポップアップを表示中か
	detailOffset   int                 // 詳細ポップアップのスクロール位置
	jobDetail      map[string][]string // key (sha/jobIdx) → 詳細行 (メモリ内キャッシュ)
	jobDetailBusy  map[string]bool     // 取得中の key
	prCache        map[string]*PRRef   // sha → 紐づく PR (nil 格納 = 確認済みで PR なし)
	prBusy         map[string]bool     // PR 取得中の sha
	diffSHA        string              // diff ポップアップ表示中の SHA ("" = 非表示)
	diffOffset     int                 // diff ポップアップのスクロール位置
	diffCache      map[string][]string // sha → 整形済み diff 行 (メモリ内キャッシュ)
	diffBusy       map[string]bool     // diff 取得中の sha
	pushConfirm    bool                // b の push 確認中 (y/N)
	pushing        bool                // git push 実行中 (終了以外のキーを無視)
	pushWarn       string              // push できない理由の警告モーダル (何かキーで閉じる)
	pushPoll       map[string]bool     // push 直後ポーリング対象の SHA (CI が見えたら外れる)
	pollAttempts   int                 // push 直後ポーリングの試行回数 (上限で諦める)
	notice         string              // hint 行に出す一時メッセージ (次のキーで消える)
	verbatim       []Line              // git log 実出力の取り込み行 (nil = 自前レンダリング)
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
		// push 直後ポーリング: CI がまだ見えない (none/unknown) SHA は結果を捨てて
		// (statuses から消してスピナーに戻し、fetched からも外してファイルキャッシュへの
		// ネガティブキャッシュ保存を防ぐ) 次の周期で取り直す。CI が見えたら対象から外す
		if len(m.pushPoll) > 0 {
			for sha := range m.pushPoll {
				switch m.statuses[sha] {
				case StatePending, StateSuccess, StateFailure, StateNeutral:
					delete(m.pushPoll, sha) // CI が見えた: 以降は通常のキャッシュ運用
				default:
					delete(m.statuses, sha)
					delete(m.fetched, sha)
				}
			}
			m.invalidateLines()
			if len(m.pushPoll) > 0 && m.pollAttempts < pushPollMaxAttempts {
				return m, tea.Batch(
					tea.Tick(pushPollInterval, func(time.Time) tea.Msg { return pushPollMsg{} }),
					tick())
			}
			m.pushPoll = nil // 上限到達: スピナーは spinnerActive から外れて止まる
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
	case pushPollMsg:
		if len(m.pushPoll) == 0 || m.fetching {
			return m, nil // fetching 中 (別経路の取得が進行) は次の ciResultMsg 側で判定する
		}
		m.pollAttempts++
		targets := slices.Collect(maps.Keys(m.pushPoll))
		m.toFetch = targets
		m.fetching = true
		repo := m.repo
		fetch := func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
			defer cancel()
			batch, ghErr := FetchCIStatuses(ctx, ExecRunner, repo, targets)
			return ciResultMsg{batch: batch, ghErr: ghErr}
		}
		return m, tea.Batch(fetch, tick())
	case pushMsg:
		m.pushing = false
		if msg.err != nil {
			m.notice = "push に失敗しました: " + firstLine(msg.err.Error())
			return m, nil
		}
		m.notice = "push しました"
		// 表示中リスト全体の CI 状態を破棄して取り直す (ユーザー要望 2026-07-19:
		// push で CI が走り出すため、起動時キャッシュ由来の表示は丸ごと古くなる)。
		// statuses から消す → スピナー表示に戻り、toFetch 差し替えで一括取得と
		// 同じ経路 (ciResultMsg) に乗せる。取得結果は fetched 経由で終了時に
		// SaveCache へマージされ、ファイルキャッシュ側も新しい観測で上書きされる
		if !m.hasRepo || len(m.commits) == 0 {
			return m, nil // 再取得先が無いなら破棄もしない (スピナーのまま固まるだけ)
		}
		// push した新規コミット (直前まで unpushed) は CI が見えるまでポーリング対象にする
		m.pushPoll = map[string]bool{}
		m.pollAttempts = 0
		var all []string
		for _, c := range m.commits {
			if m.statuses[c.SHA] == StateUnpushed {
				m.pushPoll[c.SHA] = true
			}
			all = append(all, c.SHA)
			delete(m.statuses, c.SHA)
			delete(m.details, c.SHA)
		}
		m.invalidateLines()
		m.toFetch = all
		m.fetching = true
		repo := m.repo
		fetch := func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
			defer cancel()
			batch, ghErr := FetchCIStatuses(ctx, ExecRunner, repo, all)
			return ciResultMsg{batch: batch, ghErr: ghErr}
		}
		return m, tea.Batch(fetch, tick())
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
	// C-g は即終了: tmux の C-g popup (bind -n C-g) をトグル風に開閉するため
	// (開くキーと同じキーで閉じる)。本家 glog には無い割当。
	if key == "ctrl+c" || key == "ctrl+g" {
		return m.quit()
	}
	// push 警告モーダルは何かキーで閉じる (そのキーは消費して誤操作を防ぐ)
	if m.pushWarn != "" {
		m.pushWarn = ""
		return m, nil
	}
	// push 確認 (b → y/N)。glogx の独自機能。
	if m.pushConfirm {
		if strings.ToLower(key) == "y" {
			m.pushConfirm = false
			m.pushing = true
			return m, tea.Batch(func() tea.Msg { return pushMsg{err: runGitPush()} }, tick())
		}
		m.pushConfirm = false
		return m, nil
	}
	if m.pushing { // push 実行中は終了以外のキーを無視する
		return m, nil
	}
	// emacs 流の水平移動エイリアス (C-n/C-p = ↓/↑ は各ビューで対応済み)。ここで
	// 正規化するので全ビュー (一覧/パネル/詳細/diff) に一括で効く。
	// ⚠️ 本家 glog と異なり C-b は ← の別名ではない (push を C-b → b に変えた名残で未割当)
	if key == "ctrl+f" {
		key = "right"
	}
	// diff ポップアップ表示中はスクロール/閉じる操作だけを受ける (最前面のモーダル)
	if m.diffSHA != "" {
		return m.handleDiffKey(key)
	}
	// b = push (y/N 確認へ)。glogx の独自機能。diff 表示中は b = 半ページ戻るなので、
	// diff のディスパッチより後で拾う (一覧/パネル/詳細では b は未使用)
	if key == "b" {
		return m, m.confirmPush()
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
	case "o":
		return m, m.openCommitURL()
	}
	return m, nil
}

// confirmPush は push 確認 (y/N) に入る。未 push が 1 件も無ければ確認を出さない
// (誤爆防止と「push 済みなのに聞かれる」違和感の回避)。
func (m *browseModel) confirmPush() tea.Cmd {
	if m.unpushedCount() == 0 {
		m.pushWarn = "未 push のコミットはありません" // hint 行でなくモーダルで (ユーザー要望)
		return nil
	}
	m.pushConfirm = true
	return nil
}

// unpushedCount は未 push コミット数 (push 確認モーダルと confirmPush が共用)。
func (m *browseModel) unpushedCount() int {
	n := 0
	for _, st := range m.statuses {
		if st == StateUnpushed {
			n++
		}
	}
	return n
}

// pushBoxLines は push 確認 (y/N)・実行中の中央モーダル。非表示なら nil。
// buildPanelBox を狭い幅で組み、左に空白を足して水平センタリングする
// (垂直は View 側が overlayBox の anchor で中央に置く)。
func (m *browseModel) pushBoxLines() []string {
	if !m.pushConfirm && !m.pushing && m.pushWarn == "" {
		return nil
	}
	width := m.width
	if width <= 0 {
		width = 80
	}
	boxW := min(44, width)
	var rows []string
	switch {
	case m.pushWarn != "":
		rows = []string{
			"⚠ " + m.pushWarn,
			"",
			paint("何かキーを押して閉じる", ansiDim, m.colored),
		}
	case m.pushing:
		rows = []string{m.spinner() + " pushing..."}
	default:
		rows = []string{
			fmt.Sprintf("未 push の %d コミットを push します", m.unpushedCount()),
			"",
			paint("y: 実行   n/Esc: キャンセル", ansiDim, m.colored),
		}
	}
	pad := strings.Repeat(" ", max((width-boxW)/2, 0))
	box := buildPanelBox(" git push ", rows, boxW, m.colored)
	for i := range box {
		box[i] = pad + box[i]
	}
	return box
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
		m.notice = "GitHub の remote が無いため開けません"
		return nil
	}
	return m.openURLCmd(url)
}

func (m *browseModel) copyFocusURL() {
	var url string
	if job, ok := m.focusedJob(); ok {
		url = job.URL
	} else {
		url = m.commitURL()
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

// handleDiffKey は diff ポップアップ表示中のキー操作。中身は pager なので less 流儀:
// Space/Enter は「閉じる」ではなくスクロール (実機で Space/Enter 送りの途中に突然閉じる
// 誤爆報告があり修正 2026-07-19)。末尾に達したら最終行を表示したまま止まる (自動で閉じない)。
// 閉じるのは q / h / Esc / d だけ。
func (m *browseModel) handleDiffKey(key string) (tea.Model, tea.Cmd) {
	rows := m.visibleDiffRows()
	maxOffset := max(len(m.diffCache[m.diffSHA])-rows, 0)
	switch key {
	case "q", "esc", "h", "left", "d":
		m.diffSHA = ""
		m.diffOffset = 0
	case "j", "down", "ctrl+n", "enter":
		m.diffOffset = min(m.diffOffset+1, maxOffset)
	case "k", "up", "ctrl+p":
		m.diffOffset = max(m.diffOffset-1, 0)
	case "ctrl+d", "pgdown", " ", "f":
		m.diffOffset = min(m.diffOffset+rows/2, maxOffset)
	case "ctrl+u", "pgup", "b":
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
	return m.fetching || m.pushing || len(m.pushPoll) > 0 || len(m.detailsLoading) > 0 || len(m.jobDetailBusy) > 0 || len(m.diffBusy) > 0
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
		// カーソルは行頭の溝でなくヘッダー行全体の bg 塗り (cursorLine) で示すため、
		// 折り返し幅は端末の全幅 = git log と左マージンが一致する (ユーザー要望 2026-07-19)
		Width:    m.width,
		Decor:    m.decor,
		PRs:      m.prCache,
		Verbatim: m.verbatim,
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
	pushedFrom := m.pushedBoundary()
	window := make([]string, 0, page)
	for i := offset; i < end; i++ {
		text := lines[i].Text
		// カーソルはヘッダー行全体の bg 塗りで示す (tig/fzf 流)。旧実装の全行 2 桁の
		// カーソル溝 ("❯ " / "  ") は git log と左マージンがずれるため廃止 (ユーザー要望)
		if lines[i].Header && lines[i].CommitIdx == m.cursor {
			window = append(window, m.cursorLine(text))
			continue
		}
		// push 済みエリアは薄いオレンジの bg で面を塗る (境界線と併用の視覚化。
		// ユーザー要望 2026-07-19)。カーソル行はカーソル bg を優先 (上の continue)
		// (境界線行は CommitIdx = boundary-1 なので自然に塗り対象外になる)
		if pushedFrom >= 0 && lines[i].CommitIdx >= pushedFrom {
			window = append(window, m.bgLine(text, ansiPushedBg))
			continue
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
	// push 確認/実行中は画面中央のモーダルを最前面に重ねる (ユーザー要望 2026-07-19:
	// hint 行の [y/N] だけでは気づきにくい)
	if box := m.pushBoxLines(); len(box) > 0 {
		window = overlayBox(window, box, max((page-len(box))/2, 0), page)
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

// cursorLine はカーソル位置のコミットヘッダー行を強調する。色ありでは行全体を暗色 bg で
// 塗る。色なし (NO_COLOR) では bg が使えないため "❯ " 前置に degrade する (その行だけ 2 桁ずれる)。
func (m *browseModel) cursorLine(text string) string {
	if !m.colored {
		return clipToWidth("❯ "+text, m.width)
	}
	return m.bgLine(text, ansiCursorBg)
}

// bgLine は行全体を指定 bg で端末幅まで塗る (行内の SGR リセットで bg が切れないよう、
// リセット直後に bg を張り直す)。カーソル行と push 済みエリアの塗りが共用する。
// 色なしではそのまま返す (bg が使えない)。
func (m *browseModel) bgLine(text, bg string) string {
	if !m.colored {
		return clipToWidth(text, m.width)
	}
	text = clipToWidth(text, m.width)
	pad := max(m.width-runewidth.StringWidth(stripANSI(text)), 0)
	return bg + strings.ReplaceAll(text, ansiReset, ansiReset+bg) +
		strings.Repeat(" ", pad) + ansiReset
}

// pushedBoundary は push 済み先頭のコミット index (-1 = 塗らない)。未 push と push 済みが
// 両方あるときだけ有効 (insertPushBoundary と同じ条件。全部 push 済みで全面が
// 塗られるのはノイズなので、対比が意味を持つときだけ面を出す)。
func (m *browseModel) pushedBoundary() int {
	for i, c := range m.commits {
		if stateFor(m.statuses, c.SHA) != StateUnpushed {
			if i > 0 {
				return i
			}
			return -1
		}
	}
	return -1
}

func (m *browseModel) hintLine() string {
	hint := "j/k: 移動  Enter: CI job  d: diff  o: ブラウザ  p: PR  y: URL コピー  b: push  q: 終了"
	switch {
	case m.pushConfirm:
		hint = "push しますか? [y/N]"
	case m.pushing:
		hint = m.spinner() + " pushing..."
	case m.diffSHA != "":
		hint = "j/k/Space: スクロール  g/G: 先頭/末尾  q/h: 閉じる"
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
