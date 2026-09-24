package ui

// 終了の確認。作業中・質問待ちのカードがあるときに q / ctrl+c で終了しようとしたら、tuikit/confirm のダイアログを中央に出す。
// ライブアップグレード (ctrl+r) の終了は入れ替えであって終了ではないので、ここを通さない (upgrade.go)。

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"tuikit/confirm"
	"tuikit/layout"

	"pro-con/card"
)

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
		return tea.Quit
	}
	m.quitAsk = true
	return nil
}

// handleQuitKey は確認中のキー。y / Enter で終了、確認中にもう一度 ctrl+c でも終了 (強制)。それ以外はすべて取り消し
// (docs/glogx-ui-guide.md §4。知らないキーで終了するのが最悪の失敗)。
func (m *Model) handleQuitKey(key string) tea.Cmd {
	m.quitAsk = false
	if confirm.IsYesStrict(key) || key == "ctrl+c" {
		return tea.Quit
	}
	m.flash = "終了を取り消した"
	return nil
}

// overlayQuit は確認のダイアログを画面の中央に重ねる。
func (m *Model) overlayQuit(screen []string) []string {
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
	box := confirm.Dialog(" 終了 ", body, confirm.HintYesOther, m.width, true)
	return layout.OverlayCentered(screen, box, m.width, len(screen), true)
}
