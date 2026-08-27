// 予約入力ウィザードの状態機械。画面は 3 つ (menu / form / pick) で、決めた結果を result に残して
// 終了する。tmux や job ファイルには一切触れない (呼び出し側の scripts/tmux_schedule_keys.sh が行う):
// この分離があるので、破壊的な操作 (取消) の確認と実行はシェル側のテスト済み経路に残る。
package main

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type screen int

const (
	screenMenu screen = iota
	screenForm
	screenPick
)

// result は out ファイルに書く 1 行。action は "new" / "cancel" / "" (中止)。
type result struct {
	action string
	at     time.Time // new のときの発火時刻
	text   string    // new のときの送る文字列
	id     string    // cancel のときの予約 id
}

type model struct {
	screen  screen
	label   string // 送り先 pane の表示名
	now     time.Time
	jobs    []job
	menuIdx int
	form    formState
	pickIdx int
	res     result
	quit    bool
	// nowFn は「今」を返す。起動時に凍結した now で確定すると、popup に留まった時間だけ
	// 予約が前倒しになる (60 秒迷えば 5 分後が 4 分後に、放置すれば過去になって即送信される。
	// 敵対的レビュー 2026-08-28)。表示は起動時の now、確定は押した瞬間の now を使う
	nowFn func() time.Time
	// togglePrefix は tmux の prefix キー (例 "ctrl+t")。popup が開いている間 prefix は tmux の
	// キーテーブルへ届かず、そのままこの UI に入ってくる (隔離サーバで実測 2026-08-28)。
	// prefix に続けて m / Enter / C-m を受けたら閉じる = 起動キーの再入力でトグルになる
	togglePrefix string
	prefixArmed  bool
	toast        toast
	width        int
	height       int
}

func newModel(label string, now time.Time, jobs []job) *model {
	// popup の既定 (72x16) の内側。WindowSizeMsg が来る前の 1 フレームだけこの値で描く
	return &model{label: label, now: now, jobs: jobs, form: newForm(), width: 70, height: 14, nowFn: time.Now}
}

func (m *model) Init() tea.Cmd { return tickCmd() }

type tickMsg struct{}

// tickCmd は表示用の時計を 1 秒ごとに進める。
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case toastTickMsg:
		return m, m.toast.advance()
	case toastDoneMsg:
		m.toast.done = true
		m.quit = true
		return m, tea.Quit
	case tea.PasteMsg:
		// ⚠️ ペーストは KeyPressMsg では来ない (ブラケットペーストは既定で有効)。捨てると
		//    「あとで送るコマンドを貼る」がまったく効かない (敵対的レビュー 2026-08-28)
		if m.screen == screenForm && !m.toast.shown {
			m.form.paste(msg.Content)
		}
		return m, nil
	case tickMsg:
		// 表示用の時計。止めると「N 時に送る」が開いた時刻のままになり、時刻指定では画面と
		// 実際の予約が丸 1 日ずれて見える (敵対的レビュー 2026-08-28)
		m.now = m.submitNow()
		if m.quit {
			return m, nil
		}
		return m, tickCmd()
	case tea.KeyPressMsg:
		// トースト表示中は結果が確定済み。キーで状態を変えさせない (閉じるのは早めてよい)
		if m.toast.shown {
			if msg.String() == "ctrl+c" || msg.String() == "esc" || msg.String() == "enter" {
				m.quit = true
				return m, tea.Quit
			}
			return m, nil
		}
		return m, m.handleKey(msg)
	}
	return m, nil
}

// handleKey は画面ごとのキー処理。終了するときだけ tea.Quit を返す。
func (m *model) handleKey(k tea.KeyPressMsg) tea.Cmd {
	key := k.String()
	// 起動キーの再入力で閉じる (トグル)
	if m.togglePrefix != "" {
		if m.prefixArmed {
			m.prefixArmed = false
			if key == "m" || key == "ctrl+m" || key == "enter" {
				m.res = result{}
				m.quit = true
				return tea.Quit
			}
			// ⚠️ prefix に続く別のキーは「prefix 自身」も含めて処理し直す。飲み込むと、prefix が
			//    C-b の環境で入力欄の C-b (左移動) が効かなくなる (敵対的レビュー 2026-08-28)
			m.dispatch(m.togglePrefix, "")
		} else if key == m.togglePrefix {
			m.prefixArmed = true
			return nil
		}
	}
	if key == "ctrl+c" {
		m.res = result{}
		m.quit = true
		return tea.Quit
	}
	return m.dispatch(key, k.Text)
}

// dispatch は画面ごとのキー処理へ振り分ける。
func (m *model) dispatch(key, text string) tea.Cmd {
	switch m.screen {
	case screenMenu:
		return m.keyMenu(key)
	case screenForm:
		return m.keyForm(key, text)
	case screenPick:
		return m.keyPick(key)
	}
	return nil
}

func (m *model) keyMenu(key string) tea.Cmd {
	items := m.menuItems()
	switch key {
	case "esc", "q":
		m.quit = true
		return tea.Quit
	case "j", "down", "tab", "ctrl+n":
		m.menuIdx = (m.menuIdx + 1) % len(items)
	case "k", "up", "shift+tab", "ctrl+p":
		m.menuIdx = (m.menuIdx - 1 + len(items)) % len(items)
	case "enter":
		if it := items[m.menuIdx]; it.enabled {
			m.screen = it.target
		}
	}
	return nil
}

// menuItem はメニューの 1 項目。ラベル・遷移先・選べるかを 1 箇所に持つ。
// ⚠️ 以前はこの 3 つが keyMenu (index 0/1 の分岐) / menuItems (ラベル) / viewMenu (灰色表示) に
//
//	散っていて、項目を足すと 3 箇所を直す必要があった (直し漏れると「灰色なのに入れる」等になる)。
type menuItem struct {
	label   string
	target  screen
	enabled bool
}

func (m *model) menuItems() []menuItem {
	return []menuItem{
		{"新規予約", screenForm, true},
		{fmt.Sprintf("予約一覧・取消 (%d 件)", len(m.jobs)), screenPick, len(m.jobs) > 0},
	}
}

// submitNow は確定に使う「今」。テストでは nowFn を差し替えて固定する。
func (m *model) submitNow() time.Time {
	if m.nowFn == nil {
		return m.now
	}
	return m.nowFn()
}

func (m *model) keyForm(key, text string) tea.Cmd {
	switch key {
	case "esc":
		m.screen = screenMenu
		return nil
	case "enter":
		// 「いつ」と入力欄では Enter は次の欄へ進む。予約が確定するのは最後の欄 (文字列) だけ:
		// 候補を選んだ流れのまま Enter を押して、意図せず予約されるのを避ける (ユーザー要望 2026-08-28)
		if m.form.focus != focusText {
			if err := m.form.advance(m.submitNow()); err != "" {
				m.form.err = err
			}
			return nil
		}
		// ⚠️ 確定は「押した瞬間の今」で計算する (起動時の now で計算すると前倒しになる)
		at, txt, err := m.form.submit(m.submitNow())
		if err != "" {
			m.form.err = err
			return nil
		}
		m.res = result{action: "new", at: at, text: txt}
		// 結果はもう決まっている。トーストを見せてから閉じる (キーは以降受け付けない)
		return m.toast.start(fmt.Sprintf("予約しました  %s に送る (%s後)", at.Format("15:04"), formatRemaining(at.Sub(m.submitNow()))))
	}
	m.form.handleKey(key, text)
	return nil
}

func (m *model) keyPick(key string) tea.Cmd {
	switch key {
	case "esc", "q":
		m.screen = screenMenu
	case "j", "down", "ctrl+n":
		if m.pickIdx < len(m.jobs)-1 {
			m.pickIdx++
		}
	case "k", "up", "ctrl+p":
		if m.pickIdx > 0 {
			m.pickIdx--
		}
	case "enter":
		if len(m.jobs) == 0 {
			return nil
		}
		// 取消の確認と実行はシェル側 (gum confirm --default=false → cancel_job)。ここは選ぶだけ
		m.res = result{action: "cancel", id: m.jobs[m.pickIdx].id}
		m.quit = true
		return tea.Quit
	}
	return nil
}

func (m *model) View() tea.View {
	var v tea.View
	var body string
	var cur *tea.Cursor
	switch m.screen {
	case screenMenu:
		body = m.viewMenu()
	case screenForm:
		body, cur = m.form.view(m.label, m.now, m.width, m.height)
	case screenPick:
		body = m.viewPick()
	}
	if m.toast.shown {
		lines := strings.Split(body, "\n")
		body = strings.Join(m.toast.overlay(lines, m.width, m.height), "\n")
		cur = nil // トースト中はカーソルを出さない
	}
	v.SetContent(body)
	v.Cursor = cur // form のときだけ本物のカーソルを置く (IME の未確定文字がそこに出る)
	// alt-screen で描く。inline だと画面より高い描画で端末が流れ、次のフレームの再描画が
	// ずれて表示が二重になる / カーソル位置が行数分ずれる (2026-08-28 のユーザー報告)。
	// popup は元々全面を占めるので、alt-screen でも見た目は変わらない
	v.AltScreen = true
	return v
}

func (m *model) viewMenu() string {
	f := newFrame(m.width, m.height)
	f.add(sgr(fgDim, "予約入力") + "  " + sgr(fgAccent, truncate(m.label, maxInt(m.width-10, 0))))
	f.add("")
	for i, it := range m.menuItems() {
		f.add(row(i == m.menuIdx, !it.enabled, it.label))
	}
	f.add("")
	f.add(sgr(fgDim, help(m.width, "j/k C-n/C-p 移動", "Enter 決定", "Esc 閉じる")))
	body, _ := f.render()
	return body
}

func (m *model) viewPick() string {
	f := newFrame(m.width, m.height)
	f.add(sgr(fgDim, "予約一覧"))
	f.add("")
	remW, labelW := 0, 0
	for _, j := range m.jobs {
		remW = max(remW, ansi.StringWidth(formatRemaining(j.at.Sub(m.now))))
		labelW = max(labelW, ansi.StringWidth(j.label))
	}
	// ⚠️ 送り先の表示名は #{window_name} で、長さに上限が無い。桁揃えに使うと 1 件の長い名前が
	//    全行を押し出し、文字列の列が画面外へ消える (= 何を取り消すか読めないまま確認へ進む)
	labelW = min(labelW, maxInt(m.width/3, 8))
	textW := maxInt(m.width-2-remW-labelW-4, 4)
	for i, j := range m.jobs {
		line := fmt.Sprintf("%s  %s  %s",
			pad(formatRemaining(j.at.Sub(m.now)), remW),
			pad(truncate(j.label, labelW), labelW),
			truncate(j.text, textW))
		f.add(row(i == m.pickIdx, false, line))
	}
	f.add("")
	f.add(sgr(fgDim, help(m.width, "j/k C-n/C-p 移動", "Enter 取消", "Esc 戻る")))
	body, _ := f.render()
	return body
}

// row は一覧の 1 行。選択中は行頭の > と色で示す (太字だけでは分かりにくい、の指摘 2026-08-28)。
func row(selected, disabled bool, s string) string {
	switch {
	case selected:
		return sgr(revAccent+";"+bold, "> "+s)
	case disabled:
		return "  " + sgr(fgDim, s)
	default:
		return "  " + s
	}
}
