package ui

// 終了の確認。作業中・質問待ちのカードがあるときに q / ctrl+c で終了しようとしたら、tuikit/confirm のダイアログを中央に出す。
// 本物のモード (backend.Stopper を持つ backend) は、終了のときに daemon と pro-con が起動した PG を止めてから閉じる
// (次に daemon を起動したら続きから再開する)。止めている間は「止めています」を出す。
// ライブアップグレード (ctrl+r) の終了は入れ替えであって終了ではないので、ここを通さない (upgrade.go)。

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"tuikit/confirm"
	"tuikit/layout"

	"pro-con/backend"
	"pro-con/card"
)

// stopWait は daemon と PG を止め終えるまで待つ上限 (daemon --stop の待ち 120 秒 + 余裕)。
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

// requestQuit は終了を求める。動いているカードが無ければすぐ終了し、あれば確認を出す。
func (m *Model) requestQuit() tea.Cmd {
	if r, w := m.busyCards(); r+w == 0 {
		return m.quitNow()
	}
	m.quitAsk = true
	return nil
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

// handleQuitKey は確認中のキー。y / Enter で終了、確認中にもう一度 ctrl+c でも終了 (強制)。それ以外はすべて取り消し
// (docs/glogx-ui-guide.md §4。知らないキーで終了するのが最悪の失敗)。
func (m *Model) handleQuitKey(key string) tea.Cmd {
	m.quitAsk = false
	if confirm.IsYesStrict(key) || key == "ctrl+c" {
		return m.quitNow()
	}
	m.flash = "終了を取り消した"
	return nil
}

// overlayQuit は確認のダイアログを画面の中央に重ねる。
func (m *Model) overlayQuit(screen []string) []string {
	if m.stopping {
		box := confirm.Dialog(" 終了 ", []string{"daemon と PG を止めています…", "", "ctrl+c: 待たずに閉じる"}, "", m.width, true)
		return layout.OverlayCentered(screen, box, m.width, len(screen), true)
	}
	if !m.quitAsk {
		return screen
	}
	r, w := m.busyCards()
	// 🚨 板は confirm.MaxWidth (44 桁) で頭打ちなので、1 行に繋がず行を分ける (切れると確認の意味が欠ける)
	body := []string{
		"動いているカードがあります",
		fmt.Sprintf("(作業中 %d 枚・質問待ち %d 枚)", r, w),
		"本当に終了しますか?",
		"",
		"模擬の backend なので、",
		"終了すると進み具合は消えます",
	}
	if _, ok := m.be.(backend.Stopper); ok {
		body = []string{
			"動いている PG があります",
			fmt.Sprintf("(作業中 %d 本・質問待ち %d 本)", r, w),
			"PG と daemon を止めて終了しますか?",
			"",
			"次に pro-con daemon を起動すると",
			"続きから再開します",
		}
	}
	box := confirm.Dialog(" 終了 ", body, confirm.HintYesOther, m.width, true)
	return layout.OverlayCentered(screen, box, m.width, len(screen), true)
}
