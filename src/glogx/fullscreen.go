package main

import (
	"glogx/internal/app"

	tea "charm.land/bubbletea/v2"
)

// activeFullScreen は今出ている全画面ビューア (無ければ FullScreenNone)。
//
// 高々 1 枚しか出ない前提なので順序は本来どれでもよいが、**ここを唯一の順序**にすることで
// 描画・hint・routing が同じ 1 枚を選ぶことが構造的に保証される (場所ごとに順序が違うと、
// 前提が崩れたときにどれが勝つかが場所によって変わる)。
func (m *browseModel) activeFullScreen() FullScreenID {
	switch {
	case m.rlDash.Visible():
		return FullScreenRatelimit
	case m.doctorOv.Visible():
		return FullScreenDoctor
	case m.statusOv.Visible():
		return FullScreenStatus
	case m.issuesOv.Visible():
		return FullScreenIssues
	case m.filerV.Visible():
		return FullScreenFiler
	case m.lanV.Visible():
		return FullScreenLan
	}
	return FullScreenNone
}

// fullScreenActive は全画面ビューアが 1 枚でも出ているか。
func (m *browseModel) fullScreenActive() bool { return m.activeFullScreen() != FullScreenNone }

// openFullScreen は全画面ビューア id を開く (一覧からの起動と、横断の着地の両方の唯一の口)。
// 🚨 呼ぶ時点で他の全画面が閉じていること (全画面は同時に 1 枚。toggle が「開く」でなく
// 「閉じる」に化ける)。横断元は閉じてから (演出のあるものは finishClose まで済ませてから) 呼ぶ。
func (m *browseModel) openFullScreen(id FullScreenID) tea.Cmd {
	switch id {
	case FullScreenIssues:
		return tea.Batch(m.issuesOv.Toggle(app.CurrentDir()), m.maybeTick())
	case FullScreenStatus:
		return tea.Batch(m.statusOv.Toggle(), m.maybeTick())
	case FullScreenRatelimit:
		return m.toggleRatelimitDash()
	case FullScreenDoctor:
		m.usageOv.Dismiss()
		return tea.Batch(m.doctorOv.Toggle(), m.maybeTick())
	case FullScreenFiler:
		m.usageOv.Dismiss()
		m.filerV.Toggle(app.CurrentDir())
		m.filerV.DrainNotices(m.deliverNotice) // 開けなかった理由 (エラー)
		return tea.Batch(m.filerV.WatchCmd(), m.maybeTick())
	case FullScreenLan:
		m.usageOv.Dismiss()
		m.lanV.Toggle()
		m.lanV.DrainNotices(m.deliverNotice) // 開けなかった理由・開いたときの読み込みの失敗 (台帳が読めない等)
		return tea.Batch(m.lanV.WatchCmd(), m.maybeTick())
	case FullScreenNone, FullScreenCount:
	}
	return nil
}
