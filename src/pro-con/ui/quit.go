package ui

// 終了。Q (または ctrl+c) で終了の入力欄を開き、quit と打って enter したときだけ閉じる。
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

// stopDoneMsg は止め終えた (か、止めきれなかった) 知らせ。
type stopDoneMsg struct{ err error }

// StopErr は終了のときに止めきれなかった理由 (無ければ nil)。画面を閉じた後に main が出す。
func (m *Model) StopErr() error { return m.stopErr }

// busyCards は動いている PG を持つカード (作業中・質問待ち) の枚数。タブに関係なく全部を数える。
func (m *Model) busyCards() (running, waiting int) {
	for _, c := range m.snap.Cards {
		switch c.State {
		case card.Running:
			running++
		case card.Waiting:
			waiting++
		case card.Requested, card.Planned, card.Review, card.Done:
		}
	}
	return running, waiting
}

// requestQuit は終了の入力欄を開く。閉じるのは、そこへ quit と打って enter したときだけ (2026-09-25 にユーザーが決めた形。
// q や ctrl+c の 1 打で閉じない: 本物のモードの終了は dispatcher と PG を止めるので、打ち間違いで止めない)。
func (m *Model) requestQuit() tea.Cmd {
	if m.mode == modeInput && m.inputKind != inputQuit { // 書きかけの文は消さない
		m.info("終了は Q を押して quit と打つ (書きかけの入力はそのまま)")
		return nil
	}
	m.closeAll()
	m.startInput(inputQuit)
	return nil
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

// quitLabel は終了の入力欄の見出し。
func (m *Model) quitLabel() string {
	r, w := m.busyCards()
	if _, ok := m.be.(backend.ReadOnly); ok {
		return "終了するには quit と打って enter (見ているだけ。dispatcher と PG は動いたまま)"
	}
	if _, ok := m.be.(backend.Stopper); ok {
		if n := m.snap.Screens - 1; n > 0 { // 最後に閉じる画面だけが止める (閉じる時点で dispatcher に聞き直す)
			return fmt.Sprintf("終了するには quit と打って enter (ほかに %d 画面が開いているので、この画面だけ閉じる。dispatcher と PG は動いたまま)", n)
		}
		return fmt.Sprintf("終了するには quit と打って enter (作業中 %d 本・質問待ち %d 本の PG と dispatcher を止めて閉じる。次に開くと続きから)", r, w)
	}
	return "終了するには quit と打って enter (模擬なので進み具合は消える)"
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
	return screen
}
