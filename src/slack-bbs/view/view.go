// Package view は板を読む画面 (TUI)。左に話題の一覧、右に選んだ話題の中身 (ラベル・要点・原文) を出し、
// r で今のチャンネル、R で全部のチャンネルを読み直す (取得 → claude -p で分類 → 検査。中身は Loader に任せる)。
//
// 読み直しの間は前の板を出したままにする。結果は世代で照合し、追い越された古い結果は捨てる (r を連打しても、
// 最後に頼んだ読み直しの結果だけが残る)。
package view

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/jiikko/dotfiles/src/termsafe"
	"github.com/jiikko/dotfiles/src/tuikit/confirm"
	"github.com/jiikko/dotfiles/src/tuikit/hintfit"
	"github.com/jiikko/dotfiles/src/tuikit/layout"
	"github.com/jiikko/dotfiles/src/tuikit/listnav"
	"github.com/jiikko/dotfiles/src/tuikit/spinner"
	"github.com/jiikko/dotfiles/src/tuikit/termcolor"
	"github.com/jiikko/dotfiles/src/tuikit/termwidth"
	"github.com/jiikko/dotfiles/src/tuikit/tickchain"
	"github.com/jiikko/dotfiles/src/tuikit/toast"
	"slack-bbs/board"
	"slack-bbs/render"
	"slack-bbs/subs"
	"subproc"
)

// Loader は 1 チャンネルを読み、話題に組んだ板を返す。画面の外の goroutine で呼ばれる。
type Loader func(ctx context.Context, ch subs.Channel) (render.Page, error)

type chanState struct {
	ch       subs.Channel
	page     *render.Page // 最後に読めた板 (読み直しに失敗しても残す)
	err      error        // 最後の読み直しの誤り
	loading  bool
	started  time.Time // 走っている読み直しを始めた時刻 (経過秒を出す)
	gen      int       // 最後に頼んだ読み直しの通し番号 (Model.seq)。結果はこの値と照合する
	loadedAt time.Time
	stale    bool              // page はキャッシュから出した前回の板で、この起動ではまだ読み直せていない
	hl       highlighter       // page の本文の色付け (page を置き換えるたびに作り直す)
	rows     []row             // 左の一覧の行 (日付の見出しと話題)。nil なら Model.rows が作り直す (page・隠した話題・z を変えたら捨てる)
	read     map[string]string // 既読の印 (単位の ID → 読んだときの単位の最後の発言の ts)
	hidden   map[string]string // x で隠した単位 (単位の ID → 隠したときの単位の最後の発言の ts)
	// unread は未読の話題の数の覚え (タブに出す。描くたびに全チャンネルの全発言を数え直さない)。page か read を変えたら捨てる
	unread *int
	// mentions は単位の ID → その単位の発言への言及 (page を置き換えるたびに作り直す。未読と既読の印に使う)
	mentions map[string][]render.Mention
}

// Model は画面の状態。
type Model struct {
	ctx   context.Context
	load  Loader
	open  func(url string) error
	now   func() time.Time
	chans []chanState
	cur   int
	list  listnav.List
	pager listnav.Pager
	body  bool // 右の欄を操作しているか
	// showHidden は x で隠した話題も一覧に出すか (z で切り替える。出すときは淡い色)
	showHidden bool
	// unsub は d で購読を外すか確かめているチャンネルの ID (確かめていなければ空)。チャンネルは ID で持つ
	// (確かめている間に読み直し・購読の保存でタブの番号がずれても、別のチャンネルを外さない)
	unsub string
	w, h  int
	// toast は右下に数秒出す知らせ (読み直しの始まりと終わり・開けなかった・読み込み中で読み直せない等)。中立の知らせ (ShowInfo) は
	// 既定の水色が背景が青い端末で沈むので (style.go の色の約束)、端末の文字色 (toastInfoColor) で出す
	toast toast.Stack
	// ticks は描き直しの tick の束ね。toast が動く間は速く、読み直し・一覧の読み込みを待つ間は回る印の周期で張る
	// (周期の違う 2 つの動きを 1 本の bool で張ると、遅い tick が張ってある間に始まった toast がその tick まで止まる)
	ticks tickchain.Chain
	// after は d 後に Msg を返す Cmd を作る (既定は tea.Tick。テストは待たずに捨てる)
	after    func(d time.Duration, f func(time.Time) tea.Msg) tea.Cmd
	quitting bool
	reads    *Reads
	body1    bodyCache // 本文の欄の折り返しの覚え (bodyLines)
	// seq は読み直しの通し番号。チャンネルごとに数えると、購読から外して付け直したチャンネルで、外す前の読み直しの結果を
	// 付け直した後の読み直しの結果と取り違える
	seq     int
	subs    *Subscriptions
	picker  *subs.Picker // 購読の画面を開いている間だけ非 nil
	picking bool         // 購読の画面の一覧を読んでいる
	// pickCached は、読んでいる間に前に取った一覧で購読の画面を開いたこと (読み終えたら開いている画面の一覧を差し替える。
	// 閉じた後なら捨てる: 閉じた画面を取り直しの終わりに勝手に開き直さない)
	pickCached bool
	// pickStarted は購読の画面の一覧を読み始めた時刻 (経過秒を出す)
	pickStarted time.Time
}

// Subscriptions は読む画面から購読を変える口 (s で開く画面)。nil なら s は効かない。
type Subscriptions struct {
	// Open は全チャンネルと今の購読から選ぶ画面を作る (slack-cli でチャンネルの一覧を取る。画面の外の goroutine で呼ばれる)
	Open func(ctx context.Context) (*subs.Picker, error)
	// Cached は前に取ったチャンネルの一覧と今の購読で、すぐ開ける画面を作る (一覧を取った時刻と一緒に)。無ければ ok が偽。nil なら使わない
	Cached func() (p *subs.Picker, at time.Time, ok bool)
	Save   func([]subs.Channel) error // 選んだ購読を保存する
}

// New は画面を作る。chans は 1 つ以上。
func New(ctx context.Context, chans []subs.Channel, load Loader, open func(string) error, now func() time.Time) *Model {
	m := &Model{ctx: ctx, load: load, open: open, now: now, w: 100, h: 30, after: tea.Tick}
	m.toast.InfoColor = toastInfoColor
	for _, c := range chans {
		m.chans = append(m.chans, chanState{ch: c, read: map[string]string{}, hidden: map[string]string{}})
	}
	return m
}

// SetSubscriptions は s で購読の画面を開けるようにする。
func (m *Model) SetSubscriptions(s *Subscriptions) { m.subs = s }

// Preload はチャンネル i に前回の板 (キャッシュ) を出しておく。at は前回取得した時刻。読み直せるまで出し、開いたときに読み直す。
func (m *Model) Preload(i int, p render.Page, at time.Time) {
	if i < 0 || i >= len(m.chans) {
		return
	}
	m.chans[i].setPage(p, at, true)
}

// setPage は板を p に置き換える (色付けと一覧の行も作り直す)。
func (c *chanState) setPage(p render.Page, at time.Time, stale bool) {
	c.page, c.err, c.loadedAt, c.stale, c.hl = &p, nil, at, stale, newHighlighter(p)
	c.rows = nil // 一覧の行は描くときに作る (隠した話題を出すかは画面の状態で決まる。Model.rows)
	c.mentions = map[string][]render.Mention{}
	for _, mn := range p.Mentions {
		c.mentions[mn.Unit] = append(c.mentions[mn.Unit], mn)
	}
	c.unread = nil
}

// loadedMsg は読み直しの結果。チャンネルは番号でなく ID で指す (購読を変えるとタブの番号がずれる)。
type loadedMsg struct {
	ch   string
	gen  int
	page render.Page
	err  error
	at   time.Time
}

// pickerMsg は購読の画面の一覧を読んだ結果。
type pickerMsg struct {
	p   *subs.Picker
	err error
}

// toastInfoColor は中立の知らせ (読み直しの始まり) の色: 端末の文字色。
const toastInfoColor = "\x1b[39m"

// reload はチャンネル i の読み直しを頼み、始めたことを知らせる (終わりは loaded が知らせる)。返す Cmd は画面の状態を触らない
// (Loader と照合用の値だけを持つ)。
func (m *Model) reload(i int) tea.Cmd {
	c := &m.chans[i]
	verb := "読み直しています…"
	if c.page == nil {
		verb = "読んでいます…"
	}
	m.toast.ShowInfo(c.ch.Label() + " を" + verb)
	m.seq++
	c.gen = m.seq
	c.loading, c.started = true, m.now()
	c.err = nil // 前の失敗は、この読み直しの結果で決め直す
	gen, ch, load, ctx, now := c.gen, c.ch, m.load, m.ctx, m.now
	return func() tea.Msg {
		p, err := load(ctx, ch)
		return loadedMsg{ch: ch.ID, gen: gen, page: p, err: err, at: now()}
	}
}

// Init は最初のチャンネルだけを読む (他は開いたときに読む。起動のたびに全チャンネル分 claude を呼ばない)。
func (m *Model) Init() tea.Cmd {
	if len(m.chans) == 0 {
		return tea.Quit
	}
	return m.reload(0)
}

// Update はメッセージを処理する。
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case loadedMsg:
		m.loaded(msg)
	case pickerMsg:
		m.picking = false
		if m.pickCached {
			m.pickCached = false
			switch {
			case m.picker == nil: // 前の一覧で開いた画面を、取り直しの間に閉じた
			case msg.err != nil:
				m.picker.SetNote("前に取った一覧 (取り直せなかった: " + msg.err.Error() + ")")
			default:
				m.picker.Refresh(msg.p)
				m.picker.SetNote("")
			}
			break
		}
		if msg.err != nil {
			m.toast.Show("チャンネルの一覧を読めなかった: "+msg.err.Error(), false)
			break
		}
		if m.unsub != "" {
			// 購読を外す確認の板は購読の画面の下に隠れる。隠れた確認に購読の画面のキー (Enter・y) で答えさせない
			m.unsub = ""
			m.toast.Show("購読を外すのをやめた (購読の画面を開いた)", false)
		}
		m.picker = msg.p
	case tea.PasteMsg:
		if m.picker != nil {
			m.picker.Paste(msg.Content) // 読む画面には入力欄が無いので、貼った文字はキーとして解釈しない
		}
	case tea.KeyPressMsg:
		var cmd tea.Cmd
		switch {
		case msg.String() == "ctrl+c":
			m.quitting = true // どの画面 (購読の画面・検索の欄の中) からでも、すぐ終わる (TUI ガイド §1)
		case m.unsub != "":
			cmd = m.confirmUnsubscribe(msg.String())
		case m.picker != nil:
			cmd = m.pickKey(msg.String(), msg.Text)
		default:
			cmd = m.key(msg.String())
		}
		if m.quitting {
			return m, tea.Quit
		}
		return m, tea.Batch(append(m.frameCmds(), cmd)...)
	case toast.Msg:
		m.toast.StartLeaving(msg)
	case tickMsg:
		m.ticks.Fired(msg.seq) // 追い越された古い tick でも描き直しはする。張り直すかは frameCmds (Want) が決める
	}
	return m, tea.Batch(m.frameCmds()...)
}

// LastToast は最後に出した知らせの文 (出していなければ空)。
func (m *Model) LastToast() string { return m.toast.Text() }

// tickMsg は描き直しの合図。seq は張ったときの ticks の番号。
type tickMsg struct{ seq uint64 }

const (
	frameInterval = 16 * time.Millisecond // toast が滑り込む・滑り出る間の描き直しの周期
	spinnerStep   = 80 * time.Millisecond // 回る印の 1 コマ (読み直し・一覧の読み込みを待つ間の描き直しの周期)
)

// frameCmds は toast を 1 コマ進め、退場の時計と、動くものがある間の tick を張る。
func (m *Model) frameCmds() []tea.Cmd {
	var cmds []tea.Cmd
	for _, t := range m.toast.Advance() {
		cmds = append(cmds, m.after(t.After, func(time.Time) tea.Msg { return t.Msg }))
	}
	var d time.Duration
	switch {
	case m.toast.Animating():
		d = frameInterval
	case m.busy():
		d = spinnerStep
	default:
		return cmds
	}
	if seq, arm := m.ticks.Want(m.now(), d); arm {
		cmds = append(cmds, m.after(d, func(time.Time) tea.Msg { return tickMsg{seq: seq} }))
	}
	return cmds
}

// busy は回る印を出しているか (どれかのチャンネルを読み直している・購読の画面の一覧を読んでいる)。
func (m *Model) busy() bool {
	return m.picking || slices.ContainsFunc(m.chans, func(c chanState) bool { return c.loading })
}

// spin は回る印と、from からの経過秒 (「⠋ 12 秒」)。数十秒かかる読み直しを、始まりの知らせ (数秒で消える) だけにしない
func (m *Model) spin(from time.Time) string {
	return fmt.Sprintf("%s %d 秒", spinner.At(m.now(), spinnerStep), int(m.now().Sub(from)/time.Second))
}

func (m *Model) loaded(msg loadedMsg) {
	idx := slices.IndexFunc(m.chans, func(c chanState) bool { return c.ch.ID == msg.ch })
	if idx < 0 {
		return // 読んでいる間に購読から外した
	}
	c := &m.chans[idx]
	if msg.gen != c.gen {
		return // 後から頼んだ読み直しに追い越された
	}
	c.loading = false
	if msg.err != nil {
		c.err = msg.err
		verb := "読み直せなかった"
		if c.page == nil {
			verb = "読めなかった"
		}
		m.toast.Show(c.ch.Label()+" を"+verb+": "+msg.err.Error(), false)
		return
	}
	var keep string
	if idx == m.cur {
		if t, ok := m.selected(); ok {
			keep = t.Units[0].ID
		}
	}
	verb := "読み直した"
	if c.page == nil {
		verb = "読んだ"
	}
	c.setPage(msg.page, msg.at, false)
	m.toast.Show(fmt.Sprintf("#%s を%s (未読の話題 %d 件)", c.ch.Name, verb, m.unreadCount(idx)), true)
	if idx == m.cur {
		// 読み直しで話題が並び替わっても、見ていた話題 (最初の単位で同一視する) にカーソルを戻す。無くなっていたら先頭へ
		m.list = listnav.List{}
		m.reselect(keep)
		m.pager.Reset()
		// 読み直しは既読にしない (本文の欄に出ていても): 欄は先頭へ戻り、付いた返信は欄の外にありうる。小さい端末・購読の画面の裏では
		// 欄が描かれていない。読んでいる最中に付いた返信は、選び直すまで ● で出す
	}
}

// Page はチャンネル i の最後に読めた板 (まだ読めていなければ ok が偽)。
func (m *Model) Page(i int) (render.Page, bool) {
	if i < 0 || i >= len(m.chans) || m.chans[i].page == nil {
		return render.Page{}, false
	}
	return *m.chans[i].page, true
}

func (m *Model) topics() []board.Topic {
	if p := m.chans[m.cur].page; p != nil {
		return p.Topics
	}
	return nil
}

func (m *Model) selected() (board.Topic, bool) {
	i, ok := m.selectedIndex()
	if !ok {
		return board.Topic{}, false
	}
	return m.topics()[i], true
}

// selectedIndex は選んだ話題の page.Topics の添字。
func (m *Model) selectedIndex() (int, bool) {
	rows := m.rows()
	if len(rows) == 0 {
		return 0, false
	}
	r := rows[min(max(m.list.Cursor, 0), len(rows)-1)]
	if r.header() {
		return 0, false // 描く前 (fitList の前) に見出しに乗っている
	}
	return r.topic, true
}

// key は読む画面のキー。語彙は docs/glogx-ui-guide.md (TUI ガイド) に揃える: q は 1 段戻る (本文の欄から一覧へ、一覧なら終わる)・
// Ctrl-C はすぐ終わる (Update が購読の画面より先に捌く)・Enter は本文の欄の開閉・J/K は本文を開いたまま隣の話題へ・Ctrl-F は → の別名。
func (m *Model) key(k string) tea.Cmd {
	if k == "ctrl+f" {
		k = "right"
	}
	switch k {
	case "q":
		if m.body && !m.tiny() { // 小さい端末では欄が見えないので、見えない欄を閉じるだけの 1 打にしない
			m.body = false
		} else {
			m.quitting = true
		}
		return nil
	case "s":
		return m.openPicker()
	}
	if len(m.chans) == 0 {
		return nil // 購読を全部外した後は、s で選び直すか q で終わる
	}
	// カーソルを見出しから話題へ逃がすのは描画 (leftLines) だけではない: 小さい端末の画面は一覧を描かないので、
	// タブを替えた直後のカーソルが見出しに残り、l / o / u が効かなくなる
	m.fitList(1)
	switch k {
	case "r":
		if m.chans[m.cur].loading {
			// 走っている読み直しを待つ (連打で claude を何本も起こさない)
			m.toast.Show(m.chans[m.cur].ch.Label()+" は読み込み中なので、読み直せない (終わってからもう一度押す)", false)
			return nil
		}
		return m.reload(m.cur)
	case "R":
		var cmds []tea.Cmd
		busy := 0
		for i := range m.chans {
			if m.chans[i].loading {
				busy++
				continue
			}
			cmds = append(cmds, m.reload(i))
		}
		// 始めたことはチャンネルごとでなく 1 枚で知らせる (reload が出した 1 枚ずつの知らせは、中立の知らせが次の知らせで退くので残らない)
		switch {
		case busy == len(m.chans):
			m.toast.Show("どのチャンネルも読み込み中なので、読み直せない (終わってからもう一度押す)", false)
		case busy > 0:
			m.toast.ShowInfo(fmt.Sprintf("%d チャンネルを読み直しています… (読み込み中の %d チャンネルは読み直さない)", len(cmds), busy))
		default:
			m.toast.ShowInfo(fmt.Sprintf("%d チャンネルを読み直しています…", len(cmds)))
		}
		return tea.Batch(cmds...)
	case "tab", "shift+tab":
		d := 1
		if k == "shift+tab" {
			d = -1
		}
		return m.switchTo(listnav.Cycle(m.cur, d, len(m.chans)))
	case "o":
		m.openSelected()
		return nil
	case "u":
		m.toggleRead()
		return nil
	case "x":
		m.toggleHidden()
		return nil
	case "z":
		m.toggleShowHidden()
		return nil
	case "d":
		if m.subs == nil {
			m.toast.Show("購読を外せるのは、チャンネルを指定しないで起動したときだけ (端末の slack-bbs sub でも外せる)", false)
			return nil
		}
		m.unsub = m.chans[m.cur].ch.ID
		return nil
	}
	if m.body {
		switch k {
		case "h", "left", "esc", "enter":
			m.body = false
			return nil
		case "J":
			m.step(1)
			return nil
		case "K":
			m.step(-1)
			return nil
		}
		if mo := listnav.MotionOf(k); mo != listnav.None {
			lines, _ := m.bodyLines()
			m.pager.Move(mo, len(lines), m.bodyRows(), 0)
		}
		return nil
	}
	switch k {
	case "l", "right", "enter":
		if _, ok := m.selected(); ok {
			m.body = true
			m.setRead(true)
		}
		return nil
	}
	if mo := listnav.MotionOf(k); mo != listnav.None {
		m.fitList(1)
		from := m.list.Cursor
		m.list.Move(mo, len(m.rows()), m.listRows(), 0)
		dir := 1
		if mo == listnav.Up || mo == listnav.HalfUp || mo == listnav.Top {
			dir = -1
		}
		m.fitList(dir)
		if m.list.Cursor != from {
			m.pager.Reset()
			m.setRead(true) // 選んだ話題は右の欄に中身が出る = 読んだ (起動・読み直しで先頭に乗っただけでは既読にしない)
		}
	}
	return nil
}

// step は本文の欄を開いたまま、一覧の隣の話題 (dir = 1 で下、-1 で上。日付の見出しは飛ばす) へ移り、本文を先頭から出す。
// 端では止めて知らせる (巻かない)。移った話題は本文の欄に出る = 読んだ (j / k と同じ)
func (m *Model) step(dir int) {
	rows := m.rows()
	for i := m.list.Cursor + dir; i >= 0 && i < len(rows); i += dir {
		if rows[i].header() {
			continue
		}
		m.list.Cursor = i
		m.fitList(dir)
		m.pager.Reset()
		m.setRead(true)
		return
	}
	if dir > 0 {
		m.toast.Show("これが最後の話題です", false)
	} else {
		m.toast.Show("これが最初の話題です", false)
	}
}

func (m *Model) switchTo(i int) tea.Cmd {
	if i == m.cur {
		return nil
	}
	m.cur, m.body = i, false
	m.list = listnav.List{}
	m.pager.Reset()
	c := &m.chans[i]
	if (c.page == nil || c.stale) && !c.loading && c.err == nil {
		return m.reload(i) // まだ読んでいない (キャッシュの板しか無い) チャンネルは開いたときに読む
	}
	return nil
}

// openPicker は購読の画面を開く (一覧は画面の外で読む)。読んでいる間に s を重ねても 1 回だけ読む。
func (m *Model) openPicker() tea.Cmd {
	if m.subs == nil {
		m.toast.Show("購読するチャンネルを選ぶ画面は、チャンネルを指定しないで起動したときだけ開ける (端末の slack-bbs sub でも選べる)", false)
		return nil
	}
	// 前に取った一覧があれば、すぐその一覧で開いて裏で取り直す (取り直しは slack-cli が数十秒かけることがある。
	// slack-cli の一覧の覚えは 1 時間で、ページの間を空けて取るので 18 ページで約 1 分)
	cached := false
	if m.subs.Cached != nil {
		if p, at, ok := m.subs.Cached(); ok {
			p.SetNote(at.Format("01/02 15:04") + " に取った一覧・取り直し中")
			m.picker, m.pickCached, cached = p, true, true
		}
	}
	if m.picking {
		return nil
	}
	m.picking, m.pickStarted = true, m.now()
	if !cached {
		// 押した手応えは知らせで返す (数秒かかる一覧の読み込みを、上の状態の行の回る印だけにしない)
		m.toast.ShowInfo("チャンネルの一覧を読んでいます…")
	}
	open, ctx := m.subs.Open, m.ctx
	return func() tea.Msg {
		p, err := open(ctx)
		return pickerMsg{p: p, err: err}
	}
}

// pickKey は購読の画面を開いている間のキー。保存したら画面を閉じずにタブを入れ替える。
func (m *Model) pickKey(k, text string) tea.Cmd {
	p := m.picker
	p.Press(k, text)
	if !p.Done() {
		return nil
	}
	m.picker = nil
	if !p.Saved() {
		return nil
	}
	sel := p.Selected()
	if err := m.subs.Save(sel); err != nil {
		m.toast.Show("購読するチャンネルを保存できなかった: "+err.Error(), false)
		return nil
	}
	return m.setChannels(sel)
}

// setChannels はタブを購読 sel に合わせる。残るチャンネルは読んだ板と読み直しの状態をそのまま持ち (走っている読み直しの結果も
// ID で照合して受け取る)、新しいチャンネルは開いたときに読む。見ていたチャンネルが残れば、そのまま見続ける。
func (m *Model) setChannels(sel []subs.Channel) tea.Cmd {
	curID := ""
	if len(m.chans) > 0 {
		curID = m.chans[m.cur].ch.ID
	}
	old := map[string]chanState{}
	for _, c := range m.chans {
		old[c.ch.ID] = c
	}
	// 外したチャンネルで走っている読み直し (claude -p) は止めない: 結果は ID の照合で捨てる。止めるには読み直しごとの
	// 取り消しを持つ必要があり、外す操作は稀なので費用 (1 回分の分類) を受け入れる
	next := make([]chanState, 0, len(sel))
	for _, ch := range sel {
		c, ok := old[ch.ID]
		c.ch = ch
		next = append(next, c)
		if !ok {
			defer m.loadRead(len(next) - 1) // m.chans を next に置き換えた後に読む
		}
	}
	m.chans = next
	m.toast.Show(fmt.Sprintf("購読するチャンネルを保存した (%d 件)", len(next)), true)
	if i := slices.IndexFunc(next, func(c chanState) bool { return c.ch.ID == curID }); i >= 0 {
		m.cur = i
		return nil
	}
	m.cur, m.body = 0, false
	m.list = listnav.List{}
	m.pager.Reset()
	if len(next) == 0 {
		return nil
	}
	if c := &m.chans[0]; (c.page == nil || c.stale) && !c.loading && c.err == nil {
		return m.reload(0)
	}
	return nil
}

// openSelected は選んだ話題の最後の発言を Slack で開く。
func (m *Model) openSelected() {
	t, ok := m.selected()
	p := m.chans[m.cur].page
	if !ok || p == nil || m.open == nil {
		return
	}
	last := t.Units[0]
	for _, u := range t.Units {
		if board.TsBefore(last.Last(), u.Last()) {
			last = u
		}
	}
	url := p.URL(last.Last(), last)
	if url == "" {
		m.toast.Show("Slack のリンクを組めない (ワークスペースの URL が分からない)", false)
		return
	}
	if err := m.open(url); err != nil {
		m.toast.Show("Slack で開けなかった: "+err.Error(), false)
		return
	}
	m.setRead(true) // 開けなかったら既読にしない (Slack で読めていない)
}

// 画面の割り振り: 上の topRows 行 (ロゴと状態・線・タブ・注記)、最後の helpRows 行はキーの案内、残りが 2 枚のカード
// (上下の縁で 2 行と、下の落ち影で 1 行)。本文のカードは中身の頭の 2 行に話題の題と線を出す。
func (m *Model) paneRows() int  { return max(m.h-topRows-helpRows, 4) }
func (m *Model) cardRows() int  { return max(m.paneRows()-3, 1) }
func (m *Model) listRows() int  { return m.cardRows() }
func (m *Model) bodyRows() int  { return max(m.cardRows()-bodyHeadRows, 1) }
func (m *Model) leftWidth() int { return min(max(m.w*3/10, 24), 44) }

// rightWidth は本文のカードの幅 (2 枚のカードの右の落ち影 1 桁ずつと、間の 1 桁を引く)。
func (m *Model) rightWidth() int { return m.w - m.leftWidth() - 3 }
func (m *Model) bodyInner() int  { return m.rightWidth() - cardChrome(cardPadX(m.w)) } // 本文のカードの中身の幅

const (
	topRows      = 4
	bodyHeadRows = 2 // 本文のカードの頭の、話題の題と線
)

// bodyWidth は本文を折り返す幅: 枠の内側からスクロールバーの桁を引いた幅 (引かずに組むと、全幅の行が Scrollbar で切られて末尾が … に化ける)
func (m *Model) bodyWidth() int { return max(m.bodyInner()-layout.ScrollbarWidth, 10) }

// bodyLines は右の欄の行 (枠の幅で折り返した後)。
// first[i] は lines[i] が元の行の 1 行目か (折り返しの続きでないか)。見出し・発言の頭の色は 1 行目にだけ付ける
// (続きの行は、狭い端末では字下げ無しで始まり、本文の「▍」や「時刻 名前: 」が行の頭へ来うる)。
func (m *Model) bodyLines() (lines []string, first []bool) {
	c := m.chans[m.cur]
	if c.page == nil {
		switch {
		case c.err != nil:
			return wrap(termsafe.PlainLine("読めなかった: "+c.err.Error()), m.bodyWidth()), nil
		default:
			return []string{"読んでいます…"}, nil
		}
	}
	ti, ok := m.selectedIndex()
	if !ok {
		if len(c.page.Topics) > 0 {
			return []string{"話題は全部隠している (z で隠した話題も出す)。"}, nil
		}
		return []string{"期間内の発言はありません。"}, nil
	}
	key := bodyKey{page: c.page, topic: ti, width: m.bodyWidth()}
	if m.body1.key == key {
		return m.body1.lines, m.body1.first
	}
	for _, l := range c.page.Body(c.page.Topics[ti], false) {
		for i, w := range wrap(l, m.bodyWidth()) { // 色は rightLines が、折り返した後の画面に出す行にだけ付ける
			lines, first = append(lines, w), append(first, i == 0)
		}
	}
	m.body1 = bodyCache{key: key, lines: lines, first: first}
	return lines, first
}

// bodyKey は本文の欄の折り返しの覚えの鍵。板 (読み直すと別の値になる)・話題・幅が同じなら同じ行になる。
type bodyKey struct {
	page  *render.Page
	topic int
	width int
}

// bodyCache は最後に折り返した本文の欄の行。描くたびに話題の全文を折り返し直さない (発言が数百ある話題で、知らせが動く間の
// 1 秒 60 回の描画が 1 回 2.5ms かかっていた)。🚨 lines は共有するので、呼び出し側は書き換えない (色は写しに付ける)
type bodyCache struct {
	key   bodyKey
	lines []string
	first []bool
}

// wrap は行を幅 w で折り返す。2 行目からは元の行の字下げに 2 桁足して揃える (原文の続きが左端へ戻って、誰の発言か追えなくならない)。
func wrap(s string, w int) []string {
	if s == "" {
		return []string{""}
	}
	body := strings.TrimLeft(s, " ")
	indent := len(s) - len(body)
	first := termwidth.Wrap(s, w, false)
	if len(first) <= 1 || indent+2 >= w/2 {
		return first
	}
	head := termwidth.Truncate(s, w, "")
	rest := strings.TrimPrefix(s, head)
	pad := strings.Repeat(" ", indent+2)
	out := []string{head}
	for _, l := range termwidth.Wrap(rest, w-len(pad), true) {
		out = append(out, pad+l)
	}
	return out
}

// MinWidth / MinHeight より小さい端末では枠を描かず、案内の 1 行だけを出す (枠の最小の幅・高さを割ると行がはみ出す)。
const (
	MinWidth  = 43 // これより狭いと、本文を折り返す幅 (bodyWidth の下限 10) が本文のカードの中身より広くなり、全行が … で切れる
	MinHeight = 12
)

// tiny は枠を描けない小さい端末か。
func (m *Model) tiny() bool { return m.w < MinWidth || m.h < MinHeight }

// Lines は画面の行。toast は右下 (キーの案内の上) に重ねる。
func (m *Model) Lines() []string {
	lines := m.screen()
	if i := m.unsubIndex(); i >= 0 && m.tiny() {
		// 小さい端末の画面は 1 行なので板が収まらない。問いと答え方を 1 行で出す
		return []string{termwidth.Truncate(m.chans[i].ch.Label()+" の購読を外す？ y/Enter: 外す  その他: やめる", max(m.w, 1), "…")}
	}
	if i := m.unsubIndex(); i >= 0 {
		box := confirm.Dialog(" 購読を外す ", []string{m.chans[i].ch.Label(), "の購読を外しますか？", "(s の購読の画面でまた付けられる)"}, confirm.HintYesNo, m.w, true)
		lines = layout.OverlayCentered(lines, box, m.w, len(lines), true)
	}
	if box := m.toast.BoxLines(true, max(len(lines)-1-helpRows, 1), m.w); len(box) > 0 && len(lines) > len(box)+helpRows {
		lines = layout.OverlayRight(lines, box, m.w, true, len(lines)-helpRows-len(box)) // キーの案内の上
	}
	return lines
}

func (m *Model) screen() []string {
	if m.picker != nil {
		m.picker.Resize(m.w, m.h)
		return m.picker.Lines()
	}
	if len(m.chans) == 0 {
		lines := []string{"読むチャンネルがありません (s で購読するチャンネルを選ぶ・q で終わる)"}
		if m.picking {
			lines = append(lines, m.spin(m.pickStarted)+" チャンネルの一覧を読んでいます")
		}
		if m.toast.Visible() {
			// 行が少なく箱を重ねられないので、知らせの文を行で出す (保存できなかった・一覧を読めなかったを黙らせない)
			lines = append(lines, termwidth.Truncate(termsafe.PlainLine(m.toast.Text()), max(m.w, 1), "…"))
		}
		return lines
	}
	if m.tiny() {
		return []string{termwidth.Truncate(fmt.Sprintf("端末が小さすぎる。q で終わる (%dx%d 以上にする)", MinWidth, MinHeight), max(m.w, 1), "")}
	}
	lines := []string{m.titleLine(), gradRule(m.w, cAccent, cPink, cPurple, cTeal, cBg), m.tabsLine(), m.noteLine()}
	left := m.leftLines()
	right := m.rightLines()
	for i := range m.paneRows() {
		l, r := "", ""
		if i < len(left) {
			l, r = left[i], right[i]
		}
		lines = append(lines, l+" "+r)
	}
	return append(lines, m.helpLines()...)
}

// titleLine はロゴと今のチャンネルの状態。右端に購読の画面を開くキーを出す (幅が足りなければ状態を詰め、それでも足りなければキーを外す)。
func (m *Model) titleLine() string {
	st := m.state()
	if m.picking {
		// 今のチャンネルの状態 (読み直し中・失敗) を先に置く: 狭い端末で切れるのは一覧の読み込みの方にする
		// (押した手応えは openPicker の知らせが出す)
		st = strings.TrimSpace(st + "  " + m.spin(m.pickStarted) + " チャンネルの一覧を読んでいます")
	}
	head := logo() + "  "
	room := m.w - termwidth.Of(head)
	cap := ""
	if m.subs != nil && !m.picking { // 読んでいる間は s を押し終えているので、状態に幅を譲る
		if k := keycap("s", "購読するチャンネル"); room-termwidth.Of(k)-2 >= minState {
			cap = k
		}
	}
	room -= termwidth.Of(cap)
	if cap != "" {
		room -= 2
	}
	st = termwidth.Truncate(termsafe.PlainLine(st), max(room, 0), "…")
	return head + reset(dim(st)) + termwidth.PadSpaces(max(m.w-termwidth.Of(head+st+cap), 0)) + cap
}

// minState は右端のキーより先に残す状態の幅。
const minState = 20

// tabsLine はチャンネルのタブ。幅に収まらないときは今のタブを残し、今のタブから遠いタブを「…」に畳む
// (行の右端で切ると、後ろの方のチャンネルを開いたときに、どれを見ているかが消える)。
func (m *Model) tabsLine() string {
	names := make([]string, len(m.chans))
	for i, c := range m.chans {
		names[i] = c.ch.Label()
		if c.loading {
			// 今のチャンネルにも出す (状態の行だけでは、タブを見たときに読み込み中か分からない。ユーザー要望 2026-11-11)。
			// R で読み直している他のチャンネルも、どれが終わっていないか見える
			names[i] = spinner.At(m.now(), spinnerStep) + " " + names[i]
		}
		if n := m.unreadCount(i); n > 0 {
			names[i] += fmt.Sprintf("  未読 %d", n)
		}
	}
	return tabWindow(names, m.cur, m.w)
}

func (m *Model) state() string {
	c := m.chans[m.cur]
	switch {
	case c.loading && c.stale:
		return m.spin(c.started) + " 読み直し中 (" + c.loadedAt.Format("01/02 15:04") + " に読んだ板を表示中)"
	case c.loading && c.page != nil:
		return m.spin(c.started) + " 読み直し中 (前に読んだ板を表示中)"
	case c.loading:
		return m.spin(c.started) + " 読んでいます"
	case c.err != nil && c.stale:
		return "🚨 読み直しに失敗 (" + c.loadedAt.Format("01/02 15:04") + " に読んだ板を表示中)"
	case c.err != nil && c.page != nil:
		return "🚨 読み直しに失敗 (前に読んだ板を表示中)"
	case c.err != nil:
		return "🚨 読めなかった (r で読み直す)"
	case c.stale:
		return c.loadedAt.Format("01/02 15:04") + " に読んだ板 (まだ読み直していない)"
	case c.page != nil:
		s := c.loadedAt.Format("15:04") + " に読んだ"
		// 新しい発言が無くて前回の分類を使い回したときは、分類した時刻を添える (ラベルや見出しが古いと分かるように)
		if at := c.page.ClassifiedAt; !at.IsZero() && at.Before(c.loadedAt.Add(-time.Minute)) {
			s += " (話題の分け方は " + at.Format("01/02 15:04") + " のまま)"
		}
		return s
	}
	return ""
}

const (
	tabGap  = " "
	tabMore = "…"
)

// tab は 1 つのタブ (今のタブは水色の地、他は沈んだ地)。
func tab(name string, cur bool) string {
	if cur {
		return pillTab(name, cTeal, cBg, true)
	}
	return pillTab(name, cTabIdle, cDim, false)
}

// tabWindow は names をタブにして tabGap でつないだ行。幅 room に収まらなければ cur を含む連続した範囲だけを出し、
// 外した側に tabMore を置く。cur だけでも収まらないときは、印を付けずに cur の名前を room に縮める
// (印の分だけ cur が押し出され、呼び出し側が行の右端で切ると状態が消える)。
func tabWindow(names []string, cur, room int) string {
	width := func(lo, hi int) int {
		w := termwidth.Of(tabGap) * (hi - lo)
		for _, n := range names[lo : hi+1] {
			w += termwidth.Of(n) + pillWidth
		}
		if lo > 0 {
			w += termwidth.Of(tabMore + tabGap)
		}
		if hi < len(names)-1 {
			w += termwidth.Of(tabGap + tabMore)
		}
		return w
	}
	lo, hi := cur, cur
	if width(lo, hi) > room {
		return tab(termwidth.Truncate(names[cur], max(room-pillWidth, 1), "…"), true)
	}
	for grew := true; grew; {
		grew = false
		if hi+1 < len(names) && width(lo, hi+1) <= room {
			hi, grew = hi+1, true
		}
		if lo > 0 && width(lo-1, hi) <= room {
			lo, grew = lo-1, true
		}
	}
	var parts []string
	if lo > 0 {
		parts = append(parts, reset(muted(tabMore)))
	}
	for i := lo; i <= hi; i++ {
		parts = append(parts, tab(names[i], i == cur))
	}
	if hi < len(names)-1 {
		parts = append(parts, reset(muted(tabMore)))
	}
	return strings.Join(parts, tabGap)
}

func (m *Model) noteLine() string {
	c := m.chans[m.cur]
	var s string
	switch {
	case c.err != nil && c.page != nil:
		s = "🚨 " + c.err.Error()
	case c.page != nil:
		// 期間の行は注記があっても隠さない (注記は後ろへ足す)
		s = fmt.Sprintf("読んだ期間 %s 〜 %s / %s より前に始まったスレッドは含まない",
			c.page.Since.Format("01/02 15:04"), c.page.Until.Format("01/02 15:04"), c.page.Lookback.Format("01/02"))
		if len(c.page.Notes) > 0 {
			s += " / 🚨 " + c.page.Notes[0]
			if len(c.page.Notes) > 1 {
				s += fmt.Sprintf(" (ほか %d 件)", len(c.page.Notes)-1)
			}
		}
	}
	return reset(muted(termwidth.Truncate(termsafe.PlainLine(s), m.w, "…")))
}

func (m *Model) rightLines() []string {
	title := ""
	if t, ok := m.selected(); ok {
		title = termsafe.PlainLine(t.Title)
		if mk := render.Mark(t); mk != "" {
			title = mk + ": " + title
		}
	}
	lines, first := m.bodyLines()
	m.pager.Clamp(len(lines), m.bodyRows())
	off := min(m.pager.DrawOffset(len(lines), m.bodyRows()), len(lines))
	// 色付けは画面に出す行だけ (全行を毎回の描画で照合すると、長い話題で名前の数 × 字数の手間が描画のたびにかかる)。
	// 写しに付ける: lines は bodyCache と共有している
	shown := slices.Clone(lines[off:min(off+m.bodyRows(), len(lines))])
	for i := range shown {
		if off+i < len(first) {
			shown[i] = m.chans[m.cur].hl.line(shown[i], first[off+i])
		}
	}
	// 本文が欄に収まらないときだけ右端に出る (収まるなら Scrollbar は行をそのまま返す)
	shown = layout.Scrollbar(shown, m.bodyInner(), len(lines), off, true)
	head := []string{boldText(termwidth.Truncate(title, m.bodyInner(), "…")), gradRule(m.bodyInner(), cTeal, cPurple, cPink, surfDetail)}
	edge := edgeOff
	if m.body {
		edge = edgeBodyOn
	}
	return shadow(card(append(head, shown...), m.rightWidth(), m.cardRows(), cardPadX(m.w), surfDetail, edge))
}

// helpLines はキーの案内の 2 行 (1 行目は話題と本文の操作、2 行目はチャンネルの操作と抜け方)。書き方は TUI ガイドの「キー: 説明」で、
// キーを桃色、説明を薄く描き、空白 2 つで区切る (「 • 」で区切ると、本文の欄の 1 行目が幅 100 に入らず u が落ちる)。語は略さずに書き (「s 購読」では何の画面か伝わらなかった)、幅に入らない項目は
// hintfit が優先度の低い方から落とす。抜ける手段 (一覧では q で終わる、本文の欄では h/q で一覧へ戻る) は最後まで残す。
// s (購読の画面) は上の行の右端のキーで案内する
func (m *Model) helpLines() []string {
	h := func(prio int, key, desc string) hintfit.Item {
		return hintfit.Item{Prio: prio, Text: hintText(key, desc)}
	}
	var pane []hintfit.Item
	if m.body {
		pane = []hintfit.Item{h(2, "j/k", "本文を送る"), h(3, "J/K", "隣の話題へ"), h(1, "h/q", "話題の一覧へ戻る")}
	} else {
		pane = []hintfit.Item{h(2, "j/k", "話題を選ぶ"), h(2, "l/Enter", "本文を読む")}
	}
	// 本文の欄では Tab が 1 行目に入らず幅 100 で落ちるが、案内に無くても効く
	pane = append(pane, h(3, "o", "Slack で開く"), h(3, "u", "既読と未読の切り替え"), h(4, "x", "話題を隠す"), h(5, "z", zHint(m.showHidden)))
	chans := []hintfit.Item{h(4, "Tab", "次のチャンネル"), h(3, "r", "今のチャンネルを読み直す"), h(4, "R", "全チャンネルを読み直す")}
	if m.subs != nil { // d が効くのは購読を変えられる起動だけ (案内に出したキーは必ず効く。TUI ガイド)
		chans = append(chans, h(5, "d", "購読を外す"))
	}
	if !m.body {
		chans = append(chans, h(1, "q", "終わる"))
	}
	out := make([]string, 0, helpRows)
	for _, items := range [][]hintfit.Item{pane, chans} {
		out = append(out, reset(termwidth.Truncate(" "+hintfit.Fit(max(m.w-1, 1), "  ", items), m.w, "…")))
	}
	return out
}

// helpRows はキーの案内の行数。
const helpRows = 2

// View は画面を出す。
func (m *Model) View() tea.View {
	v := tea.NewView(strings.Join(m.Lines(), "\n"))
	v.AltScreen = true
	if m.picker != nil {
		v.Cursor = m.picker.Cursor() // 検索の語を打っている間だけ、検索の欄に置く (それ以外は nil で隠す)
	}
	return v
}

// Run は画面を開き、閉じるまで待つ。tmux の中で外側の端末が 24bit の色に対応していれば、bubbletea の判定 (tmux の中では 256 色に落とす)
// を引き上げる (カードの面と枠の色の流れが 256 色では段になる)。
func Run(m *Model) error {
	var opts []tea.ProgramOption
	if termcolor.TrueColorThroughTmux(os.Getenv("TMUX"), subproc.Output) {
		opts = append(opts, tea.WithColorProfile(colorprofile.TrueColor))
	}
	_, err := tea.NewProgram(m, opts...).Run()
	return err
}

// unsubIndex は d で購読を外すか確かめているチャンネルのタブの番号 (確かめていない・もうタブに無いなら -1)。
func (m *Model) unsubIndex() int {
	if m.unsub == "" {
		return -1
	}
	return slices.IndexFunc(m.chans, func(c chanState) bool { return c.ch.ID == m.unsub })
}

// confirmUnsubscribe は購読を外す確認の板のキー: y / Enter で外して保存し、それ以外は取り消す (confirm.IsYes)。
func (m *Model) confirmUnsubscribe(k string) tea.Cmd {
	i := m.unsubIndex()
	m.unsub = ""
	if i < 0 || !confirm.IsYes(k) {
		m.toast.Show("購読を外すのをやめた", true)
		return nil
	}
	name := m.chans[i].ch.Label()
	sel := make([]subs.Channel, 0, len(m.chans)-1)
	for j, c := range m.chans {
		if j != i {
			sel = append(sel, c.ch)
		}
	}
	if err := m.subs.Save(sel); err != nil {
		m.toast.Show("購読を保存できなかった (外していない): "+err.Error(), false)
		return nil
	}
	cmd := m.setChannels(sel)
	m.toast.Show(name+" の購読を外した (s でまた付けられる)", true)
	return cmd
}

// reselect は一覧の行を作り直した後、最初の単位が keep の話題にカーソルを戻し、戻せたかを返す。戻せなければ (話題が消えた・隠れた)
// 本文の欄を閉じる: 黙って別の話題の本文に替えない (替えると、選んでいない話題を読んだことになる)。
func (m *Model) reselect(keep string) bool {
	topics := m.topics()
	for i, r := range m.rows() {
		if !r.header() && slices.ContainsFunc(topics[r.topic].Units, func(u board.Unit) bool { return u.ID == keep }) {
			m.list.Cursor = i
			m.fitList(1)
			return true
		}
	}
	m.fitList(1)
	m.body = false
	return false
}
