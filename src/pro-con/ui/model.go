// Package ui は pro-con の TUI。状態は backend が持ち、ここは Snapshot を描くことと
// Command を送ることだけをする (画面を閉じても・落ちても何も失わない。415 要件 14)。
package ui

import (
	"errors"
	"fmt"
	"iter"
	"os/exec"
	"slices"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"tuikit/anim"
	"tuikit/confirm"
	"tuikit/editor"
	"tuikit/layout"
	"tuikit/lineedit"
	"tuikit/listnav"
	"tuikit/toast"

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
	modeForm    // 選択肢つきの質問の回答フォーム (answerform.go。issue 493)
)

type inputKind int

const (
	inputAnswer inputKind = iota
	inputOrder
	inputBtw
	inputNew
	inputIssue // issue の一覧で選んだものへの補足 (空でよい。picker.go)
	inputQuit  // 終了 (quit と打って enter したときだけ閉じる。quit.go)
)

type tickMsg struct{}

// changedMsg は backend が状態の変化を知らせた (backend.Notifier)。
type changedMsg struct{}

type attachDoneMsg struct {
	cardID, session string
	from            time.Time // 端末を明け渡した時刻 (この後に打った指示をカードに残す。issue 428)
	err             error
}

// attachRecordedMsg は attach の間の指示をカードに残すよう頼んだ結果 (backend.AttachRecorder)。
type attachRecordedMsg struct {
	cardID string
	n      int
	err    error
}

type Model struct {
	be     backend.Backend
	snap   backend.Snapshot
	width  int
	height int
	// frameSink は画面の中継の受け口 (relay.go。nil なら中継しない)
	frameSink FrameSink

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
	inputRow    int             // 最後の render で入力欄を置いた行 (view.go の caret)
	form        answerForm      // modeForm で開いている回答フォーム
	line        lineedit.Line   // 入力欄 (編集キーは tuikit/lineedit。docs/glogx-ui-guide.md「入力欄の編集キー」)
	pending     backend.Command // modeConfirm で確認している操作
	confirmText string          // modeConfirm の確認の行に出す文
	inputKind   inputKind
	orderKind   card.OrderKind

	toasts toast.Stack // 操作の結果の通知 (toast.go)
	sticky string      // 消すまで残す通知 (捨てた書きかけの文など。flash は次の通知で消えるので置かない)。ボードの esc で消す

	// カードの移動の演出 (motion.go)。now は時計 (テストで差し替える)
	now       func() time.Time
	prevSlots map[string]slot
	moves     map[string]*move
	// カードの詳細の引き出し (drawer.go)。drawerCard は閉じる途中も残す (逆再生で本文が見えている必要がある)
	cursor     cursorGlide // 選択中のカードを囲む枠 (cursor.go)
	bump       bump        // 選択が端でぶつかったときのレーンの揺れ (bump.go)
	tops       map[int]int // レーンごとの先頭 (何枚目から見せるか。lanescroll.go)
	stopping   bool        // 終了のために backend (dispatcher と PG) を止めている最中 (quit.go)
	stopErr    error       // 止めきれなかった理由 (終了後に main が出す)
	attaching  bool        // attach の照合を裏で待っている
	legend     bool        // レーンの意味の表を出している (legend.go)
	legendOff  int         // 表が画面より長いときの送り (legend.go)
	lane       laneFade    // 選んでいるレーンの枠の色の移り変わり (lanefade.go)
	drawer     anim.Transition
	drawerCard string
	pager      listnav.Pager
	act        activityView // 引き出しに出している PG の活動 (activity.go)
	framing    bool         // frame の tick が回っているか (二重に回さない)
	spinning   bool         // 処理中の印の tick が回っているか (spinner.go。二重に回さない)

	picker picker // issue の一覧から依頼する画面 (picker.go)

	up       *upgrader     // ライブアップグレード (upgrade.go)。nil なら無効
	children *atomic.Int64 // 裏で外部コマンドを起こしている処理の数 (exec の前に 0 を待つ。upgrade.go の child)

	copy       func(string) error     // クリップボードへ入れる (既定は pbcopy。テストは差し替える)
	openEditor func(string) *exec.Cmd // ファイルを開くエディタのコマンド (既定は tuikit/editor。テストは差し替える)
	// execProcess は端末を明け渡して外のコマンドを走らせる (既定は tea.ExecProcess。テストは戻りの知らせを取り出すために差し替える)
	execProcess func(*exec.Cmd, tea.ExecCallback) tea.Cmd
	openFiles   func([]string) error // 添付を外のアプリで開く (既定は open。テストは差し替える。attachments.go)
	attachTexts map[string][]string  // 文字の添付の中身 (パスごとに 1 度だけ読む。attachments.go)

	set settings // s で開く設定画面 (settings.go)
}

// New は repos (config から列挙した repo) をタブの候補にして画面を作る。nil なら global だけ。
func New(be backend.Backend, repos []backend.Repo) *Model {
	m := &Model{be: be, repos: repos, width: 120, height: 40, now: time.Now, copy: pbcopy, children: &atomic.Int64{}, openEditor: func(p string) *exec.Cmd { return editor.Command(p, nil) }, execProcess: tea.ExecProcess, openFiles: openWithSystem}
	m.toasts = toast.Stack{Shadow: layout.ShadowNearBlack} // 落ち影は他の板と同じ近黒 (glogx と同じ)
	m.setSnap(be.Poll())
	m.focusFirst()
	m.resetSlots()
	return m
}

// Notify は起動時の警告など、画面の外から通知を足す。操作の結果 (flash) と違って時間では消さず、esc で消すまで残す
// (上書きしない: 引き継ぎで捨てた書きかけの文などを消さない)。
func (m *Model) Notify(s string) {
	if m.sticky != "" {
		s = m.sticky + " / " + s
	}
	m.sticky = s
}

// poll は backend の今の状態を画面に取り込む。
func (m *Model) poll() tea.Cmd {
	m.setSnap(m.be.Poll())
	m.showRejected()
	tab := m.tab
	m.ensureTab()
	m.ensureSelection()
	if m.tab != tab { // タブが消えて global へ戻った: 配置が丸ごと変わるので演出しない
		m.resetSlots()
		return nil
	}
	return m.trackMoves()
}

func tick() tea.Cmd { return tea.Tick(TickInterval, func(time.Time) tea.Msg { return tickMsg{} }) }

// waitChanged は backend の変化の知らせを待つ (Notifier でなければ nil)。
func (m *Model) waitChanged() tea.Cmd {
	n, ok := m.be.(backend.Notifier)
	if !ok {
		return nil
	}
	ch := n.Changed()
	return func() tea.Msg {
		<-ch
		return changedMsg{}
	}
}

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tick(), m.waitChanged()}
	if m.set.open { // ライブアップグレードで開いたまま引き継いだ設定画面は、見る所を読み直す
		cmds = append(cmds, m.fetchProcs(), m.measureDisk())
	}
	if m.up != nil {
		cmds = append(cmds, m.checkUpgrade())
	}
	return tea.Batch(cmds...)
}

func (m *Model) Update(msg tea.Msg) (_ tea.Model, cmd tea.Cmd) {
	defer func() { // 選択が動いたら (キーでも、カードの移動でも) 枠を滑らせる
		m.followSelection() // 枠の行き先はレーンの先頭で決まるので、枠より先に
		c := tea.Batch(m.trackCursor(), m.trackSpin(), m.trackActivity())
		if m.trackLane() {
			c = tea.Batch(c, m.startFrames())
		}
		if c != nil {
			cmd = tea.Batch(cmd, c)
		}
	}()
	switch msg := msg.(type) {
	case tickMsg:
		return m, tea.Batch(tick(), m.poll(), m.fetchActivity())
	case changedMsg:
		return m, tea.Batch(m.waitChanged(), m.poll(), m.fetchActivity())
	case activityMsg:
		m.onActivity(msg)
		return m, nil
	case procsMsg:
		m.set.procs, m.set.procsErr, m.set.procsAt, m.set.procsLoading = msg.rows, msg.err, msg.at, false
		m.set.cursor = min(m.set.cursor, max(len(m.settingsSelectable())-1, 0))
		return m, nil
	case diskMsg:
		m.set.disk, m.set.diskErr, m.set.diskLoading = msg.u, msg.err, false
		m.set.cursor = min(m.set.cursor, max(len(m.settingsSelectable())-1, 0))
		return m, nil
	case frameMsg:
		return m, m.onFrame()
	case toast.Msg:
		m.toasts.StartLeaving(msg) // 静止が明けた: 引っ込む演出へ (世代が合うときだけ)
		return m, m.startFrames()
	case spinMsg:
		return m, m.onSpin()
	case editorDoneMsg:
		m.onEditorDone(msg)
		return m, nil
	case openedMsg:
		m.onOpened(msg)
		return m, nil
	case upgradeTickMsg:
		return m, m.checkUpgrade()
	case upgradeCheckMsg:
		return m, m.onUpgradeCheck(msg)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case attachReadyMsg:
		m.attaching = false
		if msg.err != nil {
			m.fail("attach できない: " + msg.err.Error())
			return m, nil
		}
		// 照合を待つ間に入力欄を開いた・終了の確認を出した・別のカードを選んだなら、端末を明け渡さない (書いている途中の画面を奪わない)。
		// 引き出し・? の表を開いているだけなら明け渡す (戻れば同じ画面に戻る)
		if m.mode != modeBoard || m.selected != msg.cardID {
			m.info("attach を取りやめた (待っている間に画面が変わった)")
			return m, nil
		}
		done := attachDoneMsg{cardID: msg.cardID, session: msg.session, from: m.now()}
		return m, m.execProcess(msg.cmd, func(err error) tea.Msg { done.err = err; return done })
	case attachDoneMsg:
		if msg.err != nil {
			m.fail("attach が失敗した: " + msg.err.Error())
		} else {
			m.done(msg.cardID + " の session から戻った (session は動き続けている)")
		}
		// 失敗で戻っても、それまでに打った指示はある。transcript を全部読むので裏で頼む
		rec, ok := m.be.(backend.AttachRecorder)
		if !ok {
			return m, nil
		}
		to := m.now()
		return m, m.child(func() tea.Msg {
			n, err := rec.RecordAttach(msg.cardID, msg.session, msg.from, to)
			return attachRecordedMsg{cardID: msg.cardID, n: n, err: err}
		})
	case attachRecordedMsg:
		if msg.err != nil { // 残せなかったことは消さずに出す (知らずにいると、カードを見ても指示が分からない)
			m.Notify(msg.cardID + ": attach の間の指示をカードに残せなかった: " + msg.err.Error())
		} else if msg.n > 0 {
			m.done(fmt.Sprintf("%s: attach の間の指示 %d 件をカードの履歴へ送った", msg.cardID, msg.n))
		}
		return m, nil
	case tea.PasteMsg:
		// ペーストは入力欄にだけ入れる。ボードではキー操作として解釈しない
		switch m.mode {
		case modeInput:
			m.line.Insert(msg.Content)
		case modeForm:
			m.pasteForm(msg.Content)
		case modeBoard, modeConfirm:
		}
		return m, nil
	case stopDoneMsg:
		m.stopping, m.stopErr = false, msg.err
		return m, tea.Quit
	case tea.KeyPressMsg:
		if m.stopping { // 止め終えるまで待つ。ctrl+c だけは待たずに閉じる (止める処理は別プロセスの pro-con dispatcher --stop が続ける)
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			return m, nil
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
		case modeForm:
			cmd := m.handleFormKey(msg)
			return m, tea.Batch(cmd, m.trackMoves())
		case modeBoard:
		}
		if m.set.open {
			return m, m.handleSettingsKey(msg.String())
		}
		if m.picker.open {
			return m, m.handlePickerKey(msg)
		}
		return m, m.handleBoardKey(msg)
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
// showRejected は、この画面が置いた依頼を dispatcher が除けた理由を出す (ほかの画面の依頼は出さない。issue 481)。
func (m *Model) showRejected() {
	rr, ok := m.be.(backend.RejectReader)
	if !ok {
		return
	}
	for _, r := range rr.TakeRejected() {
		what := r.Kind
		if r.CardID != "" {
			what = r.CardID + " への " + r.Kind
		}
		m.refuse("打った依頼 (" + what + ") は dispatcher が除けた: " + r.Why)
	}
}

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

// カードは複製せず m.snap.Cards の中を指して渡す (描くたびに呼ぶので、1 枚 960 バイトを複製すると GC が回り続けた。issue 494)。
// 🚨 指す先は今の m.snap.Cards なので、setSnap をまたいで持ち越さず、書き換えない
func (m *Model) visible() iter.Seq[*card.Card] {
	return func(yield func(*card.Card) bool) {
		for i := range m.snap.Cards {
			if c := &m.snap.Cards[i]; (m.tab == "" || c.Repo == m.tab) && !yield(c) {
				return
			}
		}
	}
}

// columns は選んでいるタブのカードをカンバンの列に分ける。列の中はレーンの並び (card.LaneCompare: 上ほど優先。K / J で入れ替えて
// いなければ、その列に入った順)。移ってきたカードは必ず列の末尾に着地するので、既存のカードの位置は動かない (Snapshot の並び順のままだと、
// 移ってきたカードが途中に割り込んで既存のカードが一斉にずれる)。
func (m *Model) columns() [][]card.Card {
	lanes := m.lanes()
	cols := make([][]card.Card, len(lanes))
	for i, cs := range lanes {
		cols[i] = make([]card.Card, len(cs))
		for j, c := range cs {
			cols[i][j] = *c
		}
	}
	return cols
}

// lanes は columns と同じ並びを、カードの複製ではなく m.snap.Cards の中を指すポインタで返す (毎コマ描く経路はこちら。visible と同じ注意)。
func (m *Model) lanes() [][]*card.Card {
	cols := make([][]*card.Card, len(card.Columns))
	for c := range m.visible() {
		for k, st := range card.Columns {
			if c.State == st {
				cols[k] = append(cols[k], c)
			}
		}
	}
	for _, cs := range cols {
		slices.SortStableFunc(cs, func(a, b *card.Card) int { return card.LaneCompare(*a, *b) })
	}
	return cols
}

// position は選択中のカードの (列, 行)。見つからなければ ok=false。
func (m *Model) position() (col, row int, ok bool) { return m.positionOf(m.selected) }

func (m *Model) positionOf(id string) (col, row int, ok bool) {
	for i, cs := range m.lanes() {
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

// moveCol は左右のレーンへ 1 つ移る。空のレーンにも止まる (レーンにフォーカスが当たる)。行の位置はなるべく保つ。
// 端でそれ以上動けなければ、押した向きへレーンを揺らす (bump.go)。
func (m *Model) moveCol(delta int) tea.Cmd {
	from := m.col
	m.jumpCol(m.col + delta)
	if m.col == from {
		return m.startBump(delta, 0)
	}
	return nil
}

// jumpCol はレーン i (0 始まり) へ移る (h / l と 1〜6)。
func (m *Model) jumpCol(i int) {
	row := 0
	if _, r, ok := m.position(); ok {
		row = r
	}
	m.focusLane(i, row)
}

// moveRow はレーンの中で delta 枚動く。端で止める (半ページ・先頭・末尾の移動も同じ関数で端に寄せる)。動いたら true。
func (m *Model) moveRow(delta int) bool {
	col, row, ok := m.position()
	if !ok {
		return false // 空のレーン
	}
	cs := m.columns()[col]
	m.selected = cs[max(0, min(row+delta, len(cs)-1))].ID
	return m.selected != cs[row].ID
}

// stepRow は 1 枚だけ上下に動く。端でそれ以上動けなければ、押した向きへレーンを揺らす (bump.go)。
// 半ページ・先頭・末尾は「端へ寄せる」移動なので揺らさない。
func (m *Model) stepRow(delta int) tea.Cmd {
	if m.moveRow(delta) {
		return nil
	}
	return m.startBump(0, delta)
}

// writeKeys は backend に書き込む操作を始めるキーと、その操作の種類。受けない backend では押した時点で断る
// (案内の行も同じ種類で暗くする: view.go の hints)。
var writeKeys = map[string]backend.Op{"n": backend.OpNew, "i": backend.OpNew, "r": backend.OpAnswer, "+": backend.OpOrder, "w": backend.OpBtw, "x": backend.OpClear, "d": backend.OpDelete, "K": backend.OpMove, "J": backend.OpMove, "c": backend.OpResume}

func (m *Model) handleBoardKey(k tea.KeyPressMsg) tea.Cmd {
	if m.showDetail {
		if cmd, handled := m.handleDrawerKey(k.String()); handled {
			return cmd
		}
	}
	if op, ok := writeKeys[k.String()]; ok && !m.accepts(op) {
		// 入力欄を開いてから送った時点で断ると、書いた文が無駄になる (2026-09-24 の報告)。押した時点で断る
		if m.joined() {
			m.refuse("join の画面からは dispatcher を起こさない (持ち主の画面の c か、手で pro-con dispatcher を起動する)")
		} else {
			m.refuse("この画面では使えない操作 (見ているだけの画面 = pro-con --view は書き込まない)")
		}
		return nil
	}
	switch k.String() {
	case "ctrl+c":
		return m.requestQuit()
	case "ctrl+r": // 新版へ切り替える (ライブアップグレード。新版があるときだけ)
		return m.requestUpgrade()
	case "Q":
		return m.requestQuit()
	case "q":
		// q は「今の板を 1 段戻る」(docs/glogx-ui-guide.md §1) だけ。開いている板が無くても終了しない
		// (2026-09-25 にユーザーの依頼で廃止。終了は Q → quit だけ。quit.go)
		if !m.closeTop() {
			m.refuse("終了は Q を押して quit と打つ")
			return nil
		}
	case "esc":
		if !m.closeTop() {
			m.sticky = "" // 閉じる板が無いときの esc は、残しておいた通知を消す
		}
	case "tab":
		m.moveTab(1)
	case "shift+tab":
		m.moveTab(-1)
	case "left", "h", "ctrl+b": // ctrl+b は ← の別名 (ユーザー要望 2026-09-25。docs/glogx-ui-guide.md の emacs 層の例外)
		return m.moveCol(-1)
	case "right", "l", "ctrl+f": // ctrl+f は → の別名 (docs/glogx-ui-guide.md の emacs 層)
		return m.moveCol(1)
	case "enter":
		m.openDrawer()
		return m.startFrames()
	case "a":
		return m.attach()
	case "r":
		c, ok := m.selectedCard()
		if ok && c.WaitsOnPrompt() {
			m.refuse("PG の session が入力待ちで止まっている。a で attach して答える")
			return nil
		}
		if !ok || !c.Answerable() {
			m.refuse("回答できるのは質問待ちのカードだけ")
			return nil
		}
		if len(c.Wait.Questions) > 0 { // 選択肢つきの質問は radio / checkbox のフォームで答える
			m.openAnswerForm(c)
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
	case "o": // 添付の画像を外のアプリで開く (docs/glogx-ui-guide.md の o = 外で開く。issue 453)
		return m.openAttachments()
	case "i": // issue の一覧から選んで依頼する (docs/glogx-ui-guide.md の i = issues の板)
		m.loadPicker()
	case "s": // 設定画面 (PG の枠・PM の数 / プロセス / ディスク。issue 456)
		return m.openSettings()
	case "x": // 完了のレーンを片付ける (glogx の X = 捨てる の弱い版。y/N 確認を挟む)
		m.askClearDone()
	case "d": // カードを削除する (glogx の d = 削除。y/N 確認を挟む)
		m.askDelete()
	case "K": // 選んでいるカードを 1 つ上と入れ替える (優先度を上げる。j / k の強い版 = 選択ではなくカードを動かす。issue 470)
		return m.moveCard(-1)
	case "J": // 1 つ下と入れ替える (優先度を下げる)
		return m.moveCard(1)
	case "c": // 人が止めた dispatcher を起こす (continue。glogx で空いている字。PG が再開して利用枠を使うので y/N 確認を挟む)
		m.askResume()
	default:
		if i, ok := laneKey(k.String()); ok {
			m.jumpCol(i)
			return nil
		}
		return m.moveByMotion(listnav.MotionOf(k.String()))
	}
	return nil
}

// moveCard は選んでいるカードをレーンの中で delta (-1 = 上 / +1 = 下) の隣と入れ替えるよう backend に頼む。選択はカードについていく
// (選択は ID で持つ)。端では頼まずに止めて知らせる (巻かない)。隣は backend が適用の時点の並びで決めるので、速く続けて押しても押した回数だけ動く。
func (m *Model) moveCard(delta int) tea.Cmd {
	col, row, ok := m.position()
	if !ok {
		m.refuse("動かすカードを選んでいない")
		return nil
	}
	if next := row + delta; next < 0 || next >= len(m.columns()[col]) {
		if delta < 0 {
			m.info("レーンの先頭なので、これより上げられない")
		} else {
			m.info("レーンの末尾なので、これより下げられない")
		}
		return m.startBump(0, delta)
	}
	c := m.columns()[col][row]
	if res, err := m.be.Apply(backend.MoveCard{CardID: c.ID, Repo: m.tab, Delta: delta, Seen: c.Since}); err != nil {
		m.fail("失敗: " + err.Error())
	} else if res != "" {
		m.done(res)
	}
	m.setSnap(m.be.Snapshot())
	m.ensureSelection()
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
func (m *Model) moveByMotion(mo listnav.Motion) tea.Cmd {
	half := listnav.Half(m.shownCards())
	switch mo {
	case listnav.Down:
		return m.stepRow(1)
	case listnav.Up:
		return m.stepRow(-1)
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
	return nil
}

// closeTop は開いている板を手前から 1 つ閉じる (設定画面 → 詳細)。閉じたら true。
func (m *Model) closeTop() bool {
	switch {
	case m.set.open: // ここへ来るのは終了の入力欄を開くとき (closeAll) だけ: 開いている間の q / esc は設定画面が受ける。演出を待たずに閉じる
		m.set.open = false
		m.set.anim.Finish()
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
}

// handleInputKey は入力中のキー。Enter / Esc / Tab / ctrl+c 以外は編集キーとして lineedit に渡す
// (入力中は ctrl+b / ctrl+f / ctrl+u / ctrl+d も編集の意味。一覧の移動の語彙ではない)。
func (m *Model) handleInputKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.mode = modeBoard
		m.info("入力を取り消した")
	case "enter":
		if m.inputKind == inputQuit {
			return m.submitQuit()
		}
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

// askResume は人が止めた dispatcher (pro-con dispatcher --stop) を起こすかを確かめる。止めた印が無ければ確認を出さない。
func (m *Model) askResume() {
	if !m.snap.DispatcherHeld {
		m.info("dispatcher は人が止めていない (c は pro-con dispatcher --stop で止めたものを起こす)")
		return
	}
	m.askConfirm(backend.ResumeDispatcher{},
		"人が止めた dispatcher を起こします (作業中のカードの PG は続きから再開し、利用枠を使います)。よいですか? [y/N]")
}

// askClearDone は今のタブの完了のカードを片付けるかを確かめる。片付けるものが無ければ確認を出さない。
// doneInTab は今のタブの完了のカードの枚数 (x で片付ける対象)。
func (m *Model) doneInTab() int {
	n := 0
	for c := range m.visible() {
		if c.State == card.Done {
			n++
		}
	}
	return n
}

func (m *Model) askClearDone() {
	n := m.doneInTab()
	if n == 0 {
		m.refuse("片付ける完了のカードが無い")
		return
	}
	scope := "全 repo"
	if m.tab != "" {
		scope = m.tab
	}
	m.askConfirm(backend.ClearDone{Repo: m.tab},
		fmt.Sprintf("完了のカード %d 枚 (%s) をボードから片付けます (記録は残ります)。よいですか? [y/N]", n, scope))
}

// askDelete は選んでいるカードを削除するかを確かめる。依頼の列でも確かめる (docs/glogx-ui-guide.md §4: 削除は必ず y/N)。
// 依頼の列より右のカードは PG の session を止めてから消えるので、そう書く。
func (m *Model) askDelete() {
	c, ok := m.selectedCard()
	switch {
	case !ok:
		m.refuse("削除するカードを選んでいない")
		return
	case c.Deleting():
		m.info(c.ID + " は削除の依頼を受けている (PG の session を止めてから消える)")
		return
	}
	q := fmt.Sprintf("%s「%s」を削除します (依頼の列。すぐ消えます)。よいですか? [y/N]", c.ID, c.Title)
	if c.State != card.Requested {
		q = fmt.Sprintf("%s「%s」(%s) を削除します。PG の session を止めてから消えます (worktree とブランチは残ります)。よいですか? [y/N]",
			c.ID, c.Title, c.State.Label())
	}
	m.askConfirm(backend.DeleteCard{CardID: c.ID, From: "人間"}, q)
}

func (m *Model) handleConfirmKey(k tea.KeyPressMsg) tea.Cmd {
	switch key := k.String(); {
	case key == "ctrl+c":
		return m.requestQuit()
	case confirm.IsYesStrict(key):
		m.apply(m.pending)
	default:
		m.info("取り消した (実行していない)")
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
	case inputQuit: // enter は submitQuit が受ける (ここへは来ない)
		return
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
		m.fail("失敗: " + err.Error())
		if errors.Is(err, backend.ErrEmptyText) {
			m.mode = modeInput
			return // 入力欄を開いたまま書き直させる
		}
	} else {
		m.done(res)
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
	// backend は attach の前に照合し直すことがある (live は claude agents を呼ぶ。最大 3 秒)。キー処理の中で待たず裏で頼み、
	// 結果 (attachReadyMsg) が届いてから画面を明け渡す
	if m.attaching { // 照合の途中にもう一度押しても、2 回起動しない
		return nil
	}
	m.attaching = true
	be, id, session := m.be, c.ID, c.Session
	return m.child(func() tea.Msg {
		cmd, err := be.AttachCommand(session)
		return attachReadyMsg{cardID: id, session: session, cmd: cmd, err: err}
	})
}

// attachReadyMsg は attach の準備 (backend の照合) が済んだ知らせ。
type attachReadyMsg struct {
	cardID  string
	session string
	cmd     *exec.Cmd
	err     error
}
