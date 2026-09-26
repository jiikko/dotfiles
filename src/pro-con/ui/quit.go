package ui

// 終了。Q (または ctrl+c) で終了の入力欄を開き、quit と打って enter したときだけ閉じる。入力欄の上には、閉じると何が動き続けるか・
// 何を止めるかと、ほかに開いている画面の居場所 (tmux の pane / tty) をダイアログで出す (issue 519)。
// 本物のモード (backend.Stopper を持つ backend) は、終了のときに dispatcher と pro-con が起動した PG を止めてから閉じる
// (次に dispatcher を起動したら続きから再開する)。止めている間は「止めています」を出す。
// ライブアップグレード (ctrl+r) の終了は入れ替えであって終了ではないので、ここを通さない (upgrade.go)。

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"tuikit/confirm"
	"tuikit/layout"

	"pro-con/backend"
	"pro-con/card"
)

// stopWait は dispatcher と PG を止め終えるまで待つ上限 (dispatcher --stop の待ち 120 秒 + 余裕)。
const stopWait = 150 * time.Second

// paneWait は tmux に pane の一覧を聞く待ちの上限 (返らなければ tty のまま出す)。
const paneWait = 2 * time.Second

// stopDoneMsg は止め終えた (か、止めきれなかった) 知らせ。
type stopDoneMsg struct{ err error }

// quitPanesMsg は端末 → tmux の pane の名前の表 (backend.PaneNamer)。ask は問い合わせた回 (Model.quitAsk)。
type quitPanesMsg struct {
	ask   int
	panes map[string]string
}

// StopErr は終了のときに止めきれなかった理由 (無ければ nil)。画面を閉じた後に main が出す。
func (m *Model) StopErr() error { return m.stopErr }

// busyCards は動いている PG を持つカード (作業中・質問待ち) の枚数。タブに関係なく全部を数える。
func (m *Model) busyCards() (running, waiting int) {
	for _, c := range m.snap.Cards {
		switch c.State {
		case card.Running:
			running++
		case card.Waiting:
			if !c.Wait.FromPM() { // PM の問い (498) は PG を持たない
				waiting++
			}
		case card.Requested, card.Planned, card.Review, card.Done:
		}
	}
	return running, waiting
}

// requestQuit は終了の入力欄を開く。閉じるのは、そこへ quit と打って enter したときだけ (2026-09-25 にユーザーが決めた形。
// q や ctrl+c の 1 打で閉じない: 本物のモードの終了は dispatcher と PG を止めるので、打ち間違いで止めない)。
func (m *Model) requestQuit() tea.Cmd {
	if m.holdsDraft() { // 書きかけの文・選んだ答えは消さない
		m.refuse("終了は Q を押して quit と打つ (書きかけの入力はそのまま)")
		return nil
	}
	m.closeAll()
	m.startInput(inputQuit)
	m.quitPanes = nil // 前に開いたときの表を使わない (pane は動く)。引けるまでは tty のまま出す
	m.quitAsk++       // 前に開いたときの問い合わせが後から返っても使わない (quitPanesMsg)
	pn, ok := m.be.(backend.PaneNamer)
	if !ok {
		return nil
	}
	ask := m.quitAsk
	return m.child(func() tea.Msg { // 外のコマンド (tmux) を待つので、入れ替え (ctrl+r) はこれを待つ
		ctx, cancel := context.WithTimeout(context.Background(), paneWait)
		defer cancel()
		return quitPanesMsg{ask: ask, panes: pn.PaneNames(ctx)}
	})
}

// holdsDraft は書きかけの文・選んだ答えを持っている画面か (入力欄・回答フォームと、その送る前の確認)。
func (m *Model) holdsDraft() bool {
	switch m.mode {
	case modeForm:
		return true
	case modeInput:
		return m.inputKind != inputQuit
	case modeConfirm:
		return m.send != nil
	case modeBoard:
	}
	return false
}

// closeAll は開いている板を全部閉じる (終了の入力欄を、何かの板の上ではなくボードの上で開く)。
func (m *Model) closeAll() {
	m.legend = false
	m.picker.open = false
	for m.closeTop() {
	}
}

// submitQuit は終了の入力欄の enter。quit と打ったときだけ終了する。それ以外は取り消し。
func (m *Model) submitQuit() tea.Cmd {
	text := strings.TrimSpace(m.line.String())
	m.mode = modeBoard
	m.line.Reset()
	if text != "quit" {
		m.info("終了を取り消した (閉じるのは quit と打ったときだけ)")
		return nil
	}
	return m.quitNow()
}

// quitLabel は終了の入力欄の見出し。閉じると何が起きるかは見出しに書かず、ダイアログ (quitBody) にだけ出す
// (文を作る所を 1 つにする = issue 519)。
func (m *Model) quitLabel() string { return "終了するには quit と打って enter" }

// runningParts は dispatcher が動かしているもの (PG の本数は別の行に出す)。
const runningParts = "  dispatcher · supervisor · PM · 取り込みの係"

// quitBody は終了のダイアログの中身: この画面を閉じると何が起きるか (動いたまま / 止める) と、ほかに開いている画面。
// 🚨 ここに出す「残る / 止める」は開いた時点の見立て。閉じた時点で StopAll が数え直し、結論が変わったら閉じた後に main が
// 実際の結果 (KeptRunning / 止めきれなかった) を出す。
func (m *Model) quitBody() []string {
	r, w := m.busyCards()
	pgs := fmt.Sprintf("  PG (作業中 %d 本・質問待ち %d 本)", r, w)
	keep := func(why string) []string { // why は閉じても止めない理由 (板の幅に収まるよう結論と行を分ける)
		return []string{why, fg(114) + sgrBold + "この画面だけ閉じる" + sgrReset + "。動いたまま:", runningParts, pgs}
	}
	var body []string
	lastOwner := false
	switch _, stopper := m.be.(backend.Stopper); {
	case m.readOnly():
		body = keep("見ているだけの画面 (--view)")
	case m.joined():
		body = keep("join の画面 (--join)")
	case stopper:
		// 最後に閉じる持ち主の画面だけが止める (join の画面は数えない = issue 481)
		if owners, _ := m.snap.ScreenTally(); owners > 1 {
			body = keep(fmt.Sprintf("ほかに持ち主の画面が %d 開いている", owners-1))
		} else {
			lastOwner = true
			body = []string{fg(203) + sgrBold + "最後の持ち主の画面なので、止めて閉じる" + sgrReset + ":", pgs, runningParts,
				sgrDim + "  次に開くと続きから" + sgrReset}
		}
	default:
		return []string{"模擬なので、閉じると進み具合は消える"}
	}
	others := m.otherScreens()
	if len(others) == 0 {
		return body
	}
	body = append(append(body, "", sgrBold+"ほかに開いている画面"+sgrReset), others...)
	if _, joins := m.snap.ScreenTally(); lastOwner && joins > 0 {
		body = append(body, sgrYellow+"  → join の画面は、止めた後は表示が止まる"+sgrReset)
	}
	return body
}

// otherScreens はこの画面のほかに開いている画面を 1 行ずつ (モード・端末・開いた時刻)。端末は tmux の中なら pane の名前で出す。
func (m *Model) otherScreens() []string {
	var out []string
	for _, s := range m.snap.Screens {
		if s.Self {
			continue
		}
		where := s.TTY
		if p, ok := m.quitPanes[s.TTY]; ok {
			where = p + " (tmux)"
		}
		if where == "" {
			where = "端末不明"
		}
		out = append(out, "  · "+fit(screenMode(s), 14)+fit(where, 20)+s.Opened.Local().Format("15:04")+" から")
	}
	return out
}

// quitNow は終了する。backend が止める口を持てば (本物のモード)、止め終えてから閉じる。
func (m *Model) quitNow() tea.Cmd {
	st, ok := m.be.(backend.Stopper)
	if !ok {
		return tea.Quit
	}
	m.stopping = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), stopWait)
		defer cancel()
		return stopDoneMsg{err: st.StopAll(ctx)}
	}
}

// overlayQuit は確認のダイアログを画面の中央に重ねる。
func (m *Model) overlayQuit(screen []string) []string {
	if m.stopping {
		box := confirm.Dialog(" 終了 ", []string{"dispatcher と PG を止めています…", "", "ctrl+c: 待たずに閉じる"}, "", m.width, true)
		return layout.OverlayCentered(screen, box, m.width, len(screen), true)
	}
	if m.mode == modeInput && m.inputKind == inputQuit {
		// 入力欄 (quit と打つ所) は隠さない: 重ねるのはその上まで
		box := confirm.WideDialog(" 終了 ", m.quitBody(), "下の欄に quit と打って enter で閉じる · esc で取り消し", m.width, true)
		page := min(m.inputRow, len(screen))
		layout.OverlayCentered(screen[:page], box, m.width, page, true)
	}
	return screen
}
