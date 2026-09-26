package ui

// PG が turn の途中のカードのバッジの頭に、回る印 (ぐるぐる) を出す。2026-09-25 のユーザーの依頼。
// 待っているカード (テストの係の結果待ち・再開待ち) には出さず、地を暗くする (issue 455。2026-09-26 のユーザーの決定。view.go の cardCell)。
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

// processing は、そのカードの PG が turn の途中か (PG の status が busy。claude agents --json の語)。
// 待っているカードは busy でも回さない (テストの係に頼んだ直後の turn の終わり際)。テストの係の実行中も「結果待ち」なので回さない (issue 455)。
func (m *Model) processing(c card.Card) bool {
	if waiting(c) {
		return false
	}
	for _, pg := range m.snap.Consumers {
		if pg.CardID == c.ID && pg.Status == "busy" {
			return true
		}
	}
	return false
}

// waiting は、PG が居るが止まって待っているカードか: 作業中の列でテストの係の結果を待っている (実行中も含む) /
// 分解済みの列で再開を待っている (一度起動した = session がある)。まだ起動していない分解済みのカードは待ちに含めない (issue 455)。
func waiting(c card.Card) bool {
	switch c.State {
	case card.Running:
		return c.AwaitsRun()
	case card.Planned:
		return c.Session != ""
	case card.Requested, card.Waiting, card.Review, card.Done:
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
