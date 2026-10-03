package ui

import tea "charm.land/bubbletea/v2"

// join の画面から持ち主の画面への切り替え (issue 548)。O → y/N で確かめ、y なら ctrl+r と同じ暗転で終了して
// main に切り替えを任せる (main が --join を外した引数で同じバイナリを exec する。状態は ctrl+r と同じく引き継ぐ)。
// 持ち主の画面は起動のときに dispatcher を起こす (人が止めた印があれば起こさない。main.go の wireLive)。

// switchKind は暗転して終了する切り替えの行き先。
type switchKind int

const (
	switchNone    switchKind = iota
	switchUpgrade            // 新版 (ctrl+r。upgrade.go)
	switchOwner              // 持ち主の画面 (O。join の画面だけ)
)

// OwnerRequested は O で持ち主の画面への切り替えを頼まれて終了したか (main がそれを見て exec する)。
func (m *Model) OwnerRequested() bool { return m.switchRequested && m.switchTo == switchOwner }

// askOwnerSwitch は O。join の画面でだけ、持ち主の画面に切り替えるかを確かめる。
// 持ち主の画面は閉じると dispatcher と PG を止めるので、それを確認の文に書く (join は閉じても止めない)。
func (m *Model) askOwnerSwitch() {
	if !m.joined() {
		m.info("この画面は join の画面ではない (O は join の画面を持ち主の画面に切り替える)")
		return
	}
	m.pending, m.send = nil, nil
	m.pendingOwner = true
	m.confirmText = "この画面を持ち主の画面に切り替えます (dispatcher が止まっていれば起こす。人が止めてあれば起こさない。閉じると dispatcher と PG を止める画面になる)。よいですか? [y/N]"
	m.mode = modeConfirm
}

// confirmOwnerSwitch は確認の y。暗転を始め、暗くなりきったら終了する (switchfade.go の leaveDone)。
func (m *Model) confirmOwnerSwitch() tea.Cmd {
	m.pendingOwner = false
	m.mode = modeBoard
	m.switchTo = switchOwner
	return m.startLeaving()
}
