package ui

// 裏で処理中のカード (PG の turn の途中・テストの係が実行中) のバッジの頭に、回る印 (ぐるぐる) を出す。2026-09-25 のユーザーの依頼。
// 回すカードがある間だけ spinInterval で描き直し、無くなったら止める (演出の frame と同じく、アイドルで CPU を使わない)。

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"pro-con/card"
)

// spinFrames は回る印の 1 コマずつ。点字は端末で幅が揺れない 1 桁 (tuikit/termwidth の注記)。
var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinInterval は 1 コマの長さ。frame (33ms) より遅くてよい (滑らかさより、回っていることが分かれば足りる)。
const spinInterval = 100 * time.Millisecond

type spinMsg struct{}

// processing は、そのカードを裏で Claude かテストの係が処理している最中か。
// PG の turn の途中 = そのカードの PG の status が busy (claude agents --json の語)。テストの係 = 実行中のコマンドがある。
func (m *Model) processing(c card.Card) bool {
	if c.Exec.Active() {
		return true
	}
	for _, pg := range m.snap.Consumers {
		if pg.CardID == c.ID && pg.Status == "busy" {
			return true
		}
	}
	return false
}

// spinFrame は今のコマ (時刻から決める。描き直しの回数に依らず同じ時刻なら同じコマ)。
func (m *Model) spinFrame() string {
	return spinFrames[int(m.now().UnixMilli()/spinInterval.Milliseconds())%len(spinFrames)]
}

func (m *Model) anyProcessing() bool {
	for _, c := range m.snap.Cards {
		if m.processing(c) {
			return true
		}
	}
	return false
}

// trackSpin は、回すカードがあるのに tick が回っていなければ回し始める。Update の出口から呼ぶ。
func (m *Model) trackSpin() tea.Cmd {
	if m.spinning || !m.anyProcessing() {
		return nil
	}
	m.spinning = true
	return spinTick()
}

// onSpin は 1 コマ進める。回すカードが無くなったら止める。
func (m *Model) onSpin() tea.Cmd {
	if !m.anyProcessing() {
		m.spinning = false
		return nil
	}
	return spinTick()
}

func spinTick() tea.Cmd { return tea.Tick(spinInterval, func(time.Time) tea.Msg { return spinMsg{} }) }
