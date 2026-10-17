package main

import (
	"glogx/internal/app"

	tea "charm.land/bubbletea/v2"
)

// activeFullScreen は今出ている全画面ビューア (無ければ app.FullScreenNone)。
//
// 高々 1 枚しか出ない前提なので順序は本来どれでもよいが、**ここを唯一の順序**にすることで
// 描画・hint・routing が同じ 1 枚を選ぶことが構造的に保証される (以前は場所ごとに順序が
// 違い、前提が崩れたときにどれが勝つかが場所によって変わった)。
func (m *browseModel) activeFullScreen() app.FullScreenID {
	switch {
	case m.rlDash.Visible():
		return app.FullScreenRatelimit
	case m.doctorOv.Visible():
		return app.FullScreenDoctor
	case m.statusOv.Visible():
		return app.FullScreenStatus
	case m.issuesOv.Visible():
		return app.FullScreenIssues
	case m.filerV.Visible():
		return app.FullScreenFiler
	case m.lanV.Visible():
		return app.FullScreenLan
	}
	return app.FullScreenNone
}

// fullScreenActive は全画面ビューアが 1 枚でも出ているか。
func (m *browseModel) fullScreenActive() bool { return m.activeFullScreen() != app.FullScreenNone }

// openFullScreen は全画面ビューア id を開く (一覧からの起動と、横断の着地の両方の唯一の口)。
// 🚨 呼ぶ時点で他の全画面が閉じていること (全画面は同時に 1 枚。toggle が「開く」でなく
// 「閉じる」に化ける)。横断元は閉じてから (演出のあるものは finishClose まで済ませてから) 呼ぶ。
func (m *browseModel) openFullScreen(id app.FullScreenID) tea.Cmd {
	switch id {
	case app.FullScreenIssues:
		return tea.Batch(m.issuesOv.Toggle(app.CurrentDir()), m.maybeTick())
	case app.FullScreenStatus:
		return tea.Batch(m.statusOv.Toggle(), m.maybeTick())
	case app.FullScreenRatelimit:
		return m.toggleRatelimitDash()
	case app.FullScreenDoctor:
		m.usageOv.Dismiss()
		return tea.Batch(m.doctorOv.Toggle(), m.maybeTick())
	case app.FullScreenFiler:
		m.usageOv.Dismiss()
		m.filerV.Toggle(app.CurrentDir())
		if e := m.filerV.TakeOpenErr(); e != "" {
			m.showWarning(e)
		}
		return tea.Batch(m.filerV.WatchCmd(), m.maybeTick())
	case app.FullScreenLan:
		m.usageOv.Dismiss()
		m.lanV.Toggle()
		if e := m.lanV.TakeOpenErr(); e != "" {
			m.showWarning(e)
		}
		m.drainLanNotices() // 開いたときの読み込みの失敗 (台帳が読めない等)
		return tea.Batch(m.lanV.WatchCmd(), m.maybeTick())
	case app.FullScreenNone, app.FullScreenCount:
	}
	return nil
}
