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
	"github.com/jiikko/dotfiles/src/tuikit/anim"
	"github.com/jiikko/dotfiles/src/tuikit/confirm"
	"github.com/jiikko/dotfiles/src/tuikit/hintfit"
	"github.com/jiikko/dotfiles/src/tuikit/layout"
	"github.com/jiikko/dotfiles/src/tuikit/listnav"
	"github.com/jiikko/dotfiles/src/tuikit/spinner"
	"github.com/jiikko/dotfiles/src/tuikit/termcaps"
	"github.com/jiikko/dotfiles/src/tuikit/termcolor"
	"github.com/jiikko/dotfiles/src/tuikit/termsync"
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
	stale    bool // page はキャッシュから出した前回の板で、この起動ではまだ読み直せていない
	// redo は、走っている読み直しを頼んだ後にグループのメンバーが変わったこと。その結果は前のメンバーの板なので捨て、読み直す
	// (走っている読み直しを追い越してもう 1 本起こさない: 2 本が並ぶと、古い方が後で終わってキャッシュを前のメンバーの板で上書きする)
	redo   bool
	hl     highlighter       // page の本文の色付け (page を置き換えるたびに作り直す)
	rows   []row             // 左の一覧の行 (日付の見出しと話題)。nil なら Model.rows が作り直す (page・隠した話題・z を変えたら捨てる)
	read   map[string]string // 既読の印 (単位の ID → 読んだときの単位の最後の発言の ts)
	hidden map[string]string // x で隠した単位 (単位の ID → 隠したときの単位の最後の発言の ts)
	// before は単位を最後に読んだとき、その前に読んでいた所 (単位の ID → それまでの既読の印)。本文の「ここから新着」の区切りに使う。
	// 印を進めるたびに書き直す (この起動の間だけ。次の起動では、既読の印そのものが前に読んだ所になる)
	before map[string]string
	// beforeGen は before を書き直した回数 (本文の折り返しの覚えを捨てる鍵)
	beforeGen int
	// unread は未読の話題の数の覚え (タブに出す。描くたびに全チャンネルの全発言を数え直さない)。page か read を変えたら捨てる
	unread *int
	// mentions は単位の ID → その単位の発言への言及 (page を置き換えるたびに作り直す。未読と既読の印に使う)
	mentions map[string][]render.Mention
}

// Model は画面の状態。
type Model struct {
	ctx     context.Context
	load    Loader
	open    func(url string) error
	now     func() time.Time
	chans   []chanState
	cur     int
	list    listnav.List
	pager   listnav.Pager
	body    bool       // 右の欄を操作しているか
	opening anim.Slide // 本文の欄を開く演出と一覧へ戻る逆の演出 (一覧との境目が左右へ動く。openingRows)
	// showHidden は x で隠した話題も一覧に出すか (z で切り替える。出すときは淡い色)
	showHidden bool
	// stream は新着のタブ (全部のタブの話題を新着順に並べる。タブの最後) を開いているか。開いている間も cur は最後に見たチャンネル
	stream bool
	// streamRows は新着のタブの行。nil なら rows が作り直す。並べ直すのは新着のタブを開いたとき・読み直しが終わったとき・隠した話題を
	// 変えたときだけ: 選ぶたびに既読が変わって並べ直すと、選んだ話題が下へ動いてカーソルが跳ぶ
	streamRows []row
	// restore は起動のときに戻す話題 (Start。selKey の形)。板が届くたびに探し、戻せたら空にする。人がキーを押しても空にする
	// (押した後に届いた板で、カーソルを前回の話題へ奪い返さない)
	restore string
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
	seq      int
	subs     *Subscriptions
	picker   *subs.Picker          // 購読の画面を開いている間だけ非 nil
	picking  bool                  // 購読の画面の一覧を読んでいる
	apiStats func() (string, bool) // 右下に出す Slack の呼び出しの様子 (SetAPIStats)
	// pickCancel は購読の画面で走らせている最終投稿の取り直しを止める (画面を閉じたとき。nil なら走らせていない)
	pickCancel context.CancelFunc
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
	// Save は購読を保存する。from は購読の画面の Base (画面を開いたときの購読の印)。空なら読む画面のタブを作ったときの購読と照合する。
	// 開いてから別の操作が書き換えていたら subs.ErrChanged を返して書かない
	Save func(sel []subs.Channel, from string) error
	// Posts は参加しているチャンネルの最終投稿を、画面を開いている間に裏で取り直す口 (nil なら取り直さない)
	Posts *subs.LastPosts
}

// New は画面を作る。chans は 1 つ以上。
func New(ctx context.Context, chans []subs.Channel, load Loader, open func(string) error, now func() time.Time) *Model {
	m := &Model{ctx: ctx, load: load, open: open, now: now, w: 100, h: 30, after: tea.Tick}
	m.toast.InfoColor = toastInfoColor
	for _, c := range chans {
		m.chans = append(m.chans, chanState{ch: c, read: map[string]string{}, hidden: map[string]string{}, before: map[string]string{}})
	}
	return m
}

// SetAPIStats は右下に出す Slack の呼び出しの様子を決める。f は出す文字列と、描き直し続けるか (直近に呼び出しがある) を返す。
// 購読の画面にも同じ文字列を出す。nil なら出さない。
func (m *Model) SetAPIStats(f func() (string, bool)) { m.apiStats = f }

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

// Init は開いたタブ (Start。無ければ先頭) だけを読む (他は開いたときに読む。起動のたびに全チャンネル分 claude を呼ばない)。
// 新着のタブで起動したら、全部のタブを読む: 新着のタブは全部のタブの話題を並べるので、前回の板 (Preload) のままのタブも読み直さないと、
// 起動しても新しい話題が 1 つも来ない (Tab で新着のタブへ入るときは、それまでに開いたタブが読み直されている)。
// 分類は新しい発言が無ければ claude を呼ばない (main.go の classify) ので、費用が増えるのは新しい発言のあるタブだけ。
func (m *Model) Init() tea.Cmd {
	if len(m.chans) == 0 {
		return tea.Quit
	}
	if !m.stream {
		m.restoreTopic() // 前回の板 (キャッシュ) があれば、読み直しを待たずに戻す
		return m.reload(m.cur)
	}
	cmds := []tea.Cmd{m.enterStream()}
	for i := range m.chans {
		if c := &m.chans[i]; c.stale && !c.loading {
			cmds = append(cmds, m.reload(i))
		}
	}
	m.restoreTopic() // enterStream が一覧を先頭へ戻した後に戻す
	return tea.Batch(cmds...)
}

// StreamTab は新着のタブを指す Tab / Start の値 (チャンネルとグループの ID は英大文字か grp で始まるので重ならない)。
const StreamTab = "stream"

// Start は起動のときに最初に開くタブと選ぶ話題 (前回最後に見ていた所。Tab と Topic が返した値) を決める。Init より前に呼ぶ。
// 購読に無いタブ (外した・消えた) なら先頭のタブのまま。話題は板が届いたときに探し、無ければ (消えた・隠した) 先頭の話題のまま。
// 戻した話題は既読にしない (起動で先頭に乗ったときと同じ。本文の欄も開かない)。
func (m *Model) Start(tab, topic string) {
	m.restore = topic
	if tab == StreamTab && len(m.chans) > 0 {
		m.stream = true
		return
	}
	if i := slices.IndexFunc(m.chans, func(c chanState) bool { return c.ch.ID == tab }); i >= 0 {
		m.cur = i
	}
}

// Tab は今見ているタブ (チャンネルかグループの ID、新着のタブなら StreamTab。タブが無ければ空)。
func (m *Model) Tab() string {
	switch {
	case len(m.chans) == 0:
		return ""
	case m.stream:
		return StreamTab
	}
	return m.chans[m.cur].ch.ID
}

// Topic は今選んでいる話題 (Start に渡す値。選んでいなければ空)。前回の話題をまだ戻せていない (板が届く前・キーを押す前) なら、その話題。
func (m *Model) Topic() string {
	if m.restore != "" {
		return m.restore
	}
	return m.selKey()
}

// restoreTopic は Start の話題がまだ戻せていなければ、今の一覧から探してカーソルを置く。戻せたら探すのをやめる。
// 戻せないとき reselect は本文の欄を閉じるが、ここに来るのはキーを押す前 (欄はまだ開いていない) だけ。キー以外で欄を開く経路を足すなら見直す。
func (m *Model) restoreTopic() {
	if m.restore == "" || len(m.chans) == 0 {
		return
	}
	if m.reselect(m.restore) {
		m.restore = ""
		return
	}
	// 持ち主のタブが無い (購読から外した) か、読み直した板にも無い (消えた・隠した) なら諦める。持ち続けると、先頭の話題を見て
	// 終えたときに古い話題を覚え直す。前回の板 (キャッシュ) のままなら読み直しを待つ
	ch, _, _ := strings.Cut(m.restore, "|")
	if i := slices.IndexFunc(m.chans, func(c chanState) bool { return c.ch.ID == ch }); i < 0 || (m.chans[i].page != nil && !m.chans[i].stale) {
		m.restore = ""
	}
}

// Update はメッセージを処理する。
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.pager.Stop() // 折り返しが変わると、滑っている途中の行の番号が別の行を指す
	case loadedMsg:
		if cmd := m.loaded(msg); cmd != nil {
			return m, tea.Batch(append(m.frameCmds(), cmd)...)
		}
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
				m.picker.SetNote(msg.p.Note())
				return m, m.picker.ResumeFetch()
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
		return m, m.startFetch()
	case subs.LastPostMsg:
		if m.picker != nil {
			return m, m.picker.HandleLastPost(msg)
		}
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
			return m, tea.Quit // 戻す予定は残す: 戻す前に終えたら、Topic は前回の話題を返す (先頭の話題・空で覚えを上書きしない)
		}
		m.restore = "" // 人が動かし始めたら、後から届いた板で前回の話題へ引き戻さない
		return m, tea.Batch(append(m.frameCmds(), cmd)...)
	case toast.Msg:
		m.toast.StartLeaving(msg)
	case tickMsg:
		// 追い越された古い tick でも描き直しはする。張り直すかは frameCmds (Want) が決める。半ページ送りはフレームの数で進むので、
		// 古い tick では進めない (速い tick を張り直した後に遅い tick が届くと、1 コマ余計に進む)
		if m.ticks.Fired(msg.seq) {
			m.pager.Advance()
		}
		if m.opening.CloseLanded(m.now()) {
			m.opening.EndClose()
		}
	}
	return m, tea.Batch(m.frameCmds()...)
}

// LastToast は最後に出した知らせの文 (出していなければ空)。
func (m *Model) LastToast() string { return m.toast.Text() }

// tickMsg は描き直しの合図。seq は張ったときの ticks の番号。
type tickMsg struct{ seq uint64 }

const (
	// bodyOpenDuration は本文の欄を開く演出と、一覧へ戻る逆の演出の長さ。bodyGlideFrames は本文の半ページ送りを滑らせるフレーム数
	// (× frameInterval ≒ 150ms)。どちらも試作を触って選んだ長さで、glogx (開く 175ms・スクロール約 100ms) より遅い
	bodyOpenDuration = 300 * time.Millisecond
	bodyGlideFrames  = 9
	frameInterval    = 16 * time.Millisecond // toast が滑り込む・滑り出る間の描き直しの周期
	spinnerStep      = 80 * time.Millisecond // 回る印の 1 コマ (読み直し・一覧の読み込みを待つ間の描き直しの周期)
)

// frameCmds は toast を 1 コマ進め、退場の時計と、動くものがある間の tick を張る。
func (m *Model) frameCmds() []tea.Cmd {
	var cmds []tea.Cmd
	for _, t := range m.toast.Advance() {
		cmds = append(cmds, m.after(t.After, func(time.Time) tea.Msg { return t.Msg }))
	}
	var d time.Duration
	switch {
	case m.toast.Animating() || m.opening.Unsettled(m.now()) || m.pager.Animating():
		d = frameInterval
	case m.busy():
		d = spinnerStep
	case m.apiActive():
		d = time.Second // 右下の呼び出しの頻度を、何もしないでいる間も 1 秒ごとに描き直す (直近 1 分の数が減っていく)
	default:
		return cmds
	}
	if seq, arm := m.ticks.Want(m.now(), d); arm {
		cmds = append(cmds, m.after(d, func(time.Time) tea.Msg { return tickMsg{seq: seq} }))
	}
	return cmds
}

// apiActive は右下の呼び出しの様子が動いているか (直近 1 分に呼び出しがある・待っている・止めている)。
func (m *Model) apiActive() bool {
	if m.apiStats == nil {
		return false
	}
	_, active := m.apiStats()
	return active
}

// busy は回る印を出しているか (どれかのチャンネルを読み直している・購読の画面の一覧を読んでいる)。
func (m *Model) busy() bool {
	return m.picking || slices.ContainsFunc(m.chans, func(c chanState) bool { return c.loading })
}

// spin は回る印と、from からの経過秒 (「⠋ 12 秒」)。数十秒かかる読み直しを、始まりの知らせ (数秒で消える) だけにしない
func (m *Model) spin(from time.Time) string {
	return fmt.Sprintf("%s %d 秒", spinner.At(m.now(), spinnerStep), int(m.now().Sub(from)/time.Second))
}

func (m *Model) loaded(msg loadedMsg) tea.Cmd {
	idx := slices.IndexFunc(m.chans, func(c chanState) bool { return c.ch.ID == msg.ch })
	if idx < 0 {
		return nil // 読んでいる間に購読から外した
	}
	c := &m.chans[idx]
	if msg.gen != c.gen {
		return nil // 後から頼んだ読み直しに追い越された
	}
	c.loading = false
	if c.redo {
		return m.membersChanged(idx)
	}
	if msg.err != nil {
		c.err = msg.err
		verb := "読み直せなかった"
		if c.page == nil {
			verb = "読めなかった"
		}
		m.toast.Show(c.ch.Label()+" を"+verb+": "+msg.err.Error(), false)
		return nil
	}
	keep, owner := m.selKey(), m.selOwner()
	verb := "読み直した"
	if c.page == nil {
		verb = "読んだ"
	}
	c.setPage(msg.page, msg.at, false)
	m.toast.Show(fmt.Sprintf("%s を%s (未読の話題 %d 件)", c.ch.Label(), verb, m.unreadCount(idx)), true)
	if m.stream {
		// 新着のタブは読み直しが終わったら並べ直し、見ていた話題に戻す。読み直したタブの話題を見ていたなら、本文は先頭から
		m.streamRows = nil
		m.reselect(keep)
		if owner == idx {
			m.pager.Reset()
		}
		m.restoreTopic()
		return nil
	}
	if idx == m.cur {
		// 読み直しで話題が並び替わっても、見ていた話題 (最初の単位で同一視する) にカーソルを戻す。無くなっていたら先頭へ
		m.list = listnav.List{}
		m.reselect(keep)
		m.pager.Reset()
		m.restoreTopic()
		// 読み直しは既読にしない (本文の欄に出ていても): 欄は先頭へ戻り、付いた返信は欄の外にありうる。小さい端末・購読の画面の裏では
		// 欄が描かれていない。読んでいる最中に付いた返信は、選び直すまで ● で出す
	}
	return nil
}

// Page はチャンネル i の最後に読めた板 (まだ読めていなければ ok が偽)。
func (m *Model) Page(i int) (render.Page, bool) {
	if i < 0 || i >= len(m.chans) || m.chans[i].page == nil {
		return render.Page{}, false
	}
	return *m.chans[i].page, true
}

// ownerOf は行の話題を持つタブの番号 (チャンネルのタブの行は今のタブ)。
func (m *Model) ownerOf(r row) int {
	if r.owner > 0 {
		return r.owner - 1
	}
	return m.cur
}

// selRow は選んだ行 (見出しなら ok が偽)。
func (m *Model) selRow() (row, bool) {
	rows := m.rows()
	if len(rows) == 0 {
		return row{}, false
	}
	r := rows[min(max(m.list.Cursor, 0), len(rows)-1)]
	if r.header() {
		return row{}, false // 描く前 (fitList の前) に見出しに乗っている
	}
	return r, true
}

// selOwner は選んだ話題を持つタブの番号 (選んでいなければ今のタブ)。既読・隠す・Slack で開く・本文は、このタブの板と印を使う。
func (m *Model) selOwner() int {
	if r, ok := m.selRow(); ok {
		return m.ownerOf(r)
	}
	return m.cur
}

func (m *Model) selected() (board.Topic, bool) {
	r, ok := m.selRow()
	if !ok {
		return board.Topic{}, false
	}
	p := m.chans[m.ownerOf(r)].page
	if p == nil || r.topic >= len(p.Topics) {
		return board.Topic{}, false
	}
	return p.Topics[r.topic], true
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
			m.closeBody()
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
		if m.stream && !m.hasSel() {
			m.toast.Show("新着のタブでは、読み直すタブの話題を選んでから r を押す (R で全部を読み直す)", false)
			return nil
		}
		oi := m.selOwner() // 新着のタブでは、選んだ話題のタブを読み直す
		if m.chans[oi].loading {
			// 走っている読み直しを待つ (連打で claude を何本も起こさない)
			m.toast.Show(m.chans[oi].ch.Label()+" は読み込み中なので、読み直せない (終わってからもう一度押す)", false)
			return nil
		}
		return m.reload(oi)
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
		// 新着のタブは最後 (len(m.chans) 番目) に置く
		pos := m.cur
		if m.stream {
			pos = len(m.chans)
		}
		if next := listnav.Cycle(pos, d, len(m.chans)+1); next < len(m.chans) {
			return m.switchTo(next)
		}
		return m.enterStream()
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
		if m.stream && !m.hasSel() {
			m.toast.Show("新着のタブでは、購読を外すタブの話題を選んでから d を押す", false)
			return nil
		}
		m.unsub = m.chans[m.selOwner()].ch.ID // 新着のタブでは、選んだ話題のタブ (確認の板に名前を出す)
		return nil
	}
	if m.body {
		switch k {
		case "h", "left", "esc", "enter":
			m.closeBody()
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
			m.pager.Move(mo, len(lines), m.bodyRows(), bodyGlideFrames) // 半ページ送りだけ滑らせる (1 行送りは動く余地が無い)
		}
		return nil
	}
	switch k {
	case "l", "right", "enter":
		if _, ok := m.selected(); ok {
			m.body = true
			m.opening.EndClose() // 閉じる途中に開き直すと Open は何もしないので、先に閉じ切らせる
			m.opening.Open(m.now(), bodyOpenDuration)
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
	if i == m.cur && !m.stream {
		return nil
	}
	m.cur, m.body, m.stream = i, false, false
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
	var fetch tea.Cmd
	if m.subs.Cached != nil {
		if p, at, ok := m.subs.Cached(); ok {
			p.SetCachedNote(at)
			m.picker, m.pickCached, cached = p, true, true
			fetch = m.startFetch()
		}
	}
	if m.picking {
		return fetch
	}
	m.picking, m.pickStarted = true, m.now()
	if !cached {
		// 押した手応えは知らせで返す (数秒かかる一覧の読み込みを、上の状態の行の回る印だけにしない)
		m.toast.ShowInfo("チャンネルの一覧を読んでいます…")
	}
	open, ctx := m.subs.Open, m.ctx
	return tea.Batch(fetch, func() tea.Msg {
		p, err := open(ctx)
		return pickerMsg{p: p, err: err}
	})
}

// startFetch は開いた購読の画面で、参加しているチャンネルの最終投稿の取り直しを始める。画面を閉じたら (closePicker) 取り消す。
func (m *Model) startFetch() tea.Cmd {
	if m.pickCancel != nil {
		m.pickCancel()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.pickCancel = cancel
	return m.picker.StartFetch(ctx, m.subs.Posts)
}

// pickKey は購読の画面を開いている間のキー。保存したら画面を閉じずにタブを入れ替える。
func (m *Model) pickKey(k, text string) tea.Cmd {
	p := m.picker
	p.Press(k, text)
	if !p.Done() {
		return nil
	}
	m.picker = nil
	if m.pickCancel != nil {
		m.pickCancel() // 閉じた画面の最終投稿の取り直しを止める (Slack の呼び出しの列を空ける)
		m.pickCancel = nil
	}
	if !p.Saved() {
		return nil
	}
	sel := p.Selected()
	if err := m.subs.Save(sel, p.Base()); err != nil {
		m.toast.Show("購読するチャンネルを保存できなかった: "+err.Error(), false)
		return nil
	}
	return m.setChannels(sel)
}

// setChannels はタブを購読 sel に合わせる。残るチャンネルは読んだ板と読み直しの状態をそのまま持ち (走っている読み直しの結果も
// ID で照合して受け取る)、新しいチャンネルは開いたときに読む。見ていたチャンネルが残れば、そのまま見続ける。
func (m *Model) setChannels(sel []subs.Channel) tea.Cmd {
	keep := m.selKey() // 新着のタブは並びが変わるので、選んでいた話題に戻す (戻せなければ本文を閉じる)
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
	changed := map[string]bool{}
	for _, ch := range sel {
		c, ok := old[ch.ID]
		if ok && !sameMembers(c.ch, ch) {
			changed[ch.ID] = true
		}
		c.ch = ch
		next = append(next, c)
		if !ok {
			defer m.loadRead(len(next) - 1) // m.chans を next に置き換えた後に読む
		}
	}
	m.chans = next
	m.streamRows = nil
	if len(next) == 0 {
		m.stream = false
	}
	m.toast.Show(fmt.Sprintf("購読するチャンネルを保存した (%d 件)", len(next)), true)
	i := slices.IndexFunc(next, func(c chanState) bool { return c.ch.ID == curID })
	switch {
	case i >= 0:
		m.cur = i
	case m.stream:
		m.cur = 0 // 新着のタブの一覧と本文は選んでいた話題で決める (下の reselect)。最後に見たチャンネルが消えても閉じない
	default:
		m.cur, m.body = 0, false
		m.list = listnav.List{}
		m.pager.Reset()
	}
	var cmds []tea.Cmd
	for j := range m.chans {
		switch {
		case !changed[m.chans[j].ch.ID]:
		case m.chans[j].loading:
			m.chans[j].redo = true // 走っている読み直しが終わったら、その結果を捨てて読み直す (loaded)
		default:
			cmds = append(cmds, m.membersChanged(j))
		}
	}
	if i < 0 && len(next) > 0 {
		if c := &m.chans[0]; (c.page == nil || c.stale) && !c.loading && c.err == nil {
			cmds = append(cmds, m.reload(0))
		}
	}
	if m.stream {
		m.reselect(keep) // 先頭へ戻した後でも、選んでいた話題が残っていればそこへ戻す
		for j := range m.chans {
			// 新着のタブは全部のタブの板を並べるので、足したタブ (板がまだ無い) もすぐ読み始める (enterStream と同じ)
			if c := &m.chans[j]; c.page == nil && !c.loading && c.err == nil {
				cmds = append(cmds, m.reload(j))
			}
		}
	}
	return tea.Batch(cmds...)
}

// openSelected は選んだ話題の最後の発言を Slack で開く。
func (m *Model) openSelected() {
	t, ok := m.selected()
	p := m.chans[m.selOwner()].page
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
func (m *Model) rightWidth() int {
	if m.body || m.closingBody() {
		return m.w - 1 // 本文を読む間は左の一覧を出さず、本文の欄を全幅にする (screen)
	}
	return m.w - m.leftWidth() - 3
}
func (m *Model) bodyInner() int { return m.rightWidth() - cardChrome(cardPadX(m.w)) } // 本文のカードの中身の幅

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
	if m.stream {
		if _, ok := m.selRow(); !ok {
			return []string{"新着の話題はまだありません (タブを読み込むと、ここに全部のタブの話題が新しい順に並ぶ)。"}, nil
		}
	}
	oi := m.selOwner()
	c := m.chans[oi]
	if c.page == nil {
		switch {
		case c.err != nil:
			return wrap(termsafe.PlainLine("読めなかった: "+c.err.Error()), m.bodyWidth()), nil
		default:
			return []string{"読んでいます…"}, nil
		}
	}
	r, ok := m.selRow()
	ti := r.topic
	if !ok {
		if len(c.page.Topics) > 0 {
			return []string{"話題は全部隠している (z で隠した話題も出す)。"}, nil
		}
		return []string{"期間内の発言はありません。"}, nil
	}
	key := bodyKey{page: c.page, topic: ti, width: m.bodyWidth(), before: c.beforeGen}
	if m.body1.key == key {
		return m.body1.lines, m.body1.first
	}
	for _, l := range c.page.BodySince(c.page.Topics[ti], false, c.before) {
		for i, w := range wrap(l, m.bodyWidth()) { // 色は rightLines が、折り返した後の画面に出す行にだけ付ける
			lines, first = append(lines, w), append(first, i == 0)
		}
	}
	m.body1 = bodyCache{key: key, lines: lines, first: first}
	return lines, first
}

// bodyKey は本文の欄の折り返しの覚えの鍵。板 (読み直すと別の値になる)・話題・幅が同じなら同じ行になる。
type bodyKey struct {
	page   *render.Page
	topic  int
	width  int
	before int // chanState.beforeGen (「ここから新着」の区切りの位置が変わったら折り返し直す)
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
		return []string{termwidth.Truncate(m.chans[i].ch.Label()+unsubVerb(m.chans[i].ch)+"？ y/Enter: はい  その他: やめる", max(m.w, 1), "…")}
	}
	if i := m.unsubIndex(); i >= 0 {
		box := confirm.Dialog(" 購読を外す ", append([]string{m.chans[i].ch.Label()}, unsubBody(m.chans[i].ch)...), confirm.HintYesNo, m.w, true)
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
		if m.apiStats != nil {
			m.picker.SetStatus(func() string { s, _ := m.apiStats(); return s })
		}
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
	right := m.rightLines()
	if (m.body && m.opening.Unsettled(m.now())) || m.closingBody() {
		return append(append(lines, m.openingRows(right)...), m.helpLines()...)
	}
	if m.body {
		// 本文を読む間は左の一覧を出さない。一覧と本文を同じ行に並べると、本文が 1 行動くたびに左の一覧の分まで描き直しになり、
		// tmux から外側の端末へ送る量が 1 行で約 53KB になる (全幅なら約 5KB で less と同じ水準。Terminal.app でスクロールが
		// もっさりした)。一覧に戻るのは h / q
		for i := range m.paneRows() {
			r := ""
			if i < len(right) {
				r = right[i]
			}
			lines = append(lines, r)
		}
		return append(lines, m.helpLines()...)
	}
	left := m.leftLines()
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
	names := make([]string, len(m.chans), len(m.chans)+1) // 最後に新着のタブを足す
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
	cur := m.cur
	if m.stream {
		cur = len(names)
	}
	stream := "🆕 新着"
	if n := m.streamUnread(); n > 0 {
		stream += fmt.Sprintf("  未読 %d", n)
	}
	names = names[:len(m.chans)+1]
	names[len(m.chans)] = stream
	return tabWindow(names, cur, m.w)
}

func (m *Model) state() string {
	if m.stream {
		return m.streamState()
	}
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
	c := m.chans[m.selOwner()]
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
			shown[i] = m.chans[m.selOwner()].hl.line(shown[i], first[off+i])
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
	rHint := "今のチャンネルを読み直す"
	if m.stream {
		rHint = "選んだ話題のタブを読み直す"
	}
	chans := []hintfit.Item{h(4, "Tab", "次のタブ (最後は新着)"), h(3, "r", rHint), h(4, "R", "全チャンネルを読み直す")}
	if m.subs != nil { // d が効くのは購読を変えられる起動だけ (案内に出したキーは必ず効く。TUI ガイド)
		chans = append(chans, h(5, "d", "購読を外す"))
	}
	if !m.body {
		chans = append(chans, h(1, "q", "終わる"))
	}
	stats := ""
	if m.apiStats != nil {
		stats, _ = m.apiStats()
	}
	out := make([]string, 0, helpRows)
	for i, items := range [][]hintfit.Item{pane, chans} {
		if i == 1 && stats != "" {
			out = append(out, reset(subs.HintLine(" ", items, stats, m.w, dim)))
			continue
		}
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

// Run は画面を開き、閉じるまで待つ。
func Run(m *Model) error {
	_, err := tea.NewProgram(m, ProgramOptions(os.Getenv, subproc.Output)...).Run()
	return err
}

// ProgramOptions は slack-bbs の画面 (読む画面と購読の画面) の Program に渡す設定。
//   - tmux の中で外側の端末が 24bit の色に対応していれば、bubbletea の判定 (tmux の中では 256 色に落とす) を引き上げる
//     (カードの面と枠の色の流れが 256 色では段になる)
//   - 外側の端末が同期出力を持たなければ、bubbletea に同期出力を使わせない。使うと tmux は同期出力の区間が終わるたびにペインを
//     描き直し、本文の 1 行のスクロールで外側の Terminal.app へ約 199KB を書く (止めると約 54KB)
//
// どちらも tmux に外側の端末の機能を聞くので、答えを使い回す (termcaps.Memo)。
func ProgramOptions(getenv func(string) string, run termcaps.Runner) []tea.ProgramOption {
	run = termcaps.Memo(run)
	var opts []tea.ProgramOption
	if termcolor.TrueColorThroughTmux(getenv("TMUX"), run) {
		opts = append(opts, tea.WithColorProfile(colorprofile.TrueColor))
	}
	return append(opts, termsync.ProgramOptions(!termcaps.SyncedOutput(getenv, run))...)
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
	gone := m.chans[i].ch
	sel := make([]subs.Channel, 0, len(m.chans)-1)
	for j, c := range m.chans {
		if j != i {
			sel = append(sel, c.ch)
		}
	}
	if err := m.subs.Save(sel, ""); err != nil {
		m.toast.Show("購読を保存できなかった (外していない): "+err.Error(), false)
		return nil
	}
	cmd := m.setChannels(sel)
	if gone.IsGroup() {
		m.toast.Show(gone.Label()+" のグループを消した (メンバーのチャンネルの購読は変わらない)", true)
	} else {
		m.toast.Show(gone.Label()+" の購読を外した (s でまた付けられる)", true)
	}
	return cmd
}

// selKey は選んだ話題の鍵 (持ち主のタブの ID と最初の単位の ID。reselect に渡す)。選んでいなければ空。
// タブの ID まで持つのは、新着のタブで同じチャンネルの同じ単位が、単独の購読とグループの両方から並びうるため。
func (m *Model) selKey() string {
	if t, ok := m.selected(); ok {
		return m.chans[m.selOwner()].ch.ID + "|" + t.Units[0].ID
	}
	return ""
}

// reselect は一覧の行を作り直した後、selKey が keep の話題にカーソルを戻し、戻せたかを返す。戻せなければ (話題が消えた・隠れた)
// 本文の欄を閉じる: 黙って別の話題の本文に替えない (替えると、選んでいない話題を読んだことになる)。
func (m *Model) reselect(keep string) bool {
	ch, unit, _ := strings.Cut(keep, "|")
	for i, r := range m.rows() {
		if r.header() {
			continue
		}
		c := &m.chans[m.ownerOf(r)]
		if c.ch.ID == ch && c.page != nil && slices.ContainsFunc(c.page.Topics[r.topic].Units, func(u board.Unit) bool { return u.ID == unit }) {
			m.list.Cursor = i
			m.fitList(1)
			return true
		}
	}
	m.fitList(1)
	m.body = false
	return false
}

// sameMembers は a と b のグループのメンバーが同じ (並びも) か。チャンネルはメンバーを持たないので常に真。
func sameMembers(a, b subs.Channel) bool {
	return slices.EqualFunc(a.Members, b.Members, func(x, y subs.Channel) bool { return x.ID == y.ID })
}

// グループを外すとグループの定義 (メンバー) ごと消える (購読の一覧にしか定義が無い)。作り直すと別の ID になり、既読と隠した話題は
// 引き継がない。確認と知らせでそう伝える (チャンネルの「また付けられる」と同じ文にしない)。
func unsubVerb(c subs.Channel) string {
	if c.IsGroup() {
		return " のグループを消す"
	}
	return " の購読を外す"
}

func unsubBody(c subs.Channel) []string {
	if c.IsGroup() {
		return []string{"のグループを消しますか？", "(メンバーのチャンネルの購読は変わらない。", "作り直しても既読は引き継がない)"}
	}
	return []string{"の購読を外しますか？", "(s の購読の画面でまた付けられる)"}
}

// membersChanged はグループ i のメンバーが変わった (読み直しが走っていない) ときの後始末: 板は前のメンバーのものなので、
// 前の失敗を消して読み直しを待つ印 (stale) を立てる。今のタブならすぐ読み直し、他のタブは開いたときに読み直す (switchTo)。
func (m *Model) membersChanged(i int) tea.Cmd {
	c := &m.chans[i]
	c.redo, c.err, c.stale = false, nil, c.page != nil
	if i == m.cur || m.stream { // 新着のタブは全部のタブの板を並べるので、前のメンバーの板を並べ続けない
		return m.reload(i)
	}
	return nil
}

// enterStream は新着のタブを開く: 全部のタブの話題を並べ直す。板がまだ無いタブ (キャッシュも無い) は読み始める
// (キャッシュの前回の板があるタブは、その板を並べる。読み直しは r / R で)。
func (m *Model) enterStream() tea.Cmd {
	m.stream, m.body, m.streamRows = true, false, nil
	m.list = listnav.List{}
	m.pager.Reset()
	var cmds []tea.Cmd
	for i := range m.chans {
		if c := &m.chans[i]; c.page == nil && !c.loading && c.err == nil {
			cmds = append(cmds, m.reload(i))
		}
	}
	return tea.Batch(cmds...)
}

// streamUnread は新着のタブの未読の話題の数 (各タブの未読の数の合計)。
func (m *Model) streamUnread() int {
	n := 0
	for i := range m.chans {
		n += m.unreadCount(i)
	}
	return n
}

// streamState は新着のタブの状態の行: 読み込み中のタブと、前回の板 (キャッシュ) のままのタブの数。
func (m *Model) streamState() string {
	loading, stale, oldest := 0, 0, time.Time{}
	for _, c := range m.chans {
		switch {
		case c.loading:
			loading++
		case c.stale:
			stale++
		}
		if c.loading && (oldest.IsZero() || c.started.Before(oldest)) {
			oldest = c.started
		}
	}
	s := fmt.Sprintf("新着 (%d タブの話題を新しい順に)", len(m.chans))
	if stale > 0 {
		s += fmt.Sprintf("・%d タブは前回の板 (R で読み直す)", stale)
	}
	if loading > 0 {
		s = m.spin(oldest) + fmt.Sprintf(" %d タブを読んでいます・", loading) + s
	}
	return s
}

// hasSel は話題を選んでいるか (見出しに乗っている・一覧が空なら偽)。
func (m *Model) hasSel() bool {
	_, ok := m.selRow()
	return ok
}

// closeBody は本文の欄を閉じて一覧へ戻り、全幅の欄が右へ退く演出を始める。
func (m *Model) closeBody() {
	m.body = false
	m.opening.Close(m.now(), bodyOpenDuration)
}

// closingBody は一覧へ戻る演出の途中か。着地した後は tick が EndClose するまでの間も、もう一覧と並べた形で描く。
func (m *Model) closingBody() bool {
	return m.opening.Closing() && !m.opening.CloseLanded(m.now())
}

// openingRows は本文の欄を開く演出と一覧へ戻る逆の演出の途中の欄の行。一覧との境目だけが左右へ動き、境目の左に一覧、右に全幅で
// 描いた本文を最終の位置のまま出す。本文は最初から全幅で折り返す (動いている間に文の折り返しを変えない)。
// 本文を中身ごと横へずらさない: 毎フレーム全部のセルが変わり、24bit 色の背景で塗ったカードを毎回送り直すことになる。437×104 の端末で
// 1 フレーム 26〜77KB になり、tmux が外側の端末へ送るフレームが閉じる演出で 300ms に 4 枚ほどまで間引かれた (境目だけなら 5〜37KB で
// 17ms ごとに届く)。
func (m *Model) openingRows(right []string) []string {
	p := anim.EaseOutCubic(m.opening.Progress(m.now()))
	if m.opening.Closing() { // 閉じるときも動き出しが速く、着地の手前で減速する (開く動きを時間で逆に回すと着地が急になる)
		p = 1 - anim.EaseOutCubic(1-m.opening.Progress(m.now()))
	}
	x0 := m.leftWidth() + 1                           // 一覧と並べていたときの右の欄の左端
	x := min(max(int(float64(x0)*(1-p)+0.5), 0), m.w) // 今の右の欄の左端 (x0 から 0 へ)
	left := m.leftLines()
	out := make([]string, 0, m.paneRows())
	for i := range m.paneRows() {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		l = termwidth.Truncate(l, x, "")
		out = append(out, reset(l)+termwidth.PadSpaces(x-termwidth.Of(l))+termwidth.DropColumns(r, x))
	}
	return out
}
