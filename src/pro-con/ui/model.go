// Package ui は pro-con の TUI。状態は backend が持ち、ここは Snapshot を描くことと
// Command を送ることだけをする (画面を閉じても・落ちても何も失わない。415 要件 14)。
package ui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"tuikit/anim"
	"tuikit/confirm"
	"tuikit/editor"
	"tuikit/lineedit"
	"tuikit/listnav"

	"pro-con/agents"
	"pro-con/backend"
	"pro-con/card"
)

// TickInterval は Poll を呼ぶ間隔 (実時間)。fake では 1 回の Poll が模擬時間の 1 分に当たる。
const TickInterval = time.Second

type mode int

const (
	modeBoard mode = iota
	modeInput
	modeConfirm // 破壊的な操作の y/N 確認 (docs/glogx-ui-guide.md §4)
)

type inputKind int

const (
	inputAnswer inputKind = iota
	inputOrder
	inputBtw
	inputNew
	inputIssue // issue の一覧で選んだものへの補足 (空でよい。picker.go)
)

type tickMsg struct{}

type attachDoneMsg struct {
	cardID string
	err    error
}

type Model struct {
	be     backend.Backend
	snap   backend.Snapshot
	width  int
	height int

	// repos は config から列挙した repo。タブは global + 「ここに在り、カードも在る repo」。
	repos []backend.Repo
	// tab は選んでいる repo 名 ("" = global)。位置ではなく名前で持つ (タブの増減で別の repo へずれない)。
	tab string

	// フォーカスはレーン (col) と、そのレーンのカード (selected) の 2 段で持つ。カードの無いレーンにも当たる
	// (selected = "")。カードは位置ではなく ID で持つ (安定キー)。カードが列を移ったらレーンのフォーカスも付いていく。
	col        int
	selected   string
	showDetail bool

	mode        mode
	line        lineedit.Line   // 入力欄 (編集キーは tuikit/lineedit。docs/glogx-ui-guide.md「入力欄の編集キー」)
	pending     backend.Command // modeConfirm で確認している操作
	confirmText string          // modeConfirm の確認の行に出す文
	inputKind   inputKind
	orderKind   card.OrderKind

	flash  string
	sticky string // 消すまで残す通知 (捨てた書きかけの文など。flash は次の通知で消えるので置かない)。ボードの esc で消す

	// カードの移動の演出 (motion.go)。now は時計 (テストで差し替える)
	now       func() time.Time
	prevSlots map[string]slot
	moves     map[string]*move
	slides    map[panel]*slide // 下端の板の開閉の演出 (slide.go)
	// カードの詳細の引き出し (drawer.go)。drawerCard は閉じる途中も残す (逆再生で本文が見えている必要がある)
	cursor     cursorGlide // 選択中のカードを囲む枠 (cursor.go)
	quitAsk    bool        // 終了の確認ダイアログを出している (quit.go)
	legend     bool        // レーンの意味の表を出している (legend.go)
	lane       laneFade    // 選んでいるレーンの枠の色の移り変わり (lanefade.go)
	drawer     anim.Transition
	drawerCard string
	pager      listnav.Pager
	framing    bool // frame の tick が回っているか (二重に回さない)

	picker picker // issue の一覧から依頼する画面 (picker.go)

	up       *upgrader     // ライブアップグレード (upgrade.go)。nil なら無効
	children *atomic.Int64 // 裏で外部コマンドを起こしている処理の数 (exec の前に 0 を待つ。upgrade.go の child)

	copy       func(string) error     // クリップボードへ入れる (既定は pbcopy。テストは差し替える)
	openEditor func(string) *exec.Cmd // ファイルを開くエディタのコマンド (既定は tuikit/editor。テストは差し替える)

	// Claude Code の session の一覧 (sessions.go)
	listSessions func(context.Context) ([]agents.Session, error)
	sessions     []agents.Session
	sessErr      string
	sessFetched  bool
	showSessions bool
}

// New は repos (config から列挙した repo) をタブの候補にして画面を作る。nil なら global だけ。
func New(be backend.Backend, repos []backend.Repo) *Model {
	m := &Model{be: be, repos: repos, width: 120, height: 40, now: time.Now, slides: map[panel]*slide{}, copy: pbcopy, children: &atomic.Int64{}, openEditor: func(p string) *exec.Cmd { return editor.Command(p, nil) },
		listSessions: func(ctx context.Context) ([]agents.Session, error) { return agents.List(ctx, agents.ExecRunner) }}
	m.setSnap(be.Poll())
	m.focusFirst()
	m.resetSlots()
	return m
}

// Notify は起動時の警告など、画面の外から通知行へ文面を足す (上書きしない: 引き継ぎで捨てた書きかけの文などを消さない)。
func (m *Model) Notify(s string) {
	if m.flash != "" {
		s = m.flash + " / " + s
	}
	m.flash = s
}

func tick() tea.Cmd { return tea.Tick(TickInterval, func(time.Time) tea.Msg { return tickMsg{} }) }

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tick(), m.fetchSessions()}
	if m.up != nil {
		cmds = append(cmds, m.checkUpgrade())
	}
	return tea.Batch(cmds...)
}

func (m *Model) Update(msg tea.Msg) (_ tea.Model, cmd tea.Cmd) {
	defer func() { // 選択が動いたら (キーでも、カードの移動でも) 枠を滑らせる
		c := m.trackCursor()
		if m.trackLane() {
			c = tea.Batch(c, m.startFrames())
		}
		if c != nil {
			cmd = tea.Batch(cmd, c)
		}
	}()
	switch msg := msg.(type) {
	case tickMsg:
		m.setSnap(m.be.Poll())
		tab := m.tab
		m.ensureTab()
		m.ensureSelection()
		if m.tab != tab { // タブが消えて global へ戻った: 配置が丸ごと変わるので演出しない
			m.resetSlots()
			return m, tick()
		}
		return m, tea.Batch(tick(), m.trackMoves())
	case frameMsg:
		return m, m.onFrame()
	case editorDoneMsg:
		m.onEditorDone(msg)
		return m, nil
	case upgradeTickMsg:
		return m, m.checkUpgrade()
	case upgradeCheckMsg:
		return m, m.onUpgradeCheck(msg)
	case sessionsMsg:
		return m, m.onSessions(msg)
	case sessionsTickMsg:
		return m, m.fetchSessions()
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case attachDoneMsg:
		if msg.err != nil {
			m.flash = "attach が失敗した: " + msg.err.Error()
		} else {
			m.flash = msg.cardID + " の session から戻った (session は動き続けている)"
		}
		return m, nil
	case tea.PasteMsg:
		// ペーストは入力欄にだけ入れる。ボードではキー操作として解釈しない
		if m.mode == modeInput {
			m.line.Insert(msg.Content)
		}
		return m, nil
	case tea.KeyPressMsg:
		if m.quitAsk {
			return m, m.handleQuitKey(msg.String())
		}
		if m.legend {
			return m, m.handleLegendKey(msg.String())
		}
		switch m.mode {
		case modeInput:
			cmd := m.handleInputKey(msg)
			return m, tea.Batch(cmd, m.trackMoves()) // 回答などで列が変わったら演出する
		case modeConfirm:
			cmd := m.handleConfirmKey(msg)
			return m, tea.Batch(cmd, m.trackMoves())
		case modeBoard:
		}
		if m.picker.open {
			return m, m.handlePickerKey(msg)
		}
		before := m.panelState()
		cmd := m.handleBoardKey(msg)
		return m, tea.Batch(cmd, m.trackPanels(before))
	}
	return m, nil
}

// tabs は global ("") と、config に在り、かつカードが 1 枚以上ある repo 名。
// config に在ってもカードの無い repo は出さない (~/src の下は数十 repo あり、タブが埋まる)。
func (m *Model) tabs() []string {
	has := map[string]bool{}
	for _, c := range m.snap.Cards {
		has[c.Repo] = true
	}
	out := []string{""}
	for _, r := range m.repos {
		if has[r.Name] {
			out = append(out, r.Name)
		}
	}
	return out
}

// tabRepo は選んでいるタブの repo (global ならゼロ値)。新しい依頼のスコープになる。
func (m *Model) tabRepo() backend.Repo {
	for _, r := range m.repos {
		if r.Name == m.tab && m.tab != "" {
			return r
		}
	}
	return backend.Repo{}
}

// ensureTab は選んでいる repo のカードが無くなってタブが消えたら global へ戻す。
func (m *Model) ensureTab() {
	for _, t := range m.tabs() {
		if t == m.tab {
			return
		}
	}
	m.tab = ""
}

func (m *Model) moveTab(delta int) {
	ts := m.tabs()
	cur := 0
	for i, t := range ts {
		if t == m.tab {
			cur = i
		}
	}
	m.tab = ts[(cur+delta+len(ts))%len(ts)]
	m.focusFirst()
	m.resetSlots() // タブの切り替えはカードが動いたのではない
}

// visible は選んでいるタブに属するカード。global は全部 (config の外の repo のカードも含む)。
// setSnap は backend の Snapshot を画面の状態にする。片付けたカード (Archived) はここで落とす
// (タブの枚数・選択・カンバンのどれにも出さない。画面の読み手ごとに除外を書くと、1 か所の漏れで片付けたカードが戻る)。
func (m *Model) setSnap(s backend.Snapshot) {
	cards := make([]card.Card, 0, len(s.Cards))
	for _, c := range s.Cards {
		if !c.Archived {
			cards = append(cards, c)
		}
	}
	s.Cards = cards
	m.snap = s
	m.dropVanishedDrawer()
}

func (m *Model) visible() []card.Card {
	if m.tab == "" {
		return m.snap.Cards
	}
	var out []card.Card
	for _, c := range m.snap.Cards {
		if c.Repo == m.tab {
			out = append(out, c)
		}
	}
	return out
}

// columns は選んでいるタブのカードをカンバンの列に分ける。列の中は「その列に入った順」(Since、同時なら ID)。
// 移ってきたカードは必ず列の末尾に着地するので、既存のカードの位置は動かない (Snapshot の並び順のままだと、
// 移ってきたカードが途中に割り込んで既存のカードが一斉にずれる)。
func (m *Model) columns() [][]card.Card {
	cols := make([][]card.Card, len(card.Columns))
	for _, c := range m.visible() {
		for i, st := range card.Columns {
			if c.State == st {
				cols[i] = append(cols[i], c)
			}
		}
	}
	for _, cs := range cols {
		slices.SortStableFunc(cs, func(a, b card.Card) int {
			if c := a.Since.Compare(b.Since); c != 0 {
				return c
			}
			return strings.Compare(a.ID, b.ID)
		})
	}
	return cols
}

// position は選択中のカードの (列, 行)。見つからなければ ok=false。
func (m *Model) position() (col, row int, ok bool) { return m.positionOf(m.selected) }

func (m *Model) positionOf(id string) (col, row int, ok bool) {
	for i, cs := range m.columns() {
		for j, c := range cs {
			if c.ID == id {
				return i, j, true
			}
		}
	}
	return 0, 0, false
}

// ensureSelection は状態が変わった後にフォーカスを直す。選んでいたカードが別のレーンへ移ったらレーンも付いていく。
// カードが消えたら同じレーンの先頭のカード (無ければレーンだけにフォーカス)。
func (m *Model) ensureSelection() {
	if col, _, ok := m.position(); ok {
		m.col = col
		return
	}
	m.focusLane(m.col, 0)
}

// focusFirst はカードのある最初のレーンにフォーカスする (起動時とタブの切り替え)。どのレーンも空なら先頭のレーン。
func (m *Model) focusFirst() {
	for i, cs := range m.columns() {
		if len(cs) > 0 {
			m.focusLane(i, 0)
			return
		}
	}
	m.focusLane(0, 0)
}

// focusLane はレーン i にフォーカスし、row 番目 (はみ出したら末尾) のカードを選ぶ。空のレーンならカードは選ばない。
func (m *Model) focusLane(i, row int) {
	cols := m.columns()
	m.col = max(0, min(i, len(cols)-1))
	m.selected = ""
	if cs := cols[m.col]; len(cs) > 0 {
		m.selected = cs[max(0, min(row, len(cs)-1))].ID
	}
}

func (m *Model) selectedCard() (card.Card, bool) {
	for _, c := range m.snap.Cards {
		if c.ID == m.selected {
			return c, true
		}
	}
	return card.Card{}, false
}

// moveCol は左右のレーンへ移る。空のレーンにも止まる (レーンにフォーカスが当たる)。行の位置はなるべく保つ。
func (m *Model) moveCol(delta int) { m.jumpCol(m.col + delta) }

// jumpCol はレーン i (0 始まり) へ移る (h / l と 1〜6)。
func (m *Model) jumpCol(i int) {
	row := 0
	if _, r, ok := m.position(); ok {
		row = r
	}
	m.focusLane(i, row)
}

// moveRow はレーンの中で delta 枚動く。端で止める (半ページ・先頭・末尾の移動も同じ関数で端に寄せる)。
func (m *Model) moveRow(delta int) {
	col, row, ok := m.position()
	if !ok {
		return // 空のレーン
	}
	cs := m.columns()[col]
	m.selected = cs[max(0, min(row+delta, len(cs)-1))].ID
}

func (m *Model) handleBoardKey(k tea.KeyPressMsg) tea.Cmd {
	if m.showDetail {
		if cmd, handled := m.handleDrawerKey(k.String()); handled {
			return cmd
		}
	}
	switch k.String() {
	case "ctrl+c":
		return m.requestQuit()
	case "ctrl+r": // 新版へ切り替える (ライブアップグレード。新版があるときだけ)
		return m.requestUpgrade()
	case "q":
		// q は「今の板を 1 段戻る」(docs/glogx-ui-guide.md §1)。開いている板が無ければ終了
		if !m.closeTop() {
			return m.requestQuit()
		}
	case "esc":
		if !m.closeTop() {
			m.sticky = "" // 閉じる板が無いときの esc は、残しておいた通知を消す
		}
	case "tab":
		m.moveTab(1)
	case "shift+tab":
		m.moveTab(-1)
	case "left", "h":
		m.moveCol(-1)
	case "right", "l", "ctrl+f": // ctrl+f は → の別名 (docs/glogx-ui-guide.md の emacs 層。ctrl+b は ← にしない)
		m.moveCol(1)
	case "enter":
		m.openDrawer()
		return m.startFrames()
	case "a":
		return m.attach()
	case "r":
		c, ok := m.selectedCard()
		if !ok || !c.Answerable() {
			m.flash = "回答できるのは質問待ちのカードだけ"
			return nil
		}
		m.startInput(inputAnswer)
	case "+": // 追加オーダー (o はガイドで「ブラウザで開く」なので使わない)
		if _, ok := m.selectedCard(); ok {
			m.orderKind = card.OrderAppend
			m.startInput(inputOrder)
		}
	case "?": // レーンの意味の表 (legend.go)
		m.legend = true
	case "w": // btw = 「今どうなってる?」(what's up。b は移動の語彙で半ページ上なので使わない)
		if _, ok := m.selectedCard(); ok {
			m.startInput(inputBtw)
		}
	case "n":
		m.startInput(inputNew)
	case "y":
		m.yankPath()
	case "Y":
		m.yank()
	case "e":
		return m.openIssue()
	case "i": // issue の一覧から選んで依頼する (docs/glogx-ui-guide.md の i = issues の板)
		m.loadPicker()
	case "s":
		m.showSessions = !m.showSessions
	case "x": // 完了のレーンを片付ける (glogx の X = 捨てる の弱い版。y/N 確認を挟む)
		m.askClearDone()
	default:
		if i, ok := laneKey(k.String()); ok {
			m.jumpCol(i)
			return nil
		}
		m.moveByMotion(listnav.MotionOf(k.String()))
	}
	return nil
}

// laneKey は 1〜(レーンの数) の数字キーをレーンの添字 (0 始まり) にする。数字はレーンの見出しの先頭に出している。
func laneKey(key string) (int, bool) {
	if len(key) != 1 || key[0] < '1' || key[0] > '9' {
		return 0, false
	}
	i := int(key[0] - '1')
	return i, i < len(card.Columns)
}

// moveByMotion は上下の移動を tuikit の語彙 (listnav.MotionOf) で受ける。語彙は glogx と同じ
// (j/k/ctrl+n/ctrl+p/↑↓ = 1 枚、ctrl+d/ctrl+u/space/f/pgdn/pgup = 半ページ、g/G/home/end = 先頭・末尾)。
// 🚨 画面固有の動作キーは handleBoardKey の switch で先に捌いているので、ここへは来ない。
// 動作キーに移動の語彙 (b / f / space / g …) を使わないこと (使うとその移動が効かなくなる)。
func (m *Model) moveByMotion(mo listnav.Motion) {
	half := listnav.Half(m.shownCards())
	switch mo {
	case listnav.Down:
		m.moveRow(1)
	case listnav.Up:
		m.moveRow(-1)
	case listnav.HalfDown:
		m.moveRow(half)
	case listnav.HalfUp:
		m.moveRow(-half)
	case listnav.Top:
		m.moveRow(-len(m.snap.Cards))
	case listnav.Bottom:
		m.moveRow(len(m.snap.Cards))
	case listnav.None:
	}
}

// closeTop は開いている板を手前から 1 つ閉じる (session の一覧 → 詳細)。閉じたら true。
func (m *Model) closeTop() bool {
	switch {
	case m.showSessions:
		m.showSessions = false
	case m.showDetail:
		m.closeDrawer()
	default:
		return false
	}
	return true
}

func (m *Model) startInput(k inputKind) {
	m.mode = modeInput
	m.inputKind = k
	m.line.Reset()
	m.flash = ""
}

// handleInputKey は入力中のキー。Enter / Esc / Tab / ctrl+c 以外は編集キーとして lineedit に渡す
// (入力中は ctrl+b / ctrl+f / ctrl+u / ctrl+d も編集の意味。一覧の移動の語彙ではない)。
func (m *Model) handleInputKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.mode = modeBoard
		m.flash = "入力を取り消した"
	case "enter":
		m.submit()
	case "tab":
		if m.inputKind == inputOrder {
			m.orderKind = (m.orderKind + 1) % 3
		}
	case "ctrl+c":
		return m.requestQuit()
	default:
		m.line.Key(k.String(), k.Text)
	}
	return nil
}

// handleConfirmKey は y/N 確認。y と Enter だけが実行で、知らないキーはすべて取り消し (docs/glogx-ui-guide.md §4)。
// askConfirm は cmd を y/N 確認に載せる (docs/glogx-ui-guide.md §4)。question は確認の行に出す文。
func (m *Model) askConfirm(cmd backend.Command, question string) {
	m.pending = cmd
	m.confirmText = question
	m.mode = modeConfirm
}

// askClearDone は今のタブの完了のカードを片付けるかを確かめる。片付けるものが無ければ確認を出さない。
// doneInTab は今のタブの完了のカードの枚数 (x で片付ける対象)。
func (m *Model) doneInTab() int {
	n := 0
	for _, c := range m.visible() {
		if c.State == card.Done {
			n++
		}
	}
	return n
}

func (m *Model) askClearDone() {
	n := m.doneInTab()
	if n == 0 {
		m.flash = "片付ける完了のカードが無い"
		return
	}
	scope := "全 repo"
	if m.tab != "" {
		scope = m.tab
	}
	m.askConfirm(backend.ClearDone{Repo: m.tab},
		fmt.Sprintf("完了のカード %d 枚 (%s) をボードから片付けます (記録は残ります)。よいですか? [y/N]", n, scope))
}

func (m *Model) handleConfirmKey(k tea.KeyPressMsg) tea.Cmd {
	switch key := k.String(); {
	case key == "ctrl+c":
		return m.requestQuit()
	case confirm.IsYesStrict(key):
		m.apply(m.pending)
	default:
		m.flash = "取り消した (実行していない)"
		m.mode = modeBoard
		m.line.Reset()
	}
	m.pending = nil
	return nil
}

func (m *Model) submit() {
	text := m.line.String()
	var cmd backend.Command
	switch m.inputKind {
	case inputAnswer:
		cmd = backend.Answer{CardID: m.selected, Text: text, From: "人間"}
	case inputOrder:
		cmd = backend.AddOrder{CardID: m.selected, Kind: m.orderKind, Text: text}
	case inputBtw:
		cmd = backend.Btw{CardID: m.selected, Question: text}
	case inputNew:
		cmd = backend.NewRequest{Repo: m.tabRepo(), Text: text}
	case inputIssue:
		cmd = backend.NewRequest{Repo: m.picker.repo, Text: text, Issue: m.picker.target}
	}
	// 方針変更は PG を止めて指示を差し替える (途中の作業を止める) ので、送る前に確認する
	if o, ok := cmd.(backend.AddOrder); ok && o.Kind == card.OrderRedirect && text != "" {
		m.askConfirm(cmd, "方針変更: PG を止めて、指示を差し替えて再開します。よいですか? [y/N]")
		return
	}
	m.apply(cmd)
}

// apply は操作を backend へ送る。空の本文で拒否されたら入力欄を開いたままにする (書き直させる)。
func (m *Model) apply(cmd backend.Command) {
	res, err := m.be.Apply(cmd)
	if err != nil {
		m.flash = "失敗: " + err.Error()
		if errors.Is(err, backend.ErrEmptyText) {
			m.mode = modeInput
			return // 入力欄を開いたまま書き直させる
		}
	} else {
		m.flash = res
	}
	m.mode = modeBoard
	m.line.Reset()
	m.setSnap(m.be.Snapshot())
	m.ensureTab()
	m.ensureSelection()
}

func (m *Model) attach() tea.Cmd {
	c, ok := m.selectedCard()
	if !ok {
		return nil
	}
	cmd, err := m.be.AttachCommand(c.Session)
	if err != nil {
		m.flash = "attach できない: " + err.Error()
		return nil
	}
	id := c.ID
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return attachDoneMsg{cardID: id, err: err} })
}
